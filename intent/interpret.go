package intent

import (
	"context"
	"encoding/json"
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
	Prompt string
	Graph  *graph.Graph
	KB     *domain.KnowledgeBase // may be nil
	Client llm.Client
}

// InterpretResult holds the outputs of prompt-to-plan transformation.
type InterpretResult struct {
	Plan         *plan.Plan
	GoalAnalysis *GoalAnalysis
	ChainResult  *graph.ChainResult
}

// GoalAnalysis captures the output of the first LLM call.
type GoalAnalysis struct {
	Goal             string            `json:"goal"`
	Description      string            `json:"description"`
	ConditionContext map[string]any    `json:"conditionContext"`
	PathPreferences  map[string]string `json:"pathPreferences"`
	Constraints      ConstraintSet     `json:"constraints"`
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

// Interpret transforms a natural language prompt into a validated execution plan.
// It uses a two-call LLM architecture:
//  1. Goal analysis: identify goal node and classify constraints
//  2. Value fill: given a deterministic skeleton, fill literal values and assertions
//
// Between calls, backward chaining computes the minimal subgraph and BuildSkeleton
// creates a structurally complete plan. The LLM only provides creative content
// (literal values, selection strategy overrides, assertions). MergeLLMValues
// combines the LLM output into the authoritative skeleton.
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

	now := time.Now()

	// --- Call 1: Goal Analysis ---
	graphContext := FormatGraph(req.Graph)
	goalAnalysis, goalJSON, err := analyzeGoal(ctx, req.Client, graphContext, req.Prompt, req.Graph, now)
	if err != nil {
		return nil, fmt.Errorf("intent: goal analysis: %w", err)
	}

	// --- Backward Chaining ---
	chainResult, err := graph.BackwardChain(req.Graph, graph.ChainOptions{
		Goals:            []string{goalAnalysis.Goal},
		ConditionContext: goalAnalysis.ConditionContext,
		EvalPredicate:    plan.EvalPredicate,
	})
	if err != nil {
		return nil, fmt.Errorf("intent: backward chaining: %w", err)
	}

	// --- Build Skeleton ---
	skeleton := BuildSkeleton(req.Graph, chainResult, goalAnalysis, req.Prompt, now)

	// Marshal skeleton to YAML for the LLM prompt.
	skeletonBytes, err := plan.Marshal(skeleton)
	if err != nil {
		return nil, fmt.Errorf("intent: marshalling skeleton: %w", err)
	}

	// Compute unfed inputs that need LLM values.
	unfedInputs := UnfedInputs(req.Graph, chainResult)

	// --- Call 2: Fill Skeleton ---
	chainContext := FormatChainResult(chainResult, req.Graph)

	var domainContext string
	if req.KB != nil {
		domainContext = req.KB.FormatForPrompt()
	}

	system, user := buildPlanPrompt(string(skeletonBytes), unfedInputs, chainContext, domainContext, goalJSON, req.Prompt, now)

	planResp, err := req.Client.Complete(ctx, &llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: system},
			{Role: llm.RoleUser, Content: user},
		},
		Temperature: 0.2,
	})
	if err != nil {
		return nil, fmt.Errorf("intent: plan generation LLM call: %w", err)
	}

	// --- Parse and Merge ---
	yamlBytes, err := ExtractYAML(planResp.Content)
	if err != nil {
		return nil, fmt.Errorf("intent: extracting YAML from LLM response: %w", err)
	}

	llmPlan, err := plan.Parse(yamlBytes)
	if err != nil {
		return nil, fmt.Errorf("intent: parsing generated plan: %w", err)
	}

	MergeLLMValues(skeleton, llmPlan)
	PostProcess(skeleton, req.Graph, chainResult, goalAnalysis, req.Prompt)

	if err := plan.Validate(skeleton, req.Graph); err != nil {
		return nil, fmt.Errorf("intent: validating generated plan: %w", err)
	}

	return &InterpretResult{
		Plan:         skeleton,
		GoalAnalysis: goalAnalysis,
		ChainResult:  chainResult,
	}, nil
}

// analyzeGoal performs the first LLM call to identify the goal node and
// classify constraints. Returns the parsed GoalAnalysis, the raw JSON string
// (for forwarding to the second call), and any error.
func analyzeGoal(ctx context.Context, client llm.Client, graphContext, prompt string, g *graph.Graph, now time.Time) (*GoalAnalysis, string, error) {
	system, user := buildGoalPrompt(graphContext, prompt, now)

	resp, err := client.Complete(ctx, &llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: system},
			{Role: llm.RoleUser, Content: user},
		},
		Temperature: 0.1,
	})
	if err != nil {
		return nil, "", fmt.Errorf("LLM call failed: %w", err)
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
		return &ga, string(fallbackJSON), nil
	}

	// Validate goal exists in graph
	if g.Nodes[ga.Goal] == nil {
		ga = heuristicGoalAnalysis(prompt, g)
		fallbackJSON, _ := json.Marshal(ga)
		return &ga, string(fallbackJSON), nil
	}

	return &ga, content, nil
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
