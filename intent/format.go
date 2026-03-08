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
			} else if wf.IsSlot() {
				b.WriteString(" [slot]")
			}
			if wf.Template != "" {
				b.WriteString(" [template]")
			}
			if wf.Description != "" {
				fmt.Fprintf(&b, ": %s", wf.Description)
			}
			b.WriteString("\n")
			if wf.After.IsSet() {
				fmt.Fprintf(&b, "  After: %s\n", wf.After.String())
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
		formatNodeSection(&b, name, g.Nodes[name])
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

// formatNodeSection writes the Markdown section for a single graph node.
func formatNodeSection(b *strings.Builder, name string, node *graph.Node) {
	fmt.Fprintf(b, "## Node: %s\n", name)
	fmt.Fprintf(b, "%s\n", node.Description)
	if node.Adapter != "" {
		fmt.Fprintf(b, "Adapter: %s\n", node.Adapter)
	}
	if len(node.Tags) > 0 {
		fmt.Fprintf(b, "Tags: %s\n", strings.Join(node.Tags, ", "))
	}
	if node.Cleanup != "" {
		fmt.Fprintf(b, "Cleanup: %s\n", node.Cleanup)
	}
	if node.CycleBreaker {
		b.WriteString("CycleBreaker: true\n")
	}
	if node.Preferred {
		b.WriteString("Preferred: true\n")
	}
	if len(node.Requires) > 0 {
		fmt.Fprintf(b, "Requires: %s\n", strings.Join(node.Requires, ", "))
	}
	if len(node.Satisfies) > 0 {
		fmt.Fprintf(b, "Satisfies: %s\n", strings.Join(node.Satisfies, ", "))
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
			fmt.Fprintf(b, "  - %s: %s%s%s%s%s\n", inp.Name, inp.Type, constraint, opt, def, desc)
		}
	}

	if len(node.Outputs) > 0 {
		b.WriteString("Outputs:\n")
		for _, out := range node.Outputs {
			desc := ""
			if out.Description != "" {
				desc = " — " + out.Description
			}
			fmt.Fprintf(b, "  - %s: %s%s\n", out.Name, out.Type, desc)
			for _, ef := range out.ElementFields {
				if ef.Path != "" && ef.Path != ef.Name {
					fmt.Fprintf(b, "    - %s: %s (path: %s)\n", ef.Name, ef.Type, ef.Path)
				} else {
					fmt.Fprintf(b, "    - %s: %s\n", ef.Name, ef.Type)
				}
			}
		}
	}
	b.WriteString("\n")
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
// If layers is non-nil and non-empty, a Layers section is appended after Addons.
func FormatWorkflowMenu(g *graph.Graph, layers map[string]*graph.Layer) string {
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

	formatBaseWorkflows(&b, g)
	formatAddonWorkflows(&b, g)
	formatLayersSection(&b, layers)
	formatExamplesSection(&b, g.Examples)

	// Notes.
	if g.Notes != "" {
		b.WriteString("## Notes\n\n")
		b.WriteString(strings.TrimRight(g.Notes, "\n"))
		b.WriteString("\n\n")
	}

	return b.String()
}

// formatBaseWorkflows writes the base (non-addon, non-slot) workflow section.
func formatBaseWorkflows(b *strings.Builder, g *graph.Graph) {
	var hasBase bool
	for _, wf := range g.Workflows {
		if wf.IsAddon() || wf.IsSlot() || wf.Template == "" || wf.Deprecated {
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
		if wf.SelectionHint != "" {
			fmt.Fprintf(b, "- **%s**: %s [Hint: %s]\n", wf.Name, desc, wf.SelectionHint)
		} else {
			fmt.Fprintf(b, "- **%s**: %s\n", wf.Name, desc)
		}

		// Render slot choices if any.
		if len(wf.Slots) > 0 {
			b.WriteString("  Choices:\n")
			var defaults []string
			for _, sd := range wf.Slots {
				sdDesc := sd.Description
				if sdDesc == "" {
					sdDesc = sd.Name
				}
				fmt.Fprintf(b, "    - %s: %s\n", sd.Name, sdDesc)
				for _, optName := range sd.Options {
					// Skip deprecated slot options.
					optWf := findWorkflowInList(g.Workflows, optName)
					if optWf != nil && optWf.Deprecated {
						continue
					}
					optDesc := "(no description)"
					if optWf != nil && optWf.Description != "" {
						optDesc = optWf.Description
					}
					defaultMarker := ""
					if sd.Default != "" && strings.EqualFold(sd.Default, optName) {
						defaultMarker = " (default)"
					}
					fmt.Fprintf(b, "      - %s: %s%s\n", optName, optDesc, defaultMarker)
				}
				if sd.Default != "" {
					defaults = append(defaults, fmt.Sprintf("%s=%s", sd.Name, sd.Default))
				}
			}
			if len(defaults) > 0 {
				fmt.Fprintf(b, "  Defaults: %s\n", strings.Join(defaults, ", "))
			}
		}
	}
	if hasBase {
		b.WriteString("\n")
	}
}

// findWorkflowInList finds a workflow by name (case-insensitive) in a slice.
func findWorkflowInList(workflows []graph.Workflow, name string) *graph.Workflow {
	for i := range workflows {
		if strings.EqualFold(workflows[i].Name, name) {
			return &workflows[i]
		}
	}
	return nil
}

// formatAddonWorkflows writes the addon workflow section.
func formatAddonWorkflows(b *strings.Builder, g *graph.Graph) {
	var hasAddon bool
	for _, wf := range g.Workflows {
		if !wf.IsAddon() || wf.Template == "" || wf.Deprecated {
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
		if wf.SelectionHint != "" {
			fmt.Fprintf(b, "- **%s** (splices after: %s): %s [Hint: %s]\n", wf.Name, wf.After.String(), desc, wf.SelectionHint)
		} else {
			fmt.Fprintf(b, "- **%s** (splices after: %s): %s\n", wf.Name, wf.After.String(), desc)
		}
	}
	if hasAddon {
		b.WriteString("\n")
	}
}

// formatLayersSection writes the layers section if any layers are available.
func formatLayersSection(b *strings.Builder, layers map[string]*graph.Layer) {
	if len(layers) == 0 {
		return
	}
	b.WriteString("## Layers\n\n")
	b.WriteString("Layers override input defaults for test variation. Select when the user's intent aligns with a layer's purpose. Multiple layers can be combined.\n\n")

	// Sort layer names for deterministic output.
	layerNames := make([]string, 0, len(layers))
	for name := range layers {
		layerNames = append(layerNames, name)
	}
	sort.Strings(layerNames)

	for _, name := range layerNames {
		layer := layers[name]
		desc := layer.Description
		if desc == "" {
			desc = "(no description)"
		}
		if layer.SelectionHint != "" {
			fmt.Fprintf(b, "- **%s**: %s [Hint: %s]\n", name, desc, layer.SelectionHint)
		} else {
			fmt.Fprintf(b, "- **%s**: %s\n", name, desc)
		}
	}
	b.WriteString("\n")
}

// formatExamplesSection writes few-shot examples if any are defined on the graph.
func formatExamplesSection(b *strings.Builder, examples []graph.WorkflowExample) {
	if len(examples) == 0 {
		return
	}
	b.WriteString("## Examples\n\n")
	for _, ex := range examples {
		fmt.Fprintf(b, "Input: %s\nOutput: %s\n\n", ex.Input, ex.Output)
	}
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
