package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/gburgyan/aat/validate"
)

// scriptedServer answers the nth request with responses[n], repeating the last
// one, and returns a function that lists the request URIs it has received.
func scriptedServer(t *testing.T, responses ...func(w http.ResponseWriter)) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var uris []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n := len(uris)
		uris = append(uris, r.URL.RequestURI())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		responses[min(n, len(responses)-1)](w)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), uris...)
	}
}

func jsonBody(body string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) { _, _ = w.Write([]byte(body)) }
}

// searchEngine builds an engine over one read node, getSearch, whose template
// sends searchId and ref and extracts the batch count, the items, their count,
// and an optional status.
func searchEngine(t *testing.T, url string) *Engine {
	t.Helper()
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"getSearch": {
			Name:    "getSearch",
			Adapter: "test.getSearch",
			Inputs:  []graph.Input{{Name: "searchId", Type: "string"}, {Name: "ref", Type: "string"}},
			Outputs: []graph.Output{
				{Name: "remainingBatches", Type: "integer"},
				{Name: "items", Type: "item[]"},
				{Name: "itemCount", Type: "integer"},
				{Name: "status", Type: "string", Optional: true},
			},
		},
	}}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.getSearch", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "getSearch",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "GET", Path: "/searches/{{searchId}}?ref={{ref}}"},
		Response: adapter.TemplateResponse{Extract: map[string]adapter.ExtractRule{
			"remainingBatches": {Path: "remainingBatches"},
			"items":            {Path: "items"},
			"itemCount":        {Path: "items.#"},
			"status":           {Path: "status", Optional: true},
		}},
	})))
	return NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(url), &adapter.EnvironmentConfig{}))
}

func searchPlan(repeat plan.RepeatConfig, assertions ...plan.MechanicalAssertion) *plan.Plan {
	step := plan.Step{
		Node:   "getSearch",
		Values: map[string]plan.StepValue{"searchId": {Default: "s1"}, "ref": {Default: "{{random 8}}"}},
		Repeat: &repeat,
	}
	if len(assertions) > 0 {
		step.Assertions = &plan.Assertions{Mechanical: assertions}
	}
	return &plan.Plan{Metadata: plan.Metadata{GraphVersion: "1.0.0"}, Execution: plan.Execution{Steps: []plan.Step{step}}}
}

func TestRepeat_UntilHoldsAndCollects(t *testing.T) {
	srv, uris := scriptedServer(t,
		jsonBody(`{"remainingBatches": 2, "items": [{"sku": "a"}]}`),
		jsonBody(`{"remainingBatches": 1, "items": [{"sku": "b"}, {"sku": "c"}]}`),
		jsonBody(`{"remainingBatches": 0, "items": []}`),
	)
	p := searchPlan(plan.RepeatConfig{Until: "remainingBatches == 0", Collect: []string{"items", "itemCount"}, Interval: "1ms", Max: 10},
		plan.MechanicalAssertion{Type: "predicate", Expr: "itemCount == 3 && remainingBatches == 0"})

	result := searchEngine(t, srv.URL).Run(context.Background(), p)

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	step := result.Steps[0]
	require.Len(t, step.Iterations, 3)
	assert.Equal(t, RepeatStopUntil, step.RepeatStop)
	assert.Equal(t, []bool{false, false, true}, []bool{step.Iterations[0].UntilMet, step.Iterations[1].UntilMet, step.Iterations[2].UntilMet})
	assert.Equal(t, []any{map[string]any{"sku": "a"}, map[string]any{"sku": "b"}, map[string]any{"sku": "c"}}, step.Outputs["items"])
	assert.Equal(t, 3, step.Outputs["itemCount"])
	assert.Equal(t, json.Number("0"), step.Iterations[2].Outputs["itemCount"], "the last iteration keeps its own response's outputs")

	sent := uris()
	require.Len(t, sent, 3)
	assert.Equal(t, sent[0], sent[1], "every request sends the inputs resolved for the first")
	assert.Equal(t, sent[0], sent[2])

	require.NotNil(t, step.Validation)
	require.Len(t, step.Validation.Results, 1, "the step's assertions run once")
	assert.True(t, step.Validation.Results[0].Passed, "they read the collected count: %s", step.Validation.Results[0].Message)

	a, err := ToArchive(result, archive.ArchiveMetadata{}, srv.URL, nil)
	require.NoError(t, err)
	require.Len(t, a.Steps[0].Iterations, 3)
	assert.Equal(t, "until", a.Steps[0].RepeatStop)
	assert.True(t, a.Steps[0].Iterations[2].UntilMet)
	assert.Equal(t, 200, a.Steps[0].Iterations[0].Response.Status)
}

