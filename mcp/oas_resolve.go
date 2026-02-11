package mcp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gburgyan/aat/graph/oas"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// resolvedSchema is a flattened, depth-limited representation of an OAS schema.
type resolvedSchema struct {
	Name         string
	Description  string
	Type         string
	Format       string
	Properties   []resolvedProperty
	Required     []string
	Enum         []string
	Items        *resolvedSchema // array element schema
	AllOfSources []string        // inheritance breadcrumb
	Truncated    bool            // depth limit or cycle hit

	// Validation constraints
	MinLength *int64
	MaxLength *int64
	Minimum   *float64
	Maximum   *float64
	Pattern   string
}

// resolvedProperty is a single property within a resolved schema.
type resolvedProperty struct {
	Name        string
	Description string
	Schema      *resolvedSchema
	Required    bool
}

// validationError represents a single validation issue with a JSON path.
type validationError struct {
	Path    string
	Message string
}

// resolveSchema resolves a base.Schema to a resolvedSchema with depth limiting
// and cycle detection. The seen map tracks schema pointers already visited.
func resolveSchema(schema *base.Schema, name string, depth int, seen map[*base.Schema]bool) *resolvedSchema {
	if schema == nil {
		return &resolvedSchema{Name: name, Truncated: true}
	}

	// Cycle detection
	if seen[schema] {
		return &resolvedSchema{
			Name:      name,
			Type:      schemaType(schema),
			Truncated: true,
		}
	}
	seen[schema] = true
	defer delete(seen, schema)

	// Depth limit
	if depth <= 0 {
		return &resolvedSchema{
			Name:        name,
			Type:        schemaType(schema),
			Description: schema.Description,
			Truncated:   true,
		}
	}

	rs := &resolvedSchema{
		Name:        name,
		Description: schema.Description,
		Format:      schema.Format,
		Pattern:     schema.Pattern,
		MinLength:   schema.MinLength,
		MaxLength:   schema.MaxLength,
		Minimum:     schema.Minimum,
		Maximum:     schema.Maximum,
	}

	// Collect enum values
	for _, e := range schema.Enum {
		if e != nil {
			rs.Enum = append(rs.Enum, e.Value)
		}
	}

	// Handle allOf: merge properties from all members
	if len(schema.AllOf) > 0 {
		rs.Type = "object"
		propMap := make(map[string]resolvedProperty) // name → property (later wins)
		var requiredSet []string

		for _, memberProxy := range schema.AllOf {
			if memberProxy == nil {
				continue
			}
			sourceName := ""
			if memberProxy.IsReference() {
				sourceName = refBaseName(memberProxy.GetReference())
			}
			if sourceName != "" {
				rs.AllOfSources = append(rs.AllOfSources, sourceName)
			}

			member := memberProxy.Schema()
			if member == nil {
				continue
			}

			// Recursively resolve member
			resolved := resolveSchema(member, sourceName, depth, seen)
			for _, prop := range resolved.Properties {
				propMap[prop.Name] = prop
			}
			requiredSet = append(requiredSet, resolved.Required...)
		}

		// Merge direct properties on top of allOf
		if schema.Properties != nil {
			for propName, propProxy := range schema.Properties.FromOldest() {
				propSchema := propProxy.Schema()
				propResolved := resolveSchema(propSchema, propName, depth-1, seen)
				propMap[propName] = resolvedProperty{
					Name:        propName,
					Description: propResolved.Description,
					Schema:      propResolved,
				}
			}
		}
		requiredSet = append(requiredSet, schema.Required...)

		// Convert to sorted slice
		rs.Properties = sortedProperties(propMap)
		rs.Required = uniqueSorted(requiredSet)

		// Mark required properties
		reqSet := make(map[string]bool)
		for _, r := range rs.Required {
			reqSet[r] = true
		}
		for i := range rs.Properties {
			rs.Properties[i].Required = reqSet[rs.Properties[i].Name]
		}

		return rs
	}

	// Regular (non-allOf) schema
	rs.Type = schemaType(schema)

	// Object properties
	if schema.Properties != nil {
		propMap := make(map[string]resolvedProperty)
		for propName, propProxy := range schema.Properties.FromOldest() {
			propSchema := propProxy.Schema()
			propResolved := resolveSchema(propSchema, propName, depth-1, seen)
			propMap[propName] = resolvedProperty{
				Name:        propName,
				Description: propResolved.Description,
				Schema:      propResolved,
			}
		}
		rs.Properties = sortedProperties(propMap)
		rs.Required = schema.Required

		reqSet := make(map[string]bool)
		for _, r := range rs.Required {
			reqSet[r] = true
		}
		for i := range rs.Properties {
			rs.Properties[i].Required = reqSet[rs.Properties[i].Name]
		}
	}

	// Array items
	if rs.Type == "array" && schema.Items != nil && schema.Items.IsA() {
		itemSchema := schema.Items.A.Schema()
		rs.Items = resolveSchema(itemSchema, "items", depth-1, seen)
	}

	return rs
}

