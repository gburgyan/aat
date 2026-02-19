package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/llm"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubLLMClient is a test double for llm.Client.
type stubLLMClient struct {
	responses []string
	calls     int
	lastReq   *llm.Request
}

func (s *stubLLMClient) Complete(_ context.Context, req *llm.Request) (*llm.Response, error) {
	s.lastReq = req
	if s.calls >= len(s.responses) {
		return nil, fmt.Errorf("stub: no more responses")
	}
	resp := s.responses[s.calls]
	s.calls++
	return &llm.Response{Content: resp}, nil
}

func TestLLMSelectValue_StrictModeNeverCalls(t *testing.T) {
	stub := &stubLLMClient{responses: []string{"DEN"}}
	rctx := &ResolveContext{
		Mode: config.ModeStrict,
		LLM:  stub,
	}
	input := graph.Input{Name: "origin", Type: "string"}
	sv := plan.StepValue{Default: "BAD"}

	_, rec, err := llmSelectValue(context.Background(), rctx, input, sv, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strict mode")
	assert.Nil(t, rec)
	assert.Equal(t, 0, stub.calls, "LLM should not have been called")
}

func TestLLMSelectValue_LeanModeCallsLLM(t *testing.T) {
	stub := &stubLLMClient{responses: []string{"DEN"}}
	rctx := &ResolveContext{
		Mode: config.ModeLean,
		LLM:  stub,
	}
	input := graph.Input{Name: "origin", Type: "string"}
	sv := plan.StepValue{Default: "BAD"}

	val, rec, err := llmSelectValue(context.Background(), rctx, input, sv, nil)
	require.NoError(t, err)
	assert.Equal(t, "DEN", val)
	assert.Equal(t, 1, stub.calls)
	require.NotNil(t, rec)
	assert.Equal(t, "DEN", rec.Response)
	assert.Len(t, rec.Messages, 2)
	assert.Equal(t, "system", rec.Messages[0].Role)
	assert.Equal(t, "user", rec.Messages[1].Role)
}

func TestLLMSelectValue_AdaptiveModeCallsLLM(t *testing.T) {
	stub := &stubLLMClient{responses: []string{"LAX"}}
	rctx := &ResolveContext{
		Mode: config.ModeAdaptive,
		LLM:  stub,
	}
	input := graph.Input{Name: "origin", Type: "string"}
	sv := plan.StepValue{}

	val, rec, err := llmSelectValue(context.Background(), rctx, input, sv, nil)
	require.NoError(t, err)
	assert.Equal(t, "LAX", val)
	require.NotNil(t, rec)
}

func TestLLMSelectValue_NoClientError(t *testing.T) {
	rctx := &ResolveContext{
		Mode: config.ModeLean,
		LLM:  nil,
	}
	input := graph.Input{Name: "origin", Type: "string"}
	sv := plan.StepValue{}

	_, rec, err := llmSelectValue(context.Background(), rctx, input, sv, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no LLM client")
	assert.Nil(t, rec)
}

func TestLLMSelectValue_EmptyResponse(t *testing.T) {
	stub := &stubLLMClient{responses: []string{"   "}}
	rctx := &ResolveContext{
		Mode: config.ModeLean,
		LLM:  stub,
	}
	input := graph.Input{Name: "origin", Type: "string"}
	sv := plan.StepValue{}

	_, rec, err := llmSelectValue(context.Background(), rctx, input, sv, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty value")
	require.NotNil(t, rec)
	assert.Equal(t, "empty response", rec.Error)
}

func TestLLMSelectValue_LLMError(t *testing.T) {
	stub := &stubLLMClient{responses: nil} // no responses → error
	rctx := &ResolveContext{
		Mode: config.ModeLean,
		LLM:  stub,
	}
	input := graph.Input{Name: "origin", Type: "string"}
	sv := plan.StepValue{}

	_, rec, err := llmSelectValue(context.Background(), rctx, input, sv, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LLM value selection")
	require.NotNil(t, rec)
	assert.NotEmpty(t, rec.Error)
}

func TestLLMSelectValue_DefaultModeIsStrict(t *testing.T) {
	stub := &stubLLMClient{responses: []string{"DEN"}}
	rctx := &ResolveContext{
		Mode: "", // empty → defaults to strict
		LLM:  stub,
	}
	input := graph.Input{Name: "origin", Type: "string"}
	sv := plan.StepValue{}

	_, _, err := llmSelectValue(context.Background(), rctx, input, sv, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strict mode")
}

func TestBuildValueSelectionPrompt_Basic(t *testing.T) {
	input := graph.Input{
		Name:        "origin",
		Type:        "airportCode",
		Description: "Departure airport IATA code",
	}
	sv := plan.StepValue{
		Default:      "INVALID",
		Pool: []any{"BAD1", "BAD2"},
		Constraint:   "value != 'INVALID'",
	}
	resolved := map[string]any{
		"destination": "LAX",
		"date":        "2026-02-13",
	}

	system, user := buildValueSelectionPrompt(input, sv, nil, resolved)

	assert.Contains(t, system, "API testing assistant")
	assert.Contains(t, system, "ONLY the value")
	assert.Contains(t, user, "origin")
	assert.Contains(t, user, "airportCode")
	assert.Contains(t, user, "Departure airport IATA code")
	assert.Contains(t, user, "value != 'INVALID'")
	assert.Contains(t, user, "INVALID")
	assert.Contains(t, user, "BAD1")
	assert.Contains(t, user, "BAD2")
	assert.Contains(t, user, "destination = LAX")
}

func TestBuildValueSelectionPrompt_WithDomainKnowledge(t *testing.T) {
	input := graph.Input{
		Name: "origin",
		Type: "airportCode",
	}
	sv := plan.StepValue{}

	kb := &domain.KnowledgeBase{
		Types: map[string]*domain.TypeDef{
			"airportCode": {
				Name:        "airportCode",
				Description: "IATA airport code",
				Format:      "3 uppercase letters",
				Pool:        "airports",
			},
		},
		ValuePools: map[string]*domain.ValuePool{
			"airports": {
				Name:   "airports",
				Type:   "string",
				Values: []string{"DEN", "LAX", "ORD", "JFK", "SFO"},
			},
		},
		Concepts: map[string]*domain.Concept{
			"routing": {
				Name:        "routing",
				Description: "Origin and destination must be different airports",
				AppliesTo:   []string{"origin", "destination"},
			},
		},
	}

	_, user := buildValueSelectionPrompt(input, sv, kb, nil)

	assert.Contains(t, user, "IATA airport code")
	assert.Contains(t, user, "3 uppercase letters")
	assert.Contains(t, user, "airports")
	assert.Contains(t, user, "DEN")
	assert.Contains(t, user, "routing")
	assert.Contains(t, user, "different airports")
}

func TestBuildValueSelectionPrompt_NoDescription(t *testing.T) {
	input := graph.Input{Name: "code", Type: "string"}
	sv := plan.StepValue{}

	_, user := buildValueSelectionPrompt(input, sv, nil, nil)

	assert.Contains(t, user, "code")
	assert.Contains(t, user, "string")
	assert.False(t, strings.Contains(user, "Description:"))
}
