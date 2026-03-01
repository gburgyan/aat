package intent

import (
	"fmt"
	"strings"
	"time"
)

// buildWorkflowSelectionPrompt constructs the messages for the first LLM call
// (workflow selection). The user prompt contains the compact workflow menu and
// the user's intent. The system prompt contains format instructions and rules.
// preSelectedLayers are layers already selected via CLI flags — the LLM should
// not re-select them but may add others.
func buildWorkflowSelectionPrompt(workflowMenu, userPrompt string, preSelectedLayers []string, now time.Time) (system, user string) {
	dateStr := now.Format("2006-01-02")

	system = `You are an API testing assistant. Given available workflows and a user's testing intent, select the appropriate workflow.

Respond with a JSON object (no markdown fencing, just raw JSON):
{
  "workflow": "Exact Workflow Name",
  "description": "brief description of what will be tested",
  "choices": {"slotName": "Option Name"},
  "addons": ["Addon Name 1"],
  "layers": ["layer-name"]
}

Rules:
- Select the workflow whose description best matches the user's intent
- Use the EXACT workflow name from the available list
- The "choices" object maps slot names to their chosen option for workflows with choice points. Use the EXACT option name from the slot's options list. Omit "choices" if the workflow has no slots, or to use all defaults.
- The "addons" array lists addon workflows to compose into the main workflow. Include addons when the user mentions capabilities matching an addon's description. Omit "addons" if no addons are needed.
- The "layers" array lists data layers to apply. Select when the user's intent aligns with a layer's description. Use EXACT layer names. Omit "layers" if no layers are needed.
- Today's date is ` + dateStr + `. When generating dates, default to at least 7 days in the future or past depending on context. The user's prompt takes priority (e.g., "tomorrow" → {{today + 1 day}}).`

	if len(preSelectedLayers) > 0 {
		system += "\n- The following layers are ALREADY selected (do not re-select, but you may add others): " + strings.Join(preSelectedLayers, ", ")
	}

	user = fmt.Sprintf("## Available Workflows\n\n%s\n## User Intent\n\n%s", workflowMenu, userPrompt)
	return system, user
}

// buildTargetedPlanPrompt constructs the messages for the targeted phase 2 LLM call.
// Instead of sending the full skeleton YAML and asking for a complete YAML response,
// it provides per-input context and asks for a flat JSON response with only the
// decisions the LLM needs to make: values, selection overrides, and assertions.
//
// The prompt is deliberately narrow: it only includes the inputs that need values,
// selection contexts for optional overrides, and the user's intent. Plan structure
// (step ordering, deps, outputs, cleanup) is omitted because it doesn't help
// the LLM pick literal values and can confuse it into inventing wiring syntax.
func buildTargetedPlanPrompt(
	inputContexts []InputContext,
	selectionContexts []SelectionContext,
	userPrompt string,
	ws *WorkflowSelection,
	now time.Time,
) (system, user string) {
	dateStr := now.Format("2006-01-02")

	system = `You are an API testing value generator. A workflow has been composed and all data flow is pre-wired. Your ONLY job is to pick concrete values for unfed inputs. Some inputs are handled by data layers — skip them unless the user's intent conflicts.

Respond with a JSON object (no markdown fencing, just raw JSON):
{
  "values": {"stepID.inputName": "value"},
  "selections": {"stepID.selectionName": {"strategy": "match", "filter": "expr"}},
  "assertions": {"stepID": [{"type": "status", "expect": 200}]}
}
Omit empty categories.

Today's date is ` + dateStr + `. The user's prompt takes priority for dates (e.g., "tomorrow" → {{today + 1 day}}). Expression syntax: {{today + 30 days}}.

## Values

For inputs in the "Inputs That Need Values" section, provide a LITERAL value (string, number, or date expression).
For inputs in the "Pool Inputs" section, OMIT the key entirely unless the user explicitly specifies a value — pool inputs auto-select at runtime.
- Pick from the sample values when provided
- Hard constraints MUST be met; soft constraints SHOULD be met
- For required date fields (in "Inputs That Need Values"), default to at least 7 days in the future or past depending on context
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

## Wrong Workflow

If the composed workflow clearly doesn't match the user's intent (e.g., a fundamental mismatch between what the user asked for and what this workflow does), respond with ONLY:
{"wrongPlan": {"reason": "explanation", "suggested": "Workflow Name"}}
Rules:
- Only signal this for fundamental domain mismatches (completely wrong workflow type)
- Do NOT signal wrongPlan because current/default values don't match — replacing those values is YOUR job
- A workflow with example values can be adapted to different scenarios — just set the values accordingly`

	var ub strings.Builder

	classified := classifyInputs(inputContexts)
	writeRequiredInputsSection(&ub, classified.Required)
	writePoolInputsSection(&ub, classified.Pool)
	writeAutoWiredSection(&ub, classified.AutoWired)
	writeConfigurableSection(&ub, classified.Configurable)
	writeSelectionContextSection(&ub, selectionContexts)
	writeWorkflowConfigSection(&ub, ws)
	writeLayerHandledSection(&ub, classified.LayerHandled)

	// User's prompt.
	ub.WriteString("## User's Prompt\n\n")
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
		if ic.IsPoolInput {
			ub.WriteString("Date field — if providing a value, use {{today + N days}} syntax\n")
		} else {
			ub.WriteString("Date field — use {{today + N days}}\n")
		}
	}
	ub.WriteString("\n")
}

