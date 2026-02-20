package intent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/llm"
	"github.com/gburgyan/aat/plan"
)

// InterpretRequest holds the inputs for prompt-to-plan transformation.
type InterpretRequest struct {
	Prompt      string
	Graph       *graph.Graph
	KB          *domain.KnowledgeBase // may be nil
	Client      llm.Client
	EnableTrace bool   // when true, pipeline observability data is captured in the result
	GraphDir    string // directory containing graph file; for resolving template paths
}

// InterpretResult holds the outputs of prompt-to-plan transformation.
// When EnableTrace is set and an error occurs mid-pipeline, the result may be
// non-nil with only Trace populated — callers can inspect the partial trace for
// debugging even when the returned error is non-nil.
type InterpretResult struct {
	Plan               *plan.Plan
	WorkflowSelection  *WorkflowSelection
	Trace              *PlanTrace // non-nil only when EnableTrace was true
}

// WorkflowSelection captures the output of the first LLM call: which workflow
// template to use and any addons to compose.
type WorkflowSelection struct {
	Workflow    string         `json:"workflow"`
	Description string         `json:"description"`
	Addons      []string       `json:"addons,omitempty"`
	Repetitions map[string]int `json:"repetitions,omitempty"`
}

// GoalAnalysis captures the output of the first LLM call.
type GoalAnalysis struct {
	Goal             string            `json:"goal"`
	Description      string            `json:"description"`
	ConditionContext map[string]any    `json:"conditionContext"`
	PathPreferences  map[string]string `json:"pathPreferences"`
	Constraints      ConstraintSet     `json:"constraints"`
	Workflow         string            `json:"workflow,omitempty"`    // selected workflow name
	Repetitions      map[string]int    `json:"repetitions,omitempty"` // node → count (e.g. {"addTraveler": 2})
	Addons           []string          `json:"addons,omitempty"`      // addon workflow names to compose
}

// ConstraintSet classifies constraints by enforcement level.
type ConstraintSet struct {
	Hard []ConstraintInfo `json:"hard"`
	Soft []ConstraintInfo `json:"soft"`
	Free []string         `json:"free"`
}

// ConstraintInfo describes a single constraint.
type ConstraintInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	AppliesTo   []string `json:"appliesTo"`
}

// selectionResult is the internal return type from selectWorkflow, carrying the
// response metadata and prompts alongside the parsed selection.
type selectionResult struct {
	Selection     *WorkflowSelection
	SelectionJSON string
	Response      *llm.Response
	System        string // system prompt sent
	User          string // user prompt sent
	RetriedFrom   *selectionResult // non-nil if this is a retry result
}

