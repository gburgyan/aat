package intent

import (
	"testing"
	"time"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixDependsOn_AddsMissing(t *testing.T) {
	g := loadTravelportGraph(t)

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchFlights"},
				{Node: "createWorkbench"},
				{Node: "addOffer"}, // missing dependsOn
				{Node: "addTraveler"},
				{Node: "commitBooking"},
			},
		},
	}

	stepIndex := buildStepIndex(p)
	fixDependsOn(p, g, stepIndex)

	// addOffer should depend on searchFlights and createWorkbench
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "searchFlights")
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "createWorkbench")

	// commitBooking should depend on addOffer, addTraveler, createWorkbench
	assert.Contains(t, p.Execution.Steps[4].DependsOn, "addOffer")
	assert.Contains(t, p.Execution.Steps[4].DependsOn, "addTraveler")
	assert.Contains(t, p.Execution.Steps[4].DependsOn, "createWorkbench")
}

func TestFixDependsOn_RemovesInvalid(t *testing.T) {
	g := loadTravelportGraph(t)

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchFlights"},
				{Node: "createWorkbench", DependsOn: []string{"nonExistentNode"}},
			},
		},
	}

	stepIndex := buildStepIndex(p)
	fixDependsOn(p, g, stepIndex)

	// Invalid dep should be removed
	assert.NotContains(t, p.Execution.Steps[1].DependsOn, "nonExistentNode")
}

func TestFixDependsOn_PreservesValid(t *testing.T) {
	g := loadTravelportGraph(t)

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchFlights"},
				{Node: "createWorkbench"},
				{Node: "addOffer", DependsOn: []string{"searchFlights", "createWorkbench"}},
			},
		},
	}

	stepIndex := buildStepIndex(p)
	fixDependsOn(p, g, stepIndex)

	assert.Contains(t, p.Execution.Steps[2].DependsOn, "searchFlights")
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "createWorkbench")
}

func TestFixDependsOn_SortsDeterministically(t *testing.T) {
	g := loadTravelportGraph(t)

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchFlights"},
				{Node: "createWorkbench"},
				{Node: "addOffer"},
				{Node: "addTraveler"},
				{Node: "commitBooking"},
			},
		},
	}

	stepIndex := buildStepIndex(p)
	fixDependsOn(p, g, stepIndex)

	// commitBooking deps should be sorted
	deps := p.Execution.Steps[4].DependsOn
	for i := 1; i < len(deps); i++ {
		assert.True(t, deps[i-1] <= deps[i], "deps not sorted: %v", deps)
	}
}

func TestAddCleanupSteps_AddsForNodeWithCleanup(t *testing.T) {
	g := loadTravelportGraph(t)

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "createWorkbench"},
			},
		},
	}

	stepIndex := buildStepIndex(p)
	addCleanupSteps(p, g, stepIndex)

	require.Len(t, p.Execution.Cleanup, 1)
	assert.Equal(t, "ignoreWorkbench", p.Execution.Cleanup[0].Node)
	assert.Equal(t, "always", p.Execution.Cleanup[0].RunOn)
}

func TestAddCleanupSteps_DoesNotDuplicate(t *testing.T) {
	g := loadTravelportGraph(t)

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "createWorkbench"},
			},
			Cleanup: []plan.CleanupStep{
				{Node: "ignoreWorkbench", RunOn: "always"},
			},
		},
	}

	stepIndex := buildStepIndex(p)
	addCleanupSteps(p, g, stepIndex)

	assert.Len(t, p.Execution.Cleanup, 1) // no duplicate
}

func TestFixSelectionConfigs_AddsDefault(t *testing.T) {
	g := loadTravelportGraph(t)

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchFlights"},
				{
					Node: "addOffer",
					// No values for select-edge inputs
				},
			},
		},
	}

	stepIndex := buildStepIndex(p)
	fixSelectionConfigs(p, g, stepIndex)

	// addOffer.offeringId is fed by a select edge
	sv, ok := p.Execution.Steps[1].Values["offeringId"]
	require.True(t, ok, "offeringId should have been added")
	assert.Equal(t, "searchFlights.catalogOfferings", sv.From)
	require.NotNil(t, sv.Select)
	assert.Equal(t, "first", sv.Select.Strategy)
	assert.Equal(t, "id", sv.Select.Field) // uses Path from elementField

	// addOffer.productRef is also fed by a select edge
	svProd, ok := p.Execution.Steps[1].Values["productRef"]
	require.True(t, ok, "productRef should have been added")
	assert.Equal(t, "searchFlights.catalogOfferings", svProd.From)
	require.NotNil(t, svProd.Select)
	assert.Equal(t, "ProductBrandOptions.0.ProductBrandOffering.0.Product.0.productRef", svProd.Select.Field)
}

