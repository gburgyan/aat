package intent

import (
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixDependsOn_AddsMissingFromRefs(t *testing.T) {
	g := loadTravelportGraph(t)

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchFlights"},
				{Node: "createWorkbench"},
				{Node: "addOffer", Values: map[string]plan.StepValue{
					"workbenchId":        {From: "createWorkbench.workbenchId"},
					"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
				}},
				{Node: "addTraveler", Values: map[string]plan.StepValue{
					"workbenchId": {From: "createWorkbench.workbenchId"},
				}},
				{Node: "commitBooking", Values: map[string]plan.StepValue{
					"workbenchId":  {From: "createWorkbench.workbenchId"},
					"offerStatus":  {From: "addOffer.offerStatus"},
					"travelerId":   {From: "addTraveler.travelerId"},
				}},
			},
		},
	}

	stepIndex := buildStepIndex(p)
	fixDependsOn(p, g, stepIndex)

	// addOffer should depend on searchFlights and createWorkbench (from refs)
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "searchFlights")
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "createWorkbench")

	// commitBooking should depend on addOffer, addTraveler, createWorkbench (from refs)
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
				{Node: "addOffer", Values: map[string]plan.StepValue{
					"workbenchId":        {From: "createWorkbench.workbenchId"},
					"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
				}},
				{Node: "addTraveler", Values: map[string]plan.StepValue{
					"workbenchId": {From: "createWorkbench.workbenchId"},
				}},
				{Node: "commitBooking", Values: map[string]plan.StepValue{
					"workbenchId": {From: "createWorkbench.workbenchId"},
					"offerStatus": {From: "addOffer.offerStatus"},
					"travelerId":  {From: "addTraveler.travelerId"},
				}},
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

	addCleanupSteps(p, g)

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

	addCleanupSteps(p, g)

	assert.Len(t, p.Execution.Cleanup, 1) // no duplicate
}

func TestFixSelectionConfigs_DefaultsInlineStrategy(t *testing.T) {
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
								// Empty strategy — should be defaulted to "first"
								Field: "offeringId",
							},
						},
					},
				},
			},
		},
	}

	fixSelectionConfigs(p)

	sv := p.Execution.Steps[1].Values["offeringId"]
	require.NotNil(t, sv.Select)
	assert.Equal(t, "first", sv.Select.Strategy)
	assert.Equal(t, "offeringId", sv.Select.Field)
}

func TestFixSelectionConfigs_PreservesExisting(t *testing.T) {
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

	fixSelectionConfigs(p)

	sv := p.Execution.Steps[1].Values["offeringId"]
	assert.Equal(t, "match", sv.Select.Strategy)
	assert.Equal(t, "stops == 0", sv.Select.Filter)
}

func TestSetMetadata(t *testing.T) {
	g := &graph.Graph{Version: "1.0.0"}

	p := &plan.Plan{}
	setMetadata(p, g, "book a flight")

	assert.Equal(t, "book a flight", p.Metadata.Prompt)
	assert.Equal(t, "1.0.0", p.Metadata.GraphVersion)
	assert.False(t, p.Metadata.Created.IsZero())
}

func TestPopulateIntent(t *testing.T) {
	ws := &WorkflowSelection{
		Workflow:    "Booking Flow",
		Description: "Book a flight from DEN to SFO",
		Constraints: ConstraintSet{
			Hard: []ConstraintInfo{{Name: "origin", Description: "Must be DEN"}},
			Soft: []ConstraintInfo{{Name: "nonstop", Description: "Prefer direct flights"}},
			Free: []string{"departure date"},
		},
	}

	p := &plan.Plan{}
	populateIntent(p, ws)

	assert.Equal(t, "Book a flight from DEN to SFO", p.Intent.Description)
	require.NotNil(t, p.Intent.Constraints)
	assert.Len(t, p.Intent.Constraints.Hard, 1)
	assert.Len(t, p.Intent.Constraints.Soft, 1)
	assert.Contains(t, p.Intent.Constraints.Free, "departure date")
}

