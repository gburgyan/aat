package mcp

import (
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// workflowTestGraph creates a graph with workflows including addons for testing.
func workflowTestGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{
				Name:     "Full-Payload Booking",
				Template: "workflows/booking.yaml",
			},
			{
				Name:        "Seat Selection",
				Kind:        "addon",
				Description: "Select a seat",
				Template:    "workflows/seat.yaml",
				After:       graph.AfterSpec{"book"},
				Wire:        map[string]string{"workbenchId": "book.workbenchId"},
			},
		},
		Nodes: map[string]*graph.Node{
			"search":     {Name: "search", Adapter: "search", Outputs: []graph.Output{{Name: "results", Type: "string[]"}}},
			"book":       {Name: "book", Adapter: "book", Inputs: []graph.Input{{Name: "flightId", Type: "string"}}, Outputs: []graph.Output{{Name: "workbenchId", Type: "string"}}},
			"commit":     {Name: "commit", Adapter: "commit", Inputs: []graph.Input{{Name: "workbenchId", Type: "string"}}, Outputs: []graph.Output{{Name: "locator", Type: "string"}}},
			"searchSeat": {Name: "searchSeat", Adapter: "searchSeat", Inputs: []graph.Input{{Name: "workbenchId", Type: "string"}}, Outputs: []graph.Output{{Name: "seats", Type: "string[]"}}},
			"addSeat":    {Name: "addSeat", Adapter: "addSeat", Inputs: []graph.Input{{Name: "workbenchId", Type: "string"}, {Name: "seatId", Type: "string"}}, Outputs: []graph.Output{{Name: "confirmId", Type: "string"}}},
		},
	}
}

func newWorkflowTestServer(g *graph.Graph) *Server {
	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		GraphDir: ".",
	}
	return NewServer(ctx)
}

// --- list_workflows ---

func TestHandleListWorkflows_Empty(t *testing.T) {
	g := &graph.Graph{Nodes: map[string]*graph.Node{}}
	srv := newWorkflowTestServer(g)
	result := callTool(t, srv.handleListWorkflows, nil)
	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "No workflows")
}

func TestHandleListWorkflows_WithWorkflows(t *testing.T) {
	g := workflowTestGraph()
	srv := newWorkflowTestServer(g)
	result := callTool(t, srv.handleListWorkflows, nil)
	require.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Full-Payload Booking")
	assert.Contains(t, text, "Seat Selection")
	assert.Contains(t, text, "[addon]")
	assert.Contains(t, text, "After: `book`")
}

func TestHandleListWorkflows_WithStepSummary(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{
				Name:     "Test Booking",
				Template: "testdata/workflows/booking.yaml",
			},
		},
		Nodes: map[string]*graph.Node{
			"search": {Name: "search", Adapter: "search", Outputs: []graph.Output{{Name: "results", Type: "string[]"}}},
			"book":   {Name: "book", Adapter: "book", Inputs: []graph.Input{{Name: "flightId", Type: "string"}}, Outputs: []graph.Output{{Name: "workbenchId", Type: "string"}}},
			"commit": {Name: "commit", Adapter: "commit", Inputs: []graph.Input{{Name: "workbenchId", Type: "string"}}, Outputs: []graph.Output{{Name: "locator", Type: "string"}}},
			"cancel": {Name: "cancel", Adapter: "cancel"},
		},
	}
	// Use mcp package directory as the graph dir so the relative path works.
	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		GraphDir: ".",
	}
	srv := NewServer(ctx)
	result := callTool(t, srv.handleListWorkflows, nil)
	require.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Test Booking")
	assert.Contains(t, text, "Steps: 3")
	assert.Contains(t, text, "search → book → commit")
}

// --- get_workflow_detail ---

func TestHandleGetWorkflowDetail_Valid(t *testing.T) {
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
				Name:        "search",
				Description: "Search for flights",
				Adapter:     "searchTmpl",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "destination", Type: "string"},
				},
				Outputs: []graph.Output{
					{
						Name: "results",
						Type: "flight[]",
						ElementFields: []graph.Field{
							{Name: "id", Type: "string"},
							{Name: "carrier", Type: "string"},
						},
					},
				},
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
			"cancel": {
				Name:    "cancel",
				Adapter: "cancelTmpl",
			},
		},
	}

	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("searchTmpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "searchTmpl", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "POST", Path: "/api/flights/search"},
	})))
	require.NoError(t, reg.Register("bookTmpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "bookTmpl", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "POST", Path: "/api/book"},
	})))
	require.NoError(t, reg.Register("commitTmpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "commitTmpl", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "POST", Path: "/api/commit"},
	})))
	require.NoError(t, reg.Register("cancelTmpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "cancelTmpl", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "DELETE", Path: "/api/cancel"},
	})))

	ctx := &ServerContext{
		Graph:    g,
		Registry: reg,
		Manifest: &ProjectManifest{Name: "test"},
		GraphDir: ".",
	}
	srv := NewServer(ctx)
	result := callTool(t, srv.handleGetWorkflowDetail, map[string]any{"workflow": "Test Booking", "summary": false})
	require.False(t, result.IsError)
	text := resultText(t, result)

	// Header
	assert.Contains(t, text, "# Workflow: Test Booking")
	assert.Contains(t, text, "Book a test flight")
	assert.Contains(t, text, "## Steps (3)")

	// Step 1: search
	assert.Contains(t, text, "### 1. search")
	assert.Contains(t, text, "**HTTP:** POST /api/flights/search")
	assert.Contains(t, text, "**Depends on:** (none)")
	assert.Contains(t, text, "| origin | literal | DEN |")
	assert.Contains(t, text, "| destination | literal | SFO |")
	// Search outputs with element fields
	assert.Contains(t, text, "| results | flight[] | id, carrier |")

	// Step 2: book
	assert.Contains(t, text, "### 2. book")
	assert.Contains(t, text, "**HTTP:** POST /api/book")
	assert.Contains(t, text, "**Depends on:** search")
	assert.Contains(t, text, "flight")
	assert.Contains(t, text, "strategy: first")
	assert.Contains(t, text, "| flightId | fromSelection | flight.id |")

	// Step 3: commit
	assert.Contains(t, text, "### 3. commit")
	assert.Contains(t, text, "| workbenchId | from | book.workbenchId |")

	// Data flow
	assert.Contains(t, text, "## Data Flow")
	assert.Contains(t, text, "search → book")
	assert.Contains(t, text, "book → commit")

	// Cleanup
	assert.Contains(t, text, "## Cleanup")
	assert.Contains(t, text, "cancel (runOn: always)")
}

