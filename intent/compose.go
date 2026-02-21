package intent

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// ComposeWorkflowTemplate loads a base workflow template and splices in
// addon workflow templates. Each addon uses its own After field to find
// the insertion point (the step in the base plan whose Node matches
// addon.After). Addon step IDs are prefixed (inc0_, inc1_, ...) to avoid
// collisions, and AUTOWIRE values are resolved via auto-wiring from parent
// step outputs or explicit Wire overrides from the addon definition.
//
// The result is a standard *plan.Plan that requires no engine changes.
func ComposeWorkflowTemplate(base graph.Workflow, addons []graph.Workflow, graphDir string, g *graph.Graph) (*plan.Plan, error) {
	if base.Template == "" {
		return nil, fmt.Errorf("compose: workflow %q has no template", base.Name)
	}

	parent, err := LoadWorkflowTemplate(base.Template, graphDir, g)
	if err != nil {
		return nil, fmt.Errorf("compose: loading parent template: %w", err)
	}

	if len(addons) == 0 {
		return parent, nil
	}

	// Sort addons by priority (stable sort preserves original order for equal priorities).
	// Copy slice to avoid mutating the caller's slice.
	sorted := make([]graph.Workflow, len(addons))
	copy(sorted, addons)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	addons = sorted

	// Track last step per after-node for auto-chaining. When multiple addons
	// share the same insertion point, subsequent addons chain after the last
	// step of the previous addon to ensure sequential execution.
	lastStepByAfter := make(map[string]string)

	// Process each addon in order.
	for i, addon := range addons {
		prefix := fmt.Sprintf("inc%d_", i)

		if addon.Template == "" {
			return nil, fmt.Errorf("compose: addon workflow %q has no template", addon.Name)
		}

		// Find the insertion point. Try each After value; use first match.
		// If a previous addon used the same after node, chain after that
		// addon's last step instead.
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
			return nil, fmt.Errorf("compose: addon %q: none of after nodes %v found in base plan steps", addon.Name, []string(addon.After))
		}

		sub, err := LoadWorkflowTemplate(addon.Template, graphDir, g)
		if err != nil {
			return nil, fmt.Errorf("compose: loading addon template %q: %w", addon.Name, err)
		}

		// Build output map from all parent steps.
		outputMap := buildOutputMap(parent, g)

		// Prefix sub-workflow step IDs and rewrite internal references.
		prefixStepRefs(sub, prefix)

		// Resolve $after. wire references to the matched node name.
		resolvedWire := resolveAfterWire(addon.Wire, matchedAfter)

		// Auto-wire AUTOWIRE values in sub-workflow steps using addon's Wire.
		autoWirePlaceholders(sub, outputMap, resolvedWire, g)

		// Add insertion-point dependency to sub-workflow root steps.
		addInsertionDeps(sub, afterStep)

		// Ensure all from-referenced parent steps are in dependsOn.
		fixFromDependencies(sub)

		// Splice sub-workflow steps into parent after insertion point.
		spliceSteps(parent, sub, afterStep)

		// Merge cleanup (dedup by node name).
		mergeCleanup(parent, sub)

		// Record the last step of this addon for auto-chaining.
		lastAddonStep := sub.Execution.Steps[len(sub.Execution.Steps)-1].StepID()
		lastStepByAfter[matchedAfter] = lastAddonStep
	}

	return parent, nil
}

