package mcp

import (
	"path/filepath"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newOASTestServer builds a Server with a graph referencing richpetstore operations.
func newOASTestServer(t *testing.T) *Server {
	t.Helper()
	doc := loadRichPetstore(t)
	specPath := filepath.Join(testdataDir(), "richpetstore.yaml")

	g := &graph.Graph{
		OAS: specPath,
		Nodes: map[string]*graph.Node{
			"createPet": {
				Name:        "createPet",
				Description: "Create a new pet",
				Adapter:     "createPet",
				OAS:         &graph.OASRef{OperationID: "createPet"},
			},
			"getPet": {
				Name:        "getPet",
				Description: "Get a pet by ID",
				Adapter:     "getPet",
				OAS:         &graph.OASRef{OperationID: "getPet"},
			},
			"listPets": {
				Name:        "listPets",
				Description: "List all pets",
				Adapter:     "listPets",
				OAS:         &graph.OASRef{OperationID: "listPets"},
			},
			"updatePet": {
				Name:        "updatePet",
				Description: "Update an existing pet",
				Adapter:     "updatePet",
				OAS:         &graph.OASRef{OperationID: "updatePet"},
			},
			"deletePet": {
				Name:        "deletePet",
				Description: "Delete a pet",
				Adapter:     "deletePet",
				OAS:         &graph.OASRef{OperationID: "deletePet"},
			},
			"noOAS": {
				Name:        "noOAS",
				Description: "A node without OAS reference",
				Adapter:     "noOAS",
			},
		},
	}

	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		OASSpecs: map[string]*v3high.Document{
			specPath: doc,
		},
		GraphDir: testdataDir(),
	}
	return NewServer(ctx)
}

// --- list_oas_operations ---

func TestHandleListOASOperations_NoFilter(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleListOASOperations, nil)

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "6 operation(s)")
	assert.Contains(t, text, "listPets")
	assert.Contains(t, text, "createPet")
	assert.Contains(t, text, "listCategories")
}

func TestHandleListOASOperations_TagFilter(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleListOASOperations, map[string]any{"tag": "Pets"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "listPets")
	assert.Contains(t, text, "createPet")
	assert.NotContains(t, text, "listCategories")
}

func TestHandleListOASOperations_KeywordFilter(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleListOASOperations, map[string]any{"keyword": "categories"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "listCategories")
	assert.NotContains(t, text, "createPet")
}

func TestHandleListOASOperations_MethodFilter(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleListOASOperations, map[string]any{"method": "POST"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "createPet")
	assert.NotContains(t, text, "listPets")
	assert.NotContains(t, text, "getPet")
}

func TestHandleListOASOperations_CombinedFilters(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleListOASOperations, map[string]any{
		"tag":    "Pets",
		"method": "GET",
	})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "listPets")
	assert.Contains(t, text, "getPet")
	assert.NotContains(t, text, "createPet")
	assert.NotContains(t, text, "listCategories")
}

func TestHandleListOASOperations_GraphNodeMarker(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleListOASOperations, map[string]any{"keyword": "create"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	// createPet is mapped to a graph node
	assert.Contains(t, text, "createPet")
}

func TestHandleListOASOperations_NoMatches(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleListOASOperations, map[string]any{"keyword": "nonexistent"})

	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "No operations found")
}

func TestHandleListOASOperations_NoSpecs(t *testing.T) {
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		OASSpecs: map[string]*v3high.Document{},
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleListOASOperations, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "No OAS specs loaded")
}

func TestHandleListOASOperations_Deprecated(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleListOASOperations, map[string]any{"keyword": "delete"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "deprecated")
}

// --- list_oas_subtypes ---

