package graph

import (
	"fmt"
	"strings"
)

// GenerateMermaid renders a Graph as a Mermaid flowchart diagram.
// The output uses top-down layout (graph TD) with:
//   - Node labels showing name and truncated description
//   - Solid arrows for data flow edges
//   - Dashed arrows for cleanup edges
//   - Distinct styling for cleanup nodes
func GenerateMermaid(g *Graph) string {
	var b strings.Builder

	b.WriteString("graph TD\n")

	// Identify cleanup nodes (nodes that are targets of a cleanup reference)
	cleanupNodes := make(map[string]bool)
	for _, node := range g.Nodes {
		if node.Cleanup != "" {
			cleanupNodes[node.Cleanup] = true
		}
	}

	// Build node-to-node edge set to determine cleanup edges
	// A cleanup edge is from a node to its cleanup target
	cleanupEdges := make(map[string]string) // cleanupNode → parentNode
	for name, node := range g.Nodes {
		if node.Cleanup != "" {
			cleanupEdges[node.Cleanup] = name
		}
	}

	// Render nodes in sorted order for deterministic output
	names := sortedKeys(g.Nodes)
	for _, name := range names {
		node := g.Nodes[name]
		label := name
		if node.Description != "" {
			desc := truncateDescription(node.Description, 50)
			label = name + "<br/>" + desc
		}
		if cleanupNodes[name] {
			fmt.Fprintf(&b, "    %s[\"%s\"]:::cleanup\n", name, label)
		} else {
			fmt.Fprintf(&b, "    %s[\"%s\"]\n", name, label)
		}
	}

	b.WriteString("\n")

	// Collect unique node-to-node edges from data flow edges
	type edgeKey struct{ from, to string }
	seen := make(map[edgeKey]bool)

	for _, edge := range g.Edges {
		fromNode, _, err1 := splitRef(edge.From)
		toNode, _, err2 := splitRef(edge.To)
		if err1 != nil || err2 != nil {
			continue
		}
		key := edgeKey{fromNode, toNode}
		if seen[key] {
			continue
		}
		seen[key] = true

		// Check if this is an edge to a cleanup node from its parent
		if cleanupNodes[toNode] && cleanupEdges[toNode] == fromNode {
			fmt.Fprintf(&b, "    %s -.-> %s\n", fromNode, toNode)
		} else {
			fmt.Fprintf(&b, "    %s --> %s\n", fromNode, toNode)
		}
	}

	// Add cleanup style definition if there are cleanup nodes
	if len(cleanupNodes) > 0 {
		b.WriteString("\n    classDef cleanup fill:#fee,stroke:#c33,stroke-dasharray:5 5\n")
	}

	return b.String()
}

// truncateDescription shortens a description to maxLen characters,
// adding "..." if truncated.
func truncateDescription(desc string, maxLen int) string {
	if len(desc) <= maxLen {
		return desc
	}
	return desc[:maxLen-3] + "..."
}
