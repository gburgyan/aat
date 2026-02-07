package adapter

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// expectedTravelportAdapters lists all adapter names that must be loaded from
// the travelport template directory. This matches the 7 graph nodes in
// graph/testdata/valid/travelport_booking.yaml.
var expectedTravelportAdapters = []string{
	"travelport.searchFlights",
	"travelport.priceOffer",
	"travelport.createWorkbench",
	"travelport.addOffer",
	"travelport.addTraveler",
	"travelport.commitBooking",
	"travelport.ignoreWorkbench",
}

func loadTravelportRegistry(t *testing.T) *Registry {
	t.Helper()
	registry := NewRegistry()
	count, err := LoadTemplates("testdata/templates/travelport", registry)
	require.NoError(t, err)
	require.Equal(t, 7, count)
	return registry
}

func TestTravelportTemplates_Load(t *testing.T) {
	registry := loadTravelportRegistry(t)

	names := registry.Names()
	assert.Len(t, names, 7)
	for _, name := range expectedTravelportAdapters {
		_, err := registry.Get(name)
		assert.NoError(t, err, "adapter %q should be registered", name)
	}
}

func TestTravelportTemplates_BuildRequest(t *testing.T) {
	registry := loadTravelportRegistry(t)

	tests := []struct {
		name        string
		adapterName string
		inputs      map[string]any
		wantMethod  string
		wantPath    string
		// bodyContains lists strings that must appear in the request body.
		bodyContains []string
		// bodyIsEmpty is true for requests with no body (DELETE).
		bodyIsEmpty bool
	}{
		{
			name:        "searchFlights",
			adapterName: "travelport.searchFlights",
			inputs: map[string]any{
				"origin":        "JFK",
				"destination":   "LAX",
				"departureDate": "2025-09-15",
				"passengers":    2,
			},
			wantMethod: "POST",
			wantPath:   "/11/air/catalog/search/catalogproductofferings",
			bodyContains: []string{
				`"@type": "CatalogProductOfferingsQueryRequest"`,
				`"@type": "CatalogProductOfferingsRequestAir"`,
				`"@type": "PassengerCriteria"`,
				`"value": "JFK"`,
				`"value": "LAX"`,
				`"departureDate": "2025-09-15"`,
				`"number": 2`,
			},
		},
		{
			name:        "priceOffer",
			adapterName: "travelport.priceOffer",
			inputs: map[string]any{
				"catalogOfferingsId": "cat-offer-123",
				"offeringId":         "offer-456",
			},
			wantMethod: "POST",
			wantPath:   "/11/air/price/offers/buildfromcatalogproductofferings",
			bodyContains: []string{
				`"@type": "OfferQueryBuildFromCatalogProductOfferings"`,
				`"@type": "BuildFromCatalogProductOfferingsRequestAir"`,
				`"value": "cat-offer-123"`,
				`"value": "offer-456"`,
			},
		},
		{
			name:        "createWorkbench",
			adapterName: "travelport.createWorkbench",
			inputs:      map[string]any{},
			wantMethod:  "POST",
			wantPath:    "/11/air/book/session/reservationworkbench",
			bodyContains: []string{
				`"@type": "ReservationID"`,
			},
		},
		{
			name:        "addOffer",
			adapterName: "travelport.addOffer",
			inputs: map[string]any{
				"workbenchId":        "wb-789",
				"catalogOfferingsId": "cat-offer-123",
				"offeringId":         "offer-456",
			},
			wantMethod: "POST",
			wantPath:   "/11/air/book/airoffer/reservationworkbench/wb-789/offers/buildfromcatalogproductofferings",
			bodyContains: []string{
				`"@type": "OfferQueryBuildFromCatalogProductOfferings"`,
				`"value": "cat-offer-123"`,
				`"value": "offer-456"`,
			},
		},
		{
			name:        "addTraveler",
			adapterName: "travelport.addTraveler",
			inputs: map[string]any{
				"workbenchId":       "wb-789",
				"surname":          "Smith",
				"givenName":        "John",
				"birthDate":        "1990-01-15",
				"gender":           "Male",
				"passengerTypeCode": "ADT",
			},
			wantMethod: "POST",
			wantPath:   "/11/air/book/traveler/reservationworkbench/wb-789/travelers",
			bodyContains: []string{
				`"@type": "Traveler"`,
				`"@type": "PersonName"`,
				`"Surname": "Smith"`,
				`"Given": "John"`,
				`"birthDate": "1990-01-15"`,
				`"gender": "Male"`,
				`"passengerTypeCode": "ADT"`,
			},
		},
		{
			name:        "commitBooking",
			adapterName: "travelport.commitBooking",
			inputs: map[string]any{
				"workbenchId": "wb-789",
			},
			wantMethod: "POST",
			wantPath:   "/11/air/book/reservation/reservations/wb-789",
			bodyContains: []string{
				`"@type": "ReservationQueryCommitReservation"`,
				`"ReceivedFrom": "AAT"`,
			},
		},
		{
			name:        "ignoreWorkbench",
			adapterName: "travelport.ignoreWorkbench",
			inputs: map[string]any{
				"workbenchId": "wb-789",
			},
			wantMethod:  "DELETE",
			wantPath:    "/11/air/book/session/reservationworkbench/wb-789",
			bodyIsEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, err := registry.Get(tt.adapterName)
			require.NoError(t, err)

			req, err := adapter.BuildRequest(tt.inputs, &EnvironmentConfig{})
			require.NoError(t, err)

			assert.Equal(t, tt.wantMethod, req.Method)
			assert.Equal(t, tt.wantPath, req.Path)

			if tt.bodyIsEmpty {
				assert.Nil(t, req.Body)
				return
			}

			assert.Equal(t, "application/json", req.Headers["Content-Type"])
			require.NotNil(t, req.Body, "expected body for %s", tt.adapterName)
			// Verify body is valid JSON.
			assert.True(t, json.Valid(req.Body), "body should be valid JSON: %s", string(req.Body))
			bodyStr := string(req.Body)
			for _, want := range tt.bodyContains {
				assert.True(t, strings.Contains(bodyStr, want),
					"body should contain %q, got:\n%s", want, bodyStr)
			}
		})
	}
}

