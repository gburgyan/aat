package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/gburgyan/aat/adapter"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerTemplateTools adds the template and adapter browsing tools to the MCP server.
func (s *Server) registerTemplateTools() {
	s.mcp.AddTool(
		mcp.NewTool("list_adapters",
			mcp.WithDescription("List all registered adapter names. Use inspect_template to see HTTP details for a specific adapter."),
		),
		s.handleListAdapters,
	)

	s.mcp.AddTool(
		mcp.NewTool("inspect_template",
			mcp.WithDescription("Show the HTTP template for an adapter: method, path, headers, body template, and response extract rules. This shows what AAT actually sends at runtime. For the OAS spec view of the same endpoint, use get_oas_operation with the graph node name."),
			mcp.WithString("adapter",
				mcp.Description("Adapter name"),
				mcp.Required(),
			),
		),
		s.handleInspectTemplate,
	)
}

// registerIntegrationTemplateTools registers template tools for the API persona.
func (s *Server) registerIntegrationTemplateTools() {
	s.mcp.AddTool(
		mcp.NewTool("inspect_request_template",
			mcp.WithDescription("Show the HTTP request template for an API operation: method, path, headers, body template, and response extract rules. This shows the actual HTTP shape. For the OAS spec view use get_oas_operation."),
			mcp.WithString("adapter",
				mcp.Description("Adapter name (shown in describe_operation output)"),
				mcp.Required(),
			),
		),
		s.handleInspectTemplate,
	)
}

// registerTestTemplateTools registers template tools for the test persona.
func (s *Server) registerTestTemplateTools() {
	s.mcp.AddTool(
		mcp.NewTool("list_adapters",
			mcp.WithDescription("List all registered adapter names. Use inspect_template to see HTTP details for a specific adapter."),
		),
		s.handleListAdapters,
	)

	s.mcp.AddTool(
		mcp.NewTool("inspect_template",
			mcp.WithDescription("Show the HTTP template for an adapter: method, path, headers, body template, and response extract rules. This shows what AAT actually sends at runtime. For the OAS spec view of the same endpoint, use get_oas_operation with the graph node name."),
			mcp.WithString("adapter",
				mcp.Description("Adapter name"),
				mcp.Required(),
			),
		),
		s.handleInspectTemplate,
	)
}

// handleListAdapters returns a sorted list of all adapter names.
func (s *Server) handleListAdapters(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	names := s.ctx.Registry.Names()
	if len(names) == 0 {
		return mcp.NewToolResultText("No adapters registered."), nil
	}

	var b strings.Builder
	for i, name := range names {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "- %s", name)
	}
	return mcp.NewToolResultText(b.String()), nil
}

// handleInspectTemplate returns full Markdown detail for a template-based adapter.
func (s *Server) handleInspectTemplate(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("adapter")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: adapter"), nil
	}

	tmpl, ok := s.ctx.Registry.GetTemplate(name)
	if !ok {
		// Distinguish between unknown adapter and non-template adapter.
		if _, getErr := s.ctx.Registry.Get(name); getErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("unknown adapter %q", name)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("adapter %q exists but is not template-based", name)), nil
	}

	return mcp.NewToolResultText(formatTemplate(tmpl)), nil
}

// formatTemplate renders a Template as Markdown.
func formatTemplate(tmpl *adapter.Template) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", tmpl.Adapter)
	fmt.Fprintf(&b, "**Protocol:** %s\n", tmpl.Protocol)
	fmt.Fprintf(&b, "**Method:** %s\n", tmpl.Request.Method)
	fmt.Fprintf(&b, "**Path:** `%s`\n", tmpl.Request.Path)

	if len(tmpl.Request.Headers) > 0 {
		b.WriteString("\n## Headers\n\n")
		b.WriteString("| Header | Value |\n")
		b.WriteString("|--------|-------|\n")
		keys := make([]string, 0, len(tmpl.Request.Headers))
		for k := range tmpl.Request.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "| %s | %s |\n", k, tmpl.Request.Headers[k])
		}
	}

	if tmpl.Request.Body != "" {
		b.WriteString("\n## Body\n\n```\n")
		b.WriteString(tmpl.Request.Body)
		if !strings.HasSuffix(tmpl.Request.Body, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```\n")
	}

	if len(tmpl.Response.Extract) > 0 {
		b.WriteString("\n## Extract Rules\n\n")
		b.WriteString("| Output | Path |\n")
		b.WriteString("|--------|------|\n")
		keys := make([]string, 0, len(tmpl.Response.Extract))
		for k := range tmpl.Response.Extract {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			rule := tmpl.Response.Extract[k]
			fmt.Fprintf(&b, "| %s | %s |\n", k, rule.Path)
			if len(rule.Fields) > 0 {
				for fn, fp := range rule.Fields {
					fmt.Fprintf(&b, "| &nbsp;&nbsp;.%s | %s |\n", fn, fp)
				}
			}
		}
	}

	if tmpl.Response.Transform != "" {
		b.WriteString("\n## Transform (Lua)\n\n```lua\n")
		b.WriteString(tmpl.Response.Transform)
		if !strings.HasSuffix(tmpl.Response.Transform, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```\n")
	}

	required, conditional, iterable := adapter.ClassifyInputs(tmpl)
	if len(required) > 0 || len(conditional) > 0 || len(iterable) > 0 {
		b.WriteString("\n## Template Inputs\n\n")
		if len(required) > 0 {
			fmt.Fprintf(&b, "**Required:** %s\n", strings.Join(required, ", "))
		}
		if len(conditional) > 0 {
			fmt.Fprintf(&b, "**Conditional:** %s\n", strings.Join(conditional, ", "))
		}
		if len(iterable) > 0 {
			fmt.Fprintf(&b, "**Iterable:** %s\n", strings.Join(iterable, ", "))
		}
	}

	return b.String()
}
