package oas

import (
	"fmt"
	"slices"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/gburgyan/aat/graph"
)

// GenerateResult holds the output of scaffold generation.
type GenerateResult struct {
	Graph     *graph.Graph
	Templates []*ScaffoldTemplate
	Warnings  []string
}

// ScaffoldTemplate mirrors adapter.Template YAML structure.
// Defined locally to avoid graph/oas → adapter dependency.
type ScaffoldTemplate struct {
	Adapter  string                   `yaml:"adapter"`
	Protocol string                   `yaml:"protocol"`
	Request  ScaffoldTemplateRequest  `yaml:"request"`
	Response ScaffoldTemplateResponse `yaml:"response"`
}

// ScaffoldTemplateRequest defines the HTTP request shape for a scaffold template.
type ScaffoldTemplateRequest struct {
	Method  string            `yaml:"method"`
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Body    string            `yaml:"body,omitempty"`
}

// ScaffoldTemplateResponse defines output extraction for a scaffold template.
type ScaffoldTemplateResponse struct {
	Extract map[string]ScaffoldExtractRule `yaml:"extract,omitempty"`
}

// ScaffoldExtractRule mirrors adapter.ExtractRule for scaffold generation.
type ScaffoldExtractRule struct {
	Path     string            `yaml:"path"`
	Fields   map[string]string `yaml:"fields,omitempty"`
	Optional bool              `yaml:"optional,omitempty"`
}

// MarshalYAML emits a bare string for a required path without fields
// (backward-compatible scalar format), or a mapping otherwise.
func (r ScaffoldExtractRule) MarshalYAML() (interface{}, error) {
	if len(r.Fields) == 0 && !r.Optional {
		return r.Path, nil
	}
	// The raw type has no methods, so YAML renders it as {path: ..., fields: {...}}.
	type rawScaffoldExtractRule ScaffoldExtractRule
	return rawScaffoldExtractRule(r), nil
}

// Generate produces a graph and template stubs from an OAS spec. specRef
// becomes the graph's oas: reference, which AAT resolves from the directory the
// graph file is in.
func Generate(model *v3high.Document, specRef string) (*GenerateResult, error) {
	if model.Paths == nil {
		return nil, fmt.Errorf("OAS spec has no paths")
	}

	result := &GenerateResult{
		Graph: &graph.Graph{
			Version: "1.0.0",
			OAS:     specRef,
			Nodes:   make(map[string]*graph.Node),
		},
	}

	for pathStr, pathItem := range model.Paths.PathItems.FromOldest() {
		type methodOp struct {
			method string
			op     *v3high.Operation
		}
		candidates := []methodOp{
			{"GET", pathItem.Get},
			{"POST", pathItem.Post},
			{"PUT", pathItem.Put},
			{"DELETE", pathItem.Delete},
			{"PATCH", pathItem.Patch},
		}

		for _, c := range candidates {
			if c.op == nil {
				continue
			}
			if c.op.OperationId == "" {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("skipping %s %s: no operationId", c.method, pathStr))
				continue
			}

			node := generateNode(pathItem, c.op)
			tmpl := generateTemplate(c.method, pathStr, pathItem, c.op)

			result.Graph.Nodes[c.op.OperationId] = node
			result.Templates = append(result.Templates, tmpl)
		}
	}

	if len(result.Graph.Nodes) == 0 {
		return nil, fmt.Errorf("no operations with operationId found in spec")
	}

	return result, nil
}

// generateNode builds a graph.Node from one OAS operation. The node's name is
// its key in the graph, so Name stays empty and the graph file has no name:.
func generateNode(pathItem *v3high.PathItem, op *v3high.Operation) *graph.Node {
	node := &graph.Node{
		Description: op.Summary,
		Adapter:     op.OperationId,
		Inputs:      collectNodeInputs(pathItem, op),
		Outputs:     collectNodeOutputs(op),
		OAS: &graph.OASRef{
			OperationID: op.OperationId,
		},
	}
	return node
}

