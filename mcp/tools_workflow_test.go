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
				Template: "plans/booking.yaml",
				Steps:    []string{"search", "book", "commit"},
			},
			{
				Name:        "Seat Selection",
				Kind:        "addon",
				Description: "Select a seat",
				Template:    "plans/seat.yaml",
				Steps:       []string{"searchSeat", "addSeat"},
			},
			{
				Name:     "Booking with Seat",
				Template: "plans/booking.yaml",
				Includes: []graph.WorkflowInclude{
					{Workflow: "Seat Selection", After: "book", Wire: map[string]string{"workbenchId": "book.workbenchId"}},
				},
				Steps: []string{"search", "book", "searchSeat", "addSeat", "commit"},
			},
		},
		Nodes: map[string]*graph.Node{
			"search":     {Name: "search", Adapter: "search", Outputs: []graph.Output{{Name: "results", Type: "string[]"}}},
			"book":       {Name: "book", Adapter: "book", Inputs: []graph.Input{{Name: "flightId", Type: "string"}}, Outputs: []graph.Output{{Name: "workbenchId", Type: "string"}}},
			"commit":     {Name: "commit", Adapter: "commit", Inputs: []graph.Input{{Name: "workbenchId", Type: "string"}}, Outputs: []graph.Output{{Name: "locator", Type: "string"}}},
			"searchSeat": {Name: "searchSeat", Adapter: "searchSeat", Inputs: []graph.Input{{Name: "workbenchId", Type: "string"}}, Outputs: []graph.Output{{Name: "seats", Type: "string[]"}}},
			"addSeat":    {Name: "addSeat", Adapter: "addSeat", Inputs: []graph.Input{{Name: "workbenchId", Type: "string"}, {Name: "seatId", Type: "string"}}, Outputs: []graph.Output{{Name: "confirmId", Type: "string"}}},
		},
		Edges: []graph.Edge{
			{From: "search.results", To: "book.flightId", Select: true},
			{From: "book.workbenchId", To: "commit.workbenchId"},
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
	assert.Contains(t, text, "Booking with Seat")
	assert.Contains(t, text, "Includes:")
	assert.Contains(t, text, "Seat Selection")
}

// --- scaffold_template ---

func TestHandleScaffoldTemplate(t *testing.T) {
	g := workflowTestGraph()
	g.BuildEdgeIndex()
	srv := newWorkflowTestServer(g)
	result := callTool(t, srv.handleScaffoldTemplate, map[string]any{"goal": "commit"})
	require.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Scaffolded Template")
	assert.Contains(t, text, "commit")
	assert.Contains(t, text, "yaml")
}

func TestHandleScaffoldTemplate_UnknownNode(t *testing.T) {
	g := workflowTestGraph()
	srv := newWorkflowTestServer(g)
	result := callTool(t, srv.handleScaffoldTemplate, map[string]any{"goal": "nonexistent"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "unknown goal node")
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
			{Name: "NoTemplate", Steps: []string{"a"}},
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
