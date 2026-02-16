package intent

import (
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadTravelportGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.ParseFile("../graph/testdata/valid/travelport_booking.yaml")
	require.NoError(t, err)
	return g
}

func TestFormatGraph_Travelport(t *testing.T) {
	g := loadTravelportGraph(t)
	result := FormatGraph(g)

	// Should contain version
	assert.Contains(t, result, "version 1.0.0")

	// Should contain all node names
	for name := range g.Nodes {
		assert.Contains(t, result, "Node: "+name)
	}

	// Should contain inputs and outputs
	assert.Contains(t, result, "origin: string")
	assert.Contains(t, result, "destination: string")
	assert.Contains(t, result, "catalogOfferings: offering[]")
	assert.Contains(t, result, "locator: string")

	// Should contain edges
	assert.Contains(t, result, "searchFlights.catalogOfferingsId")
	assert.Contains(t, result, "[select]")

	// Should contain optional marker
	assert.Contains(t, result, "(optional)")

	// Should contain default values
	assert.Contains(t, result, "[default: 1]")
	assert.Contains(t, result, "[default: economy]")

	// Should contain cleanup
	assert.Contains(t, result, "Cleanup: ignoreWorkbench")
}

func TestFormatGraph_Deterministic(t *testing.T) {
	g := loadTravelportGraph(t)

	// Run multiple times, should be identical
	result1 := FormatGraph(g)
	result2 := FormatGraph(g)
	assert.Equal(t, result1, result2)
}

func TestFormatGraph_ElementFields(t *testing.T) {
	g := loadTravelportGraph(t)
	result := FormatGraph(g)

	// Should show element fields for array outputs
	assert.Contains(t, result, "offeringId: string (path: id)")
	assert.Contains(t, result, "carrier: string")
	assert.Contains(t, result, "stops: integer")
	assert.Contains(t, result, "productRef: string (path: ProductBrandOptions.0.ProductBrandOffering.0.Product.0.productRef)")

	// Fields without path annotations should NOT have (path: ...)
	assert.NotContains(t, result, "carrier: string (path:")
	assert.NotContains(t, result, "stops: integer (path:")
}

func TestFormatChainResult_Travelport(t *testing.T) {
	g := loadTravelportGraph(t)

	cr := &graph.ChainResult{
		Nodes:      []string{"searchFlights", "createWorkbench", "addOffer", "addTraveler", "commitBooking"},
		EntryNodes: []string{"searchFlights", "createWorkbench"},
		Edges: []graph.Edge{
			{From: "searchFlights.catalogOfferingsId", To: "addOffer.catalogOfferingsId"},
			{From: "searchFlights.catalogOfferings", To: "addOffer.offeringId", Select: true},
			{From: "createWorkbench.workbenchId", To: "addOffer.workbenchId"},
			{From: "createWorkbench.workbenchId", To: "addTraveler.workbenchId"},
			{From: "createWorkbench.workbenchId", To: "commitBooking.workbenchId"},
			{From: "addOffer.offerStatus", To: "commitBooking.offerStatus"},
			{From: "addTraveler.travelerId", To: "commitBooking.travelerId"},
		},
	}

	result := FormatChainResult(cr, g)

	// Should show chain order
	assert.Contains(t, result, "searchFlights → createWorkbench → addOffer → addTraveler → commitBooking")

	// Should show entry nodes
	assert.Contains(t, result, "searchFlights, createWorkbench")

	// Should show node details
	assert.Contains(t, result, "Step: commitBooking")
	assert.Contains(t, result, "Step: searchFlights")

	// Should show data flow with select markers
	assert.Contains(t, result, "[select]")
}

func TestFormatChainResult_WithDecisions(t *testing.T) {
	g := loadTravelportGraph(t)

	cr := &graph.ChainResult{
		Nodes:      []string{"searchFlights"},
		EntryNodes: []string{"searchFlights"},
		Decisions: []graph.ChainDecision{
			{
				Type:         graph.DecisionPathChoice,
				Node:         "addOffer",
				Detail:       "input \"offeringId\": chose producer \"searchFlights\"",
				Alternatives: []string{"priceOffer"},
			},
		},
	}

	result := FormatChainResult(cr, g)
	assert.Contains(t, result, "Chain Decisions")
	assert.Contains(t, result, "chose producer")
	assert.Contains(t, result, "priceOffer")
}

func TestFormatPlanSchema_NonEmpty(t *testing.T) {
	schema := FormatPlanSchema()

	assert.Contains(t, schema, "Fill the Plan Skeleton")
	assert.Contains(t, schema, "Literal Values")
	assert.Contains(t, schema, "strategy")
	assert.Contains(t, schema, "assertions")
	assert.Contains(t, schema, "match")
	assert.Contains(t, schema, "filter")
	assert.Contains(t, schema, "field: id")
	assert.Contains(t, schema, "named selections")
}

func TestFormatGraph_EmptyGraph(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes:   map[string]*graph.Node{},
	}
	result := FormatGraph(g)
	assert.Contains(t, result, "version 1.0.0")
	assert.NotContains(t, result, "## Node:")
}

func TestFormatChainResult_EmptyChain(t *testing.T) {
	g := &graph.Graph{Nodes: map[string]*graph.Node{}}
	cr := &graph.ChainResult{
		Nodes:      []string{},
		EntryNodes: []string{},
	}
	result := FormatChainResult(cr, g)
	assert.Contains(t, result, "Execution Chain")
}

func TestFormatGraph_WithTitle(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Title:   "My API",
		Nodes:   map[string]*graph.Node{},
	}
	result := FormatGraph(g)
	assert.Contains(t, result, "# My API (version 1.0.0)")
	assert.NotContains(t, result, "API Graph")
}