// resolveNodeOperation is a shared helper that resolves a node name to its OAS operation.
// Returns the node, method, path, operation, document, and any error message.
func (s *Server) resolveNodeOperation(nodeName string) (*v3high.Operation, string, string, *v3high.Document, string) {
	node := s.ctx.Graph.Nodes[nodeName]
	if node == nil {
		return nil, "", "", nil, fmt.Sprintf("Unknown node %q.", nodeName)
	}

	if node.OAS == nil {
		return nil, "", "", nil, fmt.Sprintf("Node %q has no OAS reference. Use inspect_template for adapter details.", nodeName)
	}

	if len(s.ctx.OASSpecs) == 0 {
		return nil, "", "", nil, "No OAS specs loaded. Configure an oas field in your graph."
	}

	specRef := oas.ResolveNodeSpec(node, s.ctx.Graph.OAS)
	if specRef == "" {
		return nil, "", "", nil, fmt.Sprintf("Node %q has no resolvable spec path.", nodeName)
	}

	// Resolve relative to graph dir
	specPath := specRef
	if !filepath.IsAbs(specRef) {
		specPath = filepath.Join(s.ctx.GraphDir, specRef)
	}

	doc, ok := s.ctx.OASSpecs[specPath]
	if !ok {
		return nil, "", "", nil, fmt.Sprintf("OAS spec %q not loaded.", specRef)
	}

	method, path, op, err := oas.FindOperation(doc, node.OAS.OperationID)
	if err != nil {
		return nil, "", "", nil, fmt.Sprintf("Operation %q not found in spec: %v", node.OAS.OperationID, err)
	}

	return op, method, path, doc, ""
}

// findSchemaByName searches all loaded specs for a component schema by name.
// Returns the schema, its name (possibly case-corrected), and the spec path.
func (s *Server) findSchemaByName(name string) (*base.Schema, string, string) {
	// Exact match first
	for specPath, doc := range s.ctx.OASSpecs {
		if doc.Components == nil || doc.Components.Schemas == nil {
			continue
		}
		proxy := doc.Components.Schemas.GetOrZero(name)
		if proxy != nil {
			return proxy.Schema(), name, specPath
		}
	}

	// Case-insensitive fallback
	lower := strings.ToLower(name)
	for specPath, doc := range s.ctx.OASSpecs {
		if doc.Components == nil || doc.Components.Schemas == nil {
			continue
		}
		for schemaName, proxy := range doc.Components.Schemas.FromOldest() {
			if strings.ToLower(schemaName) == lower {
				return proxy.Schema(), schemaName, specPath
			}
		}
	}

	return nil, "", ""
}

