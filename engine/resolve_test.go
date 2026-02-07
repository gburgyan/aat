package engine

import (
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveInputs_UpstreamOutputViaEdge(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{Name: "value", Type: "string"},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "input1", Type: "string"},
				},
			},
		},
		Edges: []graph.Edge{
			{From: "source.value", To: "target.input1"},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{"value": "hello"})

	step := plan.Step{Node: "target"}
	inputs, _, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "hello", inputs["input1"])
}

func TestResolveInputs_PlanDefault(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"origin": {Default: "DEN"},
		},
	}

	inputs, _, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "DEN", inputs["origin"])
}

func TestResolveInputs_GraphNodeDefault(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "passengers", Type: "integer", Default: 1},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{Node: "target"}

	inputs, _, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, 1, inputs["passengers"])
}

func TestResolveInputs_OptionalSkip(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "optional1", Type: "string", Optional: true},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{Node: "target"}

	inputs, _, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.NotContains(t, inputs, "optional1")
}

func TestResolveInputs_MissingRequired(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "required1", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{Node: "target"}

	_, _, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required input has no value")
}

func TestResolveInputs_SelectEdge_FirstElement(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{Name: "items", Type: "item[]"},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "itemId", Type: "string"},
				},
			},
		},
		Edges: []graph.Edge{
			{From: "source.items", To: "target.itemId", Select: true},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{
			map[string]any{"id": "first-id", "name": "First"},
			map[string]any{"id": "second-id", "name": "Second"},
		},
	})

	// With plan-level field extraction
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"itemId": {
				Select: &plan.SelectionConfig{
					Strategy: "first",
					Field:    "id",
				},
			},
		},
	}

	inputs, decisions, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "first-id", inputs["itemId"])
	require.Len(t, decisions, 1)
	assert.Equal(t, "itemId", decisions[0].InputName)
	assert.Equal(t, "first", decisions[0].Strategy)
	assert.Equal(t, 0, decisions[0].SelectedIndex)
	assert.Equal(t, 2, decisions[0].SourceSize)
}

func TestResolveInputs_SelectEdge_NoFieldExtraction(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{Name: "items", Type: "item[]"},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "item", Type: "string"},
				},
			},
		},
		Edges: []graph.Edge{
			{From: "source.items", To: "target.item", Select: true},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{"alpha", "beta", "gamma"},
	})

	step := plan.Step{Node: "target"}

	inputs, decisions, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "alpha", inputs["item"])
	require.Len(t, decisions, 1)
	assert.Equal(t, "first", decisions[0].Strategy) // default strategy
}

func TestResolveInputs_SelectEdge_EmptyArray(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{Name: "items", Type: "item[]"},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "itemId", Type: "string"},
				},
			},
		},
		Edges: []graph.Edge{
			{From: "source.items", To: "target.itemId", Select: true},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{},
	})

	step := plan.Step{Node: "target"}
	_, _, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "array is empty")
}

func TestResolveInputs_PlanFromSelect(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{Name: "offerings", Type: "offering[]"},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "offeringId", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"offerings": []any{
			map[string]any{"Identifier": map[string]any{"value": "offer-123"}},
			map[string]any{"Identifier": map[string]any{"value": "offer-456"}},
		},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"offeringId": {
				From: "source.offerings",
				Select: &plan.SelectionConfig{
					Strategy: "first",
					Field:    "Identifier.value",
				},
			},
		},
	}

	inputs, decisions, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "offer-123", inputs["offeringId"])
	require.Len(t, decisions, 1)
	assert.Equal(t, "source", decisions[0].SourceNode)
	assert.Equal(t, "offerings", decisions[0].SourceField)
}

func TestResolveInputs_PriorityOrder(t *testing.T) {
	// Edge should take priority over plan default
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{Name: "val", Type: "string"},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "input1", Type: "string", Default: "graph-default"},
				},
			},
		},
		Edges: []graph.Edge{
			{From: "source.val", To: "target.input1"},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{"val": "from-edge"})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"input1": {Default: "from-plan"},
		},
	}

	inputs, _, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "from-edge", inputs["input1"])
}

func TestResolveInputs_NestedFieldExtraction(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "deepValue", Type: "string"},
				},
			},
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{Name: "data", Type: "item[]"},
				},
			},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"data": []any{
			map[string]any{
				"level1": map[string]any{
					"level2": map[string]any{
						"target": "deep-value",
					},
				},
			},
		},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"deepValue": {
				From: "source.data",
				Select: &plan.SelectionConfig{
					Strategy: "first",
					Field:    "level1.level2.target",
				},
			},
		},
	}

	inputs, _, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "deep-value", inputs["deepValue"])
}

