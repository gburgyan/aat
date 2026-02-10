package mcp

import (
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
)

func TestFormatNodeSummary(t *testing.T) {
	tests := []struct {
		name string
		node *graph.Node
		want string
	}{
		{
			name: "with description",
			node: &graph.Node{
				Name:        "searchFlights",
				Description: "Search for available flights",
				Inputs:      []graph.Input{{Name: "origin"}, {Name: "destination"}},
				Outputs:     []graph.Output{{Name: "catalogOfferings"}},
			},
			want: "**searchFlights** — Search for available flights (2 inputs, 1 outputs)",
		},
		{
			name: "without description",
			node: &graph.Node{
				Name:    "doStuff",
				Inputs:  []graph.Input{{Name: "a"}},
				Outputs: []graph.Output{},
			},
			want: "**doStuff** (1 inputs, 0 outputs)",
		},
		{
			name: "no inputs or outputs",
			node: &graph.Node{
				Name:        "noop",
				Description: "Does nothing",
			},
			want: "**noop** — Does nothing (0 inputs, 0 outputs)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatNodeSummary(tt.node)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatInputTable(t *testing.T) {
	tests := []struct {
		name   string
		inputs []graph.Input
		want   string
	}{
		{
			name:   "empty",
			inputs: nil,
			want:   "",
		},
		{
			name: "single required",
			inputs: []graph.Input{
				{Name: "origin", Type: "string", Description: "Airport code"},
			},
			want: "| Name | Type | Required | Default | Description |\n" +
				"|------|------|----------|---------|-------------|\n" +
				"| origin | string | yes |  | Airport code |\n",
		},
		{
			name: "multiple with optional and default",
			inputs: []graph.Input{
				{Name: "origin", Type: "string"},
				{Name: "maxResults", Type: "integer", Optional: true, Default: 10, Description: "Max results"},
			},
			want: "| Name | Type | Required | Default | Description |\n" +
				"|------|------|----------|---------|-------------|\n" +
				"| origin | string | yes |  |  |\n" +
				"| maxResults | integer | no | 10 | Max results |\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatInputTable(tt.inputs)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatOutputTable(t *testing.T) {
	tests := []struct {
		name    string
		outputs []graph.Output
		want    string
	}{
		{
			name:    "empty",
			outputs: nil,
			want:    "",
		},
		{
			name: "simple outputs",
			outputs: []graph.Output{
				{Name: "result", Type: "object", Description: "The result"},
			},
			want: "| Name | Type | Description |\n" +
				"|------|------|-------------|\n" +
				"| result | object | The result |\n",
		},
		{
			name: "with elementFields",
			outputs: []graph.Output{
				{
					Name: "offerings",
					Type: "offering[]",
					ElementFields: []graph.Field{
						{Name: "id", Type: "string"},
						{Name: "productRef", Type: "string", Path: "Product.0.ProductRef"},
					},
				},
			},
			want: "| Name | Type | Description |\n" +
				"|------|------|-------------|\n" +
				"| offerings | offering[] |  |\n" +
				"| \u00a0\u00a0\u2514 id | string | elementField |\n" +
				"| \u00a0\u00a0\u2514 productRef | string | elementField (path: Product.0.ProductRef) |\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatOutputTable(tt.outputs)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatEdgeList(t *testing.T) {
	tests := []struct {
		name  string
		edges []graph.Edge
		want  string
	}{
		{
			name:  "empty",
			edges: nil,
			want:  "",
		},
		{
			name: "simple edge",
			edges: []graph.Edge{
				{From: "search.result", To: "book.input"},
			},
			want: "- search.result → book.input\n",
		},
		{
			name: "with select and preferred",
			edges: []graph.Edge{
				{From: "search.offerings", To: "detail.offeringId", Select: true},
				{From: "detail.result", To: "book.productRef", Preferred: true},
				{From: "alt.result", To: "book.altRef", Select: true, Preferred: true},
			},
			want: "- search.offerings → detail.offeringId [select]\n" +
				"- detail.result → book.productRef [preferred]\n" +
				"- alt.result → book.altRef [select] [preferred]\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatEdgeList(tt.edges)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatNodeDetail(t *testing.T) {
	t.Run("full node", func(t *testing.T) {
		g := &graph.Graph{
			Nodes: map[string]*graph.Node{
				"searchFlights": {
					Name:        "searchFlights",
					Description: "Search for flights",
					Adapter:     "searchFlightsTemplate",
					Cleanup:     "ignoreWorkbench",
					OAS: &graph.OASRef{
						OperationID: "CreateAirSearch",
						Spec:        "travelport.yaml",
					},
					Inputs: []graph.Input{
						{Name: "origin", Type: "string", Description: "Departure airport"},
					},
					Outputs: []graph.Output{
						{
							Name: "catalogOfferings",
							Type: "offering[]",
							ElementFields: []graph.Field{
								{Name: "offeringId", Type: "string"},
							},
						},
					},
				},
				"bookFlight": {
					Name: "bookFlight",
					Inputs: []graph.Input{
						{Name: "offeringId", Type: "string"},
					},
				},
			},
			Edges: []graph.Edge{
				{From: "searchFlights.catalogOfferings", To: "bookFlight.offeringId", Select: true},
			},
		}

		result := formatNodeDetail(g.Nodes["searchFlights"], g)

		assert.Contains(t, result, "# searchFlights")
		assert.Contains(t, result, "Search for flights")
		assert.Contains(t, result, "**Adapter:** searchFlightsTemplate")
		assert.Contains(t, result, "**Cleanup:** ignoreWorkbench")
		assert.Contains(t, result, "**OAS:** operationId=CreateAirSearch (spec: travelport.yaml)")
		assert.Contains(t, result, "## Inputs")
		assert.Contains(t, result, "| origin | string | yes |")
		assert.Contains(t, result, "## Outputs")
		assert.Contains(t, result, "| catalogOfferings | offering[] |")
		assert.Contains(t, result, "offeringId")
		assert.Contains(t, result, "## Outbound Edges")
		assert.Contains(t, result, "searchFlights.catalogOfferings → bookFlight.offeringId [select]")
	})

	t.Run("minimal node", func(t *testing.T) {
		g := &graph.Graph{
			Nodes: map[string]*graph.Node{
				"simple": {Name: "simple"},
			},
		}

		result := formatNodeDetail(g.Nodes["simple"], g)
		assert.Contains(t, result, "# simple")
		assert.NotContains(t, result, "## Inputs")
		assert.NotContains(t, result, "## Outputs")
		assert.NotContains(t, result, "## Inbound")
		assert.NotContains(t, result, "## Outbound")
	})

	t.Run("cycle breaker", func(t *testing.T) {
		g := &graph.Graph{
			Nodes: map[string]*graph.Node{
				"cb": {Name: "cb", CycleBreaker: true},
			},
		}

		result := formatNodeDetail(g.Nodes["cb"], g)
		assert.Contains(t, result, "**CycleBreaker:** true")
	})

	t.Run("OAS without spec override", func(t *testing.T) {
		g := &graph.Graph{
			Nodes: map[string]*graph.Node{
				"n": {
					Name: "n",
					OAS:  &graph.OASRef{OperationID: "CreateSomething"},
				},
			},
		}

		result := formatNodeDetail(g.Nodes["n"], g)
		assert.Contains(t, result, "**OAS:** operationId=CreateSomething")
		assert.NotContains(t, result, "spec:")
	})
}

func TestFormatChainTrace(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name:        "search",
				Description: "Search flights",
				Inputs:      []graph.Input{{Name: "origin", Type: "string"}},
				Outputs:     []graph.Output{{Name: "results", Type: "result[]"}},
			},
			"book": {
				Name:        "book",
				Description: "Book flight",
				Cleanup:     "cancelBooking",
				Inputs:      []graph.Input{{Name: "resultId", Type: "string"}},
				Outputs:     []graph.Output{{Name: "locator", Type: "string"}},
			},
		},
	}

	t.Run("basic chain", func(t *testing.T) {
		cr := &graph.ChainResult{
			Nodes:      []string{"search", "book"},
			EntryNodes: []string{"search"},
			Edges: []graph.Edge{
				{From: "search.results", To: "book.resultId", Select: true},
			},
		}

		result := formatChainTrace(cr, g)
		assert.Contains(t, result, "# Execution Chain")
		assert.Contains(t, result, "search → book")
		assert.Contains(t, result, "**Entry nodes:** search")
		assert.Contains(t, result, "## search")
		assert.Contains(t, result, "Search flights")
		assert.Contains(t, result, "## book")
		assert.Contains(t, result, "**Cleanup:** cancelBooking")
		assert.Contains(t, result, "## Data Flow")
		assert.Contains(t, result, "search.results → book.resultId [select]")
	})

	t.Run("with decisions", func(t *testing.T) {
		cr := &graph.ChainResult{
			Nodes:      []string{"search"},
			EntryNodes: []string{"search"},
			Decisions: []graph.ChainDecision{
				{
					Type:         graph.DecisionCycleBreak,
					Node:         "search",
					Detail:       "stopped traversal at cycle-breaker node \"search\"",
					Alternatives: []string{"alt1", "alt2"},
				},
			},
		}

		result := formatChainTrace(cr, g)
		assert.Contains(t, result, "## Chain Decisions")
		assert.Contains(t, result, "cycle-breaker")
		assert.Contains(t, result, "Alternatives: alt1, alt2")
	})
}

func TestCollectInboundEdges(t *testing.T) {
	g := &graph.Graph{
		Edges: []graph.Edge{
			{From: "a.out", To: "b.in"},
			{From: "c.out", To: "b.in2"},
			{From: "b.out", To: "d.in"},
		},
	}

	inbound := collectInboundEdges("b", g)
	assert.Len(t, inbound, 2)
	assert.Equal(t, "a.out", inbound[0].From)
	assert.Equal(t, "c.out", inbound[1].From)
}

func TestCollectOutboundEdges(t *testing.T) {
	g := &graph.Graph{
		Edges: []graph.Edge{
			{From: "a.out", To: "b.in"},
			{From: "a.out2", To: "c.in"},
			{From: "b.out", To: "d.in"},
		},
	}

	outbound := collectOutboundEdges("a", g)
	assert.Len(t, outbound, 2)
	assert.Equal(t, "b.in", outbound[0].To)
	assert.Equal(t, "c.in", outbound[1].To)
}

func TestEdgeRefNode(t *testing.T) {
	assert.Equal(t, "node", edgeRefNode("node.output"))
	assert.Equal(t, "node", edgeRefNode("node"))
	assert.Equal(t, "a", edgeRefNode("a.b.c"))
}