func TestFixSelectionConfigs_PreservesExisting(t *testing.T) {
	g := loadTravelportGraph(t)

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchFlights"},
				{
					Node: "addOffer",
					Values: map[string]plan.StepValue{
						"offeringId": {
							From: "searchFlights.catalogOfferings",
							Select: &plan.SelectionConfig{
								Strategy: "match",
								Filter:   "stops == 0",
								Field:    "offeringId",
							},
						},
					},
				},
			},
		},
	}

	stepIndex := buildStepIndex(p)
	fixSelectionConfigs(p, g, stepIndex)

	sv := p.Execution.Steps[1].Values["offeringId"]
	assert.Equal(t, "match", sv.Select.Strategy)
	assert.Equal(t, "stops == 0", sv.Select.Filter)
}

func TestSetMetadata(t *testing.T) {
	g := &graph.Graph{Version: "1.0.0"}
	ga := &GoalAnalysis{Goal: "commitBooking"}

	p := &plan.Plan{}
	setMetadata(p, g, ga, "book a flight")

	assert.Equal(t, "book a flight", p.Metadata.Prompt)
	assert.Equal(t, "1.0.0", p.Metadata.GraphVersion)
	assert.False(t, p.Metadata.Created.IsZero())
}

func TestPopulateIntent(t *testing.T) {
	ga := &GoalAnalysis{
		Goal:        "commitBooking",
		Description: "Book a flight from DEN to SFO",
		Constraints: ConstraintSet{
			Hard: []ConstraintInfo{{Name: "origin", Description: "Must be DEN"}},
			Soft: []ConstraintInfo{{Name: "nonstop", Description: "Prefer direct flights"}},
			Free: []string{"departure date"},
		},
	}

	p := &plan.Plan{}
	populateIntent(p, ga)

	assert.Equal(t, "commitBooking", p.Intent.Goal)
	assert.Equal(t, "Book a flight from DEN to SFO", p.Intent.Description)
	require.NotNil(t, p.Intent.Constraints)
	assert.Len(t, p.Intent.Constraints.Hard, 1)
	assert.Len(t, p.Intent.Constraints.Soft, 1)
	assert.Contains(t, p.Intent.Constraints.Free, "departure date")
}

func TestPopulateIntent_SkipsWhenAlreadyPopulated(t *testing.T) {
	ga := &GoalAnalysis{
		Goal:        "commitBooking",
		Description: "Book a flight from DEN to SFO",
		Constraints: ConstraintSet{
			Hard: []ConstraintInfo{{Name: "origin", Description: "Must be DEN"}},
			Soft: []ConstraintInfo{{Name: "nonstop", Description: "Prefer direct flights"}},
			Free: []string{"departure date"},
		},
	}

	// Plan already has constraints from the LLM YAML
	p := &plan.Plan{
		Intent: plan.Intent{
			Constraints: &plan.Constraints{
				Hard: []plan.Constraint{{Name: "origin", Description: "Must depart from DEN"}},
				Soft: []plan.Constraint{{Name: "direct", Description: "Prefer nonstop"}},
				Free: []string{"date"},
			},
		},
	}

	populateIntent(p, ga)

	// Should keep existing constraints, not duplicate
	assert.Len(t, p.Intent.Constraints.Hard, 1)
	assert.Len(t, p.Intent.Constraints.Soft, 1)
	assert.Len(t, p.Intent.Constraints.Free, 1)
	// Verify it's the original LLM-generated constraint, not the GoalAnalysis one
	assert.Equal(t, "Must depart from DEN", p.Intent.Constraints.Hard[0].Description)
}

