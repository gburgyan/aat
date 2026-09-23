package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gburgyan/aat/graph"
)

func TestPlanPoolRefs(t *testing.T) {
	p := &Plan{Execution: Execution{Steps: []Step{
		{ID: "b", Node: "search", Values: map[string]StepValue{"origin": {PoolRef: "airports.us"}, "cabin": {Default: "Y"}}},
		{ID: "a", Node: "search", Values: map[string]StepValue{"origin": {PoolRef: "airports.eu"}}},
	}}}
	assert.Equal(t, []graph.PoolRefUse{
		{Where: "step a value origin", Ref: "airports.eu"},
		{Where: "step b value origin", Ref: "airports.us"},
	}, p.PoolRefs())
}

func TestStepValueFromDefault_PoolRef(t *testing.T) {
	sv := StepValueFromDefault(&graph.InputDefault{PoolRef: "airports", Constraint: "value != origin"})
	assert.Equal(t, "airports", sv.PoolRef)
	assert.False(t, sv.IsEmpty())
}
