package plan

import (
	"strings"

	"github.com/gburgyan/aat/graph"
)

// Instantiate deep-copies a plan and merges graph-level input defaults into
// step values. It also injects implicit dependsOn entries for graph default
// from-references and translates node-name from-refs to step-ID from-refs.
// The original plan is not modified.
func Instantiate(p *Plan, g *graph.Graph) *Plan {
	return InstantiateWithLayers(p, g, nil)
}

// InstantiateWithLayers deep-copies a plan and merges input defaults into step
// values, using layered defaults (if provided) in place of graph defaults.
// The layeredDefaults map is keyed by "nodeName.inputName". When a layered
// default exists for a (node, input) pair, it takes priority over the graph
// default. When layeredDefaults is nil, this behaves identically to Instantiate.
func InstantiateWithLayers(p *Plan, g *graph.Graph, layeredDefaults map[string]*graph.InputDefault) *Plan {
	if p == nil || g == nil {
		return nil
	}

	cp := deepCopyPlan(p)
	injectGraphDefaultDeps(cp, g, layeredDefaults)
	mergeGraphDefaultsWithLayers(cp, g, layeredDefaults)
	return cp
}

// InstantiateAndValidate is the standard entry point for plan preparation.
// It instantiates the plan (merging graph defaults) and validates the result.
// Returns the instantiated plan on success.
func InstantiateAndValidate(p *Plan, g *graph.Graph) (*Plan, error) {
	return InstantiateAndValidateWithLayers(p, g, nil)
}

// InstantiateAndValidateWithLayers instantiates a plan with layered defaults
// and validates the result. See InstantiateWithLayers for details.
func InstantiateAndValidateWithLayers(p *Plan, g *graph.Graph, layeredDefaults map[string]*graph.InputDefault) (*Plan, error) {
	inst := InstantiateWithLayers(p, g, layeredDefaults)
	if inst == nil {
		return nil, &ValidationError{Errors: []string{"plan or graph is nil"}}
	}
	if err := Validate(inst, g); err != nil {
		return nil, err
	}
	return inst, nil
}

// mergeGraphDefaultsWithLayers merges input defaults into step values for any
// inputs the plan doesn't explicitly specify. When layeredDefaults is non-nil,
// it takes priority over graph-level defaults. From-references are translated
// from node names to step IDs for composed plans.
func mergeGraphDefaultsWithLayers(p *Plan, g *graph.Graph, layeredDefaults map[string]*graph.InputDefault) {
	for i, step := range p.Execution.Steps {
		node, ok := g.Nodes[step.Node]
		if !ok {
			continue
		}

		if step.Values == nil {
			p.Execution.Steps[i].Values = make(map[string]StepValue)
		}

		for _, input := range node.Inputs {
			if _, exists := p.Execution.Steps[i].Values[input.Name]; exists {
				// Plan already specifies this value — don't override.
				continue
			}

			// Check layered defaults first, then graph defaults.
			effectiveDefault := input.Default
			if layeredDefaults != nil {
				key := step.Node + "." + input.Name
				if ld, ok := layeredDefaults[key]; ok {
					effectiveDefault = ld
				}
			}

			if effectiveDefault == nil || !effectiveDefault.HasValue() {
				continue
			}

			sv := inputDefaultToStepValue(effectiveDefault)

			// Translate from-ref node names to step IDs for composed plans
			if sv.From != "" {
				sv.From = translateFromRef(sv.From, p)
			}

			p.Execution.Steps[i].Values[input.Name] = sv
		}
	}
}

// inputDefaultToStepValue converts a graph InputDefault to a plan StepValue.
func inputDefaultToStepValue(d *graph.InputDefault) StepValue {
	sv := StepValue{}

	if d.Value != nil {
		sv.Default = d.Value
	}

	if len(d.Pool) > 0 {
		sv.Pool = make([]any, len(d.Pool))
		copy(sv.Pool, d.Pool)
	}

	if d.PoolStrategy != nil {
		s := *d.PoolStrategy
		sv.PoolStrategy = &s
	}

	if d.Constraint != "" {
		sv.Constraint = d.Constraint
	}

	if d.From != "" {
		sv.From = d.From
	}

	if d.FromResolved != "" {
		sv.FromResolved = d.FromResolved
	}

	if d.Select != nil {
		sv.Select = &SelectionConfig{
			Strategy:  d.Select.Strategy,
			Field:     d.Select.Field,
			Filter:    d.Select.Filter,
			Index:     d.Select.Index,
			SortField: d.Select.SortField,
			Prompt:    d.Select.Prompt,
		}
	}

	return sv
}

// translateFromRef translates a "node.field" from-reference to use step IDs
// instead of node names. This handles composed plans where step IDs may be
// prefixed (e.g., "inc0_createWorkbench" instead of "createWorkbench").
func translateFromRef(fromRef string, p *Plan) string {
	nodeName := splitFromNodeName(fromRef)
	if nodeName == "" {
		return fromRef
	}

	stepID := resolveNodeToStepID(nodeName, p)
	if stepID == nodeName {
		return fromRef // no translation needed
	}

	// Replace the node name portion with the step ID
	field := fromRef[len(nodeName):]
	return stepID + field
}

// resolveNodeToStepID maps a graph node name to the step ID in a plan.
// For non-composed plans, step ID == node name (the common case).
// For composed plans with prefixed step IDs (e.g., "inc0_createWorkbench"),
// it scans for a step whose Node field matches.
// Falls back to nodeName if no match is found or plan is nil.
func resolveNodeToStepID(nodeName string, p *Plan) string {
	if p == nil {
		return nodeName
	}
	for _, step := range p.Execution.Steps {
		if step.Node == nodeName {
			return step.StepID()
		}
	}
	return nodeName
}

