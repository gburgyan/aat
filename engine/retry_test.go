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
	assert.Equal(t, []ErrorCategory{CategoryTransient, CategoryTransient}, result.Steps[0].RetriedOn,
		"a step that recovers still records why it retried")
	assert.Equal(t, int32(3), callCount.Load())

	// The step is timed from its first attempt, so its duration includes both
	// backoff waits (at least 375ms and 750ms after jitter).
	step := result.Steps[0]
	assert.GreaterOrEqual(t, step.Duration, 1125*time.Millisecond, "retry waits count toward the step's duration")
	assert.False(t, step.StartTime.Before(result.StartTime), "the step starts inside the run")
	assert.GreaterOrEqual(t, result.Duration, step.Duration, "the run lasts at least as long as its step")
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

	// A run cancelled while a step waits to retry is aborted, not an error.
	assert.Equal(t, OutcomeAborted, result.Outcome)
	assert.ErrorIs(t, result.Error, context.DeadlineExceeded)
	require.Len(t, result.Steps, 1)
	// Should have tried at least once
	assert.GreaterOrEqual(t, callCount.Load(), int32(1))
	assert.Equal(t, []ErrorCategory{CategoryTransient}, result.Steps[0].RetriedOn, "the step still says why it was retrying")
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

func TestRetryOnStatusCodeRule(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if callCount.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error": "service unavailable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	result := buildRetryEngine(t, server.URL).Run(context.Background(), buildRetryPlan(&plan.RetryConfig{Max: 3, On: []string{"503"}}))

	assert.Equal(t, OutcomePassed, result.Outcome)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, 2, result.Steps[0].RetryCount)
	assert.Equal(t, int32(3), callCount.Load())
}

func TestRetryStatusCodeRuleIgnoresOtherStatuses(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error": "bad gateway"}`))
	}))
	defer server.Close()

	result := buildRetryEngine(t, server.URL).Run(context.Background(), buildRetryPlan(&plan.RetryConfig{Max: 3, On: []string{"503"}}))

	assert.Equal(t, OutcomeFailed, result.Outcome)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, 0, result.Steps[0].RetryCount, "502 does not match the 503 rule")
	assert.Equal(t, int32(1), callCount.Load())
}

func TestRetryAfter_Parse(t *testing.T) {
	now := time.Date(2026, 9, 12, 19, 19, 30, 0, time.UTC)
	tests := []struct {
		name   string
		header string
		value  string
		status int
		want   time.Duration
		ok     bool
	}{
		{"seconds", "Retry-After", "1", 503, time.Second, true},
		{"padded seconds", "Retry-After", " 5 ", 429, 5 * time.Second, true},
		{"zero", "Retry-After", "0", 503, 0, true},
		{"IMF-fixdate", "Retry-After", "Sat, 12 Sep 2026 19:20:00 GMT", 503, 30 * time.Second, true},
		{"RFC 850 date", "Retry-After", "Saturday, 12-Sep-26 19:20:00 GMT", 503, 30 * time.Second, true},
		{"ANSI C date", "Retry-After", "Sat Sep 12 19:20:00 2026", 503, 30 * time.Second, true},
		{"past date", "Retry-After", "Sat, 12 Sep 2026 19:00:00 GMT", 503, 0, true},
		{"negative", "Retry-After", "-1", 503, 0, false},
		{"fraction", "Retry-After", "1.5", 503, 0, false},
		{"text", "Retry-After", "soon", 503, 0, false},
		{"absent", "Retry-After", "", 503, 0, false},
		{"rate-limit reset date on a 429", "RateLimit-Reset", "Sat, 12 Sep 2026 19:20:00 GMT", 429, 30 * time.Second, true},
		{"rate-limit reset seconds on a 429", "RateLimit-Reset", "12", 429, 12 * time.Second, true},
		{"rate-limit reset ignored on a 503", "RateLimit-Reset", "12", 503, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			if tt.value != "" {
				h.Set(tt.header, tt.value)
			}
			got, ok := retryAfter(h, tt.status, now)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// retryAfterServer answers the first request with status and a Retry-After of
// retryAfterValue, and every later request with 200.
func retryAfterServer(t *testing.T, calls *atomic.Int32, status int, retryAfterValue string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", retryAfterValue)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error": "slow down"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRetryHonorsRetryAfter(t *testing.T) {
	var calls atomic.Int32
	server := retryAfterServer(t, &calls, http.StatusTooManyRequests, "1")

	result := buildRetryEngine(t, server.URL).Run(context.Background(), buildRetryPlan(&plan.RetryConfig{Max: 2}))

	assert.Equal(t, OutcomePassed, result.Outcome)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, 1, result.Steps[0].RetryCount)
	assert.Equal(t, int32(2), calls.Load())
	assert.GreaterOrEqual(t, result.Steps[0].Duration, time.Second, "the retry waited the server's second, not the shorter backoff")
}

func TestRetryAfterZeroKeepsBackoff(t *testing.T) {
	var calls atomic.Int32
	server := retryAfterServer(t, &calls, http.StatusServiceUnavailable, "0")

	result := buildRetryEngine(t, server.URL).Run(context.Background(), buildRetryPlan(&plan.RetryConfig{Max: 2}))

	assert.Equal(t, OutcomePassed, result.Outcome)
	require.Len(t, result.Steps, 1)
	assert.GreaterOrEqual(t, result.Steps[0].Duration, 375*time.Millisecond, "the backoff still applies")
}

func TestRetryAfterBeyondLimitFailsFast(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	start := time.Now()
	result := buildRetryEngine(t, server.URL).Run(context.Background(), buildRetryPlan(&plan.RetryConfig{Max: 3}))

	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.Equal(t, int32(1), calls.Load(), "no retry is sent before the server's time")
	assert.Less(t, time.Since(start), time.Second)
	require.Len(t, result.Steps, 1)
	require.NotNil(t, result.Steps[0].ErrorClass)
	assert.Equal(t, "failed_fast", result.Steps[0].ErrorClass.Action)
	assert.Equal(t, "HTTP 429 Too Many Requests; the server asked to wait 1h0m0s before retrying, longer than the 1m0s limit",
		result.Steps[0].ErrorClass.Detail)
}

func TestRetryAfterWaitHonorsContext(t *testing.T) {
	var calls atomic.Int32
	server := retryAfterServer(t, &calls, http.StatusServiceUnavailable, "30")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	result := buildRetryEngine(t, server.URL).Run(ctx, buildRetryPlan(&plan.RetryConfig{Max: 2}))

	assert.Equal(t, OutcomeAborted, result.Outcome)
	assert.Less(t, time.Since(start), 5*time.Second, "a cancelled run does not sit out the server's 30 seconds")
	assert.Equal(t, int32(1), calls.Load())
}