func TestHandleListOASSubtypes_Discriminator(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleListOASSubtypes, map[string]any{"schema": "Animal"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Discriminator")
	assert.Contains(t, text, "animalType")
	assert.Contains(t, text, "Dog")
	assert.Contains(t, text, "Cat")
}

func TestHandleListOASSubtypes_OneOfVariants(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleListOASSubtypes, map[string]any{"schema": "Animal"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "oneOf Variants")
	assert.Contains(t, text, "Dog")
	assert.Contains(t, text, "Cat")
}

func TestHandleListOASSubtypes_AllOfChildren(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleListOASSubtypes, map[string]any{"schema": "Animal"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Extending Schemas")
	assert.Contains(t, text, "Dog")
	assert.Contains(t, text, "Cat")
}

func TestHandleListOASSubtypes_BaseEntity(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleListOASSubtypes, map[string]any{"schema": "BaseEntity"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	// Pet extends BaseEntity via allOf
	assert.Contains(t, text, "Extending Schemas")
	assert.Contains(t, text, "Pet")
}

func TestHandleListOASSubtypes_NonPolymorphic(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleListOASSubtypes, map[string]any{"schema": "Address"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "No polymorphic subtypes found")
}

func TestHandleListOASSubtypes_NotFound(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleListOASSubtypes, map[string]any{"schema": "Nonexistent"})

	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not found")
}

func TestHandleListOASSubtypes_MissingParam(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleListOASSubtypes, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

func TestHandleListOASSubtypes_NoSpecs(t *testing.T) {
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		OASSpecs: map[string]*v3high.Document{},
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleListOASSubtypes, map[string]any{"schema": "Animal"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "No OAS specs loaded")
}

// --- get_oas_operation ---

func TestHandleGetOASOperation_Valid(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleGetOASOperation, map[string]any{"node": "createPet"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "POST")
	assert.Contains(t, text, "/pets")
	assert.Contains(t, text, "Request Body")
	assert.Contains(t, text, "name")
}

func TestHandleGetOASOperation_WithParams(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleGetOASOperation, map[string]any{"node": "getPet"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "GET")
	assert.Contains(t, text, "/pets/{petId}")
	assert.Contains(t, text, "Parameters")
	assert.Contains(t, text, "petId")
	assert.Contains(t, text, "path")
}

func TestHandleGetOASOperation_ListWithQueryParams(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleGetOASOperation, map[string]any{"node": "listPets"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "GET")
	assert.Contains(t, text, "limit")
	assert.Contains(t, text, "status")
	assert.Contains(t, text, "query")
}

func TestHandleGetOASOperation_Responses(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleGetOASOperation, map[string]any{"node": "createPet"})

	text := resultText(t, result)
	assert.Contains(t, text, "Responses")
	assert.Contains(t, text, "201")
	assert.Contains(t, text, "400")
}

func TestHandleGetOASOperation_UnknownNode(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleGetOASOperation, map[string]any{"node": "nope"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "Unknown node")
}

func TestHandleGetOASOperation_NoOASRef(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleGetOASOperation, map[string]any{"node": "noOAS"})
	assert.True(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "no OAS reference")
	// Default (all) persona should suggest inspect_template.
	assert.Contains(t, text, "inspect_template")
	// Should include the adapter name.
	assert.Contains(t, text, "adapter: noOAS")
}

func TestHandleGetOASOperation_NoOASRef_IntegrationPersona(t *testing.T) {
	doc := loadRichPetstore(t)
	specPath := filepath.Join(testdataDir(), "richpetstore.yaml")

	g := &graph.Graph{
		OAS: specPath,
		Nodes: map[string]*graph.Node{
			"noOAS": {
				Name:    "noOAS",
				Adapter: "noOASAdapter",
			},
		},
	}

	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		OASSpecs: map[string]*v3high.Document{specPath: doc},
		GraphDir: testdataDir(),
	}
	srv := NewIntegrationServer(ctx)

	result := callTool(t, srv.handleGetOASOperation, map[string]any{"node": "noOAS"})
	assert.True(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "no OAS reference")
	// Integration persona should suggest inspect_request_template.
	assert.Contains(t, text, "inspect_request_template")
	assert.Contains(t, text, "adapter: noOASAdapter")
}

func TestHandleGetOASOperation_MissingParam(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleGetOASOperation, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

func TestHandleGetOASOperation_NoSpecs(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"test": {Name: "test", OAS: &graph.OASRef{OperationID: "testOp"}},
		},
	}
	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		OASSpecs: map[string]*v3high.Document{},
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleGetOASOperation, map[string]any{"node": "test"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "No OAS specs loaded")
}

