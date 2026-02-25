package engine

import (
	"context"
	"testing"
	"time"

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
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{"value": "hello"})

	step := plan.Step{
		Node:      "target",
		DependsOn: []string{"source"},
		Values: map[string]plan.StepValue{
			"input1": {From: "source.value"},
		},
	}
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
					{Name: "passengers", Type: "integer", Default: graph.LiteralDefault(1)},
				},
			},
		},
	}

	state := NewRunState()
	// After instantiation, graph defaults are in step values
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"passengers": {Default: 1},
		},
	}

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

func TestResolveInputs_EmptyStepValue_OptionalSkipsEdge(t *testing.T) {
	// When a plan sets an optional input to {} (empty StepValue), the engine
	// should skip it rather than resolving via From references.
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
					{Name: "carrier", Type: "string"},
					{Name: "returnCarrier", Type: "string", Optional: true},
				},
			},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{
			map[string]any{"carrier": "UA", "id": "1"},
			map[string]any{"carrier": "DL", "id": "2"},
		},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"carrier": {
				From: "source.items",
				Select: &plan.SelectionConfig{
					Strategy: "first",
					Field:    "carrier",
				},
			},
			"returnCarrier": {}, // empty — should be skipped
		},
	}

	inputs, _, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Contains(t, inputs, "carrier", "non-empty input should resolve via From")
	assert.NotContains(t, inputs, "returnCarrier", "empty StepValue should skip the input")
}

func TestResolveInputs_EmptyStepValue_RequiredErrors(t *testing.T) {
	// An empty StepValue on a required input with no graph default is an error.
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
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"required1": {}, // empty — no resolution source
		},
	}

	_, _, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty step value")
}

func TestResolveInputs_EmptyStepValue_FallsBackToGraphDefault(t *testing.T) {
	// An empty StepValue on a required input WITH a graph default should use the default.
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "contentSource", Type: "string", Default: graph.LiteralDefault("GDS")},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"contentSource": {}, // empty — should fall back to graph default
		},
	}

	inputs, _, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "GDS", inputs["contentSource"])
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
				From: "source.items",
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
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{"alpha", "beta", "gamma"},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"item": {
				From: "source.items",
				Select: &plan.SelectionConfig{
					Strategy: "first",
				},
			},
		},
	}

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
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"itemId": {
				From: "source.items",
				Select: &plan.SelectionConfig{
					Strategy: "first",
				},
			},
		},
	}
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
				From: "source.items",
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
				From: "source.items",
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
				From: "source.items",
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
				From: "source.items",
				Select: &plan.SelectionConfig{
					Strategy: "random",
					Field:    "id",
				},
			},
			"itemName": {
				From: "source.items",
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

// --- coerceValue tests ---

func TestCoerceValue_DateFromDatetime(t *testing.T) {
	result := coerceValue("2026-02-16T00:00:00Z", "date")
	assert.Equal(t, "2026-02-16", result)
}

func TestCoerceValue_DatePassthrough(t *testing.T) {
	result := coerceValue("2026-02-16", "date")
	assert.Equal(t, "2026-02-16", result)
}

func TestCoerceValue_DateFromTimeValue(t *testing.T) {
	tv := time.Date(2026, 2, 16, 14, 30, 0, 0, time.UTC)
	result := coerceValue(tv, "date")
	assert.Equal(t, "2026-02-16", result)
}

func TestCoerceValue_DatetimeFromTimeValue(t *testing.T) {
	tv := time.Date(2026, 2, 16, 14, 30, 0, 0, time.UTC)
	result := coerceValue(tv, "datetime")
	assert.Equal(t, "2026-02-16T14:30:00Z", result)
}

func TestCoerceValue_IntegerFromString(t *testing.T) {
	result := coerceValue("42", "integer")
	assert.Equal(t, 42, result)
}

func TestCoerceValue_IntegerFromFloat(t *testing.T) {
	result := coerceValue(42.0, "integer")
	assert.Equal(t, 42, result)
}

func TestCoerceValue_BoolFromString(t *testing.T) {
	result := coerceValue("true", "boolean")
	assert.Equal(t, true, result)
}

func TestCoerceValue_FloatFromString(t *testing.T) {
	result := coerceValue("3.14", "float")
	assert.Equal(t, 3.14, result)
}

func TestCoerceValue_StringPassthrough(t *testing.T) {
	result := coerceValue("hello", "string")
	assert.Equal(t, "hello", result)
}

func TestCoerceValue_UnknownType(t *testing.T) {
	result := coerceValue("foo", "custom")
	assert.Equal(t, "foo", result)
}

func TestCoerceValue_ParseError(t *testing.T) {
	result := coerceValue("not-a-number", "integer")
	assert.Equal(t, "not-a-number", result)
}

func TestCoerceValue_CaseInsensitive(t *testing.T) {
	result := coerceValue("2026-02-16T00:00:00Z", "Date")
	assert.Equal(t, "2026-02-16", result)
}

// --- ResolveInputsWithContext tests ---

func fixedNow() time.Time {
	return time.Date(2026, 2, 8, 12, 0, 0, 0, time.UTC)
}

func TestResolveInputsWithContext_ExpressionInDefault(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "departureDate", Type: "date"},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"departureDate": {Default: "{{today + 5 days}}"},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	inputs, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	assert.Equal(t, "2026-02-13", inputs["departureDate"])
}

