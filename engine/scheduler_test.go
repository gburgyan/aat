package engine

import (
	"testing"

	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTopologicalSort_AirlineBooking(t *testing.T) {
	steps := []plan.Step{
		{Node: "searchFlights"},
		{Node: "priceOffer", DependsOn: []string{"searchFlights"}},
		{Node: "createItinerary"},
		{Node: "addOffer", DependsOn: []string{"priceOffer", "createItinerary"}},
		{Node: "addTraveler", DependsOn: []string{"createItinerary"}},
		{Node: "commitBooking", DependsOn: []string{"addOffer", "addTraveler"}},
	}

	sorted, err := TopologicalSort(steps)
	require.NoError(t, err)
	require.Len(t, sorted, 6)

	// Build position map for ordering assertions
	pos := make(map[string]int)
	for i, step := range sorted {
		pos[step.Node] = i
	}

	// searchFlights must come before priceOffer
	assert.Less(t, pos["searchFlights"], pos["priceOffer"])
	// priceOffer must come before addOffer
	assert.Less(t, pos["priceOffer"], pos["addOffer"])
	// createItinerary must come before addOffer and addTraveler
	assert.Less(t, pos["createItinerary"], pos["addOffer"])
	assert.Less(t, pos["createItinerary"], pos["addTraveler"])
	// addOffer and addTraveler must come before commitBooking
	assert.Less(t, pos["addOffer"], pos["commitBooking"])
	assert.Less(t, pos["addTraveler"], pos["commitBooking"])
}

func TestTopologicalSort_SingleStep(t *testing.T) {
	steps := []plan.Step{{Node: "searchFlights"}}

	sorted, err := TopologicalSort(steps)
	require.NoError(t, err)
	require.Len(t, sorted, 1)
	assert.Equal(t, "searchFlights", sorted[0].Node)
}

func TestTopologicalSort_IndependentSteps(t *testing.T) {
	// searchFlights and createItinerary have no dependencies between them
	steps := []plan.Step{
		{Node: "searchFlights"},
		{Node: "createItinerary"},
	}

	sorted, err := TopologicalSort(steps)
	require.NoError(t, err)
	require.Len(t, sorted, 2)
	// Both should be present (order between them is valid either way)
	nodes := []string{sorted[0].Node, sorted[1].Node}
	assert.Contains(t, nodes, "searchFlights")
	assert.Contains(t, nodes, "createItinerary")
}

func TestTopologicalSort_CycleDetection(t *testing.T) {
	steps := []plan.Step{
		{Node: "a", DependsOn: []string{"b"}},
		{Node: "b", DependsOn: []string{"a"}},
	}

	_, err := TopologicalSort(steps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestTopologicalSort_ExplicitDependsOn(t *testing.T) {
	steps := []plan.Step{
		{Node: "c", DependsOn: []string{"b"}},
		{Node: "b", DependsOn: []string{"a"}},
		{Node: "a"},
	}

	sorted, err := TopologicalSort(steps)
	require.NoError(t, err)
	require.Len(t, sorted, 3)
	assert.Equal(t, "a", sorted[0].Node)
	assert.Equal(t, "b", sorted[1].Node)
	assert.Equal(t, "c", sorted[2].Node)
}

func TestTopologicalSort_StepAliasing(t *testing.T) {
	t.Run("aliased steps with explicit dependsOn", func(t *testing.T) {
		steps := []plan.Step{
			{ID: "search_leg1", Node: "search"},
			{ID: "search_leg2", Node: "search", DependsOn: []string{"search_leg1"}},
			{ID: "search_leg3", Node: "search", DependsOn: []string{"search_leg2"}},
		}

		sorted, err := TopologicalSort(steps)
		require.NoError(t, err)
		require.Len(t, sorted, 3)
		assert.Equal(t, "search_leg1", sorted[0].StepID())
		assert.Equal(t, "search_leg2", sorted[1].StepID())
		assert.Equal(t, "search_leg3", sorted[2].StepID())
	})

	t.Run("mixed aliased and non-aliased steps", func(t *testing.T) {
		steps := []plan.Step{
			{ID: "search_leg1", Node: "searchFlights", Values: map[string]plan.StepValue{
				"origin": {Default: "MEL"}, "destination": {Default: "SYD"}, "departureDate": {Default: "2026-03-01"},
			}},
			{ID: "search_leg2", Node: "searchFlights", DependsOn: []string{"search_leg1"}, Values: map[string]plan.StepValue{
				"origin": {Default: "SYD"}, "destination": {Default: "BNE"}, "departureDate": {Default: "2026-03-05"},
			}},
			{Node: "createItinerary"},
		}

		sorted, err := TopologicalSort(steps)
		require.NoError(t, err)
		require.Len(t, sorted, 3)

		pos := make(map[string]int)
		for i, s := range sorted {
			pos[s.StepID()] = i
		}
		assert.Less(t, pos["search_leg1"], pos["search_leg2"])
	})
}

// TestTopologicalSort_FuzzCasesBeforeLaterSteps checks that a target's fuzz
// cases, chained one after another, all run before the steps that follow the
// target in the plan: under the shared scope a later step that ran between
// them would change the state the remaining cases run on.
func TestTopologicalSort_FuzzCasesBeforeLaterSteps(t *testing.T) {
	p := &plan.Plan{Execution: plan.Execution{Steps: []plan.Step{
		{ID: "cart", Node: "createCart"},
		{ID: "add", Node: "addItem", DependsOn: []string{"cart"}},
		{ID: "checkout", Node: "checkout", DependsOn: []string{"add"}},
	}}}
	var cases []plan.FuzzCase
	for _, id := range []string{"q.a", "q.b", "q.c"} {
		cases = append(cases, plan.FuzzCase{ID: id, Mode: plan.FuzzEdge, Input: "quantity", Value: 1})
	}
	for _, scope := range plan.FuzzScopes {
		t.Run(scope, func(t *testing.T) {
			cp := &plan.Plan{Execution: plan.Execution{Steps: append([]plan.Step(nil), p.Execution.Steps...)}}
			require.NoError(t, plan.ExpandFuzzCases(cp, "add", cases, plan.FuzzExpandOptions{Scope: scope}))
			sorted, err := TopologicalSort(cp.Execution.Steps)
			require.NoError(t, err)
			var ids []string
			for _, s := range sorted {
				ids = append(ids, s.StepID())
			}
			assert.Equal(t, "checkout", ids[len(ids)-1], "the cases run before checkout: %v", ids)
		})
	}
}

func TestTopologicalSort_KeepsPlanOrder(t *testing.T) {
	steps := []plan.Step{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c"},
		{ID: "d", DependsOn: []string{"c"}},
	}
	sorted, err := TopologicalSort(steps)
	require.NoError(t, err)
	var ids []string
	for _, s := range sorted {
		ids = append(ids, s.StepID())
	}
	assert.Equal(t, []string{"a", "b", "c", "d"}, ids)
}