// searchSchemaNames returns all component schema names matching a regex pattern,
// grouped by spec path.
func (s *Server) searchSchemaNames(pattern string) (map[string][]string, error) {
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}

	results := make(map[string][]string)
	for specPath, doc := range s.ctx.OASSpecs {
		if doc.Components == nil || doc.Components.Schemas == nil {
			continue
		}
		for schemaName := range doc.Components.Schemas.KeysFromOldest() {
			if re.MatchString(schemaName) {
				results[specPath] = append(results[specPath], schemaName)
			}
		}
	}

	// Sort each group
	for k := range results {
		sort.Strings(results[k])
	}

	return results, nil
}

// suggestSchemaNames returns up to 5 similar schema names to help with not-found cases.
func (s *Server) suggestSchemaNames(name string) []string {
	lower := strings.ToLower(name)
	var all []string
	for _, doc := range s.ctx.OASSpecs {
		if doc.Components == nil || doc.Components.Schemas == nil {
			continue
		}
		for schemaName := range doc.Components.Schemas.KeysFromOldest() {
			all = append(all, schemaName)
		}
	}

	var matches []string
	for _, sn := range all {
		if strings.Contains(strings.ToLower(sn), lower) {
			matches = append(matches, sn)
		}
	}
	sort.Strings(matches)
	if len(matches) > 5 {
		matches = matches[:5]
	}
	return matches
}

// validatePayload validates a JSON payload against a resolved schema.
func validatePayload(payload any, schema *base.Schema, path string, depth int) []validationError {
	if schema == nil || depth <= 0 {
		return nil
	}

	var errs []validationError

	// Handle allOf: merge and validate
	if len(schema.AllOf) > 0 {
		for _, memberProxy := range schema.AllOf {
			if memberProxy == nil {
				continue
			}
			member := memberProxy.Schema()
			if member != nil {
				errs = append(errs, validatePayload(payload, member, path, depth)...)
			}
		}
		// Also validate direct properties
		if schema.Properties != nil {
			errs = append(errs, validateObjectPayload(payload, schema, path, depth)...)
		}
		return errs
	}

	typ := schemaType(schema)
	switch typ {
	case "object":
		errs = append(errs, validateObjectPayload(payload, schema, path, depth)...)
	case "array":
		arr, ok := payload.([]any)
		if !ok {
			errs = append(errs, validationError{Path: path, Message: "expected array"})
			break
		}
		if schema.Items != nil && schema.Items.IsA() {
			itemSchema := schema.Items.A.Schema()
			for i, item := range arr {
				itemPath := fmt.Sprintf("%s[%d]", path, i)
				errs = append(errs, validatePayload(item, itemSchema, itemPath, depth-1)...)
			}
		}
	case "string":
		errs = append(errs, validateStringPayload(payload, schema, path)...)
	case "integer":
		errs = append(errs, validateNumericPayload(payload, schema, path, true)...)
	case "number":
		errs = append(errs, validateNumericPayload(payload, schema, path, false)...)
	}

	// Enum check (for any type)
	if len(schema.Enum) > 0 {
		errs = append(errs, validateEnum(payload, schema, path)...)
	}

	return errs
}

func validateObjectPayload(payload any, schema *base.Schema, path string, depth int) []validationError {
	obj, ok := payload.(map[string]any)
	if !ok {
		return []validationError{{Path: path, Message: "expected object"}}
	}

	var errs []validationError

	// Required checks
	for _, req := range schema.Required {
		if _, exists := obj[req]; !exists {
			errs = append(errs, validationError{
				Path:    joinPath(path, req),
				Message: "required field missing",
			})
		}
	}

	// Validate each property that exists in both payload and schema
	if schema.Properties != nil {
		for propName, propProxy := range schema.Properties.FromOldest() {
			val, exists := obj[propName]
			if !exists {
				continue
			}
			propSchema := propProxy.Schema()
			if propSchema != nil {
				errs = append(errs, validatePayload(val, propSchema, joinPath(path, propName), depth-1)...)
			}
		}
	}

	return errs
}

