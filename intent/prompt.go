package intent

import (
	"fmt"
	"strings"
	"time"

	"github.com/gburgyan/aat/graph"
)

// buildWorkflowSelectionPrompt constructs the messages for the first LLM call
// (workflow selection). The user prompt contains the compact workflow menu and
// the user's intent. The system prompt contains format instructions and rules.
// preSelectedLayers are layers already selected via CLI flags — the LLM should
// not re-select them but may add others.
func buildWorkflowSelectionPrompt(workflowMenu, userPrompt string, preSelectedLayers []string) (system, user string) {
	var sb strings.Builder

	// Task framing.
	sb.WriteString(`You are an API testing workflow classifier. Your task is to classify the user's testing intent and select exactly one workflow configuration.

## Output Format

Return ONLY valid JSON. Do not include markdown fencing, explanations, commentary, or additional fields.
{
  "workflow": "Exact Workflow Name",
  "description": "1-2 sentence summary of the test scenario",
  "choices": {"slotName": "Option Name"},
  "addons": ["Addon Name 1"],
  "layers": ["layer-name"]
}

## Decision Procedure

1. Identify the user's testing goal
2. Select the best matching workflow
3. Fill choice slots (or use defaults)
4. Include addons only if the user explicitly requests matching capabilities
5. Apply layers based on route, payment, cabin, or date clues
6. Return the JSON result

## Rules

- Select the workflow whose description best matches the user's intent
- Use the EXACT workflow name from the available list
- The "choices" object maps slot names to their chosen option for workflows with choice points. Use the EXACT option name from the slot's options list. Omit "choices" if the workflow has no slots, or to use all defaults.
- Include addons only when the user explicitly requests capabilities matching an addon's description (e.g., "add seat", "add baggage", "modify traveler"). Omit "addons" if no addons apply.
- The "layers" array lists data layers to apply. Select when the user's intent aligns with a layer's description. Use EXACT layer names. Omit "layers" if none apply.`)

	if len(preSelectedLayers) > 0 {
		sb.WriteString("\n- The following layers are ALREADY selected (do not re-select, but you may add others): ")
		sb.WriteString(strings.Join(preSelectedLayers, ", "))
	}

	system = sb.String()
	user = fmt.Sprintf("## Available Workflows\n\n%s\n## User Intent\n\n%s", workflowMenu, userPrompt)
	return system, user
}

// targetedPromptConfig holds options for buildTargetedPlanPrompt.
type targetedPromptConfig struct {
	suppressWrongPlan bool
	graph             *graph.Graph
	availableLayers   map[string]*graph.Layer
}

// targetedPromptOption is a functional option for buildTargetedPlanPrompt.
type targetedPromptOption func(*targetedPromptConfig)

// withSuppressWrongPlan omits the "Wrong Workflow" section from the system prompt.
func withSuppressWrongPlan() targetedPromptOption {
	return func(c *targetedPromptConfig) {
		c.suppressWrongPlan = true
	}
}

// withGraph provides the graph for enriching the Selected Configuration section.
func withGraph(g *graph.Graph) targetedPromptOption {
	return func(c *targetedPromptConfig) {
		c.graph = g
	}
}

