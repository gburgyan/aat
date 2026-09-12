package engine

import (
	"context"
	"fmt"
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

// runCleanupStack runs the stack's entries in FILO order (last pushed, first
// executed). Errors are recorded in the StepResult but do not stop subsequent
// cleanup steps. The requests use ctx as given: runCleanup passes a context
// detached from the run's cancellation, with a deadline when the run was aborted.
func (e *Engine) runCleanupStack(ctx context.Context, s *CleanupStack, state *RunState) []StepResult {
	if s.Len() == 0 {
		return nil
	}

	results := make([]StepResult, 0, s.Len())
	for i := len(s.entries) - 1; i >= 0; i-- {
		results = append(results, e.executeCleanupEntry(ctx, s.entries[i], state))
	}
	return results
}

func (e *Engine) executeCleanupEntry(ctx context.Context, entry CleanupEntry, state *RunState) StepResult {
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

	// Resolve inputs by output name: first from the step that registered this
	// cleanup, then from the most recently executed step with an output of that
	// name. Outputs are stored by step ID, so ForNode stands in only for an
	// entry without a ForStep.
	source := entry.ForStep
	if source == "" {
		source = entry.ForNode
	}
	executed := state.ExecutedSteps()
	inputs := make(map[string]any)
	for _, input := range node.Inputs {
		if val, err := state.GetOutput(source, input.Name); err == nil {
			inputs[input.Name] = val
			continue
		}
		for i := len(executed) - 1; i >= 0; i-- {
			if val, err := state.GetOutput(executed[i], input.Name); err == nil {
				inputs[input.Name] = val
				break
			}
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

	return result
}
