package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/graph/oas"
	"github.com/gburgyan/aat/internal/httpstatus"
	"github.com/gburgyan/aat/internal/predicate"
	"github.com/gburgyan/aat/plan"
	"github.com/gburgyan/aat/validate"
)

// Engine orchestrates plan execution against a graph using adapters.
type Engine struct {
	graph    *graph.Graph
	registry *adapter.Registry
	router   *ExecutorRouter

	// ContinueOnAssertionFailure controls whether execution continues after
	// a step's mechanical assertions fail. When false (default), assertion
	// failure stops execution and triggers cleanup. When true, the outcome
	// is set to Failed but subsequent steps still execute.
	ContinueOnAssertionFailure bool

	// KB is the optional domain knowledge base (used for prompt context, not execution).
	KB *domain.KnowledgeBase

	// Observer receives real-time progress events during execution.
	// Nil means no notifications (zero overhead).
	Observer ProgressObserver

	// layeredDefaults holds data-layer overrides for graph input defaults.
	// Keyed by "nodeName.inputName". When set, these take priority over
	// graph-level defaults during plan instantiation.
	layeredDefaults map[string]*graph.InputDefault

	// envValues holds project-level values from env.yaml, available via {{env.KEY}}.
	// OS environment variables take priority over these values.
	envValues map[string]string

	// OAS runtime validation
	oasCache  *oas.SpecCache // nil = disabled
	graphOAS  string         // graph-level default OAS path
	oasStrict bool           // when true, OAS errors fail the step

	// pacer spaces the starts of requests; nil means no pacing.
	pacer *Pacer

	// plan is set during Run() for constraint-aware resolution.
	plan *plan.Plan

	// stopAfterStep, when non-empty, halts execution after the step whose
	// StepID() matches completes successfully. Cleanup is intentionally skipped
	// so created resources stay alive for an external harness to consume.
	stopAfterStep string
}

// NewEngine creates an Engine with the given dependencies.
// The router handles per-node executor/config resolution.
func NewEngine(g *graph.Graph, registry *adapter.Registry, router *ExecutorRouter) *Engine {
	return &Engine{
		graph:    g,
		registry: registry,
		router:   router,
	}
}

// WithDomain sets the domain knowledge base and returns the engine for chaining.
func (e *Engine) WithDomain(kb *domain.KnowledgeBase) *Engine {
	e.KB = kb
	return e
}

// WithProgress sets the progress observer and returns the engine for chaining.
func (e *Engine) WithProgress(obs ProgressObserver) *Engine {
	e.Observer = obs
	return e
}

// WithLayers sets data-layer overrides for graph input defaults.
func (e *Engine) WithLayers(ld map[string]*graph.InputDefault) *Engine {
	e.layeredDefaults = ld
	return e
}

// WithEnvValues sets project-level env values (from env.yaml) that are available
// via {{env.KEY}} expressions. OS environment variables take priority.
func (e *Engine) WithEnvValues(values map[string]string) *Engine {
	e.envValues = values
	return e
}

// WithStopAfter halts execution after the step whose StepID() equals stepID
// completes successfully. Cleanup is skipped so resources created up to that
// point stay alive for an external harness to consume. An empty stepID disables
// the checkpoint (the plan runs to completion as normal).
func (e *Engine) WithStopAfter(stepID string) *Engine {
	e.stopAfterStep = stepID
	return e
}

// WithOASSpecs enables runtime OAS validation for steps that have OAS refs.
// When strict is true, OAS validation errors cause step failure.
func (e *Engine) WithOASSpecs(cache *oas.SpecCache, graphOAS string, strict bool) *Engine {
	e.oasCache = cache
	e.graphOAS = graphOAS
	e.oasStrict = strict
	return e
}

