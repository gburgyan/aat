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

// callPrompt invokes a prompt handler and returns the result.
func callPrompt(t *testing.T, handler func(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error), args map[string]string) (*mcp.GetPromptResult, error) {
	t.Helper()
	req := mcp.GetPromptRequest{}
	req.Params.Arguments = args
	return handler(context.Background(), req)
}

func twoNodeGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0",
		Nodes: map[string]*graph.Node{
			"search": {
				Name:        "search",
				Description: "Search for flights",
				Inputs:      []graph.Input{{Name: "origin", Type: "string"}, {Name: "dest", Type: "string"}},
				Outputs:     []graph.Output{{Name: "results", Type: "result[]"}},
			},
			"book": {
				Name:        "book",
				Description: "Book a flight",
				Inputs:      []graph.Input{{Name: "resultId", Type: "string"}},
				Outputs:     []graph.Output{{Name: "locator", Type: "string"}},
			},
		},
		Edges: []graph.Edge{
			{From: "search.results", To: "book.resultId", Select: true},
		},
	}
}

// --- explain_workflow ---

func TestExplainWorkflow_Basic(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	result, err := callPrompt(t, srv.handleExplainWorkflow, map[string]string{"goal": "book"})
	require.NoError(t, err)

	require.Len(t, result.Messages, 2)

	// Context message should contain chain trace
	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "Execution Chain")
	assert.Contains(t, ctxText, "search")
	assert.Contains(t, ctxText, "book")

	// Instruction message
	instrText := result.Messages[1].Content.(mcp.TextContent).Text
	assert.Contains(t, instrText, "book")
	assert.Contains(t, instrText, "data flow")
}

func TestExplainWorkflow_WithDomain(t *testing.T) {
	g := twoNodeGraph()
	kb := &domain.KnowledgeBase{
		Concepts: map[string]*domain.Concept{
			"booking": {Name: "booking", Description: "A flight reservation"},
		},
	}
	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		KB:       kb,
		Manifest: &ProjectManifest{Name: "test"},
	}
	srv := NewServer(ctx)

	result, err := callPrompt(t, srv.handleExplainWorkflow, map[string]string{"goal": "book"})
	require.NoError(t, err)

	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "booking")
}

func TestExplainWorkflow_NoDomain(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g) // no KB
	result, err := callPrompt(t, srv.handleExplainWorkflow, map[string]string{"goal": "book"})
	require.NoError(t, err)

	// Should still work without domain
	require.Len(t, result.Messages, 2)
	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "Execution Chain")
}

func TestExplainWorkflow_UnknownGoal(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	_, err := callPrompt(t, srv.handleExplainWorkflow, map[string]string{"goal": "nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown goal node")
}

func TestExplainWorkflow_MissingGoal(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	_, err := callPrompt(t, srv.handleExplainWorkflow, map[string]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument")
}

func TestExplainWorkflow_MessageStructure(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	result, err := callPrompt(t, srv.handleExplainWorkflow, map[string]string{"goal": "book"})
	require.NoError(t, err)

	assert.Contains(t, result.Description, "book")
	require.Len(t, result.Messages, 2)
	assert.Equal(t, mcp.RoleUser, result.Messages[0].Role)
	assert.Equal(t, mcp.RoleUser, result.Messages[1].Role)
}

// --- generate_client_code ---

func TestGenerateClientCode_Basic(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	result, err := callPrompt(t, srv.handleGenerateClientCode, map[string]string{"node": "search"})
	require.NoError(t, err)

	require.Len(t, result.Messages, 2)
	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "# search")
	assert.Contains(t, ctxText, "Search for flights")
}

func TestGenerateClientCode_WithTemplate(t *testing.T) {
	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("searchTemplate", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "searchTemplate",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "POST", Path: "/api/search"},
	})))

	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name:    "search",
				Adapter: "searchTemplate",
				Inputs:  []graph.Input{{Name: "q", Type: "string"}},
			},
		},
	}
	ctx := &ServerContext{
		Graph:    g,
		Registry: reg,
		Manifest: &ProjectManifest{Name: "test"},
	}
	srv := NewServer(ctx)

	result, err := callPrompt(t, srv.handleGenerateClientCode, map[string]string{"node": "search"})
	require.NoError(t, err)

	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "**Method:** POST")
	assert.Contains(t, ctxText, "/api/search")
}

func TestGenerateClientCode_DefaultLanguage(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	result, err := callPrompt(t, srv.handleGenerateClientCode, map[string]string{"node": "search"})
	require.NoError(t, err)

	instrText := result.Messages[1].Content.(mcp.TextContent).Text
	assert.Contains(t, instrText, "go")
	assert.Contains(t, result.Description, "go")
}

func TestGenerateClientCode_CustomLanguage(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	result, err := callPrompt(t, srv.handleGenerateClientCode, map[string]string{"node": "search", "language": "python"})
	require.NoError(t, err)

	instrText := result.Messages[1].Content.(mcp.TextContent).Text
	assert.Contains(t, instrText, "python")
	assert.Contains(t, result.Description, "python")
}

func TestGenerateClientCode_UnknownNode(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	_, err := callPrompt(t, srv.handleGenerateClientCode, map[string]string{"node": "nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown node")
}

func TestGenerateClientCode_MissingNode(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	_, err := callPrompt(t, srv.handleGenerateClientCode, map[string]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument")
}
