package intent

import (
	"strings"
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
)

// helper to build a minimal graph with named nodes.
func testGraph(nodes map[string]string) *graph.Graph {
	g := &graph.Graph{Nodes: map[string]*graph.Node{}}
	for name, desc := range nodes {
		g.Nodes[name] = &graph.Node{Description: desc}
	}
	return g
}

func testPlan(steps []plan.Step) *plan.Plan {
	return &plan.Plan{
		Metadata: plan.Metadata{Prompt: "test prompt"},
		Intent:   plan.Intent{Description: "Test plan"},
		Execution: plan.Execution{
			Steps: steps,
		},
	}
}

func TestFormatPlanSummary_Minimal(t *testing.T) {
	ws := &WorkflowSelection{Workflow: "Simple Booking"}
	p := testPlan([]plan.Step{
		{Node: "search"},
		{Node: "book"},
	})
	g := testGraph(map[string]string{
		"search": "Search for flights",
		"book":   "Book a flight",
	})

	result := FormatPlanSummary(ws, nil, p, g)

	assert.Contains(t, result, "Plan: Test plan")
	assert.Contains(t, result, "Workflow: Simple Booking")
	assert.Contains(t, result, "1. search — Search for flights")
	assert.Contains(t, result, "2. book — Book a flight")
	// No values/selections/assertions/cleanup sections
	assert.NotContains(t, result, "LLM-provided values:")
	assert.NotContains(t, result, "Selection overrides:")
	assert.NotContains(t, result, "Assertions:")
	assert.NotContains(t, result, "Cleanup:")
}

func TestFormatPlanSummary_Full(t *testing.T) {
	ws := &WorkflowSelection{
		Workflow: "Round-Trip Booking",
		Choices:  map[string]string{"payment": "American Express"},
		Addons:   []string{"Seat Selection"},
		Layers:   []string{"european", "premium"},
	}
	tr := &TargetedResponse{
		Values: map[string]any{
			"search.origin":      "CDG",
			"search.destination": "FCO",
			"book.travelers":     "Marco Rossi",
		},
		Selections: map[string]TargetedSelection{
			"selectOffer.offeringId": {Strategy: "min", SortField: "price"},
		},
		Assertions: map[string][]TargetedAssertion{
			"book": {
				{Type: "status", Expect: float64(200)},
				{Type: "fieldExists", Path: "Reservation.Locator"},
			},
		},
	}
	p := testPlan([]plan.Step{
		{Node: "search"},
		{Node: "selectOffer"},
		{Node: "book"},
	})
	p.Execution.Cleanup = []plan.CleanupStep{
		{Node: "cancelWorkbench", RunOn: "always"},
		{Node: "cancelReservation", RunOn: "failure"},
	}
	g := testGraph(map[string]string{
		"search":      "Search for flights",
		"selectOffer": "Select a flight offer",
		"book":        "Book the reservation",
	})

	result := FormatPlanSummary(ws, tr, p, g)

	// Workflow section
	assert.Contains(t, result, "Workflow: Round-Trip Booking")
	assert.Contains(t, result, "Choices: payment → American Express")
	assert.Contains(t, result, "Addons: Seat Selection")
	assert.Contains(t, result, "Layers: european, premium")

	// Steps
	assert.Contains(t, result, "1. search — Search for flights")
	assert.Contains(t, result, "2. selectOffer — Select a flight offer")
	assert.Contains(t, result, "3. book — Book the reservation")

	// Values grouped by step, sorted by step order
	assert.Contains(t, result, "LLM-provided values:")
	assert.Contains(t, result, "  search:")
	assert.Contains(t, result, "    destination: FCO")
	assert.Contains(t, result, "    origin: CDG")
	assert.Contains(t, result, "  book:")
	assert.Contains(t, result, "    travelers: Marco Rossi")

	// Verify step order in values (search before book)
	searchIdx := strings.Index(result, "  search:")
	bookIdx := strings.Index(result, "  book:")
	assert.Greater(t, bookIdx, searchIdx, "search values should appear before book values")

	// Selection overrides
	assert.Contains(t, result, "Selection overrides:")
	assert.Contains(t, result, "selectOffer.offeringId: strategy: min, sortField: price")

	// Assertions
	assert.Contains(t, result, "Assertions:")
	assert.Contains(t, result, "book: status 200, fieldExists Reservation.Locator")

	// Cleanup
	assert.Contains(t, result, "Cleanup: cancelWorkbench (always), cancelReservation (failure)")
}

