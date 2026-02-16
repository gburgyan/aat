package engine

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/llm"
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

	// Mode controls LLM involvement: strict (never), lean (after pool exhausted),
	// adaptive (lean + step-level soft constraint relaxation on 4xx). Empty defaults to strict.
	Mode config.ExecutionMode

	// KB is the optional domain knowledge base for LLM-assisted value selection.
	KB *domain.KnowledgeBase

	// LLMClient is the optional LLM client for value selection calls.
	LLMClient llm.Client

	// MaxRelaxationDepth limits the number of soft constraints that can be
	// relaxed per step. Zero means use the default (3).
	MaxRelaxationDepth int

	// plan is set during Run() for constraint-aware resolution.
	plan *plan.Plan
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

// WithMode sets the execution mode and returns the engine for chaining.
func (e *Engine) WithMode(mode config.ExecutionMode) *Engine {
	e.Mode = mode
	return e
}

// WithDomain sets the domain knowledge base and returns the engine for chaining.
func (e *Engine) WithDomain(kb *domain.KnowledgeBase) *Engine {
	e.KB = kb
	return e
}

// WithLLM sets the LLM client and returns the engine for chaining.
func (e *Engine) WithLLM(client llm.Client) *Engine {
	e.LLMClient = client
	return e
}

// WithMaxRelaxationDepth sets the per-step relaxation budget and returns the engine for chaining.
func (e *Engine) WithMaxRelaxationDepth(n int) *Engine {
	e.MaxRelaxationDepth = n
	return e
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

	// 3. Validate adapter outputs match graph declarations
	if err := ValidateAdapterOutputs(e.graph, e.registry); err != nil {
		return &RunResult{Outcome: OutcomeError, Error: err}
	}

	// 4. Execute steps
	e.plan = p
	defer func() { e.plan = nil }()

	state := NewRunState()
	cleanupStack := &CleanupStack{}
	var stepResults []StepResult
	outcome := OutcomePassed

	defer func() {
		// Cleanup is handled by the caller via the returned RunResult
	}()

	for _, step := range sorted {
		select {
		case <-ctx.Done():
			cleanupResults := cleanupStack.ExecuteAll(ctx, e.graph, e.registry, e.executor, e.config, state)
			return &RunResult{
				Outcome:        OutcomeError,
				Steps:          stepResults,
				CleanupResults: cleanupResults,
				Error:          fmt.Errorf("execution cancelled: %w", ctx.Err()),
			}
		default:
		}

		node, ok := e.graph.Nodes[step.Node]
		if !ok {
			return &RunResult{
				Outcome: OutcomeError,
				Steps:   stepResults,
				Error:   fmt.Errorf("node %q not found in graph", step.Node),
			}
		}

		var result StepResult
		if e.effectiveMode() == config.ModeAdaptive {
			result = e.executeStepAdaptive(ctx, step, node, state)
		} else {
			tracker := NewRelaxationTracker(e.maxRelaxationDepth())
			result = e.executeStepWithTracking(ctx, step, node, state, tracker)
		}
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

		// Handle expectFailure steps: inverted success/failure logic
		if step.ExpectFailure != nil {
			efr := &ExpectFailureResult{
				ExpectedStatuses: step.ExpectFailure.Status,
				ActualStatus:     result.StatusCode,
				Description:      step.ExpectFailure.Description,
			}
			for _, expected := range step.ExpectFailure.Status {
				if result.StatusCode == expected {
					efr.Passed = true
					break
				}
			}
			result.ExpectFailure = efr
			stepResults[len(stepResults)-1] = result

			if efr.Passed {
				// Expected failure occurred — this is a PASS.
				// Do NOT store outputs (error responses have no useful outputs).
				// Do NOT push cleanup (no resource was created).
				// Mechanical assertions still ran in executeStep; check them.
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
				continue
			}

			// Unexpected success or wrong error code — FAIL.
			outcome = OutcomeFailed
			cleanupResults := cleanupStack.ExecuteAll(ctx, e.graph, e.registry, e.executor, e.config, state)
			return &RunResult{
				Outcome:        outcome,
				Steps:          stepResults,
				CleanupResults: cleanupResults,
				Error:          fmt.Errorf("step %q: expected failure status %v but got %d", step.Node, step.ExpectFailure.Status, result.StatusCode),
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

func (e *Engine) executeStep(ctx context.Context, step plan.Step, node *graph.Node, state *RunState, tracker *RelaxationTracker) StepResult {
	start := time.Now()

	// Construct ResolveContext from engine fields
	rctx := e.buildResolveContext(node, tracker)

	// Resolve inputs
	inputs, selections, resolutions, err := ResolveInputsWithContext(ctx, step, node, e.graph, state, rctx)
	if err != nil {
		return StepResult{
			Node:      step.Node,
			Error:     fmt.Errorf("resolving inputs: %w", err),
			StartTime: start,
			Duration:  time.Since(start),
		}
	}

	// Get adapter
	adp, err := e.registry.Get(node.Adapter)
	if err != nil {
		return StepResult{
			Node:      step.Node,
			Inputs:    inputs,
			Error:     fmt.Errorf("getting adapter: %w", err),
			StartTime: start,
			Duration:  time.Since(start),
		}
	}

	// Build request
	req, err := adp.BuildRequest(inputs, e.config)
	if err != nil {
		return StepResult{
			Node:      step.Node,
			Inputs:    inputs,
			Error:     fmt.Errorf("building request: %w", err),
			StartTime: start,
			Duration:  time.Since(start),
		}
	}

	// Execute
	resp, err := e.executor.Execute(ctx, req)
	if err != nil {
		return StepResult{
			Node:      step.Node,
			Inputs:    inputs,
			Request:   req,
			Error:     fmt.Errorf("executing request: %w", err),
			StartTime: start,
			Duration:  time.Since(start),
		}
	}

	result := StepResult{
		Node:        step.Node,
		Inputs:      inputs,
		Selections:  selections,
		Resolutions: resolutions,
		Request:     req,
		Response:    resp,
		StatusCode:  resp.StatusCode,
		StartTime:   start,
		Duration:    time.Since(start),
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

// buildResolveContext creates a ResolveContext from the engine's configuration.
// Always returns a non-nil context so that expression evaluation and constraint
// checking are active.
func (e *Engine) buildResolveContext(node *graph.Node, tracker *RelaxationTracker) *ResolveContext {
	return &ResolveContext{
		Mode:      e.effectiveMode(),
		Now:       time.Now(),
		EnvLookup: os.Getenv,
		KB:        e.KB,
		LLM:       e.LLMClient,
		Node:      node,
		Plan:      e.plan,
		Tracker:   tracker,
		Registry:  e.registry,
	}
}

// maxRelaxationDepth returns the configured depth or the default of 3.
func (e *Engine) maxRelaxationDepth() int {
	if e.MaxRelaxationDepth > 0 {
		return e.MaxRelaxationDepth
	}
	return 3
}

// executeStepAdaptive wraps executeStepWithRetry with step-level relaxation.
// When a step fails with CategoryClient, it finds the first unrelaxed soft
// constraint, relaxes it, and retries. ExpectFailure steps skip relaxation.
func (e *Engine) executeStepAdaptive(ctx context.Context, step plan.Step, node *graph.Node, state *RunState) StepResult {
	tracker := NewRelaxationTracker(e.maxRelaxationDepth())

	// ExpectFailure steps should not be relaxed
	if step.ExpectFailure != nil {
		return e.executeStepWithTracking(ctx, step, node, state, tracker)
	}

	for {
		select {
		case <-ctx.Done():
			return StepResult{
				Node:        step.Node,
				Error:       fmt.Errorf("adaptive retry cancelled: %w", ctx.Err()),
				Relaxations: tracker.Records(),
			}
		default:
		}

		result := e.executeStepWithRetry(ctx, step, node, state, tracker)

		// Success or non-client error → done
		cls := classifyStepResult(&result)
		if cls == nil || cls.Category != CategoryClient {
			result.Relaxations = tracker.Records()
			return result
		}

		// Try to find and relax a soft constraint for one of this step's inputs
		relaxed := false
		if e.plan != nil && e.plan.Intent.Constraints != nil {
			for _, sc := range e.plan.Intent.Constraints.Soft {
				if tracker.IsRelaxed(sc.Name) {
					continue
				}
				if err := tracker.CanRelax(sc.Name); err != nil {
					continue
				}
				// Check if this constraint applies to this step
				for _, ref := range sc.AppliesTo {
					nodeRef, _, _ := splitAppliesTo(ref)
					if nodeRef == step.Node {
						tracker.Relax(sc.Name, ref, "step_failed")
						relaxed = true
						break
					}
				}
				if relaxed {
					break
				}
			}
		}

		if !relaxed {
			// No more relaxable constraints — return failure
			result.Relaxations = tracker.Records()
			return result
		}
		// Loop back to retry with relaxed constraint
	}
}

// splitAppliesTo splits an AppliesTo reference like "node.input" into
// (node, input, true) or ("node", "", false) if there's no dot.
func splitAppliesTo(ref string) (string, string, bool) {
	for i, c := range ref {
		if c == '.' {
			return ref[:i], ref[i+1:], true
		}
	}
	return ref, "", false
}

// effectiveMode returns the engine's execution mode, defaulting to strict.
func (e *Engine) effectiveMode() config.ExecutionMode {
	if e.Mode == "" {
		return config.ModeStrict
	}
	return e.Mode
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
