package intent

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// ComposeWorkflowTemplate loads a parent workflow template and splices in
// sub-workflow templates specified by the workflow's Includes. Each include
// is loaded, its step IDs are prefixed (inc0_, inc1_, ...) to avoid
// collisions, and PLACEHOLDERs are resolved via auto-wiring from parent
// step outputs or explicit Wire overrides.
//
// The result is a standard *plan.Plan that requires no engine changes.
func ComposeWorkflowTemplate(wf graph.Workflow, graphDir string, g *graph.Graph) (*plan.Plan, error) {
	if wf.Template == "" {
		return nil, fmt.Errorf("compose: workflow %q has no template", wf.Name)
	}

	parent, err := LoadWorkflowTemplate(wf.Template, graphDir, g)
	if err != nil {
		return nil, fmt.Errorf("compose: loading parent template: %w", err)
	}

	if len(wf.Includes) == 0 {
		return parent, nil
	}

	// Process each include in order.
	for i, inc := range wf.Includes {
		prefix := fmt.Sprintf("inc%d_", i)

		// Look up the sub-workflow.
		subWF, found := findWorkflowByName(g, inc.Workflow)
		if !found {
			return nil, fmt.Errorf("compose: include %d references unknown workflow %q", i, inc.Workflow)
		}
		if subWF.Template == "" {
			return nil, fmt.Errorf("compose: included workflow %q has no template", inc.Workflow)
		}

		sub, err := LoadWorkflowTemplate(subWF.Template, graphDir, g)
		if err != nil {
			return nil, fmt.Errorf("compose: loading sub-workflow template %q: %w", inc.Workflow, err)
		}

		// Build output map from parent steps (up to and including insertion point).
		outputMap := buildOutputMap(parent, g, inc.After)

		// Prefix sub-workflow step IDs and rewrite internal references.
		prefixStepRefs(sub, prefix)

		// Auto-wire PLACEHOLDERs in sub-workflow steps.
		autoWirePlaceholders(sub, outputMap, inc.Wire, g)

		// Add insertion-point dependency to sub-workflow root steps.
		addInsertionDeps(sub, inc.After)

		// Ensure all from-referenced parent steps are in dependsOn.
		fixFromDependencies(sub)

		// Splice sub-workflow steps into parent after insertion point.
		spliceSteps(parent, sub, inc.After)

		// Merge cleanup (dedup by node name).
		mergeCleanup(parent, sub)
	}

	return parent, nil
}

// findWorkflowByName looks up a workflow by name (case-insensitive).
func findWorkflowByName(g *graph.Graph, name string) (graph.Workflow, bool) {
	for _, wf := range g.Workflows {
		if strings.EqualFold(wf.Name, name) {
			return wf, true
		}
	}
	return graph.Workflow{}, false
}

// buildOutputMap scans parent steps up to (and including) the insertion point
// and builds a map of outputName → "stepID.outputName" for all outputs
// produced by those steps. Last producer wins.
func buildOutputMap(p *plan.Plan, g *graph.Graph, afterStep string) map[string]string {
	outputMap := make(map[string]string)
	for _, step := range p.Execution.Steps {
		stepID := step.StepID()
		node := g.Nodes[step.Node]
		if node != nil {
			for _, out := range node.Outputs {
				outputMap[out.Name] = stepID + "." + out.Name
			}
		}
		if stepID == afterStep {
			break
		}
	}
	return outputMap
}

