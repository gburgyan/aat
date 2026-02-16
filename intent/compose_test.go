package intent

import (
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- isPlaceholder ---

func TestIsPlaceholder(t *testing.T) {
	tests := []struct {
		name   string
		sv     plan.StepValue
		expect bool
	}{
		{"bare PLACEHOLDER", plan.StepValue{Default: "PLACEHOLDER"}, true},
		{"lowercase", plan.StepValue{Default: "placeholder"}, false},
		{"non-string", plan.StepValue{Default: 123}, false},
		{"nil default", plan.StepValue{}, false},
		{"from ref", plan.StepValue{From: "step.output"}, false},
		{"normal string", plan.StepValue{Default: "DEN"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, isPlaceholder(tt.sv))
		})
	}
}

// --- buildOutputMap ---

func TestBuildOutputMap(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search",
				Outputs: []graph.Output{
					{Name: "results", Type: "string[]"},
				},
			},
			"book": {
				Name: "book",
				Outputs: []graph.Output{
					{Name: "workbenchId", Type: "string"},
					{Name: "offerIdentifierValue", Type: "string"},
				},
			},
			"commit": {
				Name: "commit",
				Outputs: []graph.Output{
					{Name: "locator", Type: "string"},
				},
			},
		},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search"},
				{Node: "book"},
				{Node: "commit"},
			},
		},
	}

	t.Run("up to book", func(t *testing.T) {
		m := buildOutputMap(p, g, "book")
		assert.Equal(t, "search.results", m["results"])
		assert.Equal(t, "book.workbenchId", m["workbenchId"])
		assert.Equal(t, "book.offerIdentifierValue", m["offerIdentifierValue"])
		_, hasLocator := m["locator"]
		assert.False(t, hasLocator, "commit output should not be included")
	})

	t.Run("all steps", func(t *testing.T) {
		m := buildOutputMap(p, g, "commit")
		assert.Equal(t, "commit.locator", m["locator"])
	})

	t.Run("first step only", func(t *testing.T) {
		m := buildOutputMap(p, g, "search")
		assert.Equal(t, "search.results", m["results"])
		_, hasWB := m["workbenchId"]
		assert.False(t, hasWB)
	})
}

// --- prefixStepRefs ---

func TestPrefixStepRefs(t *testing.T) {
	sub := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node:   "searchSeatMap",
					Values: map[string]plan.StepValue{},
				},
				{
					Node:      "addSeat",
					DependsOn: []string{"searchSeatMap"},
					Values: map[string]plan.StepValue{
						"seatData": {From: "searchSeatMap.seats"},
					},
					Selections: map[string]plan.StepSelection{
						"seat": {From: "searchSeatMap.seatOfferings"},
					},
				},
			},
		},
	}

	prefixStepRefs(sub, "inc0_")

	// Step IDs should be prefixed.
	assert.Equal(t, "inc0_searchSeatMap", sub.Execution.Steps[0].ID)
	assert.Equal(t, "inc0_addSeat", sub.Execution.Steps[1].ID)

	// DependsOn should be rewritten.
	assert.Equal(t, []string{"inc0_searchSeatMap"}, sub.Execution.Steps[1].DependsOn)

	// From refs should be rewritten.
	assert.Equal(t, "inc0_searchSeatMap.seats", sub.Execution.Steps[1].Values["seatData"].From)

	// Named selection from should be rewritten.
	assert.Equal(t, "inc0_searchSeatMap.seatOfferings", sub.Execution.Steps[1].Selections["seat"].From)
}