// injectGraphDefaultDeps scans plan steps for inputs that rely on graph-level
// (or layered) default `from` references and auto-adds the referenced step to
// dependsOn. This ensures topological sort respects implicit data flow from
// graph defaults. Must be called before topological sort.
func injectGraphDefaultDeps(p *Plan, g *graph.Graph, layeredDefaults map[string]*graph.InputDefault) {
	if p == nil || g == nil {
		return
	}

	// Build step index: stepID → true
	stepIDs := make(map[string]bool, len(p.Execution.Steps))
	for _, step := range p.Execution.Steps {
		stepIDs[step.StepID()] = true
	}

	for i, step := range p.Execution.Steps {
		node := g.Nodes[step.Node]
		if node == nil {
			continue
		}

		depsSet := make(map[string]bool, len(step.DependsOn))
		for _, dep := range step.DependsOn {
			depsSet[dep] = true
		}

		for _, input := range node.Inputs {
			// Skip if plan provides a value for this input
			if _, hasPlanValue := step.Values[input.Name]; hasPlanValue {
				continue
			}

			// Check layered defaults first, then graph defaults.
			effectiveDefault := input.Default
			if layeredDefaults != nil {
				key := step.Node + "." + input.Name
				if ld, ok := layeredDefaults[key]; ok {
					effectiveDefault = ld
				}
			}

			if effectiveDefault == nil || effectiveDefault.From == "" {
				continue
			}

			// Extract the node name from the from reference
			fromNodeName := splitFromNodeName(effectiveDefault.From)
			if fromNodeName == "" {
				continue
			}

			// Resolve to step ID
			depStepID := resolveNodeToStepID(fromNodeName, p)
			if depStepID == step.StepID() {
				continue // self-reference
			}
			if !stepIDs[depStepID] {
				continue // referenced node not in plan
			}

			if !depsSet[depStepID] {
				p.Execution.Steps[i].DependsOn = append(p.Execution.Steps[i].DependsOn, depStepID)
				depsSet[depStepID] = true
			}
		}
	}
}

// splitFromNodeName extracts the node name from a "node.field" reference.
func splitFromNodeName(ref string) string {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// deepCopyPlan creates a deep copy of a Plan, including all slices, maps,
// and pointer fields, so that mutations to the copy don't affect the original.
func deepCopyPlan(p *Plan) *Plan {
	cp := *p

	// Deep-copy headers map
	if p.Headers != nil {
		cp.Headers = make(map[string]string, len(p.Headers))
		for k, v := range p.Headers {
			cp.Headers[k] = v
		}
	}

	// Deep-copy intent constraints
	if p.Intent.Constraints != nil {
		cpc := *p.Intent.Constraints
		if len(p.Intent.Constraints.Hard) > 0 {
			cpc.Hard = make([]Constraint, len(p.Intent.Constraints.Hard))
			copy(cpc.Hard, p.Intent.Constraints.Hard)
		}
		if len(p.Intent.Constraints.Soft) > 0 {
			cpc.Soft = make([]Constraint, len(p.Intent.Constraints.Soft))
			copy(cpc.Soft, p.Intent.Constraints.Soft)
		}
		if len(p.Intent.Constraints.Free) > 0 {
			cpc.Free = make([]string, len(p.Intent.Constraints.Free))
			copy(cpc.Free, p.Intent.Constraints.Free)
		}
		cp.Intent.Constraints = &cpc
	}

	// Deep-copy execution steps
	if len(p.Execution.Steps) > 0 {
		cp.Execution.Steps = make([]Step, len(p.Execution.Steps))
		for i, s := range p.Execution.Steps {
			cp.Execution.Steps[i] = deepCopyStep(s)
		}
	}

	// Deep-copy verification steps
	if len(p.Execution.Verification) > 0 {
		cp.Execution.Verification = make([]VerificationStep, len(p.Execution.Verification))
		copy(cp.Execution.Verification, p.Execution.Verification)
	}

	// Deep-copy cleanup steps
	if len(p.Execution.Cleanup) > 0 {
		cp.Execution.Cleanup = make([]CleanupStep, len(p.Execution.Cleanup))
		copy(cp.Execution.Cleanup, p.Execution.Cleanup)
	}

	return &cp
}

// deepCopyStep creates a deep copy of a Step.
func deepCopyStep(s Step) Step {
	cp := s

	if len(s.DependsOn) > 0 {
		cp.DependsOn = make([]string, len(s.DependsOn))
		copy(cp.DependsOn, s.DependsOn)
	}

	if s.Values != nil {
		cp.Values = make(map[string]StepValue, len(s.Values))
		for k, v := range s.Values {
			cp.Values[k] = deepCopyStepValue(v)
		}
	}

	if s.Selections != nil {
		cp.Selections = make(map[string]StepSelection, len(s.Selections))
		for k, v := range s.Selections {
			cp.Selections[k] = v
		}
	}

	return cp
}

// deepCopyStepValue creates a deep copy of a StepValue.
func deepCopyStepValue(sv StepValue) StepValue {
	cp := sv

	if len(sv.Pool) > 0 {
		cp.Pool = make([]any, len(sv.Pool))
		copy(cp.Pool, sv.Pool)
	}

	if sv.PoolStrategy != nil {
		s := *sv.PoolStrategy
		cp.PoolStrategy = &s
	}

	if sv.Select != nil {
		sel := *sv.Select
		cp.Select = &sel
	}

	return cp
}