// Run executes a plan: validates, sorts steps topologically, runs each in order,
// and executes cleanup on completion.
func (e *Engine) Run(ctx context.Context, p *plan.Plan) (result *RunResult) {
	// Time the run and fire OnRunComplete on every exit path.
	start := time.Now()
	defer func() {
		if result == nil {
			return
		}
		result.StartTime = start
		result.Duration = time.Since(start)
		if e.Observer != nil {
			e.Observer.OnRunComplete(result)
		}
	}()

	// 1. Instantiate + validate: merge graph defaults (with layers), inject deps, validate
	instantiatedPlan, err := plan.InstantiateAndValidateWithLayers(p, e.graph, e.layeredDefaults)
	if err != nil {
		return &RunResult{Outcome: OutcomeError, Error: err}
	}

	// 2. Topological sort
	sorted, err := TopologicalSort(instantiatedPlan.Execution.Steps)
	if err != nil {
		return &RunResult{Outcome: OutcomeError, Error: err, InstantiatedPlan: instantiatedPlan}
	}

	// 2b. Validate the stop-after step exists, if set, so a typo fails fast
	// instead of silently running the whole plan.
	if e.stopAfterStep != "" {
		if err := stopAfterError(e.stopAfterStep, sorted); err != nil {
			return &RunResult{Outcome: OutcomeError, Error: err, InstantiatedPlan: instantiatedPlan}
		}
	}

	// 3. Validate adapter outputs match graph declarations (only for plan-used nodes)
	if err := ValidateAdapterOutputsForPlan(e.graph, e.registry, instantiatedPlan); err != nil {
		return &RunResult{Outcome: OutcomeError, Error: err, InstantiatedPlan: instantiatedPlan}
	}

	// 3b. Validate template required placeholders vs optional graph inputs
	if err := ValidateTemplateInputsForPlan(e.graph, e.registry, instantiatedPlan); err != nil {
		return &RunResult{Outcome: OutcomeError, Error: err, InstantiatedPlan: instantiatedPlan}
	}

	// 4. Set plan for constraint-aware resolution
	e.plan = instantiatedPlan
	defer func() {
		e.plan = nil
	}()

	state := NewRunState()
	cleanupStack := &CleanupStack{}
	var stepResults []StepResult
	outcome := OutcomePassed
	verificationSteps := plan.VerificationSteps(instantiatedPlan, e.graph, e.layeredDefaults)
	total := len(sorted) + len(verificationSteps)

	if e.Observer != nil {
		e.Observer.OnRunStart(total)
	}

	for i, step := range sorted {
		if ctx.Err() != nil {
			return e.abortedResult(ctx, instantiatedPlan, cleanupStack, state, stepResults)
		}

		node, ok := e.graph.Nodes[step.Node]
		if !ok {
			return &RunResult{
				Outcome:          OutcomeError,
				Steps:            stepResults,
				Error:            fmt.Errorf("node %q not found in graph", step.Node),
				InstantiatedPlan: instantiatedPlan,
			}
		}

		// Apply overlay expectFailure when the plan step doesn't declare one.
		if step.ExpectFailure == nil {
			if _, ef := e.router.ResolveValueOverride(step.Node); ef != nil {
				step.ExpectFailure = ef
			}
		}

		if e.Observer != nil {
			e.Observer.OnStepStart(i, total, step)
		}

		stepResult := e.executeStepWithTracking(ctx, step, node, state)
		stepResults = append(stepResults, stepResult)

		if e.Observer != nil {
			e.Observer.OnStepComplete(i, total, stepResult)
		}

		if stepResult.Error != nil {
			// A step that failed because the run was interrupted, mid-request or
			// while waiting to retry, is an abort, not an infrastructure error.
			if ctx.Err() != nil {
				return e.abortedResult(ctx, instantiatedPlan, cleanupStack, state, stepResults)
			}
			outcome = OutcomeError
			return e.endRun(ctx, instantiatedPlan, cleanupStack, state, outcome, stepResults, fmt.Errorf("step %s: %w", stepRef(step), stepResult.Error))
		}

		// Handle expectFailure steps: inverted success/failure logic
		if step.ExpectFailure != nil {
			efr := &ExpectFailureResult{
				ExpectedStatuses: step.ExpectFailure.Status,
				ActualStatus:     stepResult.StatusCode,
				Description:      step.ExpectFailure.Description,
			}
			for _, expected := range step.ExpectFailure.Status {
				if stepResult.StatusCode == expected {
					efr.Passed = true
					break
				}
			}
			stepResult.ExpectFailure = efr
			stepResults[len(stepResults)-1] = stepResult

			if efr.Passed {
				// Expected failure occurred — this is a PASS.
				// Do NOT store outputs (error responses have no useful outputs).
				// Do NOT push cleanup (no resource was created).
				// Mechanical assertions still ran in executeStepWith; check them.
				if stepResult.Validation != nil && !stepResult.Validation.Passed {
					outcome = OutcomeFailed
					if !e.ContinueOnAssertionFailure {
						return e.endRun(ctx, instantiatedPlan, cleanupStack, state, outcome, stepResults, fmt.Errorf("step %s failed mechanical validation", stepRef(step)))
					}
				}
				if stopped := e.checkpointResult(step, stepResults, instantiatedPlan); stopped != nil {
					return stopped
				}
				continue
			}

			// Unexpected success or wrong error code — FAIL.
			outcome = OutcomeFailed
			return e.endRun(ctx, instantiatedPlan, cleanupStack, state, outcome, stepResults, fmt.Errorf("step %s: expected failure status %v but got %d", stepRef(step), step.ExpectFailure.Status, stepResult.StatusCode))
		}

		if stepResult.StatusCode >= 400 {
			outcome = OutcomeFailed
			return e.endRun(ctx, instantiatedPlan, cleanupStack, state, outcome, stepResults, fmt.Errorf("step %s returned status %d", stepRef(step), stepResult.StatusCode))
		}

		// Check for response body errors (API returned 2xx but body indicates error)
		if stepResult.ResponseBodyError != nil {
			outcome = OutcomeFailed
			// Do NOT store outputs — error responses produce unreliable data
			// Do NOT push cleanup — failing node did not create a valid resource
			return e.endRun(ctx, instantiatedPlan, cleanupStack, state, outcome, stepResults, fmt.Errorf("step %s: %s", stepRef(step), stepResult.ResponseBodyError.Summary()))
		}

		// Store outputs keyed by step ID (supports step aliasing)
		if stepResult.Outputs != nil {
			state.StoreOutputs(step.StepID(), stepResult.Outputs)
		}

		// Push cleanup if node has one — done before assertion check because
		// the step executed successfully (HTTP-wise) and may have created resources.
		if node.Cleanup.Node != "" {
			cleanupStack.Push(CleanupEntry{
				NodeName: node.Cleanup.Node,
				ForNode:  node.Name,
				ForStep:  step.StepID(),
			})
		}

		// Strict OAS mode: a request or response that violates the spec fails
		// the step. Checked after the cleanup push because the API may have
		// accepted the request and created a resource.
		if err := e.oasStrictError(step, &stepResult); err != nil {
			outcome = OutcomeFailed
			return e.endRun(ctx, instantiatedPlan, cleanupStack, state, outcome, stepResults, err)
		}

		// Run mechanical assertions if configured
		if stepResult.Validation != nil && !stepResult.Validation.Passed {
			outcome = OutcomeFailed
			if !e.ContinueOnAssertionFailure {
				return e.endRun(ctx, instantiatedPlan, cleanupStack, state, outcome, stepResults, fmt.Errorf("step %s failed mechanical validation", stepRef(step)))
			}
		}

		if stopped := e.checkpointResult(step, stepResults, instantiatedPlan); stopped != nil {
			return stopped
		}
	}

	// Main flow complete — run verification steps (read-only checks with their
	// own assertions), then cleanup.
	verResults, verOutcome, verErr := e.runVerification(ctx, verificationSteps, state, len(sorted), total)
	stepResults = append(stepResults, verResults...)
	if verOutcome == OutcomeError && ctx.Err() != nil {
		return e.abortedResult(ctx, instantiatedPlan, cleanupStack, state, stepResults)
	}
	if verOutcome != OutcomePassed {
		outcome = verOutcome
	}

	return e.endRun(ctx, instantiatedPlan, cleanupStack, state, outcome, stepResults, verErr)
}