func TestResolveInputsWithContext_ExpressionTodayBare(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "date", Type: "date"},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"date": {Default: "{{today}}"},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	inputs, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	assert.Equal(t, "2026-02-08", inputs["date"])
}

func TestResolveInputsWithContext_ConstraintPasses(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "passengers", Type: "integer"},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"passengers": {
				Default:    2,
				Constraint: "value >= 1 && value <= 9",
			},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	inputs, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	assert.Equal(t, 2, inputs["passengers"])
}

func TestResolveInputsWithContext_ConstraintFails_Pool(t *testing.T) {
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
			"origin": {
				Default:      "INVALID",
				Constraint:   "value != 'INVALID'",
				Pool:         []any{"DEN", "LAX", "ORD"},
				PoolStrategy: strPtr("sequential"),
			},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	inputs, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	assert.Equal(t, "DEN", inputs["origin"]) // first pool value that passes
}

func TestResolveInputsWithContext_PoolSequential(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "code", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"code": {
				Default:      "BAD",
				Constraint:   "value != 'BAD' && value != 'ALSO_BAD'",
				Pool:         []any{"ALSO_BAD", "GOOD", "ALSO_GOOD"},
				PoolStrategy: strPtr("sequential"),
			},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	inputs, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	assert.Equal(t, "GOOD", inputs["code"]) // first pool value that passes
}

func TestResolveInputsWithContext_PoolRandom(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "code", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	strategy := "random"
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"code": {
				Pool:         []any{"A", "B", "C", "D", "E"},
				PoolStrategy: &strategy,
			},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}

	// Run multiple times and verify we get a result from the pool
	seen := make(map[any]bool)
	for i := 0; i < 20; i++ {
		inputs, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
		require.NoError(t, err)
		seen[inputs["code"]] = true
	}
	// With random, we should see multiple values over 20 runs
	assert.Greater(t, len(seen), 1, "expected multiple different values from random pool")
}

func TestResolveInputsWithContext_AllPoolFail(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "code", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"code": {
				Default:    "A",
				Constraint: "value == 'Z'", // nothing will match
				Pool:       []any{"B", "C"},
			},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	_, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed constraint")
}

func TestResolveInputsWithContext_ExpressionInPool(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "date", Type: "date"},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"date": {
				Pool:         []any{"{{today + 5 days}}", "{{today + 10 days}}"},
				PoolStrategy: strPtr("sequential"),
			},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	inputs, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	assert.Equal(t, "2026-02-13", inputs["date"]) // first pool value
}

func TestResolveInputsWithContext_NilContextSameAsBefore(t *testing.T) {
	// Verify that passing nil rctx gives the same result as ResolveInputs
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

	inputs1, _, err1 := ResolveInputs(step, g.Nodes["target"], g, state)
	inputs2, _, _, err2 := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, nil)
	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Equal(t, inputs1, inputs2)
}

