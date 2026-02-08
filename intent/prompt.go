package intent

import (
	"fmt"
	"strings"
)

// buildGoalPrompt constructs the messages for the first LLM call (goal analysis).
// It provides graph structure and asks the LLM to identify the goal node and
// classify constraints.
func buildGoalPrompt(graphContext, userPrompt string) (system, user string) {
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
- pathPreferences indicate when the user's intent suggests a specific path through the graph`

	user = fmt.Sprintf("## API Graph\n\n%s\n## User Intent\n\n%s", graphContext, userPrompt)
	return system, user
}

// buildPlanPrompt constructs the messages for the second LLM call (plan generation).
// It provides the pre-computed chain result, plan schema, and domain knowledge.
func buildPlanPrompt(planSchema, chainContext, domainContext, goalAnalysisJSON, userPrompt string) (system, user string) {
	var sb strings.Builder

	sb.WriteString(`You are an API testing plan generator. Given a pre-computed execution chain and domain knowledge, generate a complete plan YAML that fills in all values, selection strategies, and assertions.

`)
	sb.WriteString(planSchema)

	system = sb.String()

	var ub strings.Builder
	ub.WriteString("## Execution Chain\n\n")
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
	ub.WriteString("\n\n")

	ub.WriteString(`Generate a complete plan YAML wrapped in a ` + "```yaml" + ` code block. The plan must:
1. Include all nodes from the execution chain in dependency order
2. Set dependsOn based on data flow edges
3. Fill input values: use explicit values from the user's intent for hard constraints, reasonable defaults for free parameters
4. For inputs fed by select edges from array outputs, include a selection config with appropriate strategy
5. Mark the goal step with isGoal: true
6. Add cleanup steps for any node that has a Cleanup field
7. Include status assertions on the goal step at minimum
8. Set metadata.prompt to the user's original prompt`)

	user = ub.String()
	return system, user
}
