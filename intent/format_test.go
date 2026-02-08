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
	assert.Contains(t, result, "offeringId: string")
	assert.Contains(t, result, "carrier: string")
	assert.Contains(t, result, "stops: integer")
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

	assert.Contains(t, schema, "Plan YAML Schema")
	assert.Contains(t, schema, "metadata")
	assert.Contains(t, schema, "execution")
	assert.Contains(t, schema, "strategy")
	assert.Contains(t, schema, "dependsOn")
	assert.Contains(t, schema, "cleanup")
	assert.Contains(t, schema, "isGoal")
	assert.Contains(t, schema, "assertions")
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