func TestPopulateIntent_SkipsWhenAlreadyPopulated(t *testing.T) {
	ws := &WorkflowSelection{
		Workflow:    "Booking Flow",
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

	populateIntent(p, ws)

	// Should keep existing constraints, not duplicate
	assert.Len(t, p.Intent.Constraints.Hard, 1)
	assert.Len(t, p.Intent.Constraints.Soft, 1)
	assert.Len(t, p.Intent.Constraints.Free, 1)
	// Verify it's the original LLM-generated constraint, not the WorkflowSelection one
	assert.Equal(t, "Must depart from DEN", p.Intent.Constraints.Hard[0].Description)
}

func TestPopulateIntent_NilWorkflowSelection(t *testing.T) {
	p := &plan.Plan{}
	populateIntent(p, nil)
	assert.Empty(t, p.Intent.Description)
}

func TestPostProcess_FullPipeline(t *testing.T) {
	g := loadTravelportGraph(t)

	ws := &WorkflowSelection{
		Workflow:    "Full-Payload Booking",
		Description: "Book a flight",
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchFlights", Values: map[string]plan.StepValue{"origin": {Default: "DEN"}, "destination": {Default: "SFO"}, "departureDate": {Default: "2025-06-15"}}},
				{Node: "createWorkbench"},
				{Node: "addOffer", Values: map[string]plan.StepValue{
					"workbenchId":        {From: "createWorkbench.workbenchId"},
					"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
				}},
				{Node: "addTraveler", Values: map[string]plan.StepValue{"surname": {Default: "Smith"}, "givenName": {Default: "John"}, "workbenchId": {From: "createWorkbench.workbenchId"}}},
				{Node: "commitBooking", IsGoal: true, Values: map[string]plan.StepValue{
					"workbenchId": {From: "createWorkbench.workbenchId"},
					"offerStatus": {From: "addOffer.offerStatus"},
					"travelerId":  {From: "addTraveler.travelerId"},
				}},
			},
		},
	}

	PostProcess(p, g, ws, "book a flight from DEN to SFO")

	// dependsOn should be derived from from refs
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "searchFlights")
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "createWorkbench")

	// cleanup should be added
	assert.Len(t, p.Execution.Cleanup, 1)
	assert.Equal(t, "ignoreWorkbench", p.Execution.Cleanup[0].Node)

	// metadata should be set
	assert.Equal(t, "book a flight from DEN to SFO", p.Metadata.Prompt)
	assert.Equal(t, "1.0.0", p.Metadata.GraphVersion)

	// intent should be populated
	assert.Equal(t, "Book a flight", p.Intent.Description)
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

	MergeLLMValues(skeleton, llmPlan, nil)

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

	MergeLLMValues(skeleton, llmPlan, nil)

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

	MergeLLMValues(skeleton, llmPlan, nil)

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

	MergeLLMValues(skeleton, llmPlan, nil)

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

	MergeLLMValues(skeleton, llmPlan, nil)

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

	MergeLLMValues(skeleton, llmPlan, nil)

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

	MergeLLMValues(skeleton, llmPlan, nil)

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

// --- fixAssertions with expectFailure tests ---

func TestFixAssertions_ExpectFailure_UsesExpectedStatus(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					ExpectFailure: &plan.ExpectFailure{
						Status:      []int{401, 403},
						Description: "Should be rejected",
					},
					// No assertions — fixAssertions should add status: 401
				},
			},
		},
	}

	fixAssertions(p)

	assertions := p.Execution.Steps[0].Assertions.Mechanical
	require.Len(t, assertions, 1)
	assert.Equal(t, "status", assertions[0].Type)
	assert.Equal(t, 401, assertions[0].Expect) // first expected failure code, not 200
}