func validateStringPayload(payload any, schema *base.Schema, path string) []validationError {
	str, ok := payload.(string)
	if !ok {
		return []validationError{{Path: path, Message: "expected string"}}
	}

	var errs []validationError
	if schema.MinLength != nil && int64(len(str)) < *schema.MinLength {
		errs = append(errs, validationError{
			Path:    path,
			Message: fmt.Sprintf("string length %d < minimum %d", len(str), *schema.MinLength),
		})
	}
	if schema.MaxLength != nil && int64(len(str)) > *schema.MaxLength {
		errs = append(errs, validationError{
			Path:    path,
			Message: fmt.Sprintf("string length %d > maximum %d", len(str), *schema.MaxLength),
		})
	}
	if schema.Pattern != "" {
		re, err := regexp.Compile(schema.Pattern)
		if err == nil && !re.MatchString(str) {
			errs = append(errs, validationError{
				Path:    path,
				Message: fmt.Sprintf("does not match pattern %q", schema.Pattern),
			})
		}
	}
	return errs
}

func validateNumericPayload(payload any, schema *base.Schema, path string, integerOnly bool) []validationError {
	var val float64
	switch v := payload.(type) {
	case float64:
		val = v
	case json.Number:
		var err error
		val, err = v.Float64()
		if err != nil {
			return []validationError{{Path: path, Message: "expected number"}}
		}
	default:
		typeName := "number"
		if integerOnly {
			typeName = "integer"
		}
		return []validationError{{Path: path, Message: fmt.Sprintf("expected %s", typeName)}}
	}

	var errs []validationError
	if schema.Minimum != nil && val < *schema.Minimum {
		errs = append(errs, validationError{
			Path:    path,
			Message: fmt.Sprintf("value %v < minimum %v", val, *schema.Minimum),
		})
	}
	if schema.Maximum != nil && val > *schema.Maximum {
		errs = append(errs, validationError{
			Path:    path,
			Message: fmt.Sprintf("value %v > maximum %v", val, *schema.Maximum),
		})
	}
	return errs
}

func validateEnum(payload any, schema *base.Schema, path string) []validationError {
	valStr := fmt.Sprintf("%v", payload)
	for _, e := range schema.Enum {
		if e != nil && e.Value == valStr {
			return nil
		}
	}
	var allowed []string
	for _, e := range schema.Enum {
		if e != nil {
			allowed = append(allowed, e.Value)
		}
	}
	return []validationError{{
		Path:    path,
		Message: fmt.Sprintf("value %q not in enum [%s]", valStr, strings.Join(allowed, ", ")),
	}}
}

