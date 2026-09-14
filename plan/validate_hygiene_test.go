package plan

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
)

func hygieneGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"createOrder": {
				Name:    "createOrder",
				Outputs: []graph.Output{{Name: "orderId", Type: "string"}, {Name: "region", Type: "string"}},
			},
			"getOrder": {
				Name: "getOrder",
				Inputs: []graph.Input{
					{Name: "orderId", Type: "string"},
					{Name: "region", Type: "string"},
				},
			},
		},
	}
}

func TestValidationError_ListsEachProblemOnce(t *testing.T) {
	err := &ValidationError{Errors: []string{"b", "a", "b", "c", "a"}}

	assert.Equal(t, "plan validation failed:\n  - b\n  - a\n  - c", err.Error())
	assert.Equal(t, []string{"b", "a", "b", "c", "a"}, err.Errors, "the collected errors are left as found")
}

// TestInjectReferenceDeps_OncePerStep: two values that take data from the same
// step add it to dependsOn once, and validation doesn't ask for it.
func TestInjectReferenceDeps_OncePerStep(t *testing.T) {
	p := &Plan{Execution: Execution{Steps: []Step{
		{Node: "createOrder"},
		{
			Node: "getOrder",
			Values: map[string]StepValue{
				"orderId": {From: "createOrder.orderId"},
				"region":  {From: "createOrder.region"},
			},
		},
	}}}

	InjectReferenceDeps(p, false)
	assert.Equal(t, []string{"createOrder"}, p.Execution.Steps[1].DependsOn, "two references to one step add it once")
	if err := Validate(p, hygieneGraph()); err != nil {
		assert.NotContains(t, err.Error(), "dependsOn")
	}
}

// TestValidate_ValueErrorsInNameOrder: a step's values are checked in name
// order, so the same plan gives the same output on every run.
func TestValidate_ValueErrorsInNameOrder(t *testing.T) {
	p := &Plan{Execution: Execution{Steps: []Step{{
		Node: "getOrder",
		Values: map[string]StepValue{
			"orderId": {Default: "o-1"},
			"region":  {Default: "eu"},
			"zeta":    {Default: 1},
			"alpha":   {Default: 1},
			"mid":     {Default: 1},
		},
	}}}}

	err := Validate(p, hygieneGraph())
	require.Error(t, err)
	msg := err.Error()
	alpha, mid, zeta := strings.Index(msg, `"alpha"`), strings.Index(msg, `"mid"`), strings.Index(msg, `"zeta"`)
	require.True(t, alpha >= 0 && mid >= 0 && zeta >= 0, msg)
	assert.True(t, alpha < mid && mid < zeta, "values are reported in name order:\n%s", msg)

	for range 10 {
		assert.Equal(t, msg, Validate(p, hygieneGraph()).Error())
	}
}
