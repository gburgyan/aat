package graph

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helper to build simple graphs for testing ---

func buildLinearGraph(names ...string) *Graph {
	g := &Graph{
		Version: "1.0.0",
		Nodes:   map[string]*Node{},
	}
	for i, name := range names {
		node := &Node{
			Name:    name,
			Adapter: name,
			Inputs:  []Input{{Name: "input1", Type: "string"}},
			Outputs: []Output{{Name: "output1", Type: "string"}},
		}
		if i > 0 {
			node.Requires = []string{names[i-1] + "_done"}
		}
		if i < len(names)-1 {
			node.Satisfies = []string{name + "_done"}
		}
		g.Nodes[name] = node
	}
	g.BuildSatisfierIndex()
	return g
}

// --- Basic Chaining Tests ---

func TestBackwardChain_SingleNodeNoDeps(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"standalone": {
				Name: "standalone", Adapter: "standalone",
				Inputs:  []Input{{Name: "x", Type: "string"}},
				Outputs: []Output{{Name: "y", Type: "string"}},
			},
		},
	}

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"standalone"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"standalone"}, result.Nodes)
	assert.Equal(t, []string{"standalone"}, result.EntryNodes)
}

func TestBackwardChain_LinearChain(t *testing.T) {
	g := buildLinearGraph("a", "b", "c")

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"c"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, result.Nodes)
	assert.Len(t, result.RequiresEdges, 2)
	assert.Equal(t, []string{"a"}, result.EntryNodes)
}

func TestBackwardChain_MidChainGoal(t *testing.T) {
	g := buildLinearGraph("a", "b", "c")

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"b"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, result.Nodes)
	assert.Len(t, result.RequiresEdges, 1)
	assert.Equal(t, []string{"a"}, result.EntryNodes)
	// c should not be included
	assert.NotContains(t, result.Nodes, "c")
}

func TestBackwardChain_FanIn(t *testing.T) {
	// D depends on both B and C; B and C depend on A
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a", Satisfies: []string{"aReady"}, Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Requires: []string{"aReady"}, Satisfies: []string{"bReady"}, Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"c": {Name: "c", Adapter: "c", Requires: []string{"aReady"}, Satisfies: []string{"cReady"}, Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"d": {Name: "d", Adapter: "d", Requires: []string{"bReady", "cReady"}, Inputs: []Input{{Name: "x1", Type: "string"}, {Name: "x2", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"d"}})
	require.NoError(t, err)
	assert.Len(t, result.Nodes, 4)
	assert.Equal(t, "a", result.Nodes[0])
	assert.Equal(t, "d", result.Nodes[3])
	assert.Equal(t, []string{"a"}, result.EntryNodes)
}

func TestBackwardChain_MultipleGoals(t *testing.T) {
	g := buildLinearGraph("a", "b", "c")
	// Add an independent node
	g.Nodes["d"] = &Node{Name: "d", Adapter: "d", Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}}

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"b", "d"}})
	require.NoError(t, err)
	assert.Contains(t, result.Nodes, "a")
	assert.Contains(t, result.Nodes, "b")
	assert.Contains(t, result.Nodes, "d")
	assert.NotContains(t, result.Nodes, "c")
}

func TestBackwardChain_MultipleGoalsSharedPrereqs(t *testing.T) {
	// b and c both depend on a; requesting both as goals
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a", Satisfies: []string{"aReady"}, Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Requires: []string{"aReady"}, Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"c": {Name: "c", Adapter: "c", Requires: []string{"aReady"}, Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"b", "c"}})
	require.NoError(t, err)
	// a appears once even though both b and c depend on it
	assert.Equal(t, []string{"a", "b", "c"}, result.Nodes)
}

// --- Travelport Graph Tests ---