// prefixStepRefs prefixes all step IDs, dependsOn, from refs, and
// fromSelection refs within a sub-workflow plan.
func prefixStepRefs(sub *plan.Plan, prefix string) {
	// Build old→new ID map for the sub-workflow steps.
	idMap := make(map[string]string)
	for _, step := range sub.Execution.Steps {
		oldID := step.StepID()
		idMap[oldID] = prefix + oldID
	}

	for i := range sub.Execution.Steps {
		step := &sub.Execution.Steps[i]

		// Prefix the step ID.
		if step.ID != "" {
			step.ID = prefix + step.ID
		} else {
			step.ID = prefix + step.Node
		}

		// Rewrite dependsOn.
		for j, dep := range step.DependsOn {
			if newDep, ok := idMap[dep]; ok {
				step.DependsOn[j] = newDep
			}
		}

		// Rewrite from refs in values.
		for name, sv := range step.Values {
			if sv.From != "" {
				fromStep := splitNodeName(sv.From)
				if newID, ok := idMap[fromStep]; ok {
					sv.From = newID + sv.From[len(fromStep):]
					step.Values[name] = sv
				}
			}
			if sv.Select != nil && sv.From != "" {
				// already handled above
			}
		}

		// Rewrite named selection from refs.
		for selName, sel := range step.Selections {
			if sel.From != "" {
				fromStep := splitNodeName(sel.From)
				if newID, ok := idMap[fromStep]; ok {
					sel.From = newID + sel.From[len(fromStep):]
					step.Selections[selName] = sel
				}
			}
		}
	}

	// Prefix cleanup nodes are not prefixed — they reference graph node names.
}

// autoWirePlaceholders resolves PLACEHOLDER values in the sub-workflow.
// For each step input with Default == "PLACEHOLDER":
//  1. Check explicit Wire map — if the input name is in Wire, use that ref.
//  2. If Wire[name] == "MANUAL", clear the value (LLM fills it).
//  3. Otherwise, scan outputMap for a matching output name. Last producer wins.
//  4. If no match found, leave PLACEHOLDER (LLM or user must fill it).
func autoWirePlaceholders(sub *plan.Plan, outputMap map[string]string, wire map[string]string, g *graph.Graph) {
	for i := range sub.Execution.Steps {
		step := &sub.Execution.Steps[i]
		if step.Values == nil {
			continue
		}
		for inputName, sv := range step.Values {
			if !isPlaceholder(sv) {
				continue
			}

			// Check explicit wire overrides.
			if wireRef, hasWire := wire[inputName]; hasWire {
				if wireRef == "MANUAL" {
					// Clear the value entirely — leave for LLM.
					sv.Default = nil
					step.Values[inputName] = sv
					continue
				}
				// Wire to explicit ref.
				sv.Default = nil
				sv.From = wireRef
				step.Values[inputName] = sv
				continue
			}

			// Auto-wire: find matching output in parent steps.
			if ref, found := outputMap[inputName]; found {
				sv.Default = nil
				sv.From = ref
				step.Values[inputName] = sv
				continue
			}

			// No match — leave placeholder. Will show up as unfed input.
		}
	}
}

// isPlaceholder returns true if the StepValue is a bare PLACEHOLDER string.
func isPlaceholder(sv plan.StepValue) bool {
	s, ok := sv.Default.(string)
	return ok && s == "PLACEHOLDER"
}

// addInsertionDeps adds the insertion point step as a dependency to all
// sub-workflow root steps (steps that have no dependencies or whose
// dependencies are all within the sub-workflow).
func addInsertionDeps(sub *plan.Plan, afterStep string) {
	if afterStep == "" {
		return
	}

	// Collect sub-workflow step IDs.
	subStepIDs := make(map[string]bool)
	for _, step := range sub.Execution.Steps {
		subStepIDs[step.StepID()] = true
	}

	for i := range sub.Execution.Steps {
		step := &sub.Execution.Steps[i]
		// A root step has no deps, or all deps are outside the sub-workflow.
		isRoot := true
		for _, dep := range step.DependsOn {
			if subStepIDs[dep] {
				isRoot = false
				break
			}
		}
		if isRoot {
			step.DependsOn = append(step.DependsOn, afterStep)
		}
	}
}

// spliceSteps inserts sub-workflow steps into the parent plan right after
// the insertion point step.
func spliceSteps(parent, sub *plan.Plan, afterStep string) {
	if len(sub.Execution.Steps) == 0 {
		return
	}

	insertIdx := len(parent.Execution.Steps) // default: append at end
	for i, step := range parent.Execution.Steps {
		if step.StepID() == afterStep {
			insertIdx = i + 1
			break
		}
	}

	// Splice: left + sub + right
	newSteps := make([]plan.Step, 0, len(parent.Execution.Steps)+len(sub.Execution.Steps))
	newSteps = append(newSteps, parent.Execution.Steps[:insertIdx]...)
	newSteps = append(newSteps, sub.Execution.Steps...)
	newSteps = append(newSteps, parent.Execution.Steps[insertIdx:]...)
	parent.Execution.Steps = newSteps
}

