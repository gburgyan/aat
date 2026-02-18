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
				if inp.Default != nil {
					def = fmt.Sprintf(" [default: %v]", inp.Default)
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

// FormatPlanSchema returns concise documentation for the LLM about how to
// fill in a skeleton plan. The LLM no longer generates structure (that's in
// the skeleton), it only provides values, strategy overrides, and assertions.
func FormatPlanSchema() string {
	return `# How to Fill the Plan Skeleton

The skeleton already has all structural elements wired: nodes, dependsOn, from refs,
select configs, named selections, cleanup, metadata, and intent. You only need to add:

## 1. Literal Values

For inputs that need values, set them as bare scalars:
` + "```yaml" + `
values:
  origin: "DEN"
  departureDate: "2026-06-15"
` + "```" + `

## 2. Selection Strategy Overrides

The skeleton uses named selections when multiple inputs share the same array source.
You may override the selection strategy:
` + "```yaml" + `
selections:
  offering:
    from: searchFlights.catalogOfferings
    strategy: match                          # override from "first"
    filter: "stops == 0"                     # add filter for match
` + "```" + `

For single-input selections, the skeleton uses old-style from+select:
` + "```yaml" + `
values:
  offeringId:
    from: searchFlights.catalogOfferings
    select:
      strategy: match
      field: id
      filter: "stops == 0"
` + "```" + `

Valid strategies: first, last, random, index, min, max, match
- match: requires "filter" (predicate expression)
- min/max: requires "sortField"
- index: requires "index" (integer)

## 3. Assertions

Add mechanical assertions to verify step results:
` + "```yaml" + `
assertions:
  mechanical:
    - type: status
      expect: 200
    - type: fieldExists
      path: "$.locator"
    - type: fieldEquals
      path: "$.response.field"
      value: "expected"
    - type: predicate
      expr: "response.price > 0"
` + "```" + `

## 4. Step Descriptions (optional)

` + "```yaml" + `
description: "Search for flights from DEN to SFO"
` + "```" + `

## 5. Negative Assertions (expectFailure)

When a step should fail (e.g., security testing, input validation), use expectFailure:
` + "```yaml" + `
expectFailure:
  status: [401, 403]
  description: "Unauthenticated request must be rejected"
` + "```" + `

The step PASSES if the response status matches any listed code.
The step FAILS if the response returns 2xx (security boundary not enforced).
Retry and relaxation are automatically disabled.
`
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

// sortedNodeNames returns graph node names in sorted order.
func sortedNodeNames(g *graph.Graph) []string {
	names := make([]string, 0, len(g.Nodes))
	for name := range g.Nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