// runCleanup executes cleanup after the main flow. A graph-level cleanup
// pairing runs from the FILO stack, once for each step that registered it, so
// the most recently created resource is released first and nothing is sent for
// a resource that was never created. A cleanup step that succeeds is followed,
// depth-first, by its own node's cleanup pairing, if it has one. A plan-level
// cleanup step (execution.cleanup) whose node is a pairing for a step in the
// plan, or in the chain of one, does not run on its own: its runOn decides
// whether that node runs from the stack or the chain. Other plan-level cleanup
// steps run first, in declaration order, honoring runOn
// (always/success/failure). Cleanup inputs are matched by output name, starting
// with the cleanup steps before it in its chain and then the step that
// registered the entry. Cleanup failures are recorded but never change the run
// outcome. A registered pairing that is no longer needed is skipped and returned
// in the second result: a later main step already released its resource, or its
// when condition is false (see cleanupSkip). steps are the run's step results so
// far; its verification steps never release a cleanup.
func (e *Engine) runCleanup(ctx context.Context, p *plan.Plan, cleanupStack *CleanupStack, state *RunState, outcome Outcome, steps []StepResult) ([]StepResult, []CleanupSkip) {
	paired := e.pairedCleanupNodes(p)
	var planEntries []CleanupEntry
	declared := make(map[string]bool) // paired nodes the plan lists as cleanup steps
	selected := make(map[string]bool) // declared nodes with a runOn that matches the outcome
	for _, cs := range p.Execution.Cleanup {
		matches := cleanupRunOnMatches(cs.RunOn, outcome)
		if paired[cs.Node] {
			declared[cs.Node] = true
			selected[cs.Node] = selected[cs.Node] || matches
			continue
		}
		if matches {
			planEntries = append(planEntries, CleanupEntry{NodeName: cs.Node})
		}
	}
	allow := func(node string) bool {
		return !declared[node] || selected[node]
	}
	cleanupStack.Filter(func(entry CleanupEntry) bool {
		return allow(entry.NodeName)
	})

	// total counts the entries that start a cleanup chain; chains add steps.
	total := len(planEntries) + cleanupStack.Len()
	if total == 0 {
		return nil, nil
	}
	if e.Observer != nil {
		e.Observer.OnCleanupStart(total)
	}

	// Cleanup must run even when the run's context is cancelled. After an
	// interrupt it gets abortedCleanupBudget, so a hung API cannot keep the
	// process alive.
	cleanupCtx := context.WithoutCancel(ctx)
	if outcome == OutcomeAborted {
		var cancel context.CancelFunc
		cleanupCtx, cancel = context.WithTimeout(cleanupCtx, abortedCleanupBudget)
		defer cancel()
	}

	// steps holds a result for each main step that ran, in plan order, and then
	// the verification results.
	mainSteps := steps[:min(len(steps), len(p.Execution.Steps))]
	run := newCleanupRun(e.planStepIDs(p), allow, mainSteps)
	results := make([]StepResult, 0, total)
	for _, entry := range planEntries {
		results = append(results, e.runCleanupChain(cleanupCtx, entry, "", nil, state, run)...)
	}
	results = append(results, e.runCleanupStack(cleanupCtx, cleanupStack, state, run)...)

	if e.Observer != nil {
		for i, cr := range results {
			e.Observer.OnCleanupStepComplete(i, len(results), cr)
		}
		if so, ok := e.Observer.(CleanupSkipObserver); ok {
			for _, skip := range run.skips {
				so.OnCleanupSkipped(skip)
			}
		}
	}
	return results, run.skips
}