func TestHandleGetWorkflowDetail_StepIDDiffersFromNode(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{
				Name:     "Round Trip",
				Template: "testdata/workflows/roundtrip.yaml",
			},
		},
		Nodes: map[string]*graph.Node{
			"search": {
				Name:    "search",
				Adapter: "searchTmpl",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "destination", Type: "string"},
				},
				Outputs: []graph.Output{{Name: "results", Type: "flight[]"}},
			},
			"book": {
				Name:    "book",
				Adapter: "bookTmpl",
				Inputs:  []graph.Input{{Name: "flightId", Type: "string"}},
				Outputs: []graph.Output{{Name: "workbenchId", Type: "string"}},
			},
			"cancel": {Name: "cancel", Adapter: "cancelTmpl"},
		},
	}

	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("searchTmpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "searchTmpl", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "POST", Path: "/api/search"},
	})))
	require.NoError(t, reg.Register("bookTmpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "bookTmpl", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "POST", Path: "/api/book"},
	})))
	require.NoError(t, reg.Register("cancelTmpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "cancelTmpl", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "DELETE", Path: "/api/cancel"},
	})))

	ctx := &ServerContext{
		Graph:    g,
		Registry: reg,
		Manifest: &ProjectManifest{Name: "test"},
		GraphDir: ".",
	}
	srv := NewServer(ctx)
	result := callTool(t, srv.handleGetWorkflowDetail, map[string]any{"workflow": "Round Trip", "summary": false})
	require.False(t, result.IsError)
	text := resultText(t, result)

	// Step 1 (search): StepID == Node, no Operation line
	assert.Contains(t, text, "### 1. search")
	// Step 2 (searchNextLeg2): StepID != Node, should show Operation line
	assert.Contains(t, text, "### 2. searchNextLeg2")
	assert.Contains(t, text, "**Operation:** search")
	// Step 3 (book): StepID == Node, no Operation line
	assert.Contains(t, text, "### 3. book")
}

func TestHandleGetWorkflowDetail_UnknownWorkflow(t *testing.T) {
	g := workflowTestGraph()
	srv := newWorkflowTestServer(g)
	result := callTool(t, srv.handleGetWorkflowDetail, map[string]any{"workflow": "NonExistent"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "unknown workflow")
}

func TestHandleGetWorkflowDetail_NoTemplate(t *testing.T) {
	g := &graph.Graph{
		Workflows: []graph.Workflow{{Name: "NoTemplate"}},
		Nodes:     map[string]*graph.Node{"a": {Name: "a"}},
	}
	srv := newWorkflowTestServer(g)
	result := callTool(t, srv.handleGetWorkflowDetail, map[string]any{"workflow": "NoTemplate"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "no template")
}

func TestHandleGetWorkflowDetail_MissingParam(t *testing.T) {
	g := workflowTestGraph()
	srv := newWorkflowTestServer(g)
	result := callTool(t, srv.handleGetWorkflowDetail, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

// --- instantiate_workflow ---

func TestHandleInstantiateWorkflow_UnknownWorkflow(t *testing.T) {
	g := workflowTestGraph()
	srv := newWorkflowTestServer(g)
	result := callTool(t, srv.handleInstantiateWorkflow, map[string]any{"workflow": "NonExistent"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "unknown workflow")
}

func TestHandleInstantiateWorkflow_NoTemplate(t *testing.T) {
	g := &graph.Graph{
		Workflows: []graph.Workflow{
			{Name: "NoTemplate"},
		},
		Nodes: map[string]*graph.Node{
			"a": {Name: "a", Adapter: "a"},
		},
	}
	srv := newWorkflowTestServer(g)
	result := callTool(t, srv.handleInstantiateWorkflow, map[string]any{"workflow": "NoTemplate"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "no template")
}
