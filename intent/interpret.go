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
	Plan         *plan.Plan
	GoalAnalysis *GoalAnalysis
	ChainResult  *graph.ChainResult
	Trace        *PlanTrace // non-nil only when EnableTrace was true
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

// goalResult is the internal return type from analyzeGoal, carrying the response
// metadata and prompts alongside the parsed analysis.
type goalResult struct {
	Analysis *GoalAnalysis
	GoalJSON string
	Response *llm.Response
	Fallback bool
	System   string // system prompt sent
	User     string // user prompt sent
}

// Interpret transforms a natural language prompt into a validated execution plan.
// It uses a two-call LLM architecture:
//  1. Goal analysis: identify goal node and classify constraints
//  2. Value fill: given a deterministic skeleton, fill literal values and assertions
//
// Between calls, backward chaining computes the minimal subgraph and BuildSkeleton
// creates a structurally complete plan. The LLM only provides creative content
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

	// --- Call 1: Goal Analysis ---
	graphContext := FormatGraph(req.Graph)
	goalStart := time.Now()
	gr, err := analyzeGoal(ctx, req.Client, graphContext, req.Prompt, req.Graph, now)
	if err != nil {
		if trace != nil && gr != nil {
			trace.GoalCall = toLLMCallTrace(gr.System, gr.User, 0.1, gr.Response, time.Since(goalStart), err)
		}
		return traceErr(fmt.Errorf("intent: goal analysis: %w", err))
	}

	goalAnalysis := gr.Analysis
	goalJSON := gr.GoalJSON

	if trace != nil {
		trace.GoalCall = toLLMCallTrace(gr.System, gr.User, 0.1, gr.Response, time.Since(goalStart), nil)
		trace.GoalAnalysis = goalAnalysis
		trace.GoalFallback = gr.Fallback
	}

	// --- Workflow Template or Backward Chaining ---
	var skeleton *plan.Plan
	var skeletonBytes []byte
	var unfedInputs []string
	var chainResult *graph.ChainResult
	var chainContext string
	useTemplateIDs := false // when true, use MergeLLMValuesWithIDs instead of MergeLLMValues

	if goalAnalysis.Workflow != "" {
		templatePath, found := FindWorkflowTemplate(req.Graph, goalAnalysis.Workflow)
		if found {
			tpl, loadErr := LoadWorkflowTemplate(templatePath, req.GraphDir, req.Graph)
			if loadErr == nil {
				if trace != nil {
					trace.WorkflowName = goalAnalysis.Workflow
					trace.TemplatePath = templatePath
					trace.Repetitions = goalAnalysis.Repetitions
				}

				ExpandMultiplicity(tpl, goalAnalysis.Repetitions)

				if trace != nil {
					trace.TemplateExpanded = copyPlanShallow(tpl)
				}

				skeleton = tpl
				unfedInputs = UnfedInputsFromTemplate(tpl, req.Graph)
				useTemplateIDs = true

				// Marshal skeleton to YAML for the LLM prompt.
				var marshalErr error
				skeletonBytes, marshalErr = plan.Marshal(skeleton)
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
			}
			// on load error: fall through to backward chaining
		}
	}

	if skeleton == nil {
		// --- Backward Chaining (default path) ---
		chainStart := time.Now()
		var chainErr error
		chainResult, chainErr = graph.BackwardChain(req.Graph, graph.ChainOptions{
			Goals:            []string{goalAnalysis.Goal},
			ConditionContext: goalAnalysis.ConditionContext,
			EvalPredicate:    plan.EvalPredicate,
		})
		if chainErr != nil {
			return traceErr(fmt.Errorf("intent: backward chaining: %w", chainErr))
		}

		if trace != nil {
			trace.ChainResult = toChainTrace(chainResult, time.Since(chainStart))
		}

		// --- Build Skeleton ---
		skelStart := time.Now()
		skeleton = BuildSkeleton(req.Graph, chainResult, goalAnalysis, req.Prompt, now)

		var marshalErr error
		skeletonBytes, marshalErr = plan.Marshal(skeleton)
		if marshalErr != nil {
			return traceErr(fmt.Errorf("intent: marshalling skeleton: %w", marshalErr))
		}

		unfedInputs = UnfedInputs(req.Graph, chainResult)

		if trace != nil {
			trace.Skeleton = &SkeletonTrace{
				Plan:        skeleton,
				YAML:        string(skeletonBytes),
				UnfedInputs: unfedInputs,
				DurationMs:  time.Since(skelStart).Milliseconds(),
			}
		}

		chainContext = FormatChainResult(chainResult, req.Graph)
	}

	// --- Call 2: Fill Skeleton ---
	var domainContext string
	if req.KB != nil {
		domainContext = req.KB.FormatForPrompt()
	}

	system, user := buildPlanPrompt(string(skeletonBytes), unfedInputs, chainContext, domainContext, goalJSON, req.Prompt, now)

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

	// --- Parse and Merge ---
	yamlBytes, err := ExtractYAML(planResp.Content)
	if err != nil {
		return traceErr(fmt.Errorf("intent: extracting YAML from LLM response: %w", err))
	}

	if trace != nil {
		trace.LLMPlanYAML = string(yamlBytes)
	}

	llmPlan, err := plan.Parse(yamlBytes)
	if err != nil {
		return traceErr(fmt.Errorf("intent: parsing generated plan: %w", err))
	}

	if useTemplateIDs {
		MergeLLMValuesWithIDs(skeleton, llmPlan)
	} else {
		MergeLLMValues(skeleton, llmPlan)
	}

	if trace != nil {
		// Snapshot the plan after merge but before post-processing.
		merged := copyPlanShallow(skeleton)
		trace.MergedPlan = merged
	}

	PostProcess(skeleton, req.Graph, chainResult, goalAnalysis, req.Prompt)

	if err := plan.Validate(skeleton, req.Graph); err != nil {
		var valErr *plan.ValidationError
		if !errors.As(err, &valErr) {
			// Not a structured validation error — don't retry.
			if trace != nil {
				trace.ValidationErr = err.Error()
				trace.FinalPlan = skeleton
			}
			return traceErr(fmt.Errorf("intent: validating generated plan: %w", err))
		}

		if trace != nil {
			trace.ValidationErr = err.Error()
		}

		// Attempt a single retry: blank broken fromSelection refs, re-run call 2.
		retryResult := retryPlanGeneration(ctx, req, skeleton, skeletonBytes,
			unfedInputs, chainContext, domainContext, goalJSON, valErr, trace, pipelineStart)
		if retryResult != nil {
			retryResult.GoalAnalysis = goalAnalysis
			retryResult.ChainResult = chainResult
			return retryResult, nil
		}

		// Retry failed or not applicable — return original error.
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
		Plan:         skeleton,
		GoalAnalysis: goalAnalysis,
		ChainResult:  chainResult,
		Trace:        trace,
	}, nil
}

