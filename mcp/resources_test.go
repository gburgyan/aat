package mcp

import (
	"context"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// callResource invokes a resource handler and returns the text content.
func callResource(t *testing.T, handler func(context.Context, mcp.ReadResourceRequest) ([]mcp.ResourceContents, error), uri string, args map[string]any) (string, error) {
	t.Helper()
	req := mcp.ReadResourceRequest{}
	req.Params.URI = uri
	req.Params.Arguments = args
	contents, err := handler(context.Background(), req)
	if err != nil {
		return "", err
	}
	require.NotEmpty(t, contents)
	tc, ok := contents[0].(mcp.TextResourceContents)
	require.True(t, ok, "expected TextResourceContents, got %T", contents[0])
	return tc.Text, nil
}

// --- aat://graph ---

func TestGraphResource(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0",
		Nodes: map[string]*graph.Node{
			"search": {Name: "search", Description: "Search flights", Inputs: []graph.Input{{Name: "origin", Type: "string"}}},
			"book":   {Name: "book", Description: "Book flight", Inputs: []graph.Input{{Name: "id", Type: "string"}}},
		},
	}
	srv := newTestServer(g)
	text, err := callResource(t, srv.handleGraphResource, "aat://graph", nil)
	require.NoError(t, err)
	assert.Contains(t, text, "search")
	assert.Contains(t, text, "book")
	assert.Contains(t, text, "1.0")
}

func TestGraphResource_Empty(t *testing.T) {
	g := &graph.Graph{Version: "1.0", Nodes: map[string]*graph.Node{}}
	srv := newTestServer(g)
	text, err := callResource(t, srv.handleGraphResource, "aat://graph", nil)
	require.NoError(t, err)
	assert.Contains(t, text, "1.0")
}

// --- aat://templates ---

func TestTemplatesResource_WithTemplates(t *testing.T) {
	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("searchAPI", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "searchAPI",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "POST", Path: "/api/search"},
	})))
	require.NoError(t, reg.Register("custom", &mockAdapter{}))

	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: reg,
		Manifest: &ProjectManifest{Name: "test"},
	}
	srv := NewServer(ctx)

	text, err := callResource(t, srv.handleTemplatesResource, "aat://templates", nil)
	require.NoError(t, err)
	assert.Contains(t, text, "**custom** (non-template)")
	assert.Contains(t, text, "**searchAPI** — POST /api/search")
}

func TestTemplatesResource_Empty(t *testing.T) {
	srv := newTestServer(&graph.Graph{Nodes: map[string]*graph.Node{}})
	text, err := callResource(t, srv.handleTemplatesResource, "aat://templates", nil)
	require.NoError(t, err)
	assert.Contains(t, text, "No adapters registered")
}

// --- aat://domain ---

func TestDomainResource_WithKB(t *testing.T) {
	kb := &domain.KnowledgeBase{
		Concepts: map[string]*domain.Concept{
			"flight": {Name: "flight", Description: "A scheduled flight"},
		},
	}
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: adapter.NewRegistry(),
		KB:       kb,
		Manifest: &ProjectManifest{Name: "test"},
	}
	srv := NewServer(ctx)

	text, err := callResource(t, srv.handleDomainResource, "aat://domain", nil)
	require.NoError(t, err)
	assert.Contains(t, text, "flight")
}

func TestDomainResource_NilKB(t *testing.T) {
	srv := newTestServer(&graph.Graph{Nodes: map[string]*graph.Node{}})
	text, err := callResource(t, srv.handleDomainResource, "aat://domain", nil)
	require.NoError(t, err)
	assert.Contains(t, text, "not configured")
}

// --- aat://metadata ---

