package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
)

// AdapterValidationError collects all output-vs-extract mismatches found
// during pre-flight adapter validation.
type AdapterValidationError struct {
	Errors []string
}

func (e *AdapterValidationError) Error() string {
	return fmt.Sprintf("adapter output validation failed:\n  - %s", strings.Join(e.Errors, "\n  - "))
}

// ValidateAdapterOutputs checks that graph-declared outputs match the
// extract keys in the corresponding template for each node. Non-template
// adapters (custom Adapter implementations) are skipped. Nodes with no
// outputs (cleanup/void operations) are also skipped.
//
// Additionally validates that template extract rule fields match graph
// elementField names for array outputs.
func ValidateAdapterOutputs(g *graph.Graph, registry *adapter.Registry) error {
	var errs []string

	// Sort node names for deterministic error ordering.
	names := make([]string, 0, len(g.Nodes))
	for name := range g.Nodes {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		node := g.Nodes[name]

		// Skip nodes with no declared outputs (cleanup/void).
		if len(node.Outputs) == 0 {
			continue
		}

		tmpl, ok := registry.GetTemplate(node.Adapter)
		if !ok {
			// Not a template adapter — skip (could be a custom implementation).
			continue
		}

		// Build sets for comparison.
		graphOutputs := make(map[string]bool, len(node.Outputs))
		for _, out := range node.Outputs {
			graphOutputs[out.Name] = true
		}

		extractKeys := make(map[string]bool, len(tmpl.Response.Extract))
		for key := range tmpl.Response.Extract {
			extractKeys[key] = true
		}

		// Graph output not extracted by template.
		for _, out := range node.Outputs {
			if !extractKeys[out.Name] {
				errs = append(errs, fmt.Sprintf("node %q: graph declares output %q but template does not extract it", name, out.Name))
			}
		}

		// Template extracts key not declared in graph.
		// Sort extract keys for deterministic ordering.
		sortedExtract := make([]string, 0, len(extractKeys))
		for key := range extractKeys {
			sortedExtract = append(sortedExtract, key)
		}
		sort.Strings(sortedExtract)

		for _, key := range sortedExtract {
			if !graphOutputs[key] {
				errs = append(errs, fmt.Sprintf("node %q: template extracts %q but graph does not declare this output", name, key))
			}
		}

		// Cross-validate template extract fields vs graph elementFields.
		errs = append(errs, validateElementFields(name, node, tmpl)...)
	}

	if len(errs) > 0 {
		return &AdapterValidationError{Errors: errs}
	}
	return nil
}

// validateElementFields checks that template extract rule fields match
// the graph's declared elementFields for array outputs.
func validateElementFields(nodeName string, node *graph.Node, tmpl *adapter.Template) []string {
	var errs []string

	for _, out := range node.Outputs {
		if len(out.ElementFields) == 0 {
			continue
		}

		rule, ok := tmpl.Response.Extract[out.Name]
		if !ok {
			continue
		}

		if len(rule.Fields) == 0 {
			// Template has no fields for this array output — check if graph
			// elementFields have paths (backward-compat mode, no warning).
			continue
		}

		// Build sets for comparison.
		graphEF := make(map[string]bool, len(out.ElementFields))
		for _, ef := range out.ElementFields {
			graphEF[ef.Name] = true
		}

		templateFields := make(map[string]bool, len(rule.Fields))
		for fieldName := range rule.Fields {
			templateFields[fieldName] = true
		}

		// Template field not declared in graph elementFields.
		sortedTemplateFields := make([]string, 0, len(templateFields))
		for f := range templateFields {
			sortedTemplateFields = append(sortedTemplateFields, f)
		}
		sort.Strings(sortedTemplateFields)

		for _, f := range sortedTemplateFields {
			if !graphEF[f] {
				errs = append(errs, fmt.Sprintf(
					"node %q output %q: template field %q has no corresponding graph elementField",
					nodeName, out.Name, f))
			}
		}

		// Graph elementField not extracted by template fields.
		// Skip this check when the template has a Lua transform, since the
		// transform script may populate those fields dynamically.
		if !tmpl.HasTransform() {
			for _, ef := range out.ElementFields {
				if !templateFields[ef.Name] {
					errs = append(errs, fmt.Sprintf(
						"node %q output %q: graph elementField %q has no corresponding template field",
						nodeName, out.Name, ef.Name))
				}
			}
		}
	}

	return errs
}
