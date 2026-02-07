package plan

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

func TestValidate_TravelportBooking_HappyPath(t *testing.T) {
	g := loadTravelportGraph(t)
	p, err := ParseFile("testdata/valid/travelport_booking.yaml")
	require.NoError(t, err)

	err = Validate(p, g)
	assert.NoError(t, err)
}

func TestValidate_Minimal_HappyPath(t *testing.T) {
	g := loadTravelportGraph(t)
	p, err := ParseFile("testdata/valid/minimal.yaml")
	require.NoError(t, err)

	err = Validate(p, g)
	assert.NoError(t, err)
}

func TestValidate_MissingNode(t *testing.T) {
	g := loadTravelportGraph(t)
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{Node: "nonexistentNode"},
			},
		},
	}

	err := Validate(p, g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in graph")
}

func TestValidate_MissingRequiredInput(t *testing.T) {
	g := loadTravelportGraph(t)
	// searchFlights requires origin, destination, departureDate
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{
					Node: "searchFlights",
					Values: map[string]StepValue{
						"origin": {Default: "DEN"},
						// missing destination and departureDate
					},
				},
			},
		},
	}

	err := Validate(p, g)
	require.Error(t, err)
	valErr, ok := err.(*ValidationError)
	require.True(t, ok)
	assert.Contains(t, valErr.Error(), "destination")
	assert.Contains(t, valErr.Error(), "departureDate")
}

func TestValidate_VersionIncompatible(t *testing.T) {
	g := loadTravelportGraph(t)
	p := &Plan{
		Metadata: Metadata{
			GraphVersion: "2.0.0", // graph is 1.0.0
		},
		Execution: Execution{
			Steps: []Step{
				{
					Node: "searchFlights",
					Values: map[string]StepValue{
						"origin":        {Default: "DEN"},
						"destination":   {Default: "SFO"},
						"departureDate": {Default: "2026-03-15"},
					},
				},
			},
		},
	}

	err := Validate(p, g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "incompatible")
}

func TestValidate_VersionCompatible_MinorDrift(t *testing.T) {
	g := loadTravelportGraph(t)
	// Plan minor > graph minor is MinorDrift, not Incompatible — allowed
	p := &Plan{
		Metadata: Metadata{
			GraphVersion: "1.1.0",
		},
		Execution: Execution{
			Steps: []Step{
				{
					Node: "searchFlights",
					Values: map[string]StepValue{
						"origin":        {Default: "DEN"},
						"destination":   {Default: "SFO"},
						"departureDate": {Default: "2026-03-15"},
					},
				},
			},
		},
	}

	err := Validate(p, g)
	assert.NoError(t, err) // minor drift is a warning, not an error
}

func TestValidate_DuplicateSteps(t *testing.T) {
	g := loadTravelportGraph(t)
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{
					Node: "searchFlights",
					Values: map[string]StepValue{
						"origin":        {Default: "DEN"},
						"destination":   {Default: "SFO"},
						"departureDate": {Default: "2026-03-15"},
					},
				},
				{
					Node: "searchFlights",
					Values: map[string]StepValue{
						"origin":        {Default: "LAX"},
						"destination":   {Default: "JFK"},
						"departureDate": {Default: "2026-04-01"},
					},
				},
			},
		},
	}

	err := Validate(p, g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate step node")
}

func TestValidate_DependsOnUnknownStep(t *testing.T) {
	g := loadTravelportGraph(t)
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{
					Node:      "searchFlights",
					DependsOn: []string{"nonexistent"},
					Values: map[string]StepValue{
						"origin":        {Default: "DEN"},
						"destination":   {Default: "SFO"},
						"departureDate": {Default: "2026-03-15"},
					},
				},
			},
		},
	}

	err := Validate(p, g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dependsOn references unknown step")
}

func TestValidate_DependsOnSelf(t *testing.T) {
	g := loadTravelportGraph(t)
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{
					Node:      "searchFlights",
					DependsOn: []string{"searchFlights"},
					Values: map[string]StepValue{
						"origin":        {Default: "DEN"},
						"destination":   {Default: "SFO"},
						"departureDate": {Default: "2026-03-15"},
					},
				},
			},
		},
	}

	err := Validate(p, g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "references itself")
}

func TestValidate_DependsOnCycle(t *testing.T) {
	g := loadTravelportGraph(t)
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{
					Node:      "searchFlights",
					DependsOn: []string{"createWorkbench"},
					Values: map[string]StepValue{
						"origin":        {Default: "DEN"},
						"destination":   {Default: "SFO"},
						"departureDate": {Default: "2026-03-15"},
					},
				},
				{
					Node:      "createWorkbench",
					DependsOn: []string{"searchFlights"},
				},
			},
		},
	}

	err := Validate(p, g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestValidate_InputWiredByEdge_NoValueNeeded(t *testing.T) {
	g := loadTravelportGraph(t)
	// addOffer requires workbenchId, catalogOfferingsId, offeringId — all wired by edges
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{
					Node: "searchFlights",
					Values: map[string]StepValue{
						"origin":        {Default: "DEN"},
						"destination":   {Default: "SFO"},
						"departureDate": {Default: "2026-03-15"},
					},
				},
				{Node: "priceOffer"},
				{Node: "createWorkbench"},
				{Node: "addOffer"}, // all inputs come from edges
			},
		},
	}

	err := Validate(p, g)
	assert.NoError(t, err)
}

func TestValidate_InvalidGraphVersion(t *testing.T) {
	g := loadTravelportGraph(t)
	p := &Plan{
		Metadata: Metadata{
			GraphVersion: "not-a-version",
		},
		Execution: Execution{
			Steps: []Step{
				{
					Node: "searchFlights",
					Values: map[string]StepValue{
						"origin":        {Default: "DEN"},
						"destination":   {Default: "SFO"},
						"departureDate": {Default: "2026-03-15"},
					},
				},
			},
		},
	}

	err := Validate(p, g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid plan graphVersion")
}