func TestFixAssertions_ExpectFailure_PreservesExistingStatus(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					ExpectFailure: &plan.ExpectFailure{
						Status: []int{403},
					},
					Assertions: &plan.Assertions{
						Mechanical: []plan.MechanicalAssertion{
							{Type: "status", Expect: 403},
							{Type: "fieldExists", Path: "error.code"},
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
	assert.Equal(t, 403, assertions[0].Expect) // preserved, not overwritten
	assert.Equal(t, "fieldExists", assertions[1].Type)
}

func TestFixAssertions_NormalStep_StillGets200(t *testing.T) {
	// Verify that normal steps (no expectFailure) still get status: 200
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					// No expectFailure, no assertions
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

// --- lookupElementFieldPath tests ---

func TestLookupElementFieldPath_WithPath(t *testing.T) {
	g := loadTravelportGraph(t)

	// offeringId has path: "id" but we return the elementField name, not the gjson path
	path := lookupElementFieldPath(g, "searchFlights.catalogOfferings", "offeringId")
	assert.Equal(t, "offeringId", path)
}

func TestLookupElementFieldPath_WithLongPath(t *testing.T) {
	g := loadTravelportGraph(t)

	// productRef has a deeply nested path but we return the elementField name
	path := lookupElementFieldPath(g, "searchFlights.catalogOfferings", "productRef")
	assert.Equal(t, "productRef", path)
}

func TestLookupElementFieldPath_NoPath(t *testing.T) {
	g := loadTravelportGraph(t)

	// departure has no path annotation — returns elementField name
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

// --- Named Selections: deriveSelectionName tests ---

func TestDeriveSelectionName(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{"searchFlights.catalogOfferings", "catalogOffering"},
		{"source.items", "item"},
		{"node.result", "result"}, // no trailing 's'
		{"noField", "selection"},  // no dot → fallback
	}
	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			assert.Equal(t, tt.want, deriveSelectionName(tt.source))
		})
	}
}

// --- Named Selections: fixSelectionConfigs tests ---

func TestFixSelectionConfigs_DefaultsNamedSelectionStrategy(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchFlights"},
				{Node: "createWorkbench"},
				{
					Node:      "addOffer",
					DependsOn: []string{"searchFlights", "createWorkbench"},
					Selections: map[string]plan.StepSelection{
						"offering": {
							From:     "searchFlights.catalogOfferings",
							Strategy: "", // empty → should default to "first"
						},
					},
					Values: map[string]plan.StepValue{
						"offeringId": {FromSelection: "offering.offeringId"},
						"productRef": {FromSelection: "offering.productRef"},
					},
				},
			},
		},
	}

	fixSelectionConfigs(p)

	// Strategy should default to "first"
	assert.Equal(t, "first", p.Execution.Steps[2].Selections["offering"].Strategy)

	// fromSelection values should be untouched (not processed as regular inputs)
	assert.Equal(t, "offering.offeringId", p.Execution.Steps[2].Values["offeringId"].FromSelection)
	assert.Empty(t, p.Execution.Steps[2].Values["offeringId"].From)
	assert.Nil(t, p.Execution.Steps[2].Values["offeringId"].Select)
}

func TestFixSelectionConfigs_SkipsFromSelectionValues(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchFlights"},
				{Node: "createWorkbench"},
				{
					Node:      "addOffer",
					DependsOn: []string{"searchFlights", "createWorkbench"},
					Selections: map[string]plan.StepSelection{
						"offering": {
							From:     "searchFlights.catalogOfferings",
							Strategy: "first",
						},
					},
					Values: map[string]plan.StepValue{
						"offeringId":         {FromSelection: "offering.offeringId"},
						"productRef":         {FromSelection: "offering.productRef"},
						"workbenchId":        {From: "createWorkbench.workbenchId"},
						"catalogOfferingsId": {From: "searchFlights.catalogOfferingsId"},
					},
				},
			},
		},
	}

	fixSelectionConfigs(p)

	// fromSelection values should NOT gain from/select
	assert.Equal(t, "offering.offeringId", p.Execution.Steps[2].Values["offeringId"].FromSelection)
	assert.Empty(t, p.Execution.Steps[2].Values["offeringId"].From)
	assert.Nil(t, p.Execution.Steps[2].Values["offeringId"].Select)

	// scalar from refs should still be fine
	assert.Equal(t, "createWorkbench.workbenchId", p.Execution.Steps[2].Values["workbenchId"].From)
}

