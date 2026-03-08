package intent

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// ComposeRequest holds the parameters for composing a workflow template.
type ComposeRequest struct {
	Base     graph.Workflow
	Choices  map[string]string // slot name → option name (nil = use defaults)
	Addons   []string          // addon workflow names
	Graph    *graph.Graph
	GraphDir string
}

// Compose loads a base workflow template and applies slot fills and/or addon
// composition as specified in the request. This is the single entry point for
// all workflow composition.
func Compose(req ComposeRequest) (*plan.Plan, error) {
	if req.Base.Template == "" {
		return nil, fmt.Errorf("compose: workflow %q has no template", req.Base.Name)
	}

	parent, err := LoadWorkflowTemplate(req.Base.Template, req.GraphDir, req.Graph)
	if err != nil {
		return nil, fmt.Errorf("compose: loading template: %w", err)
	}

	// Fill slots if the base workflow has them.
	if len(req.Base.Slots) > 0 {
		if err := fillSlots(parent, req.Base, req.Choices, req.Graph, req.GraphDir); err != nil {
			return nil, err
		}
	}

	// Apply addons if requested.
	if len(req.Addons) > 0 {
		addons, err := resolveAddons(req.Addons, req.Graph)
		if err != nil {
			return nil, err
		}
		if err := composeAddonList(parent, addons, req.GraphDir, req.Graph); err != nil {
			return nil, err
		}
	}

	return parent, nil
}

// resolveAddons looks up addon workflows by name, validates they are
// proper addons with after fields, and returns the resolved workflows.
func resolveAddons(names []string, g *graph.Graph) ([]graph.Workflow, error) {
	var addons []graph.Workflow
	for _, name := range names {
		addon, found := findWorkflowByName(g, name)
		if !found {
			return nil, fmt.Errorf("compose: unknown addon workflow %q", name)
		}
		if !addon.IsAddon() {
			return nil, fmt.Errorf("compose: workflow %q is not an addon", name)
		}
		if !addon.After.IsSet() {
			return nil, fmt.Errorf("compose: addon %q has no after field", name)
		}
		addons = append(addons, addon)
	}
	return addons, nil
}

