package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gburgyan/aat/graph"
)

// formatNodeSummary returns a one-line Markdown summary for a node.
func formatNodeSummary(node *graph.Node) string {
	desc := ""
	if node.Description != "" {
		desc = " — " + node.Description
	}
	tags := ""
	if len(node.Tags) > 0 {
		tags = " [" + strings.Join(node.Tags, ", ") + "]"
	}
	return fmt.Sprintf("**%s**%s (%d inputs, %d outputs)%s",
		node.Name, desc, len(node.Inputs), len(node.Outputs), tags)
}

// formatNodeDetail returns a full Markdown block describing a node and its
// connections within the graph.
func formatNodeDetail(node *graph.Node, g *graph.Graph) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", node.Name)

	if node.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", node.Description)
	}

	// Metadata
	if node.Adapter != "" {
		fmt.Fprintf(&b, "**Adapter:** %s\n", node.Adapter)
	}
	if node.Cleanup != "" {
		fmt.Fprintf(&b, "**Cleanup:** %s\n", node.Cleanup)
	}
	if node.CycleBreaker {
		b.WriteString("**CycleBreaker:** true\n")
	}
	if len(node.Tags) > 0 {
		fmt.Fprintf(&b, "**Tags:** %s\n", strings.Join(node.Tags, ", "))
	}
	if node.OAS != nil {
		fmt.Fprintf(&b, "**OAS:** operationId=%s", node.OAS.OperationID)
		if node.OAS.Spec != "" {
			fmt.Fprintf(&b, " (spec: %s)", node.OAS.Spec)
		}
		b.WriteString("\n")
	}

	// Inputs
	if len(node.Inputs) > 0 {
		b.WriteString("\n## Inputs\n\n")
		b.WriteString(formatInputTable(node.Inputs))
	}

	// Outputs
	if len(node.Outputs) > 0 {
		b.WriteString("\n## Outputs\n\n")
		b.WriteString(formatOutputTable(node.Outputs))
	}

	// Inbound edges (edges where To targets this node)
	inbound := collectInboundEdges(node.Name, g)
	if len(inbound) > 0 {
		b.WriteString("\n## Inbound Edges\n\n")
		b.WriteString(formatEdgeList(inbound))
	}

	// Outbound edges (edges where From references this node)
	outbound := collectOutboundEdges(node.Name, g)
	if len(outbound) > 0 {
		b.WriteString("\n## Outbound Edges\n\n")
		b.WriteString(formatEdgeList(outbound))
	}

	return b.String()
}

// formatInputTable returns a Markdown table for a slice of inputs.
func formatInputTable(inputs []graph.Input) string {
	if len(inputs) == 0 {
		return ""
	}

	hasConstraints := false
	for _, inp := range inputs {
		if inp.Constraints != nil {
			hasConstraints = true
			break
		}
	}

	var b strings.Builder
	if hasConstraints {
		b.WriteString("| Name | Type | Required | Default | Constraints | Description |\n")
		b.WriteString("|------|------|----------|---------|-------------|-------------|\n")
	} else {
		b.WriteString("| Name | Type | Required | Default | Description |\n")
		b.WriteString("|------|------|----------|---------|-------------|\n")
	}
	for _, inp := range inputs {
		required := "yes"
		if inp.Optional {
			required = "no"
		}
		def := ""
		if inp.Default != nil {
			def = fmt.Sprintf("%v", inp.Default)
		}
		desc := inp.Description
		if hasConstraints {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
				inp.Name, inp.Type, required, def, formatConstraintCell(inp.Constraints), desc)
		} else {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
				inp.Name, inp.Type, required, def, desc)
		}
	}
	return b.String()
}

// formatConstraintCell formats constraints for a Markdown table cell.
func formatConstraintCell(c *graph.Constraint) string {
	if c == nil {
		return ""
	}
	var parts []string
	if c.Pattern != "" {
		parts = append(parts, "`"+c.Pattern+"`")
	}
	if c.Min != nil && c.Max != nil {
		parts = append(parts, fmt.Sprintf("%v..%v", *c.Min, *c.Max))
	} else if c.Min != nil {
		parts = append(parts, fmt.Sprintf("min: %v", *c.Min))
	} else if c.Max != nil {
		parts = append(parts, fmt.Sprintf("max: %v", *c.Max))
	}
	if c.MinLength != nil && c.MaxLength != nil {
		if *c.MinLength == *c.MaxLength {
			parts = append(parts, fmt.Sprintf("len=%d", *c.MinLength))
		} else {
			parts = append(parts, fmt.Sprintf("len: %d..%d", *c.MinLength, *c.MaxLength))
		}
	} else if c.MinLength != nil {
		parts = append(parts, fmt.Sprintf("minLen: %d", *c.MinLength))
	} else if c.MaxLength != nil {
		parts = append(parts, fmt.Sprintf("maxLen: %d", *c.MaxLength))
	}
	if c.Description != "" {
		parts = append(parts, c.Description)
	}
	return strings.Join(parts, "; ")
}

