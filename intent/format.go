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
					fmt.Fprintf(&b, "    - %s: %s\n", ef.Name, ef.Type)
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
					fmt.Fprintf(&b, "    - %s: %s\n", ef.Name, ef.Type)
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

// FormatPlanSchema returns documentation describing the plan YAML schema,
// suitable for inclusion in an LLM system prompt.
func FormatPlanSchema() string {
	return `# Plan YAML Schema

A plan describes an ordered sequence of API steps to execute.

## Top-level Structure

` + "```yaml" + `
metadata:
  prompt: "original user prompt"
  graphVersion: "1.0.0"
intent:
  goal: "node name that achieves the goal"
  description: "what the plan does"
  constraints:
    hard:
      - type: "constraint type"
        name: "constraint name"
        description: "why this is hard"
        applies_to: ["node.input"]
    soft:
      - type: "constraint type"
        name: "preference name"
        description: "why this is preferred"
        applies_to: ["node.input"]
    free: ["aspects that can vary freely"]
execution:
  steps:
    - node: nodeName
      dependsOn: [otherNode]
      description: "what this step does"
      isGoal: true  # mark the goal step
      values:
        inputName: "literal value"
        inputName:
          from: sourceNode.outputName
          select:
            strategy: first|last|random|min|max|match|index
            field: "extraction field path"
            filter: "predicate expression"
            sortField: "comparison field for min/max"
            index: 0
      assertions:
        mechanical:
          - type: status
            expect: 200
          - type: fieldExists
            path: "$.response.field"
          - type: fieldEquals
            path: "$.response.field"
            value: "expected"
          - type: predicate
            expr: "response.price > 0"
      retry:
        max: 2
        on: [transient, timeout]
  cleanup:
    - node: cleanupNodeName
      runOn: always
` + "```" + `

## Step Value Variants

A step value can be:
1. **Bare scalar**: ` + "`inputName: \"DEN\"`" + ` — sets the default value directly
2. **Mapping with from/select**: Routes data from a previous step's output
   - ` + "`from`" + `: ` + "`sourceNode.outputName`" + ` — which output to read
   - ` + "`select`" + `: selection config for array outputs

## Selection Strategies

- **first**: Take the first element
- **last**: Take the last element
- **random**: Pick a random element
- **index**: Pick element at specific index
- **min/max**: Pick element with smallest/largest value of ` + "`sortField`" + ` (or ` + "`field`" + `)
- **match**: Pick first element matching ` + "`filter`" + ` predicate

## Cleanup Steps

Cleanup runs after the main flow (success or failure). ` + "`runOn`" + ` can be:
- **always**: Run regardless of outcome (default)
- **failure**: Run only on failure
- **success**: Run only on success

## Example: Book a Flight

` + "```yaml" + `
execution:
  steps:
    - node: searchFlights
      values:
        origin: "DEN"
        destination: "SFO"
        departureDate: "2025-06-15"
    - node: createWorkbench
    - node: addOffer
      dependsOn: [searchFlights, createWorkbench]
      values:
        offeringId:
          from: searchFlights.catalogOfferings
          select:
            strategy: first
            field: offeringId
        productRef:
          from: searchFlights.catalogOfferings
          select:
            strategy: first
            field: offeringId
    - node: addTraveler
      dependsOn: [createWorkbench]
      values:
        surname: "Smith"
        givenName: "John"
    - node: commitBooking
      dependsOn: [addOffer, addTraveler]
      isGoal: true
      assertions:
        mechanical:
          - type: status
            expect: 200
          - type: fieldExists
            path: "$.locator"
  cleanup:
    - node: ignoreWorkbench
      runOn: always
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
