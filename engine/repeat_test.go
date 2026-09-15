package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
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

// pagesEngine builds an engine over one listing node, listItems, whose template
// sends limit and, when set, the after cursor, and extracts the items, their
// count, and the next page's cursor, "" on the last page.
func pagesEngine(t *testing.T, url string) *Engine {
	t.Helper()
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"listItems": {
			Name:    "listItems",
			Adapter: "test.listItems",
			Inputs:  []graph.Input{{Name: "limit", Type: "integer"}, {Name: "after", Type: "string", Optional: true}},
			Outputs: []graph.Output{
				{Name: "items", Type: "item[]"},
				{Name: "itemCount", Type: "integer"},
				{Name: "nextCursor", Type: "string"},
			},
		},
	}}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.listItems", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "listItems",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "GET", Path: "/items?limit={{limit}}{{?after}}&after={{after}}{{/after}}"},
		Response: adapter.TemplateResponse{Extract: map[string]adapter.ExtractRule{
			"items":      {Path: "items"},
			"itemCount":  {Path: "items.#"},
			"nextCursor": {Path: "next", Default: ""},
		}},
	})))
	return NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(url), &adapter.EnvironmentConfig{}))
}

func pagesPlan(repeat plan.RepeatConfig, values map[string]plan.StepValue, assertions ...plan.MechanicalAssertion) *plan.Plan {
	v := map[string]plan.StepValue{"limit": {Default: 2}}
	maps.Copy(v, values)
	step := plan.Step{Node: "listItems", Values: v, Repeat: &repeat}
	if len(assertions) > 0 {
		step.Assertions = &plan.Assertions{Mechanical: assertions}
	}
	return &plan.Plan{Metadata: plan.Metadata{GraphVersion: "1.0.0"}, Execution: plan.Execution{Steps: []plan.Step{step}}}
}

// page answers with a page of items and the cursor to the next, a JSON literal
// such as "c1" or null.
func page(items, next string) func(w http.ResponseWriter) {
	return jsonBody(fmt.Sprintf(`{"items": [%s], "next": %s}`, items, next))
}

func TestRepeat_Next(t *testing.T) {
	next := map[string]string{"after": "nextCursor"}
	tests := []struct {
		name      string
		responses []func(w http.ResponseWriter)
		repeat    plan.RepeatConfig
		values    map[string]plan.StepValue
		outcome   Outcome
		stop      string
		uris      []string
		itemCount any    // the collected count, on a step that passes
		message   string // the repeat result, on a step that fails
	}{
		{
			name:      "pages until the cursor runs out",
			responses: []func(w http.ResponseWriter){page(`"a", "b"`, `"c1"`), page(`"c", "d"`, `"c2"`), page(`"e"`, `null`)},
			repeat:    plan.RepeatConfig{Next: next, Collect: []string{"itemCount"}},
			outcome:   OutcomePassed,
			stop:      RepeatStopExhausted,
			uris:      []string{"/items?limit=2", "/items?limit=2&after=c1", "/items?limit=2&after=c2"},
			itemCount: 5,
		},
		{
			name:      "an empty last page",
			responses: []func(w http.ResponseWriter){page(`"a", "b"`, `"c1"`), page(``, `null`)},
			repeat:    plan.RepeatConfig{Next: next, Collect: []string{"itemCount"}},
			outcome:   OutcomePassed,
			stop:      RepeatStopExhausted,
			uris:      []string{"/items?limit=2", "/items?limit=2&after=c1"},
			itemCount: 2,
		},
		{
			name:      "max with a page left fails",
			responses: []func(w http.ResponseWriter){page(`"a"`, `"c1"`), page(`"b"`, `"c2"`), page(`"c"`, `null`)},
			repeat:    plan.RepeatConfig{Next: next, Max: 2},
			outcome:   OutcomeFailed,
			stop:      RepeatStopMax,
			uris:      []string{"/items?limit=2", "/items?limit=2&after=c1"},
			message:   `repeat.next: after 2 requests (repeat.max) the response still gave a next page (after "c2"); raise repeat.max or narrow the listing`,
		},
		{
			name:      "a cursor that comes back fails",
			responses: []func(w http.ResponseWriter){page(`"a"`, `"c1"`), page(`"b"`, `"c1"`)},
			repeat:    plan.RepeatConfig{Next: next},
			outcome:   OutcomeFailed,
			stop:      RepeatStopLoop,
			uris:      []string{"/items?limit=2", "/items?limit=2&after=c1"},
			message:   `repeat.next: response 2 gave after "c1", which request 2 already sent, so the pages would repeat`,
		},
		{
			name:      "a resume cursor that comes back fails",
			responses: []func(w http.ResponseWriter){page(`"a"`, `"c0"`)},
			repeat:    plan.RepeatConfig{Next: next},
			values:    map[string]plan.StepValue{"after": {Default: "c0"}},
			outcome:   OutcomeFailed,
			stop:      RepeatStopLoop,
			uris:      []string{"/items?limit=2&after=c0"},
			message:   `repeat.next: response 1 gave after "c0", which request 1 already sent, so the pages would repeat`,
		},
		{
			name:      "until stops before the last page",
			responses: []func(w http.ResponseWriter){page(`"a", "b"`, `"c1"`), page(`"c"`, `null`)},
			repeat:    plan.RepeatConfig{Until: "itemCount >= 1", Next: next, Collect: []string{"itemCount"}},
			outcome:   OutcomePassed,
			stop:      RepeatStopUntil,
			uris:      []string{"/items?limit=2"},
			itemCount: json.Number("2"), // one response: collect keeps its value as extracted
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, uris := scriptedServer(t, tt.responses...)

			result := pagesEngine(t, srv.URL).Run(context.Background(), pagesPlan(tt.repeat, tt.values))

			require.Equal(t, tt.outcome, result.Outcome, "error: %v", result.Error)
			step := result.Steps[0]
			assert.Equal(t, tt.stop, step.RepeatStop)
			assert.Equal(t, tt.uris, uris())
			if tt.message != "" {
				require.NotNil(t, step.Validation)
				assert.Equal(t, validate.AssertRepeat, step.Validation.Results[0].Type)
				assert.Equal(t, tt.message, step.Validation.Results[0].Message)
				return
			}
			assert.Equal(t, tt.itemCount, step.Outputs["itemCount"])
		})
	}
}

