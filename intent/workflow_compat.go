package intent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// WorkflowCompatResult captures the results of addon-to-base workflow
// compatibility checking.
//   - Warnings: structural AUTOWIRE inputs that the graph CAN produce but a
//     specific base workflow doesn't satisfy.
//   - NonProducible: AUTOWIRE inputs whose name doesn't match any output or
//     elementField in the graph. These indicate either misuse of AUTOWIRE
//     (should be LLM-filled) or a missing graph elementField.
//   - Errors: template loading failures (non-fatal).
type WorkflowCompatResult struct {
	Warnings      []WorkflowCompatWarning
	NonProducible []WorkflowNonProducible
	Errors        []WorkflowCompatError
}

// WorkflowNonProducible records an addon with AUTOWIRE inputs that no node
// in the graph can produce. These inputs either need a graph elementField
// or shouldn't be marked AUTOWIRE.
type WorkflowNonProducible struct {
	Addon  string
	Inputs []string
}

// WorkflowCompatWarning records an addon+base pair where some AUTOWIRE
// inputs in the addon cannot be auto-wired from the base workflow's outputs.
type WorkflowCompatWarning struct {
	Addon        string
	BaseWorkflow string
	UnfedInputs  []string
}

// WorkflowCompatError records a template loading failure for a workflow.
type WorkflowCompatError struct {
	Workflow string
	Err      error
}

// HasWarnings returns true if any compatibility warnings were found.
func (r *WorkflowCompatResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// HasErrors returns true if any template loading errors occurred.
func (r *WorkflowCompatResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// HasNonProducible returns true if any AUTOWIRE inputs reference names
// that no graph node can produce.
func (r *WorkflowCompatResult) HasNonProducible() bool {
	return len(r.NonProducible) > 0
}

// HasIssues returns true if any warnings, non-producible entries, or errors were found.
func (r *WorkflowCompatResult) HasIssues() bool {
	return r.HasWarnings() || r.HasNonProducible() || r.HasErrors()
}

// Format returns a human-readable summary of compatibility issues.
func (r *WorkflowCompatResult) Format() string {
	if !r.HasWarnings() && !r.HasNonProducible() {
		return ""
	}

	var sb strings.Builder
	if r.HasWarnings() {
		sb.WriteString("Workflow compatibility warnings:\n")
		for _, w := range r.Warnings {
			fmt.Fprintf(&sb, "  addon %q + base %q: unfed AUTOWIRE inputs: %s\n",
				w.Addon, w.BaseWorkflow, strings.Join(w.UnfedInputs, ", "))
		}
	}
	if r.HasNonProducible() {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("Non-producible AUTOWIRE inputs (no graph output or elementField matches):\n")
		for _, np := range r.NonProducible {
			fmt.Fprintf(&sb, "  addon %q: %s\n", np.Addon, strings.Join(np.Inputs, ", "))
		}
	}
	return sb.String()
}

// ValidateWorkflowCompat checks that all addon workflows can have their
// AUTOWIRE inputs satisfied when composed into compatible base workflows.
// Only AUTOWIRE inputs that are "structural" (the graph produces a matching
// output name or elementField name somewhere) are checked. Value inputs
// that no node can produce (e.g., email, commentText) are assumed to be
// LLM-filled and are not flagged.
func ValidateWorkflowCompat(g *graph.Graph, graphDir string) *WorkflowCompatResult {
	result := &WorkflowCompatResult{}

	if len(g.Workflows) == 0 {
		return result
	}

	// Partition workflows into base and addon lists.
	var bases, addons []graph.Workflow
	for _, wf := range g.Workflows {
		if wf.IsAddon() {
			addons = append(addons, wf)
		} else {
			bases = append(bases, wf)
		}
	}

	if len(addons) == 0 || len(bases) == 0 {
		return result
	}

	// Build the set of names that the graph can produce (outputs + elementFields).
	producible := buildProducibleNames(g)

	// Load all templates, caching by template path.
	templateCache := make(map[string]*plan.Plan)
	for _, wf := range g.Workflows {
		if wf.Template == "" {
			continue
		}
		if _, cached := templateCache[wf.Template]; cached {
			continue
		}
		p, err := LoadWorkflowTemplate(wf.Template, graphDir, g)
		if err != nil {
			result.Errors = append(result.Errors, WorkflowCompatError{
				Workflow: wf.Name,
				Err:      err,
			})
			continue
		}
		templateCache[wf.Template] = p
	}

	// For each addon, check compatibility with each base workflow.
	for _, addon := range addons {
		addonPlan := templateCache[addon.Template]
		if addonPlan == nil {
			continue // template failed to load
		}

		// Collect AUTOWIRE inputs from the addon plan.
		autowireInputs := collectAutowireInputs(addonPlan)
		if len(autowireInputs) == 0 {
			continue // nothing to check
		}

		// Remove inputs covered by explicit Wire entries (including MANUAL).
		for inputName := range addon.Wire {
			delete(autowireInputs, inputName)
		}

		// Separate structural vs non-producible AUTOWIRE inputs.
		// Non-producible: name doesn't match any output or elementField in the graph.
		var nonProducibleInputs []string
		for inputName := range autowireInputs {
			if !producible[inputName] {
				nonProducibleInputs = append(nonProducibleInputs, inputName)
				delete(autowireInputs, inputName)
			}
		}
		if len(nonProducibleInputs) > 0 {
			sort.Strings(nonProducibleInputs)
			result.NonProducible = append(result.NonProducible, WorkflowNonProducible{
				Addon:  addon.Name,
				Inputs: nonProducibleInputs,
			})
		}

		if len(autowireInputs) == 0 {
			continue
		}

		// Check each base workflow for compatibility.
		for _, base := range bases {
			basePlan := templateCache[base.Template]
			if basePlan == nil {
				continue // template failed to load
			}

			// Check if any of this addon's After nodes exist in the base plan.
			if addon.After.IsSet() {
				found := false
				for _, afterNode := range addon.After {
					if findStepByNode(basePlan, afterNode) != "" {
						found = true
						break
					}
				}
				if !found {
					continue // addon not compatible with this base
				}
			}

			// Build output map from base plan.
			outputMap := buildOutputMap(basePlan, g)

			// Check which structural AUTOWIRE inputs are not satisfied.
			var unfed []string
			for inputName := range autowireInputs {
				if _, found := outputMap[inputName]; !found {
					unfed = append(unfed, inputName)
				}
			}

			if len(unfed) > 0 {
				sort.Strings(unfed)
				result.Warnings = append(result.Warnings, WorkflowCompatWarning{
					Addon:        addon.Name,
					BaseWorkflow: base.Name,
					UnfedInputs:  unfed,
				})
			}
		}
	}

	return result
}

// buildProducibleNames returns the set of all names that the graph can
// produce: output names and elementField names across all nodes. An
// AUTOWIRE input is considered "structural" (expected to come from upstream)
// only if its name appears in this set.
func buildProducibleNames(g *graph.Graph) map[string]bool {
	names := make(map[string]bool)
	for _, node := range g.Nodes {
		for _, out := range node.Outputs {
			names[out.Name] = true
			for _, ef := range out.ElementFields {
				names[ef.Name] = true
			}
		}
	}
	return names
}

// collectAutowireInputs scans all steps in a plan and returns a set of
// input names that have AUTOWIRE placeholder values.
func collectAutowireInputs(p *plan.Plan) map[string]bool {
	inputs := make(map[string]bool)
	for _, step := range p.Execution.Steps {
		for inputName, sv := range step.Values {
			if isPlaceholder(sv) {
				inputs[inputName] = true
			}
		}
	}
	return inputs
}
