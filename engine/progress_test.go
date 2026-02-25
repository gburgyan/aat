package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingObserver captures all progress events for assertions.
type recordingObserver struct {
	events []progressEvent
}

type progressEvent struct {
	kind   string
	index  int
	total  int
	mode   string
	node   string
	err    error
	result *RunResult
}

func (o *recordingObserver) OnRunStart(total int, mode string) {
	o.events = append(o.events, progressEvent{kind: "run_start", total: total, mode: mode})
}

func (o *recordingObserver) OnStepStart(index, total int, step plan.Step) {
	o.events = append(o.events, progressEvent{kind: "step_start", index: index, total: total, node: step.Node})
}

func (o *recordingObserver) OnStepComplete(index, total int, result StepResult) {
	o.events = append(o.events, progressEvent{kind: "step_complete", index: index, total: total, node: result.Node, err: result.Error})
}

func (o *recordingObserver) OnCleanupStart(total int) {
	o.events = append(o.events, progressEvent{kind: "cleanup_start", total: total})
}

func (o *recordingObserver) OnCleanupStepComplete(index, total int, result StepResult) {
	o.events = append(o.events, progressEvent{kind: "cleanup_step_complete", index: index, total: total, node: result.Node})
}

func (o *recordingObserver) OnRunComplete(result *RunResult) {
	o.events = append(o.events, progressEvent{kind: "run_complete", result: result})
}

func TestProgressObserver_SuccessfulRun(t *testing.T) {
	g := buildTestGraph()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results":   []any{map[string]any{"id": "item-1"}},
			"sessionId": "sess-1",
			"confirmed": true,
			"price":     50.0,
			"bookingId": "book-1",
		})
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.search", &stubAdapter{
		method:   "POST",
		path:     "/search",
		response: map[string]any{"results": []any{map[string]any{"id": "item-1"}}, "sessionId": "sess-1"},
	}))
	require.NoError(t, registry.Register("test.select", &stubAdapter{
		method:   "POST",
		path:     "/select",
		response: map[string]any{"confirmed": true, "price": 50.0},
	}))
	require.NoError(t, registry.Register("test.book", &stubAdapter{
		method:   "POST",
		path:     "/book",
		response: map[string]any{"bookingId": "book-1"},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	obs := &recordingObserver{}
	eng := NewEngine(g, registry, NewExecutorRouter(executor, &adapter.EnvironmentConfig{})).
		WithProgress(obs)

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{"query": {Default: "test"}}},
				{Node: "select", DependsOn: []string{"search"}, Values: map[string]plan.StepValue{
					"sessionId": {From: "search.sessionId"},
					"itemId":    {From: "search.results", Select: &plan.SelectionConfig{Strategy: "first", Field: "id"}},
				}},
				{Node: "book", DependsOn: []string{"search", "select"}, Values: map[string]plan.StepValue{
					"sessionId": {From: "search.sessionId"},
					"confirmed": {From: "select.confirmed"},
				}},
			},
		},
	}

	result := eng.Run(context.Background(), p)
	require.Equal(t, OutcomePassed, result.Outcome)

	// Verify event sequence: run_start, 3×(step_start, step_complete), run_complete
	require.Len(t, obs.events, 8) // 1 + 3*2 + 1

	assert.Equal(t, "run_start", obs.events[0].kind)
	assert.Equal(t, 3, obs.events[0].total)
	assert.Equal(t, "strict", obs.events[0].mode)

	// Step 0: search
	assert.Equal(t, "step_start", obs.events[1].kind)
	assert.Equal(t, 0, obs.events[1].index)
	assert.Equal(t, 3, obs.events[1].total)
	assert.Equal(t, "search", obs.events[1].node)

	assert.Equal(t, "step_complete", obs.events[2].kind)
	assert.Equal(t, 0, obs.events[2].index)
	assert.Equal(t, "search", obs.events[2].node)

	// Step 1: select
	assert.Equal(t, "step_start", obs.events[3].kind)
	assert.Equal(t, 1, obs.events[3].index)
	assert.Equal(t, "select", obs.events[3].node)

	assert.Equal(t, "step_complete", obs.events[4].kind)
	assert.Equal(t, 1, obs.events[4].index)
	assert.Equal(t, "select", obs.events[4].node)

	// Step 2: book
	assert.Equal(t, "step_start", obs.events[5].kind)
	assert.Equal(t, 2, obs.events[5].index)
	assert.Equal(t, "book", obs.events[5].node)

	assert.Equal(t, "step_complete", obs.events[6].kind)
	assert.Equal(t, 2, obs.events[6].index)
	assert.Equal(t, "book", obs.events[6].node)

	// run_complete
	assert.Equal(t, "run_complete", obs.events[7].kind)
	assert.Equal(t, OutcomePassed, obs.events[7].result.Outcome)
}

func TestProgressObserver_StepFailure(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs:  []graph.Input{{Name: "input", Type: "string"}},
				Outputs: []graph.Output{{Name: "output", Type: "string"}},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "bad"}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method: "POST", path: "/step1", response: map[string]any{"output": "val"},
	}))

	obs := &recordingObserver{}
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{})).
		WithProgress(obs)

	p := &plan.Plan{
		Metadata:  plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{{Node: "step1", Values: map[string]plan.StepValue{"input": {Default: "test"}}}}},
	}

	result := eng.Run(context.Background(), p)
	assert.Equal(t, OutcomeFailed, result.Outcome)

	// Verify: run_start, step_start, step_complete, run_complete
	require.Len(t, obs.events, 4)
	assert.Equal(t, "run_start", obs.events[0].kind)
	assert.Equal(t, "step_start", obs.events[1].kind)
	assert.Equal(t, "step_complete", obs.events[2].kind)
	assert.Equal(t, "run_complete", obs.events[3].kind)
	assert.Equal(t, OutcomeFailed, obs.events[3].result.Outcome)
}