func TestRepeat_NextRecordsEachPagesInputs(t *testing.T) {
	srv, _ := scriptedServer(t, page(`"a"`, `"c1"`), page(`"b"`, `null`))
	p := pagesPlan(plan.RepeatConfig{Next: map[string]string{"after": "nextCursor"}, Collect: []string{"items"}}, nil,
		plan.MechanicalAssertion{Type: "predicate", Expr: `nextCursor == ""`})

	result := pagesEngine(t, srv.URL).Run(context.Background(), p)

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	step := result.Steps[0]
	assert.NotContains(t, step.Inputs, "after", "the step's inputs are the query it ran, from its first page")
	require.Len(t, step.Iterations, 2)
	assert.NotContains(t, step.Iterations[0].Inputs, "after")
	assert.Equal(t, "c1", step.Iterations[1].Inputs["after"])
	assert.Equal(t, []any{"a", "b"}, step.Outputs["items"])
	require.NotNil(t, step.Validation)
	assert.Len(t, step.Validation.Results, 1, "the step's assertions run once")

	a, err := ToArchive(result, archive.ArchiveMetadata{}, srv.URL, nil)
	require.NoError(t, err)
	assert.Equal(t, "exhausted", a.Steps[0].RepeatStop)
	assert.Equal(t, "c1", a.Steps[0].Iterations[1].Inputs["after"])
}

func TestRepeat_NextRetryResendsTheCursor(t *testing.T) {
	srv, uris := scriptedServer(t,
		page(`"a"`, `"c1"`),
		func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error": "busy"}`))
		},
		page(`"b"`, `null`),
	)
	p := pagesPlan(plan.RepeatConfig{Next: map[string]string{"after": "nextCursor"}}, nil)
	p.Execution.Steps[0].Retry = &plan.RetryConfig{Max: 1, On: []string{"transient", "server"}}

	result := pagesEngine(t, srv.URL).Run(context.Background(), p)

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	assert.Equal(t, []string{"/items?limit=2", "/items?limit=2&after=c1", "/items?limit=2&after=c1"}, uris())
	assert.Equal(t, 1, result.Steps[0].RetryCount)
}

func TestCursorValues(t *testing.T) {
	next := map[string]string{"after": "nextCursor", "page": "nextPage"}
	tests := []struct {
		name      string
		outputs   map[string]any
		want      map[string]any
		exhausted bool
	}{
		{name: "both set", outputs: map[string]any{"nextCursor": "c1", "nextPage": json.Number("2")}, want: map[string]any{"after": "c1", "page": json.Number("2")}},
		{name: "one set", outputs: map[string]any{"nextCursor": "", "nextPage": 3}, want: map[string]any{"after": nil, "page": 3}},
		{name: "zero is a page", outputs: map[string]any{"nextCursor": nil, "nextPage": 0}, want: map[string]any{"after": nil, "page": 0}},
		{name: "empty, null, and missing", outputs: map[string]any{"nextCursor": nil}, want: map[string]any{"after": nil, "page": nil}, exhausted: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, exhausted := cursorValues(tt.outputs, next)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.exhausted, exhausted)
		})
	}
}

func TestCursorKey(t *testing.T) {
	assert.Equal(t, cursorKey(map[string]any{"page": json.Number("2")}), cursorKey(map[string]any{"page": 2}), "an extracted number and an int are the same cursor")
	assert.Equal(t, "after=c1&page=2", cursorKey(map[string]any{"page": 2, "after": "c1", "offset": nil}))
	assert.Empty(t, cursorKey(map[string]any{"after": nil}))
}

func TestWithCursors(t *testing.T) {
	inputs := map[string]any{"limit": 2, "after": "c0"}

	got := withCursors(inputs, map[string]any{"after": nil, "page": 3})

	assert.Equal(t, map[string]any{"limit": 2, "page": 3}, got)
	assert.Equal(t, map[string]any{"limit": 2, "after": "c0"}, inputs, "the first request's inputs are left as they were")
}

func TestCollectOutputs(t *testing.T) {
	collected := map[string]any{}
	collectOutputs(collected, map[string]any{"items": []any{"a"}, "count": json.Number("2"), "total": 1.5}, []string{"items", "count", "total", "absent"})
	collectOutputs(collected, map[string]any{"items": []any{"b", "c"}, "count": 3, "total": json.Number("2")}, []string{"items", "count", "total", "absent"})

	assert.Equal(t, map[string]any{"items": []any{"a", "b", "c"}, "count": 5, "total": 3.5}, collected)
}
