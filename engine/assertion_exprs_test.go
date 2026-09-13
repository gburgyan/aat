package engine

import (
	"context"
	"testing"

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
