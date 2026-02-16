package graph

import (
	"fmt"
	"sort"
	"strings"
)

// DocGenOptions controls documentation generation.
type DocGenOptions struct {
	Title    string              // Override default title (default: "API Workflow")
	NodeDocs map[string]string   // nodeName → user-written Markdown (from docs/nodes/*.md)
	Examples map[string][]string // "nodeName.inputName" → example values from domain
}

// GenerateDocs renders a Graph as a single Markdown document.
func GenerateDocs(g *Graph, opts *DocGenOptions) string {
	if opts == nil {
		opts = &DocGenOptions{}
	}

	var b strings.Builder

	title := opts.Title
	if title == "" && g.Title != "" {
		title = g.Title
	}
	if title == "" {
		title = "API Workflow"
	}

	// Header
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "%d nodes, %d edges", len(g.Nodes), len(g.Edges))
	if g.Version != "" {
		fmt.Fprintf(&b, " | Version %s", g.Version)
	}
	b.WriteString("\n\n")

	// Description
	if g.Description != "" {
		b.WriteString(strings.TrimRight(g.Description, "\n"))
		b.WriteString("\n\n")
	}

	// Mermaid diagram
	b.WriteString("## Workflow Diagram\n\n")
	b.WriteString("```mermaid\n")
	b.WriteString(GenerateMermaid(g))
	b.WriteString("```\n\n")

	// Workflows
	if len(g.Workflows) > 0 {
		b.WriteString("## Workflows\n\n")
		for _, wf := range g.Workflows {
			fmt.Fprintf(&b, "### %s\n\n", wf.Name)
			if wf.Description != "" {
				fmt.Fprintf(&b, "%s\n\n", wf.Description)
			}
			if wf.IsAddon() && wf.After != "" {
				fmt.Fprintf(&b, "**Addon:** splices after `%s`\n\n", wf.After)
			}
		}
	}

	// Entry points
	entryNodes := findEntryNodes(g)
	if len(entryNodes) > 0 {
		b.WriteString("## Entry Points\n\n")
		for _, name := range entryNodes {
			node := g.Nodes[name]
			desc := ""
			if node.Description != "" {
				desc = " — " + node.Description
			}
			fmt.Fprintf(&b, "- **%s**%s\n", name, desc)
		}
		b.WriteString("\n")
	}

	// Nodes in dependency order
	ordered := topoSortNodes(g)
	b.WriteString("## Nodes\n\n")
	for _, name := range ordered {
		node := g.Nodes[name]
		writeNodeSection(&b, name, node, g, opts)
	}

	// Data flow table
	writeDataFlowTable(&b, g)

	// Cleanup table
	writeCleanupTable(&b, g)

	// Notes
	if g.Notes != "" {
		b.WriteString("## Notes\n\n")
		b.WriteString(strings.TrimRight(g.Notes, "\n"))
		b.WriteString("\n\n")
	}

	return b.String()
}

// GenerateDocsSplit renders a Graph as multiple Markdown files.
// Returns a map of relative file paths to content.
func GenerateDocsSplit(g *Graph, opts *DocGenOptions) map[string]string {
	if opts == nil {
		opts = &DocGenOptions{}
	}

	result := make(map[string]string)

	title := opts.Title
	if title == "" && g.Title != "" {
		title = g.Title
	}
	if title == "" {
		title = "API Workflow"
	}

	// Build index.md
	var idx strings.Builder
	fmt.Fprintf(&idx, "# %s\n\n", title)
	fmt.Fprintf(&idx, "%d nodes, %d edges", len(g.Nodes), len(g.Edges))
	if g.Version != "" {
		fmt.Fprintf(&idx, " | Version %s", g.Version)
	}
	idx.WriteString("\n\n")

	// Description
	if g.Description != "" {
		idx.WriteString(strings.TrimRight(g.Description, "\n"))
		idx.WriteString("\n\n")
	}

	// Mermaid diagram
	idx.WriteString("## Workflow Diagram\n\n")
	idx.WriteString("```mermaid\n")
	idx.WriteString(GenerateMermaid(g))
	idx.WriteString("```\n\n")

	// Workflows
	if len(g.Workflows) > 0 {
		idx.WriteString("## Workflows\n\n")
		for _, wf := range g.Workflows {
			fmt.Fprintf(&idx, "### %s\n\n", wf.Name)
			if wf.Description != "" {
				fmt.Fprintf(&idx, "%s\n\n", wf.Description)
			}
			if wf.IsAddon() && wf.After != "" {
				fmt.Fprintf(&idx, "**Addon:** splices after `%s`\n\n", wf.After)
			}
		}
	}

	// Entry points
	entryNodes := findEntryNodes(g)
	if len(entryNodes) > 0 {
		idx.WriteString("## Entry Points\n\n")
		for _, name := range entryNodes {
			node := g.Nodes[name]
			desc := ""
			if node.Description != "" {
				desc = " — " + node.Description
			}
			fmt.Fprintf(&idx, "- [**%s**](nodes/%s.md)%s\n", name, name, desc)
		}
		idx.WriteString("\n")
	}

	// Node summary table
	ordered := topoSortNodes(g)
	idx.WriteString("## Nodes\n\n")
	idx.WriteString("| Node | Description | Inputs | Outputs |\n")
	idx.WriteString("|------|-------------|--------|---------|\n")
	for _, name := range ordered {
		node := g.Nodes[name]
		desc := node.Description
		fmt.Fprintf(&idx, "| [%s](nodes/%s.md) | %s | %d | %d |\n",
			name, name, desc, len(node.Inputs), len(node.Outputs))
	}
	idx.WriteString("\n")

	// Data flow table
	writeDataFlowTable(&idx, g)

	// Cleanup table
	writeCleanupTable(&idx, g)

	// Notes
	if g.Notes != "" {
		idx.WriteString("## Notes\n\n")
		idx.WriteString(strings.TrimRight(g.Notes, "\n"))
		idx.WriteString("\n\n")
	}

	result["index.md"] = idx.String()

	// Per-node files
	for _, name := range ordered {
		node := g.Nodes[name]
		var nb strings.Builder
		writeNodeSection(&nb, name, node, g, opts)
		result["nodes/"+name+".md"] = nb.String()
	}

	return result
}

