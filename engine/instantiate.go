package engine

import (
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// InstantiatePlan deep-copies the plan and merges graph-level input defaults
// for any inputs the plan doesn't explicitly specify. The result is a
// point-in-time snapshot showing every input with its value source —
// useful for archive inspection and debugging.
func InstantiatePlan(p *plan.Plan, g *graph.Graph) *plan.Plan {
	if p == nil || g == nil {
		return nil
	}

	cp := deepCopyPlan(p)

	for i, step := range cp.Execution.Steps {
		node, ok := g.Nodes[step.Node]
		if !ok {
			continue
		}

		if step.Values == nil {
			cp.Execution.Steps[i].Values = make(map[string]plan.StepValue)
		}

		for _, input := range node.Inputs {
			if _, exists := cp.Execution.Steps[i].Values[input.Name]; exists {
				// Plan already specifies this value — don't override.
				continue
			}

			if input.Default == nil || !input.Default.HasValue() {
				continue
			}

			cp.Execution.Steps[i].Values[input.Name] = inputDefaultToStepValue(input.Default)
		}
	}

	return cp
}

// inputDefaultToStepValue converts a graph InputDefault to a plan StepValue.
func inputDefaultToStepValue(d *graph.InputDefault) plan.StepValue {
	sv := plan.StepValue{}

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
		sv.Select = &plan.SelectionConfig{
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

// deepCopyPlan creates a deep copy of a Plan, including all slices, maps,
// and pointer fields, so that mutations to the copy don't affect the original.
func deepCopyPlan(p *plan.Plan) *plan.Plan {
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
			cpc.Hard = make([]plan.Constraint, len(p.Intent.Constraints.Hard))
			copy(cpc.Hard, p.Intent.Constraints.Hard)
		}
		if len(p.Intent.Constraints.Soft) > 0 {
			cpc.Soft = make([]plan.Constraint, len(p.Intent.Constraints.Soft))
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
		cp.Execution.Steps = make([]plan.Step, len(p.Execution.Steps))
		for i, s := range p.Execution.Steps {
			cp.Execution.Steps[i] = deepCopyStep(s)
		}
	}

	// Deep-copy verification steps
	if len(p.Execution.Verification) > 0 {
		cp.Execution.Verification = make([]plan.VerificationStep, len(p.Execution.Verification))
		copy(cp.Execution.Verification, p.Execution.Verification)
	}

	// Deep-copy cleanup steps
	if len(p.Execution.Cleanup) > 0 {
		cp.Execution.Cleanup = make([]plan.CleanupStep, len(p.Execution.Cleanup))
		copy(cp.Execution.Cleanup, p.Execution.Cleanup)
	}

	return &cp
}

// deepCopyStep creates a deep copy of a Step.
func deepCopyStep(s plan.Step) plan.Step {
	cp := s

	if len(s.DependsOn) > 0 {
		cp.DependsOn = make([]string, len(s.DependsOn))
		copy(cp.DependsOn, s.DependsOn)
	}

	if s.Values != nil {
		cp.Values = make(map[string]plan.StepValue, len(s.Values))
		for k, v := range s.Values {
			cp.Values[k] = deepCopyStepValue(v)
		}
	}

	if s.Selections != nil {
		cp.Selections = make(map[string]plan.StepSelection, len(s.Selections))
		for k, v := range s.Selections {
			cp.Selections[k] = v
		}
	}

	return cp
}

// deepCopyStepValue creates a deep copy of a StepValue.
func deepCopyStepValue(sv plan.StepValue) plan.StepValue {
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
