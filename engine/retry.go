package engine

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// maxRetryAfter caps how long a step waits when the server asks it to wait
// before retrying. A server that asks for longer ends the step's retries.
const maxRetryAfter = 60 * time.Second

// rateLimitRetry is what a step without a retry block retries: a request the
// API turned away with a rate limit, up to three times, after the wait the
// server asks for. A 429 says the request was not looked at, so sending it
// again is safe whatever the method, and it says nothing about the behaviour
// the step tests. Every other failure stands: a server error may be the
// finding, and a request that timed out may have gone through.
var rateLimitRetry = &plan.RetryConfig{Max: 3, On: []string{strconv.Itoa(http.StatusTooManyRequests)}}

// cleanupRetry is what a cleanup step retries: what a retry block without
// rules retries, so a transient failure, a timeout, or a server error is sent
// again up to three times. A cleanup that gives up leaves a resource behind,
// so it tries as hard as a step told to retry does.
var cleanupRetry = &plan.RetryConfig{Max: 3}

// executeStepWithTracking runs a step: until its condition holds when it has a
// repeat block (see executeStepRepeated), and otherwise once, with its retries.
func (e *Engine) executeStepWithTracking(ctx context.Context, step plan.Step, node *graph.Node, state *RunState) StepResult {
	if step.Repeat != nil {
		return e.executeStepRepeated(ctx, step, node, state)
	}
	return e.executeStepWithRetry(ctx, step, node, state, nil)
}

// executeStepWithRetry runs a step and retries it under its retry policy (see
// retryPolicy). The result of a retried step is its last attempt's, timed from
// the start of the first attempt, so its duration includes the failed attempts
// and the waits between them. Every attempt sends the inputs prepared holds,
// resolving them first when it holds none; nil starts from none.
func (e *Engine) executeStepWithRetry(ctx context.Context, step plan.Step, node *graph.Node, state *RunState, prepared *stepInputs) StepResult {
	// Every attempt shares the inputs the first resolved, so a retry resends
	// the same request. A repeated step passes its own, which every one of its
	// requests shares.
	if prepared == nil {
		prepared = &stepInputs{}
	}
	result := e.executeStepWith(ctx, step, node, state, prepared)
	cfg, written := retryPolicy(step)
	if cfg == nil {
		return result
	}
	failed := classifyStepResult
	if step.ExpectFailure != nil {
		failed = unexpectedRateLimit(step)
	}
	return retryResult(ctx, result, cfg, written, failed, func() StepResult {
		return e.executeStepWith(ctx, step, node, state, prepared)
	})
}

// retryPolicy returns the retry block a step runs under, and whether the step
// wrote it. A step without one retries a rate limit (see rateLimitRetry). A
// negative test's failure is the behaviour it expects, so its own block never
// retries it; it retries a rate limit it does not expect as any step does,
// unless its block turns retries off with max 0. nil means no retries at all.
func retryPolicy(step plan.Step) (cfg *plan.RetryConfig, written bool) {
	if step.ExpectFailure != nil {
		if step.Retry != nil && step.Retry.Max == 0 {
			return nil, false
		}
		return rateLimitRetry, false
	}
	if step.Retry == nil {
		return rateLimitRetry, false
	}
	return step.Retry, true
}

// unexpectedRateLimit classifies a negative test's result only when the API
// rate-limited the request and the step does not expect a 429. The failure the
// step expects is its success, and any other outcome stands.
func unexpectedRateLimit(step plan.Step) func(*StepResult) *ErrorClassification {
	return func(r *StepResult) *ErrorClassification {
		status, name := r.FailureStatus()
		if r.Response == nil || status != http.StatusTooManyRequests || step.ExpectFailure.Status.Matches(status, name) {
			return nil
		}
		return classifyStepResult(r)
	}
}

