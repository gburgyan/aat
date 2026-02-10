package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerDomainTools adds the domain knowledge browsing tools to the MCP server.
func (s *Server) registerDomainTools() {
	s.mcp.AddTool(
		mcp.NewTool("list_concepts",
			mcp.WithDescription("List all domain concepts with descriptions and applicable fields"),
		),
		s.handleListConcepts,
	)

	s.mcp.AddTool(
		mcp.NewTool("list_types",
			mcp.WithDescription("List all domain type definitions with format and field info"),
		),
		s.handleListTypes,
	)

	s.mcp.AddTool(
		mcp.NewTool("list_value_pools",
			mcp.WithDescription("List all value pools with type, sample values, and total count"),
		),
		s.handleListValuePools,
	)

	s.mcp.AddTool(
		mcp.NewTool("explain_concept",
			mcp.WithDescription("Show full detail for a domain concept: description, constraints, examples, and related types/pools"),
			mcp.WithString("name",
				mcp.Description("Concept name"),
				mcp.Required(),
			),
		),
		s.handleExplainConcept,
	)
}

// handleListConcepts returns a sorted list of all domain concepts.
func (s *Server) handleListConcepts(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.ctx.KB == nil {
		return mcp.NewToolResultText("Domain knowledge not configured for this project."), nil
	}

	if len(s.ctx.KB.Concepts) == 0 {
		return mcp.NewToolResultText("No concepts defined."), nil
	}

	names := sortedMapKeys(s.ctx.KB.Concepts)
	var b strings.Builder
	for i, name := range names {
		if i > 0 {
			b.WriteString("\n")
		}
		c := s.ctx.KB.Concepts[name]
		fmt.Fprintf(&b, "**%s** — %s", name, c.Description)
		if len(c.AppliesTo) > 0 {
			fmt.Fprintf(&b, " (applies to: %s)", strings.Join(c.AppliesTo, ", "))
		}
	}
	return mcp.NewToolResultText(b.String()), nil
}

// handleListTypes returns a sorted list of all domain type definitions.
func (s *Server) handleListTypes(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.ctx.KB == nil {
		return mcp.NewToolResultText("Domain knowledge not configured for this project."), nil
	}

	if len(s.ctx.KB.Types) == 0 {
		return mcp.NewToolResultText("No types defined."), nil
	}

	names := sortedMapKeys(s.ctx.KB.Types)
	var b strings.Builder
	for i, name := range names {
		if i > 0 {
			b.WriteString("\n")
		}
		td := s.ctx.KB.Types[name]
		fmt.Fprintf(&b, "**%s** — %s (format: %s)", name, td.Description, td.Format)
		if len(td.Fields) > 0 {
			fieldNames := sortedMapKeys(td.Fields)
			fmt.Fprintf(&b, "\n  Fields: %s", strings.Join(fieldNames, ", "))
		}
	}
	return mcp.NewToolResultText(b.String()), nil
}

// handleListValuePools returns a sorted list of all value pools with previews.
func (s *Server) handleListValuePools(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.ctx.KB == nil {
		return mcp.NewToolResultText("Domain knowledge not configured for this project."), nil
	}

	if len(s.ctx.KB.ValuePools) == 0 {
		return mcp.NewToolResultText("No value pools defined."), nil
	}

	names := sortedMapKeys(s.ctx.KB.ValuePools)
	var b strings.Builder
	for i, name := range names {
		if i > 0 {
			b.WriteString("\n")
		}
		p := s.ctx.KB.ValuePools[name]
		allValues := s.ctx.KB.AllValues(name)
		total := len(allValues)
		fmt.Fprintf(&b, "**%s** — %s (type: %s, %d values)", name, p.Description, p.Type, total)

		if total > 0 {
			preview := allValues
			if len(preview) > 5 {
				preview = preview[:5]
			}
			fmt.Fprintf(&b, "\n  Preview: %s", strings.Join(preview, ", "))
			if total > 5 {
				b.WriteString(", ...")
			}
		}
	}
	return mcp.NewToolResultText(b.String()), nil
}

// handleExplainConcept returns full detail for a single domain concept.
func (s *Server) handleExplainConcept(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.ctx.KB == nil {
		return mcp.NewToolResultText("Domain knowledge not configured for this project."), nil
	}

	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: name"), nil
	}

	c := s.ctx.KB.GetConcept(name)
	if c == nil {
		return mcp.NewToolResultError(fmt.Sprintf("unknown concept %q", name)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", name)
	fmt.Fprintf(&b, "%s\n\n", c.Description)

	if len(c.AppliesTo) > 0 {
		fmt.Fprintf(&b, "**Applies to:** %s\n", strings.Join(c.AppliesTo, ", "))
	}
	if c.Constraint != "" {
		fmt.Fprintf(&b, "**Constraint:** %s\n", c.Constraint)
	}

	if len(c.Examples) > 0 {
		b.WriteString("\n## Examples\n\n")
		exKeys := sortedMapKeys(c.Examples)
		for _, ek := range exKeys {
			fmt.Fprintf(&b, "- **%s:** %s\n", ek, strings.Join(c.Examples[ek], ", "))
		}
	}

	// Cross-references: find types whose name appears in appliesTo
	var relatedTypes []string
	for _, field := range c.AppliesTo {
		for tName := range s.ctx.KB.Types {
			if tName == field {
				relatedTypes = append(relatedTypes, tName)
			}
		}
	}
	sort.Strings(relatedTypes)

	// Cross-references: find pools via related types
	var relatedPools []string
	seen := make(map[string]bool)
	for _, tName := range relatedTypes {
		td := s.ctx.KB.Types[tName]
		if td != nil && td.Pool != "" && !seen[td.Pool] {
			seen[td.Pool] = true
			relatedPools = append(relatedPools, td.Pool)
		}
	}
	sort.Strings(relatedPools)

	if len(relatedTypes) > 0 || len(relatedPools) > 0 {
		b.WriteString("\n## Related\n\n")
		if len(relatedTypes) > 0 {
			fmt.Fprintf(&b, "**Types:** %s\n", strings.Join(relatedTypes, ", "))
		}
		if len(relatedPools) > 0 {
			fmt.Fprintf(&b, "**Value pools:** %s\n", strings.Join(relatedPools, ", "))
		}
	}

	return mcp.NewToolResultText(b.String()), nil
}

// sortedMapKeys returns the keys of any map[string]V in sorted order.
func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
