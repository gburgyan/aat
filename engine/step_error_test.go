package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// runGetOrder runs a one-step plan on node getOrder, sending its request
// through executor.
func runGetOrder(t *testing.T, executor *adapter.HTTPExecutor) *RunResult {
	t.Helper()
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"getOrder": {Name: "getOrder", Adapter: "test.getOrder"},
		},
	}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.getOrder", &stubAdapter{
		method:   "GET",
		path:     "/orders/1",
		response: map[string]any{"orderId": "1"},
	}))
	eng := NewEngine(g, registry, NewExecutorRouter(executor, &adapter.EnvironmentConfig{}))
	p := &plan.Plan{
		Metadata:  plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{{Node: "getOrder"}}},
	}
	return eng.Run(context.Background(), p)
}

func TestEngine_Run_StepErrorNamesStep(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	baseURL := srv.URL
	srv.Close()

	result := runGetOrder(t, adapter.NewHTTPExecutor(baseURL))

	assert.Equal(t, OutcomeError, result.Outcome)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), `step "getOrder": executing HTTP request`)
}

func TestEngine_Run_RequestTimeoutNamesLimitAndStep(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	result := runGetOrder(t, adapter.NewHTTPExecutorWithClient(srv.URL, &http.Client{Timeout: 50 * time.Millisecond}))

	assert.Equal(t, OutcomeError, result.Outcome)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), `step "getOrder": executing HTTP request: no response within aat's 50ms request timeout`)
}
