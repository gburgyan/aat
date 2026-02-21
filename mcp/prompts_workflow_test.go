package mcp

import (
	"encoding/json"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- integration_guide ---

func TestIntegrationGuide_Basic(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	result, err := callPrompt(t, srv.handleIntegrationGuide, map[string]string{"goal": "book"})
	require.NoError(t, err)

	require.Len(t, result.Messages, 2)

	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "Execution Chain")
	assert.Contains(t, ctxText, "search")
	assert.Contains(t, ctxText, "book")

	instrText := result.Messages[1].Content.(mcp.TextContent).Text
	assert.Contains(t, instrText, "book")
	assert.Contains(t, instrText, "Prerequisites")
	assert.Contains(t, instrText, "Error handling")
}

func TestIntegrationGuide_WithTemplate(t *testing.T) {
	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("searchTpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "searchTpl",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "POST", Path: "/api/search"},
	})))

	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name:      "search",
				Adapter:   "searchTpl",
				Satisfies: []string{"searchResults"},
				Inputs:    []graph.Input{{Name: "q", Type: "string"}},
				Outputs:   []graph.Output{{Name: "results", Type: "result[]"}},
			},
			"book": {
				Name:     "book",
				Requires: []string{"searchResults"},
				Inputs:   []graph.Input{{Name: "resultId", Type: "string"}},
			},
		},
		SatisfiersByToken: map[string][]string{
			"searchResults": {"search"},
		},
	}
	ctx := &ServerContext{
		Graph:    g,
		Registry: reg,
		Manifest: &ProjectManifest{Name: "test"},
	}
	srv := NewServer(ctx)

	result, err := callPrompt(t, srv.handleIntegrationGuide, map[string]string{"goal": "book"})
	require.NoError(t, err)

	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "**Method:** POST")
	assert.Contains(t, ctxText, "/api/search")
}

func TestIntegrationGuide_WithDomain(t *testing.T) {
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

	result, err := callPrompt(t, srv.handleIntegrationGuide, map[string]string{"goal": "book"})
	require.NoError(t, err)

	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "booking")
}

func TestIntegrationGuide_WithDocs(t *testing.T) {
	g := twoNodeGraph()
	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		NodeDocs: map[string]string{
			"search": "## Search API\n\nSearch for available flights.",
		},
	}
	srv := NewServer(ctx)

	result, err := callPrompt(t, srv.handleIntegrationGuide, map[string]string{"goal": "book"})
	require.NoError(t, err)

	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "Search API")
	assert.Contains(t, ctxText, "Documentation")
}

func TestIntegrationGuide_UnknownGoal(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	_, err := callPrompt(t, srv.handleIntegrationGuide, map[string]string{"goal": "nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown goal node")
}

func TestIntegrationGuide_MissingGoal(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	_, err := callPrompt(t, srv.handleIntegrationGuide, map[string]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument")
}

func TestIntegrationGuide_MessageStructure(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	result, err := callPrompt(t, srv.handleIntegrationGuide, map[string]string{"goal": "book"})
	require.NoError(t, err)

	assert.Contains(t, result.Description, "book")
	require.Len(t, result.Messages, 2)
	assert.Equal(t, mcp.RoleUser, result.Messages[0].Role)
	assert.Equal(t, mcp.RoleUser, result.Messages[1].Role)
}

// --- test_workflow ---

func TestTestWorkflow_Basic(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	result, err := callPrompt(t, srv.handleTestWorkflow, map[string]string{"description": "book a flight"})
	require.NoError(t, err)

	require.Len(t, result.Messages, 2)

	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "search")
	assert.Contains(t, ctxText, "book")
	assert.Contains(t, ctxText, "API Graph Overview")

	instrText := result.Messages[1].Content.(mcp.TextContent).Text
	assert.Contains(t, instrText, "book a flight")
	assert.Contains(t, instrText, "generate_plan")
}

func TestTestWorkflow_WithDomain(t *testing.T) {
	g := twoNodeGraph()
	kb := &domain.KnowledgeBase{
		Concepts: map[string]*domain.Concept{
			"fare": {Name: "fare", Description: "Price of a ticket"},
		},
	}
	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		KB:       kb,
		Manifest: &ProjectManifest{Name: "test"},
	}
	srv := NewServer(ctx)

	result, err := callPrompt(t, srv.handleTestWorkflow, map[string]string{"description": "test fares"})
	require.NoError(t, err)

	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "fare")
}

func TestTestWorkflow_CapabilitiesFull(t *testing.T) {
	g := twoNodeGraph()
	ctx := &ServerContext{
		Graph:       g,
		Registry:    adapter.NewRegistry(),
		Manifest:    &ProjectManifest{Name: "test"},
		WorkflowsDir:    "/tmp/plans",
		ArchiveDir:  "/tmp/archives",
		Environment: &config.Environment{Name: "test"},
	}
	srv := NewServer(ctx)

	result, err := callPrompt(t, srv.handleTestWorkflow, map[string]string{"description": "test flow"})
	require.NoError(t, err)

	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "save_plan")
	assert.Contains(t, ctxText, "execute_plan")
	assert.Contains(t, ctxText, "inspect_archive")
	assert.Contains(t, ctxText, "analyze_failure")
}

func TestTestWorkflow_CapabilitiesMinimal(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g) // no WorkflowsDir, no ArchiveDir, no Environment
	result, err := callPrompt(t, srv.handleTestWorkflow, map[string]string{"description": "test flow"})
	require.NoError(t, err)

	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "generate_plan")
	assert.Contains(t, ctxText, "validate_plan")
	assert.NotContains(t, ctxText, "save_plan")
	assert.NotContains(t, ctxText, "execute_plan")
	assert.NotContains(t, ctxText, "inspect_archive")
}

