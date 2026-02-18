package intent

import (
	"fmt"
	"strings"
	"time"

	"github.com/gburgyan/aat/graph"
)

// buildWorkflowSelectionPrompt constructs the messages for the first LLM call
// (workflow selection). It lists available workflows and addons, and asks the
// LLM to select one and classify constraints.
func buildWorkflowSelectionPrompt(graphContext, userPrompt string, g *graph.Graph, now time.Time) (system, user string) {
	dateStr := now.Format("2006-01-02")

	var sb strings.Builder
	sb.WriteString(`You are an API testing assistant. Given an API graph and a user's testing intent, select the appropriate workflow and classify constraints.

Respond with a JSON object (no markdown fencing, just raw JSON):
{
  "workflow": "Exact Workflow Name",
  "description": "brief description of what will be tested",
  "addons": ["Addon Name 1"],
  "repetitions": {"nodeName": N},
  "constraints": {
    "hard": [{"name": "constraint name", "description": "why this must be met", "appliesTo": ["node.input"]}],
    "soft": [{"name": "preference name", "description": "why this is preferred", "appliesTo": ["node.input"]}],
    "free": ["aspects that can vary freely"]
  }
}

Rules:
- Select the workflow whose description best matches the user's intent
- Use the EXACT workflow name from the list below
- Hard constraints are explicit requirements that MUST be met (e.g., specific origin/destination)
- Soft constraints are preferences that SHOULD be met but can be relaxed (e.g., "cheapest", "nonstop")
- Free parameters are things the user didn't specify that can be filled with reasonable values
- The "addons" array lists addon workflows to compose into the main workflow. Include addons when the user mentions capabilities matching an addon (e.g., seat selection, ancillary services). Omit "addons" if no addons are needed.
- The "repetitions" field maps node names to how many times they should be repeated (e.g., {"addTraveler": 2} for two travelers). Omit if no nodes need repeating.
- Today's date is `)
	sb.WriteString(dateStr)
	sb.WriteString(`. When generating dates, use dates at least 7 days in the future.`)

	// List available base workflows.
	sb.WriteString("\n\nAvailable Workflows:\n")
	for _, wf := range g.Workflows {
		if wf.Template == "" || wf.IsAddon() {
			continue
		}
		desc := wf.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&sb, "- **%s**: %s\n", wf.Name, desc)
	}

	// List addon workflows.
	hasAddons := false
	for _, wf := range g.Workflows {
		if wf.IsAddon() && wf.Template != "" {
			if !hasAddons {
				sb.WriteString("\nAvailable Addons:\n")
				hasAddons = true
			}
			desc := wf.Description
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Fprintf(&sb, "- **%s** (splices after: %s): %s\n", wf.Name, wf.After, desc)
		}
	}

	system = sb.String()
	user = fmt.Sprintf("## API Graph\n\n%s\n## User Intent\n\n%s", graphContext, userPrompt)
	return system, user
}

