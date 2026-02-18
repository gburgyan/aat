package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMSelectElement_StrictModeError(t *testing.T) {
	stub := &stubLLMClient{responses: []string{"0"}}
	rctx := &ResolveContext{
		Mode: config.ModeStrict,
		LLM:  stub,
	}
	arr := []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}}
	sel := &plan.SelectionConfig{Strategy: "llm", Prompt: "pick the first"}

	_, _, err := llmSelectElement(context.Background(), rctx, arr, sel, "itemId", nil, "source", "items")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strict mode")
	assert.Equal(t, 0, stub.calls)
}

func TestLLMSelectElement_LeanModeSuccess(t *testing.T) {
	stub := &stubLLMClient{responses: []string{"1"}}
	rctx := &ResolveContext{
		Mode: config.ModeLean,
		LLM:  stub,
	}
	arr := []any{
		map[string]any{"id": "a", "price": 100},
		map[string]any{"id": "b", "price": 50},
	}
	sel := &plan.SelectionConfig{Strategy: "llm", Prompt: "pick the cheapest"}

	result, rec, err := llmSelectElement(context.Background(), rctx, arr, sel, "itemId", nil, "source", "items")
	require.NoError(t, err)
	assert.Equal(t, 1, result.index)
	assert.Equal(t, arr[1], result.element)
	assert.Equal(t, 2, result.filteredSize)
	require.NotNil(t, rec)
	assert.Equal(t, "1", rec.Response)
	assert.Len(t, rec.Messages, 2)
	assert.Equal(t, 1, stub.calls)
}

func TestLLMSelectElement_AdaptiveModeSuccess(t *testing.T) {
	stub := &stubLLMClient{responses: []string{"0"}}
	rctx := &ResolveContext{
		Mode: config.ModeAdaptive,
		LLM:  stub,
	}
	arr := []any{map[string]any{"id": "only"}}
	sel := &plan.SelectionConfig{Strategy: "llm", Prompt: "pick one"}

	result, rec, err := llmSelectElement(context.Background(), rctx, arr, sel, "itemId", nil, "source", "items")
	require.NoError(t, err)
	assert.Equal(t, 0, result.index)
	require.NotNil(t, rec)
}

func TestLLMSelectElement_NoClientError(t *testing.T) {
	rctx := &ResolveContext{
		Mode: config.ModeLean,
		LLM:  nil,
	}
	arr := []any{map[string]any{"id": "a"}}
	sel := &plan.SelectionConfig{Strategy: "llm", Prompt: "pick one"}

	_, _, err := llmSelectElement(context.Background(), rctx, arr, sel, "itemId", nil, "source", "items")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no LLM client")
}

func TestLLMSelectElement_NilResolveContext(t *testing.T) {
	arr := []any{map[string]any{"id": "a"}}
	sel := &plan.SelectionConfig{Strategy: "llm", Prompt: "pick one"}

	_, _, err := llmSelectElement(context.Background(), nil, arr, sel, "itemId", nil, "source", "items")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no resolve context")
}

func TestLLMSelectElement_InvalidIndex(t *testing.T) {
	stub := &stubLLMClient{responses: []string{"5"}}
	rctx := &ResolveContext{
		Mode: config.ModeLean,
		LLM:  stub,
	}
	arr := []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}}
	sel := &plan.SelectionConfig{Strategy: "llm", Prompt: "pick one"}

	_, rec, err := llmSelectElement(context.Background(), rctx, arr, sel, "itemId", nil, "source", "items")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of bounds")
	require.NotNil(t, rec)
	assert.NotEmpty(t, rec.Error)
}

func TestLLMSelectElement_JSONIndexResponse(t *testing.T) {
	stub := &stubLLMClient{responses: []string{`{"index": 1}`}}
	rctx := &ResolveContext{
		Mode: config.ModeLean,
		LLM:  stub,
	}
	arr := []any{
		map[string]any{"id": "a"},
		map[string]any{"id": "b"},
		map[string]any{"id": "c"},
	}
	sel := &plan.SelectionConfig{Strategy: "llm", Prompt: "pick b"}

	result, _, err := llmSelectElement(context.Background(), rctx, arr, sel, "itemId", nil, "source", "items")
	require.NoError(t, err)
	assert.Equal(t, 1, result.index)
}

