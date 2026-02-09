package plan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gburgyan/aat/graph"
)

// FormatNarrative renders a plan as human-readable text for terminal display.
// The graph parameter is optional; when provided it enriches output with node descriptions.
func FormatNarrative(p *Plan, g *graph.Graph) string {
	var b strings.Builder

	// Header
	writeHeader(&b, p, g)

	// Steps
	writeSteps(&b, p, g)

	// Constraints
	writeConstraints(&b, p)

	// Verification
	writeVerification(&b, p)

	// Cleanup
	writeCleanup(&b, p)

	return b.String()
}

func writeHeader(b *strings.Builder, p *Plan, g *graph.Graph) {
	// Plan title: prefer intent.description, fallback to metadata.prompt
	title := p.Intent.Description
	if title == "" {
		title = p.Metadata.Prompt
	}
	if title != "" {
		fmt.Fprintf(b, "Plan: %s\n", title)
	}

	// Goal line
	if p.Intent.Goal != "" {
		goalLine := p.Intent.Goal
		if g != nil {
			if node, ok := g.Nodes[p.Intent.Goal]; ok && node.Description != "" {
				goalLine += " — " + node.Description
			}
		}
		fmt.Fprintf(b, "Goal: %s\n", goalLine)
	}

	// Metadata line
	var meta []string
	if !p.Metadata.Created.IsZero() {
		meta = append(meta, fmt.Sprintf("Generated: %s", p.Metadata.Created.Format("2006-01-02 15:04")))
	}
	if p.Metadata.GraphVersion != "" {
		meta = append(meta, fmt.Sprintf("Graph: %s", p.Metadata.GraphVersion))
	}
	if len(meta) > 0 {
		fmt.Fprintf(b, "%s\n", strings.Join(meta, "  |  "))
	}

	b.WriteString("\n")
}

func writeSteps(b *strings.Builder, p *Plan, g *graph.Graph) {
	n := len(p.Execution.Steps)
	fmt.Fprintf(b, "Steps (%d):\n", n)

	for i, step := range p.Execution.Steps {
		// Step header: number, node, optional [GOAL], description
		label := step.Node
		if step.IsGoal {
			label += " [GOAL]"
		}
		desc := step.Description
		if desc == "" && g != nil {
			if node, ok := g.Nodes[step.Node]; ok {
				desc = node.Description
			}
		}
		if desc != "" {
			fmt.Fprintf(b, "  %d. %s — %s\n", i+1, label, desc)
		} else {
			fmt.Fprintf(b, "  %d. %s\n", i+1, label)
		}

		// DependsOn
		if len(step.DependsOn) > 0 {
			fmt.Fprintf(b, "     Depends on: %s\n", strings.Join(step.DependsOn, ", "))
		}

		// Selections
		writeStepSelections(b, step)

		// Values
		writeStepValues(b, step)

		// Assertions
		writeAssertions(b, step.Assertions)

		// Expect failure
		if step.ExpectFailure != nil {
			statuses := make([]string, len(step.ExpectFailure.Status))
			for j, s := range step.ExpectFailure.Status {
				statuses[j] = fmt.Sprintf("%d", s)
			}
			fmt.Fprintf(b, "     Expect failure: status %s\n", strings.Join(statuses, ", "))
		}

		// Blank line between steps (but not after the last one)
		if i < n-1 {
			b.WriteString("\n")
		}
	}
}

func writeStepSelections(b *strings.Builder, step Step) {
	if len(step.Selections) == 0 {
		return
	}

	b.WriteString("     Selections:\n")

	// Sort keys for stable output
	keys := make([]string, 0, len(step.Selections))
	for k := range step.Selections {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		sel := step.Selections[k]
		strategy := sel.Strategy
		if strategy == "" {
			strategy = "first"
		}
		detail := fmt.Sprintf("from %s (strategy: %s", sel.From, strategy)
		if sel.Filter != "" {
			detail += fmt.Sprintf(", filter: %s", sel.Filter)
		}
		detail += ")"
		fmt.Fprintf(b, "       %s: %s\n", k, detail)
	}
}