func TestRepeat_MaxReachedFailsTheStep(t *testing.T) {
	srv, uris := scriptedServer(t, jsonBody(`{"remainingBatches": 1, "items": []}`))
	p := searchPlan(plan.RepeatConfig{Until: "remainingBatches == 0", Interval: "1ms", Max: 3})

	result := searchEngine(t, srv.URL).Run(context.Background(), p)

	require.Equal(t, OutcomeFailed, result.Outcome)
	step := result.Steps[0]
	assert.Len(t, uris(), 3)
	assert.Equal(t, RepeatStopMax, step.RepeatStop)
	require.NotNil(t, step.Validation)
	assert.False(t, step.Validation.Passed)
	assert.Equal(t, validate.AssertRepeat, step.Validation.Results[0].Type)
	assert.Equal(t, `repeat.until "remainingBatches == 0" is still false after 3 requests (repeat.max)`, step.Validation.Results[0].Message)
}

func TestRepeat_TimeoutStopsBeforeTheNextWait(t *testing.T) {
	srv, _ := scriptedServer(t, jsonBody(`{"remainingBatches": 1, "items": []}`))
	p := searchPlan(plan.RepeatConfig{Until: "remainingBatches == 0", Interval: "50ms", Timeout: "60ms", Max: 100})

	result := searchEngine(t, srv.URL).Run(context.Background(), p)

	require.Equal(t, OutcomeFailed, result.Outcome)
	step := result.Steps[0]
	assert.Equal(t, RepeatStopTimeout, step.RepeatStop)
	assert.Less(t, len(step.Iterations), 100)
	assert.Contains(t, step.Validation.Results[0].Message, "(repeat.timeout 60ms)")
}

func TestRepeat_UntilReadsAnOutputTheResponseLeftOut(t *testing.T) {
	srv, _ := scriptedServer(t, jsonBody(`{"remainingBatches": 1, "items": []}`))
	p := searchPlan(plan.RepeatConfig{Until: `status == "complete"`, Interval: "1ms", Max: 5})

	result := searchEngine(t, srv.URL).Run(context.Background(), p)

	require.Equal(t, OutcomeFailed, result.Outcome)
	step := result.Steps[0]
	assert.Equal(t, RepeatStopError, step.RepeatStop)
	assert.Len(t, step.Iterations, 1)
	assert.Contains(t, step.Validation.Results[0].Message, "needs a default: on its extract rule")
}

func TestRepeat_AFailedRequestEndsTheRepeats(t *testing.T) {
	srv, uris := scriptedServer(t,
		jsonBody(`{"remainingBatches": 1, "items": []}`),
		func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(`{"error": "search expired"}`))
		},
	)
	p := searchPlan(plan.RepeatConfig{Until: "remainingBatches == 0", Interval: "1ms", Max: 10})

	result := searchEngine(t, srv.URL).Run(context.Background(), p)

	require.Equal(t, OutcomeFailed, result.Outcome)
	assert.Len(t, uris(), 2)
	step := result.Steps[0]
	assert.Equal(t, RepeatStopError, step.RepeatStop)
	assert.Equal(t, http.StatusGone, step.StatusCode)
	assert.Len(t, step.Iterations, 2)
}

func TestCollectOutputs(t *testing.T) {
	collected := map[string]any{}
	collectOutputs(collected, map[string]any{"items": []any{"a"}, "count": json.Number("2"), "total": 1.5}, []string{"items", "count", "total", "absent"})
	collectOutputs(collected, map[string]any{"items": []any{"b", "c"}, "count": 3, "total": json.Number("2")}, []string{"items", "count", "total", "absent"})

	assert.Equal(t, map[string]any{"items": []any{"a", "b", "c"}, "count": 5, "total": 3.5}, collected)
}