// --- get_oas_schema ---

func TestHandleGetOASSchema_SimpleSchema(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleGetOASSchema, map[string]any{"schema": "Address"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "# Address")
	assert.Contains(t, text, "street")
	assert.Contains(t, text, "city")
	assert.Contains(t, text, "state")
}

func TestHandleGetOASSchema_AllOfSchema(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleGetOASSchema, map[string]any{"schema": "Pet"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "# Pet")
	assert.Contains(t, text, "Inherits from")
	assert.Contains(t, text, "BaseEntity")
	// Merged properties
	assert.Contains(t, text, "id")
	assert.Contains(t, text, "name")
	assert.Contains(t, text, "status")
}

func TestHandleGetOASSchema_CaseInsensitiveFallback(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleGetOASSchema, map[string]any{"schema": "address"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "# Address")
}

func TestHandleGetOASSchema_NotFound(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleGetOASSchema, map[string]any{"schema": "Nonexistent"})

	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not found")
}

func TestHandleGetOASSchema_CustomDepth(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleGetOASSchema, map[string]any{
		"schema": "Pet",
		"depth":  float64(1),
	})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "# Pet")
}

func TestHandleGetOASSchema_MaxDepth(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleGetOASSchema, map[string]any{
		"schema": "Pet",
		"depth":  float64(10), // should be clamped to 5
	})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "# Pet")
}

func TestHandleGetOASSchema_MissingParam(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleGetOASSchema, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

func TestHandleGetOASSchema_NoSpecs(t *testing.T) {
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		OASSpecs: map[string]*v3high.Document{},
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleGetOASSchema, map[string]any{"schema": "Pet"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "No OAS specs loaded")
}

// --- search_oas_schemas ---

func TestHandleSearchOASSchemas_MatchesByName(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleSearchOASSchemas, map[string]any{"pattern": "Pet"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Pet")
	assert.Contains(t, text, "CreatePetRequest")
	assert.Contains(t, text, "UpdatePetRequest")
}

func TestHandleSearchOASSchemas_CaseInsensitive(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleSearchOASSchemas, map[string]any{"pattern": "pet"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Pet")
}

func TestHandleSearchOASSchemas_RegexPattern(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleSearchOASSchemas, map[string]any{"pattern": "^Base"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "BaseEntity")
}

func TestHandleSearchOASSchemas_NoMatches(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleSearchOASSchemas, map[string]any{"pattern": "Nonexistent"})

	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "No schemas matching")
}

func TestHandleSearchOASSchemas_InvalidRegex(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleSearchOASSchemas, map[string]any{"pattern": "[invalid"})

	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "Invalid pattern")
}

func TestHandleSearchOASSchemas_NoFilters(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleSearchOASSchemas, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "At least one filter required")
}

func TestHandleSearchOASSchemas_HasPropertyFilter(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleSearchOASSchemas, map[string]any{"has_property": "street"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Address")
	// Pet also has address via allOf but not street directly — CreatePetRequest has address ref
	assert.NotContains(t, text, "Category")
}

func TestHandleSearchOASSchemas_ExtendsFilter(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleSearchOASSchemas, map[string]any{"extends": "BaseEntity"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Pet")
	assert.NotContains(t, text, "BaseEntity")
	assert.NotContains(t, text, "Address")
}

func TestHandleSearchOASSchemas_CombinedFilters(t *testing.T) {
	srv := newOASTestServer(t)
	// Pattern + hasProperty: schemas matching "Pet" that have a "name" property
	result := callTool(t, srv.handleSearchOASSchemas, map[string]any{
		"pattern":      "Pet",
		"has_property": "name",
	})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "CreatePetRequest")
	assert.Contains(t, text, "UpdatePetRequest")
}

func TestHandleSearchOASSchemas_NoMatchesWithFilters(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleSearchOASSchemas, map[string]any{
		"extends": "NonexistentBase",
	})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "No schemas matching")
}

