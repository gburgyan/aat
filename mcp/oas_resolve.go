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
		toolName := "inspect_template"
		if s.persona == PersonaIntegration {
			toolName = "inspect_request_template"
		}
		adapterInfo := ""
		if node.Adapter != "" {
			adapterInfo = fmt.Sprintf(" (adapter: %s)", node.Adapter)
		}
		return nil, "", "", nil, fmt.Sprintf("Node %q has no OAS reference%s. Use %s with that adapter name for HTTP details.", nodeName, adapterInfo, toolName)
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

// schemaSearchFilters holds the optional filter parameters for schema search.
type schemaSearchFilters struct {
	Pattern          string // regex pattern for schema name (optional if other filters set)
	HasProperty      string // schema must have this property
	Extends          string // schema must extend this base via allOf $ref
	HasDiscriminator *bool  // schema must have/not have a discriminator
}

// searchSchemaNames returns all component schema names matching a regex pattern,
// grouped by spec path.
func (s *Server) searchSchemaNames(pattern string) (map[string][]string, error) {
	return s.searchSchemasFiltered(schemaSearchFilters{Pattern: pattern})
}

// searchSchemasFiltered returns schema names matching the given filters, grouped by spec path.
func (s *Server) searchSchemasFiltered(filters schemaSearchFilters) (map[string][]string, error) {
	var re *regexp.Regexp
	if filters.Pattern != "" {
		var err error
		re, err = regexp.Compile("(?i)" + filters.Pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
	}

	results := make(map[string][]string)
	for specPath, doc := range s.ctx.OASSpecs {
		if doc.Components == nil || doc.Components.Schemas == nil {
			continue
		}
		for schemaName, schemaProxy := range doc.Components.Schemas.FromOldest() {
			// Pattern filter
			if re != nil && !re.MatchString(schemaName) {
				continue
			}

			schema := schemaProxy.Schema()

			// HasProperty filter
			if filters.HasProperty != "" && !schemaHasProperty(schema, filters.HasProperty) {
				continue
			}

			// Extends filter
			if filters.Extends != "" && !schemaExtends(schema, filters.Extends) {
				continue
			}

			// HasDiscriminator filter
			if filters.HasDiscriminator != nil && schemaHasDiscriminator(schema) != *filters.HasDiscriminator {
				continue
			}

			results[specPath] = append(results[specPath], schemaName)
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

// oasOperation describes a single operation discovered from OAS specs.
type oasOperation struct {
	OperationID string
	Method      string
	Path        string
	Summary     string
	Tags        []string
	Deprecated  bool
	GraphNode   string // non-empty if mapped to a graph node
}

// listAllOperations scans all loaded specs and returns every operation,
// with optional filters for tag, keyword, and method.
func (s *Server) listAllOperations(tag, keyword, method string) []oasOperation {
	mapped := s.graphMappedOperations()

	var ops []oasOperation
	for _, doc := range s.ctx.OASSpecs {
		if doc.Paths == nil || doc.Paths.PathItems == nil {
			continue
		}
		for pathStr, pathItem := range doc.Paths.PathItems.FromOldest() {
			for _, pair := range []struct {
				method string
				op     *v3high.Operation
			}{
				{"GET", pathItem.Get},
				{"POST", pathItem.Post},
				{"PUT", pathItem.Put},
				{"DELETE", pathItem.Delete},
				{"PATCH", pathItem.Patch},
			} {
				if pair.op == nil {
					continue
				}

				// Method filter
				if method != "" && !strings.EqualFold(pair.method, method) {
					continue
				}

				// Tag filter
				if tag != "" {
					found := false
					for _, t := range pair.op.Tags {
						if strings.EqualFold(t, tag) {
							found = true
							break
						}
					}
					if !found {
						continue
					}
				}

				// Keyword filter (searches operationId, summary, description)
				if keyword != "" {
					kw := strings.ToLower(keyword)
					match := false
					if pair.op.OperationId != "" && strings.Contains(strings.ToLower(pair.op.OperationId), kw) {
						match = true
					}
					if !match && pair.op.Summary != "" && strings.Contains(strings.ToLower(pair.op.Summary), kw) {
						match = true
					}
					if !match && pair.op.Description != "" && strings.Contains(strings.ToLower(pair.op.Description), kw) {
						match = true
					}
					if !match {
						continue
					}
				}

				op := oasOperation{
					OperationID: pair.op.OperationId,
					Method:      pair.method,
					Path:        pathStr,
					Summary:     pair.op.Summary,
					Tags:        pair.op.Tags,
					Deprecated:  pair.op.Deprecated != nil && *pair.op.Deprecated,
					GraphNode:   mapped[pair.op.OperationId],
				}
				ops = append(ops, op)
			}
		}
	}

	// Sort by path, then method for stable output
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Path != ops[j].Path {
			return ops[i].Path < ops[j].Path
		}
		return ops[i].Method < ops[j].Method
	})

	return ops
}

// graphMappedOperations returns a map from operationID → graph node name
// for all nodes that have an OAS reference.
func (s *Server) graphMappedOperations() map[string]string {
	result := make(map[string]string)
	for name, node := range s.ctx.Graph.Nodes {
		if node != nil && node.OAS != nil && node.OAS.OperationID != "" {
			result[node.OAS.OperationID] = name
		}
	}
	return result
}

// applyCustomValues recursively merges custom values onto a generated example.
// For maps, custom values are merged key-by-key; for other types, custom values replace.
func applyCustomValues(example any, custom map[string]any) any {
	exMap, exIsMap := example.(map[string]any)
	if !exIsMap {
		// Can't merge into non-map; return example unchanged
		return example
	}

	result := make(map[string]any, len(exMap))
	for k, v := range exMap {
		result[k] = v
	}

	for k, cv := range custom {
		existing, exists := result[k]
		// If both are maps, merge recursively
		if exists {
			if existingMap, ok := existing.(map[string]any); ok {
				if cvMap, ok := cv.(map[string]any); ok {
					result[k] = applyCustomValues(existingMap, cvMap)
					continue
				}
			}
		}
		// Otherwise, custom value wins
		result[k] = cv
	}

	return result
}

// subtypeResult holds the polymorphism analysis for a schema.
type subtypeResult struct {
	SchemaName        string
	DiscriminatorProp string            // discriminator property name, if any
	DiscriminatorMap  map[string]string // value → schema name mapping
	OneOfVariants     []string          // oneOf variant names
	AnyOfVariants     []string          // anyOf variant names
	ExtendingSchemas  []string          // schemas that extend via allOf $ref
}

// findSubtypes analyzes polymorphism for a named schema across all specs.
func (s *Server) findSubtypes(schemaName string) *subtypeResult {
	schema, resolvedName, _ := s.findSchemaByName(schemaName)
	if schema == nil {
		return nil
	}

	result := &subtypeResult{SchemaName: resolvedName}

	// 1. Check discriminator
	if schema.Discriminator != nil && schema.Discriminator.PropertyName != "" {
		result.DiscriminatorProp = schema.Discriminator.PropertyName
		if schema.Discriminator.Mapping != nil {
			result.DiscriminatorMap = make(map[string]string)
			for k, v := range schema.Discriminator.Mapping.FromOldest() {
				result.DiscriminatorMap[k] = refBaseName(v)
			}
		}
	}

	// 2. Check oneOf variants
	for _, proxy := range schema.OneOf {
		if proxy == nil {
			continue
		}
		if proxy.IsReference() {
			result.OneOfVariants = append(result.OneOfVariants, refBaseName(proxy.GetReference()))
		}
	}
	sort.Strings(result.OneOfVariants)

	// 3. Check anyOf variants
	for _, proxy := range schema.AnyOf {
		if proxy == nil {
			continue
		}
		if proxy.IsReference() {
			result.AnyOfVariants = append(result.AnyOfVariants, refBaseName(proxy.GetReference()))
		}
	}
	sort.Strings(result.AnyOfVariants)

	// 4. Reverse scan: find schemas that extend this one via allOf
	for _, doc := range s.ctx.OASSpecs {
		if doc.Components == nil || doc.Components.Schemas == nil {
			continue
		}
		for name, proxy := range doc.Components.Schemas.FromOldest() {
			if name == resolvedName {
				continue
			}
			childSchema := proxy.Schema()
			if childSchema != nil && schemaExtends(childSchema, resolvedName) {
				result.ExtendingSchemas = append(result.ExtendingSchemas, name)
			}
		}
	}
	sort.Strings(result.ExtendingSchemas)

	return result
}

// formatSubtypeResult renders a subtypeResult as Markdown.
func formatSubtypeResult(r *subtypeResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Subtypes of %s\n\n", r.SchemaName)

	hasContent := false

	if r.DiscriminatorProp != "" {
		hasContent = true
		fmt.Fprintf(&b, "## Discriminator\n\n")
		fmt.Fprintf(&b, "**Property:** `%s`\n\n", r.DiscriminatorProp)
		if len(r.DiscriminatorMap) > 0 {
			b.WriteString("| Value | Schema |\n")
			b.WriteString("|-------|--------|\n")
			// Sort keys for stable output
			keys := make([]string, 0, len(r.DiscriminatorMap))
			for k := range r.DiscriminatorMap {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(&b, "| %s | %s |\n", k, r.DiscriminatorMap[k])
			}
			b.WriteString("\n")
		}
	}

	if len(r.OneOfVariants) > 0 {
		hasContent = true
		fmt.Fprintf(&b, "## oneOf Variants\n\n")
		for _, v := range r.OneOfVariants {
			fmt.Fprintf(&b, "- %s\n", v)
		}
		b.WriteString("\n")
	}

	if len(r.AnyOfVariants) > 0 {
		hasContent = true
		fmt.Fprintf(&b, "## anyOf Variants\n\n")
		for _, v := range r.AnyOfVariants {
			fmt.Fprintf(&b, "- %s\n", v)
		}
		b.WriteString("\n")
	}

	if len(r.ExtendingSchemas) > 0 {
		hasContent = true
		fmt.Fprintf(&b, "## Extending Schemas (via allOf)\n\n")
		for _, v := range r.ExtendingSchemas {
			fmt.Fprintf(&b, "- %s\n", v)
		}
		b.WriteString("\n")
	}

	if !hasContent {
		b.WriteString("No polymorphic subtypes found. This schema does not use discriminator, oneOf, anyOf, or allOf inheritance.\n")
	}

	return b.String()
}

// --- Schema filter helpers ---

// schemaHasProperty checks if a schema (including allOf members) has a property with the given name.
func schemaHasProperty(schema *base.Schema, propName string) bool {
	if schema == nil {
		return false
	}
	// Check direct properties
	if schema.Properties != nil {
		if schema.Properties.GetOrZero(propName) != nil {
			return true
		}
	}
	// Check allOf members
	for _, memberProxy := range schema.AllOf {
		if memberProxy == nil {
			continue
		}
		member := memberProxy.Schema()
		if member != nil && schemaHasProperty(member, propName) {
			return true
		}
	}
	return false
}

// schemaExtends checks if a schema extends the given base schema name via allOf $ref.
func schemaExtends(schema *base.Schema, baseName string) bool {
	if schema == nil {
		return false
	}
	for _, memberProxy := range schema.AllOf {
		if memberProxy == nil {
			continue
		}
		if memberProxy.IsReference() && refBaseName(memberProxy.GetReference()) == baseName {
			return true
		}
	}
	return false
}

// schemaHasDiscriminator checks if a schema has a discriminator defined.
func schemaHasDiscriminator(schema *base.Schema) bool {
	if schema == nil {
		return false
	}
	return schema.Discriminator != nil && schema.Discriminator.PropertyName != ""
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
