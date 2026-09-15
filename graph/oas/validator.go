package oas

import (
	"fmt"
	"slices"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/gburgyan/aat/graph"
)

// Validator implements graph.SpecValidator for OpenAPI specifications.
type Validator struct {
	specs            map[string]*v3high.Document
	outputPaths      OutputPaths
	suppliedFields   SuppliedFields
	headerInputs     HeaderInputs
	formInputFields  FormInputFields
	queryInputParams QueryInputParams
	pathTemplates    PathTemplates
}

// OutputPaths maps node name → output name → the GJSON path the node's template
// extracts that output from. An empty path marks an output a response transform
// computes, which has no response location to check.
type OutputPaths map[string]map[string]string

// SuppliedFields maps node name → the request fields (query parameters, header
// names, top-level body keys) the node's template always sends itself.
type SuppliedFields map[string]map[string]bool

// HeaderInputs maps node name → the inputs the node's template sends only in
// request headers.
type HeaderInputs map[string]map[string]bool

// FormInputFields maps node name → input → the top-level request fields the
// node's template sends that input as: the form fields whose whole value is the
// input.
type FormInputFields map[string]map[string][]string

// QueryInputParams maps node name → input → the query parameters the node's
// template sends that input as: the parameters whose whole value is the input.
type QueryInputParams map[string]map[string][]string

// PathTemplates maps node name → the node's template request path before its
// query, written as an OpenAPI path: a segment that is exactly one placeholder
// becomes {input}.
type PathTemplates map[string]string

// NewValidator creates a new OAS validator.
func NewValidator() *Validator {
	return &Validator{
		specs: make(map[string]*v3high.Document),
	}
}

// WithOutputPaths makes the output check look for each output at its template
// extract path, through nested objects and array items, instead of expecting a
// top-level response property named after the output. Outputs missing from
// paths keep the name-based check.
func (v *Validator) WithOutputPaths(paths OutputPaths) *Validator {
	v.outputPaths = paths
	return v
}

// WithSuppliedFields makes the required-parameter check accept a required
// parameter or body property that the node's template sends itself, such as a
// literal "photoUrls": [] with no graph input behind it.
func (v *Validator) WithSuppliedFields(fields SuppliedFields) *Validator {
	v.suppliedFields = fields
	return v
}

// WithHeaderInputs makes the unknown-input check accept an input that the
// node's template sends only in request headers, such as an idempotency key.
// The template names the header, so the input's name says nothing about the
// spec, and specs often leave such headers undeclared.
func (v *Validator) WithHeaderInputs(inputs HeaderInputs) *Validator {
	v.headerInputs = inputs
	return v
}

// WithFormInputFields makes the input checks match an input to the form field
// the node's template sends it as, so an input named returnedSkus and sent as
// skus[] counts as the skus field.
func (v *Validator) WithFormInputFields(fields FormInputFields) *Validator {
	v.formInputFields = fields
	return v
}

// WithQueryInputParams makes the input checks match an input to the query
// parameter the node's template sends it as, so an input named startingAfter
// and sent as starting_after={{startingAfter}} counts as starting_after.
func (v *Validator) WithQueryInputParams(params QueryInputParams) *Validator {
	v.queryInputParams = params
	return v
}

// WithPathTemplates makes the input checks match an input to the path
// parameter its template path segment fills, so /orders/{{orderId}} against the
// spec's /orders/{order} counts orderId as order.
func (v *Validator) WithPathTemplates(paths PathTemplates) *Validator {
	v.pathTemplates = paths
	return v
}

// CollectSpecPaths returns the unique set of OAS spec paths referenced by the graph.
// Includes the graph-level OAS path and any node-level spec overrides.
func (v *Validator) CollectSpecPaths(g *graph.Graph) []string {
	seen := make(map[string]bool)
	var paths []string

	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	add(g.OAS)
	for _, node := range g.Nodes {
		if node.OAS != nil && node.OAS.Spec != "" {
			add(node.OAS.Spec)
		}
	}

	return paths
}

