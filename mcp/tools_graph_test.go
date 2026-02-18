package mcp

import (
	"context"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer creates a Server with the given graph for testing tool handlers.
func newTestServer(g *graph.Graph) *Server {
	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
	}
	return NewServer(ctx)
}

// callTool builds a CallToolRequest with the given arguments and calls the handler.
func callTool(t *testing.T, handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := handler(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

// resultText extracts the text content from a tool result.
func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, result.Content)
	tc, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "expected TextContent, got %T", result.Content[0])
	return tc.Text
}

// --- list_nodes ---

func TestHandleListNodes_EmptyGraph(t *testing.T) {
	srv := newTestServer(&graph.Graph{Nodes: map[string]*graph.Node{}})
	result := callTool(t, srv.handleListNodes, nil)
	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "No nodes")
}

func TestHandleListNodes_SingleNode(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {Name: "search", Description: "Search flights", Inputs: []graph.Input{{Name: "q"}}},
		},
	}
	srv := newTestServer(g)
	result := callTool(t, srv.handleListNodes, nil)
	text := resultText(t, result)
	assert.Contains(t, text, "**search**")
	assert.Contains(t, text, "Search flights")
	assert.Contains(t, text, "1 inputs")
}

func TestHandleListNodes_SortedOrder(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"zulu":  {Name: "zulu"},
			"alpha": {Name: "alpha"},
			"mike":  {Name: "mike"},
		},
	}
	srv := newTestServer(g)
	result := callTool(t, srv.handleListNodes, nil)
	text := resultText(t, result)

	// alpha should appear before mike, mike before zulu
	alphaIdx := len(text[:indexOf(text, "alpha")])
	mikeIdx := len(text[:indexOf(text, "mike")])
	zuluIdx := len(text[:indexOf(text, "zulu")])
	assert.Less(t, alphaIdx, mikeIdx)
	assert.Less(t, mikeIdx, zuluIdx)
}

// --- describe_node ---

func TestHandleDescribeNode_ValidNode(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name:        "search",
				Description: "Search for things",
				Adapter:     "searchTemplate",
				Satisfies:   []string{"searchResults"},
				Inputs:      []graph.Input{{Name: "query", Type: "string"}},
				Outputs: []graph.Output{
					{
						Name: "results",
						Type: "result[]",
						ElementFields: []graph.Field{
							{Name: "id", Type: "string"},
						},
					},
				},
			},
			"detail": {
				Name:     "detail",
				Requires: []string{"searchResults"},
				Inputs:   []graph.Input{{Name: "id", Type: "string"}},
			},
		},
		SatisfiersByToken: map[string][]string{
			"searchResults": {"search"},
		},
	}
	srv := newTestServer(g)
	result := callTool(t, srv.handleDescribeNode, map[string]any{"node": "search"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "# search")
	assert.Contains(t, text, "Search for things")
	assert.Contains(t, text, "**Adapter:** searchTemplate")
	assert.Contains(t, text, "| query | string | yes |")
	assert.Contains(t, text, "| results | result[] |")
	assert.Contains(t, text, "id")
	assert.Contains(t, text, "## Depended On By")
	assert.Contains(t, text, "detail")
}