// withAvailableLayers provides layers for enriching the Selected Configuration section.
func withAvailableLayers(layers map[string]*graph.Layer) targetedPromptOption {
	return func(c *targetedPromptConfig) {
		c.availableLayers = layers
	}
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
	opts ...targetedPromptOption,
) (system, user string) {
	var cfg targetedPromptConfig
	for _, o := range opts {
		o(&cfg)
	}
	dateStr := now.Format("2006-01-02")

	var sb strings.Builder
	sb.WriteString(`You are an API testing value generator. A workflow has been composed and all data flow is pre-wired. Your job is to pick concrete values for unfed inputs. Some inputs are handled by data layers — skip them unless the user's intent conflicts.

Return a JSON object (no markdown fencing):
{
  "values": {"stepID.inputName": "value"},
  "selections": {"stepID.selectionName": {"strategy": "match", "filter": "expr"}},
  "assertions": {"stepID": [{"type": "status", "expect": 200}]}
}
Include a category only if it contains entries.

Example — user says "search New York, cheapest nonstop":
{
  "values": {"search.origin": "NYC", "search.date": "{{today + 7 days}}"},
  "selections": {"search.results": {"strategy": "min", "sortField": "price", "filter": "stops == 0"}}
}

Today's date is `)
	sb.WriteString(dateStr)
	sb.WriteString(`. Date precedence: user prompt → template current value → default 7+ days in the future. Expression syntax: {{today + N days}}.

## Values

Provide a LITERAL value (string, number, or date expression) for each input in "Inputs That Need Values."
Pool inputs auto-select at runtime — include a pool input only when the user specifies a concrete value.
- Pick from sample values when provided; prefer simple, deterministic values
- Hard constraints must be met; soft constraints should be met
- Keep the "Current value:" unless the user's intent requires a change
- Values must be plain scalars — no object or reference syntax (no "from", "fromSelection", or "select")
- For date fields, use {{today + N days}} syntax

## Optional Configuration

These inputs have sensible defaults. Set a value only when the user explicitly or implicitly requests it; omit to use the default.

## Selections — optional overrides

Leave selections unchanged unless the user's intent requires an override:
- "cheapest" → strategy: min, sortField: price
- "no stops" → filter: "stops == 0"
- "by vendor X" → filter: "vendor == 'X'"

Valid strategies: first, last, random, index, min, max, match
  - match: requires a "filter" using only the listed element fields
  - min/max: requires a "sortField" from element fields
  - index: requires an "index" number
Filters may only reference fields in the element fields list. Search inputs already constrain results; selection filters are for additional preferences.

## Assertions

Add assertions only when the user says "verify that..." or "assert that...".
Valid types: status, fieldExists, fieldEquals, predicate.
Use bare field names (no jsonpath $ prefix).
`)

	if !cfg.suppressWrongPlan {
		sb.WriteString(`
## Wrong Workflow

If the composed workflow fundamentally doesn't match the user's intent, respond with only:
{"wrongPlan": {"reason": "explanation", "suggested": "Workflow Name"}}
Signal wrongPlan only for fundamental domain mismatches (e.g., user wants hotels but this is a flights workflow). Adapting values, pools, and layer data to match user intent is your normal job — that is not a workflow mismatch.
`)
	}

	system = sb.String()

	var ub strings.Builder

	classified := classifyInputs(inputContexts)
	writeRequiredInputsSection(&ub, classified.Required)
	writePoolInputsSection(&ub, classified.Pool)
	writeAutoWiredSection(&ub, classified.AutoWired)
	writeConfigurableSection(&ub, classified.Configurable)
	writeSelectionContextSection(&ub, selectionContexts)
	writeWorkflowConfigSection(&ub, ws, cfg.graph, cfg.availableLayers)
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
// Priority: auto-wired > layer-handled > pool > configurable > required.
// Auto-wired inputs (fromResolved) represent structural intent that takes
// priority over layer pools — e.g., a round-trip return leg is wired back
// to the origin regardless of what city pools the layer provides.
// An input that is both configurable and pool-backed lands in pool so the
// LLM gets the "omit unless user specifies" guard-rail.
func classifyInputs(contexts []InputContext) classifiedInputs {
	var c classifiedInputs
	for _, ic := range contexts {
		switch {
		case ic.FromResolved != "":
			c.AutoWired = append(c.AutoWired, ic)
		case ic.LayerHandled:
			c.LayerHandled = append(c.LayerHandled, ic)
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
	ub.WriteString("## Pool Inputs — auto-selected at runtime\n\n")
	ub.WriteString("These inputs have curated value pools that pick random values at runtime.\n")
	ub.WriteString("Include a pool input only when the user's prompt specifies a concrete value for it.\n")
	ub.WriteString("Precedence: user specifies a value → provide it; user is silent → omit (pool handles it).\n")
	ub.WriteString("Pool values below are reference samples for mapping user intent to valid values.\n\n")
	var lastNode string
	for _, ic := range inputs {
		lastNode = writeNodeGroupHeader(ub, ic, lastNode)
		writeInputContext(ub, ic)
	}
}

// writeAutoWiredSection writes the auto-wired inputs prompt section.
func writeAutoWiredSection(ub *strings.Builder, inputs []InputContext) {
	if len(inputs) == 0 {
		return
	}
	ub.WriteString("## Auto-Wired Inputs — derived at runtime\n\n")
	ub.WriteString("These inputs are automatically derived from sibling inputs at runtime.\n")
	ub.WriteString("Include one only when the user's intent requires a value different from what auto-wiring produces.\n\n")
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
// When g is provided, it enriches choices, addons, and layers with descriptions
// from the graph and available layers.
func writeWorkflowConfigSection(ub *strings.Builder, ws *WorkflowSelection, g *graph.Graph, availableLayers map[string]*graph.Layer) {
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
		ub.WriteString("Choices:\n")
		for slot, option := range ws.Choices {
			slotDesc := lookupSlotDescription(g, ws.Workflow, slot)
			optDesc := lookupOptionDescription(g, option)
			switch {
			case slotDesc != "" && optDesc != "":
				fmt.Fprintf(ub, "- %s (%s) → %s: %s\n", slot, slotDesc, option, optDesc)
			case slotDesc != "":
				fmt.Fprintf(ub, "- %s (%s) → %s\n", slot, slotDesc, option)
			case optDesc != "":
				fmt.Fprintf(ub, "- %s → %s: %s\n", slot, option, optDesc)
			default:
				fmt.Fprintf(ub, "- %s → %s\n", slot, option)
			}
		}
	}
	if len(ws.Addons) > 0 {
		ub.WriteString("Addons:\n")
		for _, addon := range ws.Addons {
			desc := lookupWorkflowDescription(g, addon)
			if desc != "" {
				fmt.Fprintf(ub, "- %s: %s\n", addon, desc)
			} else {
				fmt.Fprintf(ub, "- %s\n", addon)
			}
		}
	}
	if len(ws.Layers) > 0 {
		ub.WriteString("Layers:\n")
		for _, layer := range ws.Layers {
			desc := lookupLayerDescription(availableLayers, layer)
			if desc != "" {
				fmt.Fprintf(ub, "- %s: %s\n", layer, desc)
			} else {
				fmt.Fprintf(ub, "- %s\n", layer)
			}
		}
	}
	ub.WriteString("\n")
}

// lookupSlotDescription finds the description for a slot definition in the base workflow.
func lookupSlotDescription(g *graph.Graph, workflowName, slotName string) string {
	if g == nil {
		return ""
	}
	wf, found := findWorkflowByName(g, workflowName)
	if !found {
		return ""
	}
	for _, sd := range wf.Slots {
		if sd.Name == slotName {
			return sd.Description
		}
	}
	return ""
}

// lookupOptionDescription finds the description for a slot option workflow.
func lookupOptionDescription(g *graph.Graph, optionName string) string {
	if g == nil {
		return ""
	}
	wf, found := findWorkflowByName(g, optionName)
	if !found {
		return ""
	}
	return wf.Description
}

// lookupWorkflowDescription finds the description for a workflow (addon or base).
func lookupWorkflowDescription(g *graph.Graph, name string) string {
	if g == nil {
		return ""
	}
	wf, found := findWorkflowByName(g, name)
	if !found {
		return ""
	}
	return wf.Description
}

// lookupLayerDescription finds the description for a layer.
func lookupLayerDescription(availableLayers map[string]*graph.Layer, name string) string {
	if availableLayers == nil {
		return ""
	}
	l, ok := availableLayers[name]
	if !ok || l == nil {
		return ""
	}
	return l.Description
}

// writeLayerHandledSection writes the layer-handled inputs prompt section.
func writeLayerHandledSection(ub *strings.Builder, inputs []InputContext) {
	if len(inputs) == 0 {
		return
	}
	ub.WriteString("## Layer-Handled Inputs — managed by data layers\n\n")
	ub.WriteString("These inputs have runtime overrides from data layers.\n")
	ub.WriteString("Include one only when the user's prompt specifies a concrete value for it.\n")
	ub.WriteString("Pool values below are reference samples for mapping user intent to valid values.\n\n")
	var lastNode string
	for _, ic := range inputs {
		lastNode = writeNodeGroupHeader(ub, ic, lastNode)
		writeInputContext(ub, ic)
	}
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
	opts ...targetedPromptOption,
) (system, user string) {
	system, user = buildTargetedPlanPrompt(inputContexts, selectionContexts, userPrompt, ws, now, opts...)

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