func TestResolveInputsWithContext_MixedInputs(t *testing.T) {
	// Some inputs use expressions/fallback, others are plain
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{Name: "sessionId", Type: "string"},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "sessionId", Type: "string"},
					{Name: "departureDate", Type: "date"},
					{Name: "origin", Type: "string"},
					{Name: "passengers", Type: "integer"},
				},
			},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{"sessionId": "sess-123"})

	step := plan.Step{
		Node:      "target",
		DependsOn: []string{"source"},
		Values: map[string]plan.StepValue{
			"sessionId":     {From: "source.sessionId"},
			"departureDate": {Default: "{{today + 5 days}}"},
			"origin":        {Default: "DEN"},
			"passengers": {
				Default:      10,
				Constraint:   "value <= 9",
				Pool:         []any{1, 2, 3},
				PoolStrategy: strPtr("sequential"),
			},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	inputs, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	assert.Equal(t, "sess-123", inputs["sessionId"])       // from plan From ref
	assert.Equal(t, "2026-02-13", inputs["departureDate"]) // from expression
	assert.Equal(t, "DEN", inputs["origin"])               // plain default
	assert.Equal(t, 1, inputs["passengers"])               // from pool (default failed constraint)
}

func TestResolveInputsWithContext_ReferenceEarlierInput(t *testing.T) {
	// Test that later inputs can reference earlier ones via expressions
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "departureDate", Type: "date"},
					{Name: "returnDate", Type: "date"},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"departureDate": {Default: "{{today + 5 days}}"},
			"returnDate":    {Default: "{{departureDate + 7 days}}"},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	inputs, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	assert.Equal(t, "2026-02-13", inputs["departureDate"])
	assert.Equal(t, "2026-02-20", inputs["returnDate"])
}

func TestResolveInputsWithContext_ConstraintFailsNoPool(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "code", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"code": {
				Default:    "BAD",
				Constraint: "value == 'GOOD'",
			},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	_, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed constraint")
}

func TestResolveInputsWithContext_EnvExpression(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "apiKey", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"apiKey": {Default: "{{env.TEST_KEY}}"},
		},
	}

	rctx := &ResolveContext{
		Now: fixedNow(),
		EnvLookup: func(key string) string {
			if key == "TEST_KEY" {
				return "secret-123"
			}
			return ""
		},
	}
	inputs, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	assert.Equal(t, "secret-123", inputs["apiKey"])
}

func TestResolveInputsWithContext_NoDefaultOnlyPool(t *testing.T) {
	// StepValue with no Default but a Pool
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "code", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"code": {
				Pool:         []any{"DEN", "LAX", "ORD"},
				PoolStrategy: strPtr("sequential"),
			},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	inputs, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	assert.Equal(t, "DEN", inputs["code"]) // first pool value
}

// --- evaluateValue / checkConstraint unit tests ---

func TestEvaluateValue_NonString(t *testing.T) {
	ectx := plan.ExprContext{Now: fixedNow()}
	val, err := evaluateValue(42, ectx)
	require.NoError(t, err)
	assert.Equal(t, 42, val)
}

func TestEvaluateValue_NoExpr(t *testing.T) {
	ectx := plan.ExprContext{Now: fixedNow()}
	val, err := evaluateValue("DEN", ectx)
	require.NoError(t, err)
	assert.Equal(t, "DEN", val)
}

func TestCheckConstraint_Empty(t *testing.T) {
	ok, err := checkConstraint("", "anything", nil)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestCheckConstraint_Passes(t *testing.T) {
	ok, err := checkConstraint("value > 0", 5, map[string]any{})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestCheckConstraint_Fails(t *testing.T) {
	ok, err := checkConstraint("value > 10", 5, map[string]any{})
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestCheckConstraint_WithOtherInputs(t *testing.T) {
	ok, err := checkConstraint("value != origin", "LAX", map[string]any{"origin": "DEN"})
	require.NoError(t, err)
	assert.True(t, ok)
}

// --- ValueResolution record tests ---

func TestResolution_EdgeSource(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name:    "source",
				Outputs: []graph.Output{{Name: "val", Type: "string"}},
			},
			"target": {
				Name:   "target",
				Inputs: []graph.Input{{Name: "input1", Type: "string"}},
			},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{"val": "hello"})
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"input1": {From: "source.val"},
		},
	}

	_, _, resolutions, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, nil)
	require.NoError(t, err)
	require.Len(t, resolutions, 1)
	assert.Equal(t, "input1", resolutions[0].InputName)
	assert.Equal(t, "plan_from", resolutions[0].Source)
	assert.Equal(t, "source", resolutions[0].FromStep)
	assert.Equal(t, "val", resolutions[0].FromOutput)
	assert.Equal(t, "hello", resolutions[0].FinalValue)
}

