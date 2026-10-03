package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/graph/oas"
	"github.com/gburgyan/aat/plan"
)

// buildHintEngine sends {"quantity": N} with an X-Hint header from the hint
// input. Its server answers each request with the status the handler picks.
func buildHintEngine(t *testing.T, handler http.HandlerFunc) *Engine {
	t.Helper()
	return buildHintEngineWith(t, handler, `{"quantity": {{quantity}}}`)
}

// buildHintEngineWith is buildHintEngine with another body, and more inputs.
func buildHintEngineWith(t *testing.T, handler http.HandlerFunc, body string, more ...graph.Input) *Engine {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"addItem": {Name: "addItem", Adapter: "test.addItem", Inputs: append([]graph.Input{
			{Name: "quantity", Type: "integer", Default: &graph.InputDefault{Value: 2}},
			{Name: "hint", Type: "string", Default: &graph.InputDefault{Value: "plain"}},
		}, more...)},
	}}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.addItem", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "test.addItem", Protocol: "http",
		Request: adapter.TemplateRequest{
			Method: "POST", Path: "/items",
			Headers: map[string]string{"Content-Type": "application/json", "X-Hint": "{{hint}}"},
			Body:    body,
		},
	})))
	return NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{}))
}

func pinnedPlan(cases ...plan.PinnedFuzzCase) *plan.Plan {
	p := quantityPlan()
	p.Execution.Steps[0].FuzzSettings = &plan.FuzzSettings{Pinned: cases}
	return p
}

// TestFuzz_HeaderTheClientRefusesIsNotSent checks that a value net/http won't
// put in a header is a case that wasn't sent, not a request that got no
// response.
func TestFuzz_HeaderTheClientRefusesIsNotSent(t *testing.T) {
	var requests atomic.Int32
	eng := buildHintEngine(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{}`))
	})
	result := eng.Run(context.Background(), pinnedPlan(
		plan.PinnedFuzzCase{ID: "hint.control-chars", Mode: plan.FuzzEdge, Input: "hint", Value: "a\x00b\x1bc"},
	))
	require.NoError(t, result.Error)
	assert.Equal(t, OutcomePassed, result.Outcome)
	assert.Equal(t, map[string]string{"hint.control-chars": FindingNotSent}, findings(result))
	assert.Equal(t, int32(1), requests.Load(), "only the happy path reached the server")
}

// TestFuzz_Throttled checks that a 429 is no verdict on the value: a case the
// API throttles is retried as its target would be, and one still throttled
// after that is reported as throttled, never as refused.
func TestFuzz_Throttled(t *testing.T) {
	var requests atomic.Int32
	eng := buildHintEngine(t, func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		var body struct {
			Quantity int `json:"quantity"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case body.Quantity == 7 && n%2 == 0: // throttled once, then answered
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		case body.Quantity == 8: // always throttled
			w.WriteHeader(http.StatusTooManyRequests)
		case body.Quantity < 0:
			w.WriteHeader(http.StatusBadRequest)
		}
		_, _ = w.Write([]byte(`{}`))
	})
	cases := []plan.PinnedFuzzCase{
		{ID: "quantity.retried", Mode: plan.FuzzNegative, Input: "quantity", Value: 7},
		{ID: "quantity.throttled", Mode: plan.FuzzNegative, Input: "quantity", Value: 8},
	}

	t.Run("without a retry block", func(t *testing.T) {
		requests.Store(0)
		result := eng.Run(context.Background(), pinnedPlan(cases[1]))
		require.NoError(t, result.Error)
		assert.Equal(t, map[string]string{"quantity.throttled": FindingThrottled}, findings(result),
			"a 429 is not the API refusing the value")
	})

	t.Run("with the target's retry block", func(t *testing.T) {
		requests.Store(0)
		p := pinnedPlan(cases...)
		p.Execution.Steps[0].Retry = &plan.RetryConfig{Max: 1, On: []string{"transient"}}
		result := eng.Run(context.Background(), p)
		require.NoError(t, result.Error)
		assert.Equal(t, map[string]string{"quantity.retried": FindingAcceptedInvalid, "quantity.throttled": FindingThrottled}, findings(result),
			"the retried case is judged on the answer it got in the end")
		for _, s := range fuzzSteps(result) {
			assert.Equal(t, 1, s.RetryCount, s.Fuzz.Case.ID)
		}
	})

	t.Run("a retry block that wouldn't retry a 429", func(t *testing.T) {
		assert.Nil(t, throttleRetry(&plan.RetryConfig{Max: 2, On: []string{"503"}}))
		assert.Nil(t, throttleRetry(nil))
		assert.Equal(t, &plan.RetryConfig{Max: 2, On: []string{"429"}}, throttleRetry(&plan.RetryConfig{Max: 2, On: []string{"RESOURCE_EXHAUSTED"}}))
	})
}

const documentedSpec = `openapi: "3.0.3"
info: {title: fuzz, version: "1"}
paths:
  /items:
    post:
      operationId: addItem
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                quantity: {type: integer, minimum: 0}
                sku: {type: string, pattern: "^SKU-[0-9]{5}$"}
      responses:
        "201":
          description: Added
        "400":
          description: Refused
`