// endRun runs cleanup for a run that ended with outcome and returns its result:
// the steps that ran, what cleanup ran and skipped, and err.
func (e *Engine) endRun(ctx context.Context, p *plan.Plan, cleanupStack *CleanupStack, state *RunState, outcome Outcome, steps []StepResult, err error) *RunResult {
	cleanupResults, cleanupSkipped := e.runCleanup(ctx, p, cleanupStack, state, outcome, steps)
	return &RunResult{
		Outcome:          outcome,
		Steps:            steps,
		CleanupResults:   cleanupResults,
		CleanupSkipped:   cleanupSkipped,
		Error:            err,
		InstantiatedPlan: p,
	}
}

// planStepIDs returns the IDs of p's main and verification steps, which no
// cleanup step ID may repeat.
func (e *Engine) planStepIDs(p *plan.Plan) []string {
	var ids []string
	for _, step := range p.Execution.Steps {
		ids = append(ids, step.StepID())
	}
	for _, step := range plan.VerificationSteps(p, e.graph, e.layeredDefaults) {
		ids = append(ids, step.StepID())
	}
	return ids
}

// pairedCleanupNodes returns the nodes that run from the cleanup stack for p:
// the graph-level cleanup of each step's node, and every node that cleanup's
// chain reaches. They run once per resource.
func (e *Engine) pairedCleanupNodes(p *plan.Plan) map[string]bool {
	paired := make(map[string]bool)
	for _, step := range p.Execution.Steps {
		node, ok := e.graph.Nodes[step.Node]
		for ok && node.Cleanup.Node != "" && !paired[node.Cleanup.Node] {
			paired[node.Cleanup.Node] = true
			node, ok = e.graph.Nodes[node.Cleanup.Node]
		}
	}
	return paired
}

// abortedCleanupBudget bounds the cleanup of an interrupted run.
const abortedCleanupBudget = 30 * time.Second

// abortedResult ends a run whose context was cancelled, such as by Ctrl+C:
// it runs cleanup under abortedCleanupBudget and records the outcome as
// aborted with the steps that ran.
func (e *Engine) abortedResult(ctx context.Context, p *plan.Plan, cleanupStack *CleanupStack, state *RunState, steps []StepResult) *RunResult {
	return e.endRun(ctx, p, cleanupStack, state, OutcomeAborted, steps, fmt.Errorf("execution cancelled: %w", ctx.Err()))
}

// cleanupRunOnMatches reports whether a plan-level cleanup step with the given
// runOn value should run for the outcome. An empty runOn means always.
func cleanupRunOnMatches(runOn string, outcome Outcome) bool {
	switch runOn {
	case "", "always":
		return true
	case "success":
		return outcome == OutcomePassed
	case "failure":
		return outcome != OutcomePassed
	default:
		return false
	}
}