func TestHandleSearchOASSchemas_HasDiscriminatorFilter(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleSearchOASSchemas, map[string]any{"has_discriminator": true})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Animal")
	assert.NotContains(t, text, "Pet")
	assert.NotContains(t, text, "Address")
}

func TestHandleSearchOASSchemas_PatternOnlyBackwardCompat(t *testing.T) {
	srv := newOASTestServer(t)
	// Verify pattern-only still works (backward compatibility)
	result := callTool(t, srv.handleSearchOASSchemas, map[string]any{"pattern": "^Create"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "CreatePetRequest")
}

func TestHandleSearchOASSchemas_NoSpecs(t *testing.T) {
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		OASSpecs: map[string]*v3high.Document{},
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleSearchOASSchemas, map[string]any{"pattern": "Pet"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "No OAS specs loaded")
}

// --- validate_oas_request ---

func TestHandleValidateOASRequest_Valid(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleValidateOASRequest, map[string]any{
		"node":    "createPet",
		"payload": `{"name":"Fluffy","species":"cat","status":"available"}`,
	})

	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "valid")
}

func TestHandleValidateOASRequest_MissingRequiredFields(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleValidateOASRequest, map[string]any{
		"node":    "createPet",
		"payload": `{"status":"available"}`,
	})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Validation failed")
	assert.Contains(t, text, "name")
	assert.Contains(t, text, "species")
}

func TestHandleValidateOASRequest_InvalidEnum(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleValidateOASRequest, map[string]any{
		"node":    "createPet",
		"payload": `{"name":"Fluffy","species":"cat","status":"invalid"}`,
	})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Validation failed")
	assert.Contains(t, text, "enum")
}

func TestHandleValidateOASRequest_InvalidJSON(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleValidateOASRequest, map[string]any{
		"node":    "createPet",
		"payload": `{invalid json}`,
	})

	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "Invalid JSON")
}

func TestHandleValidateOASRequest_NoRequestBody(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleValidateOASRequest, map[string]any{
		"node":    "getPet",
		"payload": `{}`,
	})

	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "no request body")
}

func TestHandleValidateOASRequest_UnknownNode(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleValidateOASRequest, map[string]any{
		"node":    "nope",
		"payload": `{}`,
	})

	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "Unknown node")
}

func TestHandleValidateOASRequest_MissingParams(t *testing.T) {
	srv := newOASTestServer(t)

	result := callTool(t, srv.handleValidateOASRequest, nil)
	assert.True(t, result.IsError)

	result = callTool(t, srv.handleValidateOASRequest, map[string]any{"node": "createPet"})
	assert.True(t, result.IsError)
}

func TestHandleValidateOASRequest_ConstraintViolation(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleValidateOASRequest, map[string]any{
		"node":    "createPet",
		"payload": `{"name":"","species":"cat","age":300}`,
	})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Validation failed")
	// name minLength violation
	assert.Contains(t, text, "name")
	// age maximum violation
	assert.Contains(t, text, "age")
}

// --- build_oas_example ---

func TestHandleBuildOASExample_Typical(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleBuildOASExample, map[string]any{"node": "createPet"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "typical")
	assert.Contains(t, text, "```json")
	assert.Contains(t, text, "name")
	assert.Contains(t, text, "species")
}

func TestHandleBuildOASExample_Minimal(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleBuildOASExample, map[string]any{
		"node":     "createPet",
		"scenario": "minimal",
	})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "minimal")
	assert.Contains(t, text, "name")
	assert.Contains(t, text, "species")
	// Minimal should not include optional fields like age
	assert.NotContains(t, text, "\"age\"")
}

func TestHandleBuildOASExample_Full(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleBuildOASExample, map[string]any{
		"node":     "createPet",
		"scenario": "full",
	})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "full")
	assert.Contains(t, text, "name")
	assert.Contains(t, text, "species")
	assert.Contains(t, text, "status")
	assert.Contains(t, text, "age")
}