// formatOutputTable returns a Markdown table for a slice of outputs,
// including elementField sub-items.
func formatOutputTable(outputs []graph.Output) string {
	if len(outputs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("| Name | Type | Description |\n")
	b.WriteString("|------|------|-------------|\n")
	for _, out := range outputs {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", out.Name, out.Type, out.Description)
		for _, ef := range out.ElementFields {
			path := ""
			if ef.Path != "" && ef.Path != ef.Name {
				path = fmt.Sprintf(" (path: %s)", ef.Path)
			}
			fmt.Fprintf(&b, "| \u00a0\u00a0\u2514 %s | %s | %s%s |\n",
				ef.Name, ef.Type, "elementField", path)
		}
	}
	return b.String()
}

// formatEdgeList returns a Markdown bullet list for a slice of edges.
func formatEdgeList(edges []graph.Edge) string {
	if len(edges) == 0 {
		return ""
	}
	var b strings.Builder
	for _, edge := range edges {
		annotations := ""
		if edge.Select {
			annotations += " [select]"
		}
		if edge.Preferred {
			annotations += " [preferred]"
		}
		fmt.Fprintf(&b, "- %s → %s%s\n", edge.From, edge.To, annotations)
	}
	return b.String()
}

// formatChainTrace formats a backward chain result for display as an MCP tool response.
func formatChainTrace(cr *graph.ChainResult, g *graph.Graph) string {
	var b strings.Builder

	b.WriteString("# Execution Chain\n\n")
	fmt.Fprintf(&b, "**Nodes (dependency order):** %s\n", strings.Join(cr.Nodes, " → "))
	fmt.Fprintf(&b, "**Entry nodes:** %s\n\n", strings.Join(cr.EntryNodes, ", "))

	// Node details in chain order
	for _, name := range cr.Nodes {
		node := g.Nodes[name]
		if node == nil {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n", name)
		if node.Description != "" {
			fmt.Fprintf(&b, "%s\n\n", node.Description)
		}
		if node.Cleanup != "" {
			fmt.Fprintf(&b, "**Cleanup:** %s\n\n", node.Cleanup)
		}

		if len(node.Inputs) > 0 {
			b.WriteString(formatInputTable(node.Inputs))
			b.WriteString("\n")
		}

		if len(node.Outputs) > 0 {
			b.WriteString(formatOutputTable(node.Outputs))
			b.WriteString("\n")
		}
	}

	// Data flow edges
	if len(cr.Edges) > 0 {
		b.WriteString("## Data Flow\n\n")
		b.WriteString(formatEdgeList(cr.Edges))
		b.WriteString("\n")
	}

	// Decisions
	if len(cr.Decisions) > 0 {
		b.WriteString("## Chain Decisions\n\n")
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

// collectInboundEdges returns edges where To targets the named node.
func collectInboundEdges(nodeName string, g *graph.Graph) []graph.Edge {
	var edges []graph.Edge
	for _, edge := range g.Edges {
		toNode := edgeRefNode(edge.To)
		if toNode == nodeName {
			edges = append(edges, edge)
		}
	}
	return edges
}

// collectOutboundEdges returns edges where From references the named node.
func collectOutboundEdges(nodeName string, g *graph.Graph) []graph.Edge {
	var edges []graph.Edge
	for _, edge := range g.Edges {
		fromNode := edgeRefNode(edge.From)
		if fromNode == nodeName {
			edges = append(edges, edge)
		}
	}
	return edges
}

// edgeRefNode extracts the node name from an edge reference like "node.output".
func edgeRefNode(ref string) string {
	if idx := strings.Index(ref, "."); idx >= 0 {
		return ref[:idx]
	}
	return ref
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