// retryPlanGeneration attempts a single retry of plan generation (call 2)
// after a validation failure. It blanks broken fromSelection references,
// rebuilds the skeleton YAML, and re-runs the LLM call with validation
// errors in the prompt. Returns a successful InterpretResult or nil if
// retry also fails.
func retryPlanGeneration(
	ctx context.Context,
	req InterpretRequest,
	skeleton *plan.Plan,
	skeletonBytes []byte,
	unfedInputs []string,
	chainContext, domainContext, goalJSON string,
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
		// Nothing was blanked — retry is unlikely to help.
		return nil
	}

	// Rebuild skeleton YAML with blanked values.
	retrySkeletonBytes, err := plan.Marshal(retrySkeleton)
	if err != nil {
		return nil
	}

	// Add blanked inputs to the unfed list.
	retryUnfed := append([]string{}, unfedInputs...)
	for _, b := range blanked {
		retryUnfed = append(retryUnfed, b+" (was incorrectly wired, needs correct fromSelection)")
	}

	now := time.Now()
	system, user := buildRetryPrompt(
		string(retrySkeletonBytes), retryUnfed, chainContext, domainContext, goalJSON,
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

	// Parse and merge retry response.
	yamlBytes, err := ExtractYAML(planResp.Content)
	if err != nil {
		return nil
	}

	llmPlan, err := plan.Parse(yamlBytes)
	if err != nil {
		return nil
	}

	MergeLLMValues(retrySkeleton, llmPlan)

	// MergeLLMValues won't set fromSelection from the LLM (it treats it as
	// structural). For blanked inputs, explicitly restore fromSelection from
	// the LLM response so the corrected field names take effect.
	restoreBlankedFromSelections(retrySkeleton, llmPlan, blanked)

	PostProcess(retrySkeleton, req.Graph, nil, nil, req.Prompt)

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

// analyzeGoal performs the first LLM call to identify the goal node and
// classify constraints. Returns a goalResult with the parsed analysis,
// raw JSON, LLM response metadata, and prompt text.
func analyzeGoal(ctx context.Context, client llm.Client, graphContext, prompt string, g *graph.Graph, now time.Time) (*goalResult, error) {
	system, user := buildGoalPrompt(graphContext, prompt, g, now)

	resp, err := client.Complete(ctx, &llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: system},
			{Role: llm.RoleUser, Content: user},
		},
		Temperature: 0.1,
	})
	if err != nil {
		return &goalResult{System: system, User: user}, fmt.Errorf("LLM call failed: %w", err)
	}

	content := strings.TrimSpace(resp.Content)

	// Strip markdown JSON fencing if present
	content = stripJSONFencing(content)

	var ga GoalAnalysis
	if err := json.Unmarshal([]byte(content), &ga); err != nil {
		// Fallback: heuristic goal identification
		ga = heuristicGoalAnalysis(prompt, g)
		// Marshal for forwarding
		fallbackJSON, _ := json.Marshal(ga)
		return &goalResult{
			Analysis: &ga,
			GoalJSON: string(fallbackJSON),
			Response: resp,
			Fallback: true,
			System:   system,
			User:     user,
		}, nil
	}

	// Validate goal exists in graph
	if g.Nodes[ga.Goal] == nil {
		ga = heuristicGoalAnalysis(prompt, g)
		fallbackJSON, _ := json.Marshal(ga)
		return &goalResult{
			Analysis: &ga,
			GoalJSON: string(fallbackJSON),
			Response: resp,
			Fallback: true,
			System:   system,
			User:     user,
		}, nil
	}

	return &goalResult{
		Analysis: &ga,
		GoalJSON: content,
		Response: resp,
		Fallback: false,
		System:   system,
		User:     user,
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

// heuristicGoalAnalysis attempts to identify a goal node by matching
// keywords from the prompt against node names and descriptions.
// It prefers terminal nodes (nodes that are not consumed by other edges).
func heuristicGoalAnalysis(prompt string, g *graph.Graph) GoalAnalysis {
	promptLower := strings.ToLower(prompt)

	// Find terminal nodes (not source of any edge to another node)
	isSourceOf := map[string]bool{}
	for _, edge := range g.Edges {
		parts := strings.SplitN(edge.From, ".", 2)
		if len(parts) > 0 {
			isSourceOf[parts[0]] = true
		}
	}

	// Score each node
	type scored struct {
		name  string
		score int
	}
	var candidates []scored
	for name, node := range g.Nodes {
		score := 0
		nameLower := strings.ToLower(name)
		descLower := strings.ToLower(node.Description)

		// Check if prompt keywords appear in node name/description
		words := strings.Fields(promptLower)
		for _, w := range words {
			if len(w) < 3 {
				continue
			}
			if strings.Contains(nameLower, w) {
				score += 3
			}
			if strings.Contains(descLower, w) {
				score += 1
			}
		}

		// Prefer terminal nodes (not a source for other nodes)
		if !isSourceOf[name] {
			score += 2
		}

		// Prefer nodes with cleanup (they're significant operations)
		if node.Cleanup != "" {
			score += 1
		}

		if score > 0 {
			candidates = append(candidates, scored{name, score})
		}
	}

	// Pick highest score; if tied, prefer lexicographically later (often the "commit" step)
	best := ""
	bestScore := 0
	for _, c := range candidates {
		if c.score > bestScore || (c.score == bestScore && c.name > best) {
			best = c.name
			bestScore = c.score
		}
	}

	// Fallback to any terminal node
	if best == "" {
		for name := range g.Nodes {
			if !isSourceOf[name] {
				best = name
				break
			}
		}
	}

	// Last resort: first node alphabetically
	if best == "" {
		for name := range g.Nodes {
			if best == "" || name < best {
				best = name
			}
		}
	}

	return GoalAnalysis{
		Goal:        best,
		Description: "Heuristic goal identification (LLM response was not parseable)",
	}
}
