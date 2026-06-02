package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stopAfterGraph builds a two-step graph where "create" has a cleanup node so
// we can assert that stopping early skips cleanup.
func stopAfterGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"create": {
				Name:    "create",
				Adapter: "test.create",
				Inputs:  []graph.Input{},
				Outputs: []graph.Output{{Name: "resourceId", Type: "string"}},
				Cleanup: "destroy",
			},
			"use": {
				Name:    "use",
				Adapter: "test.use",
				Inputs:  []graph.Input{{Name: "resourceId", Type: "string"}},
				Outputs: []graph.Output{},
			},
			"destroy": {
				Name:    "destroy",
				Adapter: "test.destroy",
				Inputs:  []graph.Input{{Name: "resourceId", Type: "string"}},
				Outputs: []graph.Output{},
			},
		},
	}
}

func stopAfterPlan() *plan.Plan {
	return &plan.Plan{
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
}

func TestEngine_Run_StopAfter_SkipsCleanupAndLaterSteps(t *testing.T) {
	g := stopAfterGraph()

	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.create", &stubAdapter{
		method: "POST", path: "/create", response: map[string]any{"resourceId": "res-123"},
	}))
	require.NoError(t, registry.Register("test.use", &stubAdapter{
		method: "POST", path: "/use", response: map[string]any{},
	}))
	require.NoError(t, registry.Register("test.destroy", &stubAdapter{
		method: "DELETE", path: "/destroy", response: map[string]any{},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	eng := NewEngine(g, registry, NewExecutorRouter(executor, &adapter.EnvironmentConfig{})).
		WithStopAfter("create")

	result := eng.Run(context.Background(), stopAfterPlan())

	assert.Equal(t, OutcomeStopped, result.Outcome)
	assert.True(t, result.Stopped)
	assert.Equal(t, "create", result.StoppedAt)
	assert.NoError(t, result.Error)

	// Only the checkpoint step executed; "use" never ran.
	require.Len(t, result.Steps, 1)
	assert.Equal(t, "create", result.Steps[0].StepID)

	// Cleanup was skipped — resources stay alive for handoff.
	assert.Empty(t, result.CleanupResults)
	assert.Equal(t, []string{"/create"}, paths, "only the create request should have been issued")
}

func TestEngine_Run_StopAfter_UnknownStep(t *testing.T) {
	g := stopAfterGraph()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.create", &stubAdapter{method: "POST", path: "/create", response: map[string]any{"resourceId": "x"}}))
	require.NoError(t, registry.Register("test.use", &stubAdapter{method: "POST", path: "/use", response: map[string]any{}}))
	require.NoError(t, registry.Register("test.destroy", &stubAdapter{method: "DELETE", path: "/destroy", response: map[string]any{}}))

	executor := adapter.NewHTTPExecutor(server.URL)
	eng := NewEngine(g, registry, NewExecutorRouter(executor, &adapter.EnvironmentConfig{})).
		WithStopAfter("nope")

	result := eng.Run(context.Background(), stopAfterPlan())

	assert.Equal(t, OutcomeError, result.Outcome)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), `no step "nope"`)
	assert.Empty(t, result.Steps, "no steps should run when the checkpoint is invalid")
}
