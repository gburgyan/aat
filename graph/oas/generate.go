package oas

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"gopkg.in/yaml.v3"

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
	Form    ScaffoldForm      `yaml:"form,omitempty"`
}

// ScaffoldForm is a scaffold template's request.form: one field per body
// property, in the order the spec lists them.
type ScaffoldForm []ScaffoldFormField

// ScaffoldFormField is one request.form field and its value.
type ScaffoldFormField struct {
	Name  string
	Value string
}

// MarshalYAML writes the fields as a mapping in order, which a Go map would
// sort.
func (f ScaffoldForm) MarshalYAML() (any, error) {
	n := &yaml.Node{Kind: yaml.MappingNode}
	for _, field := range f {
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: field.Name},
			&yaml.Node{Kind: yaml.ScalarNode, Value: field.Value})
	}
	return n, nil
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
// graph file is in. Warnings name the operations it skips and the parts of a
// request a template leaves to write by hand.
func Generate(model *v3high.Document, specRef string) (*GenerateResult, error) {
	return GenerateOperations(model, specRef, GenerateOptions{})
}

// GenerateOptions narrows what GenerateOperations scaffolds. With no filter
// set, every operation is generated; otherwise an operation is generated when
// it matches any of them.
type GenerateOptions struct {
	// OperationIDs are the operationIds to generate.
	OperationIDs []string
	// PathPrefixes are paths whose operations to generate, matched by whole
	// segments: /carts matches /carts and /carts/{cartId}, but not /cartsummary.
	PathPrefixes []string
}

// GenerateOperations is Generate for the operations opts selects, for a spec
// too large to scaffold whole. An operationId the spec doesn't have, or a path
// prefix that matches no path, is an error.
func GenerateOperations(model *v3high.Document, specRef string, opts GenerateOptions) (*GenerateResult, error) {
	if model.Paths == nil {
		return nil, fmt.Errorf("OAS spec has no paths")
	}
	for _, prefix := range opts.PathPrefixes {
		if !strings.HasPrefix(prefix, "/") {
			return nil, fmt.Errorf("path %q must start with /", prefix)
		}
	}
	filtered := len(opts.OperationIDs) > 0 || len(opts.PathPrefixes) > 0
	wanted := make(map[string]bool, len(opts.OperationIDs))
	for _, id := range opts.OperationIDs {
		wanted[id] = true
	}
	var specIDs []string
	matchedPrefixes := make(map[string]bool)

	result := &GenerateResult{
		Graph: &graph.Graph{
			Version: "1.0.0",
			OAS:     specRef,
			Nodes:   make(map[string]*graph.Node),
		},
	}

	for pathStr, pathItem := range model.Paths.PathItems.FromOldest() {
		pathMatched := false
		for _, prefix := range opts.PathPrefixes {
			if pathUnder(pathStr, prefix) {
				matchedPrefixes[prefix] = true
				pathMatched = true
			}
		}
		for _, mo := range PathOperations(pathItem) {
			op := mo.Operation
			if op.OperationId != "" {
				specIDs = append(specIDs, op.OperationId)
			}
			if filtered && !pathMatched && !wanted[op.OperationId] {
				continue
			}
			if op.OperationId == "" {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("skipping %s %s: no operationId", mo.Method, pathStr))
				continue
			}

			params := OperationParameters(pathItem, op)
			var notes []string
			for _, param := range params {
				if note := parameterStyleNote(param); note != "" {
					notes = append(notes, note)
				}
			}
			body, bodyNotes := describeRequestBody(op)
			for _, note := range append(notes, bodyNotes...) {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("%s %s (%s): %s", mo.Method, pathStr, op.OperationId, note))
			}

			result.Graph.Nodes[op.OperationId] = generateNode(params, op, body)
			result.Templates = append(result.Templates, generateTemplate(mo.Method, pathStr, params, op, body))
		}
	}

	if err := unmatchedFilters(opts, specIDs, matchedPrefixes); err != nil {
		return nil, err
	}
	if len(result.Graph.Nodes) == 0 {
		return nil, fmt.Errorf("no operations with operationId found in spec")
	}

	return result, nil
}

