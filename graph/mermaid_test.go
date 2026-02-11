package graph

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateMermaid_MinimalGraph(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/minimal.yaml")

	result := GenerateMermaid(g)

	assert.Contains(t, result, "graph TD")
	assert.Contains(t, result, `getUser["getUser`)
	// No cleanup, so no classDef
	assert.NotContains(t, result, "classDef cleanup")
}

func TestGenerateMermaid_TravelportBooking(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/travelport_booking.yaml")

	result := GenerateMermaid(g)

	// Should have all nodes
	for name := range g.Nodes {
		assert.Contains(t, result, name, "missing node %s", name)
	}

	// Cleanup node should have cleanup class
	assert.Contains(t, result, `ignoreWorkbench["ignoreWorkbench`)
	assert.Contains(t, result, ":::cleanup")
	assert.Contains(t, result, "classDef cleanup")

	// Should have edges
	assert.Contains(t, result, "searchFlights -->")
	assert.Contains(t, result, "createWorkbench -->")

	// Cleanup edge should be dashed
	assert.Contains(t, result, "createWorkbench -.-> ignoreWorkbench")
}

func TestGenerateMermaid_LinearGraph(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"a": {Name: "a", Description: "First"},
			"b": {Name: "b", Description: "Second"},
			"c": {Name: "c", Description: "Third"},
		},
		Edges: []Edge{
			{From: "a.out", To: "b.in"},
			{From: "b.out", To: "c.in"},
		},
	}

	result := GenerateMermaid(g)

	assert.Contains(t, result, "a --> b")
	assert.Contains(t, result, "b --> c")
	assert.NotContains(t, result, "-.->")
}

func TestGenerateMermaid_FanOut(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"source": {Name: "source", Description: "Source node"},
			"dest1":  {Name: "dest1", Description: "Destination 1"},
			"dest2":  {Name: "dest2", Description: "Destination 2"},
			"dest3":  {Name: "dest3", Description: "Destination 3"},
		},
		Edges: []Edge{
			{From: "source.out", To: "dest1.in"},
			{From: "source.out", To: "dest2.in"},
			{From: "source.out", To: "dest3.in"},
		},
	}

	result := GenerateMermaid(g)

	assert.Contains(t, result, "source --> dest1")
	assert.Contains(t, result, "source --> dest2")
	assert.Contains(t, result, "source --> dest3")
}

func TestGenerateMermaid_CleanupEdges(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"create":  {Name: "create", Description: "Create resource", Cleanup: "cleanup"},
			"use":     {Name: "use", Description: "Use resource"},
			"cleanup": {Name: "cleanup", Description: "Clean up resource"},
		},
		Edges: []Edge{
			{From: "create.id", To: "use.id"},
			{From: "create.id", To: "cleanup.id"},
		},
	}

	result := GenerateMermaid(g)

	assert.Contains(t, result, "create --> use")
	assert.Contains(t, result, "create -.-> cleanup")
	assert.Contains(t, result, ":::cleanup")
	assert.Contains(t, result, "classDef cleanup")
}

func TestGenerateMermaid_DeduplicatesEdges(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"a": {Name: "a"},
			"b": {Name: "b"},
		},
		Edges: []Edge{
			{From: "a.out1", To: "b.in1"},
			{From: "a.out2", To: "b.in2"},
		},
	}

	result := GenerateMermaid(g)

	// Should only have one edge between a and b
	count := strings.Count(result, "a --> b")
	assert.Equal(t, 1, count, "should deduplicate edges between same nodes")
}

func TestGenerateMermaid_LongDescription(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"longNode": {Name: "longNode", Description: "This is a very long description that should be truncated to fit in the diagram node label"},
		},
	}

	result := GenerateMermaid(g)

	// Description should be truncated
	assert.NotContains(t, result, "should be truncated to fit in the diagram node label")
	assert.Contains(t, result, "...")
}

func TestGenerateMermaid_DeterministicOutput(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/travelport_booking.yaml")

	result1 := GenerateMermaid(g)
	result2 := GenerateMermaid(g)

	assert.Equal(t, result1, result2, "output should be deterministic")
}

func TestTruncateDescription(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 50, "short"},
		{"exactly 50 chars long string that fits perfectly!!", 50, "exactly 50 chars long string that fits perfectly!!"},
		{"this is a long string that should get cut off here and have dots", 50, "this is a long string that should get cut off h..."},
	}
	for _, tt := range tests {
		result := truncateDescription(tt.input, tt.maxLen)
		assert.Equal(t, tt.expected, result)
	}
}

// mustParseFile is a test helper that parses a graph YAML file or fails.
func mustParseFile(t *testing.T, path string) *Graph {
	t.Helper()
	g, err := ParseFile(path)
	require.NoError(t, err)
	return g
}
