package intent

import (
	"testing"

	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReconstitute_ValueOverrideReplacesWiring overrides an input that the
// workflow template wires with from: the step sends the override.
func TestReconstitute_ValueOverrideReplacesWiring(t *testing.T) {
	recipe := &plan.Recipe{
		Kind:      "recipe",
		Selection: plan.RecipeSelection{Workflow: "Base"},
		Overrides: plan.RecipeOverrides{Values: map[string]any{"commit.itineraryId": "IT-999"}},
	}

	p, err := Reconstitute(recipe, buildRecipeTestGraph(), ".")
	require.NoError(t, err)

	var commit *plan.Step
	for i := range p.Execution.Steps {
		if p.Execution.Steps[i].StepID() == "commit" {
			commit = &p.Execution.Steps[i]
		}
	}
	require.NotNil(t, commit)
	value := commit.Values["itineraryId"]
	assert.Equal(t, "IT-999", value.Default)
	assert.Empty(t, value.From, "the override replaces the template's wiring")
}

// TestReconstitute_OverrideForMissingStepFails names steps the composed plan
// does not have, which would otherwise do nothing.
func TestReconstitute_OverrideForMissingStepFails(t *testing.T) {
	recipe := &plan.Recipe{
		Kind:      "recipe",
		Selection: plan.RecipeSelection{Workflow: "Base"},
		Overrides: plan.RecipeOverrides{
			Values:     map[string]any{"serach.query": "shoes"},
			Selections: map[string]plan.RecipeSelectionOverride{"nosuch.flight": {Strategy: "first"}},
		},
	}

	_, err := Reconstitute(recipe, buildRecipeTestGraph(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "values.serach.query")
	assert.Contains(t, err.Error(), "selections.nosuch.flight")
	assert.Contains(t, err.Error(), "its steps: book, commit, search")
}
