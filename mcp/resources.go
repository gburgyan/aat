package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/gburgyan/aat/intent"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerResources adds static and dynamic resources to the MCP server (backward compat).
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

	if s.ctx.ReadmeContent != "" {
		s.mcp.AddResource(
			mcp.NewResource("aat://readme", "Project README",
				mcp.WithResourceDescription("Project README.md documentation from the graph directory"),
				mcp.WithMIMEType("text/markdown"),
			),
			s.handleReadmeResource,
		)
	}

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

	s.mcp.AddResourceTemplate(
		mcp.NewResourceTemplate("aat://workflow/{name}", "Workflow Detail",
			mcp.WithTemplateDescription("Enriched step-by-step recipe for a workflow showing HTTP methods, data flow, and inputs"),
			mcp.WithTemplateMIMEType("text/markdown"),
		),
		s.handleWorkflowResource,
	)
}

// registerIntegrationResources adds resources for the API persona.
func (s *Server) registerIntegrationResources() {
	s.mcp.AddResource(
		mcp.NewResource("aat://api/overview", "API Overview",
			mcp.WithResourceDescription("Compact overview of all API operations: name, HTTP method, path, and description"),
			mcp.WithMIMEType("text/markdown"),
		),
		s.handleAPIOverviewResource,
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

	if s.ctx.ReadmeContent != "" {
		s.mcp.AddResource(
			mcp.NewResource("aat://readme", "Project README",
				mcp.WithResourceDescription("Project README.md documentation from the graph directory"),
				mcp.WithMIMEType("text/markdown"),
			),
			s.handleReadmeResource,
		)
	}

	s.mcp.AddResourceTemplate(
		mcp.NewResourceTemplate("aat://operation/{name}", "Operation Detail",
			mcp.WithTemplateDescription("Detailed view of a specific API operation including inputs, outputs, and dependencies"),
			mcp.WithTemplateMIMEType("text/markdown"),
		),
		s.handleNodeResource,
	)

	s.mcp.AddResourceTemplate(
		mcp.NewResourceTemplate("aat://template/{adapter}", "Request Template",
			mcp.WithTemplateDescription("HTTP request template for a specific adapter"),
			mcp.WithTemplateMIMEType("text/markdown"),
		),
		s.handleTemplateResource,
	)

	s.mcp.AddResourceTemplate(
		mcp.NewResourceTemplate("aat://flow/{name}", "Integration Flow",
			mcp.WithTemplateDescription("Step-by-step integration flow showing HTTP methods, data flow, and required inputs"),
			mcp.WithTemplateMIMEType("text/markdown"),
		),
		s.handleWorkflowResource,
	)
}

// registerTestResources adds resources for the test persona.
func (s *Server) registerTestResources() {
	s.mcp.AddResource(
		mcp.NewResource("aat://graph/overview", "Graph Overview",
			mcp.WithResourceDescription("Compact overview of all graph nodes: name, adapter, and description"),
			mcp.WithMIMEType("text/markdown"),
		),
		s.handleGraphOverviewResource,
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

	if s.ctx.ReadmeContent != "" {
		s.mcp.AddResource(
			mcp.NewResource("aat://readme", "Project README",
				mcp.WithResourceDescription("Project README.md documentation from the graph directory"),
				mcp.WithMIMEType("text/markdown"),
			),
			s.handleReadmeResource,
		)
	}

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

	s.mcp.AddResourceTemplate(
		mcp.NewResourceTemplate("aat://workflow/{name}", "Workflow Detail",
			mcp.WithTemplateDescription("Enriched step-by-step recipe for a workflow showing HTTP methods, data flow, and inputs"),
			mcp.WithTemplateMIMEType("text/markdown"),
		),
		s.handleWorkflowResource,
	)
}

// handleAPIOverviewResource returns a compact one-liner-per-operation summary.
func (s *Server) handleAPIOverviewResource(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	g := s.ctx.Graph
	names := sortedNodeNames(g)

	var b strings.Builder
	fmt.Fprintf(&b, "# API Operations (%d)\n\n", len(names))

	for _, name := range names {
		node := g.Nodes[name]
		method := ""
		path := ""

		// Try to get HTTP method/path from template
		if node.Adapter != "" {
			if tmpl, ok := s.ctx.Registry.GetTemplate(node.Adapter); ok {
				method = tmpl.Request.Method
				path = tmpl.Request.Path
			}
		}

		if method != "" {
			fmt.Fprintf(&b, "- **%s** %s `%s`", name, method, path)
		} else {
			fmt.Fprintf(&b, "- **%s**", name)
		}
		if node.Description != "" {
			fmt.Fprintf(&b, " — %s", node.Description)
		}
		b.WriteString("\n")
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     b.String(),
		},
	}, nil
}

// handleGraphOverviewResource returns a compact one-liner-per-node summary.
func (s *Server) handleGraphOverviewResource(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	g := s.ctx.Graph
	names := sortedNodeNames(g)

	var b strings.Builder
	fmt.Fprintf(&b, "# Graph Nodes (%d)\n\n", len(names))

	for _, name := range names {
		node := g.Nodes[name]
		fmt.Fprintf(&b, "- **%s** [%s]", name, node.Adapter)
		if node.Description != "" {
			fmt.Fprintf(&b, " — %s", node.Description)
		}
		fmt.Fprintf(&b, " (%d in, %d out)", len(node.Inputs), len(node.Outputs))
		b.WriteString("\n")
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     b.String(),
		},
	}, nil
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

	g := s.ctx.Graph

	if g.Title != "" {
		fmt.Fprintf(&b, "**Graph Title:** %s\n", g.Title)
	}
	if g.Description != "" {
		fmt.Fprintf(&b, "**Graph Description:** %s\n", strings.TrimRight(g.Description, "\n"))
	}
	if len(g.Workflows) > 0 {
		b.WriteString("**Workflows:** ")
		names := make([]string, len(g.Workflows))
		for i, wf := range g.Workflows {
			names[i] = wf.Name
		}
		b.WriteString(strings.Join(names, ", "))
		b.WriteString("\n")
	}
	if g.Notes != "" {
		fmt.Fprintf(&b, "**Notes:** %s\n", strings.TrimRight(g.Notes, "\n"))
	}
	b.WriteString("\n")

	b.WriteString("## Graph Statistics\n\n")
	fmt.Fprintf(&b, "**Version:** %s\n", g.Version)
	fmt.Fprintf(&b, "**Nodes:** %d\n", len(g.Nodes))
	fmt.Fprintf(&b, "**Conditions:** %d\n", len(g.Conditions))

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     b.String(),
		},
	}, nil
}

// handleReadmeResource returns the project README.md content.
func (s *Server) handleReadmeResource(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     s.ctx.ReadmeContent,
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

// handleWorkflowResource returns an enriched step-by-step recipe for a specific workflow.
func (s *Server) handleWorkflowResource(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	name, err := extractURIParam(req, "name")
	if err != nil {
		return nil, err
	}

	wf, found := findWorkflowByName(s.ctx.Graph, name)
	if !found {
		return nil, fmt.Errorf("unknown workflow %q", name)
	}
	if wf.Template == "" {
		return nil, fmt.Errorf("workflow %q has no template", name)
	}

	p, err := intent.LoadWorkflowTemplate(wf.Template, s.ctx.GraphDir, s.ctx.Graph)
	if err != nil {
		return nil, fmt.Errorf("loading workflow template: %w", err)
	}

	content := formatWorkflowDetail(wf, p, s.ctx.Graph, s.ctx.Registry, false)
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