// Interpret transforms a natural language prompt into a validated execution plan.
// It uses a two-call LLM architecture:
//  1. Workflow selection: identify which workflow template to use and classify constraints
//  2. Value fill: given a deterministic skeleton, fill literal values and assertions
//
// The first call selects a named workflow from the graph. If addons are requested,
// they are composed into the base template. The LLM only provides creative content
// (literal values, selection strategy overrides, assertions). MergeLLMValues
// combines the LLM output into the authoritative skeleton.
//
// When EnableTrace is set, pipeline observability data is captured. If an error
// occurs mid-pipeline, a partial trace is returned alongside the error so the
// CLI can still write the trace for debugging.
func Interpret(ctx context.Context, req InterpretRequest) (*InterpretResult, error) {
	if req.Prompt == "" {
		return nil, fmt.Errorf("intent: prompt is required")
	}
	if req.Graph == nil {
		return nil, fmt.Errorf("intent: graph is required")
	}
	if req.Client == nil {
		return nil, fmt.Errorf("intent: LLM client is required")
	}

	pipelineStart := time.Now()
	now := pipelineStart

	// Initialize trace if enabled.
	var trace *PlanTrace
	if req.EnableTrace {
		trace = &PlanTrace{
			TraceID:   generateTraceID(),
			Timestamp: now,
			Prompt:    req.Prompt,
		}
	}

	// traceErr is a helper that sets the error on the trace and returns a
	// partial result with the trace attached.
	traceErr := func(err error) (*InterpretResult, error) {
		if trace != nil {
			trace.Error = err.Error()
			trace.TotalDurationMs = time.Since(pipelineStart).Milliseconds()
			return &InterpretResult{Trace: trace}, err
		}
		return nil, err
	}

	// --- Call 1: Workflow Selection ---
	workflowMenu := FormatWorkflowMenu(req.Graph)
	selStart := time.Now()
	sr, err := selectWorkflow(ctx, req.Client, workflowMenu, req.Prompt, req.Graph, now)
	if err != nil {
		if trace != nil && sr != nil {
			trace.SelectionCall = toLLMCallTrace(sr.System, sr.User, 0.1, sr.Response, time.Since(selStart), err)
		}
		return traceErr(fmt.Errorf("intent: workflow selection: %w", err))
	}

	ws := sr.Selection

	if trace != nil {
		if sr.RetriedFrom != nil {
			// The first attempt was retried. Capture the original as SelectionCall
			// and the retry as SelectionRetryCall.
			orig := sr.RetriedFrom
			trace.SelectionCall = toLLMCallTrace(orig.System, orig.User, 0.1, orig.Response, time.Since(selStart), nil)
			retryTrace := toLLMCallTrace(sr.System, sr.User, 0.1, sr.Response, time.Since(selStart), nil)
			trace.SelectionRetryCall = &retryTrace
		} else {
			trace.SelectionCall = toLLMCallTrace(sr.System, sr.User, 0.1, sr.Response, time.Since(selStart), nil)
		}
		trace.WorkflowSelection = ws
	}

	// --- Build and Fill Plan ---
	skeleton, targeted, err := buildAndFillPlan(ctx, req, ws, trace, now)
	if err != nil {
		return traceErr(err)
	}

	// --- Wrong Plan Escape (max 1 round-trip) ---
	if targeted.WrongPlan != nil {
		if trace != nil {
			trace.WrongPlanSignal = targeted.WrongPlan
			wrongPlanCall := trace.PlanCall
			trace.WrongPlanCall = &wrongPlanCall
		}

		// Re-run Call 1 with feedback about the mismatch.
		feedback := fmt.Sprintf(
			"\n\nNote: The value generator indicated the selected workflow %q doesn't match the intent: %s",
			ws.Workflow, targeted.WrongPlan.Reason,
		)
		if targeted.WrongPlan.Suggested != "" {
			feedback += fmt.Sprintf(" Suggested alternative: %s.", targeted.WrongPlan.Suggested)
		}

		reselStart := time.Now()
		reselSR, reselErr := selectWorkflow(ctx, req.Client, workflowMenu, req.Prompt+feedback, req.Graph, now)
		if reselErr != nil {
			return traceErr(fmt.Errorf("intent: reselection after wrong-plan: %w", reselErr))
		}

		if trace != nil {
			reselTrace := toLLMCallTrace(reselSR.System, reselSR.User, 0.1, reselSR.Response, time.Since(reselStart), nil)
			trace.ReselectionCall = &reselTrace
			trace.WorkflowSelection = reselSR.Selection
		}

		ws = reselSR.Selection

		skeleton, _, err = buildAndFillPlan(ctx, req, ws, trace, now)
		if err != nil {
			return traceErr(err)
		}
		// Don't check wrongPlan on the second attempt — proceed with whatever we got.
	}

	// --- Validate ---
	if err := plan.Validate(skeleton, req.Graph); err != nil {
		var valErr *plan.ValidationError
		if !errors.As(err, &valErr) {
			if trace != nil {
				trace.ValidationErr = err.Error()
				trace.FinalPlan = skeleton
			}
			return traceErr(fmt.Errorf("intent: validating generated plan: %w", err))
		}

		if trace != nil {
			trace.ValidationErr = err.Error()
		}

		retryResult := retryPlanGeneration(ctx, req, skeleton, ws, valErr, trace, pipelineStart)
		if retryResult != nil {
			retryResult.WorkflowSelection = ws
			return retryResult, nil
		}

		if trace != nil {
			trace.FinalPlan = skeleton
		}
		return traceErr(fmt.Errorf("intent: validating generated plan: %w", err))
	}

	if trace != nil {
		trace.FinalPlan = skeleton
		trace.TotalDurationMs = time.Since(pipelineStart).Milliseconds()
	}

	return &InterpretResult{
		Plan:              skeleton,
		WorkflowSelection: ws,
		Trace:             trace,
	}, nil
}