func TestLLMSelectElement_UnparsableResponse(t *testing.T) {
	stub := &stubLLMClient{responses: []string{"the second one please"}}
	rctx := &ResolveContext{
		Mode: config.ModeLean,
		LLM:  stub,
	}
	arr := []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}}
	sel := &plan.SelectionConfig{Strategy: "llm", Prompt: "pick one"}

	_, rec, err := llmSelectElement(context.Background(), rctx, arr, sel, "itemId", nil, "source", "items")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not parse index")
	require.NotNil(t, rec)
}

func TestLLMSelectElement_FilterPlusLLM(t *testing.T) {
	stub := &stubLLMClient{responses: []string{"0"}}
	rctx := &ResolveContext{
		Mode: config.ModeLean,
		LLM:  stub,
	}
	arr := []any{
		map[string]any{"id": "a1", "carrier": "AA"},
		map[string]any{"id": "u1", "carrier": "UA"},
		map[string]any{"id": "a2", "carrier": "AA"},
	}
	sel := &plan.SelectionConfig{
		Strategy: "llm",
		Prompt:   "pick the best AA flight",
		Filter:   "carrier == 'AA'",
	}

	result, _, err := llmSelectElement(context.Background(), rctx, arr, sel, "itemId", nil, "source", "items")
	require.NoError(t, err)
	// After filtering, only AA flights remain: a1 and a2
	// LLM returns 0, so we get a1
	assert.Equal(t, 0, result.index)
	elem := result.element.(map[string]any)
	assert.Equal(t, "a1", elem["id"])
	assert.Equal(t, 2, result.filteredSize)
}

func TestLLMSelectElement_FilterMatchesNone(t *testing.T) {
	stub := &stubLLMClient{responses: []string{"0"}}
	rctx := &ResolveContext{
		Mode: config.ModeLean,
		LLM:  stub,
	}
	arr := []any{
		map[string]any{"id": "a1", "carrier": "AA"},
	}
	sel := &plan.SelectionConfig{
		Strategy: "llm",
		Prompt:   "pick one",
		Filter:   "carrier == 'DL'",
	}

	_, _, err := llmSelectElement(context.Background(), rctx, arr, sel, "itemId", nil, "source", "items")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "matched no elements")
	assert.Equal(t, 0, stub.calls) // LLM should not be called
}

func TestLLMSelectElement_LLMCallError(t *testing.T) {
	stub := &stubLLMClient{responses: nil} // no responses → error
	rctx := &ResolveContext{
		Mode: config.ModeLean,
		LLM:  stub,
	}
	arr := []any{map[string]any{"id": "a"}}
	sel := &plan.SelectionConfig{Strategy: "llm", Prompt: "pick one"}

	_, rec, err := llmSelectElement(context.Background(), rctx, arr, sel, "itemId", nil, "source", "items")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LLM element selection")
	require.NotNil(t, rec)
	assert.NotEmpty(t, rec.Error)
}

func TestLLMSelectElement_WithElementFields(t *testing.T) {
	stub := &stubLLMClient{responses: []string{"1"}}
	rctx := &ResolveContext{
		Mode: config.ModeLean,
		LLM:  stub,
	}
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"source": {
				Name: "source",
				Outputs: []graph.Output{
					{
						Name: "items",
						Type: "item[]",
						ElementFields: []graph.Field{
							{Name: "id", Type: "string"},
							{Name: "price", Type: "float"},
						},
					},
				},
			},
		},
	}
	arr := []any{
		map[string]any{"id": "a", "price": 100.0, "extraData": "lots of stuff"},
		map[string]any{"id": "b", "price": 50.0, "extraData": "more stuff"},
	}
	sel := &plan.SelectionConfig{Strategy: "llm", Prompt: "pick cheapest"}

	result, rec, err := llmSelectElement(context.Background(), rctx, arr, sel, "itemId", g, "source", "items")
	require.NoError(t, err)
	assert.Equal(t, 1, result.index)
	// Check that the prompt contains tabular format with elementField names
	require.Len(t, rec.Messages, 2)
	userPrompt := rec.Messages[1].Content
	assert.Contains(t, userPrompt, "id=a")
	assert.Contains(t, userPrompt, "price=100")
}

