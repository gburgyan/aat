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

// newTestServerWithDocs creates a Server with NodeDocs and optional DocsDir.
func newTestServerWithDocs(g *graph.Graph, docs map[string]string, docsDir string) *Server {
	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		NodeDocs: docs,
		DocsDir:  docsDir,
	}
	return NewServer(ctx)
}

// --- get_node_documentation ---

func TestHandleGetNodeDocumentation_WithDocs(t *testing.T) {
	g := twoNodeGraph()
	docs := map[string]string{
		"search": "Search documentation content here.",
	}
	srv := newTestServerWithDocs(g, docs, "/docs")

	result := callTool(t, srv.handleGetNodeDocumentation, map[string]any{"node": "search"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "# search")
	assert.Contains(t, text, "## Documentation")
	assert.Contains(t, text, "Search documentation content here.")
}

func TestHandleGetNodeDocumentation_NoDocs(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, nil, "")

	result := callTool(t, srv.handleGetNodeDocumentation, map[string]any{"node": "search"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "# search")
	assert.NotContains(t, text, "## Documentation")
}

func TestHandleGetNodeDocumentation_UnknownNode(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, nil, "")

	result := callTool(t, srv.handleGetNodeDocumentation, map[string]any{"node": "nope"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "unknown node")
}

func TestHandleGetNodeDocumentation_MissingParam(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, nil, "")

	result := callTool(t, srv.handleGetNodeDocumentation, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

func TestHandleGetNodeDocumentation_NilNodeDocs(t *testing.T) {
	g := twoNodeGraph()
	// NodeDocs is nil (not configured) — should still return graph metadata
	srv := newTestServerWithDocs(g, nil, "")

	result := callTool(t, srv.handleGetNodeDocumentation, map[string]any{"node": "book"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "# book")
	assert.Contains(t, text, "Book a flight")
}

// --- get_workflow_documentation ---

func TestHandleGetWorkflowDocumentation_TwoNodeChain(t *testing.T) {
	g := twoNodeGraph()
	docs := map[string]string{
		"search": "Search node docs.",
	}
	srv := newTestServerWithDocs(g, docs, "/docs")

	result := callTool(t, srv.handleGetWorkflowDocumentation, map[string]any{"goal": "book"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Workflow Documentation: book")
	assert.Contains(t, text, "search → book")
	assert.Contains(t, text, "# search")
	assert.Contains(t, text, "# book")
	// search has docs
	assert.Contains(t, text, "## Documentation")
	assert.Contains(t, text, "Search node docs.")
}

func TestHandleGetWorkflowDocumentation_PartialDocs(t *testing.T) {
	g := twoNodeGraph()
	docs := map[string]string{
		"search": "Search docs only.",
	}
	srv := newTestServerWithDocs(g, docs, "/docs")

	result := callTool(t, srv.handleGetWorkflowDocumentation, map[string]any{"goal": "book"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	// search has docs, book does not
	assert.Contains(t, text, "Search docs only.")
	// book section exists but without ## Documentation
	assert.Contains(t, text, "# book")
}

func TestHandleGetWorkflowDocumentation_UnknownGoal(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, nil, "")

	result := callTool(t, srv.handleGetWorkflowDocumentation, map[string]any{"goal": "nope"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "unknown goal node")
}

func TestHandleGetWorkflowDocumentation_MissingParam(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, nil, "")

	result := callTool(t, srv.handleGetWorkflowDocumentation, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

// --- generate_doc_stub ---

func TestHandleGenerateDocStub_Basic(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, nil, "")

	result := callTool(t, srv.handleGenerateDocStub, map[string]any{"node": "search"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "# search")
	assert.Contains(t, text, "Search for flights")
	assert.Contains(t, text, "## Inputs")
	assert.Contains(t, text, "## Outputs")
	assert.Contains(t, text, "## Overview")
	assert.Contains(t, text, "## Usage Notes")
	assert.Contains(t, text, "## Examples")
}

func TestHandleGenerateDocStub_UnknownNode(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, nil, "")

	result := callTool(t, srv.handleGenerateDocStub, map[string]any{"node": "nope"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "unknown node")
}

func TestHandleGenerateDocStub_MissingParam(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, nil, "")

	result := callTool(t, srv.handleGenerateDocStub, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

// --- list_undocumented_nodes ---

func TestHandleListUndocumentedNodes_SomeUndocumented(t *testing.T) {
	g := twoNodeGraph()
	docs := map[string]string{
		"search": "documented",
	}
	srv := newTestServerWithDocs(g, docs, "/docs")

	result := callTool(t, srv.handleListUndocumentedNodes, nil)
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "1 of 2")
	assert.Contains(t, text, "**book**")
	assert.NotContains(t, text, "**search**")
	assert.Contains(t, text, "generate_doc_stub")
}

func TestHandleListUndocumentedNodes_AllDocumented(t *testing.T) {
	g := twoNodeGraph()
	docs := map[string]string{
		"search": "documented",
		"book":   "documented",
	}
	srv := newTestServerWithDocs(g, docs, "/docs")

	result := callTool(t, srv.handleListUndocumentedNodes, nil)
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "All 2 nodes are documented")
}

func TestHandleListUndocumentedNodes_NoneDocumented(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, map[string]string{}, "/docs")

	result := callTool(t, srv.handleListUndocumentedNodes, nil)
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "2 of 2")
	assert.Contains(t, text, "**search**")
	assert.Contains(t, text, "**book**")
}

func TestHandleListUndocumentedNodes_DocsDirNotConfigured(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, nil, "")

	result := callTool(t, srv.handleListUndocumentedNodes, nil)
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "not configured")
	assert.Contains(t, text, "aat-project.yaml")
}

func TestHandleListUndocumentedNodes_WithDescriptions(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, map[string]string{}, "/docs")

	result := callTool(t, srv.handleListUndocumentedNodes, nil)
	assert.False(t, result.IsError)
	text := resultText(t, result)
	// Both nodes have descriptions set in twoNodeGraph
	assert.Contains(t, text, "Search for flights")
	assert.Contains(t, text, "Book a flight")
}

// --- enrich_documentation prompt ---

func TestEnrichDocumentation_WithExistingDoc(t *testing.T) {
	g := twoNodeGraph()
	docs := map[string]string{
		"search": "Existing search documentation.",
	}
	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		NodeDocs: docs,
		DocsDir:  "/docs",
	}
	srv := NewServer(ctx)

	result, err := callPrompt(t, srv.handleEnrichDocumentation, map[string]string{"node": "search"})
	require.NoError(t, err)

	require.Len(t, result.Messages, 2)
	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "# search")
	assert.Contains(t, ctxText, "Existing Documentation")
	assert.Contains(t, ctxText, "Existing search documentation.")

	instrText := result.Messages[1].Content.(mcp.TextContent).Text
	assert.Contains(t, instrText, "Enrich")
	assert.Contains(t, instrText, "search")
}