func TestPrefixStepRefs_WithExistingID(t *testing.T) {
	sub := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					ID:   "mySearch",
					Node: "searchSeatMap",
				},
				{
					Node:      "addSeat",
					DependsOn: []string{"mySearch"},
					Values: map[string]plan.StepValue{
						"data": {From: "mySearch.output"},
					},
				},
			},
		},
	}

	prefixStepRefs(sub, "inc1_")

	assert.Equal(t, "inc1_mySearch", sub.Execution.Steps[0].ID)
	assert.Equal(t, "inc1_addSeat", sub.Execution.Steps[1].ID)
	assert.Equal(t, []string{"inc1_mySearch"}, sub.Execution.Steps[1].DependsOn)
	assert.Equal(t, "inc1_mySearch.output", sub.Execution.Steps[1].Values["data"].From)
}

// --- autoWirePlaceholders ---

func TestAutoWirePlaceholders(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"searchSeatMap": {Name: "searchSeatMap"},
			"addSeat":       {Name: "addSeat"},
		},
	}

	sub := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					ID:   "inc0_searchSeatMap",
					Node: "searchSeatMap",
					Values: map[string]plan.StepValue{
						"workbenchId":          {Default: "PLACEHOLDER"},
						"offerIdentifierValue": {Default: "PLACEHOLDER"},
						"manualInput":          {Default: "PLACEHOLDER"},
					},
				},
			},
		},
	}

	outputMap := map[string]string{
		"workbenchId":          "createWorkbench.workbenchId",
		"offerIdentifierValue": "addOffer.offerIdentifierValue",
	}

	wire := map[string]string{
		"manualInput": "MANUAL",
	}

	autoWirePlaceholders(sub, outputMap, wire, g)

	step := sub.Execution.Steps[0]
	// Auto-wired from output map.
	assert.Equal(t, "createWorkbench.workbenchId", step.Values["workbenchId"].From)
	assert.Nil(t, step.Values["workbenchId"].Default)

	// Auto-wired from output map.
	assert.Equal(t, "addOffer.offerIdentifierValue", step.Values["offerIdentifierValue"].From)
	assert.Nil(t, step.Values["offerIdentifierValue"].Default)

	// MANUAL clears the value.
	assert.Nil(t, step.Values["manualInput"].Default)
	assert.Empty(t, step.Values["manualInput"].From)
}

func TestAutoWirePlaceholders_ExplicitWireOverride(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"step1": {Name: "step1"},
		},
	}

	sub := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					ID:   "inc0_step1",
					Node: "step1",
					Values: map[string]plan.StepValue{
						"workbenchId": {Default: "PLACEHOLDER"},
					},
				},
			},
		},
	}

	outputMap := map[string]string{
		"workbenchId": "autoStep.workbenchId", // would be auto-wired
	}

	wire := map[string]string{
		"workbenchId": "explicitStep.workbenchId", // explicit override wins
	}

	autoWirePlaceholders(sub, outputMap, wire, g)

	step := sub.Execution.Steps[0]
	assert.Equal(t, "explicitStep.workbenchId", step.Values["workbenchId"].From)
}

func TestAutoWirePlaceholders_NoMatchLeavesPlaceholder(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"step1": {Name: "step1"},
		},
	}

	sub := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					ID:   "inc0_step1",
					Node: "step1",
					Values: map[string]plan.StepValue{
						"unknownInput": {Default: "PLACEHOLDER"},
					},
				},
			},
		},
	}

	autoWirePlaceholders(sub, map[string]string{}, nil, g)

	step := sub.Execution.Steps[0]
	assert.Equal(t, "PLACEHOLDER", step.Values["unknownInput"].Default)
}

func TestAutoWirePlaceholders_NonPlaceholderUntouched(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"step1": {Name: "step1"},
		},
	}

	sub := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					ID:   "inc0_step1",
					Node: "step1",
					Values: map[string]plan.StepValue{
						"fixedValue":  {Default: "12A"},
						"wiredValue":  {From: "other.output"},
						"seatAssignment": {Default: "14C"},
					},
				},
			},
		},
	}

	outputMap := map[string]string{
		"fixedValue": "parent.fixedValue",
	}

	autoWirePlaceholders(sub, outputMap, nil, g)

	step := sub.Execution.Steps[0]
	assert.Equal(t, "12A", step.Values["fixedValue"].Default)
	assert.Equal(t, "other.output", step.Values["wiredValue"].From)
	assert.Equal(t, "14C", step.Values["seatAssignment"].Default)
}