func TestBackwardChain_TravelportCommitBooking(t *testing.T) {
	g, err := ParseFile("testdata/valid/travelport_booking.yaml")
	require.NoError(t, err)

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"commitBooking"}})
	require.NoError(t, err)

	// Should include: searchFlights, createWorkbench, addOffer, addTraveler, commitBooking
	assert.Contains(t, result.Nodes, "searchFlights")
	assert.Contains(t, result.Nodes, "createWorkbench")
	assert.Contains(t, result.Nodes, "addOffer")
	assert.Contains(t, result.Nodes, "addTraveler")
	assert.Contains(t, result.Nodes, "commitBooking")

	// Should NOT include: priceOffer, ignoreWorkbench (not needed for commitBooking)
	assert.NotContains(t, result.Nodes, "priceOffer")
	assert.NotContains(t, result.Nodes, "ignoreWorkbench")

	// commitBooking should be last
	assert.Equal(t, "commitBooking", result.Nodes[len(result.Nodes)-1])

	// searchFlights and createWorkbench should be entry nodes
	assert.Contains(t, result.EntryNodes, "searchFlights")
	assert.Contains(t, result.EntryNodes, "createWorkbench")
}

func TestBackwardChain_TravelportAddOffer(t *testing.T) {
	g, err := ParseFile("testdata/valid/travelport_booking.yaml")
	require.NoError(t, err)

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"addOffer"}})
	require.NoError(t, err)

	assert.Contains(t, result.Nodes, "searchFlights")
	assert.Contains(t, result.Nodes, "createWorkbench")
	assert.Contains(t, result.Nodes, "addOffer")
	assert.NotContains(t, result.Nodes, "addTraveler")
	assert.NotContains(t, result.Nodes, "commitBooking")
}

// --- Cycle Handling Tests ---

func TestBackwardChain_CycleBreakerStopsTraversal(t *testing.T) {
	g, err := ParseFile("testdata/valid/with_cycle_breaker.yaml")
	require.NoError(t, err)

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"getUser"}})
	require.NoError(t, err)

	assert.Contains(t, result.Nodes, "authenticate")
	assert.Contains(t, result.Nodes, "getUser")
	// refreshToken should NOT be included — authenticate is a cycle-breaker
	// and its upstream is not traversed
	assert.NotContains(t, result.Nodes, "refreshToken")

	// Should have a cycle-break decision
	var hasCycleBreak bool
	for _, d := range result.Decisions {
		if d.Type == DecisionCycleBreak && d.Node == "authenticate" {
			hasCycleBreak = true
		}
	}
	assert.True(t, hasCycleBreak, "expected DecisionCycleBreak for authenticate")

	// authenticate should be an entry node
	assert.Contains(t, result.EntryNodes, "authenticate")
}

func TestBackwardChain_CycleBreakerInNonCyclicGraph(t *testing.T) {
	// CycleBreaker on a node that isn't in a cycle — should still stop upstream traversal
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a", CycleBreaker: true, Satisfies: []string{"aDone"}, Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Requires: []string{"aDone"}, Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"b"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, result.Nodes)
	assert.Contains(t, result.EntryNodes, "a")
}

func TestBackwardChain_CycleWithoutBreaker_Error(t *testing.T) {
	// Build a cyclic graph via requires/satisfies (manually, bypassing Validate which would catch it)
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a", Requires: []string{"tokenC"}, Satisfies: []string{"tokenA"}, Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Requires: []string{"tokenA"}, Satisfies: []string{"tokenB"}, Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"c": {Name: "c", Adapter: "c", Requires: []string{"tokenB"}, Satisfies: []string{"tokenC"}, Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	_, err := BackwardChain(g, ChainOptions{Goals: []string{"c"}})
	require.Error(t, err)
	var ce *ChainError
	require.True(t, errors.As(err, &ce))
	assert.Contains(t, ce.Error(), "unresolvable cycle")
}

// --- Condition Tests ---

func TestBackwardChain_ConditionTrue_IncludesRequiredNodes(t *testing.T) {
	g, err := ParseFile("testdata/valid/travel_flow.yaml")
	require.NoError(t, err)

	evalPred := func(expr string, ctx map[string]any) (bool, error) {
		if expr == "route.type == international" {
			return true, nil
		}
		return false, nil
	}

	result, err := BackwardChain(g, ChainOptions{
		Goals:         []string{"selectFare"},
		EvalPredicate: evalPred,
	})
	require.NoError(t, err)

	// addPassportInfo should be included because condition is true
	assert.Contains(t, result.Nodes, "addPassportInfo")
	assert.Contains(t, result.Nodes, "searchFlights")
	assert.Contains(t, result.Nodes, "selectFare")
	assert.Len(t, result.IncludedConditions, 1)
	assert.Equal(t, "route.type == international", result.IncludedConditions[0].Condition.When)
}