// retryResult retries a failed request under cfg: first is the first attempt's
// result, failed says why a result is a failure the policy may retry (nil when
// it is not one), and again sends the request once more. The result is the
// last attempt's, timed from the first's start, with the category of every
// retried attempt and, on failure, the classification of the last. written is
// whether the step wrote cfg: a failure a step's own rules refuse to retry is
// failed_fast, where one a policy the step did not write does not cover is
// simply failed.
func retryResult(ctx context.Context, first StepResult, cfg *plan.RetryConfig, written bool, failed func(*StepResult) *ErrorClassification, again func() StepResult) (result StepResult) {
	result = first
	cls := failed(&result)
	if cls == nil {
		return result // success on first try
	}
	defer func() {
		if result.StartTime.After(first.StartTime) {
			end := result.StartTime.Add(result.Duration)
			result.StartTime = first.StartTime
			result.Duration = end.Sub(first.StartTime)
		}
	}()
	refused := "failed"
	if written {
		refused = "failed_fast"
	}

	// Retry loop. retriedOn keeps the category of every failed attempt that was
	// retried, so a step that eventually succeeds still reports why it retried.
	var retriedOn []ErrorCategory
	for attempt := 1; attempt <= cfg.Max; attempt++ {
		select {
		case <-ctx.Done():
			result.Error = ctx.Err()
			result.ErrorClass = &ErrorClassification{
				Category:     CategoryTimeout,
				Detail:       ctx.Err().Error(),
				Action:       "failed",
				RetryAttempt: attempt,
			}
			result.RetryCount = attempt - 1
			result.RetriedOn = append([]ErrorCategory(nil), retriedOn...)
			return result
		default:
		}

		failureStatus, failureName := result.FailureStatus()
		if !shouldRetry(retryFailure{category: cls.Category, status: failureStatus, grpcName: failureName}, cfg, attempt) {
			cls.Action = refused
			cls.RetryAttempt = attempt - 1
			result.ErrorClass = cls
			result.RetryCount = attempt - 1
			return result
		}

		// Wait out the backoff, or as long as the server asked if that is longer.
		wait := retryBackoff(attempt)
		if result.Response != nil {
			if after, ok := retryAfter(result.Response, failureStatus, time.Now()); ok {
				if after > maxRetryAfter {
					cls.Action = "failed_fast"
					cls.Detail += fmt.Sprintf("; the server asked to wait %s before retrying, longer than the %s limit",
						after.Round(time.Second), maxRetryAfter)
					cls.RetryAttempt = attempt - 1
					result.ErrorClass = cls
					result.RetryCount = attempt - 1
					return result
				}
				wait = max(wait, after)
			}
		}

		// Mark the classification as retried
		cls.Action = "retried"
		cls.RetryAttempt = attempt - 1
		retriedOn = append(retriedOn, cls.Category)

		// Wait, respecting context cancellation
		select {
		case <-ctx.Done():
			result.Error = ctx.Err()
			result.ErrorClass = &ErrorClassification{
				Category:     CategoryTimeout,
				Detail:       ctx.Err().Error(),
				Action:       "failed",
				RetryAttempt: attempt,
			}
			result.RetryCount = attempt
			result.RetriedOn = append([]ErrorCategory(nil), retriedOn...)
			return result
		case <-time.After(wait):
		}

		// Retry the step
		result = again()
		result.RetryCount = attempt
		result.RetriedOn = append([]ErrorCategory(nil), retriedOn...)

		cls = failed(&result)
		if cls == nil {
			return result // success after retry
		}
	}

	// All retries exhausted
	cls.Action = "failed"
	cls.RetryAttempt = cfg.Max
	result.ErrorClass = cls
	return result
}

// capped at 10 seconds.
func retryBackoff(attempt int) time.Duration {
	base := 500 * time.Millisecond
	maxBackoff := 10 * time.Second

	backoff := time.Duration(float64(base) * math.Pow(2, float64(attempt-1)))
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	// Add jitter: ±25%
	jitter := time.Duration(float64(backoff) * (0.75 + rand.Float64()*0.5))
	return jitter
}

// retryAfter reports how long a failed response asked the client to wait
// before retrying: its Retry-After header or, on a 429 without one, its
// RateLimit-Reset header. Either may be a number of seconds or an HTTP date
// (RFC 9110); a date already past means no wait. ok is false when the response
// names no usable delay.
func retryAfter(resp *adapter.Response, status int, now time.Time) (wait time.Duration, ok bool) {
	if resp == nil {
		return 0, false
	}
	// Read through HeaderValues rather than http.Header.Get: gRPC metadata
	// keys are lowercase as the wire sent them, and Get canonicalizes the key
	// it looks for. It also spans trailers, which is where a gRPC server may
	// put a retry hint it only knows at the end of the call.
	first := func(name string) string {
		if values := resp.HeaderValues(name); len(values) > 0 {
			return values[0]
		}
		return ""
	}
	value := strings.TrimSpace(first("Retry-After"))
	if value == "" && status == http.StatusTooManyRequests {
		value = strings.TrimSpace(first("RateLimit-Reset"))
	}
	if value == "" {
		return 0, false
	}
	if strings.Trim(value, "0123456789") == "" {
		secs, err := strconv.ParseInt(value, 10, 64)
		if err != nil || secs > int64(math.MaxInt64/time.Second) {
			return time.Duration(math.MaxInt64), true
		}
		return time.Duration(secs) * time.Second, true
	}
	at, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	return max(at.Sub(now), 0), true
}
