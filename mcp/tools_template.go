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
				mcp.Description("Adapter or node name"),
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
				mcp.Description("Adapter or operation name (use the operation name from describe_operation)"),
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
				mcp.Description("Adapter or node name"),
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
// Accepts either an adapter name or a node name (resolves node → adapter automatically).
func (s *Server) handleInspectTemplate(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("adapter")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: adapter"), nil
	}

	// Try direct adapter lookup first.
	tmpl, ok := s.ctx.Registry.GetTemplate(name)
	if ok {
		return mcp.NewToolResultText(formatTemplate(tmpl)), nil
	}

	// If not found as adapter, try as a node name and resolve to its adapter.
	if node := s.ctx.Graph.Nodes[name]; node != nil && node.Adapter != "" {
		tmpl, ok = s.ctx.Registry.GetTemplate(node.Adapter)
		if ok {
			var note string
			if node.Adapter != name {
				note = fmt.Sprintf("> Showing template for adapter %q (used by node %q)\n\n", node.Adapter, name)
			}
			return mcp.NewToolResultText(note + formatTemplate(tmpl)), nil
		}
	}

	// Neither adapter nor node name matched — give a clear error.
	if _, getErr := s.ctx.Registry.Get(name); getErr == nil {
		return mcp.NewToolResultError(fmt.Sprintf("adapter %q exists but is not template-based", name)), nil
	}
	return mcp.NewToolResultError(fmt.Sprintf("unknown adapter or node %q — if this is a workflow step ID, check the workflow detail for the underlying operation name", name)), nil
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

		if envelope := detectResponseEnvelope(tmpl.Response.Extract); envelope != "" {
			fmt.Fprintf(&b, "> **Response envelope:** The JSON response root key is `%s`. All extract paths below include this wrapper.\n\n", envelope)
		}

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

		if needsPathLegend(tmpl.Response.Extract) {
			b.WriteString("\n")
			b.WriteString(buildPathLegend(tmpl.Response.Extract))
		}
	}

	if tmpl.Response.Transform != "" {
		b.WriteString("\n## Transform (Lua)\n\n")
		if summary := extractTransformSummary(tmpl.Response.Transform); summary != "" {
			fmt.Fprintf(&b, "> %s\n\n", summary)
		}
		b.WriteString("```lua\n")
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

// detectResponseEnvelope checks if all extract rule paths share a common first
// segment, indicating a JSON response wrapper/envelope key.
func detectResponseEnvelope(extract map[string]adapter.ExtractRule) string {
	if len(extract) == 0 {
		return ""
	}

	var envelope string
	first := true
	for _, rule := range extract {
		seg := firstPathSegment(rule.Path)
		if seg == "" {
			continue
		}
		if first {
			envelope = seg
			first = false
		} else if seg != envelope {
			return ""
		}
	}
	if first {
		// All paths were empty.
		return ""
	}
	return envelope
}

// firstPathSegment extracts the first dot-delimited segment from a path,
// stripping any "$." prefix.
func firstPathSegment(path string) string {
	p := strings.TrimPrefix(path, "$.")
	if p == "" {
		return ""
	}
	if i := strings.IndexByte(p, '.'); i >= 0 {
		return p[:i]
	}
	return p
}

// needsPathLegend returns true if any extract rule path uses gjson special syntax.
func needsPathLegend(extract map[string]adapter.ExtractRule) bool {
	for _, rule := range extract {
		if hasGjsonSyntax(rule.Path) {
			return true
		}
		for _, fp := range rule.Fields {
			if hasGjsonSyntax(fp) {
				return true
			}
		}
	}
	return false
}

// hasGjsonSyntax checks if a path contains gjson special characters.
func hasGjsonSyntax(path string) bool {
	if strings.Contains(path, "#") {
		return true
	}
	if strings.Contains(path, "|@") {
		return true
	}
	// Numeric array index patterns: .N. or .N at end
	for i := 0; i < len(path); i++ {
		if path[i] == '.' && i+1 < len(path) && path[i+1] >= '0' && path[i+1] <= '9' {
			return true
		}
	}
	return false
}

// buildPathLegend constructs a legend string for gjson syntax found in the extract rules.
func buildPathLegend(extract map[string]adapter.ExtractRule) string {
	hasArrayIndex := false
	hasIterate := false
	hasFlatten := false

	check := func(path string) {
		if strings.Contains(path, "#") {
			hasIterate = true
		}
		if strings.Contains(path, "|@flatten") {
			hasFlatten = true
		}
		for i := 0; i < len(path); i++ {
			if path[i] == '.' && i+1 < len(path) && path[i+1] >= '0' && path[i+1] <= '9' {
				hasArrayIndex = true
			}
		}
	}

	for _, rule := range extract {
		check(rule.Path)
		for _, fp := range rule.Fields {
			check(fp)
		}
	}

	var parts []string
	if hasArrayIndex {
		parts = append(parts, "`.N` = array index N (zero-based)")
	}
	if hasIterate {
		parts = append(parts, "`#` = iterate all array elements")
	}
	if hasFlatten {
		parts = append(parts, "`|@flatten` = flatten nested arrays")
	}

	if len(parts) == 0 {
		return ""
	}
	return "> **Path notation (gjson):** " + strings.Join(parts, "; ") + "\n"
}

// extractTransformSummary extracts the leading comment block from a Lua script.
// Comment lines start with "--". Collection stops at the first non-comment, non-blank line.
func extractTransformSummary(script string) string {
	var parts []string
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			text := strings.TrimSpace(strings.TrimPrefix(trimmed, "--"))
			if text != "" {
				parts = append(parts, text)
			}
		} else if trimmed == "" {
			// Skip blank lines between comments.
			continue
		} else {
			// First non-comment, non-blank line — stop.
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}