func TestResolution_PlanDefault(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name:   "target",
				Inputs: []graph.Input{{Name: "origin", Type: "string"}},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node:   "target",
		Values: map[string]plan.StepValue{"origin": {Default: "DEN"}},
	}

	_, _, resolutions, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, nil)
	require.NoError(t, err)
	require.Len(t, resolutions, 1)
	assert.Equal(t, "origin", resolutions[0].InputName)
	assert.Equal(t, "plan_default", resolutions[0].Source)
	assert.Equal(t, "DEN", resolutions[0].FinalValue)
	assert.Equal(t, "DEN", resolutions[0].RawValue)
}

func TestResolution_ExpressionSource(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name:   "target",
				Inputs: []graph.Input{{Name: "date", Type: "date"}},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node:   "target",
		Values: map[string]plan.StepValue{"date": {Default: "{{today + 5 days}}"}},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	_, _, resolutions, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	require.Len(t, resolutions, 1)
	assert.Equal(t, "expression", resolutions[0].Source)
	assert.Equal(t, "{{today + 5 days}}", resolutions[0].Expression)
	assert.Equal(t, "{{today + 5 days}}", resolutions[0].RawValue)
	assert.Equal(t, "2026-02-13", resolutions[0].FinalValue)
}

func TestResolution_Pool(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name:   "target",
				Inputs: []graph.Input{{Name: "code", Type: "string"}},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"code": {
				Default:      "BAD",
				Constraint:   "value != 'BAD' && value != 'ALSO_BAD'",
				Pool:         []any{"ALSO_BAD", "GOOD", "ALSO_GOOD"},
				PoolStrategy: strPtr("sequential"),
			},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	_, _, resolutions, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	require.Len(t, resolutions, 1)
	r := resolutions[0]
	assert.Equal(t, "fallback_pool", r.Source)
	assert.Equal(t, "GOOD", r.FinalValue)
	assert.Equal(t, 1, r.PoolIndex)
	assert.Equal(t, 3, r.PoolSize)
	assert.True(t, r.ConstraintOK)
	assert.Equal(t, "value != 'BAD' && value != 'ALSO_BAD'", r.Constraint)
	// "BAD" (default) and "ALSO_BAD" (pool[0]) were tried before "GOOD"
	assert.Len(t, r.Tried, 2)
}

func TestResolution_GraphDefault(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name:   "target",
				Inputs: []graph.Input{{Name: "count", Type: "integer", Default: graph.LiteralDefault(1)}},
			},
		},
	}

	state := NewRunState()
	// After instantiation, graph defaults are in step values
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"count": {Default: 1},
		},
	}

	_, _, resolutions, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, nil)
	require.NoError(t, err)
	require.Len(t, resolutions, 1)
	assert.Equal(t, "plan_default", resolutions[0].Source)
	assert.Equal(t, 1, resolutions[0].FinalValue)
}

func TestResolution_OptionalSkip(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name:   "target",
				Inputs: []graph.Input{{Name: "opt", Type: "string", Optional: true}},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{Node: "target"}

	_, _, resolutions, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, nil)
	require.NoError(t, err)
	require.Len(t, resolutions, 1)
	assert.Equal(t, "optional_skip", resolutions[0].Source)
}

func TestResolution_SelectEdge(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name:    "source",
				Outputs: []graph.Output{{Name: "items", Type: "item[]"}},
			},
			"target": {
				Name:   "target",
				Inputs: []graph.Input{{Name: "itemId", Type: "string"}},
			},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{
			map[string]any{"id": "first"},
			map[string]any{"id": "second"},
		},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"itemId": {From: "source.items", Select: &plan.SelectionConfig{Strategy: "first", Field: "id"}},
		},
	}

	_, _, resolutions, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, nil)
	require.NoError(t, err)
	require.Len(t, resolutions, 1)
	assert.Equal(t, "select_edge", resolutions[0].Source)
	assert.Equal(t, "source", resolutions[0].FromStep)
	assert.Equal(t, "items", resolutions[0].FromOutput)
	assert.Equal(t, "first", resolutions[0].FinalValue)
}

// --- ElementField name resolution tests ---

