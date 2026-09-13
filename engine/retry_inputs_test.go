package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// buildOrderEngine returns an engine whose createOrder node sends its ref input
// in a JSON body to serverURL.
func buildOrderEngine(t *testing.T, serverURL string) *Engine {
	t.Helper()
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"createOrder": {
				Name:    "createOrder",
				Adapter: "test.createOrder",
				Inputs:  []graph.Input{{Name: "ref", Type: "string"}},
			},
		},
	}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.createOrder", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "test.createOrder",
		Protocol: "http",
		Request: adapter.TemplateRequest{
			Method:  "POST",
			Path:    "/orders",
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    `{"ref": "{{ref}}"}`,
		},
	})))
	return NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(serverURL), &adapter.EnvironmentConfig{}))
}

// TestRetry_ResendsSameInputs checks that a retried step sends the request its
// first attempt sent, even when a value is drawn at random from a pool.
func TestRetry_ResendsSameInputs(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		first := len(bodies) == 1
		mu.Unlock()
		if first {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	pool := make([]any, 20)
	for i := range pool {
		pool[i] = fmt.Sprintf("ref-%02d", i)
	}
	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{{
			Node:   "createOrder",
			Retry:  &plan.RetryConfig{Max: 2, On: []string{"transient"}},
			Values: map[string]plan.StepValue{"ref": {Pool: pool}},
		}}},
	}

	result := buildOrderEngine(t, server.URL).Run(context.Background(), p)

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, 1, result.Steps[0].RetryCount)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 2)
	assert.Equal(t, bodies[0], bodies[1], "the retry sends the same request")
}

// TestRetry_ResolutionFailureStillRetries checks that a failed resolution is not
// kept for later attempts: each attempt resolves again. The value's expression
// fails only at run time, since plan validation doesn't evaluate expressions.
func TestRetry_ResolutionFailureStillRetries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request is sent when the inputs don't resolve")
	}))
	defer server.Close()

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{{
			Node:   "createOrder",
			Retry:  &plan.RetryConfig{Max: 2, On: []string{"adapter"}},
			Values: map[string]plan.StepValue{"ref": {Default: "{{env.AAT_TEST_RETRY_UNSET_VARIABLE}}"}},
		}}},
	}

	result := buildOrderEngine(t, server.URL).Run(context.Background(), p)

	assert.Equal(t, OutcomeError, result.Outcome)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, 2, result.Steps[0].RetryCount)
	require.Error(t, result.Steps[0].Error)
	assert.Contains(t, result.Steps[0].Error.Error(), "resolving inputs")
}
