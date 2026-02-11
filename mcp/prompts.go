package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/gburgyan/aat/graph"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerPrompts adds prompt templates to the MCP server.
func (s *Server) registerPrompts() {
	s.mcp.AddPrompt(
		mcp.NewPrompt("explain_workflow",
			mcp.WithPromptDescription("Explain how an API workflow achieves a goal, with full chain trace and domain context"),
			mcp.WithArgument("goal",
				mcp.RequiredArgument(),
				mcp.ArgumentDescription("Goal node name to trace the workflow to"),
			),
		),
		s.handleExplainWorkflow,
	)

	s.mcp.AddPrompt(
		mcp.NewPrompt("generate_client_code",
			mcp.WithPromptDescription("Generate client code for calling a specific API endpoint"),
			mcp.WithArgument("node",
				mcp.RequiredArgument(),
				mcp.ArgumentDescription("Graph node name to generate code for"),
			),
			mcp.WithArgument("language",
				mcp.ArgumentDescription("Programming language (default: go)"),
			),
		),
		s.handleGenerateClientCode,
	)
}

// handleExplainWorkflow traces a workflow to a goal node and assembles context
// for explaining it.
func (s *Server) handleExplainWorkflow(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	goal := req.Params.Arguments["goal"]
	if goal == "" {
		return nil, fmt.Errorf("missing required argument: goal")
	}

	g := s.ctx.Graph
	if g.Nodes[goal] == nil {
		return nil, fmt.Errorf("unknown goal node %q", goal)
	}

	cr, err := graph.BackwardChain(g, graph.ChainOptions{Goals: []string{goal}})
	if err != nil {
		return nil, fmt.Errorf("backward chain failed: %w", err)
	}

	// Build context message with chain trace and per-node detail
	var ctxBuf strings.Builder
	ctxBuf.WriteString(formatChainTrace(cr, g))

	// Per-node detail for each node in the chain
	for _, name := range cr.Nodes {
		node := g.Nodes[name]
		if node == nil {
			continue
		}
		ctxBuf.WriteString("---\n\n")
		ctxBuf.WriteString(formatNodeDetail(node, g))
		ctxBuf.WriteString("\n")
	}

	// Domain knowledge section
	if s.ctx.KB != nil {
		domainText := s.ctx.KB.FormatForPrompt()
		if domainText != "" {
			ctxBuf.WriteString("---\n\n")
			ctxBuf.WriteString(domainText)
		}
	}

	instruction := fmt.Sprintf(
		"Explain how this API workflow achieves the goal of %s. "+
			"Describe each step, the data flow between steps, "+
			"required inputs at entry points, and what the final output provides.",
		goal,
	)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Explain the %s workflow", goal),
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

// handleGenerateClientCode assembles context for generating client code for
// a specific API endpoint.
func (s *Server) handleGenerateClientCode(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	nodeName := req.Params.Arguments["node"]
	if nodeName == "" {
		return nil, fmt.Errorf("missing required argument: node")
	}

	g := s.ctx.Graph
	node := g.Nodes[nodeName]
	if node == nil {
		return nil, fmt.Errorf("unknown node %q", nodeName)
	}

	language := req.Params.Arguments["language"]
	if language == "" {
		language = "go"
	}

	// Assemble context sections
	var ctxBuf strings.Builder

	// Node detail
	ctxBuf.WriteString(formatNodeDetail(node, g))

	// Template shape
	if node.Adapter != "" {
		if tmpl, ok := s.ctx.Registry.GetTemplate(node.Adapter); ok {
			ctxBuf.WriteString("\n---\n\n")
			ctxBuf.WriteString(formatTemplate(tmpl))
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

	// Domain types
	if s.ctx.KB != nil {
		domainText := s.ctx.KB.FormatForPrompt()
		if domainText != "" {
			ctxBuf.WriteString("\n---\n\n")
			ctxBuf.WriteString(domainText)
		}
	}

	instruction := fmt.Sprintf(
		"Generate %s client code for calling the %s API endpoint. "+
			"Include request construction, HTTP call, response parsing, and error handling.",
		language, nodeName,
	)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Generate %s client code for %s", language, nodeName),
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