func TestPopulateIntent_NilGoalAnalysis(t *testing.T) {
	p := &plan.Plan{}
	populateIntent(p, nil)
	assert.Empty(t, p.Intent.Goal)
}

func TestPostProcess_FullPipeline(t *testing.T) {
	g := loadTravelportGraph(t)

	cr := &graph.ChainResult{
		Nodes:      []string{"searchFlights", "createWorkbench", "addOffer", "addTraveler", "commitBooking"},
		EntryNodes: []string{"searchFlights", "createWorkbench"},
	}
	ga := &GoalAnalysis{
		Goal:        "commitBooking",
		Description: "Book a flight",
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchFlights", Values: map[string]plan.StepValue{"origin": {Default: "DEN"}, "destination": {Default: "SFO"}, "departureDate": {Default: "2025-06-15"}}},
				{Node: "createWorkbench"},
				{Node: "addOffer"},
				{Node: "addTraveler", Values: map[string]plan.StepValue{"surname": {Default: "Smith"}, "givenName": {Default: "John"}}},
				{Node: "commitBooking", IsGoal: true},
			},
		},
	}

	PostProcess(p, g, cr, ga, "book a flight from DEN to SFO")

	// dependsOn should be fixed
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "searchFlights")
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "createWorkbench")

	// cleanup should be added
	assert.Len(t, p.Execution.Cleanup, 1)
	assert.Equal(t, "ignoreWorkbench", p.Execution.Cleanup[0].Node)

	// metadata should be set
	assert.Equal(t, "book a flight from DEN to SFO", p.Metadata.Prompt)
	assert.Equal(t, "1.0.0", p.Metadata.GraphVersion)

	// intent should be populated
	assert.Equal(t, "commitBooking", p.Intent.Goal)
}

// --- BuildSkeleton Tests ---

func travelportChainResult() *graph.ChainResult {
	return &graph.ChainResult{
		Nodes:      []string{"searchFlights", "createWorkbench", "addOffer", "addTraveler", "commitBooking"},
		EntryNodes: []string{"searchFlights", "createWorkbench"},
		Edges: []graph.Edge{
			{From: "searchFlights.catalogOfferingsId", To: "addOffer.catalogOfferingsId"},
			{From: "searchFlights.catalogOfferings", To: "addOffer.offeringId", Select: true},
			{From: "searchFlights.catalogOfferings", To: "addOffer.productRef", Select: true},
			{From: "createWorkbench.workbenchId", To: "addOffer.workbenchId"},
			{From: "createWorkbench.workbenchId", To: "addTraveler.workbenchId"},
			{From: "createWorkbench.workbenchId", To: "commitBooking.workbenchId"},
			{From: "addOffer.offerStatus", To: "commitBooking.offerStatus"},
			{From: "addTraveler.travelerId", To: "commitBooking.travelerId"},
		},
	}
}

