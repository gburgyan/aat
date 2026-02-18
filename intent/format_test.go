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
				Template:    "plans/booking.yaml",
			},
			{
				Name: "Simple Flow",
			},
		},
		Nodes: map[string]*graph.Node{},
	}
	result := FormatGraph(g)
	assert.Contains(t, result, "## Workflows")
	assert.Contains(t, result, "**Booking Flow** [template]: Standard booking")
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

func TestFormatGraph_ConfigurableAnnotation(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"search": {
				Name:    "search",
				Adapter: "search",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "carrier", Type: "string", Optional: true, Configurable: true},
					{Name: "cabin", Type: "string", Optional: true},
					{Name: "passengers", Type: "integer", Optional: true, Configurable: true, Default: 1},
				},
				Outputs: []graph.Output{{Name: "results", Type: "string"}},
			},
		},
	}
	result := FormatGraph(g)

	// Configurable inputs show (configurable), not (optional).
	assert.Contains(t, result, "carrier: string (configurable)")
	assert.Contains(t, result, "passengers: integer (configurable)")
	assert.NotContains(t, result, "carrier: string (optional)")

	// Non-configurable optional inputs still show (optional).
	assert.Contains(t, result, "cabin: string (optional)")

	// Required inputs show neither.
	assert.Contains(t, result, "origin: string")
	assert.NotContains(t, result, "origin: string (optional)")
	assert.NotContains(t, result, "origin: string (configurable)")
}

func TestFormatGraph_WithAddonWorkflows(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{
				Name:     "Main Booking",
				Template: "plans/main.yaml",
			},
			{
				Name:     "Seat Selection",
				Kind:     "addon",
				Template: "plans/seat.yaml",
				After:    "book",
			},
		},
		Nodes: map[string]*graph.Node{},
	}
	result := FormatGraph(g)

	// Addon marker should appear
	assert.Contains(t, result, "**Seat Selection** [addon] [template]")

	// After should appear
	assert.Contains(t, result, "After: book")

	// Regular workflow should not have [addon]
	assert.Contains(t, result, "**Main Booking** [template]")
}