// pathUnder reports whether path is prefix or lies under it, by whole segments.
func pathUnder(path, prefix string) bool {
	trimmed := strings.TrimSuffix(prefix, "/")
	return path == prefix || path == trimmed || strings.HasPrefix(path, trimmed+"/")
}

// unmatchedFilters returns an error naming the operationIds in opts that the
// spec doesn't have, with the spec's operationIds that resemble each, and the
// path prefixes that matched no path. It returns nil when every filter matched.
func unmatchedFilters(opts GenerateOptions, specIDs []string, matchedPrefixes map[string]bool) error {
	present := make(map[string]bool, len(specIDs))
	for _, id := range specIDs {
		present[id] = true
	}
	var problems []string
	for _, id := range opts.OperationIDs {
		if present[id] {
			continue
		}
		problem := fmt.Sprintf("operationId %q is not in the spec", id)
		if similar := similarOperationIDs(id, specIDs); len(similar) > 0 {
			problem += fmt.Sprintf(" (did you mean %s?)", strings.Join(similar, ", "))
		}
		problems = append(problems, problem)
	}
	for _, prefix := range opts.PathPrefixes {
		if !matchedPrefixes[prefix] {
			problems = append(problems, fmt.Sprintf("path %q matches no path in the spec", prefix))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

// similarOperationIDs returns up to three of specIDs that contain id, or that id
// contains, ignoring case.
func similarOperationIDs(id string, specIDs []string) []string {
	lower := strings.ToLower(id)
	var similar []string
	for _, candidate := range specIDs {
		c := strings.ToLower(candidate)
		if strings.Contains(c, lower) || strings.Contains(lower, c) {
			similar = append(similar, candidate)
			if len(similar) == 3 {
				break
			}
		}
	}
	return similar
}

// generateNode builds a graph.Node from one OAS operation. The node's name is
// its key in the graph, so Name stays empty and the graph file has no name:.
func generateNode(params []*v3high.Parameter, op *v3high.Operation, body requestBody) *graph.Node {
	node := &graph.Node{
		Description: op.Summary,
		Adapter:     op.OperationId,
		Inputs:      collectNodeInputs(params, body),
		Outputs:     collectNodeOutputs(op),
		OAS: &graph.OASRef{
			OperationID: op.OperationId,
		},
	}
	return node
}

// generateTemplate builds a ScaffoldTemplate from one OAS operation. Optional
// query parameters, cookies, headers, and body properties sit inside {{?name}}
// blocks, so a generated template runs with only its required inputs.
func generateTemplate(method, path string, params []*v3high.Parameter, op *v3high.Operation, body requestBody) *ScaffoldTemplate {
	tmpl := &ScaffoldTemplate{
		Adapter:  op.OperationId,
		Protocol: "http",
		Request: ScaffoldTemplateRequest{
			Method: method,
		},
		Response: ScaffoldTemplateResponse{},
	}

	// Path with converted params and the query string
	var query, cookies []templateField
	for _, param := range params {
		field := templateField{name: param.Name, required: param.Required != nil && *param.Required}
		switch param.In {
		case "query":
			query = append(query, field)
		case "cookie":
			cookies = append(cookies, field)
		}
	}
	tmpl.Request.Path = convertPathParams(path) + buildQueryString(query)

	// Headers: the body's media type, header parameters, and one Cookie header.
	// A request.form sets its own Content-Type unless the spec's media type adds
	// parameters, such as a charset. A header whose value is one placeholder is
	// not sent when an optional parameter has no value.
	headers := make(map[string]string)
	if body.generated() && body.mediaType != formMediaType {
		headers["Content-Type"] = body.mediaType
	}
	for _, param := range params {
		if param.In == "header" {
			headers[param.Name] = fmt.Sprintf("{{%s}}", param.Name)
		}
	}
	if len(cookies) > 0 {
		headers["Cookie"] = buildPairs(cookies, "", "; ")
	}
	if len(headers) > 0 {
		tmpl.Request.Headers = headers
	}

	// Body
	if body.generated() {
		if body.kind == bodyForm {
			tmpl.Request.Form = buildForm(body)
		} else {
			tmpl.Request.Body = buildJSONBody(body.fields())
		}
	}

	// Extract map
	outputs := collectNodeOutputs(op)
	tmpl.Response.Extract = buildExtractMap(outputs, isArrayResponse(op))

	return tmpl
}

// templateField is a query parameter, cookie, or body property a template sends.
type templateField struct {
	name     string
	required bool
	raw      bool // JSON body only: the value is inserted as a JSON literal, not a quoted string
}

// buildQueryString renders "?a={{a}}&b={{b}}" for query parameters.
func buildQueryString(fields []templateField) string {
	return buildPairs(fields, "?", "&")
}

// buildPairs renders name={{name}} pairs joined by sep, with lead before the
// first: required fields in order, then each optional field in a conditional
// block whose separator is correct whichever optional values are present. It
// renders query strings, form bodies, and the Cookie header.
func buildPairs(fields []templateField, lead, sep string) string {
	required, optional := splitRequired(fields)
	render := func(f templateField) string {
		return f.name + "={{" + f.name + "}}"
	}
	var b strings.Builder
	for i, f := range required {
		if i == 0 {
			b.WriteString(lead)
		} else {
			b.WriteString(sep)
		}
		b.WriteString(render(f))
	}
	writeOptional(&b, optional, len(required) > 0, lead, sep, render)
	return b.String()
}

// buildForm returns a request.form with one field per body property, in spec
// order, each sending its input. A field is left out when its input has no
// value, so required and optional properties are written alike. An array
// property the spec encodes as a deepObject, as Stripe's are, is written
// name[]; any other repeats its plain name. An object value is sent as
// bracketed keys.
func buildForm(body requestBody) ScaffoldForm {
	form := make(ScaffoldForm, len(body.props))
	for i, p := range body.props {
		name := p.name
		if body.deepObject[p.name] && p.proxy != nil && schemaType(p.proxy.Schema()) == "array" {
			name += "[]"
		}
		form[i] = ScaffoldFormField{Name: name, Value: "{{" + p.name + "}}"}
	}
	return form
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

// parameterStyleNote describes a parameter serialization a template does not
// reproduce: a style other than the default for its location, or explode:
// false on a query or cookie list or object. It returns "" otherwise.
func parameterStyleNote(param *v3high.Parameter) string {
	if param == nil {
		return ""
	}
	defaultStyle := "simple"
	if param.In == "query" || param.In == "cookie" {
		defaultStyle = "form"
	}
	if param.Style != "" && param.Style != defaultStyle {
		return fmt.Sprintf("parameter %q uses style %s, which the template does not reproduce; rewrite it by hand", param.Name, param.Style)
	}
	if defaultStyle == "form" && param.Explode != nil && !*param.Explode && param.Schema != nil {
		switch schemaType(param.Schema.Schema()) {
		case "array", "object":
			return fmt.Sprintf("parameter %q sets explode: false, which the template does not reproduce; rewrite it by hand", param.Name)
		}
	}
	return ""
}

// bodyKind is how a scaffold treats a request body media type.
type bodyKind int

const (
	bodyOther     bodyKind = iota // not generated: no inputs and no body
	bodyJSON                      // application/json or another JSON type
	bodyForm                      // application/x-www-form-urlencoded
	bodyMultipart                 // multipart/*: inputs, but no body
)

// requestBody is what a scaffold takes from an operation's request body: the
// media type it picked and that schema's properties.
type requestBody struct {
	kind      bodyKind
	mediaType string // as the spec writes it; the template's Content-Type
	props     []schemaProperty
	required  map[string]bool
	// deepObject holds the form properties the spec encodes with style
	// deepObject.
	deepObject map[string]bool
}

// generated reports whether the template gets a body and a Content-Type: a
// JSON or form body whose schema has properties.
func (b requestBody) generated() bool {
	return (b.kind == bodyJSON || b.kind == bodyForm) && len(b.props) > 0
}

// fields returns the body's properties as template fields.
func (b requestBody) fields() []templateField {
	fields := make([]templateField, len(b.props))
	for i, p := range b.props {
		fields[i] = templateField{name: p.name, required: b.required[p.name], raw: isJSONLiteralType(p.proxy)}
	}
	return fields
}

// mediaKind classifies a request body media type, ignoring parameters such as
// charset.
func mediaKind(mediaType string) bodyKind {
	name, _, _ := strings.Cut(mediaType, ";")
	name = strings.ToLower(strings.TrimSpace(name))
	switch {
	case name == "application/json" || strings.HasSuffix(name, "+json"):
		return bodyJSON
	case name == "application/x-www-form-urlencoded":
		return bodyForm
	case strings.HasPrefix(name, "multipart/"):
		return bodyMultipart
	}
	return bodyOther
}

// describeRequestBody picks the request body media type a scaffold generates
// from: JSON (application/json before other JSON types), then a form, then
// multipart. The notes say what the template leaves to write by hand.
func describeRequestBody(op *v3high.Operation) (requestBody, []string) {
	if op.RequestBody == nil || op.RequestBody.Content == nil || op.RequestBody.Content.Len() == 0 {
		return requestBody{}, nil
	}

	var body requestBody
	var content *v3high.MediaType
	var listed []string
	for name, mt := range op.RequestBody.Content.FromOldest() {
		listed = append(listed, name)
		kind := mediaKind(name)
		if kind == bodyOther || mt == nil {
			continue
		}
		if content == nil || kind < body.kind || (kind == bodyJSON && name == "application/json" && body.mediaType != "application/json") {
			body.kind, body.mediaType, content = kind, name, mt
		}
	}
	if content == nil {
		return requestBody{}, []string{fmt.Sprintf("the %s request body is not generated; write the body and its Content-Type by hand", strings.Join(listed, " or "))}
	}

	var schema *base.Schema
	if content.Schema != nil {
		schema = content.Schema.Schema()
	}
	var composed bool
	body.props, body.required, composed = objectShape(schema)
	if len(body.props) == 0 && !composed && declaresNoFields(schema) {
		// A body the spec declares empty, such as a GET's, needs nothing written.
		return requestBody{}, nil
	}

	if body.kind == bodyForm && content.Encoding != nil {
		body.deepObject = make(map[string]bool)
		for name, enc := range content.Encoding.FromOldest() {
			if enc != nil && enc.Style == "deepObject" {
				body.deepObject[name] = true
			}
		}
	}

	var notes []string
	switch {
	case body.kind == bodyMultipart && len(body.props) > 0:
		notes = append(notes, fmt.Sprintf("the %s body is not generated; its properties are inputs, so write the body by hand", body.mediaType))
	case body.kind == bodyMultipart:
		notes = append(notes, fmt.Sprintf("the %s body is not generated; write it by hand", body.mediaType))
	case composed && len(body.props) == 0:
		notes = append(notes, fmt.Sprintf("the %s body schema uses oneOf or anyOf; write the body by hand", body.mediaType))
	case composed:
		notes = append(notes, fmt.Sprintf("the %s body schema uses oneOf or anyOf; only the properties outside them are generated", body.mediaType))
	case len(body.props) == 0:
		notes = append(notes, fmt.Sprintf("the %s body schema declares no properties; write the body by hand", body.mediaType))
	}
	return body, notes
}

// declaresNoFields reports whether a body schema declares that it has no fields:
// no properties, and additionalProperties: false.
func declaresNoFields(schema *base.Schema) bool {
	if schema == nil || (schema.Properties != nil && schema.Properties.Len() > 0) {
		return false
	}
	extra := schema.AdditionalProperties
	return extra != nil && extra.IsB() && !extra.B
}

// collectNodeInputs gathers inputs from an operation's parameters (path-item
// and operation level) and the properties of its request body.
func collectNodeInputs(params []*v3high.Parameter, body requestBody) []graph.Input {
	var inputs []graph.Input

	// Parameters (query, header, path, cookie)
	for _, param := range params {
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
	for _, prop := range body.props {
		inp := graph.Input{
			Name:     prop.name,
			Type:     "string",
			Optional: !body.required[prop.name],
		}
		if prop.proxy != nil {
			propSchema := prop.proxy.Schema()
			inp.Type = mapSchemaType(propSchema)
			inp.Constraints = extractConstraints(propSchema)
		}
		inputs = append(inputs, inp)
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
		if schemaType(schema) == "array" {
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
		props, _, _ := objectShape(schema.Items.A.Schema())
		for _, prop := range props {
			field := graph.Field{
				Name: prop.name,
				Type: "string",
			}
			if prop.proxy != nil {
				field.Type = mapSchemaType(prop.proxy.Schema())
			}
			out.ElementFields = append(out.ElementFields, field)
		}
	}

	return []graph.Output{out}
}

// collectObjectOutputs builds outputs from an object schema's properties,
// allOf branches included. A property the schema does not list as required
// becomes an optional output, so a response that omits it does not fail
// extraction.
func collectObjectOutputs(schema *base.Schema) []graph.Output {
	props, required, _ := objectShape(schema)
	var outputs []graph.Output

	for _, prop := range props {
		out := graph.Output{
			Name:     prop.name,
			Type:     "string",
			Optional: !required[prop.name],
		}
		if prop.proxy != nil {
			out.Type = mapSchemaType(prop.proxy.Schema())
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

// maxShapeDepth bounds how far objectShape follows allOf, which a circular
// $ref could otherwise follow forever.
const maxShapeDepth = 16

// objectShape returns the properties of an object schema, its own and then
// those of each allOf branch, in spec order, and the property names they
// require. A property declared twice keeps its first place and schema.
// composed reports a oneOf or anyOf, whose alternatives are left out.
func objectShape(schema *base.Schema) (props []schemaProperty, required map[string]bool, composed bool) {
	required = make(map[string]bool)
	seen := make(map[string]bool)
	var walk func(s *base.Schema, depth int)
	walk = func(s *base.Schema, depth int) {
		if s == nil || depth > maxShapeDepth {
			return
		}
		if len(s.OneOf) > 0 || len(s.AnyOf) > 0 {
			composed = true
		}
		for _, p := range resolveSchemaProperties(s) {
			if !seen[p.name] {
				seen[p.name] = true
				props = append(props, p)
			}
		}
		for _, name := range s.Required {
			required[name] = true
		}
		for _, branch := range s.AllOf {
			if branch != nil {
				walk(branch.Schema(), depth+1)
			}
		}
	}
	walk(schema, 0)
	return props, required, composed
}

// schemaType returns a schema's type. In an OpenAPI 3.1 type list "null" is
// skipped, so ["null", "integer"] is "integer". An untyped schema gives "".
func schemaType(schema *base.Schema) string {
	if schema == nil {
		return ""
	}
	for _, t := range schema.Type {
		if t != "null" {
			return t
		}
	}
	return ""
}

// mapSchemaType converts an OAS JSON Schema type+format to an AAT graph type.
func mapSchemaType(schema *base.Schema) string {
	return mapSchemaTypeAt(schema, 0)
}

// mapSchemaTypeAt is mapSchemaType for a schema that is the item type of depth
// enclosing arrays. A circular $ref can make an array its own item type, so
// past maxShapeDepth the item type is taken as string.
func mapSchemaTypeAt(schema *base.Schema, depth int) string {
	if schema == nil || depth > maxShapeDepth {
		return "string"
	}

	switch schemaType(schema) {
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
			elemType = mapSchemaTypeAt(schema.Items.A.Schema(), depth+1)
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

// buildJSONBody creates a JSON body with a {{placeholder}} for each property,
// in spec order: required properties first, then each optional one in a
// conditional block. Strings are quoted; integers, numbers, booleans, arrays,
// and objects are inserted as JSON literals.
func buildJSONBody(fields []templateField) string {
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
	switch schemaType(schema) {
	case "integer", "number", "boolean", "array", "object":
		return true
	case "":
		return (schema.Properties != nil && schema.Properties.Len() > 0) || len(schema.AllOf) > 0
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
		return schemaType(schema) == "array"
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
