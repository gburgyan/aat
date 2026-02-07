package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_Minimal(t *testing.T) {
	g, err := ParseFile("testdata/valid/minimal.yaml")
	require.NoError(t, err)

	assert.Equal(t, "1.0.0", g.Version)
	assert.Len(t, g.Nodes, 1)

	node := g.Nodes["getUser"]
	require.NotNil(t, node)
	assert.Equal(t, "getUser", node.Name)
	assert.Equal(t, "getUser", node.Adapter)
	assert.Equal(t, "Retrieve user details", node.Description)
	assert.Len(t, node.Inputs, 1)
	assert.Equal(t, "userId", node.Inputs[0].Name)
	assert.Equal(t, "string", node.Inputs[0].Type)
	assert.Len(t, node.Outputs, 2)
	assert.Equal(t, "name", node.Outputs[0].Name)
	assert.Equal(t, "email", node.Outputs[1].Name)
}

func TestParse_TravelFlow(t *testing.T) {
	g, err := ParseFile("testdata/valid/travel_flow.yaml")
	require.NoError(t, err)

	assert.Equal(t, "1.0.0", g.Version)
	assert.Len(t, g.Nodes, 4) // searchFlights, selectFare, addPassportInfo, cancelSearch

	// searchFlights node
	search := g.Nodes["searchFlights"]
	require.NotNil(t, search)
	assert.Equal(t, "searchFlights", search.Name)
	assert.Equal(t, "searchFlights", search.Adapter)
	assert.Len(t, search.Inputs, 5)
	assert.Equal(t, "cancelSearch", search.Cleanup)

	// Check optional input
	returnDate := search.Inputs[3]
	assert.Equal(t, "returnDate", returnDate.Name)
	assert.True(t, returnDate.Optional)

	// Check default value
	cabinClass := search.Inputs[4]
	assert.Equal(t, "cabinClass", cabinClass.Name)
	assert.Equal(t, "economy", cabinClass.Default)

	// Check array output with elementFields
	flights := search.Outputs[0]
	assert.Equal(t, "flights", flights.Name)
	assert.Equal(t, "flight[]", flights.Type)
	assert.Len(t, flights.ElementFields, 7)
	assert.Equal(t, "flightId", flights.ElementFields[0].Name)
	assert.Equal(t, "string", flights.ElementFields[0].Type)

	// selectFare node
	sel := g.Nodes["selectFare"]
	require.NotNil(t, sel)
	assert.Equal(t, "searchFlights.searchId", sel.Inputs[0].Source)

	// Edges
	assert.Len(t, g.Edges, 2)
	assert.Equal(t, "searchFlights.searchId", g.Edges[0].From)
	assert.Equal(t, "selectFare.searchId", g.Edges[0].To)
	assert.False(t, g.Edges[0].Select)
	assert.True(t, g.Edges[1].Select)

	// Conditions
	assert.Len(t, g.Conditions, 1)
	assert.Equal(t, "route.type == international", g.Conditions[0].When)
	assert.Equal(t, []string{"addPassportInfo"}, g.Conditions[0].Require)
	assert.Equal(t, []string{"selectFare"}, g.Conditions[0].Before)
}

func TestParse_WithConditions(t *testing.T) {
	g, err := ParseFile("testdata/valid/with_conditions.yaml")
	require.NoError(t, err)

	assert.Len(t, g.Conditions, 2)
	assert.Equal(t, "flag.premium == true", g.Conditions[0].When)
	assert.Equal(t, []string{"stepB"}, g.Conditions[0].Require)
	assert.Equal(t, "flag.express == true", g.Conditions[1].When)
	assert.Equal(t, []string{"stepA"}, g.Conditions[1].Before)
}

func TestParse_OptionalInputs(t *testing.T) {
	g, err := ParseFile("testdata/valid/optional_inputs.yaml")
	require.NoError(t, err)

	node := g.Nodes["createOrder"]
	require.NotNil(t, node)

	// currency: optional with default
	assert.True(t, node.Inputs[1].Optional)
	assert.Equal(t, "USD", node.Inputs[1].Default)

	// priority: optional enum with default
	assert.True(t, node.Inputs[2].Optional)
	assert.Equal(t, "normal", node.Inputs[2].Default)

	// notes: optional without default
	assert.True(t, node.Inputs[3].Optional)
	assert.Nil(t, node.Inputs[3].Default)
}

func TestParse_InvalidYAML(t *testing.T) {
	_, err := Parse([]byte(`{{{not yaml`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "YAML parse error")
}

func TestParse_EmptyDocument(t *testing.T) {
	_, err := Parse([]byte(``))
	require.Error(t, err)
}

func TestParseFile_NotFound(t *testing.T) {
	_, err := ParseFile("testdata/does_not_exist.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading graph file")
}

func TestSplitRef(t *testing.T) {
	tests := []struct {
		ref      string
		wantNode string
		wantField string
		wantErr  bool
	}{
		{ref: "node.field", wantNode: "node", wantField: "field"},
		{ref: "searchFlights.flights", wantNode: "searchFlights", wantField: "flights"},
		{ref: "nofield", wantErr: true},
		{ref: ".field", wantErr: true},
		{ref: "node.", wantErr: true},
		{ref: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			node, field, err := splitRef(tt.ref)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantNode, node)
			assert.Equal(t, tt.wantField, field)
		})
	}
}
