package plan

import (
	"testing"

	"github.com/gburgyan/aat/config"
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
	assert.Contains(t, err.Error(), "duplicate step id")
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
	// Without edges, all required inputs need explicit plan values.
	// addOffer needs: workbenchId, catalogOfferingsId, offeringId, productRef
	// priceOffer needs: catalogOfferingsId, offeringId, productRef
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
				{Node: "priceOffer", DependsOn: []string{"searchFlights"}, Selections: map[string]StepSelection{
					"offering": {From: "searchFlights.catalogOfferings", Strategy: "first"},
				}, Values: map[string]StepValue{
					"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
					"offeringId":         {FromSelection: "offering.offeringId"},
					"productRef":         {FromSelection: "offering.productRef"},
				}},
				{Node: "createWorkbench"},
				{Node: "addOffer", DependsOn: []string{"searchFlights", "createWorkbench"}, Selections: map[string]StepSelection{
					"offering": {From: "searchFlights.catalogOfferings", Strategy: "first"},
				}, Values: map[string]StepValue{
					"workbenchId":        {From: "createWorkbench.workbenchId"},
					"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
					"offeringId":         {FromSelection: "offering.offeringId"},
					"productRef":         {FromSelection: "offering.productRef"},
				}},
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
						Node:      "priceOffer",
						DependsOn: []string{"searchFlights"},
						Values: map[string]StepValue{
							"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
							"offeringId": {
								From: "searchFlights.catalogOfferings",
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
						Node:      "priceOffer",
						DependsOn: []string{"searchFlights"},
						Values: map[string]StepValue{
							"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
							"offeringId": {
								Constraint: "value > 0",
								Default:    "o1",
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
						Node:      "priceOffer",
						DependsOn: []string{"searchFlights"},
						Values: map[string]StepValue{
							"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
							"offeringId":         {From: "searchFlights.catalogOfferings", Select: sel},
							"productRef":         {Default: "p0"},
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

	t.Run("llm strategy rejected", func(t *testing.T) {
		p := baseStep(&SelectionConfig{Strategy: "llm", Prompt: "pick the cheapest option"})
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown selection strategy")
	})

	t.Run("llm without prompt also rejected", func(t *testing.T) {
		p := baseStep(&SelectionConfig{Strategy: "llm"})
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown selection strategy")
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
							"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
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
							"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
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
		assert.Contains(t, err.Error(), "no 'from' reference")
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
							"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
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
							"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
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
							"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
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
							"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
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
							"workbenchId": {Default: "wb-123"},
							"surname":     {Default: "Smith"},
							"givenName":   {Default: "Jane"},
							"birthDate":   {Default: "1990-01-15"},
							"gender":      {Default: "Male"},
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
					{Node: "priceOffer", DependsOn: []string{"searchFlights"}, Values: map[string]StepValue{
						"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
						"offeringId":         {Default: "o1"},
						"productRef":         {Default: "p0"},
					}},
					{Node: "createWorkbench"},
					{Node: "addOffer", DependsOn: []string{"searchFlights", "createWorkbench"}, Values: map[string]StepValue{
						"workbenchId":        {From: "createWorkbench.workbenchId"},
						"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
						"offeringId":         {Default: "o1"},
						"productRef":         {Default: "p0"},
					}},
					{Node: "addTraveler", DependsOn: []string{"createWorkbench"}, Values: map[string]StepValue{
						"workbenchId": {From: "createWorkbench.workbenchId"},
						"surname":     {Default: "S"},
						"givenName":   {Default: "J"},
						"birthDate":   {Default: "1990-01-15"},
						"gender":      {Default: "Male"},
					}},
					{Node: "commitBooking", DependsOn: []string{"addOffer", "addTraveler", "createWorkbench"}, IsGoal: true, Values: map[string]StepValue{
						"workbenchId": {From: "createWorkbench.workbenchId"},
						"offerStatus": {From: "addOffer.offerStatus"},
						"travelerId":  {From: "addTraveler.travelerId"},
					}},
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
					{Node: "addTraveler", Values: map[string]StepValue{"surname": {Default: "S"}, "givenName": {Default: "J"}, "birthDate": {Default: "1990-01-15"}, "gender": {Default: "Male"}}},
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
							"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
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

// --- Named Selections ---

func TestValidate_NamedSelections_HappyPath(t *testing.T) {
	g := loadTravelportGraph(t)
	p, err := ParseFile("testdata/valid/named_selections.yaml")
	require.NoError(t, err)

	err = Validate(p, g)
	assert.NoError(t, err)
}

func TestValidate_NamedSelections(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("valid selection", func(t *testing.T) {
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
						Selections: map[string]StepSelection{
							"offering": {
								From:     "searchFlights.catalogOfferings",
								Strategy: "first",
							},
						},
						Values: map[string]StepValue{
							"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
							"offeringId":         {FromSelection: "offering.offeringId"},
							"productRef":         {FromSelection: "offering.productRef"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("selection with empty from", func(t *testing.T) {
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
						Selections: map[string]StepSelection{
							"offering": {From: ""},
						},
						Values: map[string]StepValue{
							"offeringId": {FromSelection: "offering.offeringId"},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty 'from'")
	})

	t.Run("selection references non-array output", func(t *testing.T) {
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
						Selections: map[string]StepSelection{
							"offering": {
								From:     "searchFlights.catalogOfferingsId",
								Strategy: "first",
							},
						},
						Values: map[string]StepValue{
							"offeringId": {FromSelection: "offering.offeringId"},
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

	t.Run("selection references unknown step", func(t *testing.T) {
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
						Selections: map[string]StepSelection{
							"offering": {
								From:     "nonexistent.catalogOfferings",
								Strategy: "first",
							},
						},
						Values: map[string]StepValue{
							"offeringId": {FromSelection: "offering.offeringId"},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "references unknown step")
	})

	t.Run("selection missing dependsOn", func(t *testing.T) {
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
						// Missing dependsOn
						Selections: map[string]StepSelection{
							"offering": {
								From:     "searchFlights.catalogOfferings",
								Strategy: "first",
							},
						},
						Values: map[string]StepValue{
							"offeringId": {FromSelection: "offering.offeringId"},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not list it in dependsOn")
	})

	t.Run("fromSelection references unknown selection", func(t *testing.T) {
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
							"offeringId": {FromSelection: "nonexistent.offeringId"},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "references unknown selection")
	})

	t.Run("fromSelection conflicts with from", func(t *testing.T) {
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
						Selections: map[string]StepSelection{
							"offering": {
								From:     "searchFlights.catalogOfferings",
								Strategy: "first",
							},
						},
						Values: map[string]StepValue{
							"offeringId": {
								FromSelection: "offering.offeringId",
								From:          "searchFlights.catalogOfferings",
							},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mutually exclusive")
	})

	t.Run("fromSelection conflicts with select", func(t *testing.T) {
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
						Selections: map[string]StepSelection{
							"offering": {
								From:     "searchFlights.catalogOfferings",
								Strategy: "first",
							},
						},
						Values: map[string]StepValue{
							"offeringId": {
								FromSelection: "offering.offeringId",
								Select:        &SelectionConfig{Strategy: "first"},
							},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mutually exclusive")
	})

	t.Run("selection strategy validation", func(t *testing.T) {
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
						Selections: map[string]StepSelection{
							"offering": {
								From:     "searchFlights.catalogOfferings",
								Strategy: "llm",
								// llm is no longer a valid strategy
							},
						},
						Values: map[string]StepValue{
							"offeringId": {FromSelection: "offering.offeringId"},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown selection strategy")
	})
}

// --- ExpectFailure validation (Task 22a) ---

func TestValidate_ExpectFailure(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("valid expectFailure", func(t *testing.T) {
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
						ExpectFailure: &ExpectFailure{
							Status:      []int{400, 422},
							Description: "Invalid request expected",
						},
					},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("empty status list", func(t *testing.T) {
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
						ExpectFailure: &ExpectFailure{
							Status: []int{},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must have at least one status code")
	})

	t.Run("status less than 400", func(t *testing.T) {
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
						ExpectFailure: &ExpectFailure{
							Status: []int{200},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 200 must be >= 400")
	})

	t.Run("contradicting status assertion", func(t *testing.T) {
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
						ExpectFailure: &ExpectFailure{
							Status: []int{401},
						},
						Assertions: &Assertions{
							Mechanical: []MechanicalAssertion{
								{Type: "status", Expect: 200},
							},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "contradicts expectFailure")
	})

	t.Run("non-contradicting error status assertion ok", func(t *testing.T) {
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
						ExpectFailure: &ExpectFailure{
							Status: []int{401},
						},
						Assertions: &Assertions{
							Mechanical: []MechanicalAssertion{
								{Type: "status", Expect: 401},
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

func TestParseFromSelection(t *testing.T) {
	tests := []struct {
		name  string
		ref   string
		sel   string
		field string
	}{
		{name: "with dot", ref: "offering.offeringId", sel: "offering", field: "offeringId"},
		{name: "without dot", ref: "offering", sel: "offering", field: ""},
		{name: "empty", ref: "", sel: "", field: ""},
		{name: "multiple dots", ref: "offering.nested.path", sel: "offering", field: "nested.path"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sel, field := ParseFromSelection(tc.ref)
			assert.Equal(t, tc.sel, sel)
			assert.Equal(t, tc.field, field)
		})
	}
}

// --- fromSelection field validation ---

func TestValidate_FromSelectionFieldValidation(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("valid elementField", func(t *testing.T) {
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
						Selections: map[string]StepSelection{
							"offering": {
								From:     "searchFlights.catalogOfferings",
								Strategy: "first",
							},
						},
						Values: map[string]StepValue{
							"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
							"offeringId":         {FromSelection: "offering.offeringId"},
							"productRef":         {FromSelection: "offering.productRef"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("invalid elementField", func(t *testing.T) {
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
						Selections: map[string]StepSelection{
							"offering": {
								From:     "searchFlights.catalogOfferings",
								Strategy: "first",
							},
						},
						Values: map[string]StepValue{
							"offeringId": {FromSelection: "offering.origin"},
							"productRef": {FromSelection: "offering.productRef"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fromSelection")
		assert.Contains(t, err.Error(), "origin")
		assert.Contains(t, err.Error(), "elementField")
	})

	t.Run("no elementFields on output skips check", func(t *testing.T) {
		// catalogOfferingsId is a string output with no elementFields
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
						Selections: map[string]StepSelection{
							"offering": {
								From:     "searchFlights.catalogOfferingsId",
								Strategy: "first",
							},
						},
						Values: map[string]StepValue{
							"offeringId": {FromSelection: "offering.anything"},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		// Should fail on "not an array type" but NOT on elementField check
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not an array type")
		assert.NotContains(t, err.Error(), "elementField")
	})

	t.Run("fromSelection without dot skips field check", func(t *testing.T) {
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
						Selections: map[string]StepSelection{
							"offering": {
								From:     "searchFlights.catalogOfferings",
								Strategy: "first",
							},
						},
						Values: map[string]StepValue{
							"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
							// No dot — references the whole selection, field check skipped
							"offeringId": {FromSelection: "offering"},
							"productRef": {Default: "p0"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})
}

// --- Filter field validation for named selections ---

func TestValidate_FilterFieldValidation(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("valid filter fields", func(t *testing.T) {
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
						Selections: map[string]StepSelection{
							"offering": {
								From:     "searchFlights.catalogOfferings",
								Strategy: "match",
								Filter:   "carrier == 'QF'",
							},
						},
						Values: map[string]StepValue{
							"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
							"offeringId":         {FromSelection: "offering.offeringId"},
							"productRef":         {FromSelection: "offering.productRef"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("invalid filter field", func(t *testing.T) {
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
						Selections: map[string]StepSelection{
							"offering": {
								From:     "searchFlights.catalogOfferings",
								Strategy: "match",
								Filter:   "origin == 'MEL'",
							},
						},
						Values: map[string]StepValue{
							"offeringId": {FromSelection: "offering.offeringId"},
							"productRef": {FromSelection: "offering.productRef"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "filter for selection")
		assert.Contains(t, err.Error(), "origin")
		assert.Contains(t, err.Error(), "elementField")
	})

	t.Run("filter with multiple fields mixed valid and invalid", func(t *testing.T) {
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
						Selections: map[string]StepSelection{
							"offering": {
								From:     "searchFlights.catalogOfferings",
								Strategy: "match",
								Filter:   "carrier == 'QF' && nonexistent > 0",
							},
						},
						Values: map[string]StepValue{
							"offeringId": {FromSelection: "offering.offeringId"},
							"productRef": {FromSelection: "offering.productRef"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nonexistent")
		// carrier should NOT produce an error since it's a valid elementField
		assert.NotContains(t, err.Error(), "\"carrier\"")
	})
}

// --- Step Aliasing ---

func TestValidate_StepAliasing(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("same node with different IDs is valid", func(t *testing.T) {
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{
						ID:   "search_leg1",
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {Default: "MEL"},
							"destination":   {Default: "SYD"},
							"departureDate": {Default: "2026-03-01"},
						},
					},
					{
						ID:        "search_leg2",
						Node:      "searchFlights",
						DependsOn: []string{"search_leg1"},
						Values: map[string]StepValue{
							"origin":        {Default: "SYD"},
							"destination":   {Default: "BNE"},
							"departureDate": {Default: "2026-03-05"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("same node same ID is duplicate", func(t *testing.T) {
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{
						ID:   "dup",
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {Default: "MEL"},
							"destination":   {Default: "SYD"},
							"departureDate": {Default: "2026-03-01"},
						},
					},
					{
						ID:   "dup",
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {Default: "SYD"},
							"destination":   {Default: "BNE"},
							"departureDate": {Default: "2026-03-05"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate step id \"dup\"")
	})

	t.Run("dependsOn uses step IDs", func(t *testing.T) {
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{
						ID:   "search_leg1",
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {Default: "MEL"},
							"destination":   {Default: "SYD"},
							"departureDate": {Default: "2026-03-01"},
						},
					},
					{
						ID:        "search_leg2",
						Node:      "searchFlights",
						DependsOn: []string{"search_leg1"},
						Values: map[string]StepValue{
							"origin":        {Default: "SYD"},
							"destination":   {Default: "BNE"},
							"departureDate": {Default: "2026-03-05"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("dependsOn with node name fails for aliased steps", func(t *testing.T) {
		// When steps have IDs, dependsOn must use the step ID, not the node name.
		// "searchFlights" is not a step ID here; the IDs are search_leg1 and search_leg2.
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{
						ID:   "search_leg1",
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {Default: "MEL"},
							"destination":   {Default: "SYD"},
							"departureDate": {Default: "2026-03-01"},
						},
					},
					{
						ID:        "search_leg2",
						Node:      "searchFlights",
						DependsOn: []string{"searchFlights"}, // wrong: should be "search_leg1"
						Values: map[string]StepValue{
							"origin":        {Default: "SYD"},
							"destination":   {Default: "BNE"},
							"departureDate": {Default: "2026-03-05"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown step \"searchFlights\"")
	})

	t.Run("from references use step IDs", func(t *testing.T) {
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{
						ID:   "search_leg1",
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {Default: "MEL"},
							"destination":   {Default: "SYD"},
							"departureDate": {Default: "2026-03-01"},
						},
					},
					{
						Node:      "priceOffer",
						DependsOn: []string{"search_leg1"},
						Values: map[string]StepValue{
							"catalogOfferingsId": {From: "search_leg1.catalogOfferingsId"},
							"offeringId": {
								From:   "search_leg1.catalogOfferings",
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

	t.Run("from with node name fails for aliased steps", func(t *testing.T) {
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{
						ID:   "search_leg1",
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {Default: "MEL"},
							"destination":   {Default: "SYD"},
							"departureDate": {Default: "2026-03-01"},
						},
					},
					{
						Node:      "priceOffer",
						DependsOn: []string{"search_leg1"},
						Values: map[string]StepValue{
							"offeringId": {
								From:   "searchFlights.catalogOfferings", // wrong: should use "search_leg1"
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
		assert.Contains(t, err.Error(), "not a step in this plan")
	})

	t.Run("selections from uses step IDs", func(t *testing.T) {
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{
						ID:   "search_leg1",
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {Default: "MEL"},
							"destination":   {Default: "SYD"},
							"departureDate": {Default: "2026-03-01"},
						},
					},
					{
						Node:      "priceOffer",
						DependsOn: []string{"search_leg1"},
						Selections: map[string]StepSelection{
							"offering": {
								From:     "search_leg1.catalogOfferings",
								Strategy: "first",
							},
						},
						Values: map[string]StepValue{
							"catalogOfferingsId": {From: "search_leg1.catalogOfferingsId"},
							"offeringId":         {FromSelection: "offering.offeringId"},
							"productRef":         {FromSelection: "offering.productRef"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("duplicate node requires explicit values", func(t *testing.T) {
		// When searchFlights appears twice (with IDs), graph edges don't auto-wire.
		// Required inputs without plan values or defaults should fail.
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{
						ID:   "search_leg1",
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":      {Default: "MEL"},
							"destination": {Default: "SYD"},
							// Missing departureDate — required, no default, no edge for duplicates
						},
					},
					{
						ID:        "search_leg2",
						Node:      "searchFlights",
						DependsOn: []string{"search_leg1"},
						Values: map[string]StepValue{
							"origin":        {Default: "SYD"},
							"destination":   {Default: "BNE"},
							"departureDate": {Default: "2026-03-05"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has no plan value")
		assert.Contains(t, err.Error(), "departureDate")
	})

	t.Run("goal uses step ID", func(t *testing.T) {
		p := &Plan{
			Intent: Intent{Goal: "search_leg2"},
			Execution: Execution{
				Steps: []Step{
					{
						ID:   "search_leg1",
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {Default: "MEL"},
							"destination":   {Default: "SYD"},
							"departureDate": {Default: "2026-03-01"},
						},
					},
					{
						ID:        "search_leg2",
						Node:      "searchFlights",
						DependsOn: []string{"search_leg1"},
						IsGoal:    true,
						Values: map[string]StepValue{
							"origin":        {Default: "SYD"},
							"destination":   {Default: "BNE"},
							"departureDate": {Default: "2026-03-05"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("constraint appliesTo uses step IDs", func(t *testing.T) {
		p := &Plan{
			Intent: Intent{
				Constraints: &Constraints{
					Hard: []Constraint{
						{
							Name:      "carrier",
							Type:      "preference",
							AppliesTo: []string{"search_leg1.origin"},
						},
					},
				},
			},
			Execution: Execution{
				Steps: []Step{
					{
						ID:   "search_leg1",
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {Default: "MEL"},
							"destination":   {Default: "SYD"},
							"departureDate": {Default: "2026-03-01"},
						},
					},
					{
						ID:        "search_leg2",
						Node:      "searchFlights",
						DependsOn: []string{"search_leg1"},
						Values: map[string]StepValue{
							"origin":        {Default: "SYD"},
							"destination":   {Default: "BNE"},
							"departureDate": {Default: "2026-03-05"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("StepID method", func(t *testing.T) {
		s1 := Step{Node: "searchFlights"}
		assert.Equal(t, "searchFlights", s1.StepID())

		s2 := Step{ID: "search_leg1", Node: "searchFlights"}
		assert.Equal(t, "search_leg1", s2.StepID())

		s3 := Step{ID: "", Node: "searchFlights"}
		assert.Equal(t, "searchFlights", s3.StepID())
	})
}

// --- Plan-level auth validation ---

func TestValidate_PlanAuth_Valid(t *testing.T) {
	g := loadTravelportGraph(t)
	p := &Plan{
		Auth: &config.AuthConfig{
			Type: "bearer",
			Credentials: map[string]config.SecretRef{
				"token": {Source: "literal", Value: "my-token"},
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
}

func TestValidate_PlanAuth_Invalid(t *testing.T) {
	g := loadTravelportGraph(t)
	p := &Plan{
		Auth: &config.AuthConfig{
			Type: "oauth2",
			// Missing tokenUrl and credentials
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
	assert.Contains(t, err.Error(), "plan auth: auth.tokenUrl is required")
	assert.Contains(t, err.Error(), "plan auth: auth.credentials.username")
}

// --- FromResolved validation ---

func TestValidate_FromResolved(t *testing.T) {
	g := loadTravelportGraph(t)

	t.Run("valid fromResolved references earlier input", func(t *testing.T) {
		// searchFlights inputs: origin, destination, departureDate (in that order)
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {Pool: []any{"DEN", "ORD"}},
							"destination":   {FromResolved: "origin"},
							"departureDate": {Default: "2026-03-15"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		assert.NoError(t, err)
	})

	t.Run("fromResolved references non-existent input", func(t *testing.T) {
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {FromResolved: "nonexistent"},
							"destination":   {Default: "SFO"},
							"departureDate": {Default: "2026-03-15"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not an input on node")
	})

	t.Run("fromResolved forward reference", func(t *testing.T) {
		// searchFlights inputs: origin, destination, departureDate
		// origin tries to reference destination, which comes after it
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {FromResolved: "destination"},
							"destination":   {Default: "SFO"},
							"departureDate": {Default: "2026-03-15"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "forward reference")
	})

	t.Run("fromResolved with from is mutually exclusive", func(t *testing.T) {
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {Default: "DEN"},
							"destination":   {FromResolved: "origin", From: "someStep.output"},
							"departureDate": {Default: "2026-03-15"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mutually exclusive with fromResolved")
	})

	t.Run("fromResolved with pool is mutually exclusive", func(t *testing.T) {
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{
						Node: "searchFlights",
						Values: map[string]StepValue{
							"origin":        {Default: "DEN"},
							"destination":   {FromResolved: "origin", Pool: []any{"SFO", "LAX"}},
							"departureDate": {Default: "2026-03-15"},
						},
					},
				},
			},
		}
		err := Validate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mutually exclusive with fromResolved")
	})
}

func TestValidate_PlanAuth_Nil(t *testing.T) {
	// No plan auth → no auth validation errors
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
			},
		},
	}
	err := Validate(p, g)
	assert.NoError(t, err)
}