// LoadSpec loads an OAS spec and stores it by reference path.
// refPath is the key used in graph YAML; fsPath is the resolved filesystem path.
func (v *Validator) LoadSpec(refPath, fsPath string) error {
	model, err := LoadSpec(fsPath)
	if err != nil {
		return err
	}
	v.specs[refPath] = model
	return nil
}

// Validate cross-references graph nodes against loaded OAS specs.
// Only validates nodes that have an OAS field set.
func (v *Validator) Validate(g *graph.Graph) *graph.SpecValidationResult {
	result := &graph.SpecValidationResult{}

	for nodeName, node := range g.Nodes {
		if node.OAS == nil {
			continue
		}

		// Rule 1: operationId must not be empty
		if node.OAS.OperationID == "" {
			result.Issues = append(result.Issues, graph.SpecValidationIssue{
				Severity: graph.SpecError,
				Node:     nodeName,
				Message:  "oas set but operationId is empty",
			})
			continue
		}

		// Rule 2: must have a resolvable spec path
		specPath := ResolveNodeSpec(node, g.OAS)
		if specPath == "" {
			result.Issues = append(result.Issues, graph.SpecValidationIssue{
				Severity: graph.SpecError,
				Node:     nodeName,
				Message:  "no OAS spec path (neither node-level spec nor graph-level oas)",
			})
			continue
		}

		// Rule 3: spec must be in loaded specs map
		model, ok := v.specs[specPath]
		if !ok {
			result.Issues = append(result.Issues, graph.SpecValidationIssue{
				Severity: graph.SpecError,
				Node:     nodeName,
				Message:  fmt.Sprintf("OAS spec %q not loaded", specPath),
			})
			continue
		}

		// Rule 4: operationId must exist in spec
		_, opPath, pathItem, op, err := FindOperation(model, node.OAS.OperationID)
		if err != nil {
			result.Issues = append(result.Issues, graph.SpecValidationIssue{
				Severity: graph.SpecError,
				Node:     nodeName,
				Message:  fmt.Sprintf("operationId %q not found in spec %q", node.OAS.OperationID, specPath),
			})
			continue
		}

		// Rule 5: graph inputs should exist in OAS parameters or request body,
		// unless the template sends them only in headers, or under another
		// name: as a form field, a query parameter, or a path segment
		oasParamNames := collectInputNames(pathItem, op)
		sentAs := v.sentAs(nodeName, opPath)
		for _, inp := range node.Inputs {
			if !oasParamNames[inp.Name] && !v.headerInputs[nodeName][inp.Name] && !anyIn(sentAs[inp.Name], oasParamNames) {
				result.Issues = append(result.Issues, graph.SpecValidationIssue{
					Severity: graph.SpecWarning,
					Node:     nodeName,
					Message:  fmt.Sprintf("input %q not found in OAS parameters or request body for %q", inp.Name, node.OAS.OperationID),
				})
			}
		}

		// Rule 6: OAS required parameters should exist in graph inputs, unless
		// the template supplies them itself
		graphInputNames := make(map[string]bool)
		for _, inp := range node.Inputs {
			graphInputNames[inp.Name] = true
			// An input the template sends under another name counts as that field.
			for _, field := range sentAs[inp.Name] {
				graphInputNames[field] = true
			}
		}
		for name := range collectRequiredInputs(pathItem, op) {
			if !graphInputNames[name] && !v.suppliedFields[nodeName][name] {
				result.Issues = append(result.Issues, graph.SpecValidationIssue{
					Severity: graph.SpecWarning,
					Node:     nodeName,
					Message:  fmt.Sprintf("OAS required parameter %q missing from graph inputs for %q", name, node.OAS.OperationID),
				})
			}
		}

		// Rule 7: graph outputs should exist in the OAS 2xx response schema, at
		// their template extract path when one is known
		if schema := successResponseSchema(op); schema != nil {
			for _, out := range node.Outputs {
				path, fromTemplate := v.outputPath(nodeName, out.Name)
				if fromTemplate && path == "" {
					continue // computed by a transform
				}
				if schemaHasPath(schema, path) {
					continue
				}
				msg := fmt.Sprintf("output %q not found in OAS 2xx response schema for %q", out.Name, node.OAS.OperationID)
				if path != out.Name {
					msg = fmt.Sprintf("output %q (extracted from %q) not found in OAS 2xx response schema for %q", out.Name, path, node.OAS.OperationID)
				}
				result.Issues = append(result.Issues, graph.SpecValidationIssue{
					Severity: graph.SpecWarning,
					Node:     nodeName,
					Message:  msg,
				})
			}
		}
	}

	return result
}