func TestBackwardChain_ConditionFalse_ExcludesRequiredNodes(t *testing.T) {
	g, err := ParseFile("testdata/valid/travel_flow.yaml")
	require.NoError(t, err)

	evalPred := func(expr string, ctx map[string]any) (bool, error) {
		return false, nil
	}

	result, err := BackwardChain(g, ChainOptions{
		Goals:         []string{"selectFare"},
		EvalPredicate: evalPred,
	})
	require.NoError(t, err)

	// addPassportInfo should NOT be included because condition is false
	assert.NotContains(t, result.Nodes, "addPassportInfo")
	assert.Len(t, result.ExcludedConditions, 1)
}

func TestBackwardChain_NilEvalPredicate_AllConditionsFalse(t *testing.T) {
	g, err := ParseFile("testdata/valid/travel_flow.yaml")
	require.NoError(t, err)

	result, err := BackwardChain(g, ChainOptions{
		Goals:         []string{"selectFare"},
		EvalPredicate: nil,
	})
	require.NoError(t, err)

	assert.NotContains(t, result.Nodes, "addPassportInfo")
	assert.Empty(t, result.IncludedConditions)
	assert.Len(t, result.ExcludedConditions, 1)
}

func TestBackwardChain_PredicateError_Propagates(t *testing.T) {
	g, err := ParseFile("testdata/valid/travel_flow.yaml")
	require.NoError(t, err)

	evalPred := func(expr string, ctx map[string]any) (bool, error) {
		return false, fmt.Errorf("predicate evaluation failed")
	}

	_, err = BackwardChain(g, ChainOptions{
		Goals:         []string{"selectFare"},
		EvalPredicate: evalPred,
	})
	require.Error(t, err)
	var ce *ChainError
	require.True(t, errors.As(err, &ce))
	assert.Contains(t, ce.Error(), "predicate evaluation failed")
}

func TestBackwardChain_ConditionBeforeOrdering(t *testing.T) {
	g, err := ParseFile("testdata/valid/travel_flow.yaml")
	require.NoError(t, err)

	evalPred := func(expr string, ctx map[string]any) (bool, error) {
		if expr == "route.type == international" {
			return true, nil
		}
		return false, nil
	}

	result, err := BackwardChain(g, ChainOptions{
		Goals:         []string{"selectFare"},
		EvalPredicate: evalPred,
	})
	require.NoError(t, err)

	// The condition says addPassportInfo should be before selectFare
	idxPassport := -1
	idxFare := -1
	for i, n := range result.Nodes {
		if n == "addPassportInfo" {
			idxPassport = i
		}
		if n == "selectFare" {
			idxFare = i
		}
	}
	assert.Greater(t, idxFare, idxPassport, "addPassportInfo should come before selectFare")
}

// --- Entry Node Tests ---

func TestBackwardChain_EntryNodes_MultipleRoots(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"root1": {Name: "root1", Adapter: "root1", Satisfies: []string{"root1Ready"}, Inputs: nil, Outputs: []Output{{Name: "y", Type: "string"}}},
			"root2": {Name: "root2", Adapter: "root2", Satisfies: []string{"root2Ready"}, Inputs: nil, Outputs: []Output{{Name: "y", Type: "string"}}},
			"sink":  {Name: "sink", Adapter: "sink", Requires: []string{"root1Ready", "root2Ready"}, Inputs: []Input{{Name: "x1", Type: "string"}, {Name: "x2", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"sink"}})
	require.NoError(t, err)
	assert.Len(t, result.EntryNodes, 2)
	assert.Contains(t, result.EntryNodes, "root1")
	assert.Contains(t, result.EntryNodes, "root2")
}

// --- Ordering Tests ---