// buildAndFillPlan loads the workflow template, composes addons, expands
// multiplicity, runs the targeted value fill (Call 2), validates/applies the
// response, and post-processes the plan. Returns the filled plan and the
// targeted response. If the LLM signals wrong-plan, targeted.WrongPlan will
// be set and the plan will have PostProcess applied but no LLM values.
func buildAndFillPlan(
	ctx context.Context,
	req InterpretRequest,
	ws *WorkflowSelection,
	trace *PlanTrace,
	now time.Time,
) (*plan.Plan, *TargetedResponse, error) {
	// --- Load/Compose Workflow Template ---
	if ws.Workflow == "" {
		return nil, nil, fmt.Errorf("intent: no workflow selected; available workflows: %s", listWorkflowNames(req.Graph))
	}

	wf, found := findWorkflowByName(req.Graph, ws.Workflow)
	if !found || wf.Template == "" {
		return nil, nil, fmt.Errorf("intent: unknown or template-less workflow %q; available: %s", ws.Workflow, listWorkflowNames(req.Graph))
	}

	var tpl *plan.Plan
	var templatePath string

	if len(ws.Addons) > 0 {
		composed, composeErr := ComposeWithAddons(wf, ws.Addons, req.Graph, req.GraphDir)
		if composeErr != nil {
			return nil, nil, fmt.Errorf("intent: composing addons for %q: %w", ws.Workflow, composeErr)
		}
		tpl = composed
		templatePath = wf.Template
	} else {
		loaded, loadErr := LoadWorkflowTemplate(wf.Template, req.GraphDir, req.Graph)
		if loadErr != nil {
			return nil, nil, fmt.Errorf("intent: loading template for %q: %w", ws.Workflow, loadErr)
		}
		tpl = loaded
		templatePath = wf.Template
	}

	if trace != nil {
		trace.WorkflowName = ws.Workflow
		trace.TemplatePath = templatePath
		trace.Repetitions = ws.Repetitions
	}

	ExpandMultiplicity(tpl, ws.Repetitions)

	if trace != nil {
		trace.TemplateExpanded = copyPlanShallow(tpl)
	}

	skeleton := tpl
	unfedInputs := UnfedInputsFromTemplate(tpl, req.Graph)
	unfedSet := unfedInputSet(tpl, req.Graph)

	// Marshal skeleton to YAML for trace/debugging (not sent to LLM).
	skeletonBytes, marshalErr := plan.Marshal(skeleton)
	if marshalErr != nil {
		return nil, nil, fmt.Errorf("intent: marshalling template skeleton: %w", marshalErr)
	}

	if trace != nil {
		trace.Skeleton = &SkeletonTrace{
			Plan:        skeleton,
			YAML:        string(skeletonBytes),
			UnfedInputs: unfedInputs,
		}
	}

	// --- Call 2: Targeted Value Fill ---
	inputContexts := buildInputContexts(skeleton, req.Graph, req.KB)
	selectionContexts := buildSelectionContexts(skeleton, req.Graph)

	system, user := buildTargetedPlanPrompt(inputContexts, selectionContexts, req.Prompt, now)

	planCallStart := time.Now()
	planResp, err := req.Client.Complete(ctx, &llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: system},
			{Role: llm.RoleUser, Content: user},
		},
		Temperature: 0.2,
	})
	if err != nil {
		if trace != nil {
			trace.PlanCall = toLLMCallTrace(system, user, 0.2, nil, time.Since(planCallStart), err)
		}
		return nil, nil, fmt.Errorf("intent: plan generation LLM call: %w", err)
	}

	if trace != nil {
		trace.PlanCall = toLLMCallTrace(system, user, 0.2, planResp, time.Since(planCallStart), nil)
	}

	// --- Parse, Validate Response, and Apply ---
	targeted, err := parseTargetedResponse(planResp.Content)
	if err != nil {
		return nil, nil, fmt.Errorf("intent: %w", err)
	}

	if trace != nil {
		trace.TargetedResponse = targeted
	}

	// If the LLM signaled wrong-plan, skip value application but still run
	// PostProcess for structural fixes (cleanup, deps).
	if targeted.WrongPlan != nil {
		PostProcess(skeleton, req.Graph, ws, req.Prompt)
		return skeleton, targeted, nil
	}

	// Validate the LLM response before applying it to the skeleton.
	issues := validateTargetedResponse(targeted, unfedSet, inputContexts, selectionContexts)
	if len(issues) > 0 {
		// Retry once with specific error feedback.
		retryMsgs := formatValidationIssues(issues)
		retrySystem, retryUser := buildTargetedRetryPrompt(
			inputContexts, selectionContexts, req.Prompt, now, retryMsgs, nil,
		)

		retryStart := time.Now()
		retryResp, retryErr := req.Client.Complete(ctx, &llm.Request{
			Messages: []llm.Message{
				{Role: llm.RoleSystem, Content: retrySystem},
				{Role: llm.RoleUser, Content: retryUser},
			},
			Temperature: 0.2,
		})
		if retryErr == nil {
			if retried, parseErr := parseTargetedResponse(retryResp.Content); parseErr == nil {
				retryIssues := validateTargetedResponse(retried, unfedSet, inputContexts, selectionContexts)
				if len(retryIssues) == 0 {
					// Retry succeeded — use the retried response.
					targeted = retried
					if trace != nil {
						ct := toLLMCallTrace(retrySystem, retryUser, 0.2, retryResp, time.Since(retryStart), nil)
						trace.RetryCall = &ct
						trace.TargetedResponse = targeted
					}
				}
				// If retry still has issues, fall through with original response
				// and let plan.Validate catch structural problems.
			}
		}
	}

	applyTargetedResponse(skeleton, targeted, unfedSet)

	if trace != nil {
		merged := copyPlanShallow(skeleton)
		trace.MergedPlan = merged
	}

	PostProcess(skeleton, req.Graph, ws, req.Prompt)

	return skeleton, targeted, nil
}