func TestMetadataResource(t *testing.T) {
	g := &graph.Graph{
		Version: "2.0",
		Nodes: map[string]*graph.Node{
			"a": {Name: "a"},
			"b": {Name: "b"},
		},
		Conditions: []graph.Condition{{When: "true"}},
	}
	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{
			Name:        "my-project",
			Description: "A test project",
			Tags:        []string{"api", "test"},
		},
	}
	srv := NewServer(ctx)

	text, err := callResource(t, srv.handleMetadataResource, "aat://metadata", nil)
	require.NoError(t, err)
	assert.Contains(t, text, "**Name:** my-project")
	assert.Contains(t, text, "**Description:** A test project")
	assert.Contains(t, text, "api, test")
	assert.Contains(t, text, "**Version:** 2.0")
	assert.Contains(t, text, "**Nodes:** 2")
	assert.Contains(t, text, "**Conditions:** 1")
}

func TestMetadataResource_NilManifest(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0",
		Nodes:   map[string]*graph.Node{"x": {Name: "x"}},
	}
	// Build server context without manifest — but NewServer sets name from manifest,
	// so use a minimal manifest without description/tags.
	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "bare"},
	}
	srv := NewServer(ctx)

	text, err := callResource(t, srv.handleMetadataResource, "aat://metadata", nil)
	require.NoError(t, err)
	assert.Contains(t, text, "**Nodes:** 1")
	assert.Contains(t, text, "**Version:** 1.0")
}

// --- aat://readme ---

func TestReadmeResource_Present(t *testing.T) {
	ctx := &ServerContext{
		Graph:         &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry:      adapter.NewRegistry(),
		Manifest:      &ProjectManifest{Name: "test"},
		ReadmeContent: "# My Project\n\nThis is the project README.",
	}
	srv := NewServer(ctx)

	text, err := callResource(t, srv.handleReadmeResource, "aat://readme", nil)
	require.NoError(t, err)
	assert.Equal(t, "# My Project\n\nThis is the project README.", text)
}

func TestReadmeResource_NotRegisteredWhenEmpty(t *testing.T) {
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		// ReadmeContent left empty
	}
	srv := NewServer(ctx)

	// The handler still works if called directly, but the resource
	// should not be registered. Verify by checking the handler returns empty content.
	text, err := callResource(t, srv.handleReadmeResource, "aat://readme", nil)
	require.NoError(t, err)
	assert.Empty(t, text)
}

// --- aat://node/{name} ---

func TestNodeResource_Valid(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name:        "search",
				Description: "Search flights",
				Adapter:     "searchTemplate",
				Inputs:      []graph.Input{{Name: "origin", Type: "string"}},
				Outputs:     []graph.Output{{Name: "results", Type: "result[]"}},
			},
		},
	}
	srv := newTestServer(g)

	text, err := callResource(t, srv.handleNodeResource, "aat://node/search", map[string]any{"name": "search"})
	require.NoError(t, err)
	assert.Contains(t, text, "# search")
	assert.Contains(t, text, "Search flights")
	assert.Contains(t, text, "**Adapter:** searchTemplate")
}

func TestNodeResource_Unknown(t *testing.T) {
	srv := newTestServer(&graph.Graph{Nodes: map[string]*graph.Node{}})
	_, err := callResource(t, srv.handleNodeResource, "aat://node/nope", map[string]any{"name": "nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown node")
}

func TestNodeResource_MissingArg(t *testing.T) {
	srv := newTestServer(&graph.Graph{Nodes: map[string]*graph.Node{}})
	_, err := callResource(t, srv.handleNodeResource, "aat://node/", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required parameter")
}

// --- aat://template/{adapter} ---

func TestTemplateResource_Valid(t *testing.T) {
	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("searchAPI", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "searchAPI",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "POST", Path: "/api/search"},
	})))

	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: reg,
		Manifest: &ProjectManifest{Name: "test"},
	}
	srv := NewServer(ctx)

	text, err := callResource(t, srv.handleTemplateResource, "aat://template/searchAPI", map[string]any{"adapter": "searchAPI"})
	require.NoError(t, err)
	assert.Contains(t, text, "# searchAPI")
	assert.Contains(t, text, "**Method:** POST")
}