func TestBuildSkeleton_TravelportBooking(t *testing.T) {
	g := loadTravelportGraph(t)
	cr := travelportChainResult()
	ga := &GoalAnalysis{
		Goal:        "commitBooking",
		Description: "Book a flight from DEN to SFO",
		Constraints: ConstraintSet{
			Hard: []ConstraintInfo{{Name: "origin", Description: "Must be DEN", AppliesTo: []string{"searchFlights.origin"}}},
			Free: []string{"departureDate"},
		},
	}
	now := time.Date(2026, 2, 8, 12, 0, 0, 0, time.UTC)

	skel := BuildSkeleton(g, cr, ga, "book a flight from DEN to SFO", now)

	// Should have 5 steps in chain order.
	require.Len(t, skel.Execution.Steps, 5)
	assert.Equal(t, "searchFlights", skel.Execution.Steps[0].Node)
	assert.Equal(t, "createWorkbench", skel.Execution.Steps[1].Node)
	assert.Equal(t, "addOffer", skel.Execution.Steps[2].Node)
	assert.Equal(t, "addTraveler", skel.Execution.Steps[3].Node)
	assert.Equal(t, "commitBooking", skel.Execution.Steps[4].Node)

	// IsGoal should only be set on commitBooking.
	assert.False(t, skel.Execution.Steps[0].IsGoal)
	assert.True(t, skel.Execution.Steps[4].IsGoal)

	// DependsOn for addOffer: createWorkbench, searchFlights.
	assert.Equal(t, []string{"createWorkbench", "searchFlights"}, skel.Execution.Steps[2].DependsOn)

	// DependsOn for addTraveler: createWorkbench.
	assert.Equal(t, []string{"createWorkbench"}, skel.Execution.Steps[3].DependsOn)

	// DependsOn for commitBooking: addOffer, addTraveler, createWorkbench.
	assert.Equal(t, []string{"addOffer", "addTraveler", "createWorkbench"}, skel.Execution.Steps[4].DependsOn)

	// searchFlights: no dependsOn, no from refs.
	assert.Empty(t, skel.Execution.Steps[0].DependsOn)

	// addOffer: offeringId and productRef should have select configs with paths.
	addOfferVals := skel.Execution.Steps[2].Values
	require.NotNil(t, addOfferVals["offeringId"].Select)
	assert.Equal(t, "first", addOfferVals["offeringId"].Select.Strategy)
	assert.Equal(t, "id", addOfferVals["offeringId"].Select.Field) // uses Path from elementField
	assert.Equal(t, "searchFlights.catalogOfferings", addOfferVals["offeringId"].From)
	require.NotNil(t, addOfferVals["productRef"].Select)
	assert.Equal(t, "ProductBrandOptions.0.ProductBrandOffering.0.Product.0.productRef", addOfferVals["productRef"].Select.Field)
	assert.Equal(t, "searchFlights.catalogOfferings", addOfferVals["productRef"].From)

	// addOffer: workbenchId should be a scalar from ref (no select).
	assert.Equal(t, "createWorkbench.workbenchId", addOfferVals["workbenchId"].From)
	assert.Nil(t, addOfferVals["workbenchId"].Select)

	// addOffer: catalogOfferingsId should be a scalar from ref.
	assert.Equal(t, "searchFlights.catalogOfferingsId", addOfferVals["catalogOfferingsId"].From)
	assert.Nil(t, addOfferVals["catalogOfferingsId"].Select)

	// searchFlights: graph defaults should be present.
	sfVals := skel.Execution.Steps[0].Values
	assert.Equal(t, 1, sfVals["passengers"].Default)
	assert.Equal(t, "economy", sfVals["cabinPreference"].Default)
	// origin, destination, departureDate: no edge, no default → absent (unfed).
	_, hasOrigin := sfVals["origin"]
	assert.False(t, hasOrigin, "origin should be absent — unfed input for LLM")
	_, hasDest := sfVals["destination"]
	assert.False(t, hasDest, "destination should be absent — unfed input for LLM")
	_, hasDeptDate := sfVals["departureDate"]
	assert.False(t, hasDeptDate, "departureDate should be absent — unfed input for LLM")

	// Cleanup should have ignoreWorkbench.
	require.Len(t, skel.Execution.Cleanup, 1)
	assert.Equal(t, "ignoreWorkbench", skel.Execution.Cleanup[0].Node)
	assert.Equal(t, "always", skel.Execution.Cleanup[0].RunOn)

	// Metadata.
	assert.Equal(t, "book a flight from DEN to SFO", skel.Metadata.Prompt)
	assert.Equal(t, "1.0.0", skel.Metadata.GraphVersion)
	assert.Equal(t, now, skel.Metadata.Created)

	// Intent.
	assert.Equal(t, "commitBooking", skel.Intent.Goal)
	assert.Equal(t, "Book a flight from DEN to SFO", skel.Intent.Description)
	require.NotNil(t, skel.Intent.Constraints)
	assert.Len(t, skel.Intent.Constraints.Hard, 1)
}

