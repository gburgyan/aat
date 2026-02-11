package mcp

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gburgyan/aat/graph/oas"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testdataDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata")
}

func loadRichPetstore(t *testing.T) *v3high.Document {
	t.Helper()
	doc, err := oas.LoadSpec(filepath.Join(testdataDir(), "richpetstore.yaml"))
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc
}

func findSchema(t *testing.T, doc *v3high.Document, name string) *base.Schema {
	t.Helper()
	require.NotNil(t, doc.Components)
	require.NotNil(t, doc.Components.Schemas)
	proxy := doc.Components.Schemas.GetOrZero(name)
	require.NotNil(t, proxy, "schema %q not found", name)
	s := proxy.Schema()
	require.NotNil(t, s, "schema %q resolved to nil", name)
	return s
}

// --- resolveSchema ---

func TestResolveSchema_SimpleObject(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "Address")

	rs := resolveSchema(schema, "Address", 3, make(map[*base.Schema]bool))

	assert.Equal(t, "Address", rs.Name)
	assert.Equal(t, "object", rs.Type)
	assert.False(t, rs.Truncated)

	// Should have 4 properties: street, city, state, zipCode
	assert.Len(t, rs.Properties, 4)

	// Check required
	assert.Contains(t, rs.Required, "street")
	assert.Contains(t, rs.Required, "city")

	// Find street property and check constraints
	var street *resolvedProperty
	for i := range rs.Properties {
		if rs.Properties[i].Name == "street" {
			street = &rs.Properties[i]
			break
		}
	}
	require.NotNil(t, street)
	assert.True(t, street.Required)
	require.NotNil(t, street.Schema)
	assert.NotNil(t, street.Schema.MinLength)
	assert.NotNil(t, street.Schema.MaxLength)
}

func TestResolveSchema_AllOfInheritance(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "Pet")

	rs := resolveSchema(schema, "Pet", 3, make(map[*base.Schema]bool))

	assert.Equal(t, "Pet", rs.Name)
	assert.Equal(t, "object", rs.Type)
	assert.False(t, rs.Truncated)

	// Should include allOf breadcrumb
	assert.Contains(t, rs.AllOfSources, "BaseEntity")

	// Should have merged properties from BaseEntity + Pet-specific
	propNames := make(map[string]bool)
	for _, p := range rs.Properties {
		propNames[p.Name] = true
	}
	// From BaseEntity
	assert.True(t, propNames["id"])
	assert.True(t, propNames["createdAt"])
	assert.True(t, propNames["updatedAt"])
	// From Pet inline
	assert.True(t, propNames["name"])
	assert.True(t, propNames["status"])
	assert.True(t, propNames["species"])
	assert.True(t, propNames["age"])
	assert.True(t, propNames["address"])
	assert.True(t, propNames["tags"])

	// Required should merge from both
	assert.Contains(t, rs.Required, "id")   // from BaseEntity
	assert.Contains(t, rs.Required, "name") // from Pet inline
	assert.Contains(t, rs.Required, "status")
}

func TestResolveSchema_NestedRef(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "Pet")

	rs := resolveSchema(schema, "Pet", 3, make(map[*base.Schema]bool))

	// Find the address property - it should be resolved via $ref
	var address *resolvedProperty
	for i := range rs.Properties {
		if rs.Properties[i].Name == "address" {
			address = &rs.Properties[i]
			break
		}
	}
	require.NotNil(t, address)
	require.NotNil(t, address.Schema)
	assert.Equal(t, "object", address.Schema.Type)
	assert.Greater(t, len(address.Schema.Properties), 0, "address should have resolved properties")
}

func TestResolveSchema_Enum(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "CreatePetRequest")

	rs := resolveSchema(schema, "CreatePetRequest", 3, make(map[*base.Schema]bool))

	// Find status property
	var status *resolvedProperty
	for i := range rs.Properties {
		if rs.Properties[i].Name == "status" {
			status = &rs.Properties[i]
			break
		}
	}
	require.NotNil(t, status)
	require.NotNil(t, status.Schema)
	assert.Equal(t, []string{"available", "pending", "sold"}, status.Schema.Enum)
}

