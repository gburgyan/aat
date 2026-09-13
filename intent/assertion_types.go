package intent

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/gburgyan/aat/plan"
)

// checkTemplateAssertionTypes reports an assertion of an unknown type in a
// workflow template's steps or verification steps. The engine would fail such
// an assertion on every run.
func checkTemplateAssertionTypes(p *plan.Plan) error {
	check := func(where string, assertions *plan.Assertions) error {
		if assertions == nil {
			return nil
		}
		for i, a := range assertions.Mechanical {
			if !plan.IsAssertionType(a.Type) {
				return fmt.Errorf("workflow template %s: assertion %d has unknown type %q (use %s)", where, i, a.Type, strings.Join(plan.AssertionTypes(), ", "))
			}
		}
		return nil
	}
	for _, step := range p.Execution.Steps {
		if step.IsSlotMarker() {
			continue
		}
		if err := check(fmt.Sprintf("step %q", step.StepID()), step.Assertions); err != nil {
			return err
		}
	}
	for _, vs := range p.Execution.Verification {
		if err := check(fmt.Sprintf("verification of %q", vs.Node), vs.Assertions); err != nil {
			return err
		}
	}
	return nil
}

// checkOverrideAssertionTypes reports a recipe override assertion of a type the
// engine doesn't have. The LLM path drops such assertions, but a recipe's are
// written by a person, so a misspelled type is an error rather than a check that
// silently never runs.
func checkOverrideAssertionTypes(overrides plan.RecipeOverrides) error {
	for _, step := range slices.Sorted(maps.Keys(overrides.Assertions)) {
		for i, a := range overrides.Assertions[step] {
			if !plan.IsAssertionType(a.Type) {
				return fmt.Errorf("override assertion %d for step %q has unknown type %q (use %s)", i, step, a.Type, strings.Join(plan.AssertionTypes(), ", "))
			}
		}
	}
	return nil
}
