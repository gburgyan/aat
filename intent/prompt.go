package intent

import (
	"fmt"
	"strings"
	"time"
)

// buildWorkflowSelectionPrompt constructs the messages for the first LLM call
// (workflow selection). The user prompt contains the compact workflow menu and
// the user's intent. The system prompt contains format instructions and rules.
func buildWorkflowSelectionPrompt(workflowMenu, userPrompt string, now time.Time) (system, user string) {
	dateStr := now.Format("2006-01-02")

	system = `You are an API testing assistant. Given available workflows and a user's testing intent, select the appropriate workflow.

Respond with a JSON object (no markdown fencing, just raw JSON):
{
  "workflow": "Exact Workflow Name",
  "description": "brief description of what will be tested",
  "choices": {"slotName": "Option Name"},
  "addons": ["Addon Name 1"],
  "repetitions": {"nodeName": N}
}

Rules:
- Select the workflow whose description best matches the user's intent
- Use the EXACT workflow name from the available list
- The "choices" object maps slot names to their chosen option for workflows with choice points. Use the EXACT option name from the slot's options list. Omit "choices" if the workflow has no slots, or to use all defaults.
- The "addons" array lists addon workflows to compose into the main workflow. Include addons when the user mentions capabilities matching an addon's description. Omit "addons" if no addons are needed.
- The "repetitions" field maps node names to how many times they should be repeated (e.g., {"addItem": 3} for three items). Omit if no nodes need repeating.
- Today's date is ` + dateStr + `. When generating dates, default to at least 7 days in the future or past depending on context. The user's prompt takes priority (e.g., "tomorrow" → {{today + 1 day}}).`

	user = fmt.Sprintf("## Available Workflows\n\n%s\n## User Intent\n\n%s", workflowMenu, userPrompt)
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
	workflowDescription string,
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

Today's date is ` + dateStr + `. Default to dates at least 7 days in the future or past depending on context. The user's prompt takes priority (e.g., "tomorrow" → {{today + 1 day}}). Expression syntax: {{today + 30 days}}.

## Values

For each input listed below, provide a LITERAL value (string, number, or date expression).
- Pick from the sample values when provided
- Hard constraints MUST be met; soft constraints SHOULD be met
- For date fields, use {{today + N days}} syntax
- If "Current value:" is shown, keep it unless the user's intent requires a different value
- DO NOT use "from", "fromSelection", "select", or any reference/object syntax — only plain scalars

## Optional Configuration

Some inputs have sensible defaults but can be overridden based on user intent.
- Set a value ONLY if the user explicitly or implicitly requested it
- Omit the key entirely to use the default
- When in doubt, omit — the default is designed for the general case

## Selections — optional overrides

Only add selection overrides when the user's intent explicitly calls for it:
- "cheapest" → strategy: min, sortField: price
- "no stops" → filter: "stops == 0"
- "by vendor X" → filter: "vendor == 'X'"

If the user doesn't mention preferences for how results should be selected, do NOT add any selection overrides. The template defaults are correct.

Valid strategies: first, last, random, index, min, max, match
  - match: requires a "filter" using ONLY the listed element fields
  - min/max: requires a "sortField" from element fields
  - index: requires an "index" number

DO NOT filter on fields not in the element fields list.
DO NOT re-filter on values already constrained by search inputs — redundant and brittle.

## Assertions — only when explicitly requested

Do NOT add assertions unless the user explicitly says "verify that..." or "assert that...".
Valid types: status, fieldExists, fieldEquals, predicate.
Use bare field names (no jsonpath $ prefix).

## Descriptions

Add a brief description for each step explaining what it does in context.

## Wrong Workflow

If the composed workflow clearly doesn't match the user's intent (e.g., a fundamental mismatch between what the user asked for and what this workflow does), respond with ONLY:
{"wrongPlan": {"reason": "explanation", "suggested": "Workflow Name"}}
Rules:
- Only signal this for fundamental domain mismatches (completely wrong workflow type)
- Do NOT signal wrongPlan because current/default values don't match — replacing those values is YOUR job
- A workflow with example values can be adapted to different scenarios — just set the values accordingly`

	var ub strings.Builder

	// Separate required vs pool vs auto-wired vs configurable inputs.
	// Priority: auto-wired > pool > configurable > required.
	// An input that is both configurable and pool-backed should land in
	// the pool section so the LLM gets the "omit unless user specifies"
	// guard-rail. Similarly, auto-wired inputs keep their wiring even
	// when configurable.
	var requiredInputs, poolInputs, autoWiredInputs, configurableInputs []InputContext
	for _, ic := range inputContexts {
		if ic.FromResolved != "" {
			autoWiredInputs = append(autoWiredInputs, ic)
		} else if ic.IsPoolInput {
			poolInputs = append(poolInputs, ic)
		} else if ic.IsConfigurable {
			configurableInputs = append(configurableInputs, ic)
		} else {
			requiredInputs = append(requiredInputs, ic)
		}
	}

	// Per-input context — required inputs.
	if len(requiredInputs) > 0 {
		ub.WriteString("## Inputs That Need Values\n\n")
		var lastNode string
		for _, ic := range requiredInputs {
			lastNode = writeNodeGroupHeader(&ub, ic, lastNode)
			writeInputContext(&ub, ic)
		}
	}

	// Pool inputs — override when user specifies.
	if len(poolInputs) > 0 {
		ub.WriteString("## Pool Inputs — override when user specifies\n\n")
		ub.WriteString("These inputs have curated value pools with randomized defaults at runtime.\n")
		ub.WriteString("When the user's intent specifies a value for one of these inputs, provide it.\n")
		ub.WriteString("When the user doesn't specify, omit to use the random pool default.\n\n")
		var lastNode string
		for _, ic := range poolInputs {
			lastNode = writeNodeGroupHeader(&ub, ic, lastNode)
			icCopy := ic
			icCopy.PoolValues = nil // Don't show concrete pool values — format/description suffices
			writeInputContext(&ub, icCopy)
		}
	}

	// Auto-wired inputs — fromResolved, overridable when user intent conflicts.
	if len(autoWiredInputs) > 0 {
		ub.WriteString("## Auto-Wired Inputs — override only when user intent conflicts\n\n")
		ub.WriteString("These inputs are automatically derived from sibling inputs at runtime.\n")
		ub.WriteString("Do NOT provide values for these UNLESS the user's intent explicitly requires\n")
		ub.WriteString("a different value than what the wiring would produce.\n\n")
		var lastNode string
		for _, ic := range autoWiredInputs {
			lastNode = writeNodeGroupHeader(&ub, ic, lastNode)
			writeInputContext(&ub, ic)
		}
	}

	// Configurable inputs — separate section.
	if len(configurableInputs) > 0 {
		ub.WriteString("## Optional Configuration\n\n")
		var lastNode string
		for _, ic := range configurableInputs {
			lastNode = writeNodeGroupHeader(&ub, ic, lastNode)
			writeInputContext(&ub, ic)
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

	// Workflow context (from Call 1 description).
	if workflowDescription != "" {
		ub.WriteString("## Workflow Context\n\n")
		ub.WriteString(workflowDescription)
		ub.WriteString("\n\n")
	}

	// User intent.
	ub.WriteString("## User Intent\n\n")
	ub.WriteString(userPrompt)
	ub.WriteString("\n")

	user = ub.String()
	return system, user
}

// writeNodeGroupHeader emits a node description header when the node changes
// within a section. Returns the current node's StepID for tracking.
func writeNodeGroupHeader(ub *strings.Builder, ic InputContext, lastNode string) string {
	if ic.NodeDesc != "" && ic.StepID != lastNode {
		fmt.Fprintf(ub, "**%s** — %s\n\n", ic.StepID, ic.NodeDesc)
	}
	return ic.StepID
}

// writeInputContext writes the per-input prompt block for a single InputContext.
func writeInputContext(ub *strings.Builder, ic InputContext) {
	fmt.Fprintf(ub, "### %s.%s (%s)\n", ic.StepID, ic.InputName, ic.InputType)
	if ic.InputDescription != "" {
		fmt.Fprintf(ub, "Purpose: %s\n", ic.InputDescription)
	}
	if ic.FromResolved != "" {
		fmt.Fprintf(ub, "Auto-wired from: %s\n", ic.FromResolved)
	}
	if ic.CurrentDefault != "" {
		if ic.IsConfigurable {
			fmt.Fprintf(ub, "Default: %s (used if omitted)\n", ic.CurrentDefault)
		} else {
			fmt.Fprintf(ub, "Current value: %s\n", ic.CurrentDefault)
		}
	}
	if ic.DomainType != "" {
		fmt.Fprintf(ub, "Domain: %s", ic.DomainType)
		if ic.Format != "" {
			fmt.Fprintf(ub, " (%s)", ic.Format)
		}
		ub.WriteString("\n")
	}
	if len(ic.PoolValues) > 0 {
		if ic.HasTemplatePool {
			fmt.Fprintf(ub, "Pool (random at runtime): %s\n", strings.Join(ic.PoolValues, ", "))
		} else {
			fmt.Fprintf(ub, "Sample values: %s\n", strings.Join(ic.PoolValues, ", "))
		}
	}
	if ic.GraphConstr != "" {
		fmt.Fprintf(ub, "Validation: %s\n", ic.GraphConstr)
	}
	if ic.IsDate {
		ub.WriteString("Date field — use {{today + N days}}\n")
	}
	ub.WriteString("\n")
}

// buildTargetedRetryPrompt constructs prompts for retrying the targeted plan
// generation after validation failure.
func buildTargetedRetryPrompt(
	inputContexts []InputContext,
	selectionContexts []SelectionContext,
	userPrompt string,
	workflowDescription string,
	now time.Time,
	validationErrors []string,
	hints []string,
) (system, user string) {
	system, user = buildTargetedPlanPrompt(inputContexts, selectionContexts, userPrompt, workflowDescription, now)

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
