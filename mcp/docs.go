package mcp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gburgyan/aat/graph"
)

// loadNodeDocs reads Markdown files from docsDir and matches them to graph
// node names. Each file must be named <NodeName>.md (case-sensitive). If the
// directory does not exist, an empty map is returned with no error.
func loadNodeDocs(docsDir string, g *graph.Graph) (map[string]string, error) {
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}

	docs := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		nodeName := strings.TrimSuffix(name, ".md")
		if g.Nodes[nodeName] == nil {
			continue
		}
		content, err := os.ReadFile(filepath.Join(docsDir, name))
		if err != nil {
			return nil, err
		}
		docs[nodeName] = string(content)
	}
	return docs, nil
}

// mergeNodeDoc combines graph metadata, Markdown documentation, and an OAS
// summary into a single Markdown document for a node.
func mergeNodeDoc(node *graph.Node, g *graph.Graph, docContent string, oasSummary string) string {
	var b strings.Builder

	b.WriteString(formatNodeDetail(node, g))

	if docContent != "" {
		b.WriteString("\n## Documentation\n\n")
		b.WriteString(docContent)
		if !strings.HasSuffix(docContent, "\n") {
			b.WriteString("\n")
		}
	}

	if oasSummary != "" {
		b.WriteString("\n## API Specification\n\n")
		b.WriteString(oasSummary)
		if !strings.HasSuffix(oasSummary, "\n") {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// generateDocStub creates a Markdown skeleton for a node, suitable as a
// starting point for documentation. The output includes graph metadata,
// placeholder sections, and optionally an API details section.
func generateDocStub(node *graph.Node, g *graph.Graph, oasSummary string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", node.Name)

	if node.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", node.Description)
	} else {
		b.WriteString("_TODO: Add a description for this node._\n\n")
	}

	if len(node.Inputs) > 0 {
		b.WriteString("## Inputs\n\n")
		b.WriteString(formatInputTable(node.Inputs))
		b.WriteString("\n")
	}

	if len(node.Outputs) > 0 {
		b.WriteString("## Outputs\n\n")
		b.WriteString(formatOutputTable(node.Outputs))
		b.WriteString("\n")
	}

	if oasSummary != "" {
		b.WriteString("## API Details\n\n")
		b.WriteString(oasSummary)
		if !strings.HasSuffix(oasSummary, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## Overview\n\n")
	b.WriteString("_TODO: Describe what this node does and when to use it._\n\n")

	b.WriteString("## Usage Notes\n\n")
	b.WriteString("_TODO: Add any important usage notes, constraints, or prerequisites._\n\n")

	b.WriteString("## Examples\n\n")
	b.WriteString("_TODO: Add example inputs and expected outputs._\n")

	return b.String()
}
