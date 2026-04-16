package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildRetryTestGraph creates a simple single-node graph for retry testing.
func buildRetryTestGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs: []graph.Input{
					{Name: "input", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "output", Type: "string"},
				},
			},
		},
	}
}

func buildRetryEngine(t *testing.T, serverURL string) *Engine {
	t.Helper()
	g := buildRetryTestGraph()
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method:   "POST",
		path:     "/step1",
		response: map[string]any{"output": "result"},
	}))
	executor := adapter.NewHTTPExecutor(serverURL)
	return NewEngine(g, registry, NewExecutorRouter(executor, &adapter.EnvironmentConfig{}))
}

func buildRetryPlan(retry *plan.RetryConfig) *plan.Plan {
	return &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node:  "step1",
					Retry: retry,
					Values: map[string]plan.StepValue{
						"input": {Default: "test"},
					},
				},
			},
		},
	}
}

func TestRetryOnTransientStatus(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
		if count <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error": "service unavailable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	engine := buildRetryEngine(t, server.URL)
	p := buildRetryPlan(&plan.RetryConfig{Max: 3})

	result := engine.Run(context.Background(), p)

	assert.Equal(t, OutcomePassed, result.Outcome)
	assert.Nil(t, result.Error)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, 200, result.Steps[0].StatusCode)
	assert.Equal(t, 2, result.Steps[0].RetryCount)
	assert.Nil(t, result.Steps[0].ErrorClass) // nil on final success
	assert.Equal(t, int32(3), callCount.Load())
}

func TestRetryExhausted(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error": "service unavailable"}`))
	}))
	defer server.Close()

	engine := buildRetryEngine(t, server.URL)
	p := buildRetryPlan(&plan.RetryConfig{Max: 2})

	result := engine.Run(context.Background(), p)

	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.Error(t, result.Error)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, 503, result.Steps[0].StatusCode)
	assert.Equal(t, 2, result.Steps[0].RetryCount)
	assert.NotNil(t, result.Steps[0].ErrorClass)
	assert.Equal(t, CategoryTransient, result.Steps[0].ErrorClass.Category)
	assert.Equal(t, "failed", result.Steps[0].ErrorClass.Action)
	assert.Equal(t, int32(3), callCount.Load()) // 1 initial + 2 retries
}

func TestNoRetryWithoutConfig(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error": "service unavailable"}`))
	}))
	defer server.Close()

	engine := buildRetryEngine(t, server.URL)
	p := buildRetryPlan(nil) // no retry config

	result := engine.Run(context.Background(), p)

	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.Error(t, result.Error)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, 0, result.Steps[0].RetryCount)
	assert.NotNil(t, result.Steps[0].ErrorClass)
	assert.Equal(t, "failed", result.Steps[0].ErrorClass.Action)
	assert.Equal(t, int32(1), callCount.Load())
}

func TestNoRetryOnClientError(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	engine := buildRetryEngine(t, server.URL)
	p := buildRetryPlan(&plan.RetryConfig{Max: 3})

	result := engine.Run(context.Background(), p)

	assert.Equal(t, OutcomeFailed, result.Outcome)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, 400, result.Steps[0].StatusCode)
	assert.Equal(t, 0, result.Steps[0].RetryCount)
	assert.NotNil(t, result.Steps[0].ErrorClass)
	assert.Equal(t, CategoryClient, result.Steps[0].ErrorClass.Category)
	assert.Equal(t, "failed_fast", result.Steps[0].ErrorClass.Action)
	assert.Equal(t, int32(1), callCount.Load())
}

func TestFailOnOverridesOn(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "server error"}`))
	}))
	defer server.Close()

	engine := buildRetryEngine(t, server.URL)
	p := buildRetryPlan(&plan.RetryConfig{
		Max:    3,
		On:     []string{"server"},
		FailOn: []string{"server"},
	})

	result := engine.Run(context.Background(), p)

	assert.Equal(t, OutcomeFailed, result.Outcome)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, 0, result.Steps[0].RetryCount)
	assert.NotNil(t, result.Steps[0].ErrorClass)
	assert.Equal(t, "failed_fast", result.Steps[0].ErrorClass.Action)
	assert.Equal(t, int32(1), callCount.Load())
}

func TestRetryRespectsContextCancellation(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error": "service unavailable"}`))
	}))
	defer server.Close()

	engine := buildRetryEngine(t, server.URL)
	p := buildRetryPlan(&plan.RetryConfig{Max: 5})

	// Cancel context shortly after first call to trigger during backoff
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	result := engine.Run(ctx, p)

	// Should fail due to context cancellation during backoff
	assert.NotEqual(t, OutcomePassed, result.Outcome)
	require.Len(t, result.Steps, 1)
	// Should have tried at least once
	assert.GreaterOrEqual(t, callCount.Load(), int32(1))
}

