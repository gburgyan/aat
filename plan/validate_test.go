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
	// addOffer requires workbenchId, catalogOfferingsId, offeringId, productRef — all wired by edges
	// priceOffer requires productRef which has no edge, so provide it
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
				{Node: "priceOffer", Values: map[string]StepValue{
					"productRef": {Default: "p0"},
				}},
				{Node: "createWorkbench"},
				{Node: "addOffer"}, // all inputs come from edges
			},
		},
	}

	err := Validate(p, g)
	assert.NoError(t, err)
}

func TestValidate_PredicateExpressions(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("valid filter expression", func(t *testing.T) {
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
						Node: "priceOffer",
						Values: map[string]StepValue{
							"offeringId": {
								Select: &SelectionConfig{
									Strategy: "first",
									Filter:   "stops == 0 && carrier == 'AA'",
								},
							},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("invalid filter expression", func(t *testing.T) {
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
						Node: "priceOffer",
						Values: map[string]StepValue{
							"offeringId": {
								Select: &SelectionConfig{
									Strategy: "first",
									Filter:   "stops ==",
								},
							},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid filter expression")
	})

	t.Run("valid constraint expression", func(t *testing.T) {
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
						Node: "priceOffer",
						Values: map[string]StepValue{
							"offeringId": {
								Constraint: "value > 0",
							},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("invalid constraint expression", func(t *testing.T) {
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
						Node: "priceOffer",
						Values: map[string]StepValue{
							"offeringId": {
								Constraint: "value @@ 'bad'",
							},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid constraint expression")
	})

	t.Run("valid predicate assertion", func(t *testing.T) {
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
						Assertions: &Assertions{
							Mechanical: []MechanicalAssertion{
								{
									Type: "predicate",
									Expr: "price.amount < 1000",
								},
							},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("invalid predicate assertion", func(t *testing.T) {
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
						Assertions: &Assertions{
							Mechanical: []MechanicalAssertion{
								{
									Type: "predicate",
									Expr: "price.amount <<< 'invalid'",
								},
							},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid predicate assertion")
	})

	t.Run("non-predicate assertion type ignored", func(t *testing.T) {
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
						Assertions: &Assertions{
							Mechanical: []MechanicalAssertion{
								{
									Type:   "status",
									Expect: 200,
								},
							},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})
}

func TestValidate_SelectionStrategies(t *testing.T) {
	g := loadTravelportGraph(t)

	baseStep := func(sel *SelectionConfig) *Plan {
		return &Plan{
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
						Node: "priceOffer",
						Values: map[string]StepValue{
							"offeringId":  {Select: sel},
							"productRef":  {Default: "p0"},
						},
					},
				},
			},
		}
	}

	t.Run("unknown strategy", func(t *testing.T) {
		p := baseStep(&SelectionConfig{Strategy: "bogus"})
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown selection strategy")
	})

	t.Run("min requires field", func(t *testing.T) {
		p := baseStep(&SelectionConfig{Strategy: "min"})
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "min strategy requires field or sortField")
	})

	t.Run("max requires field", func(t *testing.T) {
		p := baseStep(&SelectionConfig{Strategy: "max"})
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "max strategy requires field or sortField")
	})

	t.Run("min with field ok", func(t *testing.T) {
		p := baseStep(&SelectionConfig{Strategy: "min", Field: "price"})
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("min with sortField ok", func(t *testing.T) {
		p := baseStep(&SelectionConfig{Strategy: "min", SortField: "price"})
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("match requires filter", func(t *testing.T) {
		p := baseStep(&SelectionConfig{Strategy: "match"})
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "match strategy requires filter")
	})

	t.Run("match with filter ok", func(t *testing.T) {
		p := baseStep(&SelectionConfig{Strategy: "match", Filter: "carrier == 'AA'"})
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("index negative", func(t *testing.T) {
		p := baseStep(&SelectionConfig{Strategy: "index", Index: -1})
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-negative index")
	})

	t.Run("index zero ok", func(t *testing.T) {
		p := baseStep(&SelectionConfig{Strategy: "index", Index: 0})
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("first ok", func(t *testing.T) {
		p := baseStep(&SelectionConfig{Strategy: "first"})
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("last ok", func(t *testing.T) {
		p := baseStep(&SelectionConfig{Strategy: "last"})
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("random ok", func(t *testing.T) {
		p := baseStep(&SelectionConfig{Strategy: "random"})
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("empty strategy ok", func(t *testing.T) {
		p := baseStep(&SelectionConfig{Strategy: ""})
		err := Validate(p, g)
		assert.NoError(t, err)
	})
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