func TestResolveInputs_SelectEdge_ElementFieldName(t *testing.T) {
	// Plan uses elementField name "offeringId" instead of raw gjson path "id".
	// Engine should resolve "offeringId" → the gjson path via the graph.
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{
						Name: "items",
						Type: "item[]",
						ElementFields: []graph.Field{
							{Name: "offeringId", Type: "string", Path: "id"},
							{Name: "price", Type: "number", Path: "details.price"},
						},
					},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "selectedId", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{
			map[string]any{"id": "offer-1", "details": map[string]any{"price": 100}},
			map[string]any{"id": "offer-2", "details": map[string]any{"price": 200}},
		},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"selectedId": {
				From: "source.items",
				Select: &plan.SelectionConfig{
					Strategy: "first",
					Field:    "offeringId", // elementField name, not raw "id"
				},
			},
		},
	}

	inputs, decisions, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "offer-1", inputs["selectedId"])
	require.Len(t, decisions, 1)
	assert.Equal(t, 0, decisions[0].SelectedIndex)
}

func TestResolveInputs_SelectEdge_ElementFieldName_BackwardCompat(t *testing.T) {
	// Raw gjson path should still work even when elementFields are defined.
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{
						Name: "items",
						Type: "item[]",
						ElementFields: []graph.Field{
							{Name: "offeringId", Type: "string", Path: "id"},
						},
					},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "selectedId", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{
			map[string]any{"id": "offer-1"},
			map[string]any{"id": "offer-2"},
		},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"selectedId": {
				From: "source.items",
				Select: &plan.SelectionConfig{
					Strategy: "first",
					Field:    "id", // raw gjson path — not an elementField name
				},
			},
		},
	}

	inputs, _, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "offer-1", inputs["selectedId"])
}

func TestResolveInputs_PlanFromSelect_ElementFieldName(t *testing.T) {
	// Plan uses From + Select with elementField name.
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{
						Name: "offerings",
						Type: "offering[]",
						ElementFields: []graph.Field{
							{Name: "offeringId", Type: "string", Path: "Identifier.value"},
						},
					},
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
					Field:    "offeringId", // elementField name → resolves to "Identifier.value"
				},
			},
		},
	}

	inputs, decisions, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "offer-123", inputs["offeringId"])
	require.Len(t, decisions, 1)
}

func TestResolveInputs_MinSortField_ElementFieldName(t *testing.T) {
	// SortField as elementField name.
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{
						Name: "items",
						Type: "item[]",
						ElementFields: []graph.Field{
							{Name: "itemId", Type: "string", Path: "id"},
							{Name: "cost", Type: "number", Path: "pricing.amount"},
						},
					},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "selectedId", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{
			map[string]any{"id": "expensive", "pricing": map[string]any{"amount": 300.0}},
			map[string]any{"id": "cheapest", "pricing": map[string]any{"amount": 100.0}},
			map[string]any{"id": "mid", "pricing": map[string]any{"amount": 200.0}},
		},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"selectedId": {
				From: "source.items",
				Select: &plan.SelectionConfig{
					Strategy:  "min",
					Field:     "itemId", // elementField → "id"
					SortField: "cost",   // elementField → "pricing.amount"
				},
			},
		},
	}

	inputs, decisions, err := ResolveInputs(step, g.Nodes["target"], g, state)
	require.NoError(t, err)
	assert.Equal(t, "cheapest", inputs["selectedId"])
	require.Len(t, decisions, 1)
	assert.Equal(t, "min", decisions[0].Strategy)
}

func TestResolveElementFieldPath_Unit(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{
						Name: "items",
						Type: "item[]",
						ElementFields: []graph.Field{
							{Name: "offeringId", Type: "string", Path: "id"},
							{Name: "productRef", Type: "string", Path: "ProductBrandOptions.0.ProductBrandOffering.0.Product.0.productRef"},
						},
					},
				},
			},
		},
	}

	// No registry in rctx → falls through to graph paths
	noReg := (*ResolveContext)(nil)

	// Matches elementField name → returns gjson path
	assert.Equal(t, "id", resolveElementFieldPath(noReg, g, "source", "items", "offeringId"))
	assert.Equal(t, "ProductBrandOptions.0.ProductBrandOffering.0.Product.0.productRef",
		resolveElementFieldPath(noReg, g, "source", "items", "productRef"))

	// No match → returns the fieldRef unchanged
	assert.Equal(t, "someOtherField", resolveElementFieldPath(noReg, g, "source", "items", "someOtherField"))

	// Nil graph → returns fieldRef unchanged
	assert.Equal(t, "offeringId", resolveElementFieldPath(noReg, nil, "source", "items", "offeringId"))

	// Unknown node → returns fieldRef unchanged
	assert.Equal(t, "offeringId", resolveElementFieldPath(noReg, g, "unknown", "items", "offeringId"))

	// Unknown output → returns fieldRef unchanged
	assert.Equal(t, "offeringId", resolveElementFieldPath(noReg, g, "source", "unknown", "offeringId"))
}

