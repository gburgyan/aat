package mcp

import (
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
)

func newDomainTestServer(kb *domain.KnowledgeBase) *Server {
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: adapter.NewRegistry(),
		KB:       kb,
		Manifest: &ProjectManifest{Name: "test"},
	}
	return NewServer(ctx)
}

func testKB() *domain.KnowledgeBase {
	return &domain.KnowledgeBase{
		Concepts: map[string]*domain.Concept{
			"routeValidity": {
				Name:        "routeValidity",
				Description: "Origin and destination must be valid route pairs",
				AppliesTo:   []string{"origin", "destination"},
				Constraint:  "Origin and destination must differ",
				Examples: map[string][]string{
					"valid":   {"JFK-LAX", "ORD-SFO"},
					"invalid": {"JFK-JFK"},
				},
			},
			"dateConstraint": {
				Name:        "dateConstraint",
				Description: "Departure must be in the future",
				AppliesTo:   []string{"departureDate"},
				Constraint:  "Date must be after today",
			},
		},
		Types: map[string]*domain.TypeDef{
			"airportCode": {
				Name:        "airportCode",
				Description: "IATA airport code",
				Format:      "3-letter uppercase",
				Validation:  "^[A-Z]{3}$",
				Pool:        "airports",
			},
			"passengerInfo": {
				Name:        "passengerInfo",
				Description: "Passenger details",
				Format:      "object",
				Fields: map[string]*domain.FieldDef{
					"firstName": {Type: "string", Description: "Given name"},
					"lastName":  {Type: "string", Description: "Family name"},
				},
			},
		},
		ValuePools: map[string]*domain.ValuePool{
			"airports": {
				Name:        "airports",
				Description: "Major airports",
				Type:        "airportCode",
				Values:      []string{"JFK", "LAX", "ORD", "SFO", "ATL", "DFW", "DEN"},
			},
			"carriers": {
				Name:        "carriers",
				Description: "Airline carriers",
				Type:        "carrierCode",
				Groups: map[string][]string{
					"us":   {"AA", "UA", "DL"},
					"intl": {"BA", "LH"},
				},
			},
		},
	}
}

// --- list_concepts ---

func TestHandleListConcepts_NilKB(t *testing.T) {
	srv := newDomainTestServer(nil)
	result := callTool(t, srv.handleListConcepts, nil)

	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not configured")
}

func TestHandleListConcepts_Empty(t *testing.T) {
	srv := newDomainTestServer(&domain.KnowledgeBase{
		Concepts: map[string]*domain.Concept{},
	})
	result := callTool(t, srv.handleListConcepts, nil)

	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "No concepts defined")
}

func TestHandleListConcepts_Populated(t *testing.T) {
	srv := newDomainTestServer(testKB())
	result := callTool(t, srv.handleListConcepts, nil)

	assert.False(t, result.IsError)
	text := resultText(t, result)

	// Both concepts present
	assert.Contains(t, text, "**dateConstraint**")
	assert.Contains(t, text, "**routeValidity**")
	assert.Contains(t, text, "Origin and destination must be valid route pairs")
	assert.Contains(t, text, "applies to: origin, destination")

	// Sorted: dateConstraint before routeValidity
	assert.Less(t, indexOf(text, "dateConstraint"), indexOf(text, "routeValidity"))
}

// --- list_types ---

func TestHandleListTypes_NilKB(t *testing.T) {
	srv := newDomainTestServer(nil)
	result := callTool(t, srv.handleListTypes, nil)

	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not configured")
}

func TestHandleListTypes_Empty(t *testing.T) {
	srv := newDomainTestServer(&domain.KnowledgeBase{
		Types: map[string]*domain.TypeDef{},
	})
	result := callTool(t, srv.handleListTypes, nil)

	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "No types defined")
}

func TestHandleListTypes_Populated(t *testing.T) {
	srv := newDomainTestServer(testKB())
	result := callTool(t, srv.handleListTypes, nil)

	assert.False(t, result.IsError)
	text := resultText(t, result)

	assert.Contains(t, text, "**airportCode**")
	assert.Contains(t, text, "IATA airport code")
	assert.Contains(t, text, "format: 3-letter uppercase")
	assert.Contains(t, text, "**passengerInfo**")
	assert.Contains(t, text, "Fields: firstName, lastName")

	// Sorted: airportCode before passengerInfo
	assert.Less(t, indexOf(text, "airportCode"), indexOf(text, "passengerInfo"))
}

// --- list_value_pools ---

func TestHandleListValuePools_NilKB(t *testing.T) {
	srv := newDomainTestServer(nil)
	result := callTool(t, srv.handleListValuePools, nil)

	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not configured")
}