// generateTemplate builds a ScaffoldTemplate from one OAS operation. Optional
// query parameters, headers, and body properties sit inside {{?name}} blocks,
// so a generated template runs with only its required inputs.
func generateTemplate(method, path string, pathItem *v3high.PathItem, op *v3high.Operation) *ScaffoldTemplate {
	params := OperationParameters(pathItem, op)
	tmpl := &ScaffoldTemplate{
		Adapter:  op.OperationId,
		Protocol: "http",
		Request: ScaffoldTemplateRequest{
			Method: method,
		},
		Response: ScaffoldTemplateResponse{},
	}

	// Path with converted params and the query string
	var query []templateField
	for _, param := range params {
		if param.In == "query" {
			query = append(query, templateField{name: param.Name, required: param.Required != nil && *param.Required})
		}
	}
	tmpl.Request.Path = convertPathParams(path) + buildQueryString(query)

	// Headers
	headers := make(map[string]string)
	hasRequestBody := op.RequestBody != nil && op.RequestBody.Content != nil
	if hasRequestBody {
		headers["Content-Type"] = "application/json"
	}
	for _, param := range params {
		if param.In != "header" {
			continue
		}
		if param.Required != nil && *param.Required {
			headers[param.Name] = fmt.Sprintf("{{%s}}", param.Name)
		} else {
			headers[param.Name] = fmt.Sprintf("{{?%s}}{{%s}}{{/%s}}", param.Name, param.Name, param.Name)
		}
	}
	if len(headers) > 0 {
		tmpl.Request.Headers = headers
	}

	// Body
	if hasRequestBody {
		tmpl.Request.Body = buildBodyTemplate(op)
	}

	// Extract map
	outputs := collectNodeOutputs(op)
	tmpl.Response.Extract = buildExtractMap(outputs, isArrayResponse(op))

	return tmpl
}

// templateField is a query parameter or body property a template sends.
type templateField struct {
	name     string
	required bool
	raw      bool // body only: the value is inserted as a JSON literal, not a quoted string
}

// buildQueryString renders "?a={{a}}&b={{b}}" for required parameters, with
// each optional parameter in a conditional block whose separator is correct
// whichever optional values are present.
func buildQueryString(fields []templateField) string {
	required, optional := splitRequired(fields)
	var b strings.Builder
	for i, f := range required {
		if i == 0 {
			b.WriteString("?")
		} else {
			b.WriteString("&")
		}
		b.WriteString(f.name + "={{" + f.name + "}}")
	}
	writeOptional(&b, optional, len(required) > 0, "?", "&", func(f templateField) string {
		return f.name + "={{" + f.name + "}}"
	})
	return b.String()
}

// splitRequired partitions fields into required and optional, keeping order.
func splitRequired(fields []templateField) (required, optional []templateField) {
	for _, f := range fields {
		if f.required {
			required = append(required, f)
		} else {
			optional = append(optional, f)
		}
	}
	return required, optional
}

// writeOptional writes one conditional block per optional field. After
// required fields each block starts with sep. Otherwise the first present field
// gets lead (for example "?") and later ones sep, which takes a gate on the
// earlier fields: {{?a|b}}&{{/a|b}} is emitted only if a or b was present.
func writeOptional(b *strings.Builder, optional []templateField, afterRequired bool, lead, sep string, render func(templateField) string) {
	if len(optional) == 0 {
		return
	}
	if afterRequired {
		for _, f := range optional {
			b.WriteString("{{?" + f.name + "}}" + sep + render(f) + "{{/" + f.name + "}}")
		}
		return
	}
	if len(optional) == 1 {
		f := optional[0]
		b.WriteString("{{?" + f.name + "}}" + lead + render(f) + "{{/" + f.name + "}}")
		return
	}
	names := make([]string, len(optional))
	for i, f := range optional {
		names[i] = f.name
	}
	if lead != "" {
		all := strings.Join(names, "|")
		b.WriteString("{{?" + all + "}}" + lead + "{{/" + all + "}}")
	}
	for i, f := range optional {
		b.WriteString("{{?" + f.name + "}}")
		if i > 0 {
			prev := strings.Join(names[:i], "|")
			b.WriteString("{{?" + prev + "}}" + sep + "{{/" + prev + "}}")
		}
		b.WriteString(render(f) + "{{/" + f.name + "}}")
	}
}