// buildExample generates a JSON-serializable example from an OAS schema.
// Scenario: "minimal" (required only), "typical" (required + common), "full" (all fields).
func buildExample(schema *base.Schema, scenario string, depth int, seen map[*base.Schema]bool) any {
	if schema == nil || depth <= 0 {
		return nil
	}

	if seen[schema] {
		return nil // cycle
	}
	seen[schema] = true
	defer delete(seen, schema)

	// Handle allOf: merge into single object
	if len(schema.AllOf) > 0 {
		result := make(map[string]any)
		var allRequired []string
		var allProps []struct {
			name   string
			proxy  *base.SchemaProxy
			source *base.Schema
		}

		for _, memberProxy := range schema.AllOf {
			if memberProxy == nil {
				continue
			}
			member := memberProxy.Schema()
			if member == nil {
				continue
			}
			allRequired = append(allRequired, member.Required...)
			if member.Properties != nil {
				for propName, propProxy := range member.Properties.FromOldest() {
					allProps = append(allProps, struct {
						name   string
						proxy  *base.SchemaProxy
						source *base.Schema
					}{propName, propProxy, member})
				}
			}
		}
		// Direct properties
		allRequired = append(allRequired, schema.Required...)
		if schema.Properties != nil {
			for propName, propProxy := range schema.Properties.FromOldest() {
				allProps = append(allProps, struct {
					name   string
					proxy  *base.SchemaProxy
					source *base.Schema
				}{propName, propProxy, schema})
			}
		}

		reqSet := make(map[string]bool)
		for _, r := range allRequired {
			reqSet[r] = true
		}

		for _, p := range allProps {
			if scenario == "minimal" && !reqSet[p.name] {
				continue
			}
			propSchema := p.proxy.Schema()
			result[p.name] = buildExample(propSchema, scenario, depth-1, seen)
		}
		return result
	}

	typ := schemaType(schema)

	switch typ {
	case "object":
		result := make(map[string]any)
		reqSet := make(map[string]bool)
		for _, r := range schema.Required {
			reqSet[r] = true
		}
		if schema.Properties != nil {
			for propName, propProxy := range schema.Properties.FromOldest() {
				if scenario == "minimal" && !reqSet[propName] {
					continue
				}
				propSchema := propProxy.Schema()
				result[propName] = buildExample(propSchema, scenario, depth-1, seen)
			}
		}
		return result

	case "array":
		if schema.Items != nil && schema.Items.IsA() {
			itemSchema := schema.Items.A.Schema()
			item := buildExample(itemSchema, scenario, depth-1, seen)
			return []any{item}
		}
		return []any{}

	case "string":
		return exampleString(schema)

	case "integer":
		return exampleInteger(schema)

	case "number":
		return exampleNumber(schema)

	case "boolean":
		return true

	default:
		return "example"
	}
}

func exampleString(schema *base.Schema) string {
	// Enum → first value
	if len(schema.Enum) > 0 && schema.Enum[0] != nil {
		return schema.Enum[0].Value
	}

	// Format-based
	switch schema.Format {
	case "date-time":
		return "2026-03-15T10:30:00Z"
	case "date":
		return "2026-03-15"
	case "uuid":
		return "550e8400-e29b-41d4-a716-446655440000"
	case "email":
		return "user@example.com"
	case "uri":
		return "https://example.com"
	}

	return "string"
}

func exampleInteger(schema *base.Schema) int {
	if schema.Minimum != nil {
		return int(*schema.Minimum)
	}
	return 1
}

func exampleNumber(schema *base.Schema) float64 {
	if schema.Minimum != nil {
		return *schema.Minimum
	}
	return 1.0
}

// --- Formatting helpers ---

// formatResolvedSchema renders a resolvedSchema as Markdown.
func formatResolvedSchema(rs *resolvedSchema) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", rs.Name)

	if rs.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", rs.Description)
	}

	if len(rs.AllOfSources) > 0 {
		fmt.Fprintf(&b, "**Inherits from:** %s\n\n", strings.Join(rs.AllOfSources, ", "))
	}

	fmt.Fprintf(&b, "**Type:** %s", rs.Type)
	if rs.Format != "" {
		fmt.Fprintf(&b, " (format: %s)", rs.Format)
	}
	b.WriteString("\n")

	if len(rs.Enum) > 0 {
		fmt.Fprintf(&b, "**Enum:** %s\n", strings.Join(rs.Enum, ", "))
	}

	if rs.Truncated {
		b.WriteString("\n*Schema truncated (depth limit or cycle detected)*\n")
		return b.String()
	}

	if len(rs.Properties) > 0 {
		b.WriteString("\n## Properties\n\n")
		b.WriteString("| Name | Type | Required | Description |\n")
		b.WriteString("|------|------|----------|-------------|\n")
		for _, prop := range rs.Properties {
			req := ""
			if prop.Required {
				req = "yes"
			}
			desc := prop.Description
			if prop.Schema != nil {
				desc = formatPropertyConstraints(prop.Schema, desc)
			}
			typeName := ""
			if prop.Schema != nil {
				typeName = formatPropertyType(prop.Schema)
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", prop.Name, typeName, req, desc)
		}
	}

	if rs.Items != nil {
		b.WriteString("\n## Array Items\n\n")
		b.WriteString(formatResolvedSchema(rs.Items))
	}

	return b.String()
}