// --- Named Selection tests ---

func TestResolveInputs_NamedSelection_FirstStrategy(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{
						Name: "items",
						Type: "item[]",
						ElementFields: []graph.Field{
							{Name: "itemId", Type: "string", Path: "id"},
							{Name: "itemName", Type: "string", Path: "name"},
						},
					},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "selectedId", Type: "string"},
					{Name: "selectedName", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{
			map[string]any{"id": "id-1", "name": "first-item"},
			map[string]any{"id": "id-2", "name": "second-item"},
		},
	})

	step := plan.Step{
		Node: "target",
		Selections: map[string]plan.StepSelection{
			"item": {
				From:     "source.items",
				Strategy: "first",
			},
		},
		Values: map[string]plan.StepValue{
			"selectedId":   {FromSelection: "item.itemId"},
			"selectedName": {FromSelection: "item.itemName"},
		},
	}

	inputs, decisions, resolutions, err := ResolveInputsWithContext(
		context.Background(), step, g.Nodes["target"], g, state, nil,
	)
	require.NoError(t, err)

	// Both fields from the same element
	assert.Equal(t, "id-1", inputs["selectedId"])
	assert.Equal(t, "first-item", inputs["selectedName"])

	// One decision for the named selection + one per value
	require.GreaterOrEqual(t, len(decisions), 1)
	// The named selection decision
	found := false
	for _, d := range decisions {
		if d.SelectionName == "item" {
			found = true
			assert.Equal(t, "first", d.Strategy)
			assert.Equal(t, "source", d.SourceNode)
			break
		}
	}
	assert.True(t, found, "expected named selection decision")

	// Resolutions for both values
	require.Len(t, resolutions, 2)
	for _, r := range resolutions {
		assert.Equal(t, "named_selection", r.Source)
		assert.Equal(t, "source", r.FromStep)
		assert.Equal(t, "items", r.FromOutput)
	}
}

func TestResolveInputs_NamedSelection_WholeElement(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name:    "source",
				Outputs: []graph.Output{{Name: "items", Type: "item[]"}},
			},
			"target": {
				Name:   "target",
				Inputs: []graph.Input{{Name: "item", Type: "object"}},
			},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{
			map[string]any{"id": "first"},
		},
	})

	step := plan.Step{
		Node: "target",
		Selections: map[string]plan.StepSelection{
			"result": {From: "source.items", Strategy: "first"},
		},
		Values: map[string]plan.StepValue{
			"item": {FromSelection: "result"}, // no dot → whole element
		},
	}

	inputs, _, _, err := ResolveInputsWithContext(
		context.Background(), step, g.Nodes["target"], g, state, nil,
	)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"id": "first"}, inputs["item"])
}

func TestResolveInputs_NamedSelection_MixedWithOldStyle(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{
						Name: "items",
						Type: "item[]",
						ElementFields: []graph.Field{
							{Name: "itemId", Type: "string", Path: "id"},
						},
					},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "selectedId", Type: "string"},
					{Name: "label", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{
			map[string]any{"id": "id-1"},
		},
	})

	step := plan.Step{
		Node: "target",
		Selections: map[string]plan.StepSelection{
			"item": {From: "source.items", Strategy: "first"},
		},
		Values: map[string]plan.StepValue{
			"selectedId": {FromSelection: "item.itemId"},
			"label":      {Default: "my-label"}, // old-style value
		},
	}

	inputs, _, resolutions, err := ResolveInputsWithContext(
		context.Background(), step, g.Nodes["target"], g, state, nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "id-1", inputs["selectedId"])
	assert.Equal(t, "my-label", inputs["label"])

	// Check resolution sources
	sources := map[string]string{}
	for _, r := range resolutions {
		sources[r.InputName] = r.Source
	}
	assert.Equal(t, "named_selection", sources["selectedId"])
	assert.Equal(t, "plan_default", sources["label"])
}