func TestResolveSchema_CycleDetection(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "TreeNode")

	rs := resolveSchema(schema, "TreeNode", 5, make(map[*base.Schema]bool))

	assert.Equal(t, "TreeNode", rs.Name)
	assert.Equal(t, "object", rs.Type)
	assert.False(t, rs.Truncated)

	// Find children property
	var children *resolvedProperty
	for i := range rs.Properties {
		if rs.Properties[i].Name == "children" {
			children = &rs.Properties[i]
			break
		}
	}
	require.NotNil(t, children)
	require.NotNil(t, children.Schema)
	assert.Equal(t, "array", children.Schema.Type)

	// Items should be the self-reference — at some depth it should be truncated
	require.NotNil(t, children.Schema.Items)
	// The depth limit should eventually truncate the recursive structure.
	// Walk down until we find truncation on either the array or the items.
	found := false
	current := children.Schema.Items
	for depth := 0; depth < 10 && current != nil; depth++ {
		if current.Truncated {
			found = true
			break
		}
		// Look for children in current
		var childProp *resolvedProperty
		for i := range current.Properties {
			if current.Properties[i].Name == "children" {
				childProp = &current.Properties[i]
				break
			}
		}
		if childProp == nil || childProp.Schema == nil {
			break
		}
		// The array schema itself may be truncated at depth limit
		if childProp.Schema.Truncated {
			found = true
			break
		}
		if childProp.Schema.Items == nil {
			// Items not resolved due to depth limit — effectively truncated
			found = true
			break
		}
		current = childProp.Schema.Items
	}
	assert.True(t, found, "recursion should be limited by depth")
}

func TestResolveSchema_DepthLimit(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "Pet")

	// Depth 0 should return truncated stub
	rs := resolveSchema(schema, "Pet", 0, make(map[*base.Schema]bool))
	assert.True(t, rs.Truncated)
	assert.Len(t, rs.Properties, 0)

	// Depth 1 should have properties but nested objects may be truncated
	rs = resolveSchema(schema, "Pet", 1, make(map[*base.Schema]bool))
	assert.False(t, rs.Truncated)
	assert.Greater(t, len(rs.Properties), 0)
}

func TestResolveSchema_NilSchema(t *testing.T) {
	rs := resolveSchema(nil, "Nil", 3, make(map[*base.Schema]bool))
	assert.True(t, rs.Truncated)
}

func TestResolveSchema_ArrayType(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "CreatePetRequest")

	rs := resolveSchema(schema, "CreatePetRequest", 3, make(map[*base.Schema]bool))

	// Find tags property
	var tags *resolvedProperty
	for i := range rs.Properties {
		if rs.Properties[i].Name == "tags" {
			tags = &rs.Properties[i]
			break
		}
	}
	require.NotNil(t, tags)
	require.NotNil(t, tags.Schema)
	assert.Equal(t, "array", tags.Schema.Type)
	require.NotNil(t, tags.Schema.Items)
	assert.Equal(t, "string", tags.Schema.Items.Type)
}

// --- validatePayload ---

func TestValidatePayload_ValidObject(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "CreatePetRequest")

	payload := map[string]any{
		"name":    "Fluffy",
		"species": "cat",
		"status":  "available",
		"age":     float64(3),
	}

	errs := validatePayload(payload, schema, "$", 5)
	assert.Empty(t, errs)
}

func TestValidatePayload_MissingRequired(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "CreatePetRequest")

	payload := map[string]any{
		"status": "available",
	}

	errs := validatePayload(payload, schema, "$", 5)
	assert.NotEmpty(t, errs)

	// Should flag name and species as missing
	paths := make(map[string]bool)
	for _, e := range errs {
		paths[e.Path] = true
	}
	assert.True(t, paths["$.name"])
	assert.True(t, paths["$.species"])
}

func TestValidatePayload_InvalidEnum(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "CreatePetRequest")

	payload := map[string]any{
		"name":    "Fluffy",
		"species": "cat",
		"status":  "unknown",
	}

	errs := validatePayload(payload, schema, "$", 5)
	assert.NotEmpty(t, errs)

	found := false
	for _, e := range errs {
		if e.Path == "$.status" {
			found = true
			assert.Contains(t, e.Message, "not in enum")
		}
	}
	assert.True(t, found)
}