func TestTemplateResource_Unknown(t *testing.T) {
	srv := newTestServer(&graph.Graph{Nodes: map[string]*graph.Node{}})
	_, err := callResource(t, srv.handleTemplateResource, "aat://template/nope", map[string]any{"adapter": "nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown adapter")
}

func TestTemplateResource_NonTemplate(t *testing.T) {
	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("custom", &mockAdapter{}))

	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: reg,
		Manifest: &ProjectManifest{Name: "test"},
	}
	srv := NewServer(ctx)

	_, err := callResource(t, srv.handleTemplateResource, "aat://template/custom", map[string]any{"adapter": "custom"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not template-based")
}

func TestTemplateResource_MissingArg(t *testing.T) {
	srv := newTestServer(&graph.Graph{Nodes: map[string]*graph.Node{}})
	_, err := callResource(t, srv.handleTemplateResource, "aat://template/", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required parameter")
}

// --- aat://workflow/{name} ---

func TestWorkflowResource_Valid(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{
				Name:        "Test Booking",
				Description: "Book a test flight",
				Template:    "testdata/workflows/booking.yaml",
			},
		},
		Nodes: map[string]*graph.Node{
			"search": {
				Name:    "search",
				Adapter: "searchTmpl",
				Inputs:  []graph.Input{{Name: "origin", Type: "string"}, {Name: "destination", Type: "string"}},
				Outputs: []graph.Output{{Name: "results", Type: "flight[]"}},
			},
			"book": {
				Name:    "book",
				Adapter: "bookTmpl",
				Inputs:  []graph.Input{{Name: "flightId", Type: "string"}},
				Outputs: []graph.Output{{Name: "workbenchId", Type: "string"}},
			},
			"commit": {
				Name:    "commit",
				Adapter: "commitTmpl",
				Inputs:  []graph.Input{{Name: "workbenchId", Type: "string"}},
				Outputs: []graph.Output{{Name: "locator", Type: "string"}},
			},
			"cancel": {Name: "cancel", Adapter: "cancelTmpl"},
		},
	}

	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("searchTmpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "searchTmpl", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "POST", Path: "/search"},
	})))
	require.NoError(t, reg.Register("bookTmpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "bookTmpl", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "POST", Path: "/book"},
	})))
	require.NoError(t, reg.Register("commitTmpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "commitTmpl", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "POST", Path: "/commit"},
	})))
	require.NoError(t, reg.Register("cancelTmpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "cancelTmpl", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "DELETE", Path: "/cancel"},
	})))

	ctx := &ServerContext{
		Graph:    g,
		Registry: reg,
		Manifest: &ProjectManifest{Name: "test"},
		GraphDir: ".",
	}
	srv := NewServer(ctx)

	text, err := callResource(t, srv.handleWorkflowResource, "aat://workflow/Test Booking", map[string]any{"name": "Test Booking"})
	require.NoError(t, err)
	assert.Contains(t, text, "# Workflow: Test Booking")
	assert.Contains(t, text, "## Steps (3)")
	assert.Contains(t, text, "### 1. search")
	assert.Contains(t, text, "**HTTP:** POST /search")
	assert.Contains(t, text, "## Data Flow")
	assert.Contains(t, text, "## Cleanup")
}

func TestWorkflowResource_Unknown(t *testing.T) {
	srv := newTestServer(&graph.Graph{Nodes: map[string]*graph.Node{}})
	_, err := callResource(t, srv.handleWorkflowResource, "aat://workflow/nope", map[string]any{"name": "nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown workflow")
}

func TestWorkflowResource_NoTemplate(t *testing.T) {
	g := &graph.Graph{
		Workflows: []graph.Workflow{{Name: "NoTemplate"}},
		Nodes:     map[string]*graph.Node{"a": {Name: "a"}},
	}
	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		GraphDir: ".",
	}
	srv := NewServer(ctx)

	_, err := callResource(t, srv.handleWorkflowResource, "aat://workflow/NoTemplate", map[string]any{"name": "NoTemplate"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no template")
}

func TestWorkflowResource_MissingArg(t *testing.T) {
	srv := newTestServer(&graph.Graph{Nodes: map[string]*graph.Node{}})
	_, err := callResource(t, srv.handleWorkflowResource, "aat://workflow/", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required parameter")
}