func TestFormatGraph_WithDescription(t *testing.T) {
	g := &graph.Graph{
		Version:     "1.0.0",
		Description: "This is a test API graph.",
		Nodes:       map[string]*graph.Node{},
	}
	result := FormatGraph(g)
	assert.Contains(t, result, "This is a test API graph.")
}

func TestFormatGraph_WithWorkflows(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{
				Name:        "Booking Flow",
				Description: "Standard booking",
				Steps:       []string{"search", "price", "commit"},
			},
			{
				Name: "Simple Flow",
			},
		},
		Nodes: map[string]*graph.Node{},
	}
	result := FormatGraph(g)
	assert.Contains(t, result, "## Workflows")
	assert.Contains(t, result, "**Booking Flow**: Standard booking")
	assert.Contains(t, result, "search → price → commit")
	assert.Contains(t, result, "**Simple Flow**")
}

func TestFormatGraph_WithNotes(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Notes:   "Important design notes here.",
		Nodes:   map[string]*graph.Node{},
	}
	result := FormatGraph(g)
	assert.Contains(t, result, "## Notes")
	assert.Contains(t, result, "Important design notes here.")
}

func TestFormatGraph_WithTags(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"search": {
				Name:        "search",
				Description: "Search flights",
				Adapter:     "searchFlights",
				Tags:        []string{"search", "air"},
			},
		},
	}
	result := FormatGraph(g)
	assert.Contains(t, result, "Tags: search, air")
}

func TestFormatGraph_TitleFallback(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes:   map[string]*graph.Node{},
	}
	result := FormatGraph(g)
	assert.Contains(t, result, "# API Graph (version 1.0.0)")
}

func TestFormatGraph_WithConstraints(t *testing.T) {
	minLen, maxLen := 3, 3
	min, max := 1.0, 100.0
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"search": {
				Name:    "search",
				Adapter: "search",
				Inputs: []graph.Input{
					{
						Name: "origin",
						Type: "string",
						Constraints: &graph.Constraint{
							MinLength:   &minLen,
							MaxLength:   &maxLen,
							Pattern:     "^[A-Z]{3}$",
							Description: "IATA airport code",
						},
					},
					{
						Name: "count",
						Type: "integer",
						Constraints: &graph.Constraint{
							Min: &min,
							Max: &max,
						},
					},
				},
				Outputs: []graph.Output{{Name: "results", Type: "string"}},
			},
		},
	}
	result := FormatGraph(g)
	assert.Contains(t, result, "pattern: ^[A-Z]{3}$")
	assert.Contains(t, result, "length: 3")
	assert.Contains(t, result, "IATA airport code")
	assert.Contains(t, result, "range: 1..100")
}

func TestFormatGraph_AutoWireOmitsEdges(t *testing.T) {
	g := &graph.Graph{
		Version:  "1.0.0",
		AutoWire: graph.AutoWireSources,
		Nodes: map[string]*graph.Node{
			"producer": {
				Name:    "producer",
				Adapter: "p",
				Outputs: []graph.Output{{Name: "token", Type: "string"}},
			},
			"consumer": {
				Name:    "consumer",
				Adapter: "c",
				Inputs:  []graph.Input{{Name: "token", Type: "string"}},
				Outputs: []graph.Output{{Name: "result", Type: "string"}},
			},
		},
		Edges: []graph.Edge{
			{From: "producer.token", To: "consumer.token", AutoWired: true},
		},
	}
	result := FormatGraph(g)
	assert.Contains(t, result, "Auto-wired")
	assert.NotContains(t, result, "producer.token → consumer.token")
}

func TestFormatGraph_WithAddonWorkflows(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{
				Name:     "Main Booking",
				Template: "plans/main.yaml",
				Steps:    []string{"search", "book", "commit"},
			},
			{
				Name:     "Seat Selection",
				Kind:     "addon",
				Template: "plans/seat.yaml",
				Steps:    []string{"searchSeat", "addSeat"},
			},
			{
				Name:     "Booking with Seat",
				Template: "plans/main.yaml",
				Includes: []graph.WorkflowInclude{
					{Workflow: "Seat Selection", After: "book"},
				},
				Steps: []string{"search", "book", "searchSeat", "addSeat", "commit"},
			},
		},
		Nodes: map[string]*graph.Node{},
	}
	result := FormatGraph(g)

	// Addon marker should appear
	assert.Contains(t, result, "**Seat Selection** [addon] [template]")

	// Includes should appear
	assert.Contains(t, result, "Includes: Seat Selection (after book)")

	// Regular workflow should not have [addon]
	assert.Contains(t, result, "**Main Booking** [template]")
}

func TestFormatGraph_AutoWireWithExplicit(t *testing.T) {
	g := &graph.Graph{
		Version:  "1.0.0",
		AutoWire: graph.AutoWireSources,
		Nodes: map[string]*graph.Node{
			"a": {
				Name: "a", Adapter: "a",
				Outputs: []graph.Output{
					{Name: "token", Type: "string"},
					{Name: "items", Type: "item[]"},
				},
			},
			"b": {
				Name: "b", Adapter: "b",
				Inputs:  []graph.Input{{Name: "token", Type: "string"}, {Name: "itemId", Type: "string"}},
				Outputs: []graph.Output{{Name: "out", Type: "string"}},
			},
		},
		Edges: []graph.Edge{
			{From: "a.items", To: "b.itemId", Select: true}, // explicit
			{From: "a.token", To: "b.token", AutoWired: true},
		},
	}
	result := FormatGraph(g)
	// Explicit edge should appear
	assert.Contains(t, result, "a.items → b.itemId [select]")
	// Auto-wired edge should not
	assert.NotContains(t, result, "a.token → b.token")
	assert.Contains(t, result, "Auto-wired")
}
