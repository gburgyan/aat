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

// registerIntegrationGraphTools registers graph tools with API-focused names.
func (s *Server) registerIntegrationGraphTools() {
	s.mcp.AddTool(
		mcp.NewTool("list_api_operations",
			mcp.WithDescription("List all API operations with descriptions and input/output counts. Use describe_operation to drill into a specific operation."),
		),
		s.handleListNodes,
	)

	s.mcp.AddTool(
		mcp.NewTool("describe_operation",
			mcp.WithDescription("Show full details for an API operation: inputs, outputs, dependencies, and OAS reference. For OAS spec details use get_oas_operation. For HTTP request template use inspect_request_template."),
			mcp.WithString("node",
				mcp.Description("Operation name"),
				mcp.Required(),
			),
		),
		s.handleDescribeNode,
	)

	s.mcp.AddTool(
		mcp.NewTool("trace_dependency_chain",
			mcp.WithDescription("Trace the dependency chain for a target operation using backward chaining — shows required operations, data flow between them, and entry points."),
			mcp.WithString("goal",
				mcp.Description("Target operation name to trace backward from"),
				mcp.Required(),
			),
		),
		s.handleTraceWorkflow,
	)

	s.mcp.AddTool(
		mcp.NewTool("search_api",
			mcp.WithDescription("Search for API operations by keyword across names, descriptions, and input/output names. Use describe_operation on results for full details."),
			mcp.WithString("query",
				mcp.Description("Search keyword (case-insensitive)"),
				mcp.Required(),
			),
		),
		s.handleFindWorkflows,
	)

	s.mcp.AddTool(
		mcp.NewTool("get_data_flow",
			mcp.WithDescription("Show how data flows between two API operations: which outputs of the first map to which inputs of the second."),
			mcp.WithString("node_from",
				mcp.Description("Source operation name"),
				mcp.Required(),
			),
			mcp.WithString("node_to",
				mcp.Description("Target operation name"),
				mcp.Required(),
			),
		),
		s.handleGetDataFlow,
	)

	s.mcp.AddTool(
		mcp.NewTool("get_response_shape",
			mcp.WithDescription("Show the output fields for an API operation with their extraction paths, types, and which downstream operations consume them."),
			mcp.WithString("node",
				mcp.Description("Operation name"),
				mcp.Required(),
			),
		),
		s.handleGetResponseShape,
	)

	s.mcp.AddTool(
		mcp.NewTool("explain_field",
			mcp.WithDescription("Show everything known about a specific field on an operation: type, domain concept, value pool, constraints, and which operations produce or consume it."),
			mcp.WithString("node",
				mcp.Description("Operation name"),
				mcp.Required(),
			),
			mcp.WithString("field",
				mcp.Description("Field name (input or output)"),
				mcp.Required(),
			),
		),
		s.handleExplainField,
	)
}

// registerTestGraphTools registers graph tools with test-focused names.
func (s *Server) registerTestGraphTools() {
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
			if wf.After.IsSet() {
				fmt.Fprintf(&b, "  After: %s\n", wf.After.String())
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

// handleGetDataFlow shows how data flows between two operations.
func (s *Server) handleGetDataFlow(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeFrom, err := req.RequireString("node_from")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: node_from"), nil
	}
	nodeTo, err := req.RequireString("node_to")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: node_to"), nil
	}

	g := s.ctx.Graph
	fromNode := g.Nodes[nodeFrom]
	if fromNode == nil {
		return mcp.NewToolResultError(fmt.Sprintf("unknown node %q", nodeFrom)), nil
	}
	toNode := g.Nodes[nodeTo]
	if toNode == nil {
		return mcp.NewToolResultError(fmt.Sprintf("unknown node %q", nodeTo)), nil
	}

	// Build set of tokens satisfied by nodeFrom.
	satisfiedTokens := make(map[string]bool)
	for _, token := range fromNode.Satisfies {
		satisfiedTokens[token] = true
	}

	// Find matching requires on nodeTo.
	type mapping struct {
		outputName string
		outputType string
		inputName  string
		inputType  string
	}
	var mappings []mapping

	for _, token := range toNode.Requires {
		if !satisfiedTokens[token] {
			continue
		}
		// Find the output on fromNode matching this token.
		var outName, outType string
		for _, out := range fromNode.Outputs {
			if out.Name == token {
				outName = out.Name
				outType = out.Type
				break
			}
		}
		if outName == "" {
			outName = token
		}

		// Find the input on toNode matching this token.
		var inpName, inpType string
		for _, inp := range toNode.Inputs {
			if inp.Name == token {
				inpName = inp.Name
				inpType = inp.Type
				break
			}
		}
		if inpName == "" {
			inpName = "(via token: " + token + ")"
		}

		mappings = append(mappings, mapping{
			outputName: outName, outputType: outType,
			inputName: inpName, inputType: inpType,
		})
	}

	if len(mappings) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No direct data flow from %s to %s found in the graph.", nodeFrom, nodeTo)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Data Flow: %s → %s\n\n", nodeFrom, nodeTo)
	b.WriteString("| Output | Type | → | Input | Type |\n")
	b.WriteString("|--------|------|---|-------|------|\n")
	for _, m := range mappings {
		fmt.Fprintf(&b, "| %s | %s | → | %s | %s |\n", m.outputName, m.outputType, m.inputName, m.inputType)
	}

	// Show element fields on array outputs that flow to the target.
	shown := make(map[string]bool)
	for _, m := range mappings {
		if shown[m.outputName] {
			continue
		}
		for _, out := range fromNode.Outputs {
			if out.Name == m.outputName && len(out.ElementFields) > 0 {
				shown[m.outputName] = true
				names := make([]string, len(out.ElementFields))
				for i, ef := range out.ElementFields {
					names[i] = ef.Name
				}
				fmt.Fprintf(&b, "\n**%s element fields:** %s\n", out.Name, strings.Join(names, ", "))
			}
		}
	}

	return mcp.NewToolResultText(b.String()), nil
}