// classifiedInputs groups InputContexts by their category for prompt generation.
type classifiedInputs struct {
	Required     []InputContext
	Pool         []InputContext
	AutoWired    []InputContext
	Configurable []InputContext
	LayerHandled []InputContext
}

// classifyInputs separates inputs into categories for the targeted prompt.
// Priority: layer-handled > auto-wired > pool > configurable > required.
// An input that is both configurable and pool-backed lands in pool so the
// LLM gets the "omit unless user specifies" guard-rail.
func classifyInputs(contexts []InputContext) classifiedInputs {
	var c classifiedInputs
	for _, ic := range contexts {
		switch {
		case ic.LayerHandled:
			c.LayerHandled = append(c.LayerHandled, ic)
		case ic.FromResolved != "":
			c.AutoWired = append(c.AutoWired, ic)
		case ic.IsPoolInput:
			c.Pool = append(c.Pool, ic)
		case ic.IsConfigurable:
			c.Configurable = append(c.Configurable, ic)
		default:
			c.Required = append(c.Required, ic)
		}
	}
	return c
}

// writeRequiredInputsSection writes the "Inputs That Need Values" prompt section.
func writeRequiredInputsSection(ub *strings.Builder, inputs []InputContext) {
	if len(inputs) == 0 {
		return
	}
	ub.WriteString("## Inputs That Need Values\n\n")
	var lastNode string
	for _, ic := range inputs {
		lastNode = writeNodeGroupHeader(ub, ic, lastNode)
		writeInputContext(ub, ic)
	}
}

// writePoolInputsSection writes the pool inputs prompt section with guard-rails.
func writePoolInputsSection(ub *strings.Builder, inputs []InputContext) {
	if len(inputs) == 0 {
		return
	}
	ub.WriteString("## Pool Inputs — DO NOT provide values unless the user specifies\n\n")
	ub.WriteString("These inputs have curated value pools that automatically pick random values at runtime.\n")
	ub.WriteString("DO NOT include these in your \"values\" response unless the user's prompt EXPLICITLY mentions a specific value for that input.\n")
	ub.WriteString("- User specifies a concrete value (name, code, ID, date) → provide it\n")
	ub.WriteString("- User's prompt is silent about an input → OMIT it, the pool handles it\n")
	ub.WriteString("- User says \"next week\" for a date field → provide the date expression\n")
	ub.WriteString("- User gives no date preference → OMIT date inputs entirely\n")
	ub.WriteString("When in doubt, OMIT. The pool defaults are designed for exactly this case.\n\n")
	var lastNode string
	for _, ic := range inputs {
		lastNode = writeNodeGroupHeader(ub, ic, lastNode)
		icCopy := ic
		icCopy.PoolValues = nil // Don't show concrete pool values — format/description suffices
		writeInputContext(ub, icCopy)
	}
}

