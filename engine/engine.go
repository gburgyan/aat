package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/gburgyan/aat/validate"
)

// Engine orchestrates plan execution against a graph using adapters.
type Engine struct {
	graph    *graph.Graph
	registry *adapter.Registry
	executor *adapter.HTTPExecutor
	config   *adapter.EnvironmentConfig

	// ContinueOnAssertionFailure controls whether execution continues after
	// a step's mechanical assertions fail. When false (default), assertion
	// failure stops execution and triggers cleanup. When true, the outcome
	// is set to Failed but subsequent steps still execute.
	ContinueOnAssertionFailure bool
}

// NewEngine creates an Engine with the given dependencies.
func NewEngine(g *graph.Graph, registry *adapter.Registry, executor *adapter.HTTPExecutor, config *adapter.EnvironmentConfig) *Engine {
	return &Engine{
		graph:    g,
		registry: registry,
		executor: executor,
		config:   config,
	}
}

// Run executes a plan: validates, sorts steps topologically, runs each in order,
// and executes cleanup on completion.
func (e *Engine) Run(ctx context.Context, p *plan.Plan) *RunResult {
	// 1. Validate plan against graph
	if err := plan.Validate(p, e.graph); err != nil {
		return &RunResult{Outcome: OutcomeError, Error: err}
	}

	// 2. Topological sort
	sorted, err := TopologicalSort(p.Execution.Steps, e.graph)
	if err != nil {
		return &RunResult{Outcome: OutcomeError, Error: err}
	}

	// 3. Execute steps
	state := NewRunState()
	cleanupStack := &CleanupStack{}
	var stepResults []StepResult
	outcome := OutcomePassed

	defer func() {
		// Cleanup is handled by the caller via the returned RunResult
	}()

	for _, step := range sorted {
		node, ok := e.graph.Nodes[step.Node]
		if !ok {
			return &RunResult{
				Outcome: OutcomeError,
				Steps:   stepResults,
				Error:   fmt.Errorf("node %q not found in graph", step.Node),
			}
		}

		result := e.executeStepWithRetry(ctx, step, node, state)
		stepResults = append(stepResults, result)

		if result.Error != nil {
			outcome = OutcomeError
			// Run cleanup before returning
			cleanupResults := cleanupStack.ExecuteAll(ctx, e.graph, e.registry, e.executor, e.config, state)
			return &RunResult{
				Outcome:        outcome,
				Steps:          stepResults,
				CleanupResults: cleanupResults,
				Error:          result.Error,
			}
		}

		if result.StatusCode >= 400 {
			outcome = OutcomeFailed
			// Run cleanup before returning
			cleanupResults := cleanupStack.ExecuteAll(ctx, e.graph, e.registry, e.executor, e.config, state)
			return &RunResult{
				Outcome:        outcome,
				Steps:          stepResults,
				CleanupResults: cleanupResults,
				Error:          fmt.Errorf("step %q returned status %d", step.Node, result.StatusCode),
			}
		}

		// Store outputs
		if result.Outputs != nil {
			state.StoreOutputs(step.Node, result.Outputs)
		}

		// Push cleanup if node has one — done before assertion check because
		// the step executed successfully (HTTP-wise) and may have created resources.
		if node.Cleanup != "" {
			cleanupStack.Push(CleanupEntry{
				NodeName: node.Cleanup,
				ForNode:  node.Name,
			})
		}

		// Run mechanical assertions if configured
		if result.Validation != nil && !result.Validation.Passed {
			outcome = OutcomeFailed
			if !e.ContinueOnAssertionFailure {
				cleanupResults := cleanupStack.ExecuteAll(ctx, e.graph, e.registry, e.executor, e.config, state)
				return &RunResult{
					Outcome:        outcome,
					Steps:          stepResults,
					CleanupResults: cleanupResults,
					Error:          fmt.Errorf("step %q failed mechanical validation", step.Node),
				}
			}
		}
	}

	// All steps succeeded — run cleanup
	cleanupResults := cleanupStack.ExecuteAll(ctx, e.graph, e.registry, e.executor, e.config, state)

	return &RunResult{
		Outcome:        outcome,
		Steps:          stepResults,
		CleanupResults: cleanupResults,
	}
}

func (e *Engine) executeStep(ctx context.Context, step plan.Step, node *graph.Node, state *RunState) StepResult {
	start := time.Now()

	// Resolve inputs
	inputs, selections, err := ResolveInputs(step, node, e.graph, state)
	if err != nil {
		return StepResult{
			Node:     step.Node,
			Error:    fmt.Errorf("resolving inputs: %w", err),
			Duration: time.Since(start),
		}
	}

	// Get adapter
	adp, err := e.registry.Get(node.Adapter)
	if err != nil {
		return StepResult{
			Node:     step.Node,
			Inputs:   inputs,
			Error:    fmt.Errorf("getting adapter: %w", err),
			Duration: time.Since(start),
		}
	}

	// Build request
	req, err := adp.BuildRequest(inputs, e.config)
	if err != nil {
		return StepResult{
			Node:     step.Node,
			Inputs:   inputs,
			Error:    fmt.Errorf("building request: %w", err),
			Duration: time.Since(start),
		}
	}

	// Execute
	resp, err := e.executor.Execute(ctx, req)
	if err != nil {
		return StepResult{
			Node:     step.Node,
			Inputs:   inputs,
			Request:  req,
			Error:    fmt.Errorf("executing request: %w", err),
			Duration: time.Since(start),
		}
	}

	result := StepResult{
		Node:       step.Node,
		Inputs:     inputs,
		Selections: selections,
		Request:    req,
		Response:   resp,
		StatusCode: resp.StatusCode,
		Duration:   time.Since(start),
	}

	// Extract outputs (only on success)
	if resp.StatusCode < 400 {
		outputs, err := adp.ExtractOutputs(resp)
		if err != nil {
			result.Error = fmt.Errorf("extracting outputs: %w", err)
			return result
		}
		result.Outputs = outputs
	}

	// Run mechanical assertions if configured
	if step.Assertions != nil && len(step.Assertions.Mechanical) > 0 {
		result.Validation = validate.RunMechanical(
			resp.StatusCode, resp.Body,
			convertAssertions(step.Assertions.Mechanical),
			plan.EvalPredicate,
		)
	}

	return result
}

// convertAssertions bridges plan.MechanicalAssertion to validate.MechanicalAssertion.
func convertAssertions(planAssertions []plan.MechanicalAssertion) []validate.MechanicalAssertion {
	result := make([]validate.MechanicalAssertion, len(planAssertions))
	for i, pa := range planAssertions {
		result[i] = validate.MechanicalAssertion{
			Type:   validate.AssertionType(pa.Type),
			Expect: pa.Expect,
			Ref:    pa.Ref,
			Path:   pa.Path,
			Value:  pa.Value,
			Expr:   pa.Expr,
		}
	}
	return result
}