func TestLLMSelectElement_DefaultModeIsStrict(t *testing.T) {
	stub := &stubLLMClient{responses: []string{"0"}}
	rctx := &ResolveContext{
		Mode: "", // empty → defaults to strict
		LLM:  stub,
	}
	arr := []any{map[string]any{"id": "a"}}
	sel := &plan.SelectionConfig{Strategy: "llm", Prompt: "pick one"}

	_, _, err := llmSelectElement(context.Background(), rctx, arr, sel, "itemId", nil, "source", "items")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strict mode")
}

// --- parseElementIndex tests ---

func TestParseElementIndex_PlainInt(t *testing.T) {
	idx, err := parseElementIndex("2", 5)
	require.NoError(t, err)
	assert.Equal(t, 2, idx)
}

func TestParseElementIndex_PlainIntWithWhitespace(t *testing.T) {
	idx, err := parseElementIndex("  1  \n", 5)
	require.NoError(t, err)
	assert.Equal(t, 1, idx)
}

func TestParseElementIndex_JSONFormat(t *testing.T) {
	idx, err := parseElementIndex(`{"index": 3}`, 5)
	require.NoError(t, err)
	assert.Equal(t, 3, idx)
}

func TestParseElementIndex_OutOfBounds(t *testing.T) {
	_, err := parseElementIndex("5", 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of bounds")
}

func TestParseElementIndex_NegativeIndex(t *testing.T) {
	_, err := parseElementIndex("-1", 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of bounds")
}

func TestParseElementIndex_NonNumeric(t *testing.T) {
	_, err := parseElementIndex("the first one", 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not parse index")
}

// --- Integration test: LLM selection through ResolveInputsWithContext ---

func TestResolveInputs_LLMSelection_ViaSelectEdge(t *testing.T) {
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
					{Name: "itemId", Type: "string"},
				},
			},
		},
	}

	stub := &stubLLMClient{responses: []string{"1"}}
	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{
			map[string]any{"id": "first-id"},
			map[string]any{"id": "second-id"},
		},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"itemId": {
				From: "source.items",
				Select: &plan.SelectionConfig{
					Strategy: "llm",
					Field:    "itemId",
					Prompt:   "pick the second one",
				},
			},
		},
	}

	rctx := &ResolveContext{Mode: config.ModeLean, LLM: stub}
	inputs, decisions, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	assert.Equal(t, "second-id", inputs["itemId"])
	require.Len(t, decisions, 1)
	assert.Equal(t, "llm", decisions[0].Strategy)
	assert.Equal(t, 1, decisions[0].SelectedIndex)
	require.NotNil(t, decisions[0].LLMCall)
}

func TestResolveInputs_LLMSelection_ViaPlanFromSelect(t *testing.T) {
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

	stub := &stubLLMClient{responses: []string{"0"}}
	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"offerings": []any{
			map[string]any{"id": "offer-1", "price": 200},
			map[string]any{"id": "offer-2", "price": 100},
		},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"offeringId": {
				From: "source.offerings",
				Select: &plan.SelectionConfig{
					Strategy: "llm",
					Field:    "id",
					Prompt:   "pick cheapest",
				},
			},
		},
	}

	rctx := &ResolveContext{Mode: config.ModeLean, LLM: stub}
	inputs, decisions, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.NoError(t, err)
	assert.Equal(t, "offer-1", inputs["offeringId"])
	require.Len(t, decisions, 1)
	assert.Equal(t, "llm", decisions[0].Strategy)
	require.NotNil(t, decisions[0].LLMCall)
}

func TestResolveInputs_LLMSelection_StrictModeErrors(t *testing.T) {
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

	stub := &stubLLMClient{responses: []string{"0"}}
	state := NewRunState()
	state.StoreOutputs("source", map[string]any{
		"items": []any{map[string]any{"id": "a"}},
	})

	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"itemId": {
				From: "source.items",
				Select: &plan.SelectionConfig{
					Strategy: "llm",
					Prompt:   "pick one",
				},
			},
		},
	}

	rctx := &ResolveContext{Mode: config.ModeStrict, LLM: stub}
	_, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, state, rctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strict mode")
}

// --- truncateJSON tests ---

func TestTruncateJSON_Short(t *testing.T) {
	assert.Equal(t, "short", truncateJSON("short", 100))
}

func TestTruncateJSON_Long(t *testing.T) {
	long := fmt.Sprintf("%0200d", 0)
	result := truncateJSON(long, 50)
	assert.Len(t, result, 53) // 50 + "..."
	assert.True(t, len(result) < len(long))
}