func TestBuildSkeleton_SingleNode(t *testing.T) {
	g := loadTravelportGraph(t)
	cr := &graph.ChainResult{
		Nodes:      []string{"searchFlights"},
		EntryNodes: []string{"searchFlights"},
		Edges:      []graph.Edge{},
	}
	ga := &GoalAnalysis{
		Goal:        "searchFlights",
		Description: "Search for flights",
	}
	now := time.Date(2026, 2, 8, 12, 0, 0, 0, time.UTC)

	skel := BuildSkeleton(g, cr, ga, "search for flights", now)

	require.Len(t, skel.Execution.Steps, 1)
	assert.Equal(t, "searchFlights", skel.Execution.Steps[0].Node)
	assert.True(t, skel.Execution.Steps[0].IsGoal)
	assert.Empty(t, skel.Execution.Steps[0].DependsOn)

	// No cleanup (searchFlights has no cleanup field).
	assert.Empty(t, skel.Execution.Cleanup)

	// Graph defaults present.
	assert.Equal(t, 1, skel.Execution.Steps[0].Values["passengers"].Default)
}

func TestUnfedInputs_TravelportBooking(t *testing.T) {
	g := loadTravelportGraph(t)
	cr := travelportChainResult()

	unfed := UnfedInputs(g, cr)

	// Required unfed: searchFlights.origin, searchFlights.destination, searchFlights.departureDate
	// addTraveler.surname, addTraveler.givenName, addTraveler.birthDate, addTraveler.gender
	// (optional inputs like returnDate are excluded)
	// (inputs with defaults like passengers, cabinPreference, passengerTypeCode are excluded)
	require.Len(t, unfed, 7)
	assert.Contains(t, unfed, "searchFlights.origin (string)")
	assert.Contains(t, unfed, "searchFlights.destination (string)")
	assert.Contains(t, unfed, "searchFlights.departureDate (date)")
	assert.Contains(t, unfed, "addTraveler.surname (string)")
	assert.Contains(t, unfed, "addTraveler.givenName (string)")
	assert.Contains(t, unfed, "addTraveler.birthDate (date)")
	assert.Contains(t, unfed, "addTraveler.gender (enum[Male, Female, Undisclosed])")
}

// --- MergeLLMValues Tests ---

func TestMergeLLMValues_AddsLiterals(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node:   "searchFlights",
					Values: map[string]plan.StepValue{},
				},
			},
		},
	}

	llmPlan := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "searchFlights",
					Values: map[string]plan.StepValue{
						"origin":      {Default: "DEN"},
						"destination": {Default: "SFO"},
					},
				},
			},
		},
	}

	MergeLLMValues(skeleton, llmPlan)

	vals := skeleton.Execution.Steps[0].Values
	assert.Equal(t, "DEN", vals["origin"].Default)
	assert.Equal(t, "SFO", vals["destination"].Default)
}

func TestMergeLLMValues_OverridesStrategy(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "addOffer",
					Values: map[string]plan.StepValue{
						"offeringId": {
							From: "searchFlights.catalogOfferings",
							Select: &plan.SelectionConfig{
								Strategy: "first",
								Field:    "offeringId",
							},
						},
					},
				},
			},
		},
	}

	llmPlan := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "addOffer",
					Values: map[string]plan.StepValue{
						"offeringId": {
							From: "searchFlights.catalogOfferings",
							Select: &plan.SelectionConfig{
								Strategy: "match",
								Field:    "offeringId",
								Filter:   "stops == 0",
							},
						},
					},
				},
			},
		},
	}

	MergeLLMValues(skeleton, llmPlan)

	sel := skeleton.Execution.Steps[0].Values["offeringId"].Select
	assert.Equal(t, "match", sel.Strategy)
	assert.Equal(t, "stops == 0", sel.Filter)
	// Field should still be the skeleton's value.
	assert.Equal(t, "offeringId", sel.Field)
}

func TestMergeLLMValues_IgnoresStructural(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node:      "addOffer",
					DependsOn: []string{"createWorkbench", "searchFlights"},
					IsGoal:    false,
					Values: map[string]plan.StepValue{
						"workbenchId": {From: "createWorkbench.workbenchId"},
					},
				},
			},
		},
	}

	llmPlan := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node:      "addOffer",
					DependsOn: []string{"searchFlights"}, // tries to drop createWorkbench
					IsGoal:    true,                       // tries to set goal
					Values: map[string]plan.StepValue{
						"workbenchId": {
							From: "otherNode.output", // tries to change from ref
							Select: &plan.SelectionConfig{
								Strategy: "match",
								Filter:   "x == 1",
							},
						},
					},
				},
			},
		},
	}

	MergeLLMValues(skeleton, llmPlan)

	step := skeleton.Execution.Steps[0]
	// DependsOn should not change.
	assert.Equal(t, []string{"createWorkbench", "searchFlights"}, step.DependsOn)
	// IsGoal should not change.
	assert.False(t, step.IsGoal)
	// From ref on scalar edge should not change.
	assert.Equal(t, "createWorkbench.workbenchId", step.Values["workbenchId"].From)
	// LLM tried to add select to scalar edge — should be ignored.
	assert.Nil(t, step.Values["workbenchId"].Select)
}

