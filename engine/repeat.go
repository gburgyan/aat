package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/internal/predicate"
	"github.com/gburgyan/aat/plan"
	"github.com/gburgyan/aat/validate"
)

// executeStepRepeated sends a step's request until its repeat.until predicate
// holds over a response's outputs, as when polling an asynchronous job. Every
// request resends the inputs the first resolved, and each is retried under the
// step's retry block. The result is the last request's, timed from the first,
// with the repeat block's collected outputs gathered across every response,
// and the step's assertions run once, on that result. Iterations records every
// request, and RepeatStop why the step stopped. A step that reaches max or its
// timeout before the condition holds, or whose condition can't be evaluated,
// fails with a repeat result in its Validation.
func (e *Engine) executeStepRepeated(ctx context.Context, step plan.Step, node *graph.Node, state *RunState) StepResult {
	rc := step.Repeat
	maxRequests := rc.MaxRequests()
	// Validation has checked both durations; one that doesn't parse falls back.
	interval, err := rc.IntervalDuration()
	if err != nil {
		interval = plan.DefaultRepeatInterval
	}
	timeout, _ := rc.TimeoutDuration()

	polled := step
	polled.Assertions = nil // they run once, after the last request

	var (
		prepared   stepInputs
		start      = time.Now()
		collected  = map[string]any{}
		iterations []IterationResult
		retries    int
		retriedOn  []ErrorCategory
		result     StepResult
		stop       string
		failure    string // why the condition never held, for a repeat result in Validation
	)
	for n := 1; stop == ""; n++ {
		result = e.executeStepWithRetry(ctx, polled, node, state, &prepared)
		retries += result.RetryCount
		retriedOn = append(retriedOn, result.RetriedOn...)
		it := IterationResult{
			Index:         n,
			StartTime:     result.StartTime,
			Duration:      result.Duration,
			Request:       result.Request,
			Response:      result.Response,
			StatusCode:    result.StatusCode,
			Outputs:       result.Outputs,
			RetryCount:    result.RetryCount,
			Error:         result.Error,
			OASValidation: result.OASValidation,
			ActualBaseURL: result.ActualBaseURL,
			OriginalPath:  result.OriginalPath,
		}
		if !repeatable(result) {
			iterations = append(iterations, it)
			stop = RepeatStopError
			break
		}

		collectOutputs(collected, result.Outputs, rc.Collect)
		met := false
		fields, evalErr := predicateFields(result.Outputs)
		if evalErr == nil {
			met, evalErr = plan.EvalPredicateWithExprs(rc.Until, fields, e.assertionExprContext(node, prepared.inputs, prepared.now))
		}
		it.UntilMet = met
		iterations = append(iterations, it)
		switch {
		case evalErr != nil:
			stop, failure = RepeatStopError, untilError(rc.Until, evalErr)
			continue
		case met:
			stop = RepeatStopUntil
			continue
		case n >= maxRequests:
			stop = RepeatStopMax
			failure = fmt.Sprintf("repeat.until %q is still false after %d requests (repeat.max)", rc.Until, n)
			continue
		}

		wait := interval
		if after, ok := retryAfter(result.Response.Headers, result.StatusCode, time.Now()); ok {
			wait = max(wait, min(after, maxRetryAfter))
		}
		if elapsed := time.Since(start); timeout > 0 && elapsed+wait > timeout {
			stop = RepeatStopTimeout
			failure = fmt.Sprintf("repeat.until %q is still false after %d requests in %s (repeat.timeout %s)",
				rc.Until, n, elapsed.Round(time.Millisecond), timeout)
			continue
		}
		select {
		case <-ctx.Done():
			result.Error = ctx.Err()
			stop = RepeatStopError
		case <-time.After(wait):
		}
	}

	result.Iterations = iterations
	result.RepeatStop = stop
	result.StartTime = start
	result.Duration = time.Since(start)
	result.RetryCount = retries
	result.RetriedOn = retriedOn
	if stop == RepeatStopError && failure == "" {
		return result // a request failed or the run was interrupted; Run reports it
	}

	if len(rc.Collect) > 0 && result.Outputs != nil {
		// A copy, so the last iteration keeps its own response's outputs.
		merged := maps.Clone(result.Outputs)
		maps.Copy(merged, collected)
		result.Outputs = merged
		result.DisplayOutputs = displayOutputs(node, merged)
	}
	e.runStepAssertions(step, node, &result, prepared.inputs, prepared.now)
	if failure != "" {
		if result.Validation == nil {
			result.Validation = &validate.MechanicalResult{}
		}
		repeatResult := validate.AssertionResult{Type: validate.AssertRepeat, Expr: rc.Until, Message: failure}
		result.Validation.Results = append([]validate.AssertionResult{repeatResult}, result.Validation.Results...)
		result.Validation.Passed = false
	}
	return result
}