// --- Named Selections: MergeLLMValues tests ---

func TestMergeLLMValues_NamedSelectionStrategyOverride(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "addOffer",
					Selections: map[string]plan.StepSelection{
						"offering": {
							From:     "searchFlights.catalogOfferings",
							Strategy: "first",
						},
					},
					Values: map[string]plan.StepValue{
						"offeringId": {FromSelection: "offering.offeringId"},
						"productRef": {FromSelection: "offering.productRef"},
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
					Selections: map[string]plan.StepSelection{
						"offering": {
							From:     "searchFlights.catalogOfferings",
							Strategy: "match",
							Filter:   "stops == 0",
						},
					},
				},
			},
		},
	}

	MergeLLMValues(skeleton, llmPlan, nil)

	sel := skeleton.Execution.Steps[0].Selections["offering"]
	assert.Equal(t, "match", sel.Strategy)
	assert.Equal(t, "stops == 0", sel.Filter)
	assert.Equal(t, "searchFlights.catalogOfferings", sel.From) // preserved

	// fromSelection values should be untouched
	assert.Equal(t, "offering.offeringId", skeleton.Execution.Steps[0].Values["offeringId"].FromSelection)
	assert.Equal(t, "offering.productRef", skeleton.Execution.Steps[0].Values["productRef"].FromSelection)
}

func TestMergeLLMValues_IgnoresUnknownNamedSelection(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "addOffer",
					Selections: map[string]plan.StepSelection{
						"offering": {
							From:     "searchFlights.catalogOfferings",
							Strategy: "first",
						},
					},
					Values: map[string]plan.StepValue{
						"offeringId": {FromSelection: "offering.offeringId"},
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
					Selections: map[string]plan.StepSelection{
						"bogus": {
							From:     "searchFlights.catalogOfferings",
							Strategy: "random",
						},
					},
				},
			},
		},
	}

	MergeLLMValues(skeleton, llmPlan, nil)

	// "bogus" should NOT be added
	_, exists := skeleton.Execution.Steps[0].Selections["bogus"]
	assert.False(t, exists)

	// "offering" should remain unchanged
	assert.Equal(t, "first", skeleton.Execution.Steps[0].Selections["offering"].Strategy)
}

func TestFixDependsOn_IncludesRequiresSatisfies(t *testing.T) {
	// Graph where node B requires token from A but no data-flow edge A→B.
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"a": {Name: "a", Adapter: "a", Satisfies: []string{"tokenA"}, Outputs: []graph.Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Requires: []string{"tokenA"}, Inputs: []graph.Input{{Name: "x", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "a"},
				{Node: "b"},
			},
		},
	}

	stepIndex := buildStepIndex(p)
	fixDependsOn(p, g, stepIndex)

	// b should depend on a via requires/satisfies.
	assert.Contains(t, p.Execution.Steps[1].DependsOn, "a")
}

func TestFixDependsOn_ResolvesNodeNameToStepID(t *testing.T) {
	// When LLM adds bare node names (e.g., "addSeatOffer") to dependsOn
	// but the actual step ID is prefixed (e.g., "inc0_addSeatOffer"),
	// fixDependsOn should resolve to the correct step ID.
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"createWorkbench": {Name: "createWorkbench", Adapter: "cw", Outputs: []graph.Output{{Name: "workbenchId", Type: "string"}}},
			"addSeatOffer":    {Name: "addSeatOffer", Adapter: "aso", Inputs: []graph.Input{{Name: "workbenchId", Type: "string"}}},
			"addPayment":      {Name: "addPayment", Adapter: "ap", Inputs: []graph.Input{{Name: "workbenchId", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "createWorkbench"},
				{
					ID:        "inc0_addSeatOffer",
					Node:      "addSeatOffer",
					DependsOn: []string{"createWorkbench"},
					Values: map[string]plan.StepValue{
						"workbenchId": {From: "createWorkbench.workbenchId"},
					},
				},
				{
					Node: "addPayment",
					// LLM put bare "addSeatOffer" instead of "inc0_addSeatOffer"
					DependsOn: []string{"createWorkbench", "addSeatOffer"},
					Values: map[string]plan.StepValue{
						"workbenchId": {From: "createWorkbench.workbenchId"},
					},
				},
			},
		},
	}

	stepIndex := buildStepIndex(p)
	fixDependsOn(p, g, stepIndex)

	// addPayment should have "inc0_addSeatOffer" (resolved), not "addSeatOffer" (bare)
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "inc0_addSeatOffer")
	assert.NotContains(t, p.Execution.Steps[2].DependsOn, "addSeatOffer")
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "createWorkbench")
}

