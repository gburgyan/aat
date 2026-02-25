package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupStack_FILOOrder(t *testing.T) {
	stack := &CleanupStack{}
	stack.Push(CleanupEntry{NodeName: "cleanA", ForNode: "nodeA"})
	stack.Push(CleanupEntry{NodeName: "cleanB", ForNode: "nodeB"})
	stack.Push(CleanupEntry{NodeName: "cleanC", ForNode: "nodeC"})

	assert.Equal(t, 3, stack.Len())

	// Build a minimal graph with cleanup nodes
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"cleanA": {Name: "cleanA", Adapter: "test.cleanA", Inputs: []graph.Input{}},
			"cleanB": {Name: "cleanB", Adapter: "test.cleanB", Inputs: []graph.Input{}},
			"cleanC": {Name: "cleanC", Adapter: "test.cleanC", Inputs: []graph.Input{}},
		},
	}

	var mu sync.Mutex
	var callOrder []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	for _, name := range []string{"cleanA", "cleanB", "cleanC"} {
		n := name
		require.NoError(t, registry.Register("test."+n, &orderTrackingAdapter{
			stubAdapter: stubAdapter{method: "DELETE", path: "/" + n, response: map[string]any{}},
			name:        n,
			mu:          &mu,
			order:       &callOrder,
		}))
	}

	executor := adapter.NewHTTPExecutor(server.URL)
	results := stack.ExecuteAll(context.Background(), g, registry, NewExecutorRouter(executor, &adapter.EnvironmentConfig{}), NewRunState())

	require.Len(t, results, 3)
	// FILO: C, B, A
	assert.Equal(t, "cleanC", results[0].Node)
	assert.Equal(t, "cleanB", results[1].Node)
	assert.Equal(t, "cleanA", results[2].Node)
}

type orderTrackingAdapter struct {
	stubAdapter
	name  string
	mu    *sync.Mutex
	order *[]string
}

func (a *orderTrackingAdapter) BuildRequest(inputs map[string]any, config *adapter.EnvironmentConfig) (*adapter.Request, error) {
	a.mu.Lock()
	*a.order = append(*a.order, a.name)
	a.mu.Unlock()
	return a.stubAdapter.BuildRequest(inputs, config)
}

func TestCleanupStack_Empty(t *testing.T) {
	stack := &CleanupStack{}
	results := stack.ExecuteAll(context.Background(), nil, nil, nil, nil)
	assert.Nil(t, results)
}

func TestCleanupStack_ErrorDoesNotStop(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"cleanOK":   {Name: "cleanOK", Adapter: "test.ok", Inputs: []graph.Input{}},
			"cleanFail": {Name: "cleanFail", Adapter: "test.fail", Inputs: []graph.Input{}},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.ok", &stubAdapter{
		method: "DELETE", path: "/ok", response: map[string]any{},
	}))
	// Don't register test.fail — adapter lookup will fail but shouldn't stop other cleanup

	executor := adapter.NewHTTPExecutor(server.URL)

	stack := &CleanupStack{}
	stack.Push(CleanupEntry{NodeName: "cleanOK", ForNode: "ok"})
	stack.Push(CleanupEntry{NodeName: "cleanFail", ForNode: "fail"})

	results := stack.ExecuteAll(context.Background(), g, registry, NewExecutorRouter(executor, &adapter.EnvironmentConfig{}), NewRunState())

	require.Len(t, results, 2)
	// cleanFail runs first (FILO) and errors
	assert.Error(t, results[0].Error)
	assert.Contains(t, results[0].Error.Error(), "cleanup adapter")
	// cleanOK runs second and succeeds
	assert.NoError(t, results[1].Error)
}