// --- addInsertionDeps ---

func TestAddInsertionDeps(t *testing.T) {
	sub := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{ID: "inc0_a", Node: "a"},                                           // root
				{ID: "inc0_b", Node: "b", DependsOn: []string{"inc0_a"}},            // not root
				{ID: "inc0_c", Node: "c", DependsOn: []string{"externalStep"}},      // root (dep is outside sub)
			},
		},
	}

	addInsertionDeps(sub, "addTraveler")

	// Root steps should gain the insertion dep.
	assert.Contains(t, sub.Execution.Steps[0].DependsOn, "addTraveler")
	// Non-root step should NOT gain the insertion dep.
	assert.NotContains(t, sub.Execution.Steps[1].DependsOn, "addTraveler")
	// Step with external dep is a root — should gain insertion dep.
	assert.Contains(t, sub.Execution.Steps[2].DependsOn, "addTraveler")
}

func TestAddInsertionDeps_EmptyAfter(t *testing.T) {
	sub := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{ID: "inc0_a", Node: "a"},
			},
		},
	}

	addInsertionDeps(sub, "")

	// No dep added when afterStep is empty.
	assert.Empty(t, sub.Execution.Steps[0].DependsOn)
}

// --- spliceSteps ---

func TestSpliceSteps(t *testing.T) {
	parent := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search"},
				{Node: "book"},
				{Node: "commit"},
			},
		},
	}

	sub := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{ID: "inc0_seatSearch", Node: "seatSearch"},
				{ID: "inc0_addSeat", Node: "addSeat"},
			},
		},
	}

	spliceSteps(parent, sub, "book")

	require.Len(t, parent.Execution.Steps, 5)
	assert.Equal(t, "search", parent.Execution.Steps[0].Node)
	assert.Equal(t, "book", parent.Execution.Steps[1].Node)
	assert.Equal(t, "seatSearch", parent.Execution.Steps[2].Node)
	assert.Equal(t, "addSeat", parent.Execution.Steps[3].Node)
	assert.Equal(t, "commit", parent.Execution.Steps[4].Node)
}

func TestSpliceSteps_AtEnd(t *testing.T) {
	parent := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "a"},
				{Node: "b"},
			},
		},
	}

	sub := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{ID: "inc0_c", Node: "c"},
			},
		},
	}

	spliceSteps(parent, sub, "b")

	require.Len(t, parent.Execution.Steps, 3)
	assert.Equal(t, "b", parent.Execution.Steps[1].Node)
	assert.Equal(t, "c", parent.Execution.Steps[2].Node)
}

func TestSpliceSteps_UnknownAfter(t *testing.T) {
	parent := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "a"},
			},
		},
	}

	sub := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{ID: "inc0_b", Node: "b"},
			},
		},
	}

	// Unknown afterStep → append at end.
	spliceSteps(parent, sub, "unknown")

	require.Len(t, parent.Execution.Steps, 2)
	assert.Equal(t, "a", parent.Execution.Steps[0].Node)
	assert.Equal(t, "b", parent.Execution.Steps[1].Node)
}

// --- mergeCleanup ---

func TestMergeCleanup(t *testing.T) {
	parent := &plan.Plan{
		Execution: plan.Execution{
			Cleanup: []plan.CleanupStep{
				{Node: "ignoreWorkbench", RunOn: "always"},
			},
		},
	}

	sub := &plan.Plan{
		Execution: plan.Execution{
			Cleanup: []plan.CleanupStep{
				{Node: "ignoreWorkbench", RunOn: "always"}, // duplicate — should be deduped
				{Node: "cancelSeat", RunOn: "failure"},     // new — should be added
			},
		},
	}

	mergeCleanup(parent, sub)

	require.Len(t, parent.Execution.Cleanup, 2)
	assert.Equal(t, "ignoreWorkbench", parent.Execution.Cleanup[0].Node)
	assert.Equal(t, "cancelSeat", parent.Execution.Cleanup[1].Node)
}