func TestProgressObserver_ValidationError(t *testing.T) {
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{}}

	obs := &recordingObserver{}
	eng := NewEngine(g, adapter.NewRegistry(), NewExecutorRouter(nil, nil)).
		WithProgress(obs)

	p := &plan.Plan{
		Execution: plan.Execution{Steps: []plan.Step{{Node: "nonexistent"}}},
	}

	result := eng.Run(context.Background(), p)
	assert.Equal(t, OutcomeError, result.Outcome)

	// OnRunComplete should still fire even on validation errors
	require.True(t, len(obs.events) >= 1)
	last := obs.events[len(obs.events)-1]
	assert.Equal(t, "run_complete", last.kind)
	assert.Equal(t, OutcomeError, last.result.Outcome)
}

func TestProgressObserver_NilObserver(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs:  []graph.Input{{Name: "input", Type: "string"}},
				Outputs: []graph.Output{{Name: "output", Type: "string"}},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"output": "val"})
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method: "POST", path: "/step1", response: map[string]any{"output": "val"},
	}))

	// No observer set — should not panic
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{}))

	p := &plan.Plan{
		Metadata:  plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{{Node: "step1", Values: map[string]plan.StepValue{"input": {Default: "test"}}}}},
	}

	result := eng.Run(context.Background(), p)
	assert.Equal(t, OutcomePassed, result.Outcome)
}

func TestProgressObserver_WithCleanup(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"create": {
				Name:    "create",
				Adapter: "test.create",
				Inputs:  []graph.Input{{Name: "data", Type: "string"}},
				Outputs: []graph.Output{{Name: "id", Type: "string"}},
				Cleanup: "delete",
			},
			"delete": {
				Name:    "delete",
				Adapter: "test.delete",
				Inputs:  []graph.Input{{Name: "id", Type: "string"}},
				Outputs: []graph.Output{},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "res-1"})
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.create", &stubAdapter{
		method: "POST", path: "/create", response: map[string]any{"id": "res-1"},
	}))
	require.NoError(t, registry.Register("test.delete", &stubAdapter{
		method: "DELETE", path: "/delete", response: map[string]any{},
	}))

	obs := &recordingObserver{}
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{})).
		WithProgress(obs)

	p := &plan.Plan{
		Metadata:  plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{{Node: "create", Values: map[string]plan.StepValue{"data": {Default: "test"}}}}},
	}

	result := eng.Run(context.Background(), p)
	require.Equal(t, OutcomePassed, result.Outcome)

	// Verify: run_start, step_start, step_complete, cleanup_start, cleanup_step_complete, run_complete
	kinds := make([]string, len(obs.events))
	for i, e := range obs.events {
		kinds[i] = e.kind
	}

	assert.Equal(t, []string{
		"run_start",
		"step_start",
		"step_complete",
		"cleanup_start",
		"cleanup_step_complete",
		"run_complete",
	}, kinds)

	// Verify cleanup details
	cleanupStart := obs.events[3]
	assert.Equal(t, 1, cleanupStart.total)

	cleanupComplete := obs.events[4]
	assert.Equal(t, 0, cleanupComplete.index)
	assert.Equal(t, 1, cleanupComplete.total)
	assert.Equal(t, "delete", cleanupComplete.node)
}

func TestProgressObserver_IndexValues(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"a": {Name: "a", Adapter: "test.a", Inputs: []graph.Input{{Name: "in", Type: "string"}}, Outputs: []graph.Output{{Name: "out", Type: "string"}}},
			"b": {Name: "b", Adapter: "test.b", Inputs: []graph.Input{{Name: "in", Type: "string"}}, Outputs: []graph.Output{{Name: "out", Type: "string"}}},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"out": "val"})
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.a", &stubAdapter{method: "POST", path: "/a", response: map[string]any{"out": "val"}}))
	require.NoError(t, registry.Register("test.b", &stubAdapter{method: "POST", path: "/b", response: map[string]any{"out": "val"}}))

	obs := &recordingObserver{}
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{})).
		WithProgress(obs)

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{
			{Node: "a", Values: map[string]plan.StepValue{"in": {Default: "x"}}},
			{Node: "b", DependsOn: []string{"a"}, Values: map[string]plan.StepValue{"in": {From: "a.out"}}},
		}},
	}

	result := eng.Run(context.Background(), p)
	require.Equal(t, OutcomePassed, result.Outcome)

	// Verify 0-based indices
	for _, e := range obs.events {
		if e.kind == "step_start" || e.kind == "step_complete" {
			assert.Equal(t, 2, e.total, "total should be 2 for both steps")
		}
	}

	// First step: index 0
	assert.Equal(t, 0, obs.events[1].index) // step_start for a
	assert.Equal(t, 0, obs.events[2].index) // step_complete for a

	// Second step: index 1
	assert.Equal(t, 1, obs.events[3].index) // step_start for b
	assert.Equal(t, 1, obs.events[4].index) // step_complete for b
}