// collectNodeInputs gathers inputs from OAS parameters (path-item and
// operation level) and the request body.
func collectNodeInputs(pathItem *v3high.PathItem, op *v3high.Operation) []graph.Input {
	var inputs []graph.Input

	// Parameters (query, header, path, cookie)
	for _, param := range OperationParameters(pathItem, op) {
		inp := graph.Input{
			Name: param.Name,
		}
		if param.Schema != nil {
			schema := param.Schema.Schema()
			inp.Type = mapSchemaType(schema)
			inp.Constraints = extractConstraints(schema)
		} else {
			inp.Type = "string"
		}
		// Path params are always required; for others, check the Required field
		if param.In == "path" {
			inp.Optional = false
		} else if param.Required == nil || !*param.Required {
			inp.Optional = true
		}
		inputs = append(inputs, inp)
	}

	// Request body properties
	if op.RequestBody != nil && op.RequestBody.Content != nil {
		jsonContent := op.RequestBody.Content.GetOrZero("application/json")
		if jsonContent != nil && jsonContent.Schema != nil {
			schema := jsonContent.Schema.Schema()
			if schema != nil {
				requiredSet := make(map[string]bool)
				for _, r := range schema.Required {
					requiredSet[r] = true
				}

				for _, prop := range resolveSchemaProperties(schema) {
					propName, propProxy := prop.name, prop.proxy
					inp := graph.Input{
						Name: propName,
					}
					if propProxy != nil {
						propSchema := propProxy.Schema()
						inp.Type = mapSchemaType(propSchema)
						inp.Constraints = extractConstraints(propSchema)
					} else {
						inp.Type = "string"
					}
					inp.Optional = !requiredSet[propName]
					inputs = append(inputs, inp)
				}
			}
		}
	}

	return inputs
}

// collectNodeOutputs extracts outputs from the first 2xx response.
func collectNodeOutputs(op *v3high.Operation) []graph.Output {
	if op.Responses == nil || op.Responses.Codes == nil {
		return nil
	}

	for code := range op.Responses.Codes.KeysFromOldest() {
		if !strings.HasPrefix(code, "2") {
			continue
		}
		resp := op.Responses.Codes.GetOrZero(code)
		if resp == nil || resp.Content == nil {
			continue
		}
		jsonContent := resp.Content.GetOrZero("application/json")
		if jsonContent == nil || jsonContent.Schema == nil {
			continue
		}

		schema := jsonContent.Schema.Schema()
		if schema == nil {
			continue
		}

		// Array response
		if len(schema.Type) > 0 && schema.Type[0] == "array" {
			return collectArrayOutput(op.OperationId, schema)
		}

		// Object response (direct or via $ref)
		return collectObjectOutputs(schema)
	}

	return nil
}

// collectArrayOutput builds an output for an array response with elementFields.
func collectArrayOutput(operationId string, schema *base.Schema) []graph.Output {
	outputName := deriveArrayOutputName(operationId)
	out := graph.Output{
		Name: outputName,
		Type: "object[]",
	}

	if schema.Items != nil && schema.Items.IsA() {
		itemSchema := schema.Items.A.Schema()
		if itemSchema != nil {
			for _, prop := range resolveSchemaProperties(itemSchema) {
				propName, propProxy := prop.name, prop.proxy
				field := graph.Field{
					Name: propName,
				}
				if propProxy != nil {
					field.Type = mapSchemaType(propProxy.Schema())
				} else {
					field.Type = "string"
				}
				out.ElementFields = append(out.ElementFields, field)
			}
		}
	}

	return []graph.Output{out}
}

// collectObjectOutputs builds outputs from an object schema's properties. A
// property the schema does not list as required becomes an optional output, so
// a response that omits it does not fail extraction.
func collectObjectOutputs(schema *base.Schema) []graph.Output {
	var outputs []graph.Output

	for _, prop := range resolveSchemaProperties(schema) {
		propName, propProxy := prop.name, prop.proxy
		out := graph.Output{
			Name:     propName,
			Optional: !slices.Contains(schema.Required, propName),
		}
		if propProxy != nil {
			out.Type = mapSchemaType(propProxy.Schema())
		} else {
			out.Type = "string"
		}
		outputs = append(outputs, out)
	}

	return outputs
}

