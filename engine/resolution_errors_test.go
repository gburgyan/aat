package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

func TestResolveInputs_ErrorKeepsEarlierResolutions(t *testing.T) {
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"addItem": {Name: "addItem", Inputs: []graph.Input{{Name: "sku", Type: "string"}, {Name: "quantity", Type: "integer"}}},
	}}

	t.Run("an input", func(t *testing.T) {
		step := plan.Step{Node: "addItem", Values: map[string]plan.StepValue{"sku": {Default: "SKU-1"}}}
		inputs, _, resolutions, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["addItem"], g, NewRunState(), nil)

		require.Error(t, err)
		assert.Equal(t, map[string]any{"sku": "SKU-1"}, inputs, "what resolved before the error")
		require.Len(t, resolutions, 2)
		assert.Equal(t, "plan_default", resolutions[0].Source)
		assert.Equal(t, ValueResolution{InputName: "quantity", Source: "error", Error: "required input has no value", PoolIndex: -1}, resolutions[1])
	})

	t.Run("a named selection", func(t *testing.T) {
		step := plan.Step{
			Node:       "addItem",
			Selections: map[string]plan.StepSelection{"cheapest": {From: "listProducts.products", Strategy: "min", SortField: "price"}},
			Values:     map[string]plan.StepValue{"sku": {FromSelection: "cheapest.sku"}, "quantity": {Default: 1}},
		}
		_, _, resolutions, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["addItem"], g, NewRunState(), nil)

		require.Error(t, err)
		require.Len(t, resolutions, 1)
		assert.Equal(t, "cheapest", resolutions[0].InputName)
		assert.Equal(t, "error", resolutions[0].Source)
		assert.NotEmpty(t, resolutions[0].Error)
	})
}

// buildFailAdapter extracts like stubAdapter but can never build its request.
type buildFailAdapter struct{ stubAdapter }

func (a *buildFailAdapter) BuildRequest(map[string]any, *adapter.EnvironmentConfig) (*adapter.Request, error) {
	return nil, errors.New("template placeholder {{note}} has no value")
}

func resolutionsGraph() *graph.Graph {
	return &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"listProducts": {Name: "listProducts", Adapter: "res.listProducts", Outputs: []graph.Output{{Name: "products", Type: "string[]"}}},
		"addItem": {Name: "addItem", Adapter: "res.addItem", Inputs: []graph.Input{
			{Name: "sku", Type: "string"},
			{Name: "quantity", Type: "integer"},
		}},
	}}
}

func resolutionsPlan() *plan.Plan {
	return chainPlan(
		plan.Step{Node: "listProducts"},
		plan.Step{Node: "addItem", DependsOn: []string{"listProducts"}, Values: map[string]plan.StepValue{
			"sku":      {From: "listProducts.products", Select: &plan.SelectionConfig{Strategy: "min", SortField: "price", Field: "sku"}},
			"quantity": {Default: 2},
		}},
	)
}

func TestExecuteStep_RecordsResolutionsWhenBuildFails(t *testing.T) {
	srv := newChainServer(t, nil)
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("res.listProducts", &stubAdapter{method: "GET", path: "/products", response: map[string]any{
		"products": []any{map[string]any{"sku": "a", "price": 20}, map[string]any{"sku": "b", "price": 10}},
	}}))
	require.NoError(t, registry.Register("res.addItem", &buildFailAdapter{}))
	eng := NewEngine(resolutionsGraph(), registry, NewExecutorRouter(adapter.NewHTTPExecutor(srv.URL), &adapter.EnvironmentConfig{}))

	result := eng.Run(context.Background(), resolutionsPlan())

	require.Equal(t, OutcomeError, result.Outcome)
	require.Len(t, result.Steps, 2)
	failed := result.Steps[1]
	assert.ErrorContains(t, failed.Error, "building request")
	require.Len(t, failed.Selections, 1)
	assert.Equal(t, 1, failed.Selections[0].SelectedIndex)
	require.Len(t, failed.Resolutions, 2)
	assert.Equal(t, "select_edge", failed.Resolutions[0].Source)
	assert.Equal(t, "b", failed.Resolutions[0].FinalValue)
}

func TestExecuteStep_RecordsResolutionsWhenSendFails(t *testing.T) {
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("res.addItem", &stubAdapter{method: "POST", path: "/carts/items"}))
	// Nothing listens on port 1, so the request fails before any response.
	eng := NewEngine(resolutionsGraph(), registry, NewExecutorRouter(adapter.NewHTTPExecutor("http://127.0.0.1:1"), &adapter.EnvironmentConfig{}))

	result := eng.Run(context.Background(), chainPlan(plan.Step{Node: "addItem", Values: map[string]plan.StepValue{
		"sku":      {Default: "SKU-1"},
		"quantity": {Default: 2},
	}}))

	require.Equal(t, OutcomeError, result.Outcome)
	require.Len(t, result.Steps, 1)
	require.Len(t, result.Steps[0].Resolutions, 2)
	assert.Equal(t, "plan_default", result.Steps[0].Resolutions[0].Source)
	assert.Equal(t, "SKU-1", result.Steps[0].Resolutions[0].FinalValue)
}
