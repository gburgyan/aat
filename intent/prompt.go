package intent

import (
	"fmt"
	"strings"
	"time"
)

// buildGoalPrompt constructs the messages for the first LLM call (goal analysis).
// It provides graph structure and asks the LLM to identify the goal node and
// classify constraints.
func buildGoalPrompt(graphContext, userPrompt string, now time.Time) (system, user string) {
	dateStr := now.Format("2006-01-02")
	system = `You are an API testing assistant. Given an API graph and a user's testing intent, you must identify:
1. The goal node (the final API operation that achieves the user's intent)
2. Any conditions that should be active
3. Classification of constraints from the user's prompt

Respond with a JSON object (no markdown fencing, just raw JSON):
{
  "goal": "nodeName",
  "description": "brief description of what will be tested",
  "conditionContext": {"key": "value"},
  "pathPreferences": {"nodeName": "rationale for preferring a specific upstream path"},
  "constraints": {
    "hard": [{"name": "constraint name", "description": "why this must be met", "appliesTo": ["node.input"]}],
    "soft": [{"name": "preference name", "description": "why this is preferred", "appliesTo": ["node.input"]}],
    "free": ["aspects that can vary freely"]
  }
}

Rules:
- The goal should be the terminal node that produces the user's desired outcome
- Hard constraints are explicit requirements that MUST be met (e.g., specific origin/destination)
- Soft constraints are preferences that SHOULD be met but can be relaxed (e.g., "cheapest", "nonstop")
- Free parameters are things the user didn't specify that can be filled with reasonable values
- conditionContext provides values for evaluating graph conditions (e.g., {"isRoundTrip": true})
- pathPreferences indicate when the user's intent suggests a specific path through the graph
- Today's date is ` + dateStr + `. When generating dates, use dates at least 7 days in the future.`

	user = fmt.Sprintf("## API Graph\n\n%s\n## User Intent\n\n%s", graphContext, userPrompt)
	return system, user
}

// buildPlanPrompt constructs the messages for the second LLM call (plan generation).
// It provides the pre-built skeleton YAML, lists which inputs need values, and asks
// the LLM to fill in literal values, optionally refine selection strategies, and
// add assertions.
func buildPlanPrompt(skeletonYAML string, unfedInputs []string, chainContext, domainContext, goalAnalysisJSON, userPrompt string, now time.Time) (system, user string) {
	dateStr := now.Format("2006-01-02")

	system = `You are an API testing plan generator. A pre-built plan skeleton with all data flow already wired is provided. Your job is to fill in literal values for inputs that need them, optionally refine selection strategies, and add assertions.

Rules:
- Return a complete plan YAML wrapped in a ` + "```yaml" + ` code block
- Keep ALL existing structure (nodes, dependsOn, from refs, select configs, cleanup, metadata, intent) exactly as-is
- Fill in literal values for the inputs listed as "NEEDS VALUE"
- You may override selection strategies (e.g., change "first" to "match" with a filter) if the user's intent suggests it
- Add mechanical assertions (status, fieldExists, fieldEquals, predicate) on steps, especially the goal step
- Add step descriptions if helpful
- Today's date is ` + dateStr + `. When generating dates, use dates at least 7 days in the future.
- For selection strategy overrides, valid strategies are: first, last, random, index, min, max, match
  - match: requires a "filter" predicate expression
  - min/max: requires a "sortField"
  - index: requires an "index" number`

	var ub strings.Builder

	ub.WriteString("## Plan Skeleton\n\n")
	ub.WriteString("```yaml\n")
	ub.WriteString(skeletonYAML)
	ub.WriteString("```\n\n")

	if len(unfedInputs) > 0 {
		ub.WriteString("## Inputs That Need Values\n\n")
		for _, inp := range unfedInputs {
			fmt.Fprintf(&ub, "- %s\n", inp)
		}
		ub.WriteString("\n")
	}

	ub.WriteString("## Execution Chain Context\n\n")
	ub.WriteString(chainContext)
	ub.WriteString("\n")

	if domainContext != "" {
		ub.WriteString("## Domain Knowledge\n\n")
		ub.WriteString(domainContext)
		ub.WriteString("\n")
	}

	ub.WriteString("## Goal Analysis\n\n")
	ub.WriteString(goalAnalysisJSON)
	ub.WriteString("\n\n")

	ub.WriteString("## User Intent\n\n")
	ub.WriteString(userPrompt)
	ub.WriteString("\n")

	user = ub.String()
	return system, user
}

// buildRetryPrompt constructs prompts for retrying plan generation after validation
// failure. It extends buildPlanPrompt with information about the validation errors
// and hints about correct elementField names.
func buildRetryPrompt(skeletonYAML string, unfedInputs []string, chainContext, domainContext, goalAnalysisJSON, userPrompt string, now time.Time, validationErrors []string, hints []string) (system, user string) {
	system, user = buildPlanPrompt(skeletonYAML, unfedInputs, chainContext, domainContext, goalAnalysisJSON, userPrompt, now)

	// Append validation error context to the system prompt.
	var sb strings.Builder
	sb.WriteString(system)
	sb.WriteString("\n\nIMPORTANT: The previous attempt produced a plan with these validation errors:\n")
	for _, e := range validationErrors {
		fmt.Fprintf(&sb, "- %s\n", e)
	}
	if len(hints) > 0 {
		sb.WriteString("\n")
		for _, h := range hints {
			fmt.Fprintf(&sb, "%s\n", h)
		}
	}
	sb.WriteString("\nFix these issues. For fromSelection references, use the correct elementField name.")
	system = sb.String()

	return system, user
}