// writeAutoWiredSection writes the auto-wired inputs prompt section.
func writeAutoWiredSection(ub *strings.Builder, inputs []InputContext) {
	if len(inputs) == 0 {
		return
	}
	ub.WriteString("## Auto-Wired Inputs — override only when user intent conflicts\n\n")
	ub.WriteString("These inputs are automatically derived from sibling inputs at runtime.\n")
	ub.WriteString("Do NOT provide values for these UNLESS the user's intent explicitly requires\n")
	ub.WriteString("a different value than what the wiring would produce.\n\n")
	var lastNode string
	for _, ic := range inputs {
		lastNode = writeNodeGroupHeader(ub, ic, lastNode)
		writeInputContext(ub, ic)
	}
}

// writeConfigurableSection writes the optional configuration prompt section.
func writeConfigurableSection(ub *strings.Builder, inputs []InputContext) {
	if len(inputs) == 0 {
		return
	}
	ub.WriteString("## Optional Configuration\n\n")
	var lastNode string
	for _, ic := range inputs {
		lastNode = writeNodeGroupHeader(ub, ic, lastNode)
		writeInputContext(ub, ic)
	}
}

// writeSelectionContextSection writes the selection overrides prompt section.
func writeSelectionContextSection(ub *strings.Builder, selectionContexts []SelectionContext) {
	if len(selectionContexts) == 0 {
		return
	}
	ub.WriteString("## Selections (override if user intent suggests)\n\n")
	for _, sc := range selectionContexts {
		kind := "named"
		if !sc.IsNamed {
			kind = "inline"
		}
		fmt.Fprintf(ub, "### %s.%s (%s, current: %s)\n", sc.StepID, sc.SelectionName, kind, sc.CurrentStrategy)
		fmt.Fprintf(ub, "Source: %s\n", sc.Source)
		if len(sc.ElementFields) > 0 {
			fmt.Fprintf(ub, "Available fields (ONLY these can be used in filters): %s\n", strings.Join(sc.ElementFields, ", "))
		}
		if len(sc.FeedsInto) > 0 {
			fmt.Fprintf(ub, "Feeds into: %s\n", strings.Join(sc.FeedsInto, ", "))
		}
		ub.WriteString("\n")
	}
}

// writeWorkflowConfigSection writes the selected configuration prompt section.
func writeWorkflowConfigSection(ub *strings.Builder, ws *WorkflowSelection) {
	if ws == nil || (ws.Workflow == "" && ws.Description == "") {
		return
	}
	ub.WriteString("## Selected Configuration\n\n")
	if ws.Description != "" {
		fmt.Fprintf(ub, "Workflow: %s (%s)\n", ws.Workflow, ws.Description)
	} else {
		fmt.Fprintf(ub, "Workflow: %s\n", ws.Workflow)
	}
	if len(ws.Choices) > 0 {
		var choiceParts []string
		for slot, option := range ws.Choices {
			choiceParts = append(choiceParts, fmt.Sprintf("%s → %s", slot, option))
		}
		fmt.Fprintf(ub, "Choices: %s\n", strings.Join(choiceParts, ", "))
	}
	if len(ws.Addons) > 0 {
		fmt.Fprintf(ub, "Addons: %s\n", strings.Join(ws.Addons, ", "))
	}
	if len(ws.Layers) > 0 {
		fmt.Fprintf(ub, "Layers: %s\n", strings.Join(ws.Layers, ", "))
	}
	ub.WriteString("\n")
}

// writeLayerHandledSection writes the layer-handled inputs prompt section.
func writeLayerHandledSection(ub *strings.Builder, inputs []InputContext) {
	if len(inputs) == 0 {
		return
	}
	ub.WriteString("## Layer-Handled Inputs — skip unless user intent conflicts\n\n")
	ub.WriteString("These inputs have runtime overrides from data layers. Omit them unless the user explicitly requests different values.\n\n")
	for _, ic := range inputs {
		fmt.Fprintf(ub, "- %s.%s (%s)\n", ic.StepID, ic.InputName, ic.InputType)
	}
	ub.WriteString("\n")
}

// buildTargetedRetryPrompt constructs prompts for retrying the targeted plan
// generation after validation failure.
func buildTargetedRetryPrompt(
	inputContexts []InputContext,
	selectionContexts []SelectionContext,
	userPrompt string,
	ws *WorkflowSelection,
	now time.Time,
	validationErrors []string,
	hints []string,
) (system, user string) {
	system, user = buildTargetedPlanPrompt(inputContexts, selectionContexts, userPrompt, ws, now)

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