func TestFormatPlanSummary_NilWorkflowSelection(t *testing.T) {
	tr := &TargetedResponse{
		Values: map[string]any{"step1.val": "hello"},
	}
	p := testPlan([]plan.Step{{Node: "step1"}})
	g := testGraph(map[string]string{"step1": "Do something"})

	result := FormatPlanSummary(nil, tr, p, g)

	assert.NotContains(t, result, "Workflow:")
	assert.Contains(t, result, "1. step1 — Do something")
	assert.Contains(t, result, "LLM-provided values:")
}

func TestFormatPlanSummary_NilTargetedResponse(t *testing.T) {
	ws := &WorkflowSelection{Workflow: "Test"}
	p := testPlan([]plan.Step{{Node: "step1"}})
	g := testGraph(map[string]string{"step1": "Do something"})

	result := FormatPlanSummary(ws, nil, p, g)

	assert.Contains(t, result, "Workflow: Test")
	assert.Contains(t, result, "1. step1")
	assert.NotContains(t, result, "LLM-provided values:")
	assert.NotContains(t, result, "Selection overrides:")
	assert.NotContains(t, result, "Assertions:")
}

func TestFormatPlanSummary_ValueGrouping(t *testing.T) {
	tr := &TargetedResponse{
		Values: map[string]any{
			"step2.z":     "last",
			"step2.a":     "first",
			"step1.field": "val",
		},
	}
	p := testPlan([]plan.Step{
		{Node: "step1"},
		{Node: "step2"},
	})
	g := testGraph(map[string]string{})

	result := FormatPlanSummary(nil, tr, p, g)

	// step1 group before step2 (step order)
	step1Idx := strings.Index(result, "  step1:")
	step2Idx := strings.Index(result, "  step2:")
	assert.Greater(t, step2Idx, step1Idx)

	// Within step2, a before z (alphabetical)
	aIdx := strings.Index(result, "    a: first")
	zIdx := strings.Index(result, "    z: last")
	assert.Greater(t, zIdx, aIdx)
}

func TestFormatPlanSummary_SelectionOverridesDetails(t *testing.T) {
	tr := &TargetedResponse{
		Values: map[string]any{},
		Selections: map[string]TargetedSelection{
			"step1.sel1": {Strategy: "match", Filter: "carrier == 'UA'"},
			"step1.sel2": {Prompt: "pick the cheapest"},
			"step2.sel3": {Index: 3},
		},
	}
	p := testPlan([]plan.Step{
		{Node: "step1"},
		{Node: "step2"},
	})
	g := testGraph(map[string]string{})

	result := FormatPlanSummary(nil, tr, p, g)

	assert.Contains(t, result, "Selection overrides:")
	assert.Contains(t, result, "step1.sel1: strategy: match, filter: carrier == 'UA'")
	assert.Contains(t, result, `step1.sel2: prompt: "pick the cheapest"`)
	assert.Contains(t, result, "step2.sel3: index: 3")
}

func TestFormatPlanSummary_AssertionTypes(t *testing.T) {
	tr := &TargetedResponse{
		Values: map[string]any{},
		Assertions: map[string][]TargetedAssertion{
			"step1": {
				{Type: "status", Expect: float64(201)},
				{Type: "fieldExists", Path: "Result.ID"},
				{Type: "fieldEquals", Path: "Result.Type", Value: "booking"},
				{Type: "predicate", Expr: "$.count > 0"},
			},
		},
	}
	p := testPlan([]plan.Step{{Node: "step1"}})
	g := testGraph(map[string]string{})

	result := FormatPlanSummary(nil, tr, p, g)

	assert.Contains(t, result, "status 201")
	assert.Contains(t, result, "fieldExists Result.ID")
	assert.Contains(t, result, "fieldEquals Result.Type=booking")
	assert.Contains(t, result, "predicate: $.count > 0")
}

