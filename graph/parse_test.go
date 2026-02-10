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

func TestParse_TravelportBooking(t *testing.T) {
	g, err := ParseFile("testdata/valid/travelport_booking.yaml")
	require.NoError(t, err)

	assert.Equal(t, "1.0.0", g.Version)
	assert.Len(t, g.Nodes, 7)
	assert.Len(t, g.Edges, 11)
	assert.Empty(t, g.Conditions)

	// --- searchFlights ---
	search := g.Nodes["searchFlights"]
	require.NotNil(t, search)
	assert.Equal(t, "searchFlights", search.Name)
	assert.Equal(t, "travelport.searchFlights", search.Adapter)
	assert.Equal(t, "Search for available flights via Travelport catalog", search.Description)
	assert.Len(t, search.Inputs, 6)
	assert.Len(t, search.Outputs, 2)

	// Required inputs
	assert.Equal(t, "origin", search.Inputs[0].Name)
	assert.Equal(t, "string", search.Inputs[0].Type)
	assert.False(t, search.Inputs[0].Optional)
	assert.Equal(t, "destination", search.Inputs[1].Name)
	assert.Equal(t, "departureDate", search.Inputs[2].Name)
	assert.Equal(t, "date", search.Inputs[2].Type)

	// Optional inputs with defaults
	assert.Equal(t, "returnDate", search.Inputs[3].Name)
	assert.True(t, search.Inputs[3].Optional)
	assert.Nil(t, search.Inputs[3].Default)
	assert.Equal(t, "passengers", search.Inputs[4].Name)
	assert.True(t, search.Inputs[4].Optional)
	assert.Equal(t, 1, search.Inputs[4].Default)
	assert.Equal(t, "cabinPreference", search.Inputs[5].Name)
	assert.Equal(t, "enum[economy, premiumEconomy, business, first]", search.Inputs[5].Type)
	assert.True(t, search.Inputs[5].Optional)
	assert.Equal(t, "economy", search.Inputs[5].Default)

	// Array output with elementFields
	offerings := search.Outputs[0]
	assert.Equal(t, "catalogOfferings", offerings.Name)
	assert.Equal(t, "offering[]", offerings.Type)
	require.Len(t, offerings.ElementFields, 9)
	assert.Equal(t, "offeringId", offerings.ElementFields[0].Name)
	assert.Equal(t, "string", offerings.ElementFields[0].Type)
	assert.Equal(t, "id", offerings.ElementFields[0].Path)
	assert.Equal(t, "id", offerings.ElementFields[0].EffectivePath())
	assert.Equal(t, "departureTime", offerings.ElementFields[3].Name)
	assert.Equal(t, "datetime", offerings.ElementFields[3].Type)
	assert.Empty(t, offerings.ElementFields[3].Path)
	assert.Equal(t, "departureTime", offerings.ElementFields[3].EffectivePath())
	assert.Equal(t, "stops", offerings.ElementFields[6].Name)
	assert.Equal(t, "integer", offerings.ElementFields[6].Type)
	assert.Equal(t, "cabin", offerings.ElementFields[7].Name)
	assert.Equal(t, "productRef", offerings.ElementFields[8].Name)
	assert.Equal(t, "string", offerings.ElementFields[8].Type)
	assert.Equal(t, "ProductBrandOptions.0.ProductBrandOffering.0.Product.0.productRef", offerings.ElementFields[8].Path)
	assert.Equal(t, "ProductBrandOptions.0.ProductBrandOffering.0.Product.0.productRef", offerings.ElementFields[8].EffectivePath())

	// Scalar output
	assert.Equal(t, "catalogOfferingsId", search.Outputs[1].Name)
	assert.Equal(t, "string", search.Outputs[1].Type)
	assert.Empty(t, search.Outputs[1].ElementFields)

	// No cleanup on searchFlights
	assert.Empty(t, search.Cleanup)

	// --- priceOffer ---
	price := g.Nodes["priceOffer"]
	require.NotNil(t, price)
	assert.Equal(t, "travelport.priceOffer", price.Adapter)
	assert.Len(t, price.Inputs, 3)
	assert.Equal(t, "catalogOfferingsId", price.Inputs[0].Name)
	assert.Equal(t, "offeringId", price.Inputs[1].Name)
	assert.Equal(t, "productRef", price.Inputs[2].Name)
	assert.Len(t, price.Outputs, 4)
	assert.Equal(t, "offerListId", price.Outputs[0].Name)
	assert.Equal(t, "string", price.Outputs[0].Type)
	assert.Equal(t, "offerId", price.Outputs[1].Name)
	assert.Equal(t, "totalPrice", price.Outputs[2].Name)
	assert.Equal(t, "money", price.Outputs[2].Type)
	assert.Equal(t, "currencyCode", price.Outputs[3].Name)

	// --- createWorkbench ---
	wb := g.Nodes["createWorkbench"]
	require.NotNil(t, wb)
	assert.Equal(t, "travelport.createWorkbench", wb.Adapter)
	assert.Empty(t, wb.Inputs)
	assert.Len(t, wb.Outputs, 1)
	assert.Equal(t, "workbenchId", wb.Outputs[0].Name)
	assert.Equal(t, "ignoreWorkbench", wb.Cleanup)

	// --- addOffer ---
	addOff := g.Nodes["addOffer"]
	require.NotNil(t, addOff)
	assert.Equal(t, "travelport.addOffer", addOff.Adapter)
	assert.Len(t, addOff.Inputs, 4)
	assert.Equal(t, "workbenchId", addOff.Inputs[0].Name)
	assert.Equal(t, "catalogOfferingsId", addOff.Inputs[1].Name)
	assert.Equal(t, "offeringId", addOff.Inputs[2].Name)
	assert.Equal(t, "productRef", addOff.Inputs[3].Name)
	assert.Len(t, addOff.Outputs, 1)
	assert.Equal(t, "offerStatus", addOff.Outputs[0].Name)

	// --- addTraveler ---
	trav := g.Nodes["addTraveler"]
	require.NotNil(t, trav)
	assert.Equal(t, "travelport.addTraveler", trav.Adapter)
	assert.Len(t, trav.Inputs, 6)
	assert.Equal(t, "surname", trav.Inputs[1].Name)
	assert.False(t, trav.Inputs[1].Optional)
	assert.Equal(t, "givenName", trav.Inputs[2].Name)
	assert.Equal(t, "birthDate", trav.Inputs[3].Name)
	assert.False(t, trav.Inputs[3].Optional)
	assert.Equal(t, "gender", trav.Inputs[4].Name)
	assert.Equal(t, "enum[Male, Female, Undisclosed]", trav.Inputs[4].Type)
	assert.False(t, trav.Inputs[4].Optional)
	assert.Equal(t, "passengerTypeCode", trav.Inputs[5].Name)
	assert.True(t, trav.Inputs[5].Optional)
	assert.Equal(t, "ADT", trav.Inputs[5].Default)
	assert.Len(t, trav.Outputs, 1)
	assert.Equal(t, "travelerId", trav.Outputs[0].Name)

	// --- commitBooking ---
	commit := g.Nodes["commitBooking"]
	require.NotNil(t, commit)
	assert.Equal(t, "travelport.commitBooking", commit.Adapter)
	assert.Len(t, commit.Inputs, 3)
	assert.Equal(t, "workbenchId", commit.Inputs[0].Name)
	assert.Equal(t, "offerStatus", commit.Inputs[1].Name)
	assert.Equal(t, "travelerId", commit.Inputs[2].Name)
	assert.Len(t, commit.Outputs, 2)
	assert.Equal(t, "locator", commit.Outputs[0].Name)
	assert.Equal(t, "locatorSource", commit.Outputs[1].Name)

	// --- ignoreWorkbench ---
	ignore := g.Nodes["ignoreWorkbench"]
	require.NotNil(t, ignore)
	assert.Equal(t, "travelport.ignoreWorkbench", ignore.Adapter)
	assert.Len(t, ignore.Inputs, 1)
	assert.Equal(t, "workbenchId", ignore.Inputs[0].Name)
	assert.Len(t, ignore.Outputs, 1)
	assert.Equal(t, "acknowledged", ignore.Outputs[0].Name)
	assert.Equal(t, "boolean", ignore.Outputs[0].Type)

	// --- Edges ---
	edgeMap := make(map[string]Edge)
	for _, e := range g.Edges {
		edgeMap[e.From+"->"+e.To] = e
	}

	// searchFlights → priceOffer
	assert.Contains(t, edgeMap, "searchFlights.catalogOfferingsId->priceOffer.catalogOfferingsId")
	selectEdge, ok := edgeMap["searchFlights.catalogOfferings->priceOffer.offeringId"]
	assert.True(t, ok)
	assert.True(t, selectEdge.Select)

	// searchFlights → addOffer
	assert.Contains(t, edgeMap, "searchFlights.catalogOfferingsId->addOffer.catalogOfferingsId")
	addOfferOfferingEdge, ok := edgeMap["searchFlights.catalogOfferings->addOffer.offeringId"]
	assert.True(t, ok)
	assert.True(t, addOfferOfferingEdge.Select)
	addOfferProductEdge, ok := edgeMap["searchFlights.catalogOfferings->addOffer.productRef"]
	assert.True(t, ok)
	assert.True(t, addOfferProductEdge.Select)

	// createWorkbench fan-out
	assert.Contains(t, edgeMap, "createWorkbench.workbenchId->addOffer.workbenchId")
	assert.Contains(t, edgeMap, "createWorkbench.workbenchId->addTraveler.workbenchId")
	assert.Contains(t, edgeMap, "createWorkbench.workbenchId->commitBooking.workbenchId")
	assert.Contains(t, edgeMap, "createWorkbench.workbenchId->ignoreWorkbench.workbenchId")

	// Ordering edges into commitBooking
	assert.Contains(t, edgeMap, "addOffer.offerStatus->commitBooking.offerStatus")
	assert.Contains(t, edgeMap, "addTraveler.travelerId->commitBooking.travelerId")

	// Three select edges (priceOffer.offeringId + addOffer.offeringId + addOffer.productRef)
	selectCount := 0
	for _, e := range g.Edges {
		if e.Select {
			selectCount++
		}
	}
	assert.Equal(t, 3, selectCount)
}

