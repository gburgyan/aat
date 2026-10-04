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
	assert.Contains(t, result, "1 nodes")
	assert.Contains(t, result, "```mermaid")
	assert.Contains(t, result, "### getUser")
	assert.Contains(t, result, "Retrieve user details")
}

func TestGenerateDocs_AirlineBooking(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/airline_booking.yaml")

	result := GenerateDocs(g, nil)

	// Header
	assert.Contains(t, result, "# API Workflow")
	assert.Contains(t, result, "7 nodes")
	assert.Contains(t, result, "Version 1.0.0")

	// Mermaid diagram
	assert.Contains(t, result, "```mermaid")
	assert.Contains(t, result, "graph TD")

	// Entry points
	assert.Contains(t, result, "## Entry Points")
	assert.Contains(t, result, "**searchFlights**")
	assert.Contains(t, result, "**createItinerary**")

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

	// Cleanup table
	assert.Contains(t, result, "## Cleanup")
	assert.Contains(t, result, "| ignoreItinerary | createItinerary |")
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
	g := mustParseFile(t, "testdata/valid/airline_booking.yaml")

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
	g := mustParseFile(t, "testdata/valid/airline_booking.yaml")

	// Even without domain enrichment, enum inputs should show their values
	result := GenerateDocs(g, nil)

	// cabinPreference is enum[economy, premiumEconomy, business, first]
	// The searchFlights node has this enum input, so Examples column should appear
	assert.Contains(t, result, "economy, premiumEconomy, business, first")
}

func TestGenerateDocs_ConnectionAnnotations(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/airline_booking.yaml")

	result := GenerateDocs(g, nil)

	// searchFlights provides data to priceOffer and addOffer
	assert.Contains(t, result, "**Provides data to:**")
	// commitBooking receives from addOffer, addTraveler, createItinerary
	assert.Contains(t, result, "**Receives data from:**")
}

func TestGenerateDocs_DependencyOrdering(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/airline_booking.yaml")

	result := GenerateDocs(g, nil)

	// Entry nodes should come before dependent nodes
	searchIdx := strings.Index(result, "### searchFlights")
	priceIdx := strings.Index(result, "### priceOffer")
	createIdx := strings.Index(result, "### createItinerary")
	commitIdx := strings.Index(result, "### commitBooking")

	require.Greater(t, searchIdx, 0)
	require.Greater(t, priceIdx, 0)
	require.Greater(t, createIdx, 0)
	require.Greater(t, commitIdx, 0)

	// searchFlights should come before priceOffer
	assert.Less(t, searchIdx, priceIdx, "searchFlights should come before priceOffer")
	// createItinerary should come before commitBooking
	assert.Less(t, createIdx, commitIdx, "createItinerary should come before commitBooking")
}

func TestGenerateDocsSplit_AirlineBooking(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/airline_booking.yaml")

	files := GenerateDocsSplit(g, nil)

	// Should have index.md
	index, ok := files["index.md"]
	require.True(t, ok, "should have index.md")

	// Index should have summary table with links
	assert.Contains(t, index, "| [searchFlights](nodes/searchFlights.md)")
	assert.Contains(t, index, "```mermaid")

	// Should have per-node files
	for name := range g.Nodes {
		path := "nodes/" + name + ".md"
		content, ok := files[path]
		assert.True(t, ok, "should have %s", path)
		assert.Contains(t, content, "### "+name)
	}
}

func TestGenerateDocsSplit_EntryPointLinks(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/airline_booking.yaml")

	files := GenerateDocsSplit(g, nil)
	index := files["index.md"]

	// Entry points should have links to node files
	assert.Contains(t, index, "[**searchFlights**](nodes/searchFlights.md)")
	assert.Contains(t, index, "[**createItinerary**](nodes/createItinerary.md)")
}

func TestGenerateDocs_DeterministicOutput(t *testing.T) {
	g := mustParseFile(t, "testdata/valid/airline_booking.yaml")

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
	g := mustParseFile(t, "testdata/valid/airline_booking.yaml")

	entries := findEntryNodes(g)

	assert.Contains(t, entries, "searchFlights")
	assert.Contains(t, entries, "createItinerary")
	assert.NotContains(t, entries, "priceOffer")
	assert.NotContains(t, entries, "commitBooking")
}

func TestCollectSatisfiedNodes(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"searchFlights": {Name: "searchFlights", Satisfies: []string{"searchData"}},
			"priceOffer":    {Name: "priceOffer", Requires: []string{"searchData"}},
			"addOffer":      {Name: "addOffer", Requires: []string{"searchData"}},
		},
	}
	g.BuildSatisfierIndex()

	satisfied := collectSatisfiedNodes("searchFlights", g)

	assert.Contains(t, satisfied, "priceOffer")
	assert.Contains(t, satisfied, "addOffer")
	assert.NotContains(t, satisfied, "searchFlights")
}

