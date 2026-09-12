package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gburgyan/aat/adapter"
)

// CleanupEntry records a cleanup node to execute and the step that registered it.
type CleanupEntry struct {
	NodeName string // cleanup node (e.g., "deleteOrder")
	ForNode  string // forward node that registered this cleanup
	// ForStep is the ID of the step that registered this cleanup. Its outputs
	// are consulted first, so two steps on the same node each clean up the
	// resource they created.
	ForStep string
}

// CleanupStack maintains cleanup entries in FILO order.
type CleanupStack struct {
	entries []CleanupEntry
}

// Push adds a cleanup entry to the stack.
func (s *CleanupStack) Push(entry CleanupEntry) {
	s.entries = append(s.entries, entry)
}

// Len returns the number of entries in the stack.
func (s *CleanupStack) Len() int {
	return len(s.entries)
}

// Filter removes entries for which keep returns false, preserving order.
func (s *CleanupStack) Filter(keep func(CleanupEntry) bool) {
	kept := s.entries[:0]
	for _, entry := range s.entries {
		if keep(entry) {
			kept = append(kept, entry)
		}
	}
	s.entries = kept
}

// cleanupRun tracks the cleanup of one run: the step IDs already in use, so
// every cleanup step gets its own, and which nodes a cleanup chain may reach.
type cleanupRun struct {
	ids   map[string]bool
	allow func(node string) bool // nil allows every node
}

// newCleanupRun starts a cleanup run whose step IDs avoid taken. allow decides
// whether a node reached through a cleanup chain runs; nil allows every node.
func newCleanupRun(taken []string, allow func(node string) bool) *cleanupRun {
	ids := make(map[string]bool, len(taken))
	for _, id := range taken {
		ids[id] = true
	}
	return &cleanupRun{ids: ids, allow: allow}
}

// nextID returns node as a step ID, or node_2, node_3, and so on when that ID
// is taken, and marks the ID taken.
func (r *cleanupRun) nextID(node string) string {
	id := node
	for n := 2; r.ids[id]; n++ {
		id = fmt.Sprintf("%s_%d", node, n)
	}
	r.ids[id] = true
	return id
}

// runCleanupStack runs the stack's entries in FILO order (last pushed, first
// executed), each followed by its cleanup chain. Errors are recorded in the
// StepResult but do not stop subsequent cleanup steps. The requests use ctx as
// given: runCleanup passes a context detached from the run's cancellation, with
// a deadline when the run was aborted.
func (e *Engine) runCleanupStack(ctx context.Context, s *CleanupStack, state *RunState, run *cleanupRun) []StepResult {
	if s.Len() == 0 {
		return nil
	}

	results := make([]StepResult, 0, s.Len())
	for i := len(s.entries) - 1; i >= 0; i-- {
		entry := s.entries[i]
		results = append(results, e.runCleanupChain(ctx, entry, entry.ForStep, nil, state, run)...)
	}
	return results
}

// runCleanupChain runs a cleanup entry and then, depth-first, its chain: when a
// cleanup step succeeds and its node declares a cleanup of its own, that node
// runs next. cleanupFor is the ID the entry's result links to. ancestors are
// the results of the cleanup steps before this one in its chain, nearest first;
// their outputs feed the chain and are never stored in the run state, so no
// other cleanup step picks them up.
func (e *Engine) runCleanupChain(ctx context.Context, entry CleanupEntry, cleanupFor string, ancestors []StepResult, state *RunState, run *cleanupRun) []StepResult {
	result := e.executeCleanupEntry(ctx, entry, ancestors, state)
	result.StepID = run.nextID(entry.NodeName)
	result.CleanupFor = cleanupFor
	results := []StepResult{result}

	node := e.graph.Nodes[entry.NodeName]
	if node == nil || node.Cleanup == "" || !cleanupSucceeded(result) {
		return results
	}
	next := node.Cleanup
	if run.allow != nil && !run.allow(next) {
		return results
	}

	chain := append([]StepResult{result}, ancestors...)
	if i := slices.IndexFunc(chain, func(r StepResult) bool { return r.Node == next }); i >= 0 {
		// Graph validation rejects cleanup cycles; this guards a graph built
		// without it.
		names := make([]string, 0, i+2)
		for j := i; j >= 0; j-- {
			names = append(names, chain[j].Node)
		}
		names = append(names, next)
		return append(results, StepResult{
			StepID:     run.nextID(next),
			Node:       next,
			CleanupFor: result.StepID,
			StartTime:  time.Now(),
			Error:      fmt.Errorf("cleanup cycle: %s", strings.Join(names, " → ")),
		})
	}

	child := CleanupEntry{NodeName: next, ForNode: entry.NodeName, ForStep: entry.ForStep}
	return append(results, e.runCleanupChain(ctx, child, result.StepID, chain, state, run)...)
}

// cleanupSucceeded reports whether a cleanup step did its job: it got a
// response below 400 that no error detection rule flagged.
func cleanupSucceeded(r StepResult) bool {
	return r.Error == nil && r.StatusCode < 400 && r.ResponseBodyError == nil
}

func (e *Engine) executeCleanupEntry(ctx context.Context, entry CleanupEntry, ancestors []StepResult, state *RunState) StepResult {
	start := time.Now()
	// base identifies the cleanup step and when it started, so archives place
	// it on the run's timeline like any other step.
	base := StepResult{StepID: entry.NodeName, Node: entry.NodeName, StartTime: start}
	failed := func(inputs map[string]any, err error) StepResult {
		sr := base
		sr.Inputs = inputs
		sr.Error = err
		sr.Duration = time.Since(start)
		return sr
	}

	node, ok := e.graph.Nodes[entry.NodeName]
	if !ok {
		return failed(nil, fmt.Errorf("cleanup node %q not found in graph", entry.NodeName))
	}

	// Resolve inputs by output name: first from the cleanup steps before this
	// one in its chain, nearest first; then from the step that registered the
	// cleanup; then from the most recently executed step with an output of that
	// name. Outputs are stored by step ID, so ForNode stands in only for an
	// entry without a ForStep.
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

	adp, err := e.registry.Get(node.Adapter)
	if err != nil {
		return failed(inputs, fmt.Errorf("cleanup adapter: %w", err))
	}

	// Resolve executor/config/rewrite for the cleanup node
	exec, cfg, rewrite := e.router.Resolve(entry.NodeName)
	base.ActualBaseURL = exec.BaseURL

	req, err := adp.BuildRequest(inputs, cfg)
	if err != nil {
		return failed(inputs, fmt.Errorf("cleanup build request: %w", err))
	}

	if rewrite != nil {
		base.OriginalPath = req.Path
		req.Path = adapter.RewritePath(req.Path, rewrite)
	}
	base.Request = req

	resp, err := e.send(ctx, exec, req)
	if err != nil {
		return failed(inputs, fmt.Errorf("cleanup execute: %w", err))
	}

	result := base
	result.Inputs = inputs
	result.Response = resp
	result.StatusCode = resp.StatusCode
	result.Duration = time.Since(start)

	// Extract outputs (best-effort for cleanup)
	outputs, err := adp.ExtractOutputs(resp)
	if err == nil {
		result.Outputs = outputs
	}
	// Cleanup extraction errors are silently ignored — empty outputs is fine

	// An error reported in a successful response fails the cleanup step, so
	// its chain goes no further.
	if resp.StatusCode < 400 {
		result.ResponseBodyError = CheckErrorDetection(effectiveErrorRules(node, e.graph), resp.Body)
	}

	return result
}