func TestResolveInputs_NamedSelection_UnknownName(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name:   "target",
				Inputs: []graph.Input{{Name: "val", Type: "string"}},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		// No Selections defined
		Values: map[string]plan.StepValue{
			"val": {FromSelection: "missing.field"},
		},
	}

	_, _, _, err := ResolveInputsWithContext(
		context.Background(), step, g.Nodes["target"], g, state, nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown selection")
}

func TestResolveInputs_NamedSelection_FieldNotFound(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"source": {
				Name:    "source",
				Outputs: []graph.Output{{Name: "items", Type: "item[]"}},
			},
			"target": {
				Name:   "target",
				Inputs: []graph.Input{{Name: "val", Type: "string"}},
			},
		},
	}

	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{
			map[string]any{"id": "id-1"},
		},
	})

	step := plan.Step{
		Node: "target",
		Selections: map[string]plan.StepSelection{
			"item": {From: "source.items", Strategy: "first"},
		},
		Values: map[string]plan.StepValue{
			"val": {FromSelection: "item.nonexistent"},
		},
	}

	_, _, _, err := ResolveInputsWithContext(
		context.Background(), step, g.Nodes["target"], g, state, nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in element")
}

func TestResolveInputsWithContext_CancelledContext(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name: "step1",
				Inputs: []graph.Input{
					{Name: "a", Type: "string"},
					{Name: "b", Type: "string"},
					{Name: "c", Type: "string"},
				},
			},
		},
	}

	step := plan.Step{
		Node: "step1",
		Values: map[string]plan.StepValue{
			"a": {Default: "val-a"},
			"b": {Default: "val-b"},
			"c": {Default: "val-c"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel

	_, _, _, err := ResolveInputsWithContext(ctx, step, g.Nodes["step1"], g, NewRunState(), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "cancelled")
}

// --- FromResolved tests ---

func TestResolveInputs_FromResolved_Basic(t *testing.T) {
	// Input B references input A via fromResolved, A resolves from pool
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "leg1Destination", Type: "string"},
					{Name: "leg2Origin", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"leg1Destination": {Pool: []any{"SFO", "LAX", "SEA"}, PoolStrategy: strPtr("sequential")},
			"leg2Origin":      {FromResolved: "leg1Destination"},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	inputs, _, resolutions, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	assert.Equal(t, "SFO", inputs["leg1Destination"])
	assert.Equal(t, "SFO", inputs["leg2Origin"])

	// Check resolution record
	sources := map[string]string{}
	for _, r := range resolutions {
		sources[r.InputName] = r.Source
	}
	assert.Equal(t, "from_resolved", sources["leg2Origin"])
}

func TestResolveInputs_FromResolved_WithConstraint(t *testing.T) {
	// A=pool, B=fromResolved(A), C=pool with constraint "value != leg2Origin"
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "leg1Destination", Type: "string"},
					{Name: "leg2Origin", Type: "string"},
					{Name: "leg2Destination", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"leg1Destination": {Default: "SFO"},
			"leg2Origin":      {FromResolved: "leg1Destination"},
			"leg2Destination": {
				Pool:         []any{"SFO", "LAX", "SEA"},
				PoolStrategy: strPtr("sequential"),
				Constraint:   "value != leg2Origin",
			},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	inputs, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	assert.Equal(t, "SFO", inputs["leg1Destination"])
	assert.Equal(t, "SFO", inputs["leg2Origin"])
	// SFO fails constraint (== leg2Origin), so first passing pool value is LAX
	assert.Equal(t, "LAX", inputs["leg2Destination"])
}

func TestResolveInputs_FromResolved_Unresolved(t *testing.T) {
	// fromResolved references an input that doesn't exist in resolvedInputs
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "a", Type: "string"},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"a": {FromResolved: "nonexistent"},
		},
	}

	rctx := &ResolveContext{Now: fixedNow()}
	_, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fromResolved references")
	assert.Contains(t, err.Error(), "not been resolved yet")
}

func strPtr(s string) *string { return &s }
