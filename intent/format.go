package intent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gburgyan/aat/graph"
)

// FormatGraph serializes a Graph into structured Markdown text suitable for
// inclusion in LLM prompts. Output is deterministic (sorted keys).
func FormatGraph(g *graph.Graph) string {
	var b strings.Builder

	// Title
	title := "API Graph"
	if g.Title != "" {
		title = g.Title
	}
	fmt.Fprintf(&b, "# %s (version %s)\n\n", title, g.Version)

	// Description
	if g.Description != "" {
		b.WriteString(strings.TrimRight(g.Description, "\n"))
		b.WriteString("\n\n")
	}

	// Workflows
	if len(g.Workflows) > 0 {
		b.WriteString("## Workflows\n\n")
		for _, wf := range g.Workflows {
			fmt.Fprintf(&b, "- **%s**", wf.Name)
			if wf.IsAddon() {
				b.WriteString(" [addon]")
			}
			if wf.Template != "" {
				b.WriteString(" [template]")
			}
			if wf.Description != "" {
				fmt.Fprintf(&b, ": %s", wf.Description)
			}
			b.WriteString("\n")
			if wf.After != "" {
				fmt.Fprintf(&b, "  After: %s\n", wf.After)
			}
			if len(wf.Wire) > 0 {
				b.WriteString("  Wire:")
				for k, v := range wf.Wire {
					fmt.Fprintf(&b, " %s=%s", k, v)
				}
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	// Notes
	if g.Notes != "" {
		b.WriteString("## Notes\n\n")
		b.WriteString(strings.TrimRight(g.Notes, "\n"))
		b.WriteString("\n\n")
	}

	// Nodes in sorted order
	nodeNames := sortedNodeNames(g)
	for _, name := range nodeNames {
		node := g.Nodes[name]
		fmt.Fprintf(&b, "## Node: %s\n", name)
		fmt.Fprintf(&b, "%s\n", node.Description)
		if node.Adapter != "" {
			fmt.Fprintf(&b, "Adapter: %s\n", node.Adapter)
		}
		if len(node.Tags) > 0 {
			fmt.Fprintf(&b, "Tags: %s\n", strings.Join(node.Tags, ", "))
		}
		if node.Cleanup != "" {
			fmt.Fprintf(&b, "Cleanup: %s\n", node.Cleanup)
		}
		if node.CycleBreaker {
			b.WriteString("CycleBreaker: true\n")
		}
		if node.Preferred {
			b.WriteString("Preferred: true\n")
		}
		if len(node.Requires) > 0 {
			fmt.Fprintf(&b, "Requires: %s\n", strings.Join(node.Requires, ", "))
		}
		if len(node.Satisfies) > 0 {
			fmt.Fprintf(&b, "Satisfies: %s\n", strings.Join(node.Satisfies, ", "))
		}

		if len(node.Inputs) > 0 {
			b.WriteString("Inputs:\n")
			for _, inp := range node.Inputs {
				opt := ""
				if inp.Configurable {
					opt = " (configurable)"
				} else if inp.Optional {
					opt = " (optional)"
				}
				def := ""
				if inp.Default != nil && inp.Default.HasValue() {
					def = formatInputDefault(inp.Default)
				}
				desc := ""
				if inp.Description != "" {
					desc = " — " + inp.Description
				}
				constraint := formatConstraintAnnotation(inp.Constraints)
				fmt.Fprintf(&b, "  - %s: %s%s%s%s%s\n", inp.Name, inp.Type, constraint, opt, def, desc)
			}
		}

		if len(node.Outputs) > 0 {
			b.WriteString("Outputs:\n")
			for _, out := range node.Outputs {
				desc := ""
				if out.Description != "" {
					desc = " — " + out.Description
				}
				fmt.Fprintf(&b, "  - %s: %s%s\n", out.Name, out.Type, desc)
				for _, ef := range out.ElementFields {
					if ef.Path != "" && ef.Path != ef.Name {
						fmt.Fprintf(&b, "    - %s: %s (path: %s)\n", ef.Name, ef.Type, ef.Path)
					} else {
						fmt.Fprintf(&b, "    - %s: %s\n", ef.Name, ef.Type)
					}
				}
			}
		}
		b.WriteString("\n")
	}

	// Conditions
	if len(g.Conditions) > 0 {
		b.WriteString("## Conditions\n")
		for _, cond := range g.Conditions {
			fmt.Fprintf(&b, "- when: %s\n", cond.When)
			if len(cond.Require) > 0 {
				fmt.Fprintf(&b, "  require: %s\n", strings.Join(cond.Require, ", "))
			}
			if len(cond.Before) > 0 {
				fmt.Fprintf(&b, "  before: %s\n", strings.Join(cond.Before, ", "))
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

// formatConstraintAnnotation returns a compact annotation string for input constraints.
// Returns empty string if no constraints are set.
func formatConstraintAnnotation(c *graph.Constraint) string {
	if c == nil {
		return ""
	}
	var parts []string
	if c.Pattern != "" {
		parts = append(parts, "pattern: "+c.Pattern)
	}
	if c.Min != nil && c.Max != nil {
		parts = append(parts, fmt.Sprintf("range: %v..%v", *c.Min, *c.Max))
	} else if c.Min != nil {
		parts = append(parts, fmt.Sprintf("min: %v", *c.Min))
	} else if c.Max != nil {
		parts = append(parts, fmt.Sprintf("max: %v", *c.Max))
	}
	if c.MinLength != nil && c.MaxLength != nil {
		if *c.MinLength == *c.MaxLength {
			parts = append(parts, fmt.Sprintf("length: %d", *c.MinLength))
		} else {
			parts = append(parts, fmt.Sprintf("length: %d..%d", *c.MinLength, *c.MaxLength))
		}
	} else if c.MinLength != nil {
		parts = append(parts, fmt.Sprintf("minLength: %d", *c.MinLength))
	} else if c.MaxLength != nil {
		parts = append(parts, fmt.Sprintf("maxLength: %d", *c.MaxLength))
	}
	if c.Description != "" {
		parts = append(parts, c.Description)
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// FormatWorkflowMenu produces a compact menu of available workflows and addons
// suitable for the workflow selection LLM call. It includes only graph metadata
// and workflow descriptions — no nodes, inputs, outputs, or element fields.
// This reduces Call 1 token usage by ~88% compared to FormatGraph.
func FormatWorkflowMenu(g *graph.Graph) string {
	var b strings.Builder

	// Title + version.
	title := "API Graph"
	if g.Title != "" {
		title = g.Title
	}
	fmt.Fprintf(&b, "# %s (version %s)\n\n", title, g.Version)

	// Description.
	if g.Description != "" {
		b.WriteString(strings.TrimRight(g.Description, "\n"))
		b.WriteString("\n\n")
	}

	// Base workflows (non-addon, with templates).
	var hasBase bool
	for _, wf := range g.Workflows {
		if wf.IsAddon() || wf.Template == "" {
			continue
		}
		if !hasBase {
			b.WriteString("## Workflows\n\n")
			hasBase = true
		}
		desc := wf.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "- **%s**: %s\n", wf.Name, desc)
	}
	if hasBase {
		b.WriteString("\n")
	}

	// Addon workflows.
	var hasAddon bool
	for _, wf := range g.Workflows {
		if !wf.IsAddon() || wf.Template == "" {
			continue
		}
		if !hasAddon {
			b.WriteString("## Addons\n\n")
			hasAddon = true
		}
		desc := wf.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "- **%s** (splices after: %s): %s\n", wf.Name, wf.After, desc)
	}
	if hasAddon {
		b.WriteString("\n")
	}

	// Notes.
	if g.Notes != "" {
		b.WriteString("## Notes\n\n")
		b.WriteString(strings.TrimRight(g.Notes, "\n"))
		b.WriteString("\n\n")
	}

	return b.String()
}

// formatInputDefault renders a graph InputDefault as a compact annotation
// for LLM prompts. Literal values show as [default: v], pools as [pool: v1, v2, ...],
// from refs as [from: node.field].
func formatInputDefault(d *graph.InputDefault) string {
	if d == nil {
		return ""
	}
	if d.Value != nil {
		return fmt.Sprintf(" [default: %v]", d.Value)
	}
	if len(d.Pool) > 0 {
		var items []string
		for _, v := range d.Pool {
			items = append(items, fmt.Sprintf("%v", v))
		}
		if len(items) > 5 {
			items = append(items[:5], "...")
		}
		return fmt.Sprintf(" [pool: %s]", strings.Join(items, ", "))
	}
	if d.From != "" {
		return fmt.Sprintf(" [from: %s]", d.From)
	}
	if d.FromResolved != "" {
		return fmt.Sprintf(" [fromResolved: %s]", d.FromResolved)
	}
	return ""
}

// sortedNodeNames returns graph node names in sorted order.
func sortedNodeNames(g *graph.Graph) []string {
	names := make([]string, 0, len(g.Nodes))
	for name := range g.Nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