// GetSpec returns the loaded spec for a reference path, or nil if not loaded.
func (v *Validator) GetSpec(refPath string) *v3high.Document {
	return v.specs[refPath]
}

// outputPath returns where a node's output sits in the response: the template
// extract path when one was supplied (reported by the second result), or else
// a top-level property named after the output.
func (v *Validator) outputPath(nodeName, output string) (string, bool) {
	if path, ok := v.outputPaths[nodeName][output]; ok {
		return path, true
	}
	return output, false
}

// sentAs maps each input of a node to the request fields its template sends it
// as under another name: the form fields and query parameters whose whole
// value it is, and the path parameters of opPath at the segments it fills.
func (v *Validator) sentAs(nodeName, opPath string) map[string][]string {
	fields := make(map[string][]string)
	add := func(input string, names []string) {
		for _, name := range names {
			if !slices.Contains(fields[input], name) {
				fields[input] = append(fields[input], name)
			}
		}
	}
	for input, names := range v.formInputFields[nodeName] {
		add(input, names)
	}
	for input, names := range v.queryInputParams[nodeName] {
		add(input, names)
	}
	if path, ok := v.pathTemplates[nodeName]; ok {
		for input, params := range pathParamInputs(path, opPath) {
			add(input, params)
		}
	}
	return fields
}

// anyIn reports whether names holds any of fields.
func anyIn(fields []string, names map[string]bool) bool {
	return slices.ContainsFunc(fields, func(field string) bool { return names[field] })
}

// pathParamInputs lines a template path up with an operation's path, both in
// OpenAPI form, and maps each input a segment names to the path parameter at
// that segment: /orders/{orderId} against /orders/{order} maps orderId to
// order. The paths are compared from their last segment, so a base path the
// template writes and the spec leaves to its server still lines up. A literal
// segment that differs, or an input where the spec has a literal, lines up
// nothing.
func pathParamInputs(templatePath, opPath string) map[string][]string {
	tmpl := strings.Split(strings.Trim(templatePath, "/"), "/")
	spec := strings.Split(strings.Trim(opPath, "/"), "/")
	if len(tmpl) < len(spec) {
		return nil
	}
	tmpl = tmpl[len(tmpl)-len(spec):]
	inputs := make(map[string][]string)
	for i, segment := range spec {
		param, isParam := bracedName(segment)
		input, isInput := bracedName(tmpl[i])
		switch {
		case isParam && isInput:
			inputs[input] = append(inputs[input], param)
		case isParam:
			// The template writes the parameter's value itself.
		case segment != tmpl[i]:
			return nil
		}
	}
	return inputs
}

// bracedName returns the name in a path segment written as {name}.
func bracedName(segment string) (string, bool) {
	inner, ok := strings.CutPrefix(segment, "{")
	if !ok {
		return "", false
	}
	inner, ok = strings.CutSuffix(inner, "}")
	if !ok || inner == "" || strings.ContainsAny(inner, "{}") {
		return "", false
	}
	return inner, true
}

// collectInputNames returns all parameter names (path-item and operation level)
// and request body property names, allOf branches included, for an operation.
func collectInputNames(pathItem *v3high.PathItem, op *v3high.Operation) map[string]bool {
	names := make(map[string]bool)

	// Parameters (query, header, path, cookie)
	for _, param := range OperationParameters(pathItem, op) {
		names[param.Name] = true
	}

	// Request body properties
	if op.RequestBody != nil && op.RequestBody.Content != nil {
		for mediaType := range op.RequestBody.Content.ValuesFromOldest() {
			if mediaType != nil && mediaType.Schema != nil {
				props, _, _ := objectShape(mediaType.Schema.Schema())
				for _, prop := range props {
					names[prop.name] = true
				}
			}
		}
	}

	return names
}

