package oas

import (
	"encoding/json"
	"fmt"
	"strings"

	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/datamodel/high/base"

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
	Path   string            `yaml:"path"`
	Fields map[string]string `yaml:"fields,omitempty"`
}

// MarshalYAML emits a bare string when Fields is empty (backward-compatible
// scalar format), or a mapping when Fields is set.
func (r ScaffoldExtractRule) MarshalYAML() (interface{}, error) {
	if len(r.Fields) == 0 {
		return r.Path, nil
	}
	// Return a struct so YAML renders as {path: ..., fields: {...}}
	type raw struct {
		Path   string            `yaml:"path"`
		Fields map[string]string `yaml:"fields,omitempty"`
	}
	return raw(r), nil
}

// Generate produces a graph and template stubs from an OAS spec.
// specFile is used as the graph-level OAS reference (just the filename).
func Generate(model *v3high.Document, specFile string) (*GenerateResult, error) {
	if model.Paths == nil {
		return nil, fmt.Errorf("OAS spec has no paths")
	}

	result := &GenerateResult{
		Graph: &graph.Graph{
			Version: "1.0.0",
			OAS:     specFile,
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

			node := generateNode(c.method, pathStr, c.op)
			tmpl := generateTemplate(c.method, pathStr, c.op)

			result.Graph.Nodes[c.op.OperationId] = node
			result.Templates = append(result.Templates, tmpl)
		}
	}

	if len(result.Graph.Nodes) == 0 {
		return nil, fmt.Errorf("no operations with operationId found in spec")
	}

	return result, nil
}

// generateNode builds a graph.Node from one OAS operation.
func generateNode(method, path string, op *v3high.Operation) *graph.Node {
	node := &graph.Node{
		Name:        op.OperationId,
		Description: op.Summary,
		Adapter:     op.OperationId,
		Inputs:      collectNodeInputs(op),
		Outputs:     collectNodeOutputs(op),
		OAS: &graph.OASRef{
			OperationID: op.OperationId,
		},
	}
	return node
}

// generateTemplate builds a ScaffoldTemplate from one OAS operation.
func generateTemplate(method, path string, op *v3high.Operation) *ScaffoldTemplate {
	tmpl := &ScaffoldTemplate{
		Adapter:  op.OperationId,
		Protocol: "http",
		Request: ScaffoldTemplateRequest{
			Method: method,
		},
		Response: ScaffoldTemplateResponse{},
	}

	// Build path with converted params and query string
	convertedPath := convertPathParams(path)

	// Collect query params for the URL
	var queryParams []string
	for _, param := range op.Parameters {
		if param != nil && param.In == "query" {
			queryParams = append(queryParams, fmt.Sprintf("%s={{%s}}", param.Name, param.Name))
		}
	}
	if len(queryParams) > 0 {
		convertedPath += "?" + strings.Join(queryParams, "&")
	}
	tmpl.Request.Path = convertedPath

	// Headers
	headers := make(map[string]string)
	hasRequestBody := op.RequestBody != nil && op.RequestBody.Content != nil
	if hasRequestBody {
		headers["Content-Type"] = "application/json"
	}
	for _, param := range op.Parameters {
		if param != nil && param.In == "header" {
			headers[param.Name] = fmt.Sprintf("{{%s}}", param.Name)
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
	tmpl.Response.Extract = buildExtractMap(outputs)

	return tmpl
}

// collectNodeInputs gathers inputs from OAS parameters and request body.
func collectNodeInputs(op *v3high.Operation) []graph.Input {
	var inputs []graph.Input

	// Parameters (query, header, path, cookie)
	for _, param := range op.Parameters {
		if param == nil {
			continue
		}
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

				props := resolveSchemaProperties(schema)
				for propName, propProxy := range props {
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
			props := resolveSchemaProperties(itemSchema)
			for propName, propProxy := range props {
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

// collectObjectOutputs builds outputs from an object schema's properties.
func collectObjectOutputs(schema *base.Schema) []graph.Output {
	var outputs []graph.Output

	props := resolveSchemaProperties(schema)
	for propName, propProxy := range props {
		out := graph.Output{
			Name: propName,
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

// resolveSchemaProperties returns a map of property name → schema proxy,
// following $ref proxies to get the properties map.
func resolveSchemaProperties(schema *base.Schema) map[string]*base.SchemaProxy {
	if schema.Properties != nil && schema.Properties.Len() > 0 {
		props := make(map[string]*base.SchemaProxy)
		for propName, propProxy := range schema.Properties.FromOldest() {
			props[propName] = propProxy
		}
		return props
	}
	return nil
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

// buildBodyTemplate creates a JSON body string with {{placeholder}} values for each property.
func buildBodyTemplate(op *v3high.Operation) string {
	if op.RequestBody == nil || op.RequestBody.Content == nil {
		return ""
	}

	jsonContent := op.RequestBody.Content.GetOrZero("application/json")
	if jsonContent == nil || jsonContent.Schema == nil {
		return ""
	}

	schema := jsonContent.Schema.Schema()
	if schema == nil {
		return ""
	}

	props := resolveSchemaProperties(schema)
	if props == nil {
		return ""
	}

	bodyMap := make(map[string]string, len(props))
	for propName := range props {
		bodyMap[propName] = fmt.Sprintf("{{%s}}", propName)
	}

	data, err := json.MarshalIndent(bodyMap, "", "  ")
	if err != nil {
		return ""
	}
	return string(data)
}

// buildExtractMap creates extract rules from outputs. Array outputs with
// elementFields get a fields section mapping name → name (since OAS property
// names match JSON keys directly).
func buildExtractMap(outputs []graph.Output) map[string]ScaffoldExtractRule {
	if len(outputs) == 0 {
		return nil
	}

	extract := make(map[string]ScaffoldExtractRule, len(outputs))
	for _, out := range outputs {
		if out.Type == "object[]" || strings.HasSuffix(out.Type, "[]") {
			rule := ScaffoldExtractRule{Path: "@this"}
			if len(out.ElementFields) > 0 {
				rule.Fields = make(map[string]string, len(out.ElementFields))
				for _, ef := range out.ElementFields {
					rule.Fields[ef.Name] = ef.Name
				}
			}
			extract[out.Name] = rule
		} else {
			extract[out.Name] = ScaffoldExtractRule{Path: out.Name}
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