// runVerification executes the plan's verification steps after the main flow.
// Inputs the plan does not wire are matched by name to outputs of earlier
// steps (most recent first). Verification steps never register cleanup
// entries. A failed assertion or an error status marks the run as failed; the
// remaining verification steps still run when ContinueOnAssertionFailure is
// set, otherwise verification stops at the first failure.
func (e *Engine) runVerification(ctx context.Context, steps []plan.Step, state *RunState, offset, total int) ([]StepResult, Outcome, error) {
	var results []StepResult
	outcome := OutcomePassed
	var firstErr error

	for i, step := range steps {
		node, ok := e.graph.Nodes[step.Node]
		if !ok {
			return results, OutcomeError, fmt.Errorf("verification node %q not found in graph", step.Node)
		}
		fillValuesByOutputName(&step, node, state)

		idx := offset + i
		if e.Observer != nil {
			e.Observer.OnStepStart(idx, total, step)
		}
		sr := e.executeStepWithTracking(ctx, step, node, state)
		results = append(results, sr)
		if e.Observer != nil {
			e.Observer.OnStepComplete(idx, total, sr)
		}

		var failure error
		switch {
		case sr.Error != nil:
			return results, OutcomeError, fmt.Errorf("verification step %s: %w", stepRef(step), sr.Error)
		case sr.StatusCode >= 400:
			failure = fmt.Errorf("verification step %s returned status %d", stepRef(step), sr.StatusCode)
		case sr.ResponseBodyError != nil:
			failure = fmt.Errorf("verification step %s: %s", stepRef(step), sr.ResponseBodyError.Summary())
		case sr.Validation != nil && !sr.Validation.Passed:
			failure = fmt.Errorf("verification step %s failed mechanical validation", stepRef(step))
		}
		if failure == nil {
			failure = e.oasStrictError(step, &sr)
		}
		if failure == nil {
			if sr.Outputs != nil {
				state.StoreOutputs(step.StepID(), sr.Outputs)
			}
			continue
		}
		outcome = OutcomeFailed
		if firstErr == nil {
			firstErr = failure
		}
		if !e.ContinueOnAssertionFailure {
			break
		}
	}
	return results, outcome, firstErr
}

// checkpointResult returns the stopped result when step is the --stop-after
// checkpoint, or nil otherwise. A checkpoint skips cleanup and verification so
// the resources created so far stay alive for an external harness. It is
// consulted after every step that passes, including an expectFailure step
// whose expected error came back.
func (e *Engine) checkpointResult(step plan.Step, stepResults []StepResult, p *plan.Plan) *RunResult {
	if e.stopAfterStep == "" || step.StepID() != e.stopAfterStep {
		return nil
	}
	return &RunResult{
		Outcome:          OutcomeStopped,
		Stopped:          true,
		StoppedAt:        step.StepID(),
		Steps:            stepResults,
		InstantiatedPlan: p,
	}
}

// oasStrictError returns an error when strict OAS validation is enabled and
// the step's request or response violated the spec. Skipped validations and
// schema compilation warnings never fail a step, and expected-failure steps
// are exempt because their error responses are the point of the test.
func (e *Engine) oasStrictError(step plan.Step, result *StepResult) error {
	if !e.oasStrict || step.ExpectFailure != nil {
		return nil
	}
	n := oasErrorCount(result.OASValidation)
	if n == 0 {
		return nil
	}
	return fmt.Errorf("step %s: OAS validation failed in strict mode (%d error(s))", stepRef(step), n)
}

// oasErrorCount returns how many request and response violations a validation
// found. A skipped validation, or none, counts zero.
func oasErrorCount(v *oas.ValidationResult) int {
	if v == nil || v.Skipped {
		return 0
	}
	n := 0
	if v.Request != nil {
		n += len(v.Request.Errors)
	}
	if v.Response != nil {
		n += len(v.Response.Errors)
	}
	return n
}

// stepRef names a step in an error message by its ID, which is what
// --stop-after, dependsOn, and the archive use, adding the node when the two
// differ so that two steps on one node can be told apart: "addSocks" (addItem).
func stepRef(step plan.Step) string {
	id := step.StepID()
	if id == step.Node {
		return strconv.Quote(id)
	}
	return fmt.Sprintf("%q (%s)", id, step.Node)
}

// stopAfterError returns an error when no step has the ID --stop-after names.
// Run output shows a node beside each step ID, so when the name is a node the
// error lists the IDs of the steps that run it.
func stopAfterError(name string, steps []plan.Step) error {
	var ids []string
	for _, step := range steps {
		if step.StepID() == name {
			return nil
		}
		if step.Node == name {
			ids = append(ids, strconv.Quote(step.StepID()))
		}
	}
	switch len(ids) {
	case 0:
		return fmt.Errorf("--stop-after: no step %q in plan", name)
	case 1:
		return fmt.Errorf("--stop-after: no step %q in plan (node %s is step %s)", name, name, ids[0])
	default:
		return fmt.Errorf("--stop-after: no step %q in plan (node %s is steps %s)", name, name, strings.Join(ids, ", "))
	}
}

