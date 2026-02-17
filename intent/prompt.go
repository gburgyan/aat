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

// buildPlanPrompt constructs the messages for the second LLM call (plan generation).
// It provides the pre-built skeleton YAML, lists which inputs need values, and asks
// the LLM to fill in literal values, optionally refine selection strategies, and
// add assertions.
func buildPlanPrompt(skeletonYAML string, unfedInputs []string, domainContext, selectionJSON, userPrompt string, now time.Time) (system, user string) {
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
  - index: requires an "index" number
- Use the "id" field on a step when the same graph node appears multiple times in the plan (e.g., multi-leg flights, multiple travelers). The id becomes the step's unique identifier; dependsOn and from references use step IDs (which default to the node name when id is omitted). Example:
  - id: search_leg1
    node: searchFlights
    values: {origin: MEL, destination: SYD, departureDate: "2026-03-01"}
  - id: search_leg2
    node: searchFlights
    dependsOn: [search_leg1]
    values: {origin: SYD, destination: BNE, departureDate: "2026-03-05"}`

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

	if domainContext != "" {
		ub.WriteString("## Domain Knowledge\n\n")
		ub.WriteString(domainContext)
		ub.WriteString("\n")
	}

	ub.WriteString("## Workflow Selection\n\n")
	ub.WriteString(selectionJSON)
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
func buildRetryPrompt(skeletonYAML string, unfedInputs []string, domainContext, selectionJSON, userPrompt string, now time.Time, validationErrors []string, hints []string) (system, user string) {
	system, user = buildPlanPrompt(skeletonYAML, unfedInputs, domainContext, selectionJSON, userPrompt, now)

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

// buildTargetedPlanPrompt constructs the messages for the targeted phase 2 LLM call.
// Instead of sending the full skeleton YAML and asking for a complete YAML response,
// it provides per-input context and asks for a flat JSON response with only the
// decisions the LLM needs to make: values, selection overrides, assertions, and descriptions.
func buildTargetedPlanPrompt(
	inputContexts []InputContext,
	selectionContexts []SelectionContext,
	planFlow string,
	selectionJSON string,
	userPrompt string,
	now time.Time,
) (system, user string) {
	dateStr := now.Format("2006-01-02")

	system = `You are an API testing plan generator. A workflow has been composed and all data flow is pre-wired. Your job is to provide literal values for inputs, optionally override selection strategies, add assertions, and add step descriptions.

Respond with a JSON object (no markdown fencing, just raw JSON):
{
  "values": {
    "stepID.inputName": "value"
  },
  "selections": {
    "stepID.selectionName": {
      "strategy": "match",
      "filter": "predicate expression"
    }
  },
  "assertions": {
    "stepID": [
      {"type": "status", "expect": 200},
      {"type": "fieldExists", "path": "locator"}
    ]
  },
  "descriptions": {
    "stepID": "What this step does"
  }
}

Rules:
- Provide values for ALL inputs listed below — do not skip any
- Inputs that show "Current value:" have a template default. Override them if the user's intent requires different values; otherwise keep the current value.
- Values must be appropriate for the input type and any constraints listed
- Today's date is ` + dateStr + `. When generating dates, use dates at least 7 days in the future. You may use expression syntax like {{today + 30 days}} for relative dates.
- For selection overrides, valid strategies are: first, last, random, index, min, max, match
  - match: requires a "filter" predicate expression (field references are relative to the element, not qualified by selection name)
  - min/max: requires a "sortField"
  - index: requires an "index" number
- Only override selections if the user's intent suggests a specific preference
- Add mechanical assertions on steps — at minimum a status assertion on each step, and fieldExists/fieldEquals on the goal step
- Valid assertion types: status, fieldExists, fieldEquals, predicate
  - status: {"type": "status", "expect": 200}
  - fieldExists: {"type": "fieldExists", "path": "fieldName"} — path is a dot-separated field name (e.g., "pnrLocator", "price.amount"), NOT jsonpath (no $ prefix)
  - fieldEquals: {"type": "fieldEquals", "path": "fieldName", "value": "expected"}
  - predicate: {"type": "predicate", "expr": "expression"} — the expression goes in "expr" (not "expect"). Predicate syntax: bare identifiers (not $.field), comparison operators (==, !=, <, >, <=, >=), boolean operators (&&, ||, !), membership (field in ['a', 'b']), parentheses for grouping. Examples: status == 'CONFIRMED', price > 0, carrier in ['AA', 'UA']
- Do NOT use jsonpath $ prefix in field paths or predicate expressions — use bare field names like "locator" not "$.locator"
- Do NOT use functions like size(), all(), any() in predicates — they are not supported
- For fieldExists/fieldEquals/predicate assertions, use the output field names listed in the plan flow (e.g., "workbenchId", "pnrLocator") — these are the extracted response fields available at runtime
- Add descriptions to clarify what each step is doing in the context of the user's intent
- Omit empty categories (e.g., if no selection overrides, omit "selections")`

	var ub strings.Builder

	// Plan flow summary.
	ub.WriteString("## Plan Flow\n\n")
	ub.WriteString(planFlow)
	ub.WriteString("\n")

	// Per-input context.
	if len(inputContexts) > 0 {
		ub.WriteString("## Inputs That Need Values\n\n")
		for _, ic := range inputContexts {
			fmt.Fprintf(&ub, "### %s.%s (%s)\n", ic.StepID, ic.InputName, ic.InputType)
			fmt.Fprintf(&ub, "Node: %s\n", ic.NodeDesc)
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
				ub.WriteString("Note: This is a date field. Use {{today + N days}} for relative dates.\n")
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
				fmt.Fprintf(&ub, "Available fields: %s\n", strings.Join(sc.ElementFields, ", "))
			}
			if len(sc.FeedsInto) > 0 {
				fmt.Fprintf(&ub, "Feeds into: %s\n", strings.Join(sc.FeedsInto, ", "))
			}
			ub.WriteString("\n")
		}
	}

	// Workflow selection context.
	ub.WriteString("## Workflow Selection\n\n")
	ub.WriteString(selectionJSON)
	ub.WriteString("\n\n")

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
	planFlow string,
	selectionJSON string,
	userPrompt string,
	now time.Time,
	validationErrors []string,
	hints []string,
) (system, user string) {
	system, user = buildTargetedPlanPrompt(inputContexts, selectionContexts, planFlow, selectionJSON, userPrompt, now)

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
