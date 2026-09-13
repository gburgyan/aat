package engine

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/internal/predicate"
)

// resolveCleanupInputs resolves a cleanup node's inputs by output name: first
// from the cleanup steps before this one in its chain, nearest first; then from
// the step that registered the cleanup; then from the most recently executed
// step with an output of that name. Outputs are stored by step ID, so ForNode
// stands in only for an entry without a ForStep.
func resolveCleanupInputs(node *graph.Node, entry CleanupEntry, ancestors []StepResult, state *RunState) map[string]any {
	source := entry.ForStep
	if source == "" {
		source = entry.ForNode
	}
	executed := state.ExecutedSteps()
	lookup := func(name string) (any, bool) {
		for _, a := range ancestors {
			if val, ok := a.Outputs[name]; ok {
				return val, true
			}
		}
		if val, err := state.GetOutput(source, name); err == nil {
			return val, true
		}
		for i := len(executed) - 1; i >= 0; i-- {
			if val, err := state.GetOutput(executed[i], name); err == nil {
				return val, true
			}
		}
		return nil, false
	}
	inputs := make(map[string]any)
	for _, input := range node.Inputs {
		if val, ok := lookup(input.Name); ok {
			inputs[input.Name] = val
		}
	}
	return inputs
}

// cleanupSkip decides whether a cleanup entry for a graph pairing is no longer
// needed, in this order:
//
//  1. Released: a later main step already released the resource (see
//     releasingStep). Only an entry a main step registered can be released: a
//     chained entry cleans up what the cleanup step before it created, after
//     every main step ran.
//  2. The pairing's when condition is false. It reads the outputs of the step
//     that registered the entry, or of the cleanup step before it in a chain,
//     and nothing else, so a later step's outputs never decide it.
//
// inputs are the values the cleanup resolved (see resolveCleanupInputs). A
// condition that can't be evaluated leaves the cleanup to run, and its error is
// returned for the cleanup step's record. A plan-level cleanup step is not a
// pairing and is never skipped here; its runOn alone decides it.
func (e *Engine) cleanupSkip(entry CleanupEntry, cleanupFor string, inputs map[string]any, ancestors []StepResult, state *RunState, run *cleanupRun) (*CleanupSkip, string) {
	declaring, node := e.graph.Nodes[entry.ForNode], e.graph.Nodes[entry.NodeName]
	if declaring == nil || node == nil || declaring.Cleanup.Node != entry.NodeName {
		return nil, ""
	}
	pairing := declaring.Cleanup

	if len(ancestors) == 0 {
		if by, ok := e.releasingStep(entry, pairing, inputs, run.mainSteps); ok {
			return &CleanupSkip{Node: entry.NodeName, CleanupFor: cleanupFor, Reason: CleanupSkipReleased, ReleasedBy: by}, ""
		}
	}

	if pairing.When == "" {
		return nil, ""
	}
	holds, err := predicate.Eval(pairing.When, conditionOutputs(entry, ancestors, state))
	if err != nil {
		return nil, fmt.Sprintf("when %s: %v", pairing.When, err)
	}
	if !holds {
		return &CleanupSkip{Node: entry.NodeName, CleanupFor: cleanupFor, Reason: CleanupSkipWhen, When: pairing.When}, ""
	}
	return nil, ""
}

// conditionOutputs returns the outputs a pairing's when condition reads: those
// of the cleanup step before this one in its chain, or else those of the step
// that registered the entry.
func conditionOutputs(entry CleanupEntry, ancestors []StepResult, state *RunState) map[string]any {
	if len(ancestors) > 0 {
		return ancestors[0].Outputs
	}
	outputs, _ := state.GetAllOutputs(entry.ForStep)
	return outputs
}

// releasingStep returns the ID of the main step that already released an
// entry's resource: a step after the one that registered the entry, on the
// cleanup node or on one of the pairing's releasedBy nodes, that did what it was
// sent to do (see stepReleased) with the value the cleanup resolved for every
// input the two share, and at least one such input.
func (e *Engine) releasingStep(entry CleanupEntry, pairing graph.CleanupPairing, inputs map[string]any, mainSteps []StepResult) (string, bool) {
	registered := slices.IndexFunc(mainSteps, func(r StepResult) bool { return r.StepID == entry.ForStep })
	if registered < 0 {
		return "", false
	}
	for _, r := range mainSteps[registered+1:] {
		if r.Node != entry.NodeName && !slices.Contains(pairing.ReleasedBy, r.Node) {
			continue
		}
		node := e.graph.Nodes[r.Node]
		if node != nil && stepReleased(r) && sameSharedInputs(node.Inputs, inputs, r.Inputs) {
			return r.StepID, true
		}
	}
	return "", false
}

// stepReleased reports whether a main step did what it was sent to do: no
// error, a status below 400, no error detection rule flagged its body, and it
// was not a step expected to fail.
func stepReleased(r StepResult) bool {
	return r.Error == nil && r.StatusCode < 400 && r.ResponseBodyError == nil && r.ExpectFailure == nil
}

// sameSharedInputs reports whether a releasing step sent the value the cleanup
// resolved for every input the releasing node declares and the cleanup
// resolved, and whether there is at least one such input. A releasing step that
// left one of them unset does not match.
func sameSharedInputs(declared []graph.Input, cleanup, sent map[string]any) bool {
	compared := 0
	for _, in := range declared {
		want, ok := cleanup[in.Name]
		if !ok {
			continue
		}
		got, ok := sent[in.Name]
		if !ok || !sameInputValue(want, got, in.Type) {
			return false
		}
		compared++
	}
	return compared > 0
}

// sameInputValue compares two values for an input of type typ: numbers by
// value, so 42 from a plan matches 42.0 from a JSON response, and text types
// by their text. Other types compare with their numbers normalized.
func sameInputValue(a, b any, typ string) bool {
	if ft, err := graph.ParseFieldType(typ); err == nil && ft.Kind == graph.TypeScalar {
		switch ft.Name {
		case "integer", "float", "money":
			x, errA := toFloat64(a)
			y, errB := toFloat64(b)
			return errA == nil && errB == nil && x == y
		case "string", "date", "datetime":
			return fmt.Sprint(a) == fmt.Sprint(b)
		}
	}
	return reflect.DeepEqual(normalizeNumbers(a), normalizeNumbers(b))
}

// normalizeNumbers turns every Go number in v, nested ones included, into a
// float64. Strings stay strings.
func normalizeNumbers(v any) any {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = normalizeNumbers(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = normalizeNumbers(e)
		}
		return out
	}
	if f, err := toFloat64(v); err == nil {
		return f
	}
	return v
}
