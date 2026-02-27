package intent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// FormatPlanSummary renders a user-focused summary of the LLM's decisions.
// It shows the workflow selection, compact step list, LLM-provided values,
// selection overrides, assertions, and cleanup — omitting internal wiring
// (from refs, dependsOn chains, fromSelection cross-references).
//
// ws and tr may be nil; missing sections are simply omitted.
func FormatPlanSummary(ws *WorkflowSelection, tr *TargetedResponse, p *plan.Plan, g *graph.Graph) string {
	var b strings.Builder

	// Header — plan title
	title := p.Intent.Description
	if title == "" {
		title = p.Metadata.Prompt
	}
	if title != "" {
		fmt.Fprintf(&b, "Plan: %s\n", title)
	}

	// Workflow section
	writeWorkflowSection(&b, ws)

	// Steps — compact numbered list
	writeSummarySteps(&b, p, g)

	// LLM-provided values
	writeSummaryValues(&b, tr, p)

	// Selection overrides
	writeSummarySelections(&b, tr, p)

	// Assertions
	writeSummaryAssertions(&b, tr, p)

	// Cleanup
	writeSummaryCleanup(&b, p)

	return b.String()
}

func writeWorkflowSection(b *strings.Builder, ws *WorkflowSelection) {
	if ws == nil || ws.Workflow == "" {
		return
	}

	fmt.Fprintf(b, "\nWorkflow: %s\n", ws.Workflow)

	if len(ws.Choices) > 0 {
		// Sort choices for deterministic output.
		keys := make([]string, 0, len(ws.Choices))
		for k := range ws.Choices {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var pairs []string
		for _, k := range keys {
			pairs = append(pairs, fmt.Sprintf("%s → %s", k, ws.Choices[k]))
		}
		fmt.Fprintf(b, "  Choices: %s\n", strings.Join(pairs, ", "))
	}

	if len(ws.Addons) > 0 {
		fmt.Fprintf(b, "  Addons: %s\n", strings.Join(ws.Addons, ", "))
	}

	if len(ws.Layers) > 0 {
		fmt.Fprintf(b, "  Layers: %s\n", strings.Join(ws.Layers, ", "))
	}
}

func writeSummarySteps(b *strings.Builder, p *plan.Plan, g *graph.Graph) {
	if len(p.Execution.Steps) == 0 {
		return
	}

	b.WriteString("\nSteps:\n")
	for i, step := range p.Execution.Steps {
		desc := step.Description
		if desc == "" && g != nil {
			if node, ok := g.Nodes[step.Node]; ok {
				desc = node.Description
			}
		}
		if desc != "" {
			fmt.Fprintf(b, "  %d. %s — %s\n", i+1, step.StepID(), desc)
		} else {
			fmt.Fprintf(b, "  %d. %s\n", i+1, step.StepID())
		}
	}
}

func writeSummaryValues(b *strings.Builder, tr *TargetedResponse, p *plan.Plan) {
	if tr == nil || len(tr.Values) == 0 {
		return
	}

	// Build step order index for sorting groups.
	stepOrder := make(map[string]int, len(p.Execution.Steps))
	for i, step := range p.Execution.Steps {
		stepOrder[step.StepID()] = i
	}

	// Group values by step.
	type valueEntry struct {
		input string
		value any
	}
	groups := map[string][]valueEntry{}

	for key, val := range tr.Values {
		stepID, inputName := splitStepInput(key)
		if inputName == "" {
			continue
		}
		groups[stepID] = append(groups[stepID], valueEntry{input: inputName, value: val})
	}

	if len(groups) == 0 {
		return
	}

	// Sort groups by step order.
	groupKeys := make([]string, 0, len(groups))
	for k := range groups {
		groupKeys = append(groupKeys, k)
	}
	sort.Slice(groupKeys, func(i, j int) bool {
		oi, oki := stepOrder[groupKeys[i]]
		oj, okj := stepOrder[groupKeys[j]]
		if oki && okj {
			return oi < oj
		}
		return groupKeys[i] < groupKeys[j]
	})

	b.WriteString("\nLLM-provided values:\n")
	for _, stepID := range groupKeys {
		entries := groups[stepID]
		// Sort inputs alphabetically within each group.
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].input < entries[j].input
		})

		fmt.Fprintf(b, "  %s:\n", stepID)
		for _, e := range entries {
			fmt.Fprintf(b, "    %s: %v\n", e.input, e.value)
		}
	}
}