func TestBackwardChain_TopoSortRespectsAllDeps(t *testing.T) {
	g, err := ParseFile("testdata/valid/travelport_booking.yaml")
	require.NoError(t, err)

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"commitBooking"}})
	require.NoError(t, err)

	indexOf := map[string]int{}
	for i, n := range result.Nodes {
		indexOf[n] = i
	}

	// searchFlights before addOffer (data dependency)
	assert.Less(t, indexOf["searchFlights"], indexOf["addOffer"])
	// createWorkbench before addOffer (data dependency)
	assert.Less(t, indexOf["createWorkbench"], indexOf["addOffer"])
	// createWorkbench before addTraveler
	assert.Less(t, indexOf["createWorkbench"], indexOf["addTraveler"])
	// addOffer before commitBooking
	assert.Less(t, indexOf["addOffer"], indexOf["commitBooking"])
	// addTraveler before commitBooking
	assert.Less(t, indexOf["addTraveler"], indexOf["commitBooking"])
}

// --- Error Tests ---

func TestBackwardChain_EmptyGoals(t *testing.T) {
	g := buildLinearGraph("a", "b")

	_, err := BackwardChain(g, ChainOptions{Goals: []string{}})
	require.Error(t, err)
	var ce *ChainError
	require.True(t, errors.As(err, &ce))
	assert.Contains(t, ce.Error(), "at least one goal")
}

func TestBackwardChain_UnknownGoal(t *testing.T) {
	g := buildLinearGraph("a", "b")

	_, err := BackwardChain(g, ChainOptions{Goals: []string{"nonexistent"}})
	require.Error(t, err)
	var ce *ChainError
	require.True(t, errors.As(err, &ce))
	assert.Contains(t, ce.Error(), "unknown goal node")
}

func TestBackwardChain_MultipleUnknownGoals(t *testing.T) {
	g := buildLinearGraph("a")

	_, err := BackwardChain(g, ChainOptions{Goals: []string{"x", "y"}})
	require.Error(t, err)
	var ce *ChainError
	require.True(t, errors.As(err, &ce))
	assert.Len(t, ce.Errors, 2)
}

// --- Deduplication Tests ---

func TestBackwardChain_DiamondGraph_NoDuplicates(t *testing.T) {
	// Diamond: a → b, a → c, b → d, c → d (via requires/satisfies)
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a", Satisfies: []string{"aReady"}, Inputs: nil, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Requires: []string{"aReady"}, Satisfies: []string{"bReady"}, Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"c": {Name: "c", Adapter: "c", Requires: []string{"aReady"}, Satisfies: []string{"cReady"}, Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"d": {Name: "d", Adapter: "d", Requires: []string{"bReady", "cReady"}, Inputs: []Input{{Name: "x1", Type: "string"}, {Name: "x2", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"d"}})
	require.NoError(t, err)

	// Each node appears exactly once
	seen := map[string]bool{}
	for _, n := range result.Nodes {
		assert.False(t, seen[n], "node %q appears more than once", n)
		seen[n] = true
	}
	assert.Len(t, result.Nodes, 4)
}

// --- Fixture Parsing Tests ---

func TestParse_CycleBreakerField(t *testing.T) {
	g, err := ParseFile("testdata/valid/with_cycle_breaker.yaml")
	require.NoError(t, err)

	auth := g.Nodes["authenticate"]
	require.NotNil(t, auth)
	assert.True(t, auth.CycleBreaker)

	// Other nodes should not be cycle breakers
	assert.False(t, g.Nodes["refreshToken"].CycleBreaker)
	assert.False(t, g.Nodes["getUser"].CycleBreaker)
}

// --- Validation Tests ---

func TestValidate_CycleBreakerToleratesCycle(t *testing.T) {
	g, err := ParseFile("testdata/valid/with_cycle_breaker.yaml")
	// Should parse without error despite the auth cycle
	assert.NoError(t, err)
	assert.NotNil(t, g)
}

func TestValidate_NewFixtures(t *testing.T) {
	files := []string{
		"testdata/valid/with_cycle_breaker.yaml",
		"testdata/valid/with_multiple_paths.yaml",
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			_, err := ParseFile(f)
			assert.NoError(t, err)
		})
	}
}