// handleGetResponseShape returns the output fields for an operation with their
// extraction paths and downstream consumers.
func (s *Server) handleGetResponseShape(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName, err := req.RequireString("node")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: node"), nil
	}

	g := s.ctx.Graph
	node := g.Nodes[nodeName]
	if node == nil {
		return mcp.NewToolResultError(fmt.Sprintf("unknown node %q", nodeName)), nil
	}

	if len(node.Outputs) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("Node %q has no outputs.", nodeName)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Response Shape: %s\n\n", nodeName)

	// Extract rules from template.
	var extractRules map[string]string
	if node.Adapter != "" {
		if tmpl, ok := s.ctx.Registry.GetTemplate(node.Adapter); ok {
			extractRules = make(map[string]string)
			for name, rule := range tmpl.Response.Extract {
				extractRules[name] = rule.Path
			}
		}
	}

	// Build consumers map: output name → list of consuming node names.
	consumers := make(map[string][]string)
	for _, token := range node.Satisfies {
		for otherName, otherNode := range g.Nodes {
			if otherName == nodeName {
				continue
			}
			for _, r := range otherNode.Requires {
				if r == token {
					consumers[token] = append(consumers[token], otherName)
				}
			}
		}
	}

	b.WriteString("| Output | Type | Extract Path | Consumed By |\n")
	b.WriteString("|--------|------|-------------|-------------|\n")
	for _, out := range node.Outputs {
		path := ""
		if extractRules != nil {
			if p, ok := extractRules[out.Name]; ok {
				path = "`" + p + "`"
			}
		}
		consumedBy := ""
		if c, ok := consumers[out.Name]; ok {
			consumedBy = strings.Join(c, ", ")
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", out.Name, out.Type, path, consumedBy)

		for _, ef := range out.ElementFields {
			efPath := ""
			if ef.Path != "" && ef.Path != ef.Name {
				efPath = "`" + ef.Path + "`"
			}
			fmt.Fprintf(&b, "| \u00a0\u00a0\u2514 %s | %s | %s | |\n", ef.Name, ef.Type, efPath)
		}
	}

	return mcp.NewToolResultText(b.String()), nil
}

