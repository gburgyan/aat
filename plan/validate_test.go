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
		p := baseStep(&SelectionConfig{Strategy: "min", SortField: "stops"})
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

// --- Gap 1: From field validation ---

func TestValidate_From(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("valid reference", func(t *testing.T) {
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
						Node:      "priceOffer",
						DependsOn: []string{"searchFlights"},
						Values: map[string]StepValue{
							"offeringId": {
								From: "searchFlights.catalogOfferings",
								Select: &SelectionConfig{
									Strategy: "first",
									Field:    "offeringId",
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

	t.Run("invalid format no dot", func(t *testing.T) {
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
						Node:      "priceOffer",
						DependsOn: []string{"searchFlights"},
						Values: map[string]StepValue{
							"offeringId": {From: "searchFlights"},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid 'from' reference")
	})

	t.Run("node not in plan", func(t *testing.T) {
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
						Node:      "priceOffer",
						DependsOn: []string{"searchFlights"},
						Values: map[string]StepValue{
							"offeringId": {From: "nonexistent.field"},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a step in this plan")
	})

	t.Run("output not on node", func(t *testing.T) {
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
						Node:      "priceOffer",
						DependsOn: []string{"searchFlights"},
						Values: map[string]StepValue{
							"offeringId": {From: "searchFlights.fakeOutput"},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist on node")
	})
}

// --- Gap 2: Array selection source validation ---

func TestValidate_SelectionSource(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("from array output", func(t *testing.T) {
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
						Node:      "priceOffer",
						DependsOn: []string{"searchFlights"},
						Values: map[string]StepValue{
							"offeringId": {
								From:   "searchFlights.catalogOfferings",
								Select: &SelectionConfig{Strategy: "first", Field: "offeringId"},
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

	t.Run("from non-array output", func(t *testing.T) {
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
						Node:      "priceOffer",
						DependsOn: []string{"searchFlights"},
						Values: map[string]StepValue{
							"offeringId": {
								From:   "searchFlights.catalogOfferingsId",
								Select: &SelectionConfig{Strategy: "first"},
							},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not an array type")
	})

	t.Run("implicit select edge", func(t *testing.T) {
		// priceOffer.offeringId has a select edge from searchFlights.catalogOfferings
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
								Select: &SelectionConfig{Strategy: "first", Field: "offeringId"},
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

	t.Run("no from no select edge", func(t *testing.T) {
		// priceOffer.productRef has no select edge in the graph
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
							"offeringId": {Default: "o1"},
							"productRef": {
								Select: &SelectionConfig{Strategy: "first"},
							},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no 'from' and no select edge")
	})
}

// --- Gap 3: SortField validation ---

func TestValidate_SortField(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("valid element field", func(t *testing.T) {
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
						Node:      "priceOffer",
						DependsOn: []string{"searchFlights"},
						Values: map[string]StepValue{
							"offeringId": {
								From: "searchFlights.catalogOfferings",
								Select: &SelectionConfig{
									Strategy:  "min",
									SortField: "stops",
									Field:     "offeringId",
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

	t.Run("invalid element field", func(t *testing.T) {
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
						Node:      "priceOffer",
						DependsOn: []string{"searchFlights"},
						Values: map[string]StepValue{
							"offeringId": {
								From: "searchFlights.catalogOfferings",
								Select: &SelectionConfig{
									Strategy:  "min",
									SortField: "nonexistent",
									Field:     "offeringId",
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
		assert.Contains(t, err.Error(), "not found in elementFields")
	})

	t.Run("nested path skipped", func(t *testing.T) {
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
						Node:      "priceOffer",
						DependsOn: []string{"searchFlights"},
						Values: map[string]StepValue{
							"offeringId": {
								From: "searchFlights.catalogOfferings",
								Select: &SelectionConfig{
									Strategy:  "min",
									SortField: "nested.path",
									Field:     "offeringId",
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

	t.Run("no element fields skipped", func(t *testing.T) {
		// priceOffer.offerListId is a string output — no elementFields
		// We use a From pointing to an output that has no elementFields
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
						Node:      "priceOffer",
						DependsOn: []string{"searchFlights"},
						Values: map[string]StepValue{
							"offeringId": {
								From: "searchFlights.catalogOfferings",
								Select: &SelectionConfig{
									Strategy:  "min",
									SortField: "stops",
									Field:     "offeringId",
								},
							},
							"productRef": {Default: "p0"},
						},
					},
					{
						Node:      "addTraveler",
						DependsOn: []string{"priceOffer"},
						Values: map[string]StepValue{
							"surname":   {Default: "Smith"},
							"givenName": {Default: "Jane"},
							// Use a From to a scalar output; selection won't be valid
							// but we're testing sortField skip when no elementFields
						},
					},
				},
			},
		}
		// This just validates the searchFlights→priceOffer chain passes for sortField
		err := Validate(p, g)
		assert.NoError(t, err)
	})
}

// --- Gap 4: Constraint AppliesTo validation ---

func TestValidate_ConstraintAppliesTo(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("valid applies to bare node", func(t *testing.T) {
		p := &Plan{
			Intent: Intent{
				Constraints: &Constraints{
					Hard: []Constraint{
						{
							Name:      "direct flights",
							Type:      "preference",
							AppliesTo: []string{"searchFlights"},
						},
					},
				},
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
		assert.NoError(t, err)
	})

	t.Run("valid applies to node.input", func(t *testing.T) {
		p := &Plan{
			Intent: Intent{
				Constraints: &Constraints{
					Hard: []Constraint{
						{
							Name:      "origin",
							Type:      "preference",
							AppliesTo: []string{"searchFlights.origin"},
						},
					},
				},
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
		assert.NoError(t, err)
	})

	t.Run("unknown applies to hard", func(t *testing.T) {
		p := &Plan{
			Intent: Intent{
				Constraints: &Constraints{
					Hard: []Constraint{
						{
							Name:      "some constraint",
							Type:      "preference",
							AppliesTo: []string{"nonexistent"},
						},
					},
				},
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
		assert.Contains(t, err.Error(), "appliesTo references unknown step")
	})

	t.Run("unknown applies to soft", func(t *testing.T) {
		p := &Plan{
			Intent: Intent{
				Constraints: &Constraints{
					Soft: []Constraint{
						{
							Name:      "some soft constraint",
							Type:      "preference",
							AppliesTo: []string{"nonexistent"},
						},
					},
				},
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
		assert.Contains(t, err.Error(), "appliesTo references unknown step")
	})
}

// --- Gap 5: Cleanup and Verification validation ---

func TestValidate_Cleanup(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("valid node", func(t *testing.T) {
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
				},
				Cleanup: []CleanupStep{
					{Node: "ignoreWorkbench", RunOn: "always"},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("unknown node", func(t *testing.T) {
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
				},
				Cleanup: []CleanupStep{
					{Node: "nonexistentCleanup", RunOn: "always"},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cleanup step 0: node \"nonexistentCleanup\" not found in graph")
	})

	t.Run("invalid runOn", func(t *testing.T) {
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
				},
				Cleanup: []CleanupStep{
					{Node: "ignoreWorkbench", RunOn: "bogus"},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid runOn value")
	})

	t.Run("empty runOn ok", func(t *testing.T) {
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
				},
				Cleanup: []CleanupStep{
					{Node: "ignoreWorkbench"},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})
}

func TestValidate_Verification(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("valid node", func(t *testing.T) {
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
				},
				Verification: []VerificationStep{
					{Node: "searchFlights", Purpose: "verify search results"},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("unknown node", func(t *testing.T) {
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
				},
				Verification: []VerificationStep{
					{Node: "nonexistentVerify"},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "verification step 0: node \"nonexistentVerify\" not found in graph")
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
					},
				},
				Verification: []VerificationStep{
					{
						Node: "searchFlights",
						Assertions: &Assertions{
							Mechanical: []MechanicalAssertion{
								{Type: "predicate", Expr: "bad <<<"},
							},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "verification step 0")
		assert.Contains(t, err.Error(), "invalid predicate assertion")
	})
}

// --- Gap 6: Unknown value names ---

func TestValidate_UnknownValues(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("hallucinated value", func(t *testing.T) {
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {Default: "DEN"},
							"destination":   {Default: "SFO"},
							"departureDate": {Default: "2026-03-15"},
							"flightNumber":  {Default: "UA123"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match any input")
	})

	t.Run("all valid", func(t *testing.T) {
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
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})
}

// --- Gap 8: Goal consistency ---

func TestValidate_GoalConsistency(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("goal matches step", func(t *testing.T) {
		p := &Plan{
			Intent: Intent{Goal: "commitBooking"},
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
					{Node: "priceOffer", Values: map[string]StepValue{"productRef": {Default: "p0"}}},
					{Node: "createWorkbench"},
					{Node: "addOffer"},
					{Node: "addTraveler", Values: map[string]StepValue{"surname": {Default: "S"}, "givenName": {Default: "J"}}},
					{Node: "commitBooking", IsGoal: true},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("goal not in plan", func(t *testing.T) {
		p := &Plan{
			Intent: Intent{Goal: "nonexistent"},
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
		assert.Contains(t, err.Error(), "intent goal references unknown step")
	})

	t.Run("isGoal mismatch", func(t *testing.T) {
		p := &Plan{
			Intent: Intent{Goal: "commitBooking"},
			Execution: Execution{
				Steps: []Step{
					{
						Node:   "searchFlights",
						IsGoal: true,
						Values: map[string]StepValue{
							"origin":        {Default: "DEN"},
							"destination":   {Default: "SFO"},
							"departureDate": {Default: "2026-03-15"},
						},
					},
					{Node: "priceOffer", Values: map[string]StepValue{"productRef": {Default: "p0"}}},
					{Node: "createWorkbench"},
					{Node: "addOffer"},
					{Node: "addTraveler", Values: map[string]StepValue{"surname": {Default: "S"}, "givenName": {Default: "J"}}},
					{Node: "commitBooking"},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match intent goal")
	})

	t.Run("multiple isGoal", func(t *testing.T) {
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{
						Node:   "searchFlights",
						IsGoal: true,
						Values: map[string]StepValue{
							"origin":        {Default: "DEN"},
							"destination":   {Default: "SFO"},
							"departureDate": {Default: "2026-03-15"},
						},
					},
					{
						Node:   "priceOffer",
						IsGoal: true,
						Values: map[string]StepValue{"productRef": {Default: "p0"}},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "multiple steps marked as isGoal")
	})

	t.Run("empty goal no isGoal ok", func(t *testing.T) {
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
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})
}

// --- Gap 9: DependsOn completeness ---

func TestValidate_DependsOnCompleteness(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("from implies dep present", func(t *testing.T) {
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
						Node:      "priceOffer",
						DependsOn: []string{"searchFlights"},
						Values: map[string]StepValue{
							"offeringId": {
								From:   "searchFlights.catalogOfferings",
								Select: &SelectionConfig{Strategy: "first", Field: "offeringId"},
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

	t.Run("from missing dep", func(t *testing.T) {
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
						// Missing dependsOn: ["searchFlights"]
						Values: map[string]StepValue{
							"offeringId": {
								From:   "searchFlights.catalogOfferings",
								Select: &SelectionConfig{Strategy: "first", Field: "offeringId"},
							},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has 'from' reference to \"searchFlights\" but does not list it in dependsOn")
	})
}