// ComposeWithAddons looks up addon workflows by name, validates them, and
// delegates to ComposeWorkflowTemplate.
func ComposeWithAddons(base graph.Workflow, addonNames []string, g *graph.Graph, graphDir string) (*plan.Plan, error) {
	var addons []graph.Workflow
	for _, name := range addonNames {
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
	return ComposeWorkflowTemplate(base, addons, graphDir, g)
}

// findStepByNode returns the step ID of the first step in the plan whose
// Node field matches nodeName, or empty string if not found.
func findStepByNode(p *plan.Plan, nodeName string) string {
	for _, step := range p.Execution.Steps {
		if step.Node == nodeName {
			return step.StepID()
		}
	}
	return ""
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
// position. fixFromDependencies ensures the correct dependencies are added.
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

// ensureFromDeps scans all from references in the plan and ensures that
// referenced steps are in the referencing step's dependsOn. Unlike
// fixFromDependencies (which only adds deps for external refs), this adds
// deps for ALL from references — needed after slot composition where all
// steps are in the same flat plan.
func ensureFromDeps(p *plan.Plan) {
	// Build step ID set for validation.
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
		}
		for _, sel := range step.Selections {
			if sel.From != "" {
				refs = append(refs, splitNodeName(sel.From))
			}
		}

		// Add missing dependencies — self-refs are excluded.
		sid := step.StepID()
		for _, ref := range refs {
			if ref != sid && !depSet[ref] && stepIDs[ref] {
				step.DependsOn = append(step.DependsOn, ref)
				depSet[ref] = true
			}
		}
	}
}

// resolveAfterWire creates a copy of the wire map with $after. prefixes replaced
// by the actual matched node name. For example, if matchedAfter is "priceOfferReference"
// and wire has {"offerListIdentifier": "$after.offerIdentifierValue"}, the result
// will have {"offerListIdentifier": "priceOfferReference.offerIdentifierValue"}.
// Non-$after entries are passed through unchanged.
func resolveAfterWire(wire map[string]string, matchedAfter string) map[string]string {
	if len(wire) == 0 {
		return wire
	}
	resolved := make(map[string]string, len(wire))
	for k, v := range wire {
		if strings.HasPrefix(v, "$after.") {
			resolved[k] = matchedAfter + v[len("$after"):]
		} else {
			resolved[k] = v
		}
	}
	return resolved
}

