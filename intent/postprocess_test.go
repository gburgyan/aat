package intent

import (
	"testing"

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