func TestMergeLLMValues_AddsAssertions(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "commitBooking", IsGoal: true},
			},
		},
	}

	llmPlan := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node:        "commitBooking",
					Description: "Finalize the booking",
					Assertions: &plan.Assertions{
						Mechanical: []plan.MechanicalAssertion{
							{Type: "status", Expect: 200},
							{Type: "fieldExists", Path: "$.locator"},
						},
					},
				},
			},
		},
	}

	MergeLLMValues(skeleton, llmPlan)

	step := skeleton.Execution.Steps[0]
	assert.Equal(t, "Finalize the booking", step.Description)
	require.NotNil(t, step.Assertions)
	assert.Len(t, step.Assertions.Mechanical, 2)
}

func TestMergeLLMValues_LLMAddsSelectToScalarEdge(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "commitBooking",
					Values: map[string]plan.StepValue{
						"workbenchId": {From: "createWorkbench.workbenchId"},
					},
				},
			},
		},
	}

	llmPlan := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "commitBooking",
					Values: map[string]plan.StepValue{
						"workbenchId": {
							From: "createWorkbench.workbenchId",
							Select: &plan.SelectionConfig{
								Strategy: "first",
								Field:    "workbenchId",
							},
						},
					},
				},
			},
		},
	}

	MergeLLMValues(skeleton, llmPlan)

	// Select should NOT be added to a scalar from ref.
	assert.Nil(t, skeleton.Execution.Steps[0].Values["workbenchId"].Select)
}

func TestMergeLLMValues_LLMStepNotInSkeleton(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchFlights", Values: map[string]plan.StepValue{}},
			},
		},
	}

	llmPlan := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchFlights", Values: map[string]plan.StepValue{"origin": {Default: "DEN"}}},
				{Node: "nonExistent", Values: map[string]plan.StepValue{"foo": {Default: "bar"}}},
			},
		},
	}

	MergeLLMValues(skeleton, llmPlan)

	// searchFlights should get the value.
	assert.Equal(t, "DEN", skeleton.Execution.Steps[0].Values["origin"].Default)
	// Skeleton should still have only one step.
	assert.Len(t, skeleton.Execution.Steps, 1)
}

func TestMergeLLMValues_OverridesGraphDefault(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "searchFlights",
					Values: map[string]plan.StepValue{
						"passengers": {Default: 1},
					},
				},
			},
		},
	}

	llmPlan := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "searchFlights",
					Values: map[string]plan.StepValue{
						"passengers": {Default: 2},
					},
				},
			},
		},
	}

	MergeLLMValues(skeleton, llmPlan)

	assert.Equal(t, 2, skeleton.Execution.Steps[0].Values["passengers"].Default)
}

// --- fixAssertions tests ---

func TestFixAssertions_RemovesEmptyTypes(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Assertions: &plan.Assertions{
						Mechanical: []plan.MechanicalAssertion{
							{Type: ""},
							{Type: "status", Expect: 200},
							{Type: ""},
						},
					},
				},
			},
		},
	}

	fixAssertions(p)

	assertions := p.Execution.Steps[0].Assertions.Mechanical
	require.Len(t, assertions, 1)
	assert.Equal(t, "status", assertions[0].Type)
	assert.Equal(t, 200, assertions[0].Expect)
}

func TestFixAssertions_RemovesUnknownTypes(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Assertions: &plan.Assertions{
						Mechanical: []plan.MechanicalAssertion{
							{Type: "status", Expect: 201},
							{Type: "bogus", Path: "$.foo"},
						},
					},
				},
			},
		},
	}

	fixAssertions(p)

	assertions := p.Execution.Steps[0].Assertions.Mechanical
	require.Len(t, assertions, 1)
	assert.Equal(t, "status", assertions[0].Type)
	assert.Equal(t, 201, assertions[0].Expect)
}