// fillValuesByOutputName wires any node input the step leaves unset to the
// most recently executed step that produced an output with the same name.
func fillValuesByOutputName(step *plan.Step, node *graph.Node, state *RunState) {
	if step.Values == nil {
		step.Values = make(map[string]plan.StepValue)
	}
	executed := state.ExecutedSteps()
	for _, input := range node.Inputs {
		if _, set := step.Values[input.Name]; set {
			continue
		}
		for i := len(executed) - 1; i >= 0; i-- {
			if _, err := state.GetOutput(executed[i], input.Name); err == nil {
				step.Values[input.Name] = plan.StepValue{From: executed[i] + "." + input.Name}
				break
			}
		}
	}
}

// stepInputs holds the inputs resolved for a step, so that every attempt of a
// retried step sends the same values.
type stepInputs struct {
	resolved    bool
	inputs      map[string]any
	selections  []SelectionDecision
	resolutions []ValueResolution
	now         time.Time // when they were resolved; assertion expressions read it too
}

// executeStepWith executes one attempt of a step. When prepared already holds
// the step's inputs, they are used as they are: a retry resends what the first
// attempt sent, including pool picks and dates. Otherwise the inputs are
// resolved, and a successful resolution fills prepared when it is non-nil. A
// failed resolution is not kept, so the next attempt resolves again.
func (e *Engine) executeStepWith(ctx context.Context, step plan.Step, node *graph.Node, state *RunState, prepared *stepInputs) StepResult {
	start := time.Now()
	sid := step.StepID()

	var inputs map[string]any
	var selections []SelectionDecision
	var resolutions []ValueResolution
	var resolvedAt time.Time
	if prepared != nil && prepared.resolved {
		inputs, selections, resolutions, resolvedAt = prepared.inputs, prepared.selections, prepared.resolutions, prepared.now
	} else {
		// Construct ResolveContext from engine fields
		rctx := e.buildResolveContext(node)
		resolvedAt = rctx.Now

		// Resolve inputs
		var err error
		inputs, selections, resolutions, err = ResolveInputsWithContext(ctx, step, node, e.graph, state, rctx)
		if err != nil {
			return StepResult{
				StepID:      sid,
				Node:        step.Node,
				Inputs:      inputs,
				Selections:  selections,
				Resolutions: resolutions,
				Error:       fmt.Errorf("resolving inputs: %w", err),
				StartTime:   start,
				Duration:    time.Since(start),
			}
		}

		// Overlay value overrides win over plan/graph-resolved values. Each
		// replaces the input's resolution record, so the archive shows what was
		// sent.
		overlayValues, _ := e.router.ResolveValueOverride(node.Name)
		for k, v := range overlayValues {
			inputs[k] = v
			resolutions = recordOverrideValue(resolutions, k, v)
		}

		// Store resolved inputs so later steps can reference them via fromInput
		state.StoreInputs(sid, inputs)
		if prepared != nil {
			*prepared = stepInputs{resolved: true, inputs: inputs, selections: selections, resolutions: resolutions, now: resolvedAt}
		}
	}

	// Get adapter
	adp, err := e.registry.Get(node.Adapter)
	if err != nil {
		return StepResult{
			StepID:      sid,
			Node:        step.Node,
			Inputs:      inputs,
			Selections:  selections,
			Resolutions: resolutions,
			Error:       fmt.Errorf("getting adapter: %w", err),
			StartTime:   start,
			Duration:    time.Since(start),
		}
	}

	// Resolve executor/config/rewrite for this node
	exec, cfg, rewrite := e.router.Resolve(node.Name)
	actualBaseURL := exec.BaseURL

	// Build request
	req, err := adp.BuildRequest(inputs, cfg)
	if err != nil {
		return StepResult{
			StepID:        sid,
			Node:          step.Node,
			Inputs:        inputs,
			Selections:    selections,
			Resolutions:   resolutions,
			Error:         fmt.Errorf("building request: %w", err),
			StartTime:     start,
			Duration:      time.Since(start),
			ActualBaseURL: actualBaseURL,
		}
	}

	// Apply path rewriting if configured for this node
	originalPath := req.Path
	if rewrite != nil {
		req.Path = adapter.RewritePath(req.Path, rewrite)
	}

	// Raw body overrides the adapter-built body after template substitution,
	// letting mutations inject malformed payloads.
	if step.RawBody != "" {
		req.Body = []byte(step.RawBody)
	}

	// Execute, after waiting for the pacer
	resp, err := e.send(ctx, exec, req)
	if err != nil {
		sr := StepResult{
			StepID:        sid,
			Node:          step.Node,
			Inputs:        inputs,
			Selections:    selections,
			Resolutions:   resolutions,
			Request:       req,
			Error:         err,
			StartTime:     start,
			Duration:      time.Since(start),
			ActualBaseURL: actualBaseURL,
		}
		if rewrite != nil {
			sr.OriginalPath = originalPath
		}
		return sr
	}

	result := StepResult{
		StepID:        sid,
		Node:          step.Node,
		Inputs:        inputs,
		Selections:    selections,
		Resolutions:   resolutions,
		Request:       req,
		Response:      resp,
		StatusCode:    resp.StatusCode,
		StartTime:     start,
		Duration:      time.Since(start),
		ActualBaseURL: actualBaseURL,
	}
	if rewrite != nil {
		result.OriginalPath = originalPath
	}

	// Extract outputs (only on success)
	if resp.StatusCode < 400 {
		outputs, err := adp.ExtractOutputs(resp)
		if err != nil {
			result.Error = fmt.Errorf("extracting outputs: %w", err)
			return result
		}
		result.Outputs = outputs

		// Record transform script if present
		if tmpl, ok := e.registry.GetTemplate(node.Adapter); ok && tmpl.HasTransform() {
			result.TransformScript = tmpl.Response.Transform
		}

		// Collect outputs tagged for display
		for _, out := range node.Outputs {
			if out.Display != "" {
				if v, ok := outputs[out.Name]; ok {
					result.DisplayOutputs = append(result.DisplayOutputs, DisplayOutput{
						Label: out.Display,
						Name:  out.Name,
						Value: v,
					})
				}
			}
		}

		// Check for errors buried in the response body
		rules := effectiveErrorRules(node, e.graph)
		if rbe := CheckErrorDetection(rules, resp.Body); rbe != nil {
			result.ResponseBodyError = rbe
		}
	}

	// OAS schema validation runs before mechanical assertions so that the
	// schema assertion type can surface its results.
	if e.oasCache != nil && node.OAS != nil && req != nil && resp != nil {
		result.OASValidation = oas.ValidateStep(
			node, e.graphOAS, e.oasCache,
			req.Method, req.Path, req.Headers, req.Body,
			resp.StatusCode, resp.Headers, resp.Body,
		)
	}

	// Run mechanical assertions if configured.
	// Assertions with Raw=true evaluate against the raw HTTP response body.
	// Normal assertions evaluate against serialized extracted outputs when
	// available, falling back to the raw body (e.g., on 4xx responses).
	if step.Assertions != nil && len(step.Assertions.Mechanical) > 0 {
		merged := &validate.MechanicalResult{Passed: true}
		// Expressions in a fieldEquals value or a quoted predicate string read
		// the step's inputs, as step values do, and the time they were resolved,
		// so a retry compares with the dates it resent.
		rctx := e.buildResolveContext(node)
		ectx := plan.ExprContext{Now: resolvedAt, Env: rctx.EnvLookup, Values: inputs, Random: rctx.Random}
		predicateEval := func(expr string, fields map[string]any) (bool, error) {
			return plan.EvalPredicateWithExprs(expr, fields, ectx)
		}
		var rawAssertions, normalAssertions []plan.MechanicalAssertion
		for _, a := range step.Assertions.Mechanical {
			// A status assertion that expects success (a composed "2xx" default,
			// or one written before an overlay added expectFailure) can never
			// hold on a negative step, so it is reported as skipped. One that
			// agrees with expectFailure, such as 409 or 4xx, is evaluated.
			if step.ExpectFailure != nil && a.Type == string(validate.AssertStatus) && httpstatus.ContradictsFailure(a.Expect) {
				merged.Results = append(merged.Results, validate.AssertionResult{
					Type:    validate.AssertStatus,
					Passed:  true,
					Skipped: true,
					Message: fmt.Sprintf("status assertion expecting %v contradicts expectFailure", a.Expect),
				})
				continue
			}
			if a.Type == string(validate.AssertFieldEquals) {
				expanded, err := plan.EvalExpr(a.Value, ectx)
				if err != nil {
					merged.Results = append(merged.Results, validate.AssertionResult{
						Type:    validate.AssertFieldEquals,
						Path:    a.Path,
						Raw:     a.Raw,
						Message: fmt.Sprintf("expanding value %v: %v", a.Value, err),
					})
					merged.Passed = false
					continue
				}
				a.Value = expanded
			}
			if a.Raw {
				rawAssertions = append(rawAssertions, a)
			} else {
				normalAssertions = append(normalAssertions, a)
			}
		}

		normalBody, normalEval := resp.Body, predicateEval
		if result.Outputs != nil {
			if ob, err := json.Marshal(result.Outputs); err == nil {
				normalBody, normalEval = ob, namingMissingOutputs(predicateEval)
			}
		}

		schemaCheck := buildSchemaCheck(result.OASValidation)

		if len(normalAssertions) > 0 {
			nr := validate.RunMechanical(resp.StatusCode, normalBody,
				withDisplayedExprs(convertAssertions(normalAssertions), ectx), normalEval, schemaCheck)
			merged.Results = append(merged.Results, nr.Results...)
			if !nr.Passed {
				merged.Passed = false
			}
		}
		if len(rawAssertions) > 0 {
			rr := validate.RunMechanical(resp.StatusCode, resp.Body,
				withDisplayedExprs(convertAssertions(rawAssertions), ectx), predicateEval, schemaCheck)
			merged.Results = append(merged.Results, rr.Results...)
			if !rr.Passed {
				merged.Passed = false
			}
		}
		result.Validation = merged
	}

	return result
}