func formatPropertyType(rs *resolvedSchema) string {
	t := rs.Type
	if rs.Format != "" {
		t += " (" + rs.Format + ")"
	}
	if len(rs.Enum) > 0 {
		t += " enum"
	}
	if rs.Type == "array" && rs.Items != nil {
		t = rs.Items.Type + "[]"
	}
	if rs.Truncated && rs.Type == "object" {
		t += " (...)"
	}
	return t
}

func formatPropertyConstraints(rs *resolvedSchema, desc string) string {
	var constraints []string
	if rs.MinLength != nil {
		constraints = append(constraints, fmt.Sprintf("minLength: %d", *rs.MinLength))
	}
	if rs.MaxLength != nil {
		constraints = append(constraints, fmt.Sprintf("maxLength: %d", *rs.MaxLength))
	}
	if rs.Minimum != nil {
		constraints = append(constraints, fmt.Sprintf("min: %v", *rs.Minimum))
	}
	if rs.Maximum != nil {
		constraints = append(constraints, fmt.Sprintf("max: %v", *rs.Maximum))
	}
	if rs.Pattern != "" {
		constraints = append(constraints, fmt.Sprintf("pattern: %s", rs.Pattern))
	}

	if len(constraints) == 0 {
		return desc
	}
	if desc != "" {
		return desc + " [" + strings.Join(constraints, ", ") + "]"
	}
	return strings.Join(constraints, ", ")
}

// formatOperationDetail renders an OAS operation as Markdown.
func formatOperationDetail(method, path string, op *v3high.Operation, doc *v3high.Document) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s %s\n\n", method, path)
	if op.Summary != "" {
		fmt.Fprintf(&b, "%s\n\n", op.Summary)
	}
	if op.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", op.Description)
	}

	// Parameters
	if len(op.Parameters) > 0 {
		b.WriteString("## Parameters\n\n")
		b.WriteString("| Name | In | Type | Required | Description |\n")
		b.WriteString("|------|-----|------|----------|-------------|\n")
		for _, param := range op.Parameters {
			if param == nil {
				continue
			}
			req := "no"
			if param.Required != nil && *param.Required {
				req = "yes"
			}
			if param.In == "path" {
				req = "yes"
			}
			typeName := "string"
			if param.Schema != nil {
				s := param.Schema.Schema()
				if s != nil && len(s.Type) > 0 {
					typeName = s.Type[0]
					if s.Format != "" {
						typeName += " (" + s.Format + ")"
					}
				}
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
				param.Name, param.In, typeName, req, param.Description)
		}
		b.WriteString("\n")
	}

	// Request body
	if op.RequestBody != nil && op.RequestBody.Content != nil {
		jsonContent := op.RequestBody.Content.GetOrZero("application/json")
		if jsonContent != nil && jsonContent.Schema != nil {
			b.WriteString("## Request Body\n\n")
			schema := jsonContent.Schema.Schema()
			if schema != nil {
				schemaName := ""
				if jsonContent.Schema.IsReference() {
					schemaName = refBaseName(jsonContent.Schema.GetReference())
				}
				rs := resolveSchema(schema, schemaName, 3, make(map[*base.Schema]bool))
				b.WriteString(formatResolvedSchemaInline(rs))
			}
		}
	}

	// Responses
	if op.Responses != nil && op.Responses.Codes != nil {
		b.WriteString("## Responses\n\n")
		for code, resp := range op.Responses.Codes.FromOldest() {
			fmt.Fprintf(&b, "### %s", code)
			if resp.Description != "" {
				fmt.Fprintf(&b, " — %s", resp.Description)
			}
			b.WriteString("\n\n")

			if resp.Content != nil {
				jsonContent := resp.Content.GetOrZero("application/json")
				if jsonContent != nil && jsonContent.Schema != nil {
					schema := jsonContent.Schema.Schema()
					if schema != nil {
						schemaName := ""
						if jsonContent.Schema.IsReference() {
							schemaName = refBaseName(jsonContent.Schema.GetReference())
						}
						rs := resolveSchema(schema, schemaName, 2, make(map[*base.Schema]bool))
						b.WriteString(formatResolvedSchemaInline(rs))
					}
				}
			}
		}
	}

	return b.String()
}