func TestCollectRequiredNodes(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"createItinerary": {Name: "createItinerary", Satisfies: []string{"itinerary"}},
			"addOffer":        {Name: "addOffer", Satisfies: []string{"offer"}},
			"addTraveler":     {Name: "addTraveler", Satisfies: []string{"traveler"}},
			"commitBooking":   {Name: "commitBooking", Requires: []string{"itinerary", "offer", "traveler"}},
		},
	}
	g.BuildSatisfierIndex()

	required := collectRequiredNodes("commitBooking", g)

	assert.Contains(t, required, "createItinerary")
	assert.Contains(t, required, "addOffer")
	assert.Contains(t, required, "addTraveler")
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

func TestGenerateDocs_GraphTitle(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Title:   "My Custom API",
		Nodes: map[string]*Node{
			"getUser": {Name: "getUser", Description: "Get user", Adapter: "http"},
		},
	}
	result := GenerateDocs(g, nil)
	assert.Contains(t, result, "# My Custom API")
	assert.NotContains(t, result, "# API Workflow")
}

func TestGenerateDocs_TitleOverridesGraphTitle(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Title:   "Graph Title",
		Nodes: map[string]*Node{
			"getUser": {Name: "getUser", Description: "Get user", Adapter: "http"},
		},
	}
	result := GenerateDocs(g, &DocGenOptions{Title: "CLI Override"})
	assert.Contains(t, result, "# CLI Override")
	assert.NotContains(t, result, "# Graph Title")
}

func TestGenerateDocs_WithDescription(t *testing.T) {
	g := &Graph{
		Version:     "1.0.0",
		Description: "This API handles user management.",
		Nodes: map[string]*Node{
			"getUser": {Name: "getUser", Description: "Get user", Adapter: "http"},
		},
	}
	result := GenerateDocs(g, nil)
	assert.Contains(t, result, "This API handles user management.")
}

func TestGenerateDocs_WithWorkflows(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Workflows: []Workflow{
			{
				Name:        "User Flow",
				Description: "Create and fetch users",
			},
			{
				Name:        "Addon Flow",
				Kind:        "addon",
				After:       AfterSpec{"createUser"},
				Description: "Addon that splices after createUser",
			},
		},
		Nodes: map[string]*Node{
			"createUser": {Name: "createUser", Description: "Create user", Adapter: "http"},
			"getUser":    {Name: "getUser", Description: "Get user", Adapter: "http"},
		},
	}
	result := GenerateDocs(g, nil)
	assert.Contains(t, result, "## Workflows")
	assert.Contains(t, result, "### User Flow")
	assert.Contains(t, result, "Create and fetch users")
	assert.Contains(t, result, "splices after `createUser`")
}

func TestGenerateDocs_WithNotes(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Notes:   "Important design notes.",
		Nodes: map[string]*Node{
			"getUser": {Name: "getUser", Description: "Get user", Adapter: "http"},
		},
	}
	result := GenerateDocs(g, nil)
	assert.Contains(t, result, "## Notes")
	assert.Contains(t, result, "Important design notes.")
}

func TestGenerateDocs_WithConstraints(t *testing.T) {
	minLen, maxLen := 3, 3
	min, max := 1.0, 100.0
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"search": {
				Name: "search", Description: "Search", Adapter: "http",
				Inputs: []Input{
					{
						Name: "code", Type: "string",
						Constraints: &Constraint{
							MinLength: &minLen, MaxLength: &maxLen,
							Pattern:     "^[A-Z]{3}$",
							Description: "IATA code",
						},
					},
					{
						Name: "count", Type: "integer",
						Constraints: &Constraint{
							Min: &min, Max: &max,
						},
					},
					{Name: "plain", Type: "string"},
				},
				Outputs: []Output{{Name: "results", Type: "string"}},
			},
		},
	}
	result := GenerateDocs(g, nil)

	// Should have Constraints column
	assert.Contains(t, result, "| Constraints")
	// Pattern rendered with backticks
	assert.Contains(t, result, "`^[A-Z]{3}$`")
	// Length constraint
	assert.Contains(t, result, "len=3")
	// Range constraint
	assert.Contains(t, result, "1..100")
}

func TestGenerateDocsSplit_WithMetadata(t *testing.T) {
	g := &Graph{
		Version:     "1.0.0",
		Title:       "Split API",
		Description: "A split test.",
		Workflows: []Workflow{
			{Name: "Flow A", Description: "Test flow"},
		},
		Notes: "Split notes.",
		Nodes: map[string]*Node{
			"getUser": {Name: "getUser", Description: "Get user", Adapter: "http"},
		},
	}
	files := GenerateDocsSplit(g, nil)
	index := files["index.md"]

	assert.Contains(t, index, "# Split API")
	assert.Contains(t, index, "A split test.")
	assert.Contains(t, index, "## Workflows")
	assert.Contains(t, index, "### Flow A")
	assert.Contains(t, index, "## Notes")
	assert.Contains(t, index, "Split notes.")
}