// --- Strategy integration tests through ResolveInputs ---

func TestResolveInputs_SelectEdge_LastStrategy(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{Name: "items", Type: "item[]"},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "itemId", Type: "string"},
				},
			},
		},
		Edges: []graph.Edge{
			{From: "source.items", To: "target.itemId", Select: true},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{
			map[string]any{"id": "first"},
			map[string]any{"id": "second"},
			map[string]any{"id": "third"},
		},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"itemId": {
				Select: &plan.SelectionConfig{
					Strategy: "last",
					Field:    "id",
				},
			},
		},
	}

	inputs, decisions, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "third", inputs["itemId"])
	require.Len(t, decisions, 1)
	assert.Equal(t, "last", decisions[0].Strategy)
	assert.Equal(t, 2, decisions[0].SelectedIndex)
}

func TestResolveInputs_SelectEdge_MinStrategy(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{Name: "items", Type: "item[]"},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "itemId", Type: "string"},
				},
			},
		},
		Edges: []graph.Edge{
			{From: "source.items", To: "target.itemId", Select: true},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{
			map[string]any{"id": "expensive", "price": 300.0},
			map[string]any{"id": "cheapest", "price": 100.0},
			map[string]any{"id": "mid", "price": 200.0},
		},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"itemId": {
				Select: &plan.SelectionConfig{
					Strategy:  "min",
					Field:     "id",
					SortField: "price",
				},
			},
		},
	}

	inputs, decisions, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "cheapest", inputs["itemId"])
	require.Len(t, decisions, 1)
	assert.Equal(t, "min", decisions[0].Strategy)
}

func TestResolveInputs_SelectEdge_MatchStrategy(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{Name: "items", Type: "item[]"},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "itemId", Type: "string"},
				},
			},
		},
		Edges: []graph.Edge{
			{From: "source.items", To: "target.itemId", Select: true},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{
			map[string]any{"id": "aa-1", "carrier": "AA"},
			map[string]any{"id": "ua-1", "carrier": "UA"},
			map[string]any{"id": "aa-2", "carrier": "AA"},
		},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"itemId": {
				Select: &plan.SelectionConfig{
					Strategy: "match",
					Field:    "id",
					Filter:   "carrier == 'UA'",
				},
			},
		},
	}

	inputs, decisions, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "ua-1", inputs["itemId"])
	require.Len(t, decisions, 1)
	assert.Equal(t, "match", decisions[0].Strategy)
	assert.Equal(t, 1, decisions[0].SelectedIndex)
}

func TestResolveInputs_PlanFromSelect_WithStrategy(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{Name: "offerings", Type: "offering[]"},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "offeringId", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"offerings": []any{
			map[string]any{"id": "offer-1", "price": 500.0},
			map[string]any{"id": "offer-2", "price": 200.0},
			map[string]any{"id": "offer-3", "price": 800.0},
		},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"offeringId": {
				From: "source.offerings",
				Select: &plan.SelectionConfig{
					Strategy:  "max",
					Field:     "id",
					SortField: "price",
				},
			},
		},
	}

	inputs, decisions, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "offer-3", inputs["offeringId"])
	require.Len(t, decisions, 1)
	assert.Equal(t, "max", decisions[0].Strategy)
}

func TestResolveInputs_Dedup_SameSource_SameStrategy(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{Name: "items", Type: "item[]"},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "itemId", Type: "string"},
					{Name: "itemName", Type: "string"},
				},
			},
		},
		Edges: []graph.Edge{
			{From: "source.items", To: "target.itemId", Select: true},
			{From: "source.items", To: "target.itemName", Select: true},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{
			map[string]any{"id": "id-1", "name": "First"},
			map[string]any{"id": "id-2", "name": "Second"},
			map[string]any{"id": "id-3", "name": "Third"},
		},
	})

	// Both inputs use random strategy from same source — should select same element
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"itemId": {
				Select: &plan.SelectionConfig{
					Strategy: "random",
					Field:    "id",
				},
			},
			"itemName": {
				Select: &plan.SelectionConfig{
					Strategy: "random",
					Field:    "name",
				},
			},
		},
	}

	inputs, decisions, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)

	// Both should come from the same element due to dedup
	require.Len(t, decisions, 2)
	assert.Equal(t, decisions[0].SelectedIndex, decisions[1].SelectedIndex)

	// Verify they correspond to the same element
	idx := decisions[0].SelectedIndex
	items := state.outputs["source"]["items"].([]any)
	selectedItem := items[idx].(map[string]any)
	assert.Equal(t, selectedItem["id"], inputs["itemId"])
	assert.Equal(t, selectedItem["name"], inputs["itemName"])
}
