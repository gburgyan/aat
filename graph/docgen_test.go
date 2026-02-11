package graph

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateDocs_MinimalGraph(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/minimal.yaml")

	result := GenerateDocs(g, nil)

	assert.Contains(t, result, "# API Workflow")
	assert.Contains(t, result, "1 nodes, 0 edges")
	assert.Contains(t, result, "```mermaid")
	assert.Contains(t, result, "### getUser")
	assert.Contains(t, result, "Retrieve user details")
}

func TestGenerateDocs_TravelportBooking(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/travelport_booking.yaml")

	result := GenerateDocs(g, nil)

	// Header
	assert.Contains(t, result, "# API Workflow")
	assert.Contains(t, result, "7 nodes, 11 edges")
	assert.Contains(t, result, "Version 1.0.0")

	// Mermaid diagram
	assert.Contains(t, result, "```mermaid")
	assert.Contains(t, result, "graph TD")

	// Entry points
	assert.Contains(t, result, "## Entry Points")
	assert.Contains(t, result, "**searchFlights**")
	assert.Contains(t, result, "**createWorkbench**")

	// All nodes
	for name := range g.Nodes {
		assert.Contains(t, result, "### "+name)
	}

	// Input tables
	assert.Contains(t, result, "| origin |")
	assert.Contains(t, result, "| departureDate |")

	// Output tables with elementFields
	assert.Contains(t, result, "| catalogOfferings |")
	assert.Contains(t, result, "\u00a0\u00a0\u2514 offeringId")

	// Data flow table
	assert.Contains(t, result, "## Data Flow")
	assert.Contains(t, result, "| searchFlights.catalogOfferingsId | priceOffer.catalogOfferingsId |")

	// Cleanup table
	assert.Contains(t, result, "## Cleanup")
	assert.Contains(t, result, "| ignoreWorkbench | createWorkbench |")
}

func TestGenerateDocs_CustomTitle(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/minimal.yaml")

	result := GenerateDocs(g, &DocGenOptions{
		Title: "My Custom Workflow",
	})

	assert.Contains(t, result, "# My Custom Workflow")
	assert.NotContains(t, result, "# API Workflow")
}

func TestGenerateDocs_WithNodeDocs(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/minimal.yaml")

	result := GenerateDocs(g, &DocGenOptions{
		NodeDocs: map[string]string{
			"getUser": "This is custom documentation for the getUser node.\nIt has multiple lines.",
		},
	})

	assert.Contains(t, result, "This is custom documentation for the getUser node.")
	assert.Contains(t, result, "It has multiple lines.")
}

func TestGenerateDocs_WithExamples(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/travelport_booking.yaml")

	result := GenerateDocs(g, &DocGenOptions{
		Examples: map[string][]string{
			"searchFlights.origin":      {"ATL", "DEN", "JFK"},
			"searchFlights.destination": {"LAX", "ORD", "SFO"},
		},
	})

	// Should have Examples column in the searchFlights section
	assert.Contains(t, result, "| Examples |")
	assert.Contains(t, result, "ATL, DEN, JFK")
	assert.Contains(t, result, "LAX, ORD, SFO")
}

func TestGenerateDocs_EnumExamples(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/travelport_booking.yaml")

	// Even without domain enrichment, enum inputs should show their values
	result := GenerateDocs(g, nil)

	// cabinPreference is enum[economy, premiumEconomy, business, first]
	// The searchFlights node has this enum input, so Examples column should appear
	assert.Contains(t, result, "economy, premiumEconomy, business, first")
}

func TestGenerateDocs_ConnectionAnnotations(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/travelport_booking.yaml")

	result := GenerateDocs(g, nil)

	// searchFlights provides data to priceOffer and addOffer
	assert.Contains(t, result, "**Provides data to:**")
	// commitBooking receives from addOffer, addTraveler, createWorkbench
	assert.Contains(t, result, "**Receives data from:**")
}

