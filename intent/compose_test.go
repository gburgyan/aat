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
		{"bare AUTOWIRE", plan.StepValue{Default: "AUTOWIRE"}, true},
		{"legacy PLACEHOLDER", plan.StepValue{Default: "PLACEHOLDER"}, true},
		{"lowercase", plan.StepValue{Default: "placeholder"}, false},
		{"lowercase autowire", plan.StepValue{Default: "autowire"}, false},
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

	m := buildOutputMap(p, g)

	// All outputs from all steps should be included.
	assert.Equal(t, "search.results", m["results"])
	assert.Equal(t, "book.workbenchId", m["workbenchId"])
	assert.Equal(t, "book.offerIdentifierValue", m["offerIdentifierValue"])
	assert.Equal(t, "commit.locator", m["locator"])
}

func TestBuildOutputMap_LastProducerWins(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"stepA": {
				Name:    "stepA",
				Outputs: []graph.Output{{Name: "sharedOutput", Type: "string"}},
			},
			"stepB": {
				Name:    "stepB",
				Outputs: []graph.Output{{Name: "sharedOutput", Type: "string"}},
			},
		},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "stepA"},
				{Node: "stepB"},
			},
		},
	}

	m := buildOutputMap(p, g)
	assert.Equal(t, "stepB.sharedOutput", m["sharedOutput"])
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
						"workbenchId":          {Default: "AUTOWIRE"},
						"offerIdentifierValue": {Default: "AUTOWIRE"},
						"manualInput":          {Default: "AUTOWIRE"},
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
						"workbenchId": {Default: "AUTOWIRE"},
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
						"unknownInput": {Default: "AUTOWIRE"},
					},
				},
			},
		},
	}

	autoWirePlaceholders(sub, map[string]string{}, nil, g)

	step := sub.Execution.Steps[0]
	assert.Equal(t, "AUTOWIRE", step.Values["unknownInput"].Default)
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
						"fixedValue":     {Default: "12A"},
						"wiredValue":     {From: "other.output"},
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
				{ID: "inc0_a", Node: "a"},                                      // root
				{ID: "inc0_b", Node: "b", DependsOn: []string{"inc0_a"}},       // not root
				{ID: "inc0_c", Node: "c", DependsOn: []string{"externalStep"}}, // root (dep is outside sub)
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

// --- findStepByNode ---

func TestFindStepByNode(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search"},
				{ID: "myBook", Node: "book"},
				{Node: "commit"},
			},
		},
	}

	assert.Equal(t, "search", findStepByNode(p, "search"))
	assert.Equal(t, "myBook", findStepByNode(p, "book"))
	assert.Equal(t, "commit", findStepByNode(p, "commit"))
	assert.Equal(t, "", findStepByNode(p, "nonexistent"))
}

// --- Compose integration tests ---

func TestCompose_NoAddons(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Simple",
		Template: "testdata/compose/parent.yaml",
	}

	p, err := Compose(ComposeRequest{Base: base, Graph: g, GraphDir: "."})
	require.NoError(t, err)
	require.Len(t, p.Execution.Steps, 3)
	assert.Equal(t, "search", p.Execution.Steps[0].Node)
}

