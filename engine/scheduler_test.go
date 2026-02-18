package engine

import (
	"testing"

	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTopologicalSort_TravelportBooking(t *testing.T) {
	steps := []plan.Step{
		{Node: "searchFlights"},
		{Node: "priceOffer", DependsOn: []string{"searchFlights"}},
		{Node: "createWorkbench"},
		{Node: "addOffer", DependsOn: []string{"priceOffer", "createWorkbench"}},
		{Node: "addTraveler", DependsOn: []string{"createWorkbench"}},
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
	// createWorkbench must come before addOffer and addTraveler
	assert.Less(t, pos["createWorkbench"], pos["addOffer"])
	assert.Less(t, pos["createWorkbench"], pos["addTraveler"])
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
	// searchFlights and createWorkbench have no dependencies between them
	steps := []plan.Step{
		{Node: "searchFlights"},
		{Node: "createWorkbench"},
	}

	sorted, err := TopologicalSort(steps)
	require.NoError(t, err)
	require.Len(t, sorted, 2)
	// Both should be present (order between them is valid either way)
	nodes := []string{sorted[0].Node, sorted[1].Node}
	assert.Contains(t, nodes, "searchFlights")
	assert.Contains(t, nodes, "createWorkbench")
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
			{Node: "createWorkbench"},
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