func TestFixAssertions_AddsDefaultStatus(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node:       "step1",
					Assertions: nil,
				},
			},
		},
	}

	fixAssertions(p)

	assertions := p.Execution.Steps[0].Assertions.Mechanical
	require.Len(t, assertions, 1)
	assert.Equal(t, "status", assertions[0].Type)
	assert.Equal(t, 200, assertions[0].Expect)
}

func TestFixAssertions_AddsStatusWhenAllInvalid(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Assertions: &plan.Assertions{
						Mechanical: []plan.MechanicalAssertion{
							{Type: ""},
							{Type: ""},
						},
					},
				},
			},
		},
	}

	fixAssertions(p)

	assertions := p.Execution.Steps[0].Assertions.Mechanical
	require.Len(t, assertions, 1)
	assert.Equal(t, "status", assertions[0].Type)
	assert.Equal(t, 200, assertions[0].Expect)
}

func TestFixAssertions_PreservesValidAssertions(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Assertions: &plan.Assertions{
						Mechanical: []plan.MechanicalAssertion{
							{Type: "status", Expect: 200},
							{Type: "fieldExists", Path: "$.locator"},
							{Type: "predicate", Expr: "price > 0"},
						},
					},
				},
			},
		},
	}

	fixAssertions(p)

	assertions := p.Execution.Steps[0].Assertions.Mechanical
	require.Len(t, assertions, 3)
	assert.Equal(t, "status", assertions[0].Type)
	assert.Equal(t, "fieldExists", assertions[1].Type)
	assert.Equal(t, "predicate", assertions[2].Type)
}

func TestFixAssertions_StatusPrependedWhenMissing(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Assertions: &plan.Assertions{
						Mechanical: []plan.MechanicalAssertion{
							{Type: "fieldExists", Path: "$.id"},
						},
					},
				},
			},
		},
	}

	fixAssertions(p)

	assertions := p.Execution.Steps[0].Assertions.Mechanical
	require.Len(t, assertions, 2)
	assert.Equal(t, "status", assertions[0].Type)
	assert.Equal(t, 200, assertions[0].Expect)
	assert.Equal(t, "fieldExists", assertions[1].Type)
}

// --- lookupElementFieldPath tests ---

func TestLookupElementFieldPath_WithPath(t *testing.T) {
	g := loadTravelportGraph(t)

	// offeringId has path: "id"
	path := lookupElementFieldPath(g, "searchFlights.catalogOfferings", "offeringId")
	assert.Equal(t, "id", path)
}

func TestLookupElementFieldPath_WithLongPath(t *testing.T) {
	g := loadTravelportGraph(t)

	// productRef has a deeply nested path
	path := lookupElementFieldPath(g, "searchFlights.catalogOfferings", "productRef")
	assert.Equal(t, "ProductBrandOptions.0.ProductBrandOffering.0.Product.0.productRef", path)
}

func TestLookupElementFieldPath_NoPath(t *testing.T) {
	g := loadTravelportGraph(t)

	// departure has no path annotation — falls back to Name
	path := lookupElementFieldPath(g, "searchFlights.catalogOfferings", "departure")
	assert.Equal(t, "departure", path)
}

func TestLookupElementFieldPath_UnknownField(t *testing.T) {
	g := loadTravelportGraph(t)

	// nonexistent field falls back to the input name
	path := lookupElementFieldPath(g, "searchFlights.catalogOfferings", "unknownField")
	assert.Equal(t, "unknownField", path)
}

func TestLookupElementFieldPath_UnknownNode(t *testing.T) {
	g := loadTravelportGraph(t)

	path := lookupElementFieldPath(g, "nonExistent.output", "offeringId")
	assert.Equal(t, "offeringId", path)
}

func TestLookupElementFieldPath_WrongOutput(t *testing.T) {
	g := loadTravelportGraph(t)

	// catalogOfferingsId is a scalar output — no elementFields
	path := lookupElementFieldPath(g, "searchFlights.catalogOfferingsId", "offeringId")
	assert.Equal(t, "offeringId", path)
}
