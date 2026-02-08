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
	for _, name := range names {
		g.Nodes[name] = &Node{
			Name:    name,
			Adapter: name,
			Inputs:  []Input{{Name: "input1", Type: "string"}},
			Outputs: []Output{{Name: "output1", Type: "string"}},
		}
	}
	for i := 0; i < len(names)-1; i++ {
		g.Edges = append(g.Edges, Edge{
			From: names[i] + ".output1",
			To:   names[i+1] + ".input1",
		})
	}
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
	assert.Empty(t, result.Edges)
	assert.Equal(t, []string{"standalone"}, result.EntryNodes)
}

func TestBackwardChain_LinearChain(t *testing.T) {
	g := buildLinearGraph("a", "b", "c")

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"c"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, result.Nodes)
	assert.Len(t, result.Edges, 2)
	assert.Equal(t, []string{"a"}, result.EntryNodes)
}

func TestBackwardChain_MidChainGoal(t *testing.T) {
	g := buildLinearGraph("a", "b", "c")

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"b"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, result.Nodes)
	assert.Len(t, result.Edges, 1)
	assert.Equal(t, []string{"a"}, result.EntryNodes)
	// c should not be included
	assert.NotContains(t, result.Nodes, "c")
}

func TestBackwardChain_FanIn(t *testing.T) {
	// D depends on both B and C; B and C depend on A
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a", Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"c": {Name: "c", Adapter: "c", Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"d": {Name: "d", Adapter: "d", Inputs: []Input{{Name: "x1", Type: "string"}, {Name: "x2", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
		Edges: []Edge{
			{From: "a.y", To: "b.x"},
			{From: "a.y", To: "c.x"},
			{From: "b.y", To: "d.x1"},
			{From: "c.y", To: "d.x2"},
		},
	}

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
			"a": {Name: "a", Adapter: "a", Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"c": {Name: "c", Adapter: "c", Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
		Edges: []Edge{
			{From: "a.y", To: "b.x"},
			{From: "a.y", To: "c.x"},
		},
	}

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
			"a": {Name: "a", Adapter: "a", CycleBreaker: true, Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
		Edges: []Edge{{From: "a.y", To: "b.x"}},
	}

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"b"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, result.Nodes)
	assert.Contains(t, result.EntryNodes, "a")
}

func TestBackwardChain_CycleWithoutBreaker_Error(t *testing.T) {
	// Build a cyclic graph (manually, bypassing Validate which would catch it)
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a", Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"c": {Name: "c", Adapter: "c", Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
		Edges: []Edge{
			{From: "a.y", To: "b.x"},
			{From: "b.y", To: "c.x"},
			{From: "c.y", To: "a.x"},
		},
	}

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

// --- Multiple Path Tests ---

func TestBackwardChain_ShortestPathChosen(t *testing.T) {
	// Build graph where consumer.result can come from transform (depth 2) or shortcut (depth 0)
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"source":   {Name: "source", Adapter: "source", Inputs: nil, Outputs: []Output{{Name: "data", Type: "string"}}},
			"transform": {Name: "transform", Adapter: "transform", Inputs: []Input{{Name: "data", Type: "string"}}, Outputs: []Output{{Name: "result", Type: "string"}}},
			"shortcut":  {Name: "shortcut", Adapter: "shortcut", Inputs: nil, Outputs: []Output{{Name: "result", Type: "string"}}},
			"consumer":  {Name: "consumer", Adapter: "consumer", Inputs: []Input{{Name: "result", Type: "string"}}, Outputs: []Output{{Name: "output", Type: "string"}}},
		},
		Edges: []Edge{
			{From: "source.data", To: "transform.data"},
			{From: "transform.result", To: "consumer.result"},
			{From: "shortcut.result", To: "consumer.result"},
		},
	}

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"consumer"}})
	require.NoError(t, err)

	// shortcut has depth 0, source has depth 0, transform has depth 1
	// shortcut is shallowest producer for consumer.result
	assert.Contains(t, result.Nodes, "shortcut")
	assert.Contains(t, result.Nodes, "consumer")
	// source and transform should be pruned as unreachable
	assert.NotContains(t, result.Nodes, "source")
	assert.NotContains(t, result.Nodes, "transform")

	// Should have a path choice decision
	var hasPathChoice bool
	for _, d := range result.Decisions {
		if d.Type == DecisionPathChoice {
			hasPathChoice = true
		}
	}
	assert.True(t, hasPathChoice)
}