// repeatable reports whether a request of a repeated step came back in a state
// its condition can be evaluated on: no error, a status below 400, and no
// error in the response body.
func repeatable(r StepResult) bool {
	return r.Error == nil && r.Response != nil && r.StatusCode < 400 && r.ResponseBodyError == nil
}

// predicateFields returns a response's outputs as a predicate reads them:
// through JSON, as a step's assertions read them, so that an extracted
// json.Number compares as a number.
func predicateFields(outputs map[string]any) (map[string]any, error) {
	data, err := json.Marshal(outputs)
	if err != nil {
		return nil, fmt.Errorf("reading the outputs: %w", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, fmt.Errorf("reading the outputs: %w", err)
	}
	return fields, nil
}

// untilError describes why a repeat condition couldn't be evaluated. A field
// the response didn't produce, such as an output its extract rule found
// missing, gets the remedy.
func untilError(until string, err error) string {
	var unknown *predicate.UnknownFieldError
	if errors.As(err, &unknown) {
		return fmt.Sprintf("repeat.until %q could not be evaluated: %v; a response that leaves an output out needs a default: on its extract rule", until, err)
	}
	return fmt.Sprintf("repeat.until %q could not be evaluated: %v", until, err)
}

// collectOutputs gathers the named outputs of one response into collected: a
// list's items are appended, and a number is added. An output the response
// left out is skipped, and a value of any other kind replaces what was there.
func collectOutputs(collected, outputs map[string]any, names []string) {
	for _, name := range names {
		v, ok := outputs[name]
		if !ok || v == nil {
			continue
		}
		list, isList := v.([]any)
		prev, seen := collected[name]
		switch {
		case !seen && isList:
			collected[name] = append([]any(nil), list...)
		case !seen:
			collected[name] = v
		case isList:
			if prevList, ok := prev.([]any); ok {
				collected[name] = append(prevList, list...)
			} else {
				collected[name] = append([]any(nil), list...)
			}
		default:
			if sum, ok := addNumbers(prev, v); ok {
				collected[name] = sum
			} else {
				collected[name] = v
			}
		}
	}
}

// addNumbers adds two numeric output values, as an int when both are whole
// numbers. ok is false when either isn't a number.
func addNumbers(a, b any) (any, bool) {
	ai, af, aWhole, aOK := numberValue(a)
	bi, bf, bWhole, bOK := numberValue(b)
	switch {
	case !aOK || !bOK:
		return nil, false
	case aWhole && bWhole:
		return int(ai + bi), true
	default:
		return af + bf, true
	}
}

// numberValue reads an output value as a number: an extracted json.Number, or
// an int or float64 from a default, a header conversion, or a transform.
func numberValue(v any) (i int64, f float64, whole, ok bool) {
	switch n := v.(type) {
	case json.Number:
		if x, err := n.Int64(); err == nil {
			return x, float64(x), true, true
		}
		if x, err := n.Float64(); err == nil {
			return 0, x, false, true
		}
	case int:
		return int64(n), float64(n), true, true
	case int64:
		return n, float64(n), true, true
	case float64:
		return 0, n, false, true
	}
	return 0, 0, false, false
}