func TestResolveConstraintRefs_PrefixedStepIDs(t *testing.T) {
	// When LLM generates constraints with bare node names (e.g., "searchSeatMap")
	// but the plan has prefixed step IDs (e.g., "inc0_searchSeatMap"),
	// resolveConstraintRefs should rewrite the AppliesTo references.
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchFlights"},
				{Node: "createWorkbench"},
				{ID: "inc0_searchSeatMap", Node: "searchSeatMap"},
				{ID: "inc0_addSeatOffer", Node: "addSeatOffer"},
			},
		},
		Intent: plan.Intent{
			Constraints: &plan.Constraints{
				Hard: []plan.Constraint{
					{
						Name:      "origin",
						AppliesTo: []string{"searchFlights.origin"},
					},
				},
				Soft: []plan.Constraint{
					{
						Name:      "seat selection",
						AppliesTo: []string{"searchSeatMap.workbenchId", "addSeatOffer.seatId"},
					},
					{
						Name:      "bare node ref",
						AppliesTo: []string{"searchSeatMap"},
					},
				},
			},
		},
	}

	resolveConstraintRefs(p)

	// Hard constraint with non-prefixed step should be unchanged.
	assert.Equal(t, []string{"searchFlights.origin"}, p.Intent.Constraints.Hard[0].AppliesTo)

	// Soft constraints with bare node names should be resolved to prefixed step IDs.
	assert.Equal(t, []string{"inc0_searchSeatMap.workbenchId", "inc0_addSeatOffer.seatId"}, p.Intent.Constraints.Soft[0].AppliesTo)
	assert.Equal(t, []string{"inc0_searchSeatMap"}, p.Intent.Constraints.Soft[1].AppliesTo)
}

func TestResolveConstraintRefs_NoPrefixes(t *testing.T) {
	// When no steps have prefixed IDs, resolveConstraintRefs should be a no-op.
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchFlights"},
				{Node: "createWorkbench"},
			},
		},
		Intent: plan.Intent{
			Constraints: &plan.Constraints{
				Soft: []plan.Constraint{
					{
						Name:      "origin",
						AppliesTo: []string{"searchFlights.origin"},
					},
				},
			},
		},
	}

	resolveConstraintRefs(p)

	assert.Equal(t, []string{"searchFlights.origin"}, p.Intent.Constraints.Soft[0].AppliesTo)
}

func TestMergeLLMValues_SkipsFromSelectionValues(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "addOffer",
					Selections: map[string]plan.StepSelection{
						"offering": {
							From:     "searchFlights.catalogOfferings",
							Strategy: "first",
						},
					},
					Values: map[string]plan.StepValue{
						"offeringId": {FromSelection: "offering.offeringId"},
						"productRef": {FromSelection: "offering.productRef"},
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
						// LLM tries to override fromSelection values
						"offeringId": {Default: "some-id"},
						"productRef": {
							From: "otherNode.output",
							Select: &plan.SelectionConfig{
								Strategy: "random",
							},
						},
					},
				},
			},
		},
	}

	MergeLLMValues(skeleton, llmPlan, nil)

	// fromSelection refs should be preserved, LLM changes ignored
	assert.Equal(t, "offering.offeringId", skeleton.Execution.Steps[0].Values["offeringId"].FromSelection)
	assert.Nil(t, skeleton.Execution.Steps[0].Values["offeringId"].Select)
	assert.Equal(t, "offering.productRef", skeleton.Execution.Steps[0].Values["productRef"].FromSelection)
}