func TestBackwardChain_PreferredEdgeOverridesShortest(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"source":    {Name: "source", Adapter: "source", Inputs: nil, Outputs: []Output{{Name: "data", Type: "string"}}},
			"preferred": {Name: "preferred", Adapter: "preferred", Inputs: []Input{{Name: "data", Type: "string"}}, Outputs: []Output{{Name: "result", Type: "string"}}},
			"shortcut":  {Name: "shortcut", Adapter: "shortcut", Inputs: nil, Outputs: []Output{{Name: "result", Type: "string"}}},
			"consumer":  {Name: "consumer", Adapter: "consumer", Inputs: []Input{{Name: "result", Type: "string"}}, Outputs: []Output{{Name: "output", Type: "string"}}},
		},
		Edges: []Edge{
			{From: "source.data", To: "preferred.data"},
			{From: "preferred.result", To: "consumer.result", Preferred: true},
			{From: "shortcut.result", To: "consumer.result"},
		},
	}

	result, err := BackwardChain(g, ChainOptions{Goals: []string{"consumer"}})
	require.NoError(t, err)

	// preferred path should be chosen despite being longer
	assert.Contains(t, result.Nodes, "source")
	assert.Contains(t, result.Nodes, "preferred")
	assert.Contains(t, result.Nodes, "consumer")
	// shortcut should be pruned
	assert.NotContains(t, result.Nodes, "shortcut")
}

// --- Entry Node Tests ---

func TestBackwardChain_EntryNodes_MultipleRoots(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"root1": {Name: "root1", Adapter: "root1", Inputs: nil, Outputs: []Output{{Name: "y", Type: "string"}}},
			"root2": {Name: "root2", Adapter: "root2", Inputs: nil, Outputs: []Output{{Name: "y", Type: "string"}}},
			"sink":  {Name: "sink", Adapter: "sink", Inputs: []Input{{Name: "x1", Type: "string"}, {Name: "x2", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
		Edges: []Edge{
			{From: "root1.y", To: "sink.x1"},
			{From: "root2.y", To: "sink.x2"},
		},
	}

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
	// Diamond: a → b, a → c, b → d, c → d
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a", Inputs: nil, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"c": {Name: "c", Adapter: "c", Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"d": {Name: "d", Adapter: "d", Inputs: []Input{{Name: "x1", Type: "string"}, {Name: "x2", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
		Edges: []Edge{
			{From: "a.y", To: "b.x"},
			{From: "a.y", To: "c.x"},
			{From: "b.y", To: "d.x1"},
			{From: "c.y", To: "d.x2"},
		},
	}

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

func TestParse_PreferredEdgeField(t *testing.T) {
	g, err := ParseFile("testdata/valid/with_multiple_paths.yaml")
	require.NoError(t, err)

	var preferredCount int
	for _, e := range g.Edges {
		if e.Preferred {
			preferredCount++
			assert.Equal(t, "preferredSource.result", e.From)
			assert.Equal(t, "consumer.result", e.To)
		}
	}
	assert.Equal(t, 1, preferredCount)
}

// --- Validation Tests ---

func TestValidate_CycleBreakerToleratesCycle(t *testing.T) {
	g, err := ParseFile("testdata/valid/with_cycle_breaker.yaml")
	// Should parse without error despite the auth cycle
	assert.NoError(t, err)
	assert.NotNil(t, g)
}

func TestValidate_CycleWithoutBreakerStillFails(t *testing.T) {
	_, err := ParseFile("testdata/invalid/cycle.yaml")
	require.Error(t, err)
	assertValidationContains(t, err, "cycle detected")
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
			"a": {Name: "a", Adapter: "a", Inputs: nil, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
		Edges: []Edge{
			{From: "a.y", To: "b.x"},
		},
		Conditions: []Condition{
			{When: "always == true", Before: []string{"b"}},
		},
	}

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
	assert.Empty(t, result.Edges)
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
