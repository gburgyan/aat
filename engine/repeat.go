package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/internal/predicate"
	"github.com/gburgyan/aat/plan"
	"github.com/gburgyan/aat/validate"
)

// executeStepRepeated sends a step's request again and again: until its
// repeat.until predicate holds over a response's outputs, as when polling an
// asynchronous job, or, with repeat.next, through every page of a listing.
// Every request resends the inputs the first resolved, except that a paging
// step sends the previous response's cursors, and each request is retried
// under the step's retry block. The result is the last request's, timed from
// the first, with the inputs the first resolved and the repeat block's
// collected outputs gathered across every response, and the step's assertions
// run once, on that result. Iterations records every request, and RepeatStop
// why the step stopped. A step fails with a repeat result in its Validation
// when it reaches max or its timeout before the condition holds or the pages
// run out, when a response gives cursors an earlier request already sent, or
// when its condition can't be evaluated.
func (e *Engine) executeStepRepeated(ctx context.Context, step plan.Step, node *graph.Node, state *RunState) StepResult {
	rc := step.Repeat
	paging := len(rc.Next) > 0
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
		first      stepInputs     // the inputs the first request resolved
		prepared   stepInputs     // the inputs the next request sends
		sent       map[string]int // for a paging step: the cursors each request sent, and which request
		start      = time.Now()
		collected  = map[string]any{}
		iterations []IterationResult
		retries    int
		retriedOn  []ErrorCategory
		result     StepResult
		stop       string
		failure    string // why the step never finished, for a repeat result in Validation
	)
	if paging {
		sent = map[string]int{}
	}
	for n := 1; stop == ""; n++ {
		result = e.executeStepWithRetry(ctx, polled, node, state, &prepared)
		if n == 1 {
			first = prepared
			if key := cursorKey(sentCursors(first.inputs, rc.Next)); key != "" {
				sent[key] = 1 // a cursor the plan gave, to resume a listing
			}
		}
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
		if paging {
			it.Inputs = result.Inputs
		}
		if !repeatable(result) {
			iterations = append(iterations, it)
			stop = RepeatStopError
			break
		}

		collectOutputs(collected, result.Outputs, rc.Collect)
		met := false
		var evalErr error
		if rc.Until != "" {
			var fields map[string]any
			if fields, evalErr = predicateFields(result.Outputs); evalErr == nil {
				met, evalErr = plan.EvalPredicateWithExprs(rc.Until, fields, e.assertionExprContext(node, state, prepared.inputs, prepared.now))
			}
		}
		it.UntilMet = met
		iterations = append(iterations, it)

		var cursors map[string]any
		exhausted := false
		if paging {
			cursors, exhausted = cursorValues(result.Outputs, rc.Next)
		}
		switch {
		case evalErr != nil:
			stop, failure = RepeatStopError, untilError(rc.Until, evalErr)
			continue
		case met:
			stop = RepeatStopUntil
			continue
		case exhausted:
			stop = RepeatStopExhausted
			continue
		}
		if prev, repeated := sent[cursorKey(cursors)]; paging && repeated {
			stop = RepeatStopLoop
			failure = fmt.Sprintf("repeat.next: response %d gave %s, which request %d already sent, so the pages would repeat",
				n, describeCursors(cursors), prev)
			continue
		}
		if n >= maxRequests {
			stop = RepeatStopMax
			if paging {
				failure = fmt.Sprintf("repeat.next: after %d requests (repeat.max) the response still gave a next page (%s); raise repeat.max or narrow the listing",
					n, describeCursors(cursors))
			} else {
				failure = fmt.Sprintf("repeat.until %q is still false after %d requests (repeat.max)", rc.Until, n)
			}
			continue
		}

		wait := interval
		if after, ok := retryAfter(result.Response.Headers, result.StatusCode, time.Now()); ok {
			wait = max(wait, min(after, maxRetryAfter))
		}
		if elapsed := time.Since(start); timeout > 0 && elapsed+wait > timeout {
			stop = RepeatStopTimeout
			if paging {
				failure = fmt.Sprintf("repeat.next: after %d requests in %s (repeat.timeout %s) the response still gave a next page (%s)",
					n, elapsed.Round(time.Millisecond), timeout, describeCursors(cursors))
			} else {
				failure = fmt.Sprintf("repeat.until %q is still false after %d requests in %s (repeat.timeout %s)",
					rc.Until, n, elapsed.Round(time.Millisecond), timeout)
			}
			continue
		}
		select {
		case <-ctx.Done():
			result.Error = ctx.Err()
			stop = RepeatStopError
			continue
		case <-time.After(wait):
		}
		if paging {
			sent[cursorKey(cursors)] = n + 1
			prepared = stepInputs{resolved: true, inputs: withCursors(first.inputs, cursors),
				selections: first.selections, resolutions: first.resolutions, now: first.now}
		}
	}

	result.Iterations = iterations
	result.RepeatStop = stop
	result.StartTime = start
	result.Duration = time.Since(start)
	result.RetryCount = retries
	result.RetriedOn = retriedOn
	if paging && first.resolved {
		// The step's inputs are the query it ran, not the last page's cursor.
		result.Inputs, result.Selections, result.Resolutions = first.inputs, first.selections, first.resolutions
	}
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
	e.runStepAssertions(step, node, state, &result, first.inputs, first.now)
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