// --- Condition with Before and no Require ---

func TestBackwardChain_ConditionWithOnlyBefore(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a", Satisfies: []string{"aDone"}, Inputs: nil, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Requires: []string{"aDone"}, Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
		Conditions: []Condition{
			{When: "always == true", Before: []string{"b"}},
		},
	}
	g.BuildSatisfierIndex()

	evalPred := func(expr string, ctx map[string]any) (bool, error) {
		return true, nil
	}

	result, err := BackwardChain(g, ChainOptions{
		Goals:         []string{"b"},
		EvalPredicate: evalPred,
	})
	require.NoError(t, err)
	// Condition has no Require, so no extra nodes added
	assert.Equal(t, []string{"a", "b"}, result.Nodes)
	assert.Len(t, result.IncludedConditions, 1)
}

// --- CycleBreaker with upstream chain ---

func TestBackwardChain_CycleBreakerChain(t *testing.T) {
	g, err := ParseFile("testdata/valid/with_cycle_breaker.yaml")
	require.NoError(t, err)

	// Goal: updateUser — should chain: authenticate → getUser → updateUser
	// (authenticate is cycle-breaker, so refreshToken not traversed)
	result, err := BackwardChain(g, ChainOptions{Goals: []string{"updateUser"}})
	require.NoError(t, err)

	assert.Contains(t, result.Nodes, "authenticate")
	assert.Contains(t, result.Nodes, "getUser")
	assert.Contains(t, result.Nodes, "updateUser")
	assert.NotContains(t, result.Nodes, "refreshToken")

	// Ordering: authenticate before getUser before updateUser
	indexOf := map[string]int{}
	for i, n := range result.Nodes {
		indexOf[n] = i
	}
	assert.Less(t, indexOf["authenticate"], indexOf["getUser"])
	assert.Less(t, indexOf["getUser"], indexOf["updateUser"])
}

// --- Edge cases ---

func TestBackwardChain_GoalIsEntryNode(t *testing.T) {
	g := buildLinearGraph("a", "b", "c")

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"a"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, result.Nodes)
	assert.Equal(t, []string{"a"}, result.EntryNodes)
}

