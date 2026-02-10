package oas

import (
	"fmt"
	"strings"

	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/gburgyan/aat/graph"
)

// Validator implements graph.SpecValidator for OpenAPI specifications.
type Validator struct {
	specs map[string]*v3high.Document
}

// NewValidator creates a new OAS validator.
func NewValidator() *Validator {
	return &Validator{
		specs: make(map[string]*v3high.Document),
	}
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
		_, _, op, err := FindOperation(model, node.OAS.OperationID)
		if err != nil {
			result.Issues = append(result.Issues, graph.SpecValidationIssue{
				Severity: graph.SpecError,
				Node:     nodeName,
				Message:  fmt.Sprintf("operationId %q not found in spec %q", node.OAS.OperationID, specPath),
			})
			continue
		}

		// Rule 5: graph inputs should exist in OAS parameters or request body
		oasParamNames := collectInputNames(op)
		for _, inp := range node.Inputs {
			if _, exists := oasParamNames[inp.Name]; !exists {
				result.Issues = append(result.Issues, graph.SpecValidationIssue{
					Severity: graph.SpecWarning,
					Node:     nodeName,
					Message:  fmt.Sprintf("input %q not found in OAS parameters or request body for %q", inp.Name, node.OAS.OperationID),
				})
			}
		}

		// Rule 6: OAS required parameters should exist in graph inputs
		graphInputNames := make(map[string]bool)
		for _, inp := range node.Inputs {
			graphInputNames[inp.Name] = true
		}
		for name := range collectRequiredInputs(op) {
			if !graphInputNames[name] {
				result.Issues = append(result.Issues, graph.SpecValidationIssue{
					Severity: graph.SpecWarning,
					Node:     nodeName,
					Message:  fmt.Sprintf("OAS required parameter %q missing from graph inputs for %q", name, node.OAS.OperationID),
				})
			}
		}

		// Rule 7: graph outputs should exist in OAS 2xx response schema
		oasOutputNames := collectOutputNames(op)
		if oasOutputNames != nil {
			for _, out := range node.Outputs {
				if _, exists := oasOutputNames[out.Name]; !exists {
					result.Issues = append(result.Issues, graph.SpecValidationIssue{
						Severity: graph.SpecWarning,
						Node:     nodeName,
						Message:  fmt.Sprintf("output %q not found in OAS 2xx response schema for %q", out.Name, node.OAS.OperationID),
					})
				}
			}
		}
	}

	return result
}

// GetSpec returns the loaded spec for a reference path, or nil if not loaded.
func (v *Validator) GetSpec(refPath string) *v3high.Document {
	return v.specs[refPath]
}

// collectInputNames returns all parameter names and request body property names for an operation.
func collectInputNames(op *v3high.Operation) map[string]bool {
	names := make(map[string]bool)

	// Parameters (query, header, path, cookie)
	for _, param := range op.Parameters {
		if param != nil {
			names[param.Name] = true
		}
	}

	// Request body properties
	if op.RequestBody != nil && op.RequestBody.Content != nil {
		for mediaType := range op.RequestBody.Content.ValuesFromOldest() {
			if mediaType.Schema != nil {
				schema := mediaType.Schema.Schema()
				if schema != nil && schema.Properties != nil {
					for propName := range schema.Properties.KeysFromOldest() {
						names[propName] = true
					}
				}
			}
		}
	}

	return names
}

// collectRequiredInputs returns names of required parameters and required request body properties.
func collectRequiredInputs(op *v3high.Operation) map[string]bool {
	names := make(map[string]bool)

	// Required parameters
	for _, param := range op.Parameters {
		if param != nil && param.Required != nil && *param.Required {
			names[param.Name] = true
		}
	}

	// Required request body properties
	if op.RequestBody != nil && op.RequestBody.Content != nil {
		for mediaType := range op.RequestBody.Content.ValuesFromOldest() {
			if mediaType.Schema != nil {
				schema := mediaType.Schema.Schema()
				if schema != nil {
					for _, req := range schema.Required {
						names[req] = true
					}
				}
			}
		}
	}

	return names
}

// collectOutputNames returns property names from the first 2xx response schema.
// Returns nil if no 2xx response with a schema is found (skips rule 7).
func collectOutputNames(op *v3high.Operation) map[string]bool {
	if op.Responses == nil || op.Responses.Codes == nil {
		return nil
	}

	// Check common 2xx codes
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
					names := make(map[string]bool)
					for propName := range schema.Properties.KeysFromOldest() {
						names[propName] = true
					}
					return names
				}
			}
		}
	}

	return nil
}
