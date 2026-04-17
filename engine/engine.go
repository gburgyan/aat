package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/graph/oas"
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

	// plan is set during Run() for constraint-aware resolution.
	plan *plan.Plan
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
	// Ensure OnRunComplete fires on every exit path.
	defer func() {
		if e.Observer != nil && result != nil {
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
	total := len(sorted)

	if e.Observer != nil {
		e.Observer.OnRunStart(total, "strict")
	}

	for i, step := range sorted {
		select {
		case <-ctx.Done():
			// Use a detached context for cleanup so cleanup steps can complete
			// even though the parent context is cancelled.
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			cleanupResults := e.executeCleanupWithNotifications(cleanupCtx, cleanupStack, state)
			return &RunResult{
				Outcome:          OutcomeAborted,
				Steps:            stepResults,
				CleanupResults:   cleanupResults,
				Error:            fmt.Errorf("execution cancelled: %w", ctx.Err()),
				InstantiatedPlan: instantiatedPlan,
			}
		default:
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
			outcome = OutcomeError
			// Run cleanup before returning
			cleanupResults := e.executeCleanupWithNotifications(ctx, cleanupStack, state)
			return &RunResult{
				Outcome:          outcome,
				Steps:            stepResults,
				CleanupResults:   cleanupResults,
				Error:            stepResult.Error,
				InstantiatedPlan: instantiatedPlan,
			}
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
				// Mechanical assertions still ran in executeStep; check them.
				if stepResult.Validation != nil && !stepResult.Validation.Passed {
					outcome = OutcomeFailed
					if !e.ContinueOnAssertionFailure {
						cleanupResults := e.executeCleanupWithNotifications(ctx, cleanupStack, state)
						return &RunResult{
							Outcome:          outcome,
							Steps:            stepResults,
							CleanupResults:   cleanupResults,
							Error:            fmt.Errorf("step %q failed mechanical validation", step.Node),
							InstantiatedPlan: instantiatedPlan,
						}
					}
				}
				continue
			}

			// Unexpected success or wrong error code — FAIL.
			outcome = OutcomeFailed
			cleanupResults := e.executeCleanupWithNotifications(ctx, cleanupStack, state)
			return &RunResult{
				Outcome:          outcome,
				Steps:            stepResults,
				CleanupResults:   cleanupResults,
				Error:            fmt.Errorf("step %q: expected failure status %v but got %d", step.Node, step.ExpectFailure.Status, stepResult.StatusCode),
				InstantiatedPlan: instantiatedPlan,
			}
		}

		if stepResult.StatusCode >= 400 {
			outcome = OutcomeFailed
			// Run cleanup before returning
			cleanupResults := e.executeCleanupWithNotifications(ctx, cleanupStack, state)
			return &RunResult{
				Outcome:          outcome,
				Steps:            stepResults,
				CleanupResults:   cleanupResults,
				Error:            fmt.Errorf("step %q returned status %d", step.Node, stepResult.StatusCode),
				InstantiatedPlan: instantiatedPlan,
			}
		}

		// Check for response body errors (API returned 2xx but body indicates error)
		if stepResult.ResponseBodyError != nil {
			outcome = OutcomeFailed
			// Do NOT store outputs — error responses produce unreliable data
			// Do NOT push cleanup — failing node did not create a valid resource
			cleanupResults := e.executeCleanupWithNotifications(ctx, cleanupStack, state)
			return &RunResult{
				Outcome:          outcome,
				Steps:            stepResults,
				CleanupResults:   cleanupResults,
				Error:            fmt.Errorf("step %q: %s", step.Node, stepResult.ResponseBodyError.Summary()),
				InstantiatedPlan: instantiatedPlan,
			}
		}

		// Store outputs keyed by step ID (supports step aliasing)
		if stepResult.Outputs != nil {
			state.StoreOutputs(step.StepID(), stepResult.Outputs)
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
		if stepResult.Validation != nil && !stepResult.Validation.Passed {
			outcome = OutcomeFailed
			if !e.ContinueOnAssertionFailure {
				cleanupResults := e.executeCleanupWithNotifications(ctx, cleanupStack, state)
				return &RunResult{
					Outcome:          outcome,
					Steps:            stepResults,
					CleanupResults:   cleanupResults,
					Error:            fmt.Errorf("step %q failed mechanical validation", step.Node),
					InstantiatedPlan: instantiatedPlan,
				}
			}
		}
	}

	// All steps succeeded — run cleanup
	cleanupResults := e.executeCleanupWithNotifications(ctx, cleanupStack, state)

	return &RunResult{
		Outcome:          outcome,
		Steps:            stepResults,
		CleanupResults:   cleanupResults,
		InstantiatedPlan: instantiatedPlan,
	}
}

// executeCleanupWithNotifications runs cleanup and notifies the observer.
func (e *Engine) executeCleanupWithNotifications(ctx context.Context, cleanupStack *CleanupStack, state *RunState) []StepResult {
	if e.Observer != nil && cleanupStack.Len() > 0 {
		e.Observer.OnCleanupStart(cleanupStack.Len())
	}
	cleanupResults := cleanupStack.ExecuteAll(ctx, e.graph, e.registry, e.router, state)
	if e.Observer != nil {
		for i, cr := range cleanupResults {
			e.Observer.OnCleanupStepComplete(i, len(cleanupResults), cr)
		}
	}
	return cleanupResults
}

func (e *Engine) executeStep(ctx context.Context, step plan.Step, node *graph.Node, state *RunState) StepResult {
	start := time.Now()
	sid := step.StepID()

	// Construct ResolveContext from engine fields
	rctx := e.buildResolveContext(node)

	// Resolve inputs
	inputs, selections, resolutions, err := ResolveInputsWithContext(ctx, step, node, e.graph, state, rctx)
	if err != nil {
		return StepResult{
			StepID:    sid,
			Node:      step.Node,
			Error:     fmt.Errorf("resolving inputs: %w", err),
			StartTime: start,
			Duration:  time.Since(start),
		}
	}

	// Overlay value overrides win over plan/graph-resolved values.
	overlayValues, _ := e.router.ResolveValueOverride(node.Name)
	for k, v := range overlayValues {
		inputs[k] = v
	}

	// Store resolved inputs so later steps can reference them via fromInput
	state.StoreInputs(sid, inputs)

	// Get adapter
	adp, err := e.registry.Get(node.Adapter)
	if err != nil {
		return StepResult{
			StepID:    sid,
			Node:      step.Node,
			Inputs:    inputs,
			Error:     fmt.Errorf("getting adapter: %w", err),
			StartTime: start,
			Duration:  time.Since(start),
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

	// Execute
	resp, err := exec.Execute(ctx, req)
	if err != nil {
		sr := StepResult{
			StepID:        sid,
			Node:          step.Node,
			Inputs:        inputs,
			Request:       req,
			Error:         fmt.Errorf("executing request: %w", err),
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
		var rawAssertions, normalAssertions []plan.MechanicalAssertion
		for _, a := range step.Assertions.Mechanical {
			if a.Raw {
				rawAssertions = append(rawAssertions, a)
			} else {
				normalAssertions = append(normalAssertions, a)
			}
		}

		normalBody := resp.Body
		if result.Outputs != nil {
			if ob, err := json.Marshal(result.Outputs); err == nil {
				normalBody = ob
			}
		}

		schemaCheck := buildSchemaCheck(result.OASValidation)

		merged := &validate.MechanicalResult{Passed: true}
		if len(normalAssertions) > 0 {
			nr := validate.RunMechanical(resp.StatusCode, normalBody,
				convertAssertions(normalAssertions), plan.EvalPredicate, schemaCheck)
			merged.Results = append(merged.Results, nr.Results...)
			if !nr.Passed {
				merged.Passed = false
			}
		}
		if len(rawAssertions) > 0 {
			rr := validate.RunMechanical(resp.StatusCode, resp.Body,
				convertAssertions(rawAssertions), plan.EvalPredicate, schemaCheck)
			merged.Results = append(merged.Results, rr.Results...)
			if !rr.Passed {
				merged.Passed = false
			}
		}
		result.Validation = merged
	}

	return result
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
	}
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
			Raw:    pa.Raw,
		}
	}
	return result
}