func TestValidatePayload_StringConstraints(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "Address")

	payload := map[string]any{
		"street":  "", // minLength: 1
		"city":    "Portland",
		"state":   "Oregon", // pattern: ^[A-Z]{2}$
		"zipCode": "abc",    // pattern: ^\d{5}(-\d{4})?$
	}

	errs := validatePayload(payload, schema, "$", 5)
	assert.NotEmpty(t, errs)

	pathMessages := make(map[string]string)
	for _, e := range errs {
		pathMessages[e.Path] = e.Message
	}
	assert.Contains(t, pathMessages["$.street"], "minimum")
	assert.Contains(t, pathMessages["$.state"], "pattern")
	assert.Contains(t, pathMessages["$.zipCode"], "pattern")
}

func TestValidatePayload_NumericConstraints(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "CreatePetRequest")

	payload := map[string]any{
		"name":    "Fluffy",
		"species": "cat",
		"age":     float64(300), // maximum: 200
	}

	errs := validatePayload(payload, schema, "$", 5)
	assert.NotEmpty(t, errs)

	found := false
	for _, e := range errs {
		if e.Path == "$.age" {
			found = true
			assert.Contains(t, e.Message, "maximum")
		}
	}
	assert.True(t, found)
}

func TestValidatePayload_TypeMismatch(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "CreatePetRequest")

	payload := map[string]any{
		"name":    123, // expected string
		"species": "cat",
	}

	errs := validatePayload(payload, schema, "$", 5)
	assert.NotEmpty(t, errs)

	found := false
	for _, e := range errs {
		if e.Path == "$.name" {
			found = true
			assert.Contains(t, e.Message, "expected string")
		}
	}
	assert.True(t, found)
}

func TestValidatePayload_AllOfMerge(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "Pet")

	// Pet uses allOf — requires "id" from BaseEntity and "name", "status" from inline
	payload := map[string]any{
		"name": "Spot",
		// missing: id (from BaseEntity), status (from inline)
	}

	errs := validatePayload(payload, schema, "$", 5)
	assert.NotEmpty(t, errs)

	paths := make(map[string]bool)
	for _, e := range errs {
		paths[e.Path] = true
	}
	assert.True(t, paths["$.id"], "should flag missing id from BaseEntity")
	assert.True(t, paths["$.status"], "should flag missing status from Pet inline")
}

func TestValidatePayload_NestedObject(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "CreatePetRequest")

	payload := map[string]any{
		"name":    "Fluffy",
		"species": "cat",
		"address": map[string]any{
			// missing required: street, city
		},
	}

	errs := validatePayload(payload, schema, "$", 5)
	assert.NotEmpty(t, errs)

	paths := make(map[string]bool)
	for _, e := range errs {
		paths[e.Path] = true
	}
	assert.True(t, paths["$.address.street"])
	assert.True(t, paths["$.address.city"])
}

// --- buildExample ---

func TestBuildExample_Minimal(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "CreatePetRequest")

	ex := buildExample(schema, "minimal", 5, make(map[*base.Schema]bool))
	obj, ok := ex.(map[string]any)
	require.True(t, ok)

	// Minimal should only have required fields: name, species
	assert.Contains(t, obj, "name")
	assert.Contains(t, obj, "species")
	assert.NotContains(t, obj, "status")
	assert.NotContains(t, obj, "age")
}

func TestBuildExample_Typical(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "CreatePetRequest")

	ex := buildExample(schema, "typical", 5, make(map[*base.Schema]bool))
	obj, ok := ex.(map[string]any)
	require.True(t, ok)

	// Typical should include all fields
	assert.Contains(t, obj, "name")
	assert.Contains(t, obj, "species")
	assert.Contains(t, obj, "status")
	assert.Contains(t, obj, "age")
}

