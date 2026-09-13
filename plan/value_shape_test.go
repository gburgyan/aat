package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
)

func TestStepValueFromDefault(t *testing.T) {
	strategy := "random"
	sv := StepValueFromDefault(&graph.InputDefault{
		Pool:         []any{"us", "eu"},
		PoolStrategy: &strategy,
		Constraint:   `region != "apac"`,
		Select:       &graph.InputDefaultSelect{Strategy: "min", SortField: "price", OnTie: "fail"},
	})

	assert.Equal(t, []any{"us", "eu"}, sv.Pool)
	require.NotNil(t, sv.PoolStrategy)
	assert.Equal(t, "random", *sv.PoolStrategy)
	assert.Equal(t, `region != "apac"`, sv.Constraint)
	require.NotNil(t, sv.Select)
	assert.Equal(t, "fail", sv.Select.OnTie)
	assert.Equal(t, StepValue{Default: []any{1, 2}}, StepValueFromDefault(&graph.InputDefault{Value: []any{1, 2}}))
}

func TestValidate_StepValueShape(t *testing.T) {
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"addItem": {Name: "addItem", Inputs: []graph.Input{
			{Name: "quantity", Type: "integer"},
			{Name: "skus", Type: "string[]"},
			{Name: "note", Type: "string", Optional: true},
		}},
	}}

	bad := &Plan{Execution: Execution{Steps: []Step{{Node: "addItem", Values: map[string]StepValue{
		"quantity": {Default: map[string]any{"n": 2}},
		"skus":     {Pool: []any{"SKU-1", "SKU-2"}},
	}}}}}
	err := Validate(bad, g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `step 0 (addItem): value "quantity": a map, where integer takes a single value`)
	assert.Contains(t, err.Error(), `step 0 (addItem): value "skus": pool entry 0: a single value, where string[] takes a list`)

	good := &Plan{Execution: Execution{Steps: []Step{{Node: "addItem", Values: map[string]StepValue{
		"quantity": {Default: "{{random 1}}"},
		"skus":     {Default: []any{"SKU-1"}},
		"note":     {Default: "AUTOWIRE?"},
	}}}}}
	err = Validate(good, g)
	if err != nil {
		assert.NotContains(t, err.Error(), "takes a")
	}
}