func TestEnrichDocumentation_WithoutDoc(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, nil, "")

	result, err := callPrompt(t, srv.handleEnrichDocumentation, map[string]string{"node": "search"})
	require.NoError(t, err)

	require.Len(t, result.Messages, 2)
	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "# search")
	assert.NotContains(t, ctxText, "Existing Documentation")

	instrText := result.Messages[1].Content.(mcp.TextContent).Text
	assert.Contains(t, instrText, "Create")
	assert.Contains(t, instrText, "search")
}

func TestEnrichDocumentation_WithDomain(t *testing.T) {
	g := twoNodeGraph()
	kb := &domain.KnowledgeBase{
		Concepts: map[string]*domain.Concept{
			"flight": {Name: "flight", Description: "A flight booking"},
		},
	}
	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		KB:       kb,
	}
	srv := NewServer(ctx)

	result, err := callPrompt(t, srv.handleEnrichDocumentation, map[string]string{"node": "search"})
	require.NoError(t, err)

	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "flight")
}

func TestEnrichDocumentation_ConnectedNodes(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, nil, "")

	result, err := callPrompt(t, srv.handleEnrichDocumentation, map[string]string{"node": "book"})
	require.NoError(t, err)

	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "Connected Nodes")
	assert.Contains(t, ctxText, "Receives data from")
	assert.Contains(t, ctxText, "search")
}

func TestEnrichDocumentation_UnknownNode(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, nil, "")

	_, err := callPrompt(t, srv.handleEnrichDocumentation, map[string]string{"node": "nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown node")
}

func TestEnrichDocumentation_MissingArg(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, nil, "")

	_, err := callPrompt(t, srv.handleEnrichDocumentation, map[string]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument")
}

func TestEnrichDocumentation_Description(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithDocs(g, nil, "")

	req := mcp.GetPromptRequest{}
	req.Params.Arguments = map[string]string{"node": "search"}
	result, err := srv.handleEnrichDocumentation(context.Background(), req)
	require.NoError(t, err)
	assert.Contains(t, result.Description, "search")
}
