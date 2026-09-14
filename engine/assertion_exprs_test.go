package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/gburgyan/aat/validate"
)

// runAssertions runs one addItem step, whose inputs and outputs are both
// quantity 3 and sku SKU-1, with the given assertions.
func runAssertions(t *testing.T, assertions ...plan.MechanicalAssertion) *validate.MechanicalResult {
	t.Helper()
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"addItem": {
			Name: "addItem", Adapter: "expr.addItem",
			Inputs:  []graph.Input{{Name: "quantity", Type: "integer"}, {Name: "sku", Type: "string"}},
			Outputs: []graph.Output{{Name: "quantity", Type: "integer"}, {Name: "sku", Type: "string"}},
		},
	}}
	srv := newChainServer(t, nil)
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("expr.addItem", &stubAdapter{method: "POST", path: "/items", response: map[string]any{"quantity": 3, "sku": "SKU-1"}}))
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(srv.URL), &adapter.EnvironmentConfig{}))

	result := eng.Run(context.Background(), chainPlan(plan.Step{
		Node:       "addItem",
		Values:     map[string]plan.StepValue{"quantity": {Default: 3}, "sku": {Default: "SKU-1"}},
		Assertions: &plan.Assertions{Mechanical: assertions},
	}))
	require.Len(t, result.Steps, 1, "error: %v", result.Error)
	require.NotNil(t, result.Steps[0].Validation)
	return result.Steps[0].Validation
}

func TestExecuteStep_FieldEqualsExpandsExpression(t *testing.T) {
	v := runAssertions(t,
		plan.MechanicalAssertion{Type: "fieldEquals", Path: "quantity", Value: "{{quantity}}"},
		plan.MechanicalAssertion{Type: "fieldEquals", Path: "sku", Value: "{{sku}}"},
	)
	assert.True(t, v.Passed, "%+v", v.Results)
}

func TestExecuteStep_PredicateLiteralExpands(t *testing.T) {
	v := runAssertions(t, plan.MechanicalAssertion{Type: "predicate", Expr: `quantity == "{{quantity}}" && sku == "{{sku}}"`})
	assert.True(t, v.Passed, "%+v", v.Results)
}

// TestExecuteStep_PredicateNamesMissingOutput checks that a predicate naming an
// output the step didn't produce says so, while one reading the raw body keeps
// the predicate's own message.
func TestExecuteStep_PredicateNamesMissingOutput(t *testing.T) {
	v := runAssertions(t,
		plan.MechanicalAssertion{Type: "predicate", Expr: `trackingNumber == "TRK-0001"`},
		plan.MechanicalAssertion{Type: "predicate", Expr: `sku.code == "SKU-1"`},
		plan.MechanicalAssertion{Type: "predicate", Expr: `trackingNumber == "TRK-0001"`, Raw: true},
	)
	assert.False(t, v.Passed)
	require.Len(t, v.Results, 3)
	assert.Equal(t, `predicate evaluation error: unknown field "trackingNumber": the step produced no output "trackingNumber" (an optional output is absent when the response doesn't hold it)`, v.Results[0].Message)
	assert.NotContains(t, v.Results[1].Message, "produced no output", "sku is an output")
	assert.Equal(t, `predicate evaluation error: unknown field "trackingNumber"`, v.Results[2].Message)
}

func TestExecuteStep_AssertionExpressionErrorFailsAssertion(t *testing.T) {
	v := runAssertions(t,
		plan.MechanicalAssertion{Type: "fieldEquals", Path: "sku", Value: "{{missingInput}}"},
		plan.MechanicalAssertion{Type: "fieldEquals", Path: "sku", Value: "SKU-1"},
	)
	assert.False(t, v.Passed)
	require.Len(t, v.Results, 2)
	assert.False(t, v.Results[0].Passed)
	assert.Contains(t, v.Results[0].Message, "expanding value {{missingInput}}")
	assert.True(t, v.Results[1].Passed, "the other assertion still runs")
}

// TestExecuteStep_AssertionExpressionsReadTheResolutionTime pins that an attempt
// that resends prepared inputs evaluates its assertions at the time the inputs
// were resolved, so a retry after midnight still matches the date it sent.
func TestExecuteStep_AssertionExpressionsReadTheResolutionTime(t *testing.T) {
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"scheduleDelivery": {
			Name: "scheduleDelivery", Adapter: "expr.scheduleDelivery",
			Inputs:  []graph.Input{{Name: "deliveryDate", Type: "date"}},
			Outputs: []graph.Output{{Name: "deliveryDate", Type: "date"}},
		},
	}}
	srv := newChainServer(t, nil)
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("expr.scheduleDelivery", &stubAdapter{method: "POST", path: "/deliveries", response: map[string]any{"deliveryDate": "2026-01-04"}}))
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(srv.URL), &adapter.EnvironmentConfig{}))
	step := plan.Step{
		Node:       "scheduleDelivery",
		Values:     map[string]plan.StepValue{"deliveryDate": {Default: "{{today + 3 days}}"}},
		Assertions: &plan.Assertions{Mechanical: []plan.MechanicalAssertion{{Type: "fieldEquals", Path: "deliveryDate", Value: "{{today + 3 days}}"}}},
	}
	prepared := stepInputs{
		resolved: true,
		inputs:   map[string]any{"deliveryDate": "2026-01-04"},
		now:      time.Date(2026, 1, 1, 23, 59, 59, 0, time.UTC),
	}

	result := eng.executeStepWith(context.Background(), step, g.Nodes["scheduleDelivery"], NewRunState(), &prepared)
	require.NoError(t, result.Error)
	require.NotNil(t, result.Validation)
	assert.True(t, result.Validation.Passed, "%+v", result.Validation.Results)
}