// handleExplainField returns everything known about a specific field on an operation.
func (s *Server) handleExplainField(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName, err := req.RequireString("node")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: node"), nil
	}
	fieldName, err := req.RequireString("field")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: field"), nil
	}

	g := s.ctx.Graph
	node := g.Nodes[nodeName]
	if node == nil {
		return mcp.NewToolResultError(fmt.Sprintf("unknown node %q", nodeName)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Field: %s.%s\n\n", nodeName, fieldName)

	found := false

	// Check inputs.
	for _, inp := range node.Inputs {
		if inp.Name == fieldName {
			found = true
			b.WriteString("**Kind:** input\n")
			fmt.Fprintf(&b, "**Type:** %s\n", inp.Type)
			if inp.Description != "" {
				fmt.Fprintf(&b, "**Description:** %s\n", inp.Description)
			}
			fmt.Fprintf(&b, "**Required:** %v\n", !inp.Optional)
			if inp.Default != nil && inp.Default.HasValue() {
				fmt.Fprintf(&b, "**Default:** %s\n", formatGraphDefault(inp.Default))
			}
			if inp.Constraints != nil {
				fmt.Fprintf(&b, "**Constraints:** %s\n", formatConstraintCell(inp.Constraints))
			}

			// Find producers: which operations output this field?
			b.WriteString("\n## Produced By\n\n")
			foundProducer := false
			for otherName, otherNode := range g.Nodes {
				if otherName == nodeName {
					continue
				}
				for _, out := range otherNode.Outputs {
					if out.Name == fieldName {
						fmt.Fprintf(&b, "- **%s** → %s (%s)\n", otherName, out.Name, out.Type)
						foundProducer = true
					}
					for _, ef := range out.ElementFields {
						if ef.Name == fieldName {
							fmt.Fprintf(&b, "- **%s** → %s.%s (%s)\n", otherName, out.Name, ef.Name, ef.Type)
							foundProducer = true
						}
					}
				}
			}
			if !foundProducer {
				b.WriteString("(no graph node produces this field — must be supplied externally)\n")
			}
			break
		}
	}

	// Check outputs.
	if !found {
		for _, out := range node.Outputs {
			if out.Name == fieldName {
				found = true
				b.WriteString("**Kind:** output\n")
				fmt.Fprintf(&b, "**Type:** %s\n", out.Type)
				if out.Description != "" {
					fmt.Fprintf(&b, "**Description:** %s\n", out.Description)
				}
				if len(out.ElementFields) > 0 {
					b.WriteString("\n**Element Fields:**\n")
					for _, ef := range out.ElementFields {
						path := ""
						if ef.Path != "" && ef.Path != ef.Name {
							path = fmt.Sprintf(" (path: %s)", ef.Path)
						}
						fmt.Fprintf(&b, "- %s (%s)%s\n", ef.Name, ef.Type, path)
					}
				}

				// Find consumers.
				b.WriteString("\n## Consumed By\n\n")
				foundConsumer := false
				for otherName, otherNode := range g.Nodes {
					if otherName == nodeName {
						continue
					}
					for _, inp := range otherNode.Inputs {
						if inp.Name == fieldName {
							fmt.Fprintf(&b, "- **%s** ← %s (%s)\n", otherName, inp.Name, inp.Type)
							foundConsumer = true
						}
					}
				}
				if !foundConsumer {
					b.WriteString("(no graph node consumes this field directly)\n")
				}

				// Extract path from template.
				if node.Adapter != "" {
					if tmpl, ok := s.ctx.Registry.GetTemplate(node.Adapter); ok {
						if rule, ok := tmpl.Response.Extract[fieldName]; ok {
							fmt.Fprintf(&b, "\n**Extract Path:** `%s`\n", rule.Path)
						}
					}
				}
				break
			}
			// Check element fields.
			for _, ef := range out.ElementFields {
				if ef.Name == fieldName {
					found = true
					b.WriteString("**Kind:** elementField\n")
					fmt.Fprintf(&b, "**Parent Output:** %s (%s)\n", out.Name, out.Type)
					fmt.Fprintf(&b, "**Type:** %s\n", ef.Type)
					if ef.Path != "" && ef.Path != ef.Name {
						fmt.Fprintf(&b, "**Extract Path:** `%s`\n", ef.Path)
					}
					break
				}
			}
			if found {
				break
			}
		}
	}

	if !found {
		return mcp.NewToolResultError(fmt.Sprintf("field %q not found on node %q", fieldName, nodeName)), nil
	}

	// Domain knowledge cross-reference.
	if s.ctx.KB != nil {
		if td, ok := s.ctx.KB.Types[fieldName]; ok {
			fmt.Fprintf(&b, "\n## Domain Type: %s\n\n", fieldName)
			fmt.Fprintf(&b, "%s (format: %s)\n", td.Description, td.Format)
			if td.Pool != "" {
				allValues := s.ctx.KB.AllValues(td.Pool)
				if len(allValues) > 0 {
					preview := allValues
					if len(preview) > 5 {
						preview = preview[:5]
					}
					fmt.Fprintf(&b, "**Value pool (%s):** %s", td.Pool, strings.Join(preview, ", "))
					if len(allValues) > 5 {
						b.WriteString(", ...")
					}
					b.WriteString("\n")
				}
			}
		}

		for cName, c := range s.ctx.KB.Concepts {
			for _, applies := range c.AppliesTo {
				if applies == fieldName {
					fmt.Fprintf(&b, "\n## Domain Concept: %s\n\n", cName)
					fmt.Fprintf(&b, "%s\n", c.Description)
					if c.Constraint != "" {
						fmt.Fprintf(&b, "**Constraint:** %s\n", c.Constraint)
					}
					break
				}
			}
		}
	}

	return mcp.NewToolResultText(b.String()), nil
}
