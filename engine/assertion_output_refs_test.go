package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// runOutputRefs runs a checkout that totals 13066, then a refund of 13066
// with the given assertions, and a verification read of the refund with
// verify.
func runOutputRefs(t *testing.T, refund, verify []plan.MechanicalAssertion) *RunResult {
	t.Helper()
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"checkoutCart":  {Name: "checkoutCart", Adapter: "refs.checkout", Outputs: []graph.Output{{Name: "total", Type: "integer"}, {Name: "coupon", Type: "string", Optional: true}}},
		"paymentRefund": {Name: "paymentRefund", Adapter: "refs.refund", Outputs: []graph.Output{{Name: "amount", Type: "integer"}}},
	}}
	srv := newChainServer(t, nil)
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("refs.checkout", &stubAdapter{method: "POST", path: "/checkout", response: map[string]any{"total": 13066}}))
	require.NoError(t, registry.Register("refs.refund", &stubAdapter{method: "POST", path: "/refunds", response: map[string]any{"amount": 13066}}))
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(srv.URL), &adapter.EnvironmentConfig{}))

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{ID: "checkout", Node: "checkoutCart"},
				{ID: "refund", Node: "paymentRefund", DependsOn: []string{"checkout"}, Assertions: &plan.Assertions{Mechanical: refund}},
			},
		},
	}
	if verify != nil {
		p.Execution.Verification = []plan.VerificationStep{{Node: "paymentRefund", Assertions: &plan.Assertions{Mechanical: verify}}}
	}
	return eng.Run(context.Background(), p)
}

func TestAssertions_ReadEarlierStepOutputs(t *testing.T) {
	result := runOutputRefs(t,
		[]plan.MechanicalAssertion{
			{Type: "predicate", Expr: `amount == "{{checkout.total}}"`},
			{Type: "fieldEquals", Path: "amount", Value: "{{checkout.total}}"},
		},
		[]plan.MechanicalAssertion{{Type: "predicate", Expr: `amount >= "{{checkout.total}}"`}},
	)

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	refund := result.Steps[1]
	require.NotNil(t, refund.Validation)
	assert.True(t, refund.Validation.Passed, "%+v", refund.Validation.Results)
	assert.Equal(t, `predicate "amount == 13066" is true`, refund.Validation.Results[0].Message, "the message shows the value it compared with")
	verification := result.Steps[len(result.Steps)-1]
	require.NotNil(t, verification.Validation)
	assert.True(t, verification.Validation.Passed, "a verification step reads a main step: %+v", verification.Validation.Results)
}

func TestAssertions_EarlierStepOutputThatFails(t *testing.T) {
	result := runOutputRefs(t, []plan.MechanicalAssertion{
		{Type: "predicate", Expr: `amount > "{{checkout.total}}"`},
		{Type: "predicate", Expr: `amount == "{{checkout.coupon}}"`},
	}, nil)

	require.Equal(t, OutcomeFailed, result.Outcome, "error: %v", result.Error)
	v := result.Steps[1].Validation
	require.NotNil(t, v)
	require.Len(t, v.Results, 2)
	assert.False(t, v.Results[0].Passed)
	assert.Contains(t, v.Results[0].Message, "13066")
	assert.False(t, v.Results[1].Passed)
	assert.Contains(t, v.Results[1].Message, `step "checkout" produced no output "coupon"`)
}

// TestAssertions_StepThatStoredNoOutputs runs a step on its own, past the plan
// validation that reports an unknown step, to check what an assertion says
// when the step it reads has no outputs in the run.
func TestAssertions_StepThatStoredNoOutputs(t *testing.T) {
	node := &graph.Node{Name: "paymentRefund", Adapter: "refs.refund", Outputs: []graph.Output{{Name: "amount", Type: "integer"}}}
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{"paymentRefund": node}}
	srv := newChainServer(t, nil)
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("refs.refund", &stubAdapter{method: "POST", path: "/refunds", response: map[string]any{"amount": 13066}}))
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(srv.URL), &adapter.EnvironmentConfig{}))
	step := plan.Step{ID: "refund", Node: "paymentRefund", Assertions: &plan.Assertions{Mechanical: []plan.MechanicalAssertion{
		{Type: "fieldEquals", Path: "amount", Value: "{{checkout.total}}"},
	}}}

	result := eng.executeStepWith(context.Background(), step, node, NewRunState(), nil)

	require.NoError(t, result.Error)
	require.NotNil(t, result.Validation)
	assert.False(t, result.Validation.Passed)
	assert.Contains(t, result.Validation.Results[0].Message, `step "checkout" stored no outputs`)
}