func TestGenerateDocs_DependencyOrdering(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/travelport_booking.yaml")

	result := GenerateDocs(g, nil)

	// Entry nodes should come before dependent nodes
	searchIdx := strings.Index(result, "### searchFlights")
	priceIdx := strings.Index(result, "### priceOffer")
	createIdx := strings.Index(result, "### createWorkbench")
	commitIdx := strings.Index(result, "### commitBooking")

	require.Greater(t, searchIdx, 0)
	require.Greater(t, priceIdx, 0)
	require.Greater(t, createIdx, 0)
	require.Greater(t, commitIdx, 0)

	// searchFlights should come before priceOffer
	assert.Less(t, searchIdx, priceIdx, "searchFlights should come before priceOffer")
	// createWorkbench should come before commitBooking
	assert.Less(t, createIdx, commitIdx, "createWorkbench should come before commitBooking")
}

func TestGenerateDocs_SelectMarker(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/travelport_booking.yaml")

	result := GenerateDocs(g, nil)

	// Select edges should have checkmark in data flow table
	assert.Contains(t, result, "\u2713")
}

func TestGenerateDocsSplit_TravelportBooking(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/travelport_booking.yaml")

	files := GenerateDocsSplit(g, nil)

	// Should have index.md
	index, ok := files["index.md"]
	require.True(t, ok, "should have index.md")

	// Index should have summary table with links
	assert.Contains(t, index, "| [searchFlights](nodes/searchFlights.md)")
	assert.Contains(t, index, "```mermaid")
	assert.Contains(t, index, "## Data Flow")

	// Should have per-node files
	for name := range g.Nodes {
		path := "nodes/" + name + ".md"
		content, ok := files[path]
		assert.True(t, ok, "should have %s", path)
		assert.Contains(t, content, "### "+name)
	}
}

func TestGenerateDocsSplit_EntryPointLinks(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/travelport_booking.yaml")

	files := GenerateDocsSplit(g, nil)
	index := files["index.md"]

	// Entry points should have links to node files
	assert.Contains(t, index, "[**searchFlights**](nodes/searchFlights.md)")
	assert.Contains(t, index, "[**createWorkbench**](nodes/createWorkbench.md)")
}

func TestGenerateDocs_DeterministicOutput(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/travelport_booking.yaml")

	result1 := GenerateDocs(g, nil)
	result2 := GenerateDocs(g, nil)

	assert.Equal(t, result1, result2, "output should be deterministic")
}

func TestGenerateDocs_NoEdges(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/minimal.yaml")

	result := GenerateDocs(g, nil)

	// No edges → no data flow or cleanup sections
	assert.NotContains(t, result, "## Data Flow")
	assert.NotContains(t, result, "## Cleanup")
}

func TestFindEntryNodes(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/travelport_booking.yaml")

	entries := findEntryNodes(g)

	assert.Contains(t, entries, "searchFlights")
	assert.Contains(t, entries, "createWorkbench")
	assert.NotContains(t, entries, "priceOffer")
	assert.NotContains(t, entries, "commitBooking")
}

func TestCollectOutboundNodes(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/travelport_booking.yaml")

	outbound := collectOutboundNodes("searchFlights", g)

	assert.Contains(t, outbound, "priceOffer")
	assert.Contains(t, outbound, "addOffer")
	assert.NotContains(t, outbound, "searchFlights")
}

func TestCollectInboundNodes(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/travelport_booking.yaml")

	inbound := collectInboundNodes("commitBooking", g)

	assert.Contains(t, inbound, "createWorkbench")
	assert.Contains(t, inbound, "addOffer")
	assert.Contains(t, inbound, "addTraveler")
}

func TestHasExamplesForNode_WithDomainExamples(t *testing.T) {
	node := &Node{
		Inputs: []Input{
			{Name: "origin", Type: "string"},
		},
	}
	opts := &DocGenOptions{
		Examples: map[string][]string{
			"search.origin": {"JFK", "LAX"},
		},
	}

	assert.True(t, hasExamplesForNode("search", node, opts))
}

func TestHasExamplesForNode_WithEnum(t *testing.T) {
	node := &Node{
		Inputs: []Input{
			{Name: "cabin", Type: "enum[Economy, Business]"},
		},
	}
	opts := &DocGenOptions{}

	assert.True(t, hasExamplesForNode("any", node, opts))
}

func TestHasExamplesForNode_NoExamples(t *testing.T) {
	node := &Node{
		Inputs: []Input{
			{Name: "id", Type: "string"},
		},
	}
	opts := &DocGenOptions{}

	assert.False(t, hasExamplesForNode("any", node, opts))
}