func TestEngine_Run_WithCleanup(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"create": {
				Name:    "create",
				Adapter: "test.create",
				Inputs:  []graph.Input{},
				Outputs: []graph.Output{
					{Name: "resourceId", Type: "string"},
				},
				Cleanup: "destroy",
			},
			"use": {
				Name:    "use",
				Adapter: "test.use",
				Inputs: []graph.Input{
					{Name: "resourceId", Type: "string"},
				},
				Outputs: []graph.Output{},
			},
			"destroy": {
				Name:    "destroy",
				Adapter: "test.destroy",
				Inputs: []graph.Input{
					{Name: "resourceId", Type: "string"},
				},
				Outputs: []graph.Output{},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.create", &stubAdapter{
		method: "POST",
		path:   "/create",
		response: map[string]any{
			"resourceId": "res-123",
		},
	}))
	require.NoError(t, registry.Register("test.use", &stubAdapter{
		method:   "POST",
		path:     "/use",
		response: map[string]any{},
	}))

	var capturedDestroyInputs map[string]any
	require.NoError(t, registry.Register("test.destroy", &captureAdapter{
		stubAdapter: stubAdapter{
			method:   "DELETE",
			path:     "/destroy",
			response: map[string]any{},
		},
		capturedInputs: &capturedDestroyInputs,
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	engine := NewEngine(g, registry, NewExecutorRouter(executor, &adapter.EnvironmentConfig{}))

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "create"},
				{Node: "use", DependsOn: []string{"create"}, Values: map[string]plan.StepValue{
					"resourceId": {From: "create.resourceId"},
				}},
			},
		},
	}

	result := engine.Run(context.Background(), p)

	assert.Equal(t, OutcomePassed, result.Outcome)
	assert.Nil(t, result.Error)
	require.Len(t, result.Steps, 2)
	require.Len(t, result.CleanupResults, 1)

	// Verify cleanup ran with correct inputs
	assert.Equal(t, "destroy", result.CleanupResults[0].Node)
	assert.NoError(t, result.CleanupResults[0].Error)
	require.NotNil(t, capturedDestroyInputs)
	assert.Equal(t, "res-123", capturedDestroyInputs["resourceId"])
}

func TestEngine_Run_CleanupOnFailure(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"create": {
				Name:    "create",
				Adapter: "test.create",
				Inputs:  []graph.Input{},
				Outputs: []graph.Output{
					{Name: "resourceId", Type: "string"},
				},
				Cleanup: "destroy",
			},
			"fail": {
				Name:    "fail",
				Adapter: "test.fail",
				Inputs: []graph.Input{
					{Name: "resourceId", Type: "string"},
				},
				Outputs: []graph.Output{},
			},
			"destroy": {
				Name:    "destroy",
				Adapter: "test.destroy",
				Inputs: []graph.Input{
					{Name: "resourceId", Type: "string"},
				},
				Outputs: []graph.Output{},
			},
		},
	}

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 2 {
			// Second request (fail step) returns 500
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error": "server error"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.create", &stubAdapter{
		method: "POST",
		path:   "/create",
		response: map[string]any{
			"resourceId": "res-456",
		},
	}))
	require.NoError(t, registry.Register("test.fail", &stubAdapter{
		method:   "POST",
		path:     "/fail",
		response: map[string]any{},
	}))

	destroyCalled := false
	require.NoError(t, registry.Register("test.destroy", &stubAdapter{
		method:   "DELETE",
		path:     "/destroy",
		response: map[string]any{},
	}))
	_ = destroyCalled // tracked by cleanup results

	executor := adapter.NewHTTPExecutor(server.URL)
	engine := NewEngine(g, registry, NewExecutorRouter(executor, &adapter.EnvironmentConfig{}))

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "create"},
				{Node: "fail", DependsOn: []string{"create"}, Values: map[string]plan.StepValue{
					"resourceId": {From: "create.resourceId"},
				}},
			},
		},
	}

	result := engine.Run(context.Background(), p)

	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.Error(t, result.Error)
	require.Len(t, result.Steps, 2)
	assert.Equal(t, 500, result.Steps[1].StatusCode)

	// Cleanup should have run even though the flow failed
	require.Len(t, result.CleanupResults, 1)
	assert.Equal(t, "destroy", result.CleanupResults[0].Node)
}

func TestEngine_Run_DeleteNoBody(t *testing.T) {
	// Simulates ignoreWorkbench: DELETE with no response body
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"create": {
				Name:    "create",
				Adapter: "test.create",
				Inputs:  []graph.Input{},
				Outputs: []graph.Output{
					{Name: "id", Type: "string"},
				},
				Cleanup: "delete",
			},
			"delete": {
				Name:    "delete",
				Adapter: "test.delete",
				Inputs: []graph.Input{
					{Name: "id", Type: "string"},
				},
				Outputs: []graph.Output{},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "resource-789"})
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.create", &stubAdapter{
		method:   "POST",
		path:     "/create",
		response: map[string]any{"id": "resource-789"},
	}))
	require.NoError(t, registry.Register("test.delete", &stubAdapter{
		method:   "DELETE",
		path:     "/delete",
		response: map[string]any{}, // empty outputs — fine for cleanup
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	engine := NewEngine(g, registry, NewExecutorRouter(executor, &adapter.EnvironmentConfig{}))

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "create"},
			},
		},
	}

	result := engine.Run(context.Background(), p)

	assert.Equal(t, OutcomePassed, result.Outcome)
	require.Len(t, result.CleanupResults, 1)
	assert.Equal(t, "delete", result.CleanupResults[0].Node)
	// 204 No Content is success
	assert.Equal(t, 204, result.CleanupResults[0].StatusCode)
	assert.NoError(t, result.CleanupResults[0].Error)
}
