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
	inputs, err := ResolveInputs(step, g.Nodes["target"], g, state)
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

	inputs, err := ResolveInputs(step, g.Nodes["target"], g, state)
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

	inputs, err := ResolveInputs(step, g.Nodes["target"], g, state)
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

	inputs, err := ResolveInputs(step, g.Nodes["target"], g, state)
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

	_, err := ResolveInputs(step, g.Nodes["target"], g, state)
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

	inputs, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "first-id", inputs["itemId"])
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

	inputs, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "alpha", inputs["item"])
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
	_, err := ResolveInputs(step, g.Nodes["target"], g, state)
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

	inputs, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "offer-123", inputs["offeringId"])
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

	inputs, err := ResolveInputs(step, g.Nodes["target"], g, state)
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

	inputs, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "deep-value", inputs["deepValue"])
}