func loadSpec(t *testing.T, eng *Engine, spec string) *Engine {
	t.Helper()
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(spec), 0o644))
	cache := oas.NewSpecCache()
	require.NoError(t, cache.Load("spec.yaml", specPath))
	eng.graph.Nodes["addItem"].OAS = &graph.OASRef{OperationID: "addItem", Spec: "spec.yaml"}
	return eng.WithOASSpecs(cache, "", false)
}

// TestFuzz_UndocumentedStatusHidesNothingWorse checks that a status the spec
// doesn't list is reported only when the response is otherwise what the case
// called for.
func TestFuzz_UndocumentedStatusHidesNothingWorse(t *testing.T) {
	eng := loadSpec(t, buildHintEngine(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Quantity int `json:"quantity"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Quantity == 5 {
			w.WriteHeader(http.StatusUnprocessableEntity)
		}
		_, _ = w.Write([]byte(`{}`)) // 200, which the spec doesn't list either
	}), documentedSpec)
	result := eng.Run(context.Background(), pinnedPlan(
		plan.PinnedFuzzCase{ID: "quantity.below-min", Mode: plan.FuzzNegative, Input: "quantity", Value: -1},
		plan.PinnedFuzzCase{ID: "quantity.refused", Mode: plan.FuzzPositive, Input: "quantity", Value: 5},
		plan.PinnedFuzzCase{ID: "quantity.ok", Mode: plan.FuzzPositive, Input: "quantity", Value: 6},
	))
	assert.Equal(t, map[string]string{
		"quantity.below-min": FindingAcceptedInvalid,
		"quantity.refused":   FindingRejectedValid,
		"quantity.ok":        FindingUndocumentedStatus,
	}, findings(result))
}

// TestFuzz_InheritedViolationsDontJudgeTheCase checks that a request
// violation the happy path's own request has is not held against a case.
func TestFuzz_InheritedViolationsDontJudgeTheCase(t *testing.T) {
	eng := loadSpec(t, buildHintEngineWith(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}, `{"quantity": {{quantity}}, "sku": "{{sku}}"}`,
		graph.Input{Name: "sku", Type: "string", Default: &graph.InputDefault{Value: "SKU-1"}}), // the spec wants five digits
		documentedSpec)

	result := eng.Run(context.Background(), pinnedPlan(
		plan.PinnedFuzzCase{ID: "quantity.zero", Mode: plan.FuzzPositive, Input: "quantity", Value: 0},
		plan.PinnedFuzzCase{ID: "quantity.below-min", Mode: plan.FuzzPositive, Input: "quantity", Value: -1},
	))
	byID := map[string]*FuzzResult{}
	for _, s := range fuzzSteps(result) {
		byID[s.Fuzz.Case.ID] = s.Fuzz
	}
	require.Len(t, byID, 2)
	assert.Equal(t, plan.FuzzPositive, byID["quantity.zero"].JudgedAs, "the sku violation is the happy path's")
	assert.Empty(t, byID["quantity.zero"].SpecViolations)
	assert.Empty(t, byID["quantity.zero"].Finding)
	assert.Equal(t, plan.FuzzNegative, byID["quantity.below-min"].JudgedAs, "a violation of its own makes the case negative")
	require.Len(t, byID["quantity.below-min"].SpecViolations, 1)
	assert.Contains(t, byID["quantity.below-min"].SpecViolations[0], "quantity")
	assert.Equal(t, FindingAcceptedInvalid, byID["quantity.below-min"].Finding)
}

// TestFuzz_PositiveValueTheConstraintRulesOut checks that a positive value
// the step's constraint rules out is judged as edge: the plan would never
// send it, so its refusal is not a rejected-valid.
func TestFuzz_PositiveValueTheConstraintRulesOut(t *testing.T) {
	eng := buildHintEngine(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hint") == "same" {
			w.WriteHeader(http.StatusBadRequest)
		}
		_, _ = w.Write([]byte(`{}`))
	})
	p := pinnedPlan(
		plan.PinnedFuzzCase{ID: "hint.pool-1", Mode: plan.FuzzPositive, Input: "hint", Value: "same"},
		plan.PinnedFuzzCase{ID: "hint.pool-2", Mode: plan.FuzzPositive, Input: "hint", Value: "other"},
	)
	p.Execution.Steps[0].Values = map[string]plan.StepValue{"hint": {Default: "plain", Constraint: `value != "same"`}}
	result := eng.Run(context.Background(), p)
	require.NoError(t, result.Error)
	byID := map[string]*FuzzResult{}
	for _, s := range fuzzSteps(result) {
		byID[s.Fuzz.Case.ID] = s.Fuzz
	}
	assert.Equal(t, plan.FuzzEdge, byID["hint.pool-1"].JudgedAs)
	assert.Empty(t, byID["hint.pool-1"].Finding, "an edge case may be refused")
	assert.Equal(t, plan.FuzzPositive, byID["hint.pool-2"].JudgedAs)
}