// cursorValues reads the cursors a response gives for a repeat block's next
// map: for each input next sets, the value of its output, or nil when the
// response left that output out or it is null or "". exhausted reports that
// every one is nil, so the response was the listing's last page.
func cursorValues(outputs map[string]any, next map[string]string) (cursors map[string]any, exhausted bool) {
	cursors = make(map[string]any, len(next))
	exhausted = true
	for input, output := range next {
		v := cursorValue(outputs[output])
		cursors[input] = v
		if v != nil {
			exhausted = false
		}
	}
	return cursors, exhausted
}

// sentCursors reads the cursors a request's inputs carry for a repeat block's
// next map, as cursorValues reads them from a response.
func sentCursors(inputs map[string]any, next map[string]string) map[string]any {
	cursors := make(map[string]any, len(next))
	for input := range next {
		cursors[input] = cursorValue(inputs[input])
	}
	return cursors
}

// cursorValue returns v as a cursor, or nil when it is null or "".
func cursorValue(v any) any {
	if s, ok := v.(string); ok && s == "" {
		return nil
	}
	return v
}

// cursorKey identifies a set of cursors, so that a repeated one is caught: the
// set ones as sorted name=value pairs, which read an extracted json.Number and
// an int alike. It is "" when none is set.
func cursorKey(cursors map[string]any) string {
	var parts []string
	for _, name := range slices.Sorted(maps.Keys(cursors)) {
		if v := cursors[name]; v != nil {
			parts = append(parts, fmt.Sprintf("%s=%v", name, v))
		}
	}
	return strings.Join(parts, "&")
}

// describeCursors quotes the set cursors for a message, as in after "c2",
// shortening a long value.
func describeCursors(cursors map[string]any) string {
	var parts []string
	for _, name := range slices.Sorted(maps.Keys(cursors)) {
		v := cursors[name]
		if v == nil {
			continue
		}
		s := fmt.Sprint(v)
		if len(s) > 64 {
			s = s[:61] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s %q", name, s))
	}
	return strings.Join(parts, ", ")
}

// withCursors returns a copy of inputs with each cursor set, or removed when it
// is nil, so that a template's {{?after}} block leaves it out.
func withCursors(inputs, cursors map[string]any) map[string]any {
	out := make(map[string]any, len(inputs)+len(cursors))
	maps.Copy(out, inputs)
	for name, v := range cursors {
		if v == nil {
			delete(out, name)
			continue
		}
		out[name] = v
	}
	return out
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
