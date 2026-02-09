package engine

import (
	"context"
	"math"
	"math/rand"
	"time"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// executeStepWithTracking wraps executeStepWithRetry, passing the tracker through
// and attaching relaxation records to the result.
func (e *Engine) executeStepWithTracking(ctx context.Context, step plan.Step, node *graph.Node, state *RunState, tracker *RelaxationTracker) StepResult {
	result := e.executeStepWithRetry(ctx, step, node, state, tracker)
	if tracker != nil {
		result.Relaxations = tracker.Records()
	}
	return result
}

// executeStepWithRetry wraps executeStep with retry logic based on the step's
// RetryConfig. If no RetryConfig is set, it behaves identically to executeStep.
func (e *Engine) executeStepWithRetry(ctx context.Context, step plan.Step, node *graph.Node, state *RunState, tracker *RelaxationTracker) StepResult {
	result := e.executeStep(ctx, step, node, state, tracker)

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

	// Retry loop
	for attempt := 1; attempt <= step.Retry.Max; attempt++ {
		if !shouldRetry(cls.Category, step.Retry, attempt) {
			cls.Action = "failed_fast"
			cls.RetryAttempt = attempt - 1
			result.ErrorClass = cls
			result.RetryCount = attempt - 1
			return result
		}

		// Mark the classification as retried
		cls.Action = "retried"
		cls.RetryAttempt = attempt - 1

		// Wait with backoff, respecting context cancellation
		backoff := retryBackoff(attempt)
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
			return result
		case <-time.After(backoff):
		}

		// Retry the step
		result = e.executeStep(ctx, step, node, state, tracker)
		result.RetryCount = attempt

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
