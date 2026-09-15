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

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// maxRetryAfter caps how long a step waits when the server asks it to wait
// before retrying. A server that asks for longer ends the step's retries.
const maxRetryAfter = 60 * time.Second

// executeStepWithTracking runs a step: until its condition holds when it has a
// repeat block (see executeStepRepeated), and otherwise once, with its retries.
func (e *Engine) executeStepWithTracking(ctx context.Context, step plan.Step, node *graph.Node, state *RunState) StepResult {
	if step.Repeat != nil {
		return e.executeStepRepeated(ctx, step, node, state)
	}
	return e.executeStepWithRetry(ctx, step, node, state, nil)
}

// executeStepWithRetry wraps executeStepWith with retry logic based on the
// step's RetryConfig. If no RetryConfig is set, it runs a single attempt.
// The result of a retried step is its last attempt's, timed from the start of
// the first attempt, so its duration includes the failed attempts and the
// waits between them. Every attempt sends the inputs prepared holds, resolving
// them first when it holds none; nil starts from none.
func (e *Engine) executeStepWithRetry(ctx context.Context, step plan.Step, node *graph.Node, state *RunState, prepared *stepInputs) (result StepResult) {
	// Every attempt shares the inputs the first resolved, so a retry resends
	// the same request. A repeated step passes its own, which every one of its
	// requests shares.
	if prepared == nil {
		prepared = &stepInputs{}
	}
	result = e.executeStepWith(ctx, step, node, state, prepared)
	firstStart := result.StartTime
	defer func() {
		if result.StartTime.After(firstStart) {
			end := result.StartTime.Add(result.Duration)
			result.StartTime = firstStart
			result.Duration = end.Sub(firstStart)
		}
	}()

	// Negative assertion steps should not retry — the failure IS the expected behavior.
	if step.ExpectFailure != nil {
		return result
	}

	if step.Retry == nil {
		// No retry config — classify for reporting but don't retry
		if cls := classifyStepResult(&result); cls != nil {
			cls.Action = "failed"
			result.ErrorClass = cls
		}
		return result
	}

	// Check if the first attempt succeeded
	cls := classifyStepResult(&result)
	if cls == nil {
		return result // success on first try
	}

	// Retry loop. retriedOn keeps the category of every failed attempt that was
	// retried, so a step that eventually succeeds still reports why it retried.
	var retriedOn []ErrorCategory
	for attempt := 1; attempt <= step.Retry.Max; attempt++ {
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

		if !shouldRetry(cls.Category, result.StatusCode, step.Retry, attempt) {
			cls.Action = "failed_fast"
			cls.RetryAttempt = attempt - 1
			result.ErrorClass = cls
			result.RetryCount = attempt - 1
			return result
		}

		// Wait out the backoff, or as long as the server asked if that is longer.
		wait := retryBackoff(attempt)
		if result.Response != nil {
			if after, ok := retryAfter(result.Response.Headers, result.StatusCode, time.Now()); ok {
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
		result = e.executeStepWith(ctx, step, node, state, prepared)
		result.RetryCount = attempt
		result.RetriedOn = append([]ErrorCategory(nil), retriedOn...)

		cls = classifyStepResult(&result)
		if cls == nil {
			return result // success after retry
		}
	}

	// All retries exhausted
	cls.Action = "failed"
	cls.RetryAttempt = step.Retry.Max
	result.ErrorClass = cls
	return result
}

// retryBackoff calculates the backoff duration for a given attempt using
// exponential backoff with jitter. Base is 500ms, multiplier is 2x per attempt,
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
func retryAfter(h http.Header, status int, now time.Time) (wait time.Duration, ok bool) {
	value := strings.TrimSpace(h.Get("Retry-After"))
	if value == "" && status == http.StatusTooManyRequests {
		value = strings.TrimSpace(h.Get("RateLimit-Reset"))
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
