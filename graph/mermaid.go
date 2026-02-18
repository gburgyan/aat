package graph

import (
	"fmt"
	"strings"
)

// GenerateMermaid renders a Graph as a Mermaid flowchart diagram.
// The output uses top-down layout (graph TD) with:
//   - Node labels showing name and truncated description
//   - Solid arrows for requires/satisfies relationships
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

	// Draw arrows from requires/satisfies relationships
	type edgeKey struct{ from, to string }
	seen := make(map[edgeKey]bool)

	for _, name := range names {
		node := g.Nodes[name]
		for _, token := range node.Requires {
			for _, satisfier := range g.SatisfiersByToken[token] {
				key := edgeKey{satisfier, name}
				if seen[key] {
					continue
				}
				seen[key] = true
				fmt.Fprintf(&b, "    %s --> %s\n", satisfier, name)
			}
		}
	}

	// Draw cleanup dashed arrows
	for _, name := range names {
		node := g.Nodes[name]
		if node.Cleanup != "" {
			key := edgeKey{name, node.Cleanup}
			if !seen[key] {
				seen[key] = true
				fmt.Fprintf(&b, "    %s -.-> %s\n", name, node.Cleanup)
			}
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
