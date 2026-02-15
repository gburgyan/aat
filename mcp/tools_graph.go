package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/gburgyan/aat/graph"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerGraphTools adds the graph browsing tools to the MCP server.
func (s *Server) registerGraphTools() {
	s.mcp.AddTool(
		mcp.NewTool("list_nodes",
			mcp.WithDescription("List all nodes in the API graph with descriptions and input/output counts. Use describe_node to drill into a specific node."),
		),
		s.handleListNodes,
	)

	s.mcp.AddTool(
		mcp.NewTool("describe_node",
			mcp.WithDescription("Show full details for a graph node: inputs, outputs, edges, adapter, and OAS reference. For OAS spec details use get_oas_operation with the same node name. For HTTP template details use inspect_template with the adapter name shown here."),
			mcp.WithString("node",
				mcp.Description("Node name"),
				mcp.Required(),
			),
		),
		s.handleDescribeNode,
	)

	s.mcp.AddTool(
		mcp.NewTool("trace_workflow",
			mcp.WithDescription("Trace the dependency chain for a goal node using backward chaining — shows required nodes, data flow, and entry points. Use describe_node on individual nodes in the chain for details."),
			mcp.WithString("goal",
				mcp.Description("Goal node name to trace backward from"),
				mcp.Required(),
			),
		),
		s.handleTraceWorkflow,
	)

	s.mcp.AddTool(
		mcp.NewTool("find_workflows",
			mcp.WithDescription("Search for nodes by keyword across names, descriptions, and input/output names. Use describe_node on results to see full details."),
			mcp.WithString("query",
				mcp.Description("Search keyword (case-insensitive)"),
				mcp.Required(),
			),
		),
		s.handleFindWorkflows,
	)
}

// handleListNodes returns a sorted list of all graph nodes.
func (s *Server) handleListNodes(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	g := s.ctx.Graph
	if len(g.Nodes) == 0 {
		return mcp.NewToolResultText("No nodes in the graph."), nil
	}

	var b strings.Builder

	// Graph intro
	if g.Title != "" {
		fmt.Fprintf(&b, "# %s\n\n", g.Title)
	}
	if g.Description != "" {
		b.WriteString(strings.TrimRight(g.Description, "\n"))
		b.WriteString("\n\n")
	}

	names := sortedNodeNames(g)
	fmt.Fprintf(&b, "%d nodes:\n\n", len(names))
	for i, name := range names {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(formatNodeSummary(g.Nodes[name]))
	}
	return mcp.NewToolResultText(b.String()), nil
}

// handleDescribeNode returns full Markdown detail for a single node.
func (s *Server) handleDescribeNode(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName, err := req.RequireString("node")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: node"), nil
	}

	node := s.ctx.Graph.Nodes[nodeName]
	if node == nil {
		return mcp.NewToolResultError(fmt.Sprintf("unknown node %q", nodeName)), nil
	}

	return mcp.NewToolResultText(formatNodeDetail(node, s.ctx.Graph)), nil
}

// handleTraceWorkflow traces backward from a goal node and returns the
// dependency chain.
func (s *Server) handleTraceWorkflow(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	return mcp.NewToolResultText(formatChainTrace(cr, s.ctx.Graph)), nil
}

// handleFindWorkflows performs case-insensitive keyword search across node
// names, descriptions, input/output names, tags, and named workflows.
func (s *Server) handleFindWorkflows(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: query"), nil
	}

	q := strings.ToLower(query)
	g := s.ctx.Graph

	var b strings.Builder

	// Search named workflows
	var wfMatches []graph.Workflow
	for _, wf := range g.Workflows {
		if strings.Contains(strings.ToLower(wf.Name), q) ||
			strings.Contains(strings.ToLower(wf.Description), q) {
			wfMatches = append(wfMatches, wf)
		}
	}
	if len(wfMatches) > 0 {
		fmt.Fprintf(&b, "Found %d workflow(s) matching %q:\n\n", len(wfMatches), query)
		for _, wf := range wfMatches {
			fmt.Fprintf(&b, "- **%s**", wf.Name)
			if wf.Description != "" {
				fmt.Fprintf(&b, ": %s", wf.Description)
			}
			b.WriteString("\n")
			if len(wf.Steps) > 0 {
				fmt.Fprintf(&b, "  Steps: %s\n", strings.Join(wf.Steps, " → "))
			}
		}
		b.WriteString("\n")
	}

	// Search nodes
	names := sortedNodeNames(g)

	type match struct {
		node    *graph.Node
		reasons []string
	}
	var matches []match

	for _, name := range names {
		node := g.Nodes[name]
		var reasons []string

		if strings.Contains(strings.ToLower(name), q) {
			reasons = append(reasons, "name")
		}
		if strings.Contains(strings.ToLower(node.Description), q) {
			reasons = append(reasons, "description")
		}
		for _, tag := range node.Tags {
			if strings.Contains(strings.ToLower(tag), q) {
				reasons = append(reasons, fmt.Sprintf("tag %q", tag))
				break
			}
		}
		for _, inp := range node.Inputs {
			if strings.Contains(strings.ToLower(inp.Name), q) {
				reasons = append(reasons, fmt.Sprintf("input %q", inp.Name))
				break
			}
		}
		for _, out := range node.Outputs {
			if strings.Contains(strings.ToLower(out.Name), q) {
				reasons = append(reasons, fmt.Sprintf("output %q", out.Name))
				break
			}
		}

		if len(reasons) > 0 {
			matches = append(matches, match{node: node, reasons: reasons})
		}
	}

	if len(matches) == 0 && len(wfMatches) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No nodes or workflows matching %q.", query)), nil
	}

	if len(matches) > 0 {
		fmt.Fprintf(&b, "Found %d node(s) matching %q:\n\n", len(matches), query)
		for _, m := range matches {
			fmt.Fprintf(&b, "%s\n  Matched: %s\n", formatNodeSummary(m.node), strings.Join(m.reasons, ", "))
		}
	}
	return mcp.NewToolResultText(b.String()), nil
}