func TestHandleDescribeNode_UnknownNode(t *testing.T) {
	srv := newTestServer(&graph.Graph{Nodes: map[string]*graph.Node{}})
	result := callTool(t, srv.handleDescribeNode, map[string]any{"node": "nope"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "unknown node")
}

func TestHandleDescribeNode_MissingParam(t *testing.T) {
	srv := newTestServer(&graph.Graph{Nodes: map[string]*graph.Node{}})
	result := callTool(t, srv.handleDescribeNode, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

// --- trace_workflow ---

func TestHandleTraceWorkflow_ValidGoal(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name:        "search",
				Description: "Search",
				Satisfies:   []string{"searchResults"},
				Inputs:      []graph.Input{{Name: "origin", Type: "string"}},
				Outputs:     []graph.Output{{Name: "results", Type: "result[]"}},
			},
			"book": {
				Name:        "book",
				Description: "Book",
				Requires:    []string{"searchResults"},
				Inputs:      []graph.Input{{Name: "resultId", Type: "string"}},
				Outputs:     []graph.Output{{Name: "locator", Type: "string"}},
			},
		},
		SatisfiersByToken: map[string][]string{
			"searchResults": {"search"},
		},
	}
	srv := newTestServer(g)
	result := callTool(t, srv.handleTraceWorkflow, map[string]any{"goal": "book"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "# Execution Chain")
	assert.Contains(t, text, "search → book")
	assert.Contains(t, text, "**Entry nodes:** search")
	assert.Contains(t, text, "## search")
	assert.Contains(t, text, "## book")
}

func TestHandleTraceWorkflow_UnknownGoal(t *testing.T) {
	srv := newTestServer(&graph.Graph{Nodes: map[string]*graph.Node{}})
	result := callTool(t, srv.handleTraceWorkflow, map[string]any{"goal": "nope"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "unknown goal node")
}

func TestHandleTraceWorkflow_MissingParam(t *testing.T) {
	srv := newTestServer(&graph.Graph{Nodes: map[string]*graph.Node{}})
	result := callTool(t, srv.handleTraceWorkflow, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

func TestHandleTraceWorkflow_CycleBreaker(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"entry": {
				Name:         "entry",
				CycleBreaker: true,
				Satisfies:    []string{"entryData"},
				Outputs:      []graph.Output{{Name: "data", Type: "string"}},
			},
			"process": {
				Name:     "process",
				Requires: []string{"entryData"},
				Inputs:   []graph.Input{{Name: "data", Type: "string"}},
				Outputs:  []graph.Output{{Name: "out", Type: "string"}},
			},
		},
		SatisfiersByToken: map[string][]string{
			"entryData": {"entry"},
		},
	}
	srv := newTestServer(g)
	result := callTool(t, srv.handleTraceWorkflow, map[string]any{"goal": "process"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Chain Decisions")
	assert.Contains(t, text, "cycle-breaker")
}

// --- find_workflows ---

func TestHandleFindWorkflows_ByName(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"searchFlights":   {Name: "searchFlights", Description: "Search flights"},
			"bookFlight":      {Name: "bookFlight", Description: "Book a flight"},
			"cancelReservation": {Name: "cancelReservation", Description: "Cancel reservation"},
		},
	}
	srv := newTestServer(g)
	result := callTool(t, srv.handleFindWorkflows, map[string]any{"query": "flight"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "2 node(s)")
	assert.Contains(t, text, "searchFlights")
	assert.Contains(t, text, "bookFlight")
	assert.NotContains(t, text, "cancelReservation")
}

func TestHandleFindWorkflows_ByDescription(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"node1": {Name: "node1", Description: "Creates a reservation in the system"},
			"node2": {Name: "node2", Description: "Does other stuff"},
		},
	}
	srv := newTestServer(g)
	result := callTool(t, srv.handleFindWorkflows, map[string]any{"query": "reservation"})

	text := resultText(t, result)
	assert.Contains(t, text, "1 node(s)")
	assert.Contains(t, text, "node1")
	assert.Contains(t, text, "description")
}

func TestHandleFindWorkflows_ByInputName(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"node1": {
				Name:   "node1",
				Inputs: []graph.Input{{Name: "airportCode", Type: "string"}},
			},
			"node2": {
				Name:   "node2",
				Inputs: []graph.Input{{Name: "date", Type: "string"}},
			},
		},
	}
	srv := newTestServer(g)
	result := callTool(t, srv.handleFindWorkflows, map[string]any{"query": "airport"})

	text := resultText(t, result)
	assert.Contains(t, text, "1 node(s)")
	assert.Contains(t, text, "node1")
	assert.Contains(t, text, "input \"airportCode\"")
}

func TestHandleFindWorkflows_ByOutputName(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"node1": {
				Name:    "node1",
				Outputs: []graph.Output{{Name: "locator", Type: "string"}},
			},
		},
	}
	srv := newTestServer(g)
	result := callTool(t, srv.handleFindWorkflows, map[string]any{"query": "locator"})

	text := resultText(t, result)
	assert.Contains(t, text, "1 node(s)")
	assert.Contains(t, text, "output \"locator\"")
}

func TestHandleFindWorkflows_NoMatches(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {Name: "search", Description: "Search flights"},
		},
	}
	srv := newTestServer(g)
	result := callTool(t, srv.handleFindWorkflows, map[string]any{"query": "hotel"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "No nodes or workflows matching")
}

func TestHandleFindWorkflows_CaseInsensitive(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"SearchFlights": {Name: "SearchFlights", Description: "SEARCH FLIGHTS"},
		},
	}
	srv := newTestServer(g)
	result := callTool(t, srv.handleFindWorkflows, map[string]any{"query": "search"})

	text := resultText(t, result)
	assert.Contains(t, text, "1 node(s)")
}

func TestHandleFindWorkflows_MissingParam(t *testing.T) {
	srv := newTestServer(&graph.Graph{Nodes: map[string]*graph.Node{}})
	result := callTool(t, srv.handleFindWorkflows, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

func TestHandleDescribeNode_WithConstraints(t *testing.T) {
	minLen, maxLen := 3, 3
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name:    "search",
				Adapter: "search",
				Inputs: []graph.Input{
					{
						Name: "code",
						Type: "string",
						Constraints: &graph.Constraint{
							MinLength: &minLen,
							MaxLength: &maxLen,
							Pattern:   "^[A-Z]{3}$",
						},
					},
				},
				Outputs: []graph.Output{{Name: "results", Type: "string"}},
			},
		},
	}
	srv := newTestServer(g)
	result := callTool(t, srv.handleDescribeNode, map[string]any{"node": "search"})

	text := resultText(t, result)
	assert.Contains(t, text, "Constraints")
	assert.Contains(t, text, "`^[A-Z]{3}$`")
	assert.Contains(t, text, "len=3")
}

// indexOf returns the byte offset of the first occurrence of substr in s.
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