func TestMergeCleanup_EmptySub(t *testing.T) {
	parent := &plan.Plan{
		Execution: plan.Execution{
			Cleanup: []plan.CleanupStep{
				{Node: "cleanup1"},
			},
		},
	}

	sub := &plan.Plan{}

	mergeCleanup(parent, sub)

	require.Len(t, parent.Execution.Cleanup, 1)
}

// --- FindComposedWorkflow ---

func TestFindComposedWorkflow(t *testing.T) {
	g := &graph.Graph{
		Workflows: []graph.Workflow{
			{Name: "Full-Payload Booking", Template: "plans/booking.yaml"},
			{Name: "Seat Selection", Kind: "addon", Template: "plans/seat.yaml"},
			{
				Name:     "Booking with Seat",
				Template: "plans/booking.yaml",
				Includes: []graph.WorkflowInclude{
					{Workflow: "Seat Selection", After: "book"},
				},
			},
		},
	}

	t.Run("exact match", func(t *testing.T) {
		wf, found := FindComposedWorkflow(g, "Full-Payload Booking", []string{"Seat Selection"})
		assert.True(t, found)
		assert.Equal(t, "Booking with Seat", wf.Name)
	})

	t.Run("no match", func(t *testing.T) {
		_, found := FindComposedWorkflow(g, "Full-Payload Booking", []string{"Nonexistent"})
		assert.False(t, found)
	})

	t.Run("empty addons", func(t *testing.T) {
		_, found := FindComposedWorkflow(g, "Full-Payload Booking", nil)
		assert.False(t, found)
	})

	t.Run("case insensitive", func(t *testing.T) {
		wf, found := FindComposedWorkflow(g, "Full-Payload Booking", []string{"seat selection"})
		assert.True(t, found)
		assert.Equal(t, "Booking with Seat", wf.Name)
	})
}

// --- BuildSyntheticWorkflow ---

func TestBuildSyntheticWorkflow(t *testing.T) {
	g := &graph.Graph{
		Workflows: []graph.Workflow{
			{Name: "Main", Template: "plans/main.yaml", Steps: []string{"search", "book", "pay", "commit"}},
		},
	}

	base := g.Workflows[0]
	wf := BuildSyntheticWorkflow(g, base, []string{"Seat Selection", "Ancillary"})

	assert.Equal(t, "Main (composed)", wf.Name)
	assert.Equal(t, "plans/main.yaml", wf.Template)
	require.Len(t, wf.Includes, 2)
	assert.Equal(t, "Seat Selection", wf.Includes[0].Workflow)
	assert.Equal(t, "pay", wf.Includes[0].After) // second-to-last
	assert.Equal(t, "Ancillary", wf.Includes[1].Workflow)
	assert.Equal(t, "pay", wf.Includes[1].After)
}

// --- ComposeWorkflowTemplate integration test ---

func TestComposeWorkflowTemplate_NoIncludes(t *testing.T) {
	g := buildComposeTestGraph()

	wf := graph.Workflow{
		Name:     "Simple",
		Template: "testdata/compose/parent.yaml",
	}

	p, err := ComposeWorkflowTemplate(wf, ".", g)
	require.NoError(t, err)
	require.Len(t, p.Execution.Steps, 3)
	assert.Equal(t, "search", p.Execution.Steps[0].Node)
}