// mergeCleanup appends sub-workflow cleanup steps to parent, deduplicating
// by node name (parent cleanup takes precedence).
func mergeCleanup(parent, sub *plan.Plan) {
	if len(sub.Execution.Cleanup) == 0 {
		return
	}

	existing := make(map[string]bool)
	for _, cs := range parent.Execution.Cleanup {
		existing[cs.Node] = true
	}

	for _, cs := range sub.Execution.Cleanup {
		if !existing[cs.Node] {
			parent.Execution.Cleanup = append(parent.Execution.Cleanup, cs)
			existing[cs.Node] = true
		}
	}
}

// fixFromDependencies scans all from references in sub-workflow step values
// and selections. If a from reference points to a step outside the sub-workflow
// (i.e., a parent step), that step is added to dependsOn if not already present.
func fixFromDependencies(sub *plan.Plan) {
	// Collect sub-workflow step IDs.
	subStepIDs := make(map[string]bool)
	for _, step := range sub.Execution.Steps {
		subStepIDs[step.StepID()] = true
	}

	for i := range sub.Execution.Steps {
		step := &sub.Execution.Steps[i]

		depSet := make(map[string]bool)
		for _, dep := range step.DependsOn {
			depSet[dep] = true
		}

		// Collect all from-referenced step IDs.
		var refs []string
		for _, sv := range step.Values {
			if sv.From != "" {
				refs = append(refs, splitNodeName(sv.From))
			}
		}
		for _, sel := range step.Selections {
			if sel.From != "" {
				refs = append(refs, splitNodeName(sel.From))
			}
		}

		// Add missing parent step dependencies.
		for _, ref := range refs {
			if !subStepIDs[ref] && !depSet[ref] {
				step.DependsOn = append(step.DependsOn, ref)
				depSet[ref] = true
			}
		}
	}
}

// FindComposedWorkflow looks up a pre-declared composed workflow in the graph
// that matches the given base workflow name plus requested addon names.
// Returns the workflow and true if an exact match is found.
func FindComposedWorkflow(g *graph.Graph, baseWorkflow string, addons []string) (graph.Workflow, bool) {
	if len(addons) == 0 {
		return graph.Workflow{}, false
	}

	addonSet := make(map[string]bool)
	for _, a := range addons {
		addonSet[strings.ToLower(a)] = true
	}

	for _, wf := range g.Workflows {
		if wf.Kind == "addon" || len(wf.Includes) == 0 {
			continue
		}
		// Check if this workflow's includes match the requested addons exactly.
		if len(wf.Includes) != len(addons) {
			continue
		}
		match := true
		for _, inc := range wf.Includes {
			if !addonSet[strings.ToLower(inc.Workflow)] {
				match = false
				break
			}
		}
		if match {
			return wf, true
		}
	}
	return graph.Workflow{}, false
}

// BuildSyntheticWorkflow creates a Workflow struct for dynamic composition
// when no pre-declared composed workflow exists. It takes a base workflow
// and a list of addon names and creates includes with auto-detected
// insertion points.
func BuildSyntheticWorkflow(g *graph.Graph, base graph.Workflow, addons []string) graph.Workflow {
	wf := graph.Workflow{
		Name:     base.Name + " (composed)",
		Template: base.Template,
		Steps:    base.Steps,
	}

	for _, addonName := range addons {
		inc := graph.WorkflowInclude{
			Workflow: addonName,
		}

		// Auto-detect insertion point: use the last non-cleanup, non-goal
		// step before the commit step. Heuristic: second-to-last step.
		if len(base.Steps) >= 2 {
			inc.After = base.Steps[len(base.Steps)-2]
		} else if len(base.Steps) > 0 {
			inc.After = base.Steps[len(base.Steps)-1]
		}

		wf.Includes = append(wf.Includes, inc)
	}

	return wf
}

// ResolveWorkflowDir returns the absolute directory containing the graph file,
// used for resolving template paths.
func ResolveWorkflowDir(graphDir string) string {
	abs, err := filepath.Abs(graphDir)
	if err != nil {
		return graphDir
	}
	return abs
}
