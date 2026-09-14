package intent

import (
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
)

// expectFailureGraph has an order to create and one to read.
func expectFailureGraph() *graph.Graph {
	return &graph.Graph{Nodes: map[string]*graph.Node{
		"createOrder": {Name: "createOrder", Outputs: []graph.Output{{Name: "orderId", Type: "string"}}},
		"getOrder":    {Name: "getOrder", Inputs: []graph.Input{{Name: "orderId", Type: "string"}, {Name: "note", Type: "string", Optional: true}}},
	}}
}

// TestResolveRemainingAutowire_PassesOverExpectedFailures checks that an
// AUTOWIRE marker takes the nearest earlier producer that isn't expected to
// fail, since a refused request returns none of the outputs it declares.
func TestResolveRemainingAutowire_PassesOverExpectedFailures(t *testing.T) {
	p := &plan.Plan{Execution: plan.Execution{Steps: []plan.Step{
		{ID: "good", Node: "createOrder"},
		{ID: "refused", Node: "createOrder", ExpectFailure: &plan.ExpectFailure{Status: []int{409}}},
		{ID: "read", Node: "getOrder", Values: map[string]plan.StepValue{"orderId": {Default: "AUTOWIRE"}}},
	}}}

	resolveRemainingAutowire(p, expectFailureGraph())

	assert.Equal(t, "good.orderId", p.Execution.Steps[2].Values["orderId"].From)
	assert.Equal(t, []string{"good"}, p.Execution.Steps[2].DependsOn)
}

func TestBuildOutputMap_PassesOverExpectedFailures(t *testing.T) {
	p := &plan.Plan{Execution: plan.Execution{Steps: []plan.Step{
		{ID: "good", Node: "createOrder"},
		{ID: "refused", Node: "createOrder", ExpectFailure: &plan.ExpectFailure{Status: []int{409}}},
	}}}

	assert.Equal(t, "good.orderId", buildOutputMap(p, expectFailureGraph())["orderId"])
}

// TestPrefixStepRefs_VerificationValues checks that a sub-workflow's
// verification values follow its steps' new IDs.
func TestPrefixStepRefs_VerificationValues(t *testing.T) {
	sub := &plan.Plan{Execution: plan.Execution{
		Steps: []plan.Step{{Node: "createOrder"}},
		Verification: []plan.VerificationStep{{Node: "getOrder", Values: map[string]plan.StepValue{
			"orderId": {From: "createOrder.orderId"},
			"note":    {FromInput: "base.note"},
		}}},
	}}

	prefixStepRefs(sub, "inc0_")

	values := sub.Execution.Verification[0].Values
	assert.Equal(t, "inc0_createOrder.orderId", values["orderId"].From)
	assert.Equal(t, "base.note", values["note"].FromInput, "a step outside the sub-workflow keeps its ID")
}