func TestBackwardChain_WithConditionContextValues(t *testing.T) {
	g, err := ParseFile("testdata/valid/travel_flow.yaml")
	require.NoError(t, err)

	evalPred := func(expr string, ctx map[string]any) (bool, error) {
		if expr == "route.type == international" {
			routeVal, ok := ctx["route"]
			if !ok {
				return false, nil
			}
			routeMap, ok := routeVal.(map[string]any)
			if !ok {
				return false, nil
			}
			return routeMap["type"] == "international", nil
		}
		return false, nil
	}

	// Test with international route
	result, err := BackwardChain(g, ChainOptions{
		Goals:            []string{"selectFare"},
		EvalPredicate:    evalPred,
		ConditionContext: map[string]any{"route": map[string]any{"type": "international"}},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Nodes, "addPassportInfo")

	// Test with domestic route
	result, err = BackwardChain(g, ChainOptions{
		Goals:            []string{"selectFare"},
		EvalPredicate:    evalPred,
		ConditionContext: map[string]any{"route": map[string]any{"type": "domestic"}},
	})
	require.NoError(t, err)
	assert.NotContains(t, result.Nodes, "addPassportInfo")
}

// --- Requires/Satisfies Tests ---

// buildRequiresGraph creates the Travelport-like graph from the plan design.
// commitReservation requires [offerAdded, travelerAdded, paymentApplied]
// addOffer satisfies [offerAdded], requires [offerPriced, workbenchCreated]
// addTraveler satisfies [travelerAdded], requires [workbenchCreated]
// addPayment satisfies [paymentApplied], requires [formOfPaymentAdded, offerAdded]
// addFormOfPayment satisfies [formOfPaymentAdded], requires [workbenchCreated]
// createWorkbench satisfies [workbenchCreated]
// priceOffer satisfies [offerPriced], requires [flightsSearched]
// searchFlights satisfies [flightsSearched]
func buildRequiresGraph() *Graph {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"searchFlights": {
				Name: "searchFlights", Adapter: "search",
				Satisfies: []string{"flightsSearched"},
				Inputs:    []Input{{Name: "origin", Type: "string"}},
				Outputs:   []Output{{Name: "offerings", Type: "offering[]"}},
			},
			"priceOffer": {
				Name: "priceOffer", Adapter: "price",
				Requires:  []string{"flightsSearched"},
				Satisfies: []string{"offerPriced"},
				Preferred: true,
				Inputs:    []Input{{Name: "offeringId", Type: "string"}},
				Outputs:   []Output{{Name: "pricedOffer", Type: "string"}},
			},
			"createWorkbench": {
				Name: "createWorkbench", Adapter: "createWB",
				Satisfies: []string{"workbenchCreated"},
				Preferred: true,
				Outputs:   []Output{{Name: "workbenchId", Type: "string"}},
			},
			"addOffer": {
				Name: "addOffer", Adapter: "addOffer",
				Requires:  []string{"offerPriced", "workbenchCreated"},
				Satisfies: []string{"offerAdded"},
				Inputs:    []Input{{Name: "workbenchId", Type: "string"}},
				Outputs:   []Output{{Name: "offerStatus", Type: "string"}},
			},
			"addTraveler": {
				Name: "addTraveler", Adapter: "addTraveler",
				Requires:  []string{"workbenchCreated"},
				Satisfies: []string{"travelerAdded"},
				Inputs:    []Input{{Name: "workbenchId", Type: "string"}},
				Outputs:   []Output{{Name: "travelerId", Type: "string"}},
			},
			"addFormOfPayment": {
				Name: "addFormOfPayment", Adapter: "addFOP",
				Requires:  []string{"workbenchCreated"},
				Satisfies: []string{"formOfPaymentAdded"},
				Preferred: true,
				Inputs:    []Input{{Name: "workbenchId", Type: "string"}},
				Outputs:   []Output{{Name: "fopId", Type: "string"}},
			},
			"addPayment": {
				Name: "addPayment", Adapter: "addPayment",
				Requires:  []string{"formOfPaymentAdded", "offerAdded"},
				Satisfies: []string{"paymentApplied"},
				Inputs:    []Input{{Name: "workbenchId", Type: "string"}},
				Outputs:   []Output{{Name: "paymentId", Type: "string"}},
			},
			"commitReservation": {
				Name: "commitReservation", Adapter: "commit",
				Requires: []string{"offerAdded", "travelerAdded", "paymentApplied"},
				Inputs:   []Input{{Name: "workbenchId", Type: "string"}},
				Outputs:  []Output{{Name: "locator", Type: "string"}},
			},
		},
	}
	g.BuildSatisfierIndex()
	return g
}

func TestBackwardChain_RequiresSatisfies_BasicChain(t *testing.T) {
	g := buildRequiresGraph()

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"commitReservation"}})
	require.NoError(t, err)

	// All state-mutation nodes should be included.
	assert.Contains(t, result.Nodes, "searchFlights")
	assert.Contains(t, result.Nodes, "priceOffer")
	assert.Contains(t, result.Nodes, "createWorkbench")
	assert.Contains(t, result.Nodes, "addOffer")
	assert.Contains(t, result.Nodes, "addTraveler")
	assert.Contains(t, result.Nodes, "addFormOfPayment")
	assert.Contains(t, result.Nodes, "addPayment")
	assert.Contains(t, result.Nodes, "commitReservation")

	// commitReservation should be last.
	assert.Equal(t, "commitReservation", result.Nodes[len(result.Nodes)-1])

	// RequiresEdges should be populated.
	assert.NotEmpty(t, result.RequiresEdges)

	// Verify ordering via indexOf.
	indexOf := map[string]int{}
	for i, n := range result.Nodes {
		indexOf[n] = i
	}
	// searchFlights before priceOffer (flightsSearched)
	assert.Less(t, indexOf["searchFlights"], indexOf["priceOffer"])
	// priceOffer before addOffer (offerPriced)
	assert.Less(t, indexOf["priceOffer"], indexOf["addOffer"])
	// createWorkbench before addOffer, addTraveler, addFormOfPayment
	assert.Less(t, indexOf["createWorkbench"], indexOf["addOffer"])
	assert.Less(t, indexOf["createWorkbench"], indexOf["addTraveler"])
	assert.Less(t, indexOf["createWorkbench"], indexOf["addFormOfPayment"])
	// addOffer before addPayment (offerAdded)
	assert.Less(t, indexOf["addOffer"], indexOf["addPayment"])
	// addFormOfPayment before addPayment (formOfPaymentAdded)
	assert.Less(t, indexOf["addFormOfPayment"], indexOf["addPayment"])
	// addOffer, addTraveler, addPayment before commitReservation
	assert.Less(t, indexOf["addOffer"], indexOf["commitReservation"])
	assert.Less(t, indexOf["addTraveler"], indexOf["commitReservation"])
	assert.Less(t, indexOf["addPayment"], indexOf["commitReservation"])
}

