package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

func TestApplySelection_MinMaxCountsTies(t *testing.T) {
	products := []any{
		map[string]any{"sku": "a", "price": "125.00", "category": "gear"},
		map[string]any{"sku": "b", "price": 99.5, "category": "apparel"},
		map[string]any{"sku": "c", "price": "125", "category": "apparel"},
		map[string]any{"sku": "d", "price": 125, "category": "gear"},
	}

	highest, err := applySelection(products, &plan.SelectionConfig{Strategy: "max", SortField: "price"})
	require.NoError(t, err)
	assert.Equal(t, 0, highest.index, "the first of the tied elements")
	assert.Equal(t, 3, highest.ties)
	require.NotNil(t, highest.sortValue)
	assert.Equal(t, 125.0, *highest.sortValue)

	lowest, err := applySelection(products, &plan.SelectionConfig{Strategy: "min", Field: "price"})
	require.NoError(t, err)
	assert.Equal(t, 1, lowest.index)
	assert.Equal(t, 1, lowest.ties, "no tie")

	apparel, err := applySelection(products, &plan.SelectionConfig{Strategy: "max", SortField: "price", Filter: `category == "apparel"`})
	require.NoError(t, err)
	assert.Equal(t, 2, apparel.filteredSize)
	assert.Equal(t, 1, apparel.index, "the index is within the filtered elements")
	assert.Equal(t, 1, apparel.ties)

	first, err := applySelection(products, &plan.SelectionConfig{Strategy: "first"})
	require.NoError(t, err)
	assert.Zero(t, first.ties)
	assert.Nil(t, first.sortValue)
}

func TestApplySelection_MatchReportsMatchCount(t *testing.T) {
	products := []any{
		map[string]any{"sku": "a", "stock": 0},
		map[string]any{"sku": "b", "stock": 3},
		map[string]any{"sku": "c"}, // no stock: after the first match, not counted and not an error
		map[string]any{"sku": "d", "stock": 5},
	}

	got, err := applySelection(products, &plan.SelectionConfig{Strategy: "match", Filter: "stock > 0"})
	require.NoError(t, err)
	assert.Equal(t, 1, got.index)
	assert.Equal(t, "b", got.element.(map[string]any)["sku"])
	assert.Equal(t, 2, got.filteredSize)

	_, err = applySelection([]any{map[string]any{"sku": "a"}}, &plan.SelectionConfig{Strategy: "match", Filter: "stock > 0"})
	assert.Error(t, err, "a predicate error before any match still fails")
}

// tieGraph and tieState model a product list where two products share the
// lowest price and two share the fastest delivery.
func tieGraph() *graph.Graph {
	return &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"listProducts": {Name: "listProducts", Outputs: []graph.Output{{Name: "products", Type: "string[]"}}},
		"addItem": {Name: "addItem", Inputs: []graph.Input{
			{Name: "cheapestSku", Type: "string"},
			{Name: "fastestSku", Type: "string"},
		}},
	}}
}

func tieState() *RunState {
	state := NewRunState()
	state.StoreOutputs("listProducts", map[string]any{"products": []any{
		map[string]any{"sku": "a", "price": "12.50", "deliveryDays": 3},
		map[string]any{"sku": "b", "price": "12.50", "deliveryDays": 1},
		map[string]any{"sku": "c", "price": "19.99", "deliveryDays": 1},
	}})
	return state
}

func TestResolveInputs_DifferentSortFieldsPickSeparately(t *testing.T) {
	g := tieGraph()
	step := plan.Step{Node: "addItem", Values: map[string]plan.StepValue{
		"cheapestSku": {From: "listProducts.products", Select: &plan.SelectionConfig{Strategy: "min", SortField: "price", Field: "sku"}},
		"fastestSku":  {From: "listProducts.products", Select: &plan.SelectionConfig{Strategy: "min", SortField: "deliveryDays", Field: "sku"}},
	}}

	inputs, decisions, err := ResolveInputs(step, g.Nodes["addItem"], g, tieState())
	require.NoError(t, err)
	assert.Equal(t, "a", inputs["cheapestSku"])
	assert.Equal(t, "b", inputs["fastestSku"], "a pick by another field is not the cached one")
	require.Len(t, decisions, 2)
}

func TestResolveInputs_SameSortFieldSharesPick(t *testing.T) {
	g := tieGraph()
	g.Nodes["addItem"].Inputs = []graph.Input{{Name: "sku", Type: "string"}, {Name: "price", Type: "string"}}
	step := plan.Step{Node: "addItem", Values: map[string]plan.StepValue{
		"sku":   {From: "listProducts.products", Select: &plan.SelectionConfig{Strategy: "min", SortField: "price", Field: "sku"}},
		"price": {From: "listProducts.products", Select: &plan.SelectionConfig{Strategy: "min", SortField: "price", Field: "price"}},
	}}

	inputs, decisions, err := ResolveInputs(step, g.Nodes["addItem"], g, tieState())
	require.NoError(t, err)
	assert.Equal(t, "a", inputs["sku"])
	assert.Equal(t, "12.50", inputs["price"], "both inputs read the same element")
	require.Len(t, decisions, 2)
	for _, d := range decisions {
		assert.Equal(t, 2, d.Ties, d.InputName)
		assert.Equal(t, "price", d.SortField, d.InputName)
		require.NotNil(t, d.SortValue, d.InputName)
		assert.Equal(t, 12.5, *d.SortValue, d.InputName)
	}
}

func TestResolveInputs_OnTieFail(t *testing.T) {
	g := tieGraph()

	inline := plan.Step{Node: "addItem", Values: map[string]plan.StepValue{
		"cheapestSku": {From: "listProducts.products", Select: &plan.SelectionConfig{Strategy: "min", SortField: "price", Field: "sku", OnTie: "fail"}},
		"fastestSku":  {Default: "x"},
	}}
	_, _, err := ResolveInputs(inline, g.Nodes["addItem"], g, tieState())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 of 3 elements tie for min price at 12.5, and onTie is fail")

	named := plan.Step{
		Node:       "addItem",
		Selections: map[string]plan.StepSelection{"cheapest": {From: "listProducts.products", Strategy: "min", SortField: "price", OnTie: "fail"}},
		Values:     map[string]plan.StepValue{"cheapestSku": {FromSelection: "cheapest.sku"}, "fastestSku": {Default: "x"}},
	}
	_, _, err = ResolveInputs(named, g.Nodes["addItem"], g, tieState())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `selection "cheapest"`)
	assert.Contains(t, err.Error(), "onTie is fail")

	noTie := plan.Step{Node: "addItem", Values: map[string]plan.StepValue{
		"cheapestSku": {From: "listProducts.products", Select: &plan.SelectionConfig{Strategy: "max", SortField: "price", Field: "sku", OnTie: "fail"}},
		"fastestSku":  {Default: "x"},
	}}
	inputs, _, err := ResolveInputs(noTie, g.Nodes["addItem"], g, tieState())
	require.NoError(t, err)
	assert.Equal(t, "c", inputs["cheapestSku"])
}