// findStepByNode returns the step ID of the last step in the plan whose
// Node field matches nodeName, or empty string if not found. Returning the
// last match ensures that addons splice after the final instance when
// multiple steps share a node (e.g., two addTraveler steps from a slot).
func findStepByNode(p *plan.Plan, nodeName string) string {
	last := ""
	for _, step := range p.Execution.Steps {
		if step.Node == nodeName {
			last = step.StepID()
		}
	}
	return last
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

// buildOutputMap scans all parent steps and builds a map of
// outputName → "stepID.outputName" for all outputs produced by those steps.
// Last producer wins. All parent outputs are available for auto-wiring
// because the engine resolves execution order via dependsOn, not step list
// position. ensureFromDeps ensures the correct dependencies are added.
func buildOutputMap(p *plan.Plan, g *graph.Graph) map[string]string {
	outputMap := make(map[string]string)
	for _, step := range p.Execution.Steps {
		stepID := step.StepID()
		node := g.Nodes[step.Node]
		if node != nil {
			for _, out := range node.Outputs {
				outputMap[out.Name] = stepID + "." + out.Name
			}
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
			if sv.FromInput != "" {
				fromStep := splitNodeName(sv.FromInput)
				if newID, ok := idMap[fromStep]; ok {
					sv.FromInput = newID + sv.FromInput[len(fromStep):]
					step.Values[name] = sv
				}
			}
			// sv.Select != nil && sv.From != "" — already handled above
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

// autoWirePlaceholders resolves AUTOWIRE values in the sub-workflow.
// For each step input with Default == "AUTOWIRE":
//  1. Check explicit Wire map — if the input name is in Wire, use that ref.
//  2. If Wire[name] == "MANUAL", clear the value (LLM fills it).
//  3. Otherwise, scan outputMap for a matching output name. Last producer wins.
//  4. If no match found, leave AUTOWIRE (LLM or user must fill it).
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

			// No match — leave AUTOWIRE marker. Will show up as unfed input.
		}
	}
}

// isPlaceholder returns true if the StepValue is an AUTOWIRE marker string.
// Also accepts legacy "PLACEHOLDER" for backward compatibility.
func isPlaceholder(sv plan.StepValue) bool {
	s, ok := sv.Default.(string)
	return ok && (s == "AUTOWIRE" || s == "PLACEHOLDER")
}

// isRootStep returns true if the step has no dependencies within the given
// step ID set. A root step either has no deps at all or all its deps point
// outside the set.
func isRootStep(step plan.Step, withinSet map[string]bool) bool {
	for _, dep := range step.DependsOn {
		if withinSet[dep] {
			return false
		}
	}
	return true
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
		if isRootStep(*step, subStepIDs) {
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

// ensureFromDeps scans all from references in the plan and ensures that
// referenced steps are in the referencing step's dependsOn.
//
// When externalOnly is true, only references pointing OUTSIDE the plan's step
// set are added (used per-addon before splice). When false, all valid from
// references are added (used after slot composition on the flat plan).
func ensureFromDeps(p *plan.Plan, externalOnly bool) {
	// Build step ID set.
	stepIDs := make(map[string]bool, len(p.Execution.Steps))
	for _, step := range p.Execution.Steps {
		stepIDs[step.StepID()] = true
	}

	for i := range p.Execution.Steps {
		step := &p.Execution.Steps[i]

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
			if sv.FromInput != "" {
				refs = append(refs, splitNodeName(sv.FromInput))
			}
		}
		for _, sel := range step.Selections {
			if sel.From != "" {
				refs = append(refs, splitNodeName(sel.From))
			}
		}

		sid := step.StepID()
		for _, ref := range refs {
			if ref == sid || depSet[ref] {
				continue
			}
			if externalOnly {
				// Only add deps for refs pointing outside the plan.
				if !stepIDs[ref] {
					step.DependsOn = append(step.DependsOn, ref)
					depSet[ref] = true
				}
			} else {
				// Add deps for all valid refs within the plan.
				if stepIDs[ref] {
					step.DependsOn = append(step.DependsOn, ref)
					depSet[ref] = true
				}
			}
		}
	}
}

// resolveAfterWire creates a copy of the wire map with $after. prefixes replaced
// by the actual step ID. For example, if afterStep is "addTraveler_2"
// and wire has {"travelerId": "$after.travelerIdentifierValue"}, the result
// will have {"travelerId": "addTraveler_2.travelerIdentifierValue"}.
// Non-$after entries are passed through unchanged.
func resolveAfterWire(wire map[string]string, afterStep string) map[string]string {
	if len(wire) == 0 {
		return wire
	}
	resolved := make(map[string]string, len(wire))
	for k, v := range wire {
		if strings.HasPrefix(v, "$after.") {
			resolved[k] = afterStep + v[len("$after"):]
		} else {
			resolved[k] = v
		}
	}
	return resolved
}

// fillSlots fills slot marker steps in the parent plan with the chosen slot
// option templates. For each slot marker:
//  1. Look up the chosen option (from choices map, falling back to slot default)
//  2. Load the option's template
//  3. Replace the marker step with the option's steps (unprefixed)
//  4. Rewrite dependsOn: references to the slot name → last step of the option
//
// After all slots are filled, AUTOWIRE resolution and ensureFromDeps run
// across the full plan. Modifies parent in-place.
func fillSlots(parent *plan.Plan, base graph.Workflow, choices map[string]string, g *graph.Graph, graphDir string) error {
	if len(base.Slots) == 0 {
		return nil
	}

	// Build slot definitions map for lookup.
	slotDefs := make(map[string]graph.SlotDef, len(base.Slots))
	for _, sd := range base.Slots {
		slotDefs[sd.Name] = sd
	}

	// Track slot name → last step ID of the option (for dependsOn rewriting).
	slotLastStep := make(map[string]string)

	// Process slot markers in order. We rebuild the step list each iteration
	// to handle index shifts from replacement.
	for slotName, sd := range slotDefs {
		// Determine chosen option.
		optionName := sd.Default
		if chosen, ok := choices[slotName]; ok && chosen != "" {
			optionName = chosen
		}
		if optionName == "" {
			return fmt.Errorf("compose: slot %q has no choice and no default", slotName)
		}

		// Look up the option workflow.
		optionWF, found := findWorkflowByName(g, optionName)
		if !found {
			return fmt.Errorf("compose: slot %q option %q not found in graph", slotName, optionName)
		}
		if optionWF.Template == "" {
			return fmt.Errorf("compose: slot %q option %q has no template", slotName, optionName)
		}

		// Load the option template.
		optionPlan, loadErr := LoadWorkflowTemplate(optionWF.Template, graphDir, g)
		if loadErr != nil {
			return fmt.Errorf("compose: loading slot %q option %q: %w", slotName, optionName, loadErr)
		}

		if len(optionPlan.Execution.Steps) == 0 {
			return fmt.Errorf("compose: slot %q option %q has no steps", slotName, optionName)
		}

		// Find the slot marker in the parent plan.
		markerIdx := -1
		var markerDeps []string
		for i, step := range parent.Execution.Steps {
			if step.IsSlotMarker() && step.Slot == slotName {
				markerIdx = i
				markerDeps = step.DependsOn
				break
			}
		}
		if markerIdx < 0 {
			return fmt.Errorf("compose: slot %q marker not found in base template", slotName)
		}

		// Replace the marker with option steps.
		// Option steps are NOT prefixed — only one option fills each slot.
		newSteps := make([]plan.Step, 0, len(parent.Execution.Steps)-1+len(optionPlan.Execution.Steps))
		newSteps = append(newSteps, parent.Execution.Steps[:markerIdx]...)
		newSteps = append(newSteps, optionPlan.Execution.Steps...)
		newSteps = append(newSteps, parent.Execution.Steps[markerIdx+1:]...)
		parent.Execution.Steps = newSteps

		// Add the marker's dependsOn to root steps of the option.
		optionStepIDs := make(map[string]bool, len(optionPlan.Execution.Steps))
		for _, s := range optionPlan.Execution.Steps {
			optionStepIDs[s.StepID()] = true
		}
		for i := markerIdx; i < markerIdx+len(optionPlan.Execution.Steps); i++ {
			step := &parent.Execution.Steps[i]
			if isRootStep(*step, optionStepIDs) && len(markerDeps) > 0 {
				step.DependsOn = append(step.DependsOn, markerDeps...)
			}
		}

		// Record the last step of this slot option for dependsOn rewriting.
		lastOptionStep := optionPlan.Execution.Steps[len(optionPlan.Execution.Steps)-1].StepID()
		slotLastStep[slotName] = lastOptionStep

		// Merge cleanup from option.
		mergeCleanup(parent, optionPlan)
	}

	// Rewrite dependsOn references: any step depending on a slot name gets
	// rewritten to that slot's last step.
	for i := range parent.Execution.Steps {
		step := &parent.Execution.Steps[i]
		for j, dep := range step.DependsOn {
			if lastStep, ok := slotLastStep[dep]; ok {
				step.DependsOn[j] = lastStep
			}
		}
	}

	// Apply inject values from slot options.
	for slotName, sd := range slotDefs {
		optionName := sd.Default
		if chosen, ok := choices[slotName]; ok && chosen != "" {
			optionName = chosen
		}
		optionWF, found := findWorkflowByName(g, optionName)
		if !found || len(optionWF.Inject) == 0 {
			continue
		}
		applyInjectValues(parent, optionWF.Inject, g)
	}

	// Run AUTOWIRE resolution across the full plan.
	outputMap := buildOutputMap(parent, g)
	autoWirePlaceholders(parent, outputMap, nil, g)

	// Ensure all from-referenced steps are in dependsOn.
	ensureFromDeps(parent, false)

	return nil
}

// composeAddonList is the shared implementation for addon composition. It sorts
// addons by priority, then processes each addon: loading its template, prefixing
// step IDs, auto-wiring, and splicing into the parent plan.
func composeAddonList(parent *plan.Plan, addons []graph.Workflow, graphDir string, g *graph.Graph) error {
	// Sort addons by priority (stable sort preserves original order for equal priorities).
	sorted := make([]graph.Workflow, len(addons))
	copy(sorted, addons)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	addons = sorted

	// Track last step per after-node for auto-chaining.
	lastStepByAfter := make(map[string]string)

	for i, addon := range addons {
		prefix := fmt.Sprintf("inc%d_", i)

		if addon.Template == "" {
			return fmt.Errorf("compose: addon workflow %q has no template", addon.Name)
		}

		// Find the insertion point.
		var afterStep string
		var matchedAfter string
		for _, afterNode := range addon.After {
			if lastStep, ok := lastStepByAfter[afterNode]; ok {
				afterStep = lastStep
				matchedAfter = afterNode
				break
			}
			if step := findStepByNode(parent, afterNode); step != "" {
				afterStep = step
				matchedAfter = afterNode
				break
			}
		}
		if afterStep == "" {
			return fmt.Errorf("compose: addon %q: none of after nodes %v found in base plan steps", addon.Name, []string(addon.After))
		}

		sub, err := LoadWorkflowTemplate(addon.Template, graphDir, g)
		if err != nil {
			return fmt.Errorf("compose: loading addon template %q: %w", addon.Name, err)
		}

		outputMap := buildOutputMap(parent, g)
		prefixStepRefs(sub, prefix)
		resolvedWire := resolveAfterWire(addon.Wire, afterStep)
		autoWirePlaceholders(sub, outputMap, resolvedWire, g)
		addInsertionDeps(sub, afterStep)
		ensureFromDeps(sub, true)
		spliceSteps(parent, sub, afterStep)
		mergeCleanup(parent, sub)

		lastAddonStep := sub.Execution.Steps[len(sub.Execution.Steps)-1].StepID()
		lastStepByAfter[matchedAfter] = lastAddonStep
	}

	return nil
}

// applyInjectValues sets default values on steps whose graph node has a matching
// input. This is used by slot options to propagate values across the composed
// plan (e.g., a two-traveler slot sets passengers=2 on search steps).
// Steps that already have an explicit value for the input are skipped.
func applyInjectValues(p *plan.Plan, inject map[string]any, g *graph.Graph) {
	for inputName, value := range inject {
		for i := range p.Execution.Steps {
			step := &p.Execution.Steps[i]
			node := g.Nodes[step.Node]
			if node == nil {
				continue
			}
			// Check if this node has the input.
			hasInput := false
			for _, inp := range node.Inputs {
				if inp.Name == inputName {
					hasInput = true
					break
				}
			}
			if !hasInput {
				continue
			}
			// Skip if the step already has an explicit value.
			if sv, exists := step.Values[inputName]; exists {
				if sv.Default != nil || sv.From != "" || sv.FromSelection != "" || sv.Select != nil || sv.FromResolved != "" || sv.Locked {
					continue
				}
			}
			// Set the injected value.
			if step.Values == nil {
				step.Values = make(map[string]plan.StepValue)
			}
			step.Values[inputName] = plan.StepValue{Default: value}
		}
	}
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
