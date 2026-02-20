package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// TemplateInputValidationError collects all template-vs-graph input mismatches
// found during pre-flight validation.
type TemplateInputValidationError struct {
	Errors []string
}

func (e *TemplateInputValidationError) Error() string {
	return fmt.Sprintf("template input validation failed:\n  - %s", strings.Join(e.Errors, "\n  - "))
}

// ValidateTemplateInputs checks all nodes in the graph for template placeholders
// that require a value but the corresponding graph input is optional with no default.
// This catches cases where a template unconditionally uses {{key}} but the graph
// declares the input as optional without providing a default value.
func ValidateTemplateInputs(g *graph.Graph, registry *adapter.Registry) error {
	return validateTemplateInputsForNodes(g, registry, nil, nil)
}

// ValidateTemplateInputsForPlan checks only the nodes referenced by the plan's
// steps and cleanup. For plan-scoped checks, it also considers whether the plan
// step provides a value for the input.
func ValidateTemplateInputsForPlan(g *graph.Graph, registry *adapter.Registry, p *plan.Plan) error {
	nodeSet := make(map[string]bool)
	// Build a map of step node → step values for plan-level overrides.
	// Key is "node:inputName" to handle multiple steps using the same node.
	planValues := make(map[string]map[string]bool)

	for _, step := range p.Execution.Steps {
		nodeSet[step.Node] = true
		if len(step.Values) > 0 {
			if planValues[step.Node] == nil {
				planValues[step.Node] = make(map[string]bool)
			}
			for k, v := range step.Values {
				if !v.IsEmpty() {
					planValues[step.Node][k] = true
				}
			}
		}
	}
	for _, cs := range p.Execution.Cleanup {
		nodeSet[cs.Node] = true
	}
	return validateTemplateInputsForNodes(g, registry, nodeSet, planValues)
}

// validateTemplateInputsForNodes is the shared implementation. When nodeFilter
// is non-nil, only nodes in the set are checked. When planValues is non-nil,
// inputs that have a plan-provided value are skipped.
func validateTemplateInputsForNodes(g *graph.Graph, registry *adapter.Registry, nodeFilter map[string]bool, planValues map[string]map[string]bool) error {
	var errs []string

	// Sort node names for deterministic error ordering.
	names := make([]string, 0, len(g.Nodes))
	for name := range g.Nodes {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if nodeFilter != nil && !nodeFilter[name] {
			continue
		}
		node := g.Nodes[name]

		tmpl, ok := registry.GetTemplate(node.Adapter)
		if !ok {
			// Not a template adapter — skip.
			continue
		}

		// Get required placeholders from the template.
		required, _, _ := adapter.ClassifyInputs(tmpl)

		// Build a lookup of graph inputs by name.
		graphInputs := make(map[string]*graph.Input, len(node.Inputs))
		for i := range node.Inputs {
			graphInputs[node.Inputs[i].Name] = &node.Inputs[i]
		}

		for _, placeholder := range required {
			inp, exists := graphInputs[placeholder]
			if !exists {
				// Placeholder not in graph inputs — could be an env config value
				// or path parameter. Not our concern here.
				continue
			}

			if !inp.Optional {
				// Required input — plan.Validate already ensures these are provided.
				continue
			}

			if inp.Default != nil {
				// Optional with a default — engine will resolve via pool/value/from.
				continue
			}

			// Check if the plan provides a value for this input.
			if planValues != nil {
				if nodeVals, ok := planValues[name]; ok && nodeVals[placeholder] {
					continue
				}
			}

			errs = append(errs, fmt.Sprintf(
				"node %q: template requires placeholder %q but graph input is optional with no default",
				name, placeholder))
		}
	}

	if len(errs) > 0 {
		return &TemplateInputValidationError{Errors: errs}
	}
	return nil
}