// buildTargetedPlanPrompt constructs the messages for the targeted phase 2 LLM call.
// Instead of sending the full skeleton YAML and asking for a complete YAML response,
// it provides per-input context and asks for a flat JSON response with only the
// decisions the LLM needs to make: values, selection overrides, assertions, and descriptions.
//
// The prompt is deliberately narrow: it only includes the inputs that need values,
// selection contexts for optional overrides, and the user's intent. Plan structure
// (step ordering, deps, outputs, cleanup) is omitted because it doesn't help
// the LLM pick literal values and can confuse it into inventing wiring syntax.
func buildTargetedPlanPrompt(
	inputContexts []InputContext,
	selectionContexts []SelectionContext,
	userPrompt string,
	now time.Time,
) (system, user string) {
	dateStr := now.Format("2006-01-02")

	system = `You are an API testing value generator. A workflow has been composed and all data flow is pre-wired. Your ONLY job is to pick concrete values for unfed inputs.

Respond with a JSON object (no markdown fencing, just raw JSON):
{
  "values": {"stepID.inputName": "value"},
  "selections": {"stepID.selectionName": {"strategy": "match", "filter": "expr"}},
  "assertions": {"stepID": [{"type": "status", "expect": 200}]},
  "descriptions": {"stepID": "What this step does"}
}
Omit empty categories.

Today's date is ` + dateStr + `. Use dates at least 7 days in the future. Expression syntax: {{today + 30 days}}.

## Values

For each input listed below, provide a LITERAL value (string, number, or date expression).
- Pick from the sample values when provided (e.g., airport codes from the sample list)
- Hard constraints MUST be met; soft constraints SHOULD be met
- For date fields, use {{today + N days}} syntax
- If "Current value:" is shown, keep it unless the user's intent requires a different value
- DO NOT use "from", "fromSelection", "select", or any reference/object syntax — only plain scalars

## Selections — optional

Only override if the user's intent specifically suggests a preference (e.g., "cheapest", "nonstop").
Valid strategies: first, last, random, index, min, max, match
  - match: requires a "filter" using ONLY the listed element fields
  - min/max: requires a "sortField" from element fields
  - index: requires an "index" number

DO NOT filter on fields not in the element fields list.
DO NOT re-filter on values already constrained by search inputs (dates, prices — redundant and brittle).

## Assertions — only when explicitly requested

Do NOT add assertions unless the user explicitly says "verify that..." or "assert that...".
Valid types: status, fieldExists, fieldEquals, predicate.
Use bare field names (no jsonpath $ prefix).

## Descriptions

Add a brief description for each step explaining what it does in context.`

	var ub strings.Builder

	// Per-input context — the core of the prompt.
	if len(inputContexts) > 0 {
		ub.WriteString("## Inputs That Need Values\n\n")
		for _, ic := range inputContexts {
			fmt.Fprintf(&ub, "### %s.%s (%s)\n", ic.StepID, ic.InputName, ic.InputType)
			if ic.CurrentDefault != "" {
				fmt.Fprintf(&ub, "Current value: %s\n", ic.CurrentDefault)
			}
			if ic.DomainType != "" {
				fmt.Fprintf(&ub, "Domain: %s", ic.DomainType)
				if ic.Format != "" {
					fmt.Fprintf(&ub, " (%s)", ic.Format)
				}
				ub.WriteString("\n")
			}
			if len(ic.PoolValues) > 0 {
				fmt.Fprintf(&ub, "Sample values: %s\n", strings.Join(ic.PoolValues, ", "))
			}
			if ic.GraphConstr != "" {
				fmt.Fprintf(&ub, "Validation: %s\n", ic.GraphConstr)
			}
			if ic.IsDate {
				ub.WriteString("Date field — use {{today + N days}}\n")
			}
			for _, c := range ic.Constraints {
				fmt.Fprintf(&ub, "Constraint: %s\n", c)
			}
			ub.WriteString("\n")
		}
	}

	// Selection context.
	if len(selectionContexts) > 0 {
		ub.WriteString("## Selections (override if user intent suggests)\n\n")
		for _, sc := range selectionContexts {
			kind := "named"
			if !sc.IsNamed {
				kind = "inline"
			}
			fmt.Fprintf(&ub, "### %s.%s (%s, current: %s)\n", sc.StepID, sc.SelectionName, kind, sc.CurrentStrategy)
			fmt.Fprintf(&ub, "Source: %s\n", sc.Source)
			if len(sc.ElementFields) > 0 {
				fmt.Fprintf(&ub, "Available fields (ONLY these can be used in filters): %s\n", strings.Join(sc.ElementFields, ", "))
			}
			if len(sc.FeedsInto) > 0 {
				fmt.Fprintf(&ub, "Feeds into: %s\n", strings.Join(sc.FeedsInto, ", "))
			}
			ub.WriteString("\n")
		}
	}

	// User intent.
	ub.WriteString("## User Intent\n\n")
	ub.WriteString(userPrompt)
	ub.WriteString("\n")

	user = ub.String()
	return system, user
}

// buildTargetedRetryPrompt constructs prompts for retrying the targeted plan
// generation after validation failure.
func buildTargetedRetryPrompt(
	inputContexts []InputContext,
	selectionContexts []SelectionContext,
	userPrompt string,
	now time.Time,
	validationErrors []string,
	hints []string,
) (system, user string) {
	system, user = buildTargetedPlanPrompt(inputContexts, selectionContexts, userPrompt, now)

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
	sb.WriteString("\nFix these issues in your JSON response.")
	system = sb.String()

	return system, user
}