func TestParse_WithOASFields(t *testing.T) {
	data := []byte(`
version: "1.0.0"
oas: petstore.yaml
nodes:
  listPets:
    description: "List pets"
    adapter: listPets
    oas:
      operationId: listPets
    inputs:
      - name: limit
        type: integer
    outputs:
      - name: pets
        type: string
  getPet:
    description: "Get a pet"
    adapter: getPet
    oas:
      operationId: getPet
      spec: other-spec.yaml
    inputs:
      - name: petId
        type: string
    outputs:
      - name: name
        type: string
edges: []
`)

	g, err := Parse(data)
	require.NoError(t, err)

	// Graph-level OAS
	assert.Equal(t, "petstore.yaml", g.OAS)

	// Node with OAS ref using graph default
	listPets := g.Nodes["listPets"]
	require.NotNil(t, listPets.OAS)
	assert.Equal(t, "listPets", listPets.OAS.OperationID)
	assert.Empty(t, listPets.OAS.Spec)

	// Node with OAS ref and spec override
	getPet := g.Nodes["getPet"]
	require.NotNil(t, getPet.OAS)
	assert.Equal(t, "getPet", getPet.OAS.OperationID)
	assert.Equal(t, "other-spec.yaml", getPet.OAS.Spec)
}

func TestParse_WithoutOASFields(t *testing.T) {
	// Existing minimal graph should still parse fine (backward compat)
	g, err := ParseFile("testdata/valid/minimal.yaml")
	require.NoError(t, err)

	assert.Empty(t, g.OAS)
	node := g.Nodes["getUser"]
	require.NotNil(t, node)
	assert.Nil(t, node.OAS)
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
