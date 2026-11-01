package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cleanupRetryEngine builds a create → destroy graph whose destroy node has a
// cleanup of its own (confirm), against a server that answers /destroy as
// destroy says and everything else with 200.
func cleanupRetryEngine(t *testing.T, destroy func(w http.ResponseWriter)) (*Engine, *plan.Plan, *atomic.Int32) {
	t.Helper()
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"create": {Name: "create", Adapter: "test.create",
				Outputs: []graph.Output{{Name: "resourceId", Type: "string"}},
				Cleanup: graph.CleanupPairing{Node: "destroy"}},
			"destroy": {Name: "destroy", Adapter: "test.destroy",
				Inputs:  []graph.Input{{Name: "resourceId", Type: "string"}},
				Outputs: []graph.Output{{Name: "ticket", Type: "string"}},
				Cleanup: graph.CleanupPairing{Node: "confirm"}},
			"confirm": {Name: "confirm", Adapter: "test.confirm",
				Inputs: []graph.Input{{Name: "ticket", Type: "string"}}},
		},
	}
	var destroyCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/destroy") {
			destroyCalls.Add(1)
			destroy(w)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resourceId": "res-1", "ticket": "t-1"}`))
	}))
	t.Cleanup(server.Close)

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.create", &stubAdapter{method: "POST", path: "/create", response: map[string]any{"resourceId": "res-1"}}))
	require.NoError(t, registry.Register("test.destroy", &stubAdapter{method: "DELETE", path: "/destroy", response: map[string]any{"ticket": "t-1"}}))
	require.NoError(t, registry.Register("test.confirm", &stubAdapter{method: "POST", path: "/confirm", response: map[string]any{}}))
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{}))
	p := &plan.Plan{
		Metadata:  plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{{Node: "create"}}},
	}
	return eng, p, &destroyCalls
}

// A cleanup step that is rate-limited or hits a transient failure is sent
// again, after the wait the server asks for, and its chain goes on once it
// succeeds.
func TestCleanupRetriesTransientFailure(t *testing.T) {
	var answers atomic.Int32
	eng, p, calls := cleanupRetryEngine(t, func(w http.ResponseWriter) {
		switch answers.Add(1) {
		case 1:
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ticket": "t-1"}`))
		}
	})

	result := eng.Run(context.Background(), p)

	assert.Equal(t, OutcomePassed, result.Outcome)
	require.Len(t, result.CleanupResults, 2, "destroy, then its chain's confirm")
	destroy := result.CleanupResults[0]
	assert.Equal(t, "destroy", destroy.StepID)
	assert.NoError(t, destroy.Error)
	assert.Equal(t, 200, destroy.StatusCode)
	assert.Equal(t, 2, destroy.RetryCount)
	assert.Equal(t, []ErrorCategory{CategoryTransient, CategoryTransient}, destroy.RetriedOn)
	assert.Nil(t, destroy.ErrorClass)
	assert.GreaterOrEqual(t, destroy.Duration, 375*time.Millisecond, "the waits count toward the cleanup step's duration")
	assert.Equal(t, int32(3), calls.Load())
	assert.Equal(t, "confirm", result.CleanupResults[1].StepID)
	assert.Equal(t, "destroy", result.CleanupResults[1].CleanupFor)
	assert.Equal(t, "t-1", result.CleanupResults[1].Inputs["ticket"])
}

// A cleanup the API refuses is not sent again: the refusal is the answer, and
// the chain stops as before.
func TestCleanupDoesNotRetryRefusal(t *testing.T) {
	eng, p, calls := cleanupRetryEngine(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error": "already released"}`))
	})

	result := eng.Run(context.Background(), p)

	assert.Equal(t, OutcomePassed, result.Outcome, "a failed cleanup never changes the outcome")
	require.Len(t, result.CleanupResults, 1, "the chain stops at the failed step")
	destroy := result.CleanupResults[0]
	assert.Equal(t, 409, destroy.StatusCode)
	assert.Equal(t, 0, destroy.RetryCount)
	require.NotNil(t, destroy.ErrorClass)
	assert.Equal(t, CategoryClient, destroy.ErrorClass.Category)
	assert.Equal(t, "failed", destroy.ErrorClass.Action)
	assert.Equal(t, int32(1), calls.Load())
}

// A cleanup that keeps failing gives up after its retries, with the last
// attempt's classification.
func TestCleanupRetriesExhausted(t *testing.T) {
	eng, p, calls := cleanupRetryEngine(t, func(w http.ResponseWriter) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	result := eng.Run(context.Background(), p)

	require.Len(t, result.CleanupResults, 1)
	destroy := result.CleanupResults[0]
	assert.Equal(t, 429, destroy.StatusCode)
	assert.Equal(t, cleanupRetry.Max, destroy.RetryCount)
	require.NotNil(t, destroy.ErrorClass)
	assert.Equal(t, "failed", destroy.ErrorClass.Action)
	assert.Equal(t, int32(cleanupRetry.Max+1), calls.Load())
}