func TestBackwardChain_RequiresSatisfies_SingleSatisfier(t *testing.T) {
	// Simple case: A requires [tokenX], B satisfies [tokenX].
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"producer": {Name: "producer", Adapter: "p", Satisfies: []string{"dataReady"},
				Outputs: []Output{{Name: "data", Type: "string"}}},
			"consumer": {Name: "consumer", Adapter: "c", Requires: []string{"dataReady"},
				Inputs: []Input{{Name: "x", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"consumer"}})
	require.NoError(t, err)

	assert.Equal(t, []string{"producer", "consumer"}, result.Nodes)
	require.Len(t, result.RequiresEdges, 1)
	assert.Equal(t, "producer", result.RequiresEdges[0].From)
	assert.Equal(t, "consumer", result.RequiresEdges[0].To)
	assert.Equal(t, "dataReady", result.RequiresEdges[0].Token)
}

func TestBackwardChain_RequiresSatisfies_PreferredWins(t *testing.T) {
	// Two nodes satisfy the same token; preferred one should be chosen.
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"fastPath": {Name: "fastPath", Adapter: "fast", Satisfies: []string{"processed"},
				Preferred: true, Outputs: []Output{{Name: "out", Type: "string"}}},
			"slowPath": {Name: "slowPath", Adapter: "slow", Satisfies: []string{"processed"},
				Outputs: []Output{{Name: "out", Type: "string"}}},
			"consumer": {Name: "consumer", Adapter: "c", Requires: []string{"processed"},
				Inputs: []Input{{Name: "x", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"consumer"}})
	require.NoError(t, err)

	assert.Contains(t, result.Nodes, "fastPath")
	assert.Contains(t, result.Nodes, "consumer")
	assert.NotContains(t, result.Nodes, "slowPath")

	// Should have a satisfier decision.
	var hasSatisfierDecision bool
	for _, d := range result.Decisions {
		if d.Type == DecisionSatisfier {
			hasSatisfierDecision = true
			assert.Equal(t, "fastPath", d.Node)
			assert.Contains(t, d.Alternatives, "slowPath")
		}
	}
	assert.True(t, hasSatisfierDecision)
}

func TestBackwardChain_RequiresSatisfies_NoPreferredPicksAlphabetical(t *testing.T) {
	// Two satisfiers, neither preferred. Should pick alphabetically.
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"beta":     {Name: "beta", Adapter: "b", Satisfies: []string{"processed"}, Outputs: []Output{{Name: "out", Type: "string"}}},
			"alpha":    {Name: "alpha", Adapter: "a", Satisfies: []string{"processed"}, Outputs: []Output{{Name: "out", Type: "string"}}},
			"consumer": {Name: "consumer", Adapter: "c", Requires: []string{"processed"}, Inputs: []Input{{Name: "x", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"consumer"}})
	require.NoError(t, err)

	assert.Contains(t, result.Nodes, "alpha")
	assert.Contains(t, result.Nodes, "consumer")
	assert.NotContains(t, result.Nodes, "beta")
}