// namingMissingOutputs wraps the predicate evaluator of assertions that read a
// step's outputs. When a predicate names an output the step didn't produce,
// such as an optional one the response didn't hold, the error says so, since
// the predicate package's own message doesn't know its fields are outputs.
func namingMissingOutputs(eval validate.PredicateEvalFunc) validate.PredicateEvalFunc {
	return func(expr string, outputs map[string]any) (bool, error) {
		ok, err := eval(expr, outputs)
		var unknown *predicate.UnknownFieldError
		if errors.As(err, &unknown) {
			name, _, _ := strings.Cut(unknown.Name, ".")
			if _, produced := outputs[name]; !produced {
				return ok, fmt.Errorf("%w: the step produced no output %q (an optional output is absent when the response doesn't hold it)", err, name)
			}
		}
		return ok, err
	}
}

// buildSchemaCheck returns a SchemaCheckFunc that reports the OAS response
// validation result as a schema assertion outcome. Returns nil when no OAS
// validation is available so checkSchema can mark the assertion Skipped.
func buildSchemaCheck(v *oas.ValidationResult) validate.SchemaCheckFunc {
	if v == nil {
		return nil
	}
	return func(a validate.MechanicalAssertion) validate.AssertionResult {
		ar := validate.AssertionResult{}
		if v.Skipped {
			ar.Passed = true
			ar.Skipped = true
			ar.Message = "schema validation skipped: " + v.SkipReason
			return ar
		}
		if v.Response == nil {
			ar.Passed = true
			ar.Skipped = true
			ar.Message = "schema validation skipped: no response body validated"
			return ar
		}
		if v.Response.Skipped {
			ar.Passed = true
			ar.Skipped = true
			ar.Message = "schema validation skipped: " + v.Response.SkipReason
			return ar
		}
		if v.Response.Valid {
			ar.Passed = true
			ar.Message = "response matches OAS schema"
			return ar
		}
		ar.Passed = false
		msgs := make([]string, 0, len(v.Response.Errors))
		for _, e := range v.Response.Errors {
			if e.Path != "" {
				msgs = append(msgs, e.Path+": "+e.Message)
			} else {
				msgs = append(msgs, e.Message)
			}
		}
		ar.Message = "response does not match OAS schema: " + strings.Join(msgs, "; ")
		return ar
	}
}