// schemaProperty is one named property of an object schema.
type schemaProperty struct {
	name  string
	proxy *base.SchemaProxy
}

// resolveSchemaProperties returns an object schema's properties in spec order,
// so regenerating a scaffold produces the same file.
func resolveSchemaProperties(schema *base.Schema) []schemaProperty {
	if schema.Properties == nil || schema.Properties.Len() == 0 {
		return nil
	}
	props := make([]schemaProperty, 0, schema.Properties.Len())
	for name, proxy := range schema.Properties.FromOldest() {
		props = append(props, schemaProperty{name: name, proxy: proxy})
	}
	return props
}

// mapSchemaType converts an OAS JSON Schema type+format to an AAT graph type.
func mapSchemaType(schema *base.Schema) string {
	if schema == nil {
		return "string"
	}

	typeName := ""
	if len(schema.Type) > 0 {
		typeName = schema.Type[0]
	}

	switch typeName {
	case "string":
		switch schema.Format {
		case "date":
			return "date"
		case "date-time":
			return "datetime"
		default:
			return "string"
		}
	case "integer":
		return "integer"
	case "number":
		return "float"
	case "boolean":
		return "boolean"
	case "array":
		elemType := "string"
		if schema.Items != nil && schema.Items.IsA() {
			elemType = mapSchemaType(schema.Items.A.Schema())
		}
		return elemType + "[]"
	case "object":
		return "object"
	case "":
		// An untyped schema that declares properties, or composes them with
		// allOf, is an object.
		if (schema.Properties != nil && schema.Properties.Len() > 0) || len(schema.AllOf) > 0 {
			return "object"
		}
		return "string"
	default:
		return "string"
	}
}

// extractConstraints builds a Constraint from OAS schema validation properties.
// Returns nil if the schema has no relevant constraints.
func extractConstraints(schema *base.Schema) *graph.Constraint {
	if schema == nil {
		return nil
	}

	var c graph.Constraint
	hasAny := false

	if schema.MinLength != nil {
		v := int(*schema.MinLength)
		c.MinLength = &v
		hasAny = true
	}
	if schema.MaxLength != nil {
		v := int(*schema.MaxLength)
		c.MaxLength = &v
		hasAny = true
	}
	if schema.Pattern != "" {
		c.Pattern = schema.Pattern
		hasAny = true
	}
	if schema.Minimum != nil {
		v := *schema.Minimum
		c.Min = &v
		hasAny = true
	}
	if schema.Maximum != nil {
		v := *schema.Maximum
		c.Max = &v
		hasAny = true
	}
	if schema.Description != "" {
		c.Description = schema.Description
		hasAny = true
	}

	if !hasAny {
		return nil
	}
	return &c
}

// convertPathParams replaces OAS path parameters {param} with template placeholders {{param}}.
func convertPathParams(path string) string {
	var result strings.Builder
	i := 0
	for i < len(path) {
		if path[i] == '{' {
			// Find closing brace
			end := strings.IndexByte(path[i:], '}')
			if end == -1 {
				result.WriteString(path[i:])
				break
			}
			paramName := path[i+1 : i+end]
			result.WriteString("{{")
			result.WriteString(paramName)
			result.WriteString("}}")
			i += end + 1
		} else {
			result.WriteByte(path[i])
			i++
		}
	}
	return result.String()
}