// collectRequiredInputs returns names of required parameters (path-item and
// operation level) and required request body properties, allOf branches
// included.
func collectRequiredInputs(pathItem *v3high.PathItem, op *v3high.Operation) map[string]bool {
	names := make(map[string]bool)

	// Required parameters
	for _, param := range OperationParameters(pathItem, op) {
		if param.Required != nil && *param.Required {
			names[param.Name] = true
		}
	}

	// Required request body properties
	if op.RequestBody != nil && op.RequestBody.Content != nil {
		for mediaType := range op.RequestBody.Content.ValuesFromOldest() {
			if mediaType != nil && mediaType.Schema != nil {
				_, required, _ := objectShape(mediaType.Schema.Schema())
				for name := range required {
					names[name] = true
				}
			}
		}
	}

	return names
}

// successResponseSchema returns the first 2xx response schema that declares
// properties. Returns nil if there is none, which skips rule 7.
func successResponseSchema(op *v3high.Operation) *base.Schema {
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
		for mediaType := range resp.Content.ValuesFromOldest() {
			if mediaType.Schema != nil {
				schema := mediaType.Schema.Schema()
				if schema != nil && schema.Properties != nil {
					return schema
				}
			}
		}
	}

	return nil
}

// schemaHasPath reports whether a GJSON extract path can resolve in a response
// described by schema. Object segments must be declared properties, and "#" or
// index segments step into array items. Whatever the schema leaves open (no
// declared properties, additionalProperties, a composition branch that
// matches) counts as present, and GJSON queries, modifiers, and multipaths are
// not checked, so only a definite mismatch is reported.
func schemaHasPath(schema *base.Schema, path string) bool {
	if path == "" || strings.ContainsAny(path, `@|()*?\{}[],:!=<>%`) {
		return true
	}
	return schemaHasSegments(schema, strings.Split(path, "."))
}

func schemaHasSegments(schema *base.Schema, segments []string) bool {
	return schemaHasSegmentsAt(schema, segments, 0)
}

// schemaHasSegmentsAt is schemaHasSegments for a schema reached through depth
// composition branches. A branch doesn't consume a segment, so a circular
// allOf, oneOf, or anyOf could be followed forever; past maxShapeDepth the rest
// of the path counts as present.
func schemaHasSegmentsAt(schema *base.Schema, segments []string, depth int) bool {
	if schema == nil || len(segments) == 0 || depth > maxShapeDepth {
		return true
	}
	for _, group := range [][]*base.SchemaProxy{schema.AllOf, schema.OneOf, schema.AnyOf} {
		for _, branch := range group {
			if branch != nil && schemaHasSegmentsAt(branch.Schema(), segments, depth+1) {
				return true
			}
		}
	}

	segment, rest := segments[0], segments[1:]
	if segment == "#" || isArrayIndex(segment) {
		if schema.Items == nil || !schema.Items.IsA() || schema.Items.A == nil {
			return true // no item schema to check against
		}
		return schemaHasSegmentsAt(schema.Items.A.Schema(), rest, depth)
	}
	if schema.Properties == nil {
		return true // no declared properties to check against
	}
	if property := schema.Properties.GetOrZero(segment); property != nil {
		return schemaHasSegmentsAt(property.Schema(), rest, depth)
	}
	if extra := schema.AdditionalProperties; extra != nil && ((extra.IsA() && extra.A != nil) || (extra.IsB() && extra.B)) {
		return true
	}
	return false
}

// isArrayIndex reports whether a path segment is a numeric array index.
func isArrayIndex(segment string) bool {
	if segment == "" {
		return false
	}
	for _, c := range segment {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