func TestBackwardChain_RequiresSatisfies_RecursiveRequires(t *testing.T) {
	// A requires B requires C (transitive chain via tokens).
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"step1":  {Name: "step1", Adapter: "s1", Satisfies: []string{"phase1Done"}, Outputs: []Output{{Name: "out", Type: "string"}}},
			"step2":  {Name: "step2", Adapter: "s2", Requires: []string{"phase1Done"}, Satisfies: []string{"phase2Done"}, Outputs: []Output{{Name: "out", Type: "string"}}},
			"step3":  {Name: "step3", Adapter: "s3", Requires: []string{"phase2Done"}, Satisfies: []string{"phase3Done"}, Outputs: []Output{{Name: "out", Type: "string"}}},
			"finish": {Name: "finish", Adapter: "f", Requires: []string{"phase3Done"}, Inputs: []Input{{Name: "x", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"finish"}})
	require.NoError(t, err)

	assert.Equal(t, []string{"step1", "step2", "step3", "finish"}, result.Nodes)
}

func TestBackwardChain_RequiresSatisfies_MixedRequiresChain(t *testing.T) {
	// Dependencies via requires/satisfies chain.
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"search": {Name: "search", Adapter: "s", Satisfies: []string{"searched"},
				Outputs: []Output{{Name: "results", Type: "string"}}},
			"prepare": {Name: "prepare", Adapter: "p", Requires: []string{"searched"},
				Satisfies: []string{"prepared"},
				Inputs:    []Input{{Name: "data", Type: "string"}},
				Outputs:   []Output{{Name: "out", Type: "string"}}},
			"commit": {Name: "commit", Adapter: "c", Requires: []string{"prepared"},
				Inputs:  []Input{{Name: "out", Type: "string"}},
				Outputs: []Output{{Name: "result", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"commit"}})
	require.NoError(t, err)

	assert.Equal(t, []string{"search", "prepare", "commit"}, result.Nodes)
	// Should have requires edges for the chain.
	assert.NotEmpty(t, result.RequiresEdges)
}

func TestBackwardChain_RequiresSatisfies_CycleBreakerStopsRequires(t *testing.T) {
	// CycleBreaker should stop requires traversal.
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"root":   {Name: "root", Adapter: "r", Satisfies: []string{"rootReady"}, Outputs: []Output{{Name: "out", Type: "string"}}},
			"middle": {Name: "middle", Adapter: "m", CycleBreaker: true, Requires: []string{"rootReady"}, Satisfies: []string{"middleDone"}, Outputs: []Output{{Name: "out", Type: "string"}}},
			"end":    {Name: "end", Adapter: "e", Requires: []string{"middleDone"}, Inputs: []Input{{Name: "x", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"end"}})
	require.NoError(t, err)

	// middle is a cycle-breaker, so root should NOT be included.
	assert.Contains(t, result.Nodes, "middle")
	assert.Contains(t, result.Nodes, "end")
	assert.NotContains(t, result.Nodes, "root")
}

func TestBackwardChain_RequiresSatisfies_NodeAppearsOnce(t *testing.T) {
	// Node included via requires — should appear exactly once.
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"source": {Name: "source", Adapter: "s", Satisfies: []string{"sourceReady"},
				Outputs: []Output{{Name: "data", Type: "string"}}},
			"target": {Name: "target", Adapter: "t", Requires: []string{"sourceReady"},
				Inputs:  []Input{{Name: "data", Type: "string"}},
				Outputs: []Output{{Name: "out", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"target"}})
	require.NoError(t, err)

	assert.Equal(t, []string{"source", "target"}, result.Nodes)
	assert.Len(t, result.RequiresEdges, 1)
}

func TestBackwardChain_RequiresSatisfies_NoRequiresReturnsGoalOnly(t *testing.T) {
	// Graph with no requires/satisfies — only the goal node is reachable.
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a", Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Inputs: []Input{{Name: "x", Type: "string"}}},
		},
	}
	g.BuildSatisfierIndex()

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"b"}})
	require.NoError(t, err)

	assert.Equal(t, []string{"b"}, result.Nodes)
	assert.Empty(t, result.RequiresEdges)
}