func writeSummarySelections(b *strings.Builder, tr *TargetedResponse, p *plan.Plan) {
	if tr == nil || len(tr.Selections) == 0 {
		return
	}

	// Sort keys for deterministic output.
	keys := make([]string, 0, len(tr.Selections))
	for k := range tr.Selections {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.WriteString("\nSelection overrides:\n")
	for _, key := range keys {
		sel := tr.Selections[key]
		var parts []string
		if sel.Strategy != "" {
			parts = append(parts, fmt.Sprintf("strategy: %s", sel.Strategy))
		}
		if sel.Filter != "" {
			parts = append(parts, fmt.Sprintf("filter: %s", sel.Filter))
		}
		if sel.SortField != "" {
			parts = append(parts, fmt.Sprintf("sortField: %s", sel.SortField))
		}
		if sel.Prompt != "" {
			parts = append(parts, fmt.Sprintf("prompt: %q", sel.Prompt))
		}
		if sel.Index != 0 {
			parts = append(parts, fmt.Sprintf("index: %d", sel.Index))
		}
		if len(parts) > 0 {
			fmt.Fprintf(b, "  %s: %s\n", key, strings.Join(parts, ", "))
		} else {
			fmt.Fprintf(b, "  %s\n", key)
		}
	}
}

func writeSummaryAssertions(b *strings.Builder, tr *TargetedResponse, p *plan.Plan) {
	if tr == nil || len(tr.Assertions) == 0 {
		return
	}

	// Build step order for sorting.
	stepOrder := make(map[string]int, len(p.Execution.Steps))
	for i, step := range p.Execution.Steps {
		stepOrder[step.StepID()] = i
	}

	// Sort step keys by step order.
	keys := make([]string, 0, len(tr.Assertions))
	for k := range tr.Assertions {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		oi, oki := stepOrder[keys[i]]
		oj, okj := stepOrder[keys[j]]
		if oki && okj {
			return oi < oj
		}
		return keys[i] < keys[j]
	})

	b.WriteString("\nAssertions:\n")
	for _, stepID := range keys {
		assertions := tr.Assertions[stepID]
		if len(assertions) == 0 {
			continue
		}
		var parts []string
		for _, a := range assertions {
			parts = append(parts, formatTargetedAssertion(a))
		}
		fmt.Fprintf(b, "  %s: %s\n", stepID, strings.Join(parts, ", "))
	}
}

// formatTargetedAssertion renders a TargetedAssertion as a compact string.
func formatTargetedAssertion(a TargetedAssertion) string {
	switch a.Type {
	case "status":
		return fmt.Sprintf("status %v", a.Expect)
	case "fieldExists":
		return fmt.Sprintf("fieldExists %s", a.Path)
	case "fieldEquals":
		return fmt.Sprintf("fieldEquals %s=%v", a.Path, a.Value)
	case "predicate":
		return fmt.Sprintf("predicate: %s", a.Expr)
	default:
		return a.Type
	}
}

func writeSummaryCleanup(b *strings.Builder, p *plan.Plan) {
	if len(p.Execution.Cleanup) == 0 {
		return
	}

	var parts []string
	for _, c := range p.Execution.Cleanup {
		runOn := c.RunOn
		if runOn == "" {
			runOn = "always"
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", c.Node, runOn))
	}
	fmt.Fprintf(b, "\nCleanup: %s\n", strings.Join(parts, ", "))
}