func TestComposeWorkflowTemplate_WithInclude(t *testing.T) {
	g := buildComposeTestGraph()

	wf := graph.Workflow{
		Name:     "Composed",
		Template: "testdata/compose/parent.yaml",
		Includes: []graph.WorkflowInclude{
			{
				Workflow: "Addon",
				After:    "book",
				Wire: map[string]string{
					"specialInput": "MANUAL",
				},
			},
		},
	}

	p, err := ComposeWorkflowTemplate(wf, ".", g)
	require.NoError(t, err)

	// Parent has 3 steps, addon has 2 steps = 5 total.
	require.Len(t, p.Execution.Steps, 5)

	// Order: search, book, inc0_addon1, inc0_addon2, commit.
	assert.Equal(t, "search", p.Execution.Steps[0].Node)
	assert.Equal(t, "book", p.Execution.Steps[1].Node)
	assert.Equal(t, "addon1", p.Execution.Steps[2].Node)
	assert.Equal(t, "inc0_addon1", p.Execution.Steps[2].StepID())
	assert.Equal(t, "addon2", p.Execution.Steps[3].Node)
	assert.Equal(t, "inc0_addon2", p.Execution.Steps[3].StepID())
	assert.Equal(t, "commit", p.Execution.Steps[4].Node)

	// Check auto-wiring: workbenchId in addon1 should be wired to book.workbenchId.
	addon1 := p.Execution.Steps[2]
	assert.Equal(t, "book.workbenchId", addon1.Values["workbenchId"].From)
	assert.Nil(t, addon1.Values["workbenchId"].Default)

	// Check MANUAL wire: specialInput should be cleared.
	assert.Nil(t, addon1.Values["specialInput"].Default)
	assert.Empty(t, addon1.Values["specialInput"].From)

	// Check insertion dep: addon1 depends on "book".
	assert.Contains(t, addon1.DependsOn, "book")

	// addon2 depends on inc0_addon1 (internal dep preserved).
	addon2 := p.Execution.Steps[3]
	assert.Contains(t, addon2.DependsOn, "inc0_addon1")

	// Cleanup should have parent cleanup only (addon has none in this test).
	assert.Len(t, p.Execution.Cleanup, 1)
}

func TestComposeWorkflowTemplate_NoTemplate(t *testing.T) {
	g := buildComposeTestGraph()

	wf := graph.Workflow{
		Name: "NoTemplate",
	}

	_, err := ComposeWorkflowTemplate(wf, ".", g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no template")
}

func TestComposeWorkflowTemplate_UnknownInclude(t *testing.T) {
	g := buildComposeTestGraph()

	wf := graph.Workflow{
		Name:     "Bad",
		Template: "testdata/compose/parent.yaml",
		Includes: []graph.WorkflowInclude{
			{Workflow: "NonExistent", After: "book"},
		},
	}

	_, err := ComposeWorkflowTemplate(wf, ".", g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown workflow")
}

// buildComposeTestGraph creates a synthetic graph for composition tests.
func buildComposeTestGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{Name: "Addon", Kind: "addon", Template: "testdata/compose/addon.yaml"},
		},
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Adapter: "search",
				Outputs: []graph.Output{{Name: "results", Type: "string[]"}},
			},
			"book": {
				Name: "book", Adapter: "book",
				Inputs:  []graph.Input{{Name: "flightId", Type: "string"}},
				Outputs: []graph.Output{{Name: "workbenchId", Type: "string"}},
			},
			"commit": {
				Name: "commit", Adapter: "commit",
				Inputs:  []graph.Input{{Name: "workbenchId", Type: "string"}},
				Outputs: []graph.Output{{Name: "locator", Type: "string"}},
			},
			"addon1": {
				Name: "addon1", Adapter: "addon1",
				Inputs: []graph.Input{
					{Name: "workbenchId", Type: "string"},
					{Name: "specialInput", Type: "string"},
				},
				Outputs: []graph.Output{{Name: "result1", Type: "string"}},
			},
			"addon2": {
				Name: "addon2", Adapter: "addon2",
				Inputs:  []graph.Input{{Name: "data", Type: "string"}},
				Outputs: []graph.Output{{Name: "result2", Type: "string"}},
			},
		},
	}
}
