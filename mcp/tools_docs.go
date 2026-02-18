package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/gburgyan/aat/graph"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerDocsTools adds the documentation browsing and generation tools plus
// the enrich_documentation prompt to the MCP server.
func (s *Server) registerDocsTools() {
	s.mcp.AddTool(
		mcp.NewTool("get_node_documentation",
			mcp.WithDescription("Show merged documentation for a graph node: graph metadata, user-written Markdown docs, and OAS summary. Works even without docs — falls back to graph metadata only."),
			mcp.WithString("node",
				mcp.Description("Node name"),
				mcp.Required(),
			),
		),
		s.handleGetNodeDocumentation,
	)

	s.mcp.AddTool(
		mcp.NewTool("get_workflow_documentation",
			mcp.WithDescription("Show merged documentation for every node in a workflow chain, traced backward from the goal node."),
			mcp.WithString("goal",
				mcp.Description("Goal node name to trace backward from"),
				mcp.Required(),
			),
		),
		s.handleGetWorkflowDocumentation,
	)

	s.mcp.AddTool(
		mcp.NewTool("generate_doc_stub",
			mcp.WithDescription("Generate a Markdown documentation skeleton for a node, including graph metadata, input/output tables, and placeholder sections. Returns text — does not write to disk."),
			mcp.WithString("node",
				mcp.Description("Node name"),
				mcp.Required(),
			),
		),
		s.handleGenerateDocStub,
	)

	s.mcp.AddTool(
		mcp.NewTool("list_undocumented_nodes",
			mcp.WithDescription("List graph nodes that do not have a corresponding Markdown doc file. Shows which nodes need documentation."),
		),
		s.handleListUndocumentedNodes,
	)

	// Prompt: enrich_documentation
	s.mcp.AddPrompt(
		mcp.NewPrompt("enrich_documentation",
			mcp.WithPromptDescription("Enrich or create documentation for a graph node using graph metadata, existing docs, OAS details, and domain knowledge"),
			mcp.WithArgument("node",
				mcp.RequiredArgument(),
				mcp.ArgumentDescription("Graph node name to document"),
			),
		),
		s.handleEnrichDocumentation,
	)
}

// handleGetNodeDocumentation merges graph detail, user docs, and OAS summary.
func (s *Server) handleGetNodeDocumentation(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName, err := req.RequireString("node")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: node"), nil
	}

	node := s.ctx.Graph.Nodes[nodeName]
	if node == nil {
		return mcp.NewToolResultError(fmt.Sprintf("unknown node %q", nodeName)), nil
	}

	docContent := ""
	if s.ctx.NodeDocs != nil {
		docContent = s.ctx.NodeDocs[nodeName]
	}

	oasSummary := s.getOASSummary(nodeName)

	return mcp.NewToolResultText(mergeNodeDoc(node, s.ctx.Graph, docContent, oasSummary)), nil
}

// handleGetWorkflowDocumentation traces backward from a goal and returns
// merged documentation for each node in the chain.
func (s *Server) handleGetWorkflowDocumentation(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	goal, err := req.RequireString("goal")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: goal"), nil
	}

	if s.ctx.Graph.Nodes[goal] == nil {
		return mcp.NewToolResultError(fmt.Sprintf("unknown goal node %q", goal)), nil
	}

	cr, err := graph.BackwardChain(s.ctx.Graph, graph.ChainOptions{
		Goals: []string{goal},
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("backward chaining failed: %v", err)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Workflow Documentation: %s\n\n", goal)
	fmt.Fprintf(&b, "**Chain:** %s\n\n", strings.Join(cr.Nodes, " → "))

	for i, name := range cr.Nodes {
		node := s.ctx.Graph.Nodes[name]
		if node == nil {
			continue
		}
		if i > 0 {
			b.WriteString("---\n\n")
		}

		docContent := ""
		if s.ctx.NodeDocs != nil {
			docContent = s.ctx.NodeDocs[name]
		}
		oasSummary := s.getOASSummary(name)
		b.WriteString(mergeNodeDoc(node, s.ctx.Graph, docContent, oasSummary))
		b.WriteString("\n")
	}

	return mcp.NewToolResultText(b.String()), nil
}

// handleGenerateDocStub creates a documentation skeleton for a node.
func (s *Server) handleGenerateDocStub(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName, err := req.RequireString("node")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: node"), nil
	}

	node := s.ctx.Graph.Nodes[nodeName]
	if node == nil {
		return mcp.NewToolResultError(fmt.Sprintf("unknown node %q", nodeName)), nil
	}

	oasSummary := s.getOASSummary(nodeName)

	return mcp.NewToolResultText(generateDocStub(node, s.ctx.Graph, oasSummary)), nil
}