func TestTestWorkflow_MissingDescription(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	_, err := callPrompt(t, srv.handleTestWorkflow, map[string]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument")
}

func TestTestWorkflow_MessageStructure(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)
	result, err := callPrompt(t, srv.handleTestWorkflow, map[string]string{"description": "test booking"})
	require.NoError(t, err)

	assert.Contains(t, result.Description, "test booking")
	require.Len(t, result.Messages, 2)
	assert.Equal(t, mcp.RoleUser, result.Messages[0].Role)
	assert.Equal(t, mcp.RoleUser, result.Messages[1].Role)
}

// --- debug_failing_test ---

func TestDebugFailingTest_FailedArchive(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()

	a := &archive.Archive{
		Metadata: archive.ArchiveMetadata{
			RunID: "run-20260210-140000-fail0001",
		},
		Steps: []archive.StepRecord{
			{
				Node:       "search",
				DurationMs: 100,
				Request:    &archive.RequestRecord{Method: "POST", URL: "https://api.example.com/search"},
				Response:   &archive.ResponseRecord{Status: 200, Body: json.RawMessage(`{"results":["a"]}`)},
				Outputs:    map[string]any{"results": []any{"a"}},
			},
			{
				Node:       "book",
				DurationMs: 200,
				Request:    &archive.RequestRecord{Method: "POST", URL: "https://api.example.com/book"},
				Response:   &archive.ResponseRecord{Status: 400, Body: json.RawMessage(`{"error":"bad request"}`)},
				Error:      "booking failed",
			},
		},
		Result: archive.ArchiveResult{Outcome: "failed"},
	}
	writeTestArchive(t, dir, "run-20260210-140000-fail0001", a)

	srv := newTestServerWithArchives(g, dir)
	result, err := callPrompt(t, srv.handleDebugFailingTest, map[string]string{"run_id": "run-20260210-140000-fail0001"})
	require.NoError(t, err)

	require.Len(t, result.Messages, 2)
	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "Failure Analysis")
	assert.Contains(t, ctxText, "book")
	assert.Contains(t, ctxText, "Failed Step Details")
}

func TestDebugFailingTest_PassedArchive(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()

	a := &archive.Archive{
		Metadata: archive.ArchiveMetadata{
			RunID: "run-20260210-140000-pass0001",
		},
		Steps: []archive.StepRecord{
			{
				Node:     "search",
				Response: &archive.ResponseRecord{Status: 200},
			},
		},
		Result: archive.ArchiveResult{Outcome: "passed"},
	}
	writeTestArchive(t, dir, "run-20260210-140000-pass0001", a)

	srv := newTestServerWithArchives(g, dir)
	result, err := callPrompt(t, srv.handleDebugFailingTest, map[string]string{"run_id": "run-20260210-140000-pass0001"})
	require.NoError(t, err)

	require.Len(t, result.Messages, 1)
	text := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, text, "passed")
	assert.Contains(t, text, "inspect_archive")
}

func TestDebugFailingTest_NoArchiveDir(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, "")
	_, err := callPrompt(t, srv.handleDebugFailingTest, map[string]string{"run_id": "run-123"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "archive directory not configured")
}

func TestDebugFailingTest_ArchiveNotFound(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)
	_, err := callPrompt(t, srv.handleDebugFailingTest, map[string]string{"run_id": "run-nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDebugFailingTest_MissingRunID(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)
	_, err := callPrompt(t, srv.handleDebugFailingTest, map[string]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument")
}

func TestDebugFailingTest_MessageStructure(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()

	a := &archive.Archive{
		Metadata: archive.ArchiveMetadata{RunID: "run-20260210-140000-struct"},
		Steps: []archive.StepRecord{
			{
				Node:     "search",
				Response: &archive.ResponseRecord{Status: 500},
				Error:    "server error",
			},
		},
		Result: archive.ArchiveResult{Outcome: "error"},
	}
	writeTestArchive(t, dir, "run-20260210-140000-struct", a)

	srv := newTestServerWithArchives(g, dir)
	result, err := callPrompt(t, srv.handleDebugFailingTest, map[string]string{"run_id": "run-20260210-140000-struct"})
	require.NoError(t, err)

	assert.Contains(t, result.Description, "run-20260210-140000-struct")
	require.Len(t, result.Messages, 2)
	assert.Equal(t, mcp.RoleUser, result.Messages[0].Role)
	assert.Equal(t, mcp.RoleUser, result.Messages[1].Role)
}

func TestDebugFailingTest_IncludesGraphContext(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()

	a := &archive.Archive{
		Metadata: archive.ArchiveMetadata{RunID: "run-20260210-140000-graph"},
		Steps: []archive.StepRecord{
			{
				Node:     "book",
				Response: &archive.ResponseRecord{Status: 400},
				Error:    "bad request",
			},
		},
		Result: archive.ArchiveResult{Outcome: "failed"},
	}
	writeTestArchive(t, dir, "run-20260210-140000-graph", a)

	srv := newTestServerWithArchives(g, dir)
	result, err := callPrompt(t, srv.handleDebugFailingTest, map[string]string{"run_id": "run-20260210-140000-graph"})
	require.NoError(t, err)

	ctxText := result.Messages[0].Content.(mcp.TextContent).Text
	assert.Contains(t, ctxText, "Graph Context for Failed Nodes")
	assert.Contains(t, ctxText, "# book")
	assert.Contains(t, ctxText, "Book a flight")
}