// retryPlanGeneration attempts a single retry of plan generation (call 2)
// after a validation failure. It blanks broken fromSelection references,
// rebuilds the context, and re-runs the targeted LLM call with validation
// errors in the prompt. Returns a successful InterpretResult or nil if
// retry also fails.
func retryPlanGeneration(
	ctx context.Context,
	req InterpretRequest,
	skeleton *plan.Plan,
	ws *WorkflowSelection,
	valErr *plan.ValidationError,
	trace *PlanTrace,
	pipelineStart time.Time,
) *InterpretResult {
	// Make a copy of the skeleton so we don't mutate the original for trace purposes.
	retrySkeleton := copyPlanShallow(skeleton)

	// Deep-copy the steps slice so mutations don't affect the original.
	retrySteps := make([]plan.Step, len(skeleton.Execution.Steps))
	copy(retrySteps, skeleton.Execution.Steps)
	retrySkeleton.Execution.Steps = retrySteps

	// Deep-copy value maps within steps.
	for i, step := range retrySkeleton.Execution.Steps {
		if step.Values != nil {
			newValues := make(map[string]plan.StepValue, len(step.Values))
			for k, v := range step.Values {
				newValues[k] = v
			}
			retrySkeleton.Execution.Steps[i].Values = newValues
		}
	}

	blanked, hints := blankBrokenFromSelections(retrySkeleton, req.Graph)
	if len(blanked) == 0 {
		return nil
	}

	// Rebuild contexts for the retry skeleton.
	inputContexts := buildInputContexts(retrySkeleton, req.Graph, req.KB)
	selectionContexts := buildSelectionContexts(retrySkeleton, req.Graph)

	now := time.Now()
	system, user := buildTargetedRetryPrompt(
		inputContexts, selectionContexts,
		req.Prompt, now, valErr.Errors, hints,
	)

	retryCallStart := time.Now()
	planResp, err := req.Client.Complete(ctx, &llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: system},
			{Role: llm.RoleUser, Content: user},
		},
		Temperature: 0.2,
	})
	if err != nil {
		if trace != nil {
			ct := toLLMCallTrace(system, user, 0.2, nil, time.Since(retryCallStart), err)
			trace.RetryCall = &ct
		}
		return nil
	}

	if trace != nil {
		ct := toLLMCallTrace(system, user, 0.2, planResp, time.Since(retryCallStart), nil)
		trace.RetryCall = &ct
	}

	targeted, err := parseTargetedResponse(planResp.Content)
	if err != nil {
		return nil
	}

	retryUnfedSet := unfedInputSet(retrySkeleton, req.Graph)
	applyTargetedResponse(retrySkeleton, targeted, retryUnfedSet)

	PostProcess(retrySkeleton, req.Graph, nil, req.Prompt)

	if err := plan.Validate(retrySkeleton, req.Graph); err != nil {
		if trace != nil {
			trace.RetryValidationErr = err.Error()
			trace.FinalPlan = retrySkeleton
		}
		return nil
	}

	if trace != nil {
		trace.FinalPlan = retrySkeleton
		trace.TotalDurationMs = time.Since(pipelineStart).Milliseconds()
	}

	return &InterpretResult{
		Plan:  retrySkeleton,
		Trace: trace,
	}
}

// copyPlanShallow creates a shallow copy of a Plan for trace snapshots.
// It copies the top-level struct so later mutations to the original don't
// affect the snapshot.
func copyPlanShallow(p *plan.Plan) *plan.Plan {
	cp := *p
	return &cp
}