func TestTravelportTemplates_ExtractOutputs(t *testing.T) {
	registry := loadTravelportRegistry(t)

	tests := []struct {
		name        string
		adapterName string
		responseJSON string
		wantOutputs map[string]any
	}{
		{
			name:        "searchFlights extracts offerings and ID",
			adapterName: "travelport.searchFlights",
			responseJSON: `{
				"CatalogProductOfferingsResponse": {
					"CatalogProductOfferings": {
						"Identifier": {"value": "cat-session-abc"},
						"CatalogProductOffering": [
							{"id": "offering-1"},
							{"id": "offering-2"}
						]
					}
				}
			}`,
			wantOutputs: map[string]any{
				"catalogOfferingsId": "cat-session-abc",
				"catalogOfferings": []any{
					map[string]any{"id": "offering-1"},
					map[string]any{"id": "offering-2"},
				},
			},
		},
		{
			name:        "priceOffer extracts price and currency",
			adapterName: "travelport.priceOffer",
			responseJSON: `{
				"OfferListResponse": {
					"OfferID": [
						{
							"Identifier": {"value": "confirmed-offer-123"},
							"Price": {
								"TotalPrice": 542.50,
								"CurrencyCode": {"value": "USD"}
							}
						}
					]
				}
			}`,
			wantOutputs: map[string]any{
				"totalPrice":         542.50,
				"currencyCode":       "USD",
				"confirmedOfferingId": "confirmed-offer-123",
			},
		},
		{
			name:        "createWorkbench extracts workbenchId",
			adapterName: "travelport.createWorkbench",
			responseJSON: `{
				"ReservationResponse": {
					"Identifier": {"value": "wb-new-456"}
				}
			}`,
			wantOutputs: map[string]any{
				"workbenchId": "wb-new-456",
			},
		},
		{
			name:        "addOffer extracts status and price",
			adapterName: "travelport.addOffer",
			responseJSON: `{
				"OfferListResponse": {
					"reservationStatus": "Confirmed",
					"OfferID": [
						{
							"Price": {
								"TotalPrice": 542.50
							}
						}
					]
				}
			}`,
			wantOutputs: map[string]any{
				"offerStatus": "Confirmed",
				"totalPrice":  542.50,
			},
		},
		{
			name:        "addTraveler extracts travelerId",
			adapterName: "travelport.addTraveler",
			responseJSON: `{
				"TravelerResponse": {
					"Identifier": {"value": "tvl-789"}
				}
			}`,
			wantOutputs: map[string]any{
				"travelerId": "tvl-789",
			},
		},
		{
			name:        "commitBooking extracts locator and status",
			adapterName: "travelport.commitBooking",
			responseJSON: `{
				"ReservationResponse": {
					"Identifier": {"value": "res-001"},
					"reservationStatus": "Confirmed",
					"Reservation": {
						"LocatorCode": "ABC123"
					}
				}
			}`,
			wantOutputs: map[string]any{
				"locator":           "ABC123",
				"reservationId":     "res-001",
				"reservationStatus": "Confirmed",
			},
		},
		{
			name:        "ignoreWorkbench extracts nothing",
			adapterName: "travelport.ignoreWorkbench",
			responseJSON: `{}`,
			wantOutputs: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, err := registry.Get(tt.adapterName)
			require.NoError(t, err)

			resp := &Response{
				StatusCode: 200,
				Body:       []byte(tt.responseJSON),
			}

			outputs, err := adapter.ExtractOutputs(resp)
			require.NoError(t, err)

			for key, want := range tt.wantOutputs {
				assert.Equal(t, want, outputs[key], "output %q", key)
			}
			assert.Len(t, outputs, len(tt.wantOutputs))
		})
	}
}
