package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// resolveGraphDefault resolves a rich graph-level InputDefault at priority 4
// in the resolution chain. It supports literal values, pools with constraints,
// from references (with optional select), and fromResolved intra-step refs.
//
// Plan values at any priority (1-4) always skip this function entirely.
func resolveGraphDefault(ctx context.Context, input graph.Input, step plan.Step, g *graph.Graph, state *RunState, dedupCache map[string]*selectionResult, rctx *ResolveContext, ectx *plan.ExprContext, resolvedInputs map[string]any) (any, *SelectionDecision, *ValueResolution, error) {
	d := input.Default
	if d == nil {
		return nil, nil, nil, fmt.Errorf("no graph default for input %q", input.Name)
	}

	// FromResolved: intra-step reference within the same step
	if d.FromResolved != "" {
		val, exists := resolvedInputs[d.FromResolved]
		if !exists {
			return nil, nil, nil, fmt.Errorf("graph default fromResolved references %q which has not been resolved yet", d.FromResolved)
		}
		res := &ValueResolution{
			InputName:  input.Name,
			Source:     "graph_default",
			FinalValue: val,
			PoolIndex:  -1,
		}
		return val, nil, res, nil
	}

	// From reference (with optional select)
	if d.From != "" {
		fromNode, fromField, err := splitGraphDefaultRef(d.From)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("graph default from reference %q: %w", d.From, err)
		}

		// Resolve node name → step ID for composed plans
		p := planFromContext(rctx)
		stepID := resolveNodeToStepID(fromNode, p)

		if d.Select != nil {
			// From + Select: array selection
			selCfg := &plan.SelectionConfig{
				Strategy:  d.Select.Strategy,
				Field:     d.Select.Field,
				Filter:    d.Select.Filter,
				Index:     d.Select.Index,
				SortField: d.Select.SortField,
				Prompt:    d.Select.Prompt,
			}
			if selCfg.Strategy == "" {
				selCfg.Strategy = "first"
			}
			val, decision, err := resolveSelectValue(ctx, stepID, fromField, input.Name, selCfg, g, state, dedupCache, rctx)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("graph default from+select %q: %w", d.From, err)
			}
			res := &ValueResolution{
				InputName:  input.Name,
				Source:     "graph_default",
				FinalValue: val,
				FromStep:   stepID,
				FromOutput: fromField,
				PoolIndex:  -1,
			}
			return val, decision, res, nil
		}

		// Plain from reference
		val, err := state.GetOutput(stepID, fromField)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("graph default from reference %q (resolved to %s.%s): %w", d.From, stepID, fromField, err)
		}
		res := &ValueResolution{
			InputName:  input.Name,
			Source:     "graph_default",
			FinalValue: val,
			FromStep:   stepID,
			FromOutput: fromField,
			PoolIndex:  -1,
		}
		return val, nil, res, nil
	}

	// Pool with optional constraint
	if len(d.Pool) > 0 {
		sv := plan.StepValue{
			Pool:       d.Pool,
			Constraint: d.Constraint,
		}
		if d.PoolStrategy != nil {
			sv.PoolStrategy = d.PoolStrategy
		}
		if rctx != nil && ectx != nil {
			val, decision, resolution, err := resolveWithFallback(ctx, sv, input, *ectx, resolvedInputs, rctx)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("graph default pool for %q: %w", input.Name, err)
			}
			if resolution != nil {
				resolution.Source = "graph_default"
			}
			return val, decision, resolution, nil
		}
		// Without enhanced context, pick first pool value
		val := d.Pool[0]
		res := &ValueResolution{
			InputName:  input.Name,
			Source:     "graph_default",
			FinalValue: val,
			PoolIndex:  0,
			PoolSize:   len(d.Pool),
		}
		return val, nil, res, nil
	}

	// Simple literal value
	if d.Value != nil {
		val := d.Value
		if rctx != nil && ectx != nil {
			evaluated, err := evaluateValue(val, *ectx)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("evaluating graph default for %q: %w", input.Name, err)
			}
			val = evaluated
		}
		res := &ValueResolution{
			InputName:  input.Name,
			Source:     "graph_default",
			FinalValue: val,
			PoolIndex:  -1,
		}
		return val, nil, res, nil
	}

	return nil, nil, nil, fmt.Errorf("graph default for %q has no resolvable value", input.Name)
}

// resolveNodeToStepID maps a graph node name to the step ID in a plan.
// For non-composed plans, step ID == node name (the common case).
// For composed plans with prefixed step IDs (e.g., "inc0_createWorkbench"),
// it scans for a step whose Node field matches.
// Falls back to nodeName if no match is found or plan is nil.
func resolveNodeToStepID(nodeName string, p *plan.Plan) string {
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

// planFromContext extracts the plan from a ResolveContext, if available.
func planFromContext(rctx *ResolveContext) *plan.Plan {
	if rctx != nil {
		return rctx.Plan
	}
	return nil
}

// InjectGraphDefaultDeps scans plan steps for inputs that rely on graph-level
// default `from` references and auto-adds the referenced step to dependsOn.
// This ensures topological sort respects implicit data flow from graph defaults.
// Must be called before topological sort.
func InjectGraphDefaultDeps(p *plan.Plan, g *graph.Graph) {
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

		changed := false
		for _, input := range node.Inputs {
			// Skip if plan provides a value for this input
			if _, hasPlanValue := step.Values[input.Name]; hasPlanValue {
				continue
			}

			if input.Default == nil || input.Default.From == "" {
				continue
			}

			// Extract the node name from the from reference
			fromNodeName := splitFromNodeName(input.Default.From)
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
				changed = true
			}
		}
		_ = changed
	}
}

// splitGraphDefaultRef splits a "node.field" reference.
func splitGraphDefaultRef(ref string) (string, string, error) {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected format \"node.field\", got %q", ref)
	}
	return parts[0], parts[1], nil
}

// splitFromNodeName extracts the node name from a "node.field" reference.
func splitFromNodeName(ref string) string {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
