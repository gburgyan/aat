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
// template to use, any addons to compose, and constraint classification.
type WorkflowSelection struct {
	Workflow    string         `json:"workflow"`
	Description string         `json:"description"`
	Addons      []string       `json:"addons,omitempty"`
	Repetitions map[string]int `json:"repetitions,omitempty"`
	Constraints ConstraintSet  `json:"constraints"`
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
	graphContext := FormatGraph(req.Graph)
	selStart := time.Now()
	sr, err := selectWorkflow(ctx, req.Client, graphContext, req.Prompt, req.Graph, now)
	if err != nil {
		if trace != nil && sr != nil {
			trace.SelectionCall = toLLMCallTrace(sr.System, sr.User, 0.1, sr.Response, time.Since(selStart), err)
		}
		return traceErr(fmt.Errorf("intent: workflow selection: %w", err))
	}

	ws := sr.Selection
	selJSON := sr.SelectionJSON

	if trace != nil {
		trace.SelectionCall = toLLMCallTrace(sr.System, sr.User, 0.1, sr.Response, time.Since(selStart), nil)
		trace.WorkflowSelection = ws
	}

	// --- Load/Compose Workflow Template ---
	if ws.Workflow == "" {
		return traceErr(fmt.Errorf("intent: no workflow selected; available workflows: %s", listWorkflowNames(req.Graph)))
	}

	wf, found := findWorkflowByName(req.Graph, ws.Workflow)
	if !found || wf.Template == "" {
		return traceErr(fmt.Errorf("intent: unknown or template-less workflow %q; available: %s", ws.Workflow, listWorkflowNames(req.Graph)))
	}

	var tpl *plan.Plan
	var templatePath string

	if len(ws.Addons) > 0 {
		composed, composeErr := ComposeWithAddons(wf, ws.Addons, req.Graph, req.GraphDir)
		if composeErr == nil {
			tpl = composed
			templatePath = wf.Template
		}
	}

	if tpl == nil {
		loaded, loadErr := LoadWorkflowTemplate(wf.Template, req.GraphDir, req.Graph)
		if loadErr != nil {
			return traceErr(fmt.Errorf("intent: loading template for %q: %w", ws.Workflow, loadErr))
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
		return traceErr(fmt.Errorf("intent: marshalling template skeleton: %w", marshalErr))
	}

	if trace != nil {
		trace.Skeleton = &SkeletonTrace{
			Plan:        skeleton,
			YAML:        string(skeletonBytes),
			UnfedInputs: unfedInputs,
		}
	}

	// --- Call 2: Targeted Value Fill ---
	inputContexts := buildInputContexts(skeleton, req.Graph, req.KB, ws)
	selectionContexts := buildSelectionContexts(skeleton, req.Graph)
	planFlow := buildCompactPlanFlow(skeleton, req.Graph)

	system, user := buildTargetedPlanPrompt(inputContexts, selectionContexts, planFlow, selJSON, req.Prompt, now)

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
		return traceErr(fmt.Errorf("intent: plan generation LLM call: %w", err))
	}

	if trace != nil {
		trace.PlanCall = toLLMCallTrace(system, user, 0.2, planResp, time.Since(planCallStart), nil)
	}

	// --- Parse and Apply ---
	targeted, err := parseTargetedResponse(planResp.Content)
	if err != nil {
		return traceErr(fmt.Errorf("intent: %w", err))
	}

	if trace != nil {
		trace.TargetedResponse = targeted
	}

	applyTargetedResponse(skeleton, targeted, unfedSet)

	if trace != nil {
		merged := copyPlanShallow(skeleton)
		trace.MergedPlan = merged
	}

	PostProcess(skeleton, req.Graph, ws, req.Prompt)

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

		retryResult := retryPlanGeneration(ctx, req, skeleton, ws, selJSON, valErr, trace, pipelineStart)
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
	selJSON string,
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
	inputContexts := buildInputContexts(retrySkeleton, req.Graph, req.KB, ws)
	selectionContexts := buildSelectionContexts(retrySkeleton, req.Graph)
	planFlow := buildCompactPlanFlow(retrySkeleton, req.Graph)

	now := time.Now()
	system, user := buildTargetedRetryPrompt(
		inputContexts, selectionContexts, planFlow, selJSON,
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

// selectWorkflow performs the first LLM call to select a workflow and
// classify constraints. Returns a selectionResult with the parsed selection,
// raw JSON, LLM response metadata, and prompt text.
func selectWorkflow(ctx context.Context, client llm.Client, graphContext, prompt string, g *graph.Graph, now time.Time) (*selectionResult, error) {
	system, user := buildWorkflowSelectionPrompt(graphContext, prompt, g, now)

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

	return &selectionResult{
		Selection:     &ws,
		SelectionJSON: content,
		Response:      resp,
		System:        system,
		User:          user,
	}, nil
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