// formatResolvedSchemaInline renders a resolvedSchema as inline Markdown (no top-level heading).
func formatResolvedSchemaInline(rs *resolvedSchema) string {
	var b strings.Builder

	if rs.Name != "" {
		fmt.Fprintf(&b, "**Schema:** %s", rs.Name)
	} else {
		fmt.Fprintf(&b, "**Type:** %s", rs.Type)
	}
	if rs.Format != "" {
		fmt.Fprintf(&b, " (format: %s)", rs.Format)
	}
	b.WriteString("\n")

	if len(rs.AllOfSources) > 0 {
		fmt.Fprintf(&b, "**Inherits from:** %s\n", strings.Join(rs.AllOfSources, ", "))
	}

	if rs.Truncated {
		b.WriteString("*Truncated*\n\n")
		return b.String()
	}

	if len(rs.Properties) > 0 {
		b.WriteString("\n| Name | Type | Required | Description |\n")
		b.WriteString("|------|------|----------|-------------|\n")
		for _, prop := range rs.Properties {
			req := ""
			if prop.Required {
				req = "yes"
			}
			desc := prop.Description
			if prop.Schema != nil {
				desc = formatPropertyConstraints(prop.Schema, desc)
			}
			typeName := ""
			if prop.Schema != nil {
				typeName = formatPropertyType(prop.Schema)
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", prop.Name, typeName, req, desc)
		}
		b.WriteString("\n")
	}

	if rs.Type == "array" && rs.Items != nil {
		b.WriteString("**Array of:** ")
		if rs.Items.Name != "" {
			fmt.Fprintf(&b, "%s\n", rs.Items.Name)
		} else {
			fmt.Fprintf(&b, "%s\n", rs.Items.Type)
		}
	}

	return b.String()
}

// formatValidationErrors renders validation errors as Markdown.
func formatValidationErrors(errs []validationError) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Validation failed** (%d error(s)):\n\n", len(errs))
	for _, e := range errs {
		fmt.Fprintf(&b, "- `%s`: %s\n", e.Path, e.Message)
	}
	return b.String()
}

// --- Utility helpers ---

// schemaType extracts the primary type from a schema.
func schemaType(schema *base.Schema) string {
	if schema == nil {
		return ""
	}
	if len(schema.Type) > 0 {
		return schema.Type[0]
	}
	// If no explicit type but has properties, treat as object
	if schema.Properties != nil && schema.Properties.Len() > 0 {
		return "object"
	}
	return ""
}

// refBaseName extracts "Pet" from "#/components/schemas/Pet".
func refBaseName(ref string) string {
	if idx := strings.LastIndex(ref, "/"); idx >= 0 {
		return ref[idx+1:]
	}
	return ref
}

// sortedProperties converts a property map to a sorted slice.
func sortedProperties(m map[string]resolvedProperty) []resolvedProperty {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	result := make([]resolvedProperty, len(names))
	for i, name := range names {
		result[i] = m[name]
	}
	return result
}

// uniqueSorted returns sorted unique strings.
func uniqueSorted(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	sort.Strings(result)
	return result
}

// joinPath builds a dotted JSON path.
func joinPath(base, field string) string {
	if base == "" || base == "$" {
		return "$." + field
	}
	return base + "." + field
}
