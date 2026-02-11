package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/gburgyan/aat/intent"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerResources adds static and dynamic resources to the MCP server.
func (s *Server) registerResources() {
	// Static resources
	s.mcp.AddResource(
		mcp.NewResource("aat://graph", "API Graph",
			mcp.WithResourceDescription("Full API graph showing all nodes, edges, and conditions"),
			mcp.WithMIMEType("text/markdown"),
		),
		s.handleGraphResource,
	)

	s.mcp.AddResource(
		mcp.NewResource("aat://templates", "Adapter Templates",
			mcp.WithResourceDescription("HTTP templates for all registered adapters"),
			mcp.WithMIMEType("text/markdown"),
		),
		s.handleTemplatesResource,
	)

	s.mcp.AddResource(
		mcp.NewResource("aat://domain", "Domain Knowledge",
			mcp.WithResourceDescription("Domain concepts, types, and value pools"),
			mcp.WithMIMEType("text/markdown"),
		),
		s.handleDomainResource,
	)

	s.mcp.AddResource(
		mcp.NewResource("aat://metadata", "Project Metadata",
			mcp.WithResourceDescription("Project manifest and graph statistics"),
			mcp.WithMIMEType("text/markdown"),
		),
		s.handleMetadataResource,
	)

	// Dynamic resources (URI templates)
	s.mcp.AddResourceTemplate(
		mcp.NewResourceTemplate("aat://node/{name}", "Node Detail",
			mcp.WithTemplateDescription("Detailed view of a specific graph node including inputs, outputs, and edges"),
			mcp.WithTemplateMIMEType("text/markdown"),
		),
		s.handleNodeResource,
	)

	s.mcp.AddResourceTemplate(
		mcp.NewResourceTemplate("aat://template/{adapter}", "Template Detail",
			mcp.WithTemplateDescription("HTTP template detail for a specific adapter"),
			mcp.WithTemplateMIMEType("text/markdown"),
		),
		s.handleTemplateResource,
	)
}

// handleGraphResource returns the full API graph formatted as Markdown.
func (s *Server) handleGraphResource(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	content := intent.FormatGraph(s.ctx.Graph)
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     content,
		},
	}, nil
}

// handleTemplatesResource returns a summary of all registered adapter templates.
func (s *Server) handleTemplatesResource(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	names := s.ctx.Registry.Names()
	if len(names) == 0 {
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: "text/markdown",
				Text:     "No adapters registered.",
			},
		}, nil
	}

	var b strings.Builder
	b.WriteString("# Adapter Templates\n\n")
	for _, name := range names {
		tmpl, ok := s.ctx.Registry.GetTemplate(name)
		if ok {
			fmt.Fprintf(&b, "- **%s** — %s %s\n", name, tmpl.Request.Method, tmpl.Request.Path)
		} else {
			fmt.Fprintf(&b, "- **%s** (non-template)\n", name)
		}
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     b.String(),
		},
	}, nil
}

// handleDomainResource returns domain knowledge formatted for prompts.
func (s *Server) handleDomainResource(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	content := ""
	if s.ctx.KB != nil {
		content = s.ctx.KB.FormatForPrompt()
	}
	if content == "" {
		content = "Domain knowledge not configured."
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     content,
		},
	}, nil
}

// handleMetadataResource returns project manifest and graph statistics.
func (s *Server) handleMetadataResource(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	var b strings.Builder
	b.WriteString("# Project Metadata\n\n")

	if s.ctx.Manifest != nil {
		if s.ctx.Manifest.Name != "" {
			fmt.Fprintf(&b, "**Name:** %s\n", s.ctx.Manifest.Name)
		}
		if s.ctx.Manifest.Description != "" {
			fmt.Fprintf(&b, "**Description:** %s\n", s.ctx.Manifest.Description)
		}
		if len(s.ctx.Manifest.Tags) > 0 {
			fmt.Fprintf(&b, "**Tags:** %s\n", strings.Join(s.ctx.Manifest.Tags, ", "))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Graph Statistics\n\n")
	g := s.ctx.Graph
	fmt.Fprintf(&b, "**Version:** %s\n", g.Version)
	fmt.Fprintf(&b, "**Nodes:** %d\n", len(g.Nodes))
	fmt.Fprintf(&b, "**Edges:** %d\n", len(g.Edges))
	fmt.Fprintf(&b, "**Conditions:** %d\n", len(g.Conditions))

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     b.String(),
		},
	}, nil
}

// handleNodeResource returns detailed information about a specific graph node.
func (s *Server) handleNodeResource(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	name, err := extractURIParam(req, "name")
	if err != nil {
		return nil, err
	}

	node := s.ctx.Graph.Nodes[name]
	if node == nil {
		return nil, fmt.Errorf("unknown node %q", name)
	}

	content := formatNodeDetail(node, s.ctx.Graph)
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     content,
		},
	}, nil
}

// handleTemplateResource returns detailed information about a specific adapter template.
func (s *Server) handleTemplateResource(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	adapterName, err := extractURIParam(req, "adapter")
	if err != nil {
		return nil, err
	}

	tmpl, ok := s.ctx.Registry.GetTemplate(adapterName)
	if !ok {
		if _, getErr := s.ctx.Registry.Get(adapterName); getErr != nil {
			return nil, fmt.Errorf("unknown adapter %q", adapterName)
		}
		return nil, fmt.Errorf("adapter %q exists but is not template-based", adapterName)
	}

	content := formatTemplate(tmpl)
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     content,
		},
	}, nil
}

// extractURIParam extracts a named parameter from a resource template request.
func extractURIParam(req mcp.ReadResourceRequest, key string) (string, error) {
	val, ok := req.Params.Arguments[key]
	if !ok {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	str, ok := val.(string)
	if !ok || str == "" {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	return str, nil
}