// ComposeWithSlots fills slot marker steps in a base workflow template with
// the chosen slot option templates. For each slot marker in the base plan:
//  1. Look up the chosen option (from choices map, falling back to slot default)
//  2. Load the option's template
//  3. Replace the marker step with the option's steps (unprefixed)
//  4. Rewrite dependsOn: references to the slot name → last step of the option
//
// After all slots are filled, AUTOWIRE resolution and fixFromDependencies run
// across the full plan. The result can then be passed to ComposeWithAddons
// for addon composition.
func ComposeWithSlots(base graph.Workflow, choices map[string]string, g *graph.Graph, graphDir string) (*plan.Plan, error) {
	if base.Template == "" {
		return nil, fmt.Errorf("compose: workflow %q has no template", base.Name)
	}

	parent, err := LoadWorkflowTemplate(base.Template, graphDir, g)
	if err != nil {
		return nil, fmt.Errorf("compose: loading base template: %w", err)
	}

	if len(base.Slots) == 0 {
		return parent, nil
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
			return nil, fmt.Errorf("compose: slot %q has no choice and no default", slotName)
		}

		// Look up the option workflow.
		optionWF, found := findWorkflowByName(g, optionName)
		if !found {
			return nil, fmt.Errorf("compose: slot %q option %q not found in graph", slotName, optionName)
		}
		if optionWF.Template == "" {
			return nil, fmt.Errorf("compose: slot %q option %q has no template", slotName, optionName)
		}

		// Load the option template.
		optionPlan, loadErr := LoadWorkflowTemplate(optionWF.Template, graphDir, g)
		if loadErr != nil {
			return nil, fmt.Errorf("compose: loading slot %q option %q: %w", slotName, optionName, loadErr)
		}

		if len(optionPlan.Execution.Steps) == 0 {
			return nil, fmt.Errorf("compose: slot %q option %q has no steps", slotName, optionName)
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
			return nil, fmt.Errorf("compose: slot %q marker not found in base template", slotName)
		}

		// Replace the marker with option steps.
		// Option steps are NOT prefixed — only one option fills each slot.
		newSteps := make([]plan.Step, 0, len(parent.Execution.Steps)-1+len(optionPlan.Execution.Steps))
		newSteps = append(newSteps, parent.Execution.Steps[:markerIdx]...)
		newSteps = append(newSteps, optionPlan.Execution.Steps...)
		newSteps = append(newSteps, parent.Execution.Steps[markerIdx+1:]...)
		parent.Execution.Steps = newSteps

		// Add the marker's dependsOn to root steps of the option.
		// A root step is one whose deps are all within the option itself.
		optionStepIDs := make(map[string]bool, len(optionPlan.Execution.Steps))
		for _, s := range optionPlan.Execution.Steps {
			optionStepIDs[s.StepID()] = true
		}
		for i := markerIdx; i < markerIdx+len(optionPlan.Execution.Steps); i++ {
			step := &parent.Execution.Steps[i]
			isRoot := true
			for _, dep := range step.DependsOn {
				if optionStepIDs[dep] {
					isRoot = false
					break
				}
			}
			if isRoot && len(markerDeps) > 0 {
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

	// Run AUTOWIRE resolution across the full plan.
	outputMap := buildOutputMap(parent, g)
	autoWirePlaceholders(parent, outputMap, nil, g)

	// Ensure all from-referenced steps are in dependsOn.
	// Use ensureFromDeps (not fixFromDependencies) because the full plan
	// is one flat step list — fixFromDependencies only adds deps for
	// steps outside the plan.
	ensureFromDeps(parent)

	return parent, nil
}

// ComposeWithSlotsAndAddons composes a workflow with both slot choices and addons.
// It first fills slots, then delegates to ComposeWithAddons for addon composition.
func ComposeWithSlotsAndAddons(base graph.Workflow, choices map[string]string, addonNames []string, g *graph.Graph, graphDir string) (*plan.Plan, error) {
	slotPlan, err := ComposeWithSlots(base, choices, g, graphDir)
	if err != nil {
		return nil, err
	}

	if len(addonNames) == 0 {
		return slotPlan, nil
	}

	// Build addon list.
	var addons []graph.Workflow
	for _, name := range addonNames {
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

	// Use ComposeWorkflowTemplate with the slot-filled plan as the parent.
	// We need to pass the slotPlan directly instead of loading from template.
	return composeAddonsOnPlan(slotPlan, base, addons, graphDir, g)
}

// composeAddonsOnPlan applies addon composition to an already-loaded plan.
// This is used when slot composition has already produced the base plan.
func composeAddonsOnPlan(parent *plan.Plan, base graph.Workflow, addons []graph.Workflow, graphDir string, g *graph.Graph) (*plan.Plan, error) {
	if len(addons) == 0 {
		return parent, nil
	}

	// Sort addons by priority.
	sorted := make([]graph.Workflow, len(addons))
	copy(sorted, addons)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	addons = sorted

	lastStepByAfter := make(map[string]string)

	for i, addon := range addons {
		prefix := fmt.Sprintf("inc%d_", i)

		if addon.Template == "" {
			return nil, fmt.Errorf("compose: addon workflow %q has no template", addon.Name)
		}

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
			return nil, fmt.Errorf("compose: addon %q: none of after nodes %v found in base plan steps", addon.Name, []string(addon.After))
		}

		sub, err := LoadWorkflowTemplate(addon.Template, graphDir, g)
		if err != nil {
			return nil, fmt.Errorf("compose: loading addon template %q: %w", addon.Name, err)
		}

		outputMap := buildOutputMap(parent, g)
		prefixStepRefs(sub, prefix)
		resolvedWire := resolveAfterWire(addon.Wire, matchedAfter)
		autoWirePlaceholders(sub, outputMap, resolvedWire, g)
		addInsertionDeps(sub, afterStep)
		fixFromDependencies(sub)
		spliceSteps(parent, sub, afterStep)
		mergeCleanup(parent, sub)

		lastAddonStep := sub.Execution.Steps[len(sub.Execution.Steps)-1].StepID()
		lastStepByAfter[matchedAfter] = lastAddonStep
	}

	return parent, nil
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