// selectWorkflow performs the first LLM call to select a workflow.
// It validates the response and retries once if validation fails.
// Returns a selectionResult with the parsed selection, raw JSON, LLM response
// metadata, and prompt text.
func selectWorkflow(ctx context.Context, client llm.Client, workflowMenu, prompt string, g *graph.Graph, now time.Time) (*selectionResult, error) {
	system, user := buildWorkflowSelectionPrompt(workflowMenu, prompt, now)

	resp, err := client.Complete(ctx, &llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: system},
			{Role: llm.RoleUser, Content: user},
		},
		Temperature: 0.1,
	})
	if err != nil {
		return &selectionResult{System: system, User: user}, fmt.Errorf("LLM call failed: %w", err)
	}

	content := strings.TrimSpace(resp.Content)
	content = stripJSONFencing(content)

	var ws WorkflowSelection
	if err := json.Unmarshal([]byte(content), &ws); err != nil {
		return &selectionResult{
			Selection:     &ws,
			SelectionJSON: content,
			Response:      resp,
			System:        system,
			User:          user,
		}, fmt.Errorf("parsing workflow selection response: %w", err)
	}

	sr := &selectionResult{
		Selection:     &ws,
		SelectionJSON: content,
		Response:      resp,
		System:        system,
		User:          user,
	}

	// Validate the selection.
	validationErrors := validateWorkflowSelection(&ws, g)
	if len(validationErrors) == 0 {
		return sr, nil
	}

	// Retry once with error feedback.
	retrySystem := system + "\n\nIMPORTANT: The previous response had these issues:\n"
	for _, e := range validationErrors {
		retrySystem += "- " + e + "\n"
	}
	retrySystem += "Fix these issues in your JSON response."

	retryResp, retryErr := client.Complete(ctx, &llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: retrySystem},
			{Role: llm.RoleUser, Content: user},
		},
		Temperature: 0.1,
	})
	if retryErr != nil {
		// Retry LLM call failed — return the original (possibly flawed) result.
		return sr, nil
	}

	retryContent := strings.TrimSpace(retryResp.Content)
	retryContent = stripJSONFencing(retryContent)

	var retryWS WorkflowSelection
	if err := json.Unmarshal([]byte(retryContent), &retryWS); err != nil {
		// Can't parse retry — use original.
		return sr, nil
	}

	retrySR := &selectionResult{
		Selection:     &retryWS,
		SelectionJSON: retryContent,
		Response:      retryResp,
		System:        retrySystem,
		User:          user,
	}

	// Use retry result regardless of whether it passes validation — we only retry once.
	retryValidationErrors := validateWorkflowSelection(&retryWS, g)
	if len(retryValidationErrors) == 0 {
		retrySR.RetriedFrom = sr
		return retrySR, nil
	}

	// Retry also failed validation — return original.
	return sr, nil
}

// validateWorkflowSelection checks that the LLM's workflow selection is valid
// against the graph. Returns human-readable error strings (empty = valid).
func validateWorkflowSelection(ws *WorkflowSelection, g *graph.Graph) []string {
	var errs []string

	if ws.Workflow == "" {
		errs = append(errs, "workflow name is empty; select one from the available list")
		return errs
	}

	// Check workflow matches an available base workflow.
	wf, found := findWorkflowByName(g, ws.Workflow)
	if !found {
		errs = append(errs, fmt.Sprintf("workflow %q not found; available: %s", ws.Workflow, listWorkflowNames(g)))
		return errs
	}
	if wf.Template == "" {
		errs = append(errs, fmt.Sprintf("workflow %q has no template", ws.Workflow))
	}
	if wf.IsAddon() {
		errs = append(errs, fmt.Sprintf("workflow %q is an addon, not a base workflow; select a base workflow instead", ws.Workflow))
	}

	// Check each addon exists and is actually an addon.
	seen := map[string]bool{}
	for _, addonName := range ws.Addons {
		if seen[addonName] {
			errs = append(errs, fmt.Sprintf("duplicate addon %q", addonName))
			continue
		}
		seen[addonName] = true

		addon, addonFound := findWorkflowByName(g, addonName)
		if !addonFound {
			errs = append(errs, fmt.Sprintf("addon %q not found", addonName))
			continue
		}
		if !addon.IsAddon() {
			errs = append(errs, fmt.Sprintf("%q is not an addon workflow", addonName))
		}
		if addon.Template == "" {
			errs = append(errs, fmt.Sprintf("addon %q has no template", addonName))
		}
	}

	return errs
}

// stripJSONFencing removes ```json ... ``` fencing if present.
func stripJSONFencing(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = s[len("```json"):]
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	} else if strings.HasPrefix(s, "```") {
		s = s[3:]
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}
	return s
}

// listWorkflowNames returns a comma-separated list of workflow names for error messages.
func listWorkflowNames(g *graph.Graph) string {
	var names []string
	for _, wf := range g.Workflows {
		if wf.Template != "" {
			label := wf.Name
			if wf.IsAddon() {
				label += " [addon]"
			}
			names = append(names, label)
		}
	}
	return strings.Join(names, ", ")
}