// buildResolveContext creates a ResolveContext from the engine's configuration.
// Always returns a non-nil context so that expression evaluation and constraint
// checking are active.
func (e *Engine) buildResolveContext(node *graph.Node) *ResolveContext {
	envLookup := os.Getenv
	if len(e.envValues) > 0 {
		envLookup = func(key string) string {
			if v := os.Getenv(key); v != "" {
				return v
			}
			return e.envValues[key]
		}
	}
	return &ResolveContext{
		Now:       time.Now(),
		EnvLookup: envLookup,
		KB:        e.KB,
		Node:      node,
		Plan:      e.plan,
		Registry:  e.registry,

		LayeredDefaults: e.layeredDefaults,
	}
}

// convertAssertions bridges plan.MechanicalAssertion to validate.MechanicalAssertion.
// withDisplayedExprs fills in the text a predicate's message shows: the
// predicate with the {{…}} expressions in its literals expanded, so the message,
// and the archive, say what was compared.
func withDisplayedExprs(assertions []validate.MechanicalAssertion, ectx plan.ExprContext) []validate.MechanicalAssertion {
	for i, a := range assertions {
		if a.Type != validate.AssertPredicate || !plan.ContainsExpr(a.Expr) {
			continue
		}
		if text, err := plan.ExpandPredicateText(a.Expr, ectx); err == nil {
			assertions[i].Display = text
		}
	}
	return assertions
}

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
			Raw:    pa.Raw,
		}
	}
	return result
}