func TestHandleListValuePools_Empty(t *testing.T) {
	srv := newDomainTestServer(&domain.KnowledgeBase{
		ValuePools: map[string]*domain.ValuePool{},
	})
	result := callTool(t, srv.handleListValuePools, nil)

	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "No value pools defined")
}

func TestHandleListValuePools_Populated(t *testing.T) {
	srv := newDomainTestServer(testKB())
	result := callTool(t, srv.handleListValuePools, nil)

	assert.False(t, result.IsError)
	text := resultText(t, result)

	// airports: 7 flat values, preview first 5
	assert.Contains(t, text, "**airports**")
	assert.Contains(t, text, "Major airports")
	assert.Contains(t, text, "type: airportCode")
	assert.Contains(t, text, "7 values")
	assert.Contains(t, text, "JFK")
	assert.Contains(t, text, "...")

	// carriers: 5 values total (3 us + 2 intl)
	assert.Contains(t, text, "**carriers**")
	assert.Contains(t, text, "5 values")

	// Sorted: airports before carriers
	assert.Less(t, indexOf(text, "airports"), indexOf(text, "carriers"))
}

// --- explain_concept ---

func TestHandleExplainConcept_NilKB(t *testing.T) {
	srv := newDomainTestServer(nil)
	result := callTool(t, srv.handleExplainConcept, map[string]any{"name": "routeValidity"})

	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not configured")
}

func TestHandleExplainConcept_Valid(t *testing.T) {
	srv := newDomainTestServer(testKB())
	result := callTool(t, srv.handleExplainConcept, map[string]any{"name": "routeValidity"})

	assert.False(t, result.IsError)
	text := resultText(t, result)

	assert.Contains(t, text, "# routeValidity")
	assert.Contains(t, text, "Origin and destination must be valid route pairs")
	assert.Contains(t, text, "**Applies to:** origin, destination")
	assert.Contains(t, text, "**Constraint:** Origin and destination must differ")
	assert.Contains(t, text, "## Examples")
	assert.Contains(t, text, "**valid:** JFK-LAX, ORD-SFO")
	assert.Contains(t, text, "**invalid:** JFK-JFK")
}

func TestHandleExplainConcept_Unknown(t *testing.T) {
	srv := newDomainTestServer(testKB())
	result := callTool(t, srv.handleExplainConcept, map[string]any{"name": "nonexistent"})

	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "unknown concept")
}

func TestHandleExplainConcept_MissingParam(t *testing.T) {
	srv := newDomainTestServer(testKB())
	result := callTool(t, srv.handleExplainConcept, nil)

	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

func TestHandleExplainConcept_WithCrossReferences(t *testing.T) {
	// Create a KB where a concept's appliesTo field matches a type name
	// and that type references a pool.
	kb := &domain.KnowledgeBase{
		Concepts: map[string]*domain.Concept{
			"airportRule": {
				Name:        "airportRule",
				Description: "Airport codes must be valid",
				AppliesTo:   []string{"airportCode"},
				Constraint:  "Must be a real airport",
			},
		},
		Types: map[string]*domain.TypeDef{
			"airportCode": {
				Name:        "airportCode",
				Description: "IATA code",
				Format:      "3-letter",
				Pool:        "airports",
			},
		},
		ValuePools: map[string]*domain.ValuePool{
			"airports": {
				Name:        "airports",
				Description: "Airport pool",
				Type:        "airportCode",
				Values:      []string{"JFK", "LAX"},
			},
		},
	}

	srv := newDomainTestServer(kb)
	result := callTool(t, srv.handleExplainConcept, map[string]any{"name": "airportRule"})

	assert.False(t, result.IsError)
	text := resultText(t, result)

	assert.Contains(t, text, "## Related")
	assert.Contains(t, text, "**Types:** airportCode")
	assert.Contains(t, text, "**Value pools:** airports")
}

func TestHandleExplainConcept_NoCrossReferences(t *testing.T) {
	kb := &domain.KnowledgeBase{
		Concepts: map[string]*domain.Concept{
			"simple": {
				Name:        "simple",
				Description: "A simple concept",
				AppliesTo:   []string{"someField"},
			},
		},
		Types:      map[string]*domain.TypeDef{},
		ValuePools: map[string]*domain.ValuePool{},
	}

	srv := newDomainTestServer(kb)
	result := callTool(t, srv.handleExplainConcept, map[string]any{"name": "simple"})

	assert.False(t, result.IsError)
	text := resultText(t, result)

	// No Related section when there are no cross-references
	assert.NotContains(t, text, "## Related")
}