// buildBodyTemplate creates a JSON body with a {{placeholder}} for each top-level
// property, in spec order: required properties first, then each optional one
// in a conditional block. Strings are quoted; integers, numbers, booleans, and
// arrays are inserted as JSON literals.
func buildBodyTemplate(op *v3high.Operation) string {
	if op.RequestBody == nil || op.RequestBody.Content == nil {
		return ""
	}

	jsonContent := op.RequestBody.Content.GetOrZero("application/json")
	if jsonContent == nil || jsonContent.Schema == nil {
		return ""
	}

	schema := jsonContent.Schema.Schema()
	if schema == nil || schema.Properties == nil || schema.Properties.Len() == 0 {
		return ""
	}

	requiredSet := make(map[string]bool, len(schema.Required))
	for _, r := range schema.Required {
		requiredSet[r] = true
	}
	var fields []templateField
	for name, proxy := range schema.Properties.FromOldest() {
		fields = append(fields, templateField{name: name, required: requiredSet[name], raw: isJSONLiteralType(proxy)})
	}
	required, optional := splitRequired(fields)

	render := func(f templateField) string {
		if f.raw {
			return fmt.Sprintf("  %q: {{%s}}", f.name, f.name)
		}
		return fmt.Sprintf("  %q: \"{{%s}}\"", f.name, f.name)
	}
	var b strings.Builder
	b.WriteString("{\n")
	for i, f := range required {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString(render(f))
	}
	writeOptional(&b, optional, len(required) > 0, "", ",\n", render)
	b.WriteString("\n}")
	return b.String()
}

// isJSONLiteralType reports whether a body property is inserted unquoted: an
// integer, number, boolean, array, or object, including an untyped schema that
// declares properties or allOf.
func isJSONLiteralType(proxy *base.SchemaProxy) bool {
	if proxy == nil {
		return false
	}
	schema := proxy.Schema()
	if schema == nil {
		return false
	}
	if len(schema.Type) == 0 {
		return (schema.Properties != nil && schema.Properties.Len() > 0) || len(schema.AllOf) > 0
	}
	switch schema.Type[0] {
	case "integer", "number", "boolean", "array", "object":
		return true
	}
	return false
}

// isArrayResponse reports whether the operation's first 2xx JSON response is
// an array at the top level, the shape collectArrayOutput extracts with @this.
func isArrayResponse(op *v3high.Operation) bool {
	if op.Responses == nil || op.Responses.Codes == nil {
		return false
	}
	for code := range op.Responses.Codes.KeysFromOldest() {
		if !strings.HasPrefix(code, "2") {
			continue
		}
		resp := op.Responses.Codes.GetOrZero(code)
		if resp == nil || resp.Content == nil {
			continue
		}
		jsonContent := resp.Content.GetOrZero("application/json")
		if jsonContent == nil || jsonContent.Schema == nil {
			continue
		}
		schema := jsonContent.Schema.Schema()
		if schema == nil {
			continue
		}
		return len(schema.Type) > 0 && schema.Type[0] == "array"
	}
	return false
}

// buildExtractMap creates extract rules from outputs. For an array response
// (rootArray) the single output is the whole body, @this, with a fields section
// mapping name → name (OAS property names match JSON keys directly). Every other
// output, arrays included, is extracted from the property of the same name.
func buildExtractMap(outputs []graph.Output, rootArray bool) map[string]ScaffoldExtractRule {
	if len(outputs) == 0 {
		return nil
	}

	extract := make(map[string]ScaffoldExtractRule, len(outputs))
	for _, out := range outputs {
		if rootArray {
			rule := ScaffoldExtractRule{Path: "@this"}
			if len(out.ElementFields) > 0 {
				rule.Fields = make(map[string]string, len(out.ElementFields))
				for _, ef := range out.ElementFields {
					rule.Fields[ef.Name] = ef.Name
				}
			}
			extract[out.Name] = rule
		} else {
			extract[out.Name] = ScaffoldExtractRule{Path: out.Name, Optional: out.Optional}
		}
	}
	return extract
}

// deriveArrayOutputName converts an operationId like "listPets" into a shorter
// output name like "pets" by stripping common prefixes.
func deriveArrayOutputName(operationId string) string {
	prefixes := []string{"list", "search", "find", "get", "fetch", "query"}
	lower := strings.ToLower(operationId)

	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			rest := operationId[len(prefix):]
			if rest != "" {
				// Lowercase first letter
				return strings.ToLower(rest[:1]) + rest[1:]
			}
		}
	}

	// Fallback: if no prefix matched or nothing left after stripping
	return "items"
}