func writeStepValues(b *strings.Builder, step Step) {
	if len(step.Values) == 0 {
		return
	}

	b.WriteString("     Values:\n")

	// Sort keys for stable output
	keys := make([]string, 0, len(step.Values))
	for k := range step.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := step.Values[k]
		if v.FromSelection != "" {
			fmt.Fprintf(b, "       %s: from selection %s\n", k, v.FromSelection)
		} else if v.From != "" {
			// From reference
			detail := fmt.Sprintf("from %s", v.From)
			if v.Select != nil {
				detail += fmt.Sprintf(" (select: %s", v.Select.Strategy)
				if v.Select.Filter != "" {
					detail += fmt.Sprintf(", filter: %s", v.Select.Filter)
				}
				if v.Select.Field != "" {
					detail += fmt.Sprintf(", field: %s", v.Select.Field)
				}
				detail += ")"
			}
			fmt.Fprintf(b, "       %s: %s\n", k, detail)
		} else if v.Default != nil {
			fmt.Fprintf(b, "       %s: %q\n", k, fmt.Sprintf("%v", v.Default))
		}
	}
}

func writeAssertions(b *strings.Builder, a *Assertions) {
	if a == nil {
		return
	}

	var parts []string
	for _, m := range a.Mechanical {
		parts = append(parts, formatMechanicalAssertion(m))
	}
	for _, s := range a.Semantic {
		parts = append(parts, fmt.Sprintf("semantic: %s", s))
	}
	if len(parts) > 0 {
		fmt.Fprintf(b, "     Assertions: %s\n", strings.Join(parts, ", "))
	}
}

func formatMechanicalAssertion(m MechanicalAssertion) string {
	switch m.Type {
	case "status":
		return fmt.Sprintf("status %v", m.Expect)
	case "fieldExists":
		return fmt.Sprintf("fieldExists %s", m.Path)
	case "fieldEquals":
		return fmt.Sprintf("fieldEquals %s=%v", m.Path, m.Value)
	case "predicate":
		return fmt.Sprintf("predicate: %s", m.Expr)
	default:
		return m.Type
	}
}

func writeConstraints(b *strings.Builder, p *Plan) {
	c := p.Intent.Constraints
	if c == nil {
		return
	}
	if len(c.Hard) == 0 && len(c.Soft) == 0 && len(c.Free) == 0 {
		return
	}

	b.WriteString("\nConstraints:\n")
	for _, h := range c.Hard {
		desc := h.Name
		if h.Description != "" {
			desc += " — " + h.Description
		}
		fmt.Fprintf(b, "  Hard: %s\n", desc)
	}
	for _, s := range c.Soft {
		desc := s.Name
		if s.Description != "" {
			desc += " — " + s.Description
		}
		fmt.Fprintf(b, "  Soft: %s\n", desc)
	}
	if len(c.Free) > 0 {
		fmt.Fprintf(b, "  Free: %s\n", strings.Join(c.Free, ", "))
	}
}

func writeVerification(b *strings.Builder, p *Plan) {
	if len(p.Execution.Verification) == 0 {
		return
	}

	b.WriteString("\nVerification:\n")
	for _, v := range p.Execution.Verification {
		line := v.Node
		if v.Purpose != "" {
			line += " — " + v.Purpose
		}
		fmt.Fprintf(b, "  - %s\n", line)
	}
}

func writeCleanup(b *strings.Builder, p *Plan) {
	if len(p.Execution.Cleanup) == 0 {
		return
	}

	b.WriteString("\nCleanup:\n")
	for _, c := range p.Execution.Cleanup {
		runOn := c.RunOn
		if runOn == "" {
			runOn = "always"
		}
		fmt.Fprintf(b, "  - %s (%s)\n", c.Node, runOn)
	}
}