// handleListUndocumentedNodes lists graph nodes without a corresponding doc file.
func (s *Server) handleListUndocumentedNodes(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.ctx.DocsDir == "" {
		return mcp.NewToolResultText(
			"Documentation directory not configured. Set the `docs` field in aat-project.yaml to enable per-node documentation.",
		), nil
	}

	names := sortedNodeNames(s.ctx.Graph)
	var undocumented []string
	for _, name := range names {
		if s.ctx.NodeDocs == nil || s.ctx.NodeDocs[name] == "" {
			undocumented = append(undocumented, name)
		}
	}

	if len(undocumented) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("All %d nodes are documented.", len(names))), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d nodes are undocumented:\n\n", len(undocumented), len(names))
	for _, name := range undocumented {
		node := s.ctx.Graph.Nodes[name]
		desc := ""
		if node != nil && node.Description != "" {
			desc = " — " + node.Description
		}
		fmt.Fprintf(&b, "- **%s**%s\n", name, desc)
	}
	fmt.Fprintf(&b, "\nUse `generate_doc_stub` to create a documentation skeleton for any of these nodes.")
	return mcp.NewToolResultText(b.String()), nil
}

// handleEnrichDocumentation assembles context for the LLM to enrich or create
// documentation for a node.
func (s *Server) handleEnrichDocumentation(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	nodeName := req.Params.Arguments["node"]
	if nodeName == "" {
		return nil, fmt.Errorf("missing required argument: node")
	}

	node := s.ctx.Graph.Nodes[nodeName]
	if node == nil {
		return nil, fmt.Errorf("unknown node %q", nodeName)
	}

	g := s.ctx.Graph

	var ctxBuf strings.Builder

	// Node graph detail
	ctxBuf.WriteString(formatNodeDetail(node, g))

	// Existing documentation
	if s.ctx.NodeDocs != nil && s.ctx.NodeDocs[nodeName] != "" {
		ctxBuf.WriteString("\n---\n\n## Existing Documentation\n\n")
		ctxBuf.WriteString(s.ctx.NodeDocs[nodeName])
		if !strings.HasSuffix(s.ctx.NodeDocs[nodeName], "\n") {
			ctxBuf.WriteString("\n")
		}
	}

	// OAS operation detail
	if node.OAS != nil {
		op, method, path, doc, errMsg := s.resolveNodeOperation(nodeName)
		if errMsg == "" && op != nil {
			ctxBuf.WriteString("\n---\n\n")
			ctxBuf.WriteString(formatOperationDetail(method, path, op, doc))
		}
	}

	// Domain knowledge
	if s.ctx.KB != nil {
		domainText := s.ctx.KB.FormatForPrompt()
		if domainText != "" {
			ctxBuf.WriteString("\n---\n\n")
			ctxBuf.WriteString(domainText)
		}
	}

	// Connected nodes for context
	requires := collectRequiredNodeNames(nodeName, g)
	satisfies := collectSatisfiedNodeNames(nodeName, g)
	if len(requires) > 0 || len(satisfies) > 0 {
		ctxBuf.WriteString("\n---\n\n## Connected Nodes\n\n")
		if len(requires) > 0 {
			ctxBuf.WriteString("**Depends on:**\n")
			for _, name := range requires {
				if n := g.Nodes[name]; n != nil && n.Description != "" {
					fmt.Fprintf(&ctxBuf, "- %s — %s\n", name, n.Description)
				} else {
					fmt.Fprintf(&ctxBuf, "- %s\n", name)
				}
			}
			ctxBuf.WriteString("\n")
		}
		if len(satisfies) > 0 {
			ctxBuf.WriteString("**Depended on by:**\n")
			for _, name := range satisfies {
				if n := g.Nodes[name]; n != nil && n.Description != "" {
					fmt.Fprintf(&ctxBuf, "- %s — %s\n", name, n.Description)
				} else {
					fmt.Fprintf(&ctxBuf, "- %s\n", name)
				}
			}
		}
	}

	hasExisting := s.ctx.NodeDocs != nil && s.ctx.NodeDocs[nodeName] != ""
	var instruction string
	if hasExisting {
		instruction = fmt.Sprintf(
			"Enrich the existing documentation for the %s node. "+
				"Improve clarity, fill in any TODO sections, add usage examples, "+
				"and ensure all inputs and outputs are described. "+
				"Keep the existing structure but expand it with the API and domain context provided.",
			nodeName,
		)
	} else {
		instruction = fmt.Sprintf(
			"Create comprehensive documentation for the %s node. "+
				"Include an overview of what the node does, describe all inputs and outputs, "+
				"add usage examples, and note any important constraints or prerequisites. "+
				"Use the graph metadata, API specification, and domain context provided.",
			nodeName,
		)
	}

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Enrich documentation for %s", nodeName),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: ctxBuf.String(),
				},
			},
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: instruction,
				},
			},
		},
	}, nil
}

// getOASSummary returns a brief OAS operation summary for a node, or empty
// string if the node has no OAS reference or the operation can't be resolved.
func (s *Server) getOASSummary(nodeName string) string {
	node := s.ctx.Graph.Nodes[nodeName]
	if node == nil || node.OAS == nil {
		return ""
	}
	op, method, path, _, errMsg := s.resolveNodeOperation(nodeName)
	if errMsg != "" || op == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**%s %s**", method, path)
	if op.Summary != "" {
		fmt.Fprintf(&b, " — %s", op.Summary)
	}
	b.WriteString("\n")
	return b.String()
}