func TestGenerateDocs_ConfigurableInput(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"search": {
				Name:        "search",
				Adapter:     "search",
				Description: "Search for things",
				Inputs: []Input{
					{Name: "query", Type: "string"},
					{Name: "carrier", Type: "string", Optional: true, Configurable: true},
					{Name: "cabin", Type: "string", Optional: true},
				},
				Outputs: []Output{{Name: "results", Type: "string"}},
			},
		},
	}
	g.BuildSatisfierIndex()
	result := GenerateDocs(g, nil)

	// Configurable input shows "configurable" in Required column.
	assert.Contains(t, result, "| carrier | string | configurable |")

	// Non-configurable optional shows "no" in Required column.
	assert.Contains(t, result, "| cabin | string | no |")

	// Required input shows "yes".
	assert.Contains(t, result, "| query | string | yes |")
}

func TestGenerateDocs_GRPCNodeNamesItsMethod(t *testing.T) {
	g, err := Parse([]byte(`version: "1.0.0"
proto: qdrant.protoset
nodes:
  createCollection:
    description: Create a collection.
    adapter: createCollection
    proto: qdrant.Collections/Create
    inputs:
      - name: collectionName
        type: string
    outputs:
      - name: collectionName
        type: string
        fromInput: collectionName
`))
	require.NoError(t, err)

	result := GenerateDocs(g, nil)

	assert.Contains(t, result, "**gRPC:** `qdrant.Collections/Create`")
	assert.Contains(t, result, "| collectionName | string | (echoes input `collectionName`) |")
}

func TestFormatDefaultValue(t *testing.T) {
	points := []any{
		map[string]any{"id": map[string]any{"num": "1"}, "payload": map[string]any{"city": map[string]any{"stringValue": "Berlin"}}},
		map[string]any{"id": map[string]any{"num": "2"}, "payload": map[string]any{"city": map[string]any{"stringValue": "London"}}},
	}
	tests := []struct {
		name  string
		value any
		max   int
		want  string
	}{
		{"a scalar as it is", 4, 0, "4"},
		{"a string as it is", "aat-qdrant-{{random 8}}", 0, "aat-qdrant-{{random 8}}"},
		{"a list of strings as JSON", []any{"", "extra"}, 0, `["","extra"]`},
		{"an object as JSON, strings quoted", map[string]any{"num": "1"}, 0, `{"num":"1"}`},
		{"a YAML map with non-string keys", map[any]any{"num": "1"}, 0, `{"num":"1"}`},
		{"a list of objects in full", points, 0,
			`[{"id":{"num":"1"},"payload":{"city":{"stringValue":"Berlin"}}},{"id":{"num":"2"},"payload":{"city":{"stringValue":"London"}}}]`},
		{"a long list as its first element and a count", points, 80,
			`[{"id":{"num":"1"},"payload":{"city":{"stringValue":"Berlin"}}}, … 2 items]`},
		{"a long object cut short", map[string]any{"a": "0123456789"}, 10, `{"a":"0123…`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FormatDefaultValue(tt.value, tt.max))
		})
	}
}

// Generated docs are compared with the committed copy in CI, so the same graph
// must give the same bytes every time, including where several nodes satisfy
// one requirement and the map they come from has no order.
func TestGenerateDocs_IsDeterministic(t *testing.T) {
	var yaml strings.Builder
	yaml.WriteString("version: \"1.0.0\"\nnodes:\n  reader:\n    adapter: reader\n    requires: [points]\n")
	for _, name := range []string{"upsertPoints", "jwtRbacUpsertPoints", "readOnlyUpsertPoints", "anonUpsertPoints", "zUpsertPoints"} {
		yaml.WriteString("  " + name + ":\n    adapter: " + name + "\n    satisfies: [points]\n")
	}
	g, err := Parse([]byte(yaml.String()))
	require.NoError(t, err)
	first := GenerateDocs(g, nil)
	for i := 0; i < 20; i++ {
		again, err := Parse([]byte(yaml.String()))
		require.NoError(t, err)
		require.Equal(t, first, GenerateDocs(again, nil), "run %d differs", i)
	}
	assert.Less(t, strings.Index(first, "anonUpsertPoints --> reader"), strings.Index(first, "zUpsertPoints --> reader"))
}

// TestGenerateDocs_ErrorStatus checks that generated docs say what status a
// rule's errors stand for, and the graph's mapping.
func TestGenerateDocs_ErrorStatus(t *testing.T) {
	g, err := Parse([]byte(`version: 1.0.0
errorDetection:
  - path: "*.result.errors"
    rule: non-empty
    details: {category: "*.result.errors.0.type"}
    categories: {VALIDATION: 400}
errorStatus:
  status: 500
  categories: {TEMPORARY: 503}
nodes:
  getUser:
    description: Read a user
    adapter: getUser
`))
	require.NoError(t, err)
	docs := GenerateDocs(g, nil)
	assert.Contains(t, docs, "status: VALIDATION 400")
	assert.Contains(t, docs, "Errors a rule gives no status: TEMPORARY 503, otherwise 500 (graph `errorStatus`).")
	assert.True(t, strings.Contains(docs, "`*.result.errors`"))
}