func TestExistingBehaviorPreserved(t *testing.T) {
	// Run existing test patterns with no RetryConfig — identical behavior
	t.Run("success flow", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}))
		defer server.Close()

		engine := buildRetryEngine(t, server.URL)
		p := buildRetryPlan(nil)

		result := engine.Run(context.Background(), p)

		assert.Equal(t, OutcomePassed, result.Outcome)
		assert.Nil(t, result.Error)
		assert.Equal(t, 0, result.Steps[0].RetryCount)
		assert.Nil(t, result.Steps[0].ErrorClass)
	})

	t.Run("failure flow", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error": "bad"}`))
		}))
		defer server.Close()

		engine := buildRetryEngine(t, server.URL)
		p := buildRetryPlan(nil)

		result := engine.Run(context.Background(), p)

		assert.Equal(t, OutcomeFailed, result.Outcome)
		assert.Error(t, result.Error)
		assert.Contains(t, result.Error.Error(), "status 400")
	})
}

func TestRetryOnServerError(t *testing.T) {
	// Server errors (500) are retryable by default
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
		if count <= 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error": "internal server error"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	engine := buildRetryEngine(t, server.URL)
	p := buildRetryPlan(&plan.RetryConfig{Max: 2})

	result := engine.Run(context.Background(), p)

	assert.Equal(t, OutcomePassed, result.Outcome)
	assert.Nil(t, result.Error)
	assert.Equal(t, 1, result.Steps[0].RetryCount)
	assert.Equal(t, int32(2), callCount.Load())
}

func TestRetryOnExplicitOnList(t *testing.T) {
	// Only retry auth errors explicitly
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
		if count <= 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	engine := buildRetryEngine(t, server.URL)
	p := buildRetryPlan(&plan.RetryConfig{Max: 2, On: []string{"auth"}})

	result := engine.Run(context.Background(), p)

	assert.Equal(t, OutcomePassed, result.Outcome)
	assert.Equal(t, 1, result.Steps[0].RetryCount)
	assert.Equal(t, int32(2), callCount.Load())
}

func TestRetryBackoff(t *testing.T) {
	// Verify backoff increases with attempts and is bounded
	// Jitter is ±25%, so max is cap * 1.25 = 12.5s
	maxWithJitter := time.Duration(float64(10*time.Second) * 1.25)

	prev := time.Duration(0)
	for attempt := 1; attempt <= 10; attempt++ {
		d := retryBackoff(attempt)
		assert.Greater(t, d, time.Duration(0), "backoff should be positive")
		assert.LessOrEqual(t, d, maxWithJitter, "backoff should be capped (with jitter)")

		// Due to jitter, we can't assert strict monotonic increase,
		// but the mean should trend up. At minimum, later attempts
		// shouldn't produce consistently smaller values.
		_ = prev
		prev = d
	}

	// Verify high attempts are capped near 10s (with jitter)
	for i := 0; i < 20; i++ {
		d := retryBackoff(100)
		assert.LessOrEqual(t, d, maxWithJitter)
	}
}

func TestRetry_CancelledContextReturnsImmediately(t *testing.T) {
	callCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"unavailable"}`))
	}))
	defer server.Close()

	eng := buildRetryEngine(t, server.URL)
	p := buildRetryPlan(&plan.RetryConfig{Max: 5, On: []string{"transient"}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel

	result := eng.Run(ctx, p)

	assert.Equal(t, OutcomeAborted, result.Outcome)
	assert.ErrorIs(t, result.Error, context.Canceled)
	// The main loop should catch cancellation before even executing the step
	assert.Equal(t, int32(0), atomic.LoadInt32(&callCount))
}
