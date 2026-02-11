package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
)

// CleanupEntry records a cleanup node to execute and which forward node triggered it.
type CleanupEntry struct {
	NodeName string // cleanup node (e.g., "ignoreWorkbench")
	ForNode  string // forward node that registered this cleanup
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

// ExecuteAll runs all cleanup entries in FILO order (last pushed, first executed).
// Errors are recorded in the StepResult but do not stop subsequent cleanup steps.
func (s *CleanupStack) ExecuteAll(
	ctx context.Context,
	g *graph.Graph,
	registry *adapter.Registry,
	executor *adapter.HTTPExecutor,
	config *adapter.EnvironmentConfig,
	state *RunState,
) []StepResult {
	if len(s.entries) == 0 {
		return nil
	}

	results := make([]StepResult, 0, len(s.entries))

	// Use context.WithoutCancel so cleanup HTTP calls still execute
	// even when the parent context is cancelled. Cleanup is for resource
	// deletion — it must run.
	cleanupCtx := context.WithoutCancel(ctx)

	// Execute in reverse order (FILO)
	for i := len(s.entries) - 1; i >= 0; i-- {
		entry := s.entries[i]
		result := executeCleanupEntry(cleanupCtx, entry, g, registry, executor, config, state)
		results = append(results, result)
	}

	return results
}

func executeCleanupEntry(
	ctx context.Context,
	entry CleanupEntry,
	g *graph.Graph,
	registry *adapter.Registry,
	executor *adapter.HTTPExecutor,
	config *adapter.EnvironmentConfig,
	state *RunState,
) StepResult {
	start := time.Now()

	node, ok := g.Nodes[entry.NodeName]
	if !ok {
		return StepResult{
			Node:     entry.NodeName,
			Error:    fmt.Errorf("cleanup node %q not found in graph", entry.NodeName),
			Duration: time.Since(start),
		}
	}

	// Resolve inputs from current state
	// Build edge lookup for this cleanup node
	edgeMap := make(map[string]graph.Edge)
	for _, edge := range g.Edges {
		toNode, toField, err := splitRef(edge.To)
		if err != nil {
			continue
		}
		if toNode == entry.NodeName {
			edgeMap[toField] = edge
		}
	}

	inputs := make(map[string]any)
	for _, input := range node.Inputs {
		if edge, ok := edgeMap[input.Name]; ok {
			fromNode, fromField, err := splitRef(edge.From)
			if err != nil {
				continue
			}
			val, err := state.GetOutput(fromNode, fromField)
			if err != nil {
				continue // best-effort for cleanup
			}
			inputs[input.Name] = val
		}
	}

	adp, err := registry.Get(node.Adapter)
	if err != nil {
		return StepResult{
			Node:     entry.NodeName,
			Inputs:   inputs,
			Error:    fmt.Errorf("cleanup adapter: %w", err),
			Duration: time.Since(start),
		}
	}

	req, err := adp.BuildRequest(inputs, config)
	if err != nil {
		return StepResult{
			Node:     entry.NodeName,
			Inputs:   inputs,
			Error:    fmt.Errorf("cleanup build request: %w", err),
			Duration: time.Since(start),
		}
	}

	resp, err := executor.Execute(ctx, req)
	if err != nil {
		return StepResult{
			Node:     entry.NodeName,
			Inputs:   inputs,
			Request:  req,
			Error:    fmt.Errorf("cleanup execute: %w", err),
			Duration: time.Since(start),
		}
	}

	result := StepResult{
		Node:       entry.NodeName,
		Inputs:     inputs,
		Request:    req,
		Response:   resp,
		StatusCode: resp.StatusCode,
		Duration:   time.Since(start),
	}

	// Extract outputs (best-effort for cleanup)
	outputs, err := adp.ExtractOutputs(resp)
	if err == nil {
		result.Outputs = outputs
	}
	// Cleanup extraction errors are silently ignored — empty outputs is fine

	return result
}