func TestHandleBuildOASExample_InvalidScenario(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleBuildOASExample, map[string]any{
		"node":     "createPet",
		"scenario": "extreme",
	})

	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "Invalid scenario")
}

func TestHandleBuildOASExample_NoRequestBody(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleBuildOASExample, map[string]any{"node": "getPet"})

	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "no request body")
}

func TestHandleBuildOASExample_UnknownNode(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleBuildOASExample, map[string]any{"node": "nope"})

	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "Unknown node")
}

func TestHandleBuildOASExample_MissingParam(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleBuildOASExample, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

func TestHandleBuildOASExample_RequiredFieldsListed(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleBuildOASExample, map[string]any{"node": "createPet"})

	text := resultText(t, result)
	assert.Contains(t, text, "Required fields")
	assert.Contains(t, text, "name")
	assert.Contains(t, text, "species")
}

func TestHandleBuildOASExample_CustomValues(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleBuildOASExample, map[string]any{
		"node":          "createPet",
		"custom_values": `{"name":"Buddy","species":"dog"}`,
	})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, `"name": "Buddy"`)
	assert.Contains(t, text, `"species": "dog"`)
}

func TestHandleBuildOASExample_CustomValuesNested(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleBuildOASExample, map[string]any{
		"node":          "createPet",
		"scenario":      "full",
		"custom_values": `{"address":{"city":"Portland"}}`,
	})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, `"city": "Portland"`)
	// Other address fields should still be present from the example
	assert.Contains(t, text, `"street"`)
}

func TestHandleBuildOASExample_CustomValuesInvalidJSON(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleBuildOASExample, map[string]any{
		"node":          "createPet",
		"custom_values": `{invalid}`,
	})

	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "Invalid custom_values JSON")
}

func TestHandleBuildOASExample_CustomValuesEmpty(t *testing.T) {
	srv := newOASTestServer(t)
	// Empty string should be treated as no custom values
	result := callTool(t, srv.handleBuildOASExample, map[string]any{
		"node":          "createPet",
		"custom_values": "",
	})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "```json")
}

func TestHandleBuildOASExample_UpdatePet(t *testing.T) {
	srv := newOASTestServer(t)
	result := callTool(t, srv.handleBuildOASExample, map[string]any{"node": "updatePet"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "```json")
}

// --- findSchemaByName ---

func TestFindSchemaByName_ExactMatch(t *testing.T) {
	srv := newOASTestServer(t)
	schema, name, specPath := srv.findSchemaByName("Pet")
	require.NotNil(t, schema)
	assert.Equal(t, "Pet", name)
	assert.NotEmpty(t, specPath)
}

func TestFindSchemaByName_CaseInsensitive(t *testing.T) {
	srv := newOASTestServer(t)
	schema, name, _ := srv.findSchemaByName("pet")
	require.NotNil(t, schema)
	assert.Equal(t, "Pet", name)
}

func TestFindSchemaByName_NotFound(t *testing.T) {
	srv := newOASTestServer(t)
	schema, _, _ := srv.findSchemaByName("DoesNotExist")
	assert.Nil(t, schema)
}

// --- searchSchemaNames ---

func TestSearchSchemaNames_MatchesMultiple(t *testing.T) {
	srv := newOASTestServer(t)
	results, err := srv.searchSchemaNames("Pet")
	require.NoError(t, err)

	total := 0
	for _, names := range results {
		total += len(names)
	}
	assert.GreaterOrEqual(t, total, 3) // Pet, CreatePetRequest, UpdatePetRequest
}

func TestSearchSchemaNames_InvalidRegex(t *testing.T) {
	srv := newOASTestServer(t)
	_, err := srv.searchSchemaNames("[invalid")
	assert.Error(t, err)
}

// --- suggestSchemaNames ---

func TestSuggestSchemaNames(t *testing.T) {
	srv := newOASTestServer(t)
	suggestions := srv.suggestSchemaNames("pet")
	assert.NotEmpty(t, suggestions)
	// Should contain Pet-related names
	found := false
	for _, s := range suggestions {
		if s == "Pet" {
			found = true
			break
		}
	}
	assert.True(t, found)
}
