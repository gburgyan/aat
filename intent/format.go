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

	fmt.Fprintf(&b, "# API Graph (version %s)\n\n", g.Version)

	// Nodes in sorted order
	nodeNames := sortedNodeNames(g)
	for _, name := range nodeNames {
		node := g.Nodes[name]
		fmt.Fprintf(&b, "## Node: %s\n", name)
		fmt.Fprintf(&b, "%s\n", node.Description)
		if node.Adapter != "" {
			fmt.Fprintf(&b, "Adapter: %s\n", node.Adapter)
		}
		if node.Cleanup != "" {
			fmt.Fprintf(&b, "Cleanup: %s\n", node.Cleanup)
		}
		if node.CycleBreaker {
			b.WriteString("CycleBreaker: true\n")
		}

		if len(node.Inputs) > 0 {
			b.WriteString("Inputs:\n")
			for _, inp := range node.Inputs {
				opt := ""
				if inp.Optional {
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
				fmt.Fprintf(&b, "  - %s: %s%s%s%s\n", inp.Name, inp.Type, opt, def, desc)
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

	// Edges
	if len(g.Edges) > 0 {
		b.WriteString("## Edges\n")
		for _, edge := range g.Edges {
			sel := ""
			if edge.Select {
				sel = " [select]"
			}
			pref := ""
			if edge.Preferred {
				pref = " [preferred]"
			}
			fmt.Fprintf(&b, "- %s → %s%s%s\n", edge.From, edge.To, sel, pref)
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

// FormatChainResult serializes a ChainResult with its graph context into
// structured text showing only the relevant subgraph. Used for the second
// LLM call after backward chaining.
func FormatChainResult(cr *graph.ChainResult, g *graph.Graph) string {
	var b strings.Builder

	b.WriteString("# Execution Chain\n\n")
	fmt.Fprintf(&b, "Nodes (dependency order): %s\n", strings.Join(cr.Nodes, " → "))
	fmt.Fprintf(&b, "Entry nodes: %s\n\n", strings.Join(cr.EntryNodes, ", "))

	// Node details in chain order
	for _, name := range cr.Nodes {
		node := g.Nodes[name]
		if node == nil {
			continue
		}
		fmt.Fprintf(&b, "## Step: %s\n", name)
		fmt.Fprintf(&b, "%s\n", node.Description)
		if node.Cleanup != "" {
			fmt.Fprintf(&b, "Cleanup: %s\n", node.Cleanup)
		}

		if len(node.Inputs) > 0 {
			b.WriteString("Inputs:\n")
			for _, inp := range node.Inputs {
				opt := ""
				if inp.Optional {
					opt = " (optional)"
				}
				def := ""
				if inp.Default != nil {
					def = fmt.Sprintf(" [default: %v]", inp.Default)
				}
				fmt.Fprintf(&b, "  - %s: %s%s%s\n", inp.Name, inp.Type, opt, def)
			}
		}

		if len(node.Outputs) > 0 {
			b.WriteString("Outputs:\n")
			for _, out := range node.Outputs {
				fmt.Fprintf(&b, "  - %s: %s\n", out.Name, out.Type)
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

	// Chain edges
	if len(cr.Edges) > 0 {
		b.WriteString("## Data Flow\n")
		for _, edge := range cr.Edges {
			sel := ""
			if edge.Select {
				sel = " [select]"
			}
			fmt.Fprintf(&b, "- %s → %s%s\n", edge.From, edge.To, sel)
		}
		b.WriteString("\n")
	}

	// Decisions
	if len(cr.Decisions) > 0 {
		b.WriteString("## Chain Decisions\n")
		for _, d := range cr.Decisions {
			fmt.Fprintf(&b, "- %s\n", d.Detail)
			if len(d.Alternatives) > 0 {
				fmt.Fprintf(&b, "  Alternatives: %s\n", strings.Join(d.Alternatives, ", "))
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
`
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
