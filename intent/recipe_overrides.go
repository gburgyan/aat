package intent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gburgyan/aat/plan"
)

// checkOverrideSteps reports recipe overrides whose step the composed plan does
// not have. The error lists the plan's step IDs, so a missing addon prefix such
// as inc0_ is easy to spot.
func checkOverrideSteps(p *plan.Plan, resp *TargetedResponse) error {
	ids := make(map[string]bool, len(p.Execution.Steps))
	var steps []string
	for _, s := range p.Execution.Steps {
		ids[s.StepID()] = true
		steps = append(steps, s.StepID())
	}

	var unknown []string
	for key := range resp.Values {
		if id, _ := splitStepInput(key); !ids[id] {
			unknown = append(unknown, "values."+key)
		}
	}
	for key := range resp.Selections {
		if id, _ := splitStepInput(key); !ids[id] {
			unknown = append(unknown, "selections."+key)
		}
	}
	for id := range resp.Assertions {
		if !ids[id] {
			unknown = append(unknown, "assertions."+id)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	sort.Strings(steps)
	return fmt.Errorf("overrides name steps the composed plan does not have: %s (its steps: %s)",
		strings.Join(unknown, ", "), strings.Join(steps, ", "))
}

// replaceWiring makes each input a recipe value override sets send the
// override's value, by removing the reference the workflow template or
// composition gave it (from, select, fromSelection, fromInput, fromResolved).
// applyTargetedResponse keeps references on purpose, because model output must
// not shadow auto-wired edges; a recipe states what the step should send.
func replaceWiring(p *plan.Plan, values map[string]any) {
	for key := range values {
		stepID, input := splitStepInput(key)
		for i := range p.Execution.Steps {
			step := &p.Execution.Steps[i]
			if step.StepID() != stepID {
				continue
			}
			if sv, ok := step.Values[input]; ok {
				sv.From = ""
				sv.Select = nil
				sv.FromSelection = ""
				sv.FromInput = ""
				sv.FromResolved = ""
				step.Values[input] = sv
			}
		}
	}
}