// writeNodeSection writes the Markdown for a single node.
func writeNodeSection(b *strings.Builder, name string, node *Node, g *Graph, opts *DocGenOptions) {
	fmt.Fprintf(b, "### %s\n\n", name)

	if node.Description != "" {
		fmt.Fprintf(b, "%s\n\n", node.Description)
	}

	if node.Adapter != "" {
		fmt.Fprintf(b, "**Adapter:** `%s`\n\n", node.Adapter)
	}

	// User-provided docs
	if opts.NodeDocs != nil {
		if doc, ok := opts.NodeDocs[name]; ok && doc != "" {
			b.WriteString(doc)
			if !strings.HasSuffix(doc, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}

	// Inputs table
	hasExamples := hasExamplesForNode(name, node, opts)
	hasConstraints := hasConstraintsForNode(node)
	if len(node.Inputs) > 0 {
		b.WriteString("**Inputs:**\n\n")
		header := "| Name | Type | Required | Default | Description"
		divider := "|------|------|----------|---------|-------------"
		if hasConstraints {
			header += " | Constraints"
			divider += "|------------"
		}
		if hasExamples {
			header += " | Examples"
			divider += "|----------"
		}
		header += " |\n"
		divider += "|\n"
		b.WriteString(header)
		b.WriteString(divider)
		for _, inp := range node.Inputs {
			required := "yes"
			if inp.Optional {
				required = "no"
			}
			def := ""
			if inp.Default != nil {
				def = fmt.Sprintf("%v", inp.Default)
			}
			fmt.Fprintf(b, "| %s | %s | %s | %s | %s",
				inp.Name, inp.Type, required, def, inp.Description)
			if hasConstraints {
				fmt.Fprintf(b, " | %s", formatConstraintCell(inp.Constraints))
			}
			if hasExamples {
				examples := ""
				if opts.Examples != nil {
					key := name + "." + inp.Name
					if vals, ok := opts.Examples[key]; ok && len(vals) > 0 {
						examples = strings.Join(vals, ", ")
					}
				}
				// Also check for enum types
				if examples == "" {
					ft, err := ParseFieldType(inp.Type)
					if err == nil && ft.Kind == TypeEnum {
						examples = strings.Join(ft.EnumValues, ", ")
					}
				}
				fmt.Fprintf(b, " | %s", examples)
			}
			b.WriteString(" |\n")
		}
		b.WriteString("\n")
	}

	// Outputs table
	if len(node.Outputs) > 0 {
		b.WriteString("**Outputs:**\n\n")
		b.WriteString("| Name | Type | Description |\n")
		b.WriteString("|------|------|-------------|\n")
		for _, out := range node.Outputs {
			fmt.Fprintf(b, "| %s | %s | %s |\n", out.Name, out.Type, out.Description)
			for _, ef := range out.ElementFields {
				path := ""
				if ef.Path != "" && ef.Path != ef.Name {
					path = fmt.Sprintf(" (path: `%s`)", ef.Path)
				}
				fmt.Fprintf(b, "| \u00a0\u00a0\u2514 %s | %s | elementField%s |\n",
					ef.Name, ef.Type, path)
			}
		}
		b.WriteString("\n")
	}

	// Connections
	outbound := collectOutboundNodes(name, g)
	if len(outbound) > 0 {
		fmt.Fprintf(b, "**Provides data to:** %s\n\n", strings.Join(outbound, ", "))
	}

	inbound := collectInboundNodes(name, g)
	if len(inbound) > 0 {
		fmt.Fprintf(b, "**Receives data from:** %s\n\n", strings.Join(inbound, ", "))
	}
}

// writeDataFlowTable writes the data flow section.
func writeDataFlowTable(b *strings.Builder, g *Graph) {
	if len(g.Edges) == 0 {
		return
	}
	b.WriteString("## Data Flow\n\n")
	b.WriteString("| From | To | Select |\n")
	b.WriteString("|------|----|--------|\n")
	for _, edge := range g.Edges {
		sel := ""
		if edge.Select {
			sel = "\u2713"
		}
		fmt.Fprintf(b, "| %s | %s | %s |\n", edge.From, edge.To, sel)
	}
	b.WriteString("\n")
}

// writeCleanupTable writes the cleanup section.
func writeCleanupTable(b *strings.Builder, g *Graph) {
	type cleanupEntry struct {
		node    string
		cleansUp string
		desc    string
	}
	var entries []cleanupEntry
	for _, name := range sortedKeys(g.Nodes) {
		node := g.Nodes[name]
		if node.Cleanup != "" {
			cleanupNode := g.Nodes[node.Cleanup]
			desc := ""
			if cleanupNode != nil {
				desc = cleanupNode.Description
			}
			entries = append(entries, cleanupEntry{
				node:    node.Cleanup,
				cleansUp: name,
				desc:    desc,
			})
		}
	}
	if len(entries) == 0 {
		return
	}
	b.WriteString("## Cleanup\n\n")
	b.WriteString("| Node | Cleans Up | Description |\n")
	b.WriteString("|------|-----------|-------------|\n")
	for _, e := range entries {
		fmt.Fprintf(b, "| %s | %s | %s |\n", e.node, e.cleansUp, e.desc)
	}
	b.WriteString("\n")
}

// findEntryNodes returns nodes with no inbound data edges (in-degree 0), sorted.
func findEntryNodes(g *Graph) []string {
	hasInbound := make(map[string]bool)
	for _, edge := range g.Edges {
		toNode, _, err := splitRef(edge.To)
		if err != nil {
			continue
		}
		hasInbound[toNode] = true
	}

	var entry []string
	for _, name := range sortedKeys(g.Nodes) {
		if !hasInbound[name] {
			entry = append(entry, name)
		}
	}
	return entry
}

// topoSortNodes returns graph nodes in dependency order using Kahn's algorithm.
// Falls back to alphabetical order if cycle detection fails.
func topoSortNodes(g *Graph) []string {
	nodes := make(map[string]bool)
	for name := range g.Nodes {
		nodes[name] = true
	}

	sorted, err := chainTopoSort(nodes, g.Edges, nil)
	if err != nil {
		// Fallback to alphabetical
		return sortedKeys(g.Nodes)
	}
	return sorted
}

// hasConstraintsForNode returns true if any input has constraints.
func hasConstraintsForNode(node *Node) bool {
	for _, inp := range node.Inputs {
		if inp.Constraints != nil {
			return true
		}
	}
	return false
}

// formatConstraintCell formats constraints for a Markdown table cell.
func formatConstraintCell(c *Constraint) string {
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

// hasExamplesForNode checks whether any input has example values
// (from domain enrichment or enum types).
func hasExamplesForNode(nodeName string, node *Node, opts *DocGenOptions) bool {
	for _, inp := range node.Inputs {
		if opts.Examples != nil {
			key := nodeName + "." + inp.Name
			if vals, ok := opts.Examples[key]; ok && len(vals) > 0 {
				return true
			}
		}
		// Check enum types
		ft, err := ParseFieldType(inp.Type)
		if err == nil && ft.Kind == TypeEnum {
			return true
		}
	}
	return false
}

// collectOutboundNodes returns unique sorted node names that receive data from nodeName.
func collectOutboundNodes(nodeName string, g *Graph) []string {
	seen := make(map[string]bool)
	var result []string
	for _, edge := range g.Edges {
		fromNode, _, err1 := splitRef(edge.From)
		toNode, _, err2 := splitRef(edge.To)
		if err1 != nil || err2 != nil {
			continue
		}
		if fromNode == nodeName && !seen[toNode] {
			seen[toNode] = true
			result = append(result, toNode)
		}
	}
	sort.Strings(result)
	return result
}

// collectInboundNodes returns unique sorted node names that provide data to nodeName.
func collectInboundNodes(nodeName string, g *Graph) []string {
	seen := make(map[string]bool)
	var result []string
	for _, edge := range g.Edges {
		fromNode, _, err1 := splitRef(edge.From)
		toNode, _, err2 := splitRef(edge.To)
		if err1 != nil || err2 != nil {
			continue
		}
		if toNode == nodeName && !seen[fromNode] {
			seen[fromNode] = true
			result = append(result, fromNode)
		}
	}
	sort.Strings(result)
	return result
}