func TestCompose_WithAddon(t *testing.T) {
	g := buildComposeTestGraph()
	setWorkflowWire(g, "Addon", map[string]string{"specialInput": "MANUAL"})

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	p, err := Compose(ComposeRequest{Base: base, Addons: []string{"Addon"}, Graph: g, GraphDir: "."})
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

func TestCompose_NoTemplate(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name: "NoTemplate",
	}

	_, err := Compose(ComposeRequest{Base: base, Graph: g, GraphDir: "."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no template")
}

func TestCompose_AddonAfterNotInBase(t *testing.T) {
	g := buildComposeTestGraph()
	setWorkflowAfter(g, "Addon", graph.AfterSpec{"nonexistentNode"})

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	_, err := Compose(ComposeRequest{Base: base, Addons: []string{"Addon"}, Graph: g, GraphDir: "."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "found in base plan steps")
}

func TestCompose_AddonNoTemplate(t *testing.T) {
	g := buildComposeTestGraph()
	g.Workflows = append(g.Workflows, graph.Workflow{
		Name:  "BadAddon",
		Kind:  "addon",
		After: graph.AfterSpec{"book"},
		// No Template
	})

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	_, err := Compose(ComposeRequest{Base: base, Addons: []string{"BadAddon"}, Graph: g, GraphDir: "."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no template")
}

func TestCompose_WithAddons_Success(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	p, err := Compose(ComposeRequest{Base: base, Addons: []string{"Addon"}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	// Parent 3 + addon 2 = 5 steps.
	require.Len(t, p.Execution.Steps, 5)
}

func TestCompose_UnknownAddon(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	_, err := Compose(ComposeRequest{Base: base, Addons: []string{"NonExistent"}, Graph: g, GraphDir: "."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown addon workflow")
}

func TestCompose_NotAnAddon(t *testing.T) {
	g := buildComposeTestGraph()
	// Add a non-addon workflow.
	g.Workflows = append(g.Workflows, graph.Workflow{
		Name:     "Regular",
		Template: "testdata/compose/parent.yaml",
	})

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	_, err := Compose(ComposeRequest{Base: base, Addons: []string{"Regular"}, Graph: g, GraphDir: "."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not an addon")
}

// --- Auto-chaining tests ---

func TestCompose_AutoChainSameAfterNode(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	// Both addons target "book" (default in test graph) — should be auto-chained.
	p, err := Compose(ComposeRequest{Base: base, Addons: []string{"Addon", "Addon2"}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	// Parent has 3 steps, addon1 has 2, addon2 has 2 = 7 total.
	require.Len(t, p.Execution.Steps, 7)

	// Find steps by ID.
	stepsByID := make(map[string]plan.Step)
	for _, s := range p.Execution.Steps {
		stepsByID[s.StepID()] = s
	}

	// First addon root (inc0_addon1) depends on "book".
	addon1Root := stepsByID["inc0_addon1"]
	assert.Contains(t, addon1Root.DependsOn, "book")

	// Second addon root (inc1_addon3) should depend on inc0_addon2
	// (last step of first addon) via auto-chaining.
	addon2Root := stepsByID["inc1_addon3"]
	assert.Contains(t, addon2Root.DependsOn, "inc0_addon2",
		"auto-chained addon should depend on last step of previous addon")
}

func TestCompose_DifferentAfterNodesNotChained(t *testing.T) {
	g := buildComposeTestGraph()
	setWorkflowAfter(g, "Addon2", graph.AfterSpec{"search"})

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	// Addon targets "book", Addon2 targets "search" — should NOT be auto-chained.
	p, err := Compose(ComposeRequest{Base: base, Addons: []string{"Addon", "Addon2"}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	require.Len(t, p.Execution.Steps, 7)

	// Find steps by ID.
	stepsByID := make(map[string]plan.Step)
	for _, s := range p.Execution.Steps {
		stepsByID[s.StepID()] = s
	}

	// First addon root depends on "book".
	addon1Root := stepsByID["inc0_addon1"]
	assert.Contains(t, addon1Root.DependsOn, "book")

	// Second addon root depends on "search" (different insertion point, no chaining).
	addon2Root := stepsByID["inc1_addon3"]
	assert.Contains(t, addon2Root.DependsOn, "search")
	assert.NotContains(t, addon2Root.DependsOn, "inc0_addon2",
		"addon with different after node should not be chained to first addon")
}

func TestCompose_ThreeAddonsChained(t *testing.T) {
	g := buildComposeTestGraph()
	// Add a third addon targeting "book".
	g.Workflows = append(g.Workflows, graph.Workflow{
		Name:     "Addon3",
		Kind:     "addon",
		Template: "testdata/compose/addon.yaml",
		After:    graph.AfterSpec{"book"},
	})

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	// Three addons sharing "book" — chain all three sequentially.
	p, err := Compose(ComposeRequest{Base: base, Addons: []string{"Addon", "Addon2", "Addon3"}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	// Parent 3 + addon1 2 + addon2 2 + addon3 2 = 9 steps.
	require.Len(t, p.Execution.Steps, 9)

	// Find steps by ID.
	stepsByID := make(map[string]plan.Step)
	for _, s := range p.Execution.Steps {
		stepsByID[s.StepID()] = s
	}

	// First addon root depends on "book".
	assert.Contains(t, stepsByID["inc0_addon1"].DependsOn, "book")

	// Second addon root depends on last step of first addon (inc0_addon2).
	assert.Contains(t, stepsByID["inc1_addon3"].DependsOn, "inc0_addon2")

	// Third addon root depends on last step of second addon (inc1_addon4).
	assert.Contains(t, stepsByID["inc2_addon1"].DependsOn, "inc1_addon4")
}

// --- Priority sorting tests ---

func TestCompose_PrioritySortsAddons(t *testing.T) {
	g := buildComposeTestGraph()
	setWorkflowPriority(g, "Addon", 20)
	setWorkflowPriority(g, "Addon2", 90)

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	// Addons given in wrong order: Addon2 (90) before Addon (20).
	// Priority sort should reorder Addon before Addon2.
	p, err := Compose(ComposeRequest{Base: base, Addons: []string{"Addon2", "Addon"}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	// Parent 3 + addon1 (priority 20) 2 + addon2 (priority 90) 2 = 7.
	require.Len(t, p.Execution.Steps, 7)

	// After priority sorting, the lower-priority addon (Addon, p=20) should be
	// processed first (inc0_), and the higher-priority addon (Addon2, p=90) second (inc1_).
	stepsByID := make(map[string]plan.Step)
	for _, s := range p.Execution.Steps {
		stepsByID[s.StepID()] = s
	}

	// Addon (priority 20) gets inc0_ prefix.
	_, hasAddon1 := stepsByID["inc0_addon1"]
	assert.True(t, hasAddon1, "lower priority addon should be processed first (inc0_)")

	// Addon2 (priority 90) gets inc1_ prefix.
	_, hasAddon3 := stepsByID["inc1_addon3"]
	assert.True(t, hasAddon3, "higher priority addon should be processed second (inc1_)")

	// Second addon root should be auto-chained to last step of first addon.
	assert.Contains(t, stepsByID["inc1_addon3"].DependsOn, "inc0_addon2")
}

func TestCompose_EqualPriorityPreservesOrder(t *testing.T) {
	g := buildComposeTestGraph()
	setWorkflowPriority(g, "Addon", 10)
	setWorkflowPriority(g, "Addon2", 10)

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	p, err := Compose(ComposeRequest{Base: base, Addons: []string{"Addon", "Addon2"}, Graph: g, GraphDir: "."})
	require.NoError(t, err)
	require.Len(t, p.Execution.Steps, 7)

	stepsByID := make(map[string]plan.Step)
	for _, s := range p.Execution.Steps {
		stepsByID[s.StepID()] = s
	}

	// Original order preserved: Addon first (inc0_), Addon2 second (inc1_).
	_, hasAddon1 := stepsByID["inc0_addon1"]
	assert.True(t, hasAddon1, "first addon should keep inc0_ prefix")

	_, hasAddon3 := stepsByID["inc1_addon3"]
	assert.True(t, hasAddon3, "second addon should keep inc1_ prefix")
}

func TestCompose_ZeroPriorityDefault(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	// No priority set (default 0) — should work exactly like before.
	p, err := Compose(ComposeRequest{Base: base, Addons: []string{"Addon", "Addon2"}, Graph: g, GraphDir: "."})
	require.NoError(t, err)
	require.Len(t, p.Execution.Steps, 7)

	stepsByID := make(map[string]plan.Step)
	for _, s := range p.Execution.Steps {
		stepsByID[s.StepID()] = s
	}

	// Both have priority 0, so original order is preserved.
	_, hasAddon1 := stepsByID["inc0_addon1"]
	assert.True(t, hasAddon1)

	_, hasAddon3 := stepsByID["inc1_addon3"]
	assert.True(t, hasAddon3)
}

// --- resolveAfterWire ---

func TestResolveAfterWire(t *testing.T) {
	tests := []struct {
		name         string
		wire         map[string]string
		matchedAfter string
		expected     map[string]string
	}{
		{
			name:         "nil wire",
			wire:         nil,
			matchedAfter: "book",
			expected:     nil,
		},
		{
			name:         "no $after refs",
			wire:         map[string]string{"workbenchId": "book.workbenchId"},
			matchedAfter: "priceOffer",
			expected:     map[string]string{"workbenchId": "book.workbenchId"},
		},
		{
			name:         "$after substitution",
			wire:         map[string]string{"offerListIdentifier": "$after.offerIdentifierValue"},
			matchedAfter: "priceOfferReference",
			expected:     map[string]string{"offerListIdentifier": "priceOfferReference.offerIdentifierValue"},
		},
		{
			name:         "mixed $after and regular",
			wire:         map[string]string{"a": "$after.x", "b": "explicit.y", "c": "MANUAL"},
			matchedAfter: "nodeA",
			expected:     map[string]string{"a": "nodeA.x", "b": "explicit.y", "c": "MANUAL"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveAfterWire(tt.wire, tt.matchedAfter)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- Multi-After composition tests ---

func TestCompose_MultiAfterFirstMatch(t *testing.T) {
	g := buildComposeTestGraph()
	setWorkflowAfter(g, "Addon", graph.AfterSpec{"search", "book"})

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	p, err := Compose(ComposeRequest{Base: base, Addons: []string{"Addon"}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	// Should splice after "search" (first match).
	require.Len(t, p.Execution.Steps, 5)
	assert.Equal(t, "search", p.Execution.Steps[0].Node)
	assert.Equal(t, "addon1", p.Execution.Steps[1].Node)
	assert.Contains(t, p.Execution.Steps[1].DependsOn, "search")
}

func TestCompose_MultiAfterSecondMatch(t *testing.T) {
	g := buildComposeTestGraph()
	setWorkflowAfter(g, "Addon", graph.AfterSpec{"nonexistent", "book"})

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	p, err := Compose(ComposeRequest{Base: base, Addons: []string{"Addon"}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	// Should splice after "book" (second match).
	require.Len(t, p.Execution.Steps, 5)
	assert.Equal(t, "book", p.Execution.Steps[1].Node)
	assert.Equal(t, "addon1", p.Execution.Steps[2].Node)
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "book")
}

func TestCompose_MultiAfterNoneFound(t *testing.T) {
	g := buildComposeTestGraph()
	setWorkflowAfter(g, "Addon", graph.AfterSpec{"missing1", "missing2"})

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	_, err := Compose(ComposeRequest{Base: base, Addons: []string{"Addon"}, Graph: g, GraphDir: "."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "found in base plan steps")
}

func TestCompose_AfterWireSubstitution(t *testing.T) {
	g := buildComposeTestGraph()
	// Add an offerIdentifierValue output to the book node for wire testing.
	g.Nodes["book"].Outputs = append(g.Nodes["book"].Outputs,
		graph.Output{Name: "offerIdentifierValue", Type: "string"})
	setWorkflowWire(g, "Addon", map[string]string{
		"workbenchId":  "$after.offerIdentifierValue",
		"specialInput": "MANUAL",
	})

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	p, err := Compose(ComposeRequest{Base: base, Addons: []string{"Addon"}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	addon1 := p.Execution.Steps[2]
	// $after.offerIdentifierValue resolved to book.offerIdentifierValue.
	assert.Equal(t, "book.offerIdentifierValue", addon1.Values["workbenchId"].From)
	// MANUAL should still clear the value.
	assert.Nil(t, addon1.Values["specialInput"].Default)
	assert.Empty(t, addon1.Values["specialInput"].From)
}

// --- Compose with slots tests ---

func TestCompose_Slots_BasicReplacement(t *testing.T) {
	g := buildSlotTestGraph()
	base := findSlotBaseWorkflow(g)

	p, err := Compose(ComposeRequest{Base: base, Choices: map[string]string{
		"trip-search": "OptionA",
		"payment":     "CashPayment",
	}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	// Base has: createWorkbench, [trip-search], addTraveler, [payment], finalStep
	// OptionA contributes: search, book (2 steps)
	// CashPayment contributes: addCashPayment (1 step)
	// Total: 1 + 2 + 1 + 1 + 1 = 6
	require.Len(t, p.Execution.Steps, 6)

	// Verify step order.
	assert.Equal(t, "createWorkbench", p.Execution.Steps[0].Node)
	assert.Equal(t, "search", p.Execution.Steps[1].Node)
	assert.Equal(t, "book", p.Execution.Steps[2].Node)
	assert.Equal(t, "addTraveler", p.Execution.Steps[3].Node)
	assert.Equal(t, "addCashPayment", p.Execution.Steps[4].Node)
	assert.Equal(t, "finalStep", p.Execution.Steps[5].Node)
}

func TestCompose_Slots_DependsOnRewriting(t *testing.T) {
	g := buildSlotTestGraph()
	base := findSlotBaseWorkflow(g)

	p, err := Compose(ComposeRequest{Base: base, Choices: map[string]string{
		"trip-search": "OptionA",
		"payment":     "CashPayment",
	}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	// addTraveler originally depends on "trip-search" slot → rewritten to "book" (last step of OptionA).
	addTraveler := p.Execution.Steps[3]
	assert.Equal(t, "addTraveler", addTraveler.Node)
	assert.Contains(t, addTraveler.DependsOn, "book", "trip-search slot dep should be rewritten to last option step")
	assert.NotContains(t, addTraveler.DependsOn, "trip-search", "slot name should be gone from dependsOn")

	// payment marker depended on trip-search. addCashPayment (its replacement)
	// should inherit that rewritten to "book".
	addCashPayment := p.Execution.Steps[4]
	assert.Equal(t, "addCashPayment", addCashPayment.Node)
	assert.Contains(t, addCashPayment.DependsOn, "book", "payment root should inherit rewritten trip-search dep")

	// finalStep depends on [addTraveler, payment]. "payment" should be rewritten to "addCashPayment".
	finalStep := p.Execution.Steps[5]
	assert.Equal(t, "finalStep", finalStep.Node)
	assert.Contains(t, finalStep.DependsOn, "addTraveler")
	assert.Contains(t, finalStep.DependsOn, "addCashPayment", "payment slot dep should be rewritten")
	assert.NotContains(t, finalStep.DependsOn, "payment")
}

func TestCompose_Slots_AutoWiring(t *testing.T) {
	g := buildSlotTestGraph()
	base := findSlotBaseWorkflow(g)

	p, err := Compose(ComposeRequest{Base: base, Choices: map[string]string{
		"trip-search": "OptionA",
		"payment":     "CashPayment",
	}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	// finalStep has amount: AUTOWIRE → should be wired to addCashPayment.amount
	// (addCashPayment produces "amount" output).
	finalStep := p.Execution.Steps[5]
	assert.Equal(t, "addCashPayment.amount", finalStep.Values["amount"].From,
		"AUTOWIRE should be resolved from slot option outputs")
	assert.Nil(t, finalStep.Values["amount"].Default, "AUTOWIRE default should be cleared")
}

func TestCompose_Slots_CrossSlotFromRef(t *testing.T) {
	g := buildSlotTestGraph()
	base := findSlotBaseWorkflow(g)

	p, err := Compose(ComposeRequest{Base: base, Choices: map[string]string{
		"trip-search": "CashPayment", // Intentionally wrong to use CashPayment as trip-search
		"payment":     "CashPayment",
	}, Graph: g, GraphDir: "."})
	// CashPayment has a from: createWorkbench.workbenchId — this is a cross-slot
	// ref to a base template step. ensureFromDeps should add createWorkbench
	// to the step's dependsOn.
	require.NoError(t, err)

	// The first CashPayment step replaces trip-search slot.
	cashStep := p.Execution.Steps[1]
	assert.Equal(t, "addCashPayment", cashStep.Node)
	assert.Contains(t, cashStep.DependsOn, "createWorkbench",
		"cross-slot from ref should add dependency via ensureFromDeps")
}

func TestCompose_Slots_OptionBMultiStep(t *testing.T) {
	g := buildSlotTestGraph()
	base := findSlotBaseWorkflow(g)

	p, err := Compose(ComposeRequest{Base: base, Choices: map[string]string{
		"trip-search": "OptionB",
		"payment":     "CashPayment",
	}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	// OptionB has 3 steps: search, searchLeg2, book
	// Total: createWorkbench + (search, searchLeg2, book) + addTraveler + addCashPayment + finalStep = 7
	require.Len(t, p.Execution.Steps, 7)

	assert.Equal(t, "createWorkbench", p.Execution.Steps[0].Node)
	assert.Equal(t, "search", p.Execution.Steps[1].Node)
	assert.Equal(t, "searchLeg2", p.Execution.Steps[2].Node)
	assert.Equal(t, "book", p.Execution.Steps[3].Node)
	assert.Equal(t, "addTraveler", p.Execution.Steps[4].Node)
	assert.Equal(t, "addCashPayment", p.Execution.Steps[5].Node)
	assert.Equal(t, "finalStep", p.Execution.Steps[6].Node)

	// addTraveler should depend on "book" (last step of OptionB, not "search").
	assert.Contains(t, p.Execution.Steps[4].DependsOn, "book")
}

func TestCompose_Slots_DefaultChoice(t *testing.T) {
	g := buildSlotTestGraph()
	base := findSlotBaseWorkflow(g)

	// Provide no choices — should use defaults.
	p, err := Compose(ComposeRequest{Base: base, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	// Default trip-search is OptionA (2 steps), default payment is CashPayment (1 step).
	require.Len(t, p.Execution.Steps, 6)
	assert.Equal(t, "search", p.Execution.Steps[1].Node)
}

func TestCompose_Slots_PartialChoices(t *testing.T) {
	g := buildSlotTestGraph()
	base := findSlotBaseWorkflow(g)

	// Only specify trip-search — payment should default to CashPayment.
	p, err := Compose(ComposeRequest{Base: base, Choices: map[string]string{
		"trip-search": "OptionB",
	}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	// OptionB (3 steps) + CashPayment (1 step) + createWorkbench + addTraveler + finalStep = 7
	require.Len(t, p.Execution.Steps, 7)
}

func TestCompose_Slots_UnknownOption(t *testing.T) {
	g := buildSlotTestGraph()
	base := findSlotBaseWorkflow(g)

	_, err := Compose(ComposeRequest{Base: base, Choices: map[string]string{
		"trip-search": "NonExistent",
	}, Graph: g, GraphDir: "."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in graph")
}

func TestCompose_Slots_NoDefaultNoChoice(t *testing.T) {
	g := buildSlotTestGraph()

	base := findSlotBaseWorkflow(g)
	base.Slots[0].Default = "" // trip-search has no default now

	_, err := Compose(ComposeRequest{Base: base, Graph: g, GraphDir: "."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no choice and no default")
}

func TestCompose_NoSlots(t *testing.T) {
	g := buildSlotTestGraph()

	// Use a workflow with no slots — should just load the template.
	noSlotWF := graph.Workflow{
		Name:     "NoSlots",
		Template: "testdata/compose/parent.yaml",
	}

	p, err := Compose(ComposeRequest{Base: noSlotWF, Graph: g, GraphDir: "."})
	require.NoError(t, err)
	require.Len(t, p.Execution.Steps, 3) // parent.yaml has 3 steps
}

func TestCompose_Slots_CleanupMerge(t *testing.T) {
	g := buildSlotTestGraph()
	base := findSlotBaseWorkflow(g)

	p, err := Compose(ComposeRequest{Base: base, Choices: map[string]string{
		"trip-search": "OptionA",
		"payment":     "CashPayment",
	}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	// Base has cleanup: commit (always). Options have no cleanup.
	require.Len(t, p.Execution.Cleanup, 1)
	assert.Equal(t, "commit", p.Execution.Cleanup[0].Node)
}

func TestCompose_SlotsAndAddons_Success(t *testing.T) {
	g := buildSlotTestGraph()
	base := findSlotBaseWorkflow(g)

	p, err := Compose(ComposeRequest{Base: base, Choices: map[string]string{
		"trip-search": "OptionA",
		"payment":     "CashPayment",
	}, Addons: []string{"Addon"}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	// 6 from slot composition + 2 from addon = 8
	require.Len(t, p.Execution.Steps, 8)

	// Addon steps should be prefixed with inc0_.
	stepIDs := make([]string, len(p.Execution.Steps))
	for i, s := range p.Execution.Steps {
		stepIDs[i] = s.StepID()
	}
	assert.Contains(t, stepIDs, "inc0_addon1")
	assert.Contains(t, stepIDs, "inc0_addon2")
}

func TestCompose_SlotsAndAddons_NoAddons(t *testing.T) {
	g := buildSlotTestGraph()
	base := findSlotBaseWorkflow(g)

	p, err := Compose(ComposeRequest{Base: base, Choices: map[string]string{
		"trip-search": "OptionA",
		"payment":     "CashPayment",
	}, Graph: g, GraphDir: "."})
	require.NoError(t, err)

	// Same as slots only — 6 steps.
	require.Len(t, p.Execution.Steps, 6)
}

func TestCompose_SlotsAndAddons_BadAddon(t *testing.T) {
	g := buildSlotTestGraph()
	base := findSlotBaseWorkflow(g)

	_, err := Compose(ComposeRequest{Base: base, Choices: map[string]string{
		"trip-search": "OptionA",
		"payment":     "CashPayment",
	}, Addons: []string{"NonExistent"}, Graph: g, GraphDir: "."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown addon")
}

// --- ensureFromDeps ---

func TestEnsureFromDeps_AddsFromRefDeps(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "createWorkbench"},
				{
					Node: "addOffer",
					Values: map[string]plan.StepValue{
						"workbenchId": {From: "createWorkbench.workbenchId"},
					},
				},
				{
					Node:      "commit",
					DependsOn: []string{"addOffer"},
					Values: map[string]plan.StepValue{
						"workbenchId": {From: "createWorkbench.workbenchId"},
					},
				},
			},
		},
	}

	ensureFromDeps(p, false)

	// addOffer should gain createWorkbench dep from from ref.
	assert.Contains(t, p.Execution.Steps[1].DependsOn, "createWorkbench")

	// commit should gain createWorkbench dep (not just addOffer).
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "createWorkbench")
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "addOffer")
}

func TestEnsureFromDeps_NoDuplicateDeps(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "a"},
				{
					Node:      "b",
					DependsOn: []string{"a"},
					Values: map[string]plan.StepValue{
						"input1": {From: "a.output1"},
						"input2": {From: "a.output2"},
					},
				},
			},
		},
	}

	ensureFromDeps(p, false)

	// "a" should appear only once in b's dependsOn.
	count := 0
	for _, dep := range p.Execution.Steps[1].DependsOn {
		if dep == "a" {
			count++
		}
	}
	assert.Equal(t, 1, count, "dep 'a' should appear exactly once")
}

func TestEnsureFromDeps_SkipsSelfRef(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "a",
					Values: map[string]plan.StepValue{
						"input": {From: "a.output"}, // self-ref (weird but shouldn't crash)
					},
				},
			},
		},
	}

	ensureFromDeps(p, false)
	assert.Empty(t, p.Execution.Steps[0].DependsOn, "self-ref should not add self to dependsOn")
}

// findSlotBaseWorkflow returns the slot base workflow from the test graph.
func findSlotBaseWorkflow(g *graph.Graph) graph.Workflow {
	for _, wf := range g.Workflows {
		if wf.Name == "SlotBase" {
			return wf
		}
	}
	panic("SlotBase workflow not found in test graph")
}

// buildSlotTestGraph creates a graph with slot-based workflows for testing.
func buildSlotTestGraph() *graph.Graph {
	g := buildComposeTestGraph()

	// Add nodes used by slot templates.
	g.Nodes["createWorkbench"] = &graph.Node{
		Name: "createWorkbench", Adapter: "createWorkbench",
		Outputs: []graph.Output{{Name: "workbenchId", Type: "string"}},
	}
	g.Nodes["addTraveler"] = &graph.Node{
		Name: "addTraveler", Adapter: "addTraveler",
		Inputs:  []graph.Input{{Name: "workbenchId", Type: "string"}},
		Outputs: []graph.Output{{Name: "travelerId", Type: "string"}},
	}
	g.Nodes["finalStep"] = &graph.Node{
		Name: "finalStep", Adapter: "finalStep",
		Inputs: []graph.Input{
			{Name: "workbenchId", Type: "string"},
			{Name: "amount", Type: "string"},
		},
		Outputs: []graph.Output{{Name: "locator", Type: "string"}},
	}
	g.Nodes["addCashPayment"] = &graph.Node{
		Name: "addCashPayment", Adapter: "addCashPayment",
		Inputs:  []graph.Input{{Name: "workbenchId", Type: "string"}},
		Outputs: []graph.Output{{Name: "amount", Type: "string"}},
	}
	g.Nodes["searchLeg2"] = &graph.Node{
		Name: "searchLeg2", Adapter: "searchLeg2",
		Inputs:  []graph.Input{{Name: "leg1Data", Type: "string"}},
		Outputs: []graph.Output{{Name: "results", Type: "string"}},
	}

	// Add slot workflows.
	g.Workflows = append(g.Workflows,
		graph.Workflow{
			Name:     "SlotBase",
			Template: "testdata/compose/slot_base.yaml",
			Slots: []graph.SlotDef{
				{
					Name:    "trip-search",
					Options: []string{"OptionA", "OptionB", "CashPayment"},
					Default: "OptionA",
				},
				{
					Name:    "payment",
					Options: []string{"CashPayment"},
					Default: "CashPayment",
				},
			},
		},
		graph.Workflow{
			Name:     "OptionA",
			Kind:     "slot",
			Template: "testdata/compose/slot_option_a.yaml",
		},
		graph.Workflow{
			Name:     "OptionB",
			Kind:     "slot",
			Template: "testdata/compose/slot_option_b.yaml",
		},
		graph.Workflow{
			Name:     "CashPayment",
			Kind:     "slot",
			Template: "testdata/compose/slot_payment_cash.yaml",
		},
	)

	return g
}

// --- Inject Tests ---

func TestCompose_Slots_InjectAppliesValue(t *testing.T) {
	g := buildSlotTestGraph()

	// Add a "passengers" input to the search node so inject can target it.
	searchNode := g.Nodes["search"]
	searchNode.Inputs = append(searchNode.Inputs, graph.Input{Name: "passengers", Type: "integer"})

	// Add an inject-bearing slot option and a traveler slot to the base workflow.
	g.Workflows = append(g.Workflows,
		graph.Workflow{
			Name:     "TwoTravelers",
			Kind:     "slot",
			Template: "testdata/compose/slot_option_a.yaml", // reuse — steps don't matter for inject test
			Inject:   map[string]any{"passengers": 2},
		},
	)

	// Add a traveler slot to SlotBase.
	for i, wf := range g.Workflows {
		if wf.Name == "SlotBase" {
			g.Workflows[i].Slots = append(g.Workflows[i].Slots, graph.SlotDef{
				Name:    "traveler",
				Options: []string{"OptionA", "TwoTravelers"},
				Default: "OptionA",
			})
			break
		}
	}

	// We need to add the traveler slot marker to the base template plan.
	// Since the test template is a file, we'll compose manually with a plan
	// that has the slot marker.
	basePlan := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "createWorkbench"},
				{Slot: "trip-search"},
				{Slot: "traveler", DependsOn: []string{"trip-search"}},
				{Node: "finalStep", DependsOn: []string{"traveler"}, Values: map[string]plan.StepValue{
					"workbenchId": {From: "createWorkbench.workbenchId"},
					"amount":      {Default: "AUTOWIRE"},
				}},
			},
		},
	}

	// Call applyInjectValues directly to test the inject mechanism.
	applyInjectValues(basePlan, map[string]any{"passengers": 2}, g)

	// search is not in the basePlan steps, so nothing should be injected.
	// Let's test with a plan that has the search step.
	planWithSearch := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "createWorkbench"},
				{Node: "search"},
				{Node: "addTraveler", Values: map[string]plan.StepValue{
					"workbenchId": {From: "createWorkbench.workbenchId"},
				}},
				{Node: "finalStep", Values: map[string]plan.StepValue{
					"workbenchId": {From: "createWorkbench.workbenchId"},
				}},
			},
		},
	}

	applyInjectValues(planWithSearch, map[string]any{"passengers": 2}, g)

	// search step should now have passengers=2
	searchStep := planWithSearch.Execution.Steps[1]
	assert.Equal(t, "search", searchStep.Node)
	require.NotNil(t, searchStep.Values)
	assert.Equal(t, 2, searchStep.Values["passengers"].Default)

	// addTraveler doesn't have "passengers" input, so no injection.
	travelerStep := planWithSearch.Execution.Steps[2]
	_, hasPassengers := travelerStep.Values["passengers"]
	assert.False(t, hasPassengers)
}

func TestApplyInjectValues_SkipsExistingValues(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name:   "search",
				Inputs: []graph.Input{{Name: "passengers", Type: "integer"}},
			},
		},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{
					"passengers": {Default: 5},
				}},
			},
		},
	}

	applyInjectValues(p, map[string]any{"passengers": 2}, g)

	// Should keep the existing value of 5, not overwrite with 2.
	assert.Equal(t, 5, p.Execution.Steps[0].Values["passengers"].Default)
}

func TestApplyInjectValues_SkipsFromWiring(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name:   "search",
				Inputs: []graph.Input{{Name: "passengers", Type: "integer"}},
			},
		},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{
					"passengers": {From: "config.passengers"},
				}},
			},
		},
	}

	applyInjectValues(p, map[string]any{"passengers": 2}, g)

	// Should keep the from wiring, not overwrite.
	assert.Equal(t, "config.passengers", p.Execution.Steps[0].Values["passengers"].From)
	assert.Nil(t, p.Execution.Steps[0].Values["passengers"].Default)
}

func TestApplyInjectValues_MultipleSteps(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"searchOutbound": {
				Name:   "searchOutbound",
				Inputs: []graph.Input{{Name: "passengers", Type: "integer"}, {Name: "origin", Type: "string"}},
			},
			"searchReturn": {
				Name:   "searchReturn",
				Inputs: []graph.Input{{Name: "passengers", Type: "integer"}, {Name: "origin", Type: "string"}},
			},
			"addTraveler": {
				Name:   "addTraveler",
				Inputs: []graph.Input{{Name: "name", Type: "string"}},
			},
		},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchOutbound"},
				{Node: "searchReturn"},
				{Node: "addTraveler"},
			},
		},
	}

	applyInjectValues(p, map[string]any{"passengers": 2}, g)

	// Both search steps get passengers=2.
	assert.Equal(t, 2, p.Execution.Steps[0].Values["passengers"].Default)
	assert.Equal(t, 2, p.Execution.Steps[1].Values["passengers"].Default)

	// addTraveler has no "passengers" input — no injection.
	_, has := p.Execution.Steps[2].Values["passengers"]
	assert.False(t, has)
}

// --- Test graph helpers ---

// setWorkflowWire modifies the Wire field of a named workflow in the graph.
func setWorkflowWire(g *graph.Graph, name string, wire map[string]string) {
	for i, wf := range g.Workflows {
		if wf.Name == name {
			g.Workflows[i].Wire = wire
			return
		}
	}
}

// setWorkflowAfter modifies the After field of a named workflow in the graph.
func setWorkflowAfter(g *graph.Graph, name string, after graph.AfterSpec) {
	for i, wf := range g.Workflows {
		if wf.Name == name {
			g.Workflows[i].After = after
			return
		}
	}
}

// setWorkflowPriority modifies the Priority field of a named workflow in the graph.
func setWorkflowPriority(g *graph.Graph, name string, priority int) {
	for i, wf := range g.Workflows {
		if wf.Name == name {
			g.Workflows[i].Priority = priority
			return
		}
	}
}

// buildComposeTestGraph creates a synthetic graph for composition tests.
func buildComposeTestGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{
				Name:     "Addon",
				Kind:     "addon",
				Template: "testdata/compose/addon.yaml",
				After:    graph.AfterSpec{"book"},
			},
			{
				Name:     "Addon2",
				Kind:     "addon",
				Template: "testdata/compose/addon2.yaml",
				After:    graph.AfterSpec{"book"},
			},
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
			"addon3": {
				Name: "addon3", Adapter: "addon3",
				Inputs:  []graph.Input{{Name: "workbenchId", Type: "string"}},
				Outputs: []graph.Output{{Name: "result3", Type: "string"}},
			},
			"addon4": {
				Name: "addon4", Adapter: "addon4",
				Inputs:  []graph.Input{{Name: "data", Type: "string"}},
				Outputs: []graph.Output{{Name: "result4", Type: "string"}},
			},
		},
	}
}