func TestBuildExample_EnumValues(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "CreatePetRequest")

	ex := buildExample(schema, "full", 5, make(map[*base.Schema]bool))
	obj, ok := ex.(map[string]any)
	require.True(t, ok)

	// status enum should use first value
	assert.Equal(t, "available", obj["status"])
}

func TestBuildExample_FormatDefaults(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "BaseEntity")

	ex := buildExample(schema, "full", 5, make(map[*base.Schema]bool))
	obj, ok := ex.(map[string]any)
	require.True(t, ok)

	// uuid format
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", obj["id"])
	// date-time format
	assert.Equal(t, "2026-03-15T10:30:00Z", obj["createdAt"])
}

func TestBuildExample_AllOfMerge(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "Pet")

	ex := buildExample(schema, "minimal", 5, make(map[*base.Schema]bool))
	obj, ok := ex.(map[string]any)
	require.True(t, ok)

	// Should have required from BaseEntity (id) and Pet inline (name, status)
	assert.Contains(t, obj, "id")
	assert.Contains(t, obj, "name")
	assert.Contains(t, obj, "status")
}

func TestBuildExample_CycleHandling(t *testing.T) {
	doc := loadRichPetstore(t)
	schema := findSchema(t, doc, "TreeNode")

	// Should not panic on self-referencing schema
	ex := buildExample(schema, "full", 5, make(map[*base.Schema]bool))
	obj, ok := ex.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, obj, "value")
}

// --- format helpers ---

func TestFormatResolvedSchema_BasicOutput(t *testing.T) {
	rs := &resolvedSchema{
		Name:        "TestSchema",
		Description: "A test schema",
		Type:        "object",
		Properties: []resolvedProperty{
			{Name: "id", Schema: &resolvedSchema{Type: "string", Format: "uuid"}, Required: true},
			{Name: "name", Schema: &resolvedSchema{Type: "string"}, Required: true},
			{Name: "age", Schema: &resolvedSchema{Type: "integer"}, Required: false},
		},
		Required: []string{"id", "name"},
	}

	text := formatResolvedSchema(rs)
	assert.Contains(t, text, "# TestSchema")
	assert.Contains(t, text, "A test schema")
	assert.Contains(t, text, "**Type:** object")
	assert.Contains(t, text, "## Properties")
	assert.Contains(t, text, "| id |")
	assert.Contains(t, text, "| name |")
	assert.Contains(t, text, "| age |")
}

func TestFormatResolvedSchema_AllOfBreadcrumb(t *testing.T) {
	rs := &resolvedSchema{
		Name:         "Pet",
		Type:         "object",
		AllOfSources: []string{"BaseEntity"},
		Properties:   []resolvedProperty{},
	}

	text := formatResolvedSchema(rs)
	assert.Contains(t, text, "**Inherits from:** BaseEntity")
}

func TestFormatResolvedSchema_Truncated(t *testing.T) {
	rs := &resolvedSchema{
		Name:      "Deep",
		Type:      "object",
		Truncated: true,
	}

	text := formatResolvedSchema(rs)
	assert.Contains(t, text, "truncated")
}

func TestFormatValidationErrors(t *testing.T) {
	errs := []validationError{
		{Path: "$.name", Message: "required field missing"},
		{Path: "$.age", Message: "value 300 > maximum 200"},
	}

	text := formatValidationErrors(errs)
	assert.Contains(t, text, "2 error(s)")
	assert.Contains(t, text, "$.name")
	assert.Contains(t, text, "$.age")
}

// --- utility helpers ---

func TestRefBaseName(t *testing.T) {
	assert.Equal(t, "Pet", refBaseName("#/components/schemas/Pet"))
	assert.Equal(t, "Address", refBaseName("#/components/schemas/Address"))
	assert.Equal(t, "standalone", refBaseName("standalone"))
}

func TestJoinPath(t *testing.T) {
	assert.Equal(t, "$.name", joinPath("$", "name"))
	assert.Equal(t, "$.name", joinPath("", "name"))
	assert.Equal(t, "$.address.street", joinPath("$.address", "street"))
}

func TestSchemaType(t *testing.T) {
	assert.Equal(t, "", schemaType(nil))
}