func TestFormatPlanSummary_StepDescFromGraph(t *testing.T) {
	p := testPlan([]plan.Step{
		{Node: "nodeA", Description: "Custom desc"},
		{Node: "nodeB"}, // no step description — should use graph
	})
	g := testGraph(map[string]string{
		"nodeA": "Graph desc A",
		"nodeB": "Graph desc B",
	})

	result := FormatPlanSummary(nil, nil, p, g)

	assert.Contains(t, result, "1. nodeA — Custom desc")
	assert.Contains(t, result, "2. nodeB — Graph desc B")
}

func TestFormatPlanSummary_CleanupFormat(t *testing.T) {
	p := testPlan([]plan.Step{{Node: "step1"}})
	p.Execution.Cleanup = []plan.CleanupStep{
		{Node: "cleanup1"},
		{Node: "cleanup2", RunOn: "failure"},
		{Node: "cleanup3", RunOn: "success"},
	}
	g := testGraph(map[string]string{})

	result := FormatPlanSummary(nil, nil, p, g)

	assert.Contains(t, result, "Cleanup: cleanup1 (always), cleanup2 (failure), cleanup3 (success)")
}

func TestFormatPlanSummary_TitleFallback(t *testing.T) {
	p := &plan.Plan{
		Metadata: plan.Metadata{Prompt: "do something"},
		Execution: plan.Execution{
			Steps: []plan.Step{{Node: "step1"}},
		},
	}
	g := testGraph(map[string]string{})

	result := FormatPlanSummary(nil, nil, p, g)

	// Falls back to metadata.prompt when intent.description is empty
	assert.Contains(t, result, "Plan: do something")
}

func TestFormatPlanSummary_StepWithID(t *testing.T) {
	p := testPlan([]plan.Step{
		{ID: "leg1Search", Node: "search", Description: "First leg search"},
		{ID: "leg2Search", Node: "search", Description: "Second leg search"},
	})
	g := testGraph(map[string]string{})

	result := FormatPlanSummary(nil, nil, p, g)

	assert.Contains(t, result, "1. leg1Search — First leg search")
	assert.Contains(t, result, "2. leg2Search — Second leg search")
}

func TestFormatPlanSummary_EmptySelectionsAndAssertions(t *testing.T) {
	tr := &TargetedResponse{
		Values:     map[string]any{"step1.val": "x"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}
	p := testPlan([]plan.Step{{Node: "step1"}})
	g := testGraph(map[string]string{})

	result := FormatPlanSummary(nil, tr, p, g)

	assert.Contains(t, result, "LLM-provided values:")
	assert.NotContains(t, result, "Selection overrides:")
	assert.NotContains(t, result, "Assertions:")
}

func TestFormatPlanSummary_Deterministic(t *testing.T) {
	ws := &WorkflowSelection{
		Workflow: "Test",
		Choices:  map[string]string{"a": "1", "b": "2"},
		Layers:   []string{"x", "y"},
	}
	tr := &TargetedResponse{
		Values: map[string]any{
			"s2.b": "2",
			"s1.a": "1",
			"s2.a": "3",
		},
		Selections: map[string]TargetedSelection{
			"s2.sel": {Strategy: "first"},
			"s1.sel": {Strategy: "last"},
		},
	}
	p := testPlan([]plan.Step{
		{Node: "s1"},
		{Node: "s2"},
	})
	g := testGraph(map[string]string{})

	r1 := FormatPlanSummary(ws, tr, p, g)
	r2 := FormatPlanSummary(ws, tr, p, g)
	assert.Equal(t, r1, r2)
}
