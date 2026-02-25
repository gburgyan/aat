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

// --- ComposeWorkflowTemplate integration tests ---

func TestComposeWorkflowTemplate_NoAddons(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Simple",
		Template: "testdata/compose/parent.yaml",
	}

	p, err := ComposeWorkflowTemplate(base, nil, ".", g)
	require.NoError(t, err)
	require.Len(t, p.Execution.Steps, 3)
	assert.Equal(t, "search", p.Execution.Steps[0].Node)
}

func TestComposeWorkflowTemplate_WithAddon(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}
	addons := []graph.Workflow{
		{
			Name:     "Addon",
			Kind:     "addon",
			Template: "testdata/compose/addon.yaml",
			After:    graph.AfterSpec{"book"},
			Wire: map[string]string{
				"specialInput": "MANUAL",
			},
		},
	}

	p, err := ComposeWorkflowTemplate(base, addons, ".", g)
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

	base := graph.Workflow{
		Name: "NoTemplate",
	}

	_, err := ComposeWorkflowTemplate(base, nil, ".", g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no template")
}

func TestComposeWorkflowTemplate_AddonAfterNotInBase(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}
	addons := []graph.Workflow{
		{
			Name:     "Addon",
			Kind:     "addon",
			Template: "testdata/compose/addon.yaml",
			After:    graph.AfterSpec{"nonexistentNode"},
		},
	}

	_, err := ComposeWorkflowTemplate(base, addons, ".", g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "found in base plan steps")
}

func TestComposeWorkflowTemplate_AddonNoTemplate(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}
	addons := []graph.Workflow{
		{
			Name:  "BadAddon",
			Kind:  "addon",
			After: graph.AfterSpec{"book"},
			// No Template
		},
	}

	_, err := ComposeWorkflowTemplate(base, addons, ".", g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no template")
}

// --- ComposeWithAddons ---

func TestComposeWithAddons_Success(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	p, err := ComposeWithAddons(base, []string{"Addon"}, g, ".")
	require.NoError(t, err)

	// Parent 3 + addon 2 = 5 steps.
	require.Len(t, p.Execution.Steps, 5)
}

func TestComposeWithAddons_UnknownAddon(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	_, err := ComposeWithAddons(base, []string{"NonExistent"}, g, ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown addon workflow")
}

func TestComposeWithAddons_NotAnAddon(t *testing.T) {
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

	_, err := ComposeWithAddons(base, []string{"Regular"}, g, ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not an addon")
}

// --- Auto-chaining tests ---

func TestComposeWorkflowTemplate_AutoChainSameAfterNode(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	// Two addons that both target "book" — should be auto-chained.
	addons := []graph.Workflow{
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
	}

	p, err := ComposeWorkflowTemplate(base, addons, ".", g)
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
	// (last step of first addon) via auto-chaining. It may also have
	// additional deps from auto-wired from refs.
	addon2Root := stepsByID["inc1_addon3"]
	assert.Contains(t, addon2Root.DependsOn, "inc0_addon2",
		"auto-chained addon should depend on last step of previous addon")
}

func TestComposeWorkflowTemplate_DifferentAfterNodesNotChained(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	// Two addons targeting different nodes — should NOT be auto-chained.
	addons := []graph.Workflow{
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
			After:    graph.AfterSpec{"search"},
		},
	}

	p, err := ComposeWorkflowTemplate(base, addons, ".", g)
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

func TestComposeWorkflowTemplate_ThreeAddonsChained(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	// Three addons sharing "book" — chain all three sequentially.
	addons := []graph.Workflow{
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
		{
			Name:     "Addon3",
			Kind:     "addon",
			Template: "testdata/compose/addon.yaml",
			After:    graph.AfterSpec{"book"},
		},
	}

	p, err := ComposeWorkflowTemplate(base, addons, ".", g)
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

func TestComposeWorkflowTemplate_PrioritySortsAddons(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	// Addons given in wrong order: cancel (90) before seat (20).
	// Priority sort should reorder seat before cancel.
	addons := []graph.Workflow{
		{
			Name:     "Addon2",
			Kind:     "addon",
			Template: "testdata/compose/addon2.yaml",
			After:    graph.AfterSpec{"book"},
			Priority: 90,
		},
		{
			Name:     "Addon",
			Kind:     "addon",
			Template: "testdata/compose/addon.yaml",
			After:    graph.AfterSpec{"book"},
			Priority: 20,
		},
	}

	p, err := ComposeWorkflowTemplate(base, addons, ".", g)
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

func TestComposeWorkflowTemplate_EqualPriorityPreservesOrder(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	// Both addons have same priority — original order should be preserved.
	addons := []graph.Workflow{
		{
			Name:     "Addon",
			Kind:     "addon",
			Template: "testdata/compose/addon.yaml",
			After:    graph.AfterSpec{"book"},
			Priority: 10,
		},
		{
			Name:     "Addon2",
			Kind:     "addon",
			Template: "testdata/compose/addon2.yaml",
			After:    graph.AfterSpec{"book"},
			Priority: 10,
		},
	}

	p, err := ComposeWorkflowTemplate(base, addons, ".", g)
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

func TestComposeWorkflowTemplate_ZeroPriorityDefault(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	// No priority set (default 0) — should work exactly like before.
	addons := []graph.Workflow{
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
	}

	p, err := ComposeWorkflowTemplate(base, addons, ".", g)
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

func TestComposeWorkflowTemplate_DoesNotMutateCallerSlice(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}

	addons := []graph.Workflow{
		{
			Name:     "Addon2",
			Kind:     "addon",
			Template: "testdata/compose/addon2.yaml",
			After:    graph.AfterSpec{"book"},
			Priority: 90,
		},
		{
			Name:     "Addon",
			Kind:     "addon",
			Template: "testdata/compose/addon.yaml",
			After:    graph.AfterSpec{"book"},
			Priority: 20,
		},
	}

	// Save original order.
	originalFirst := addons[0].Name
	originalSecond := addons[1].Name

	_, err := ComposeWorkflowTemplate(base, addons, ".", g)
	require.NoError(t, err)

	// Caller's slice should not be reordered.
	assert.Equal(t, originalFirst, addons[0].Name)
	assert.Equal(t, originalSecond, addons[1].Name)
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

func TestComposeWorkflowTemplate_MultiAfterFirstMatch(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}
	// Addon with multi-after: [search, book] — search is first and present in base.
	addons := []graph.Workflow{
		{
			Name:     "Addon",
			Kind:     "addon",
			Template: "testdata/compose/addon.yaml",
			After:    graph.AfterSpec{"search", "book"},
		},
	}

	p, err := ComposeWorkflowTemplate(base, addons, ".", g)
	require.NoError(t, err)

	// Should splice after "search" (first match).
	require.Len(t, p.Execution.Steps, 5)
	assert.Equal(t, "search", p.Execution.Steps[0].Node)
	assert.Equal(t, "addon1", p.Execution.Steps[1].Node)
	assert.Contains(t, p.Execution.Steps[1].DependsOn, "search")
}

func TestComposeWorkflowTemplate_MultiAfterSecondMatch(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}
	// Addon with multi-after: [nonexistent, book] — only book is present.
	addons := []graph.Workflow{
		{
			Name:     "Addon",
			Kind:     "addon",
			Template: "testdata/compose/addon.yaml",
			After:    graph.AfterSpec{"nonexistent", "book"},
		},
	}

	p, err := ComposeWorkflowTemplate(base, addons, ".", g)
	require.NoError(t, err)

	// Should splice after "book" (second match).
	require.Len(t, p.Execution.Steps, 5)
	assert.Equal(t, "book", p.Execution.Steps[1].Node)
	assert.Equal(t, "addon1", p.Execution.Steps[2].Node)
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "book")
}

func TestComposeWorkflowTemplate_MultiAfterNoneFound(t *testing.T) {
	g := buildComposeTestGraph()

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}
	addons := []graph.Workflow{
		{
			Name:     "Addon",
			Kind:     "addon",
			Template: "testdata/compose/addon.yaml",
			After:    graph.AfterSpec{"missing1", "missing2"},
		},
	}

	_, err := ComposeWorkflowTemplate(base, addons, ".", g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "found in base plan steps")
}

func TestComposeWorkflowTemplate_AfterWireSubstitution(t *testing.T) {
	g := buildComposeTestGraph()
	// Add an offerIdentifierValue output to the book node for wire testing.
	g.Nodes["book"].Outputs = append(g.Nodes["book"].Outputs,
		graph.Output{Name: "offerIdentifierValue", Type: "string"})

	base := graph.Workflow{
		Name:     "Base",
		Template: "testdata/compose/parent.yaml",
	}
	addons := []graph.Workflow{
		{
			Name:     "Addon",
			Kind:     "addon",
			Template: "testdata/compose/addon.yaml",
			After:    graph.AfterSpec{"book"},
			Wire: map[string]string{
				"workbenchId":  "$after.offerIdentifierValue",
				"specialInput": "MANUAL",
			},
		},
	}

	p, err := ComposeWorkflowTemplate(base, addons, ".", g)
	require.NoError(t, err)

	addon1 := p.Execution.Steps[2]
	// $after.offerIdentifierValue resolved to book.offerIdentifierValue.
	assert.Equal(t, "book.offerIdentifierValue", addon1.Values["workbenchId"].From)
	// MANUAL should still clear the value.
	assert.Nil(t, addon1.Values["specialInput"].Default)
	assert.Empty(t, addon1.Values["specialInput"].From)
}

// --- ComposeWithSlots tests ---

func TestComposeWithSlots_BasicReplacement(t *testing.T) {
	g := buildSlotTestGraph()

	base := findSlotBaseWorkflow(g)

	p, err := ComposeWithSlots(base, map[string]string{
		"trip-search": "OptionA",
		"payment":     "CashPayment",
	}, g, ".")
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

func TestComposeWithSlots_DependsOnRewriting(t *testing.T) {
	g := buildSlotTestGraph()

	base := findSlotBaseWorkflow(g)

	p, err := ComposeWithSlots(base, map[string]string{
		"trip-search": "OptionA",
		"payment":     "CashPayment",
	}, g, ".")
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

func TestComposeWithSlots_AutoWiring(t *testing.T) {
	g := buildSlotTestGraph()

	base := findSlotBaseWorkflow(g)

	p, err := ComposeWithSlots(base, map[string]string{
		"trip-search": "OptionA",
		"payment":     "CashPayment",
	}, g, ".")
	require.NoError(t, err)

	// finalStep has amount: AUTOWIRE → should be wired to addCashPayment.amount
	// (addCashPayment produces "amount" output).
	finalStep := p.Execution.Steps[5]
	assert.Equal(t, "addCashPayment.amount", finalStep.Values["amount"].From,
		"AUTOWIRE should be resolved from slot option outputs")
	assert.Nil(t, finalStep.Values["amount"].Default, "AUTOWIRE default should be cleared")
}

func TestComposeWithSlots_CrossSlotFromRef(t *testing.T) {
	g := buildSlotTestGraph()

	base := findSlotBaseWorkflow(g)

	p, err := ComposeWithSlots(base, map[string]string{
		"trip-search": "CashPayment", // Intentionally wrong to use CashPayment as trip-search
		"payment":     "CashPayment",
	}, g, ".")
	// CashPayment has a from: createWorkbench.workbenchId — this is a cross-slot
	// ref to a base template step. fixFromDependencies should add createWorkbench
	// to the step's dependsOn.
	require.NoError(t, err)

	// The first CashPayment step replaces trip-search slot.
	cashStep := p.Execution.Steps[1]
	assert.Equal(t, "addCashPayment", cashStep.Node)
	assert.Contains(t, cashStep.DependsOn, "createWorkbench",
		"cross-slot from ref should add dependency via fixFromDependencies")
}

func TestComposeWithSlots_OptionBMultiStep(t *testing.T) {
	g := buildSlotTestGraph()

	base := findSlotBaseWorkflow(g)

	p, err := ComposeWithSlots(base, map[string]string{
		"trip-search": "OptionB",
		"payment":     "CashPayment",
	}, g, ".")
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

func TestComposeWithSlots_DefaultChoice(t *testing.T) {
	g := buildSlotTestGraph()

	base := findSlotBaseWorkflow(g)

	// Provide no choices — should use defaults.
	p, err := ComposeWithSlots(base, nil, g, ".")
	require.NoError(t, err)

	// Default trip-search is OptionA (2 steps), default payment is CashPayment (1 step).
	require.Len(t, p.Execution.Steps, 6)
	assert.Equal(t, "search", p.Execution.Steps[1].Node)
}

func TestComposeWithSlots_PartialChoices(t *testing.T) {
	g := buildSlotTestGraph()

	base := findSlotBaseWorkflow(g)

	// Only specify trip-search — payment should default to CashPayment.
	p, err := ComposeWithSlots(base, map[string]string{
		"trip-search": "OptionB",
	}, g, ".")
	require.NoError(t, err)

	// OptionB (3 steps) + CashPayment (1 step) + createWorkbench + addTraveler + finalStep = 7
	require.Len(t, p.Execution.Steps, 7)
}

func TestComposeWithSlots_UnknownOption(t *testing.T) {
	g := buildSlotTestGraph()

	base := findSlotBaseWorkflow(g)

	_, err := ComposeWithSlots(base, map[string]string{
		"trip-search": "NonExistent",
	}, g, ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in graph")
}

func TestComposeWithSlots_NoDefaultNoChoice(t *testing.T) {
	g := buildSlotTestGraph()

	// Add a slot with no default.
	base := findSlotBaseWorkflow(g)
	base.Slots = append(base.Slots, graph.SlotDef{
		Name:    "extra",
		Options: []string{"OptionA"},
		// No Default
	})

	// Modify base template to include the slot marker. Instead of modifying the
	// file, we skip the extra slot — ComposeWithSlots will fail at "marker not found"
	// which is a different error. Let's test the "no choice and no default" by
	// clearing the default of an existing slot.
	base.Slots[0].Default = "" // trip-search has no default now

	_, err := ComposeWithSlots(base, nil, g, ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no choice and no default")
}

func TestComposeWithSlots_NoSlots(t *testing.T) {
	g := buildSlotTestGraph()

	// Use a workflow with no slots — should just load the template.
	noSlotWF := graph.Workflow{
		Name:     "NoSlots",
		Template: "testdata/compose/parent.yaml",
	}

	p, err := ComposeWithSlots(noSlotWF, nil, g, ".")
	require.NoError(t, err)
	require.Len(t, p.Execution.Steps, 3) // parent.yaml has 3 steps
}

func TestComposeWithSlots_CleanupMerge(t *testing.T) {
	g := buildSlotTestGraph()

	base := findSlotBaseWorkflow(g)

	p, err := ComposeWithSlots(base, map[string]string{
		"trip-search": "OptionA",
		"payment":     "CashPayment",
	}, g, ".")
	require.NoError(t, err)

	// Base has cleanup: commit (always). Options have no cleanup.
	require.Len(t, p.Execution.Cleanup, 1)
	assert.Equal(t, "commit", p.Execution.Cleanup[0].Node)
}

func TestComposeWithSlotsAndAddons_Success(t *testing.T) {
	g := buildSlotTestGraph()

	base := findSlotBaseWorkflow(g)

	p, err := ComposeWithSlotsAndAddons(base, map[string]string{
		"trip-search": "OptionA",
		"payment":     "CashPayment",
	}, []string{"Addon"}, g, ".")
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

func TestComposeWithSlotsAndAddons_NoAddons(t *testing.T) {
	g := buildSlotTestGraph()

	base := findSlotBaseWorkflow(g)

	p, err := ComposeWithSlotsAndAddons(base, map[string]string{
		"trip-search": "OptionA",
		"payment":     "CashPayment",
	}, nil, g, ".")
	require.NoError(t, err)

	// Same as ComposeWithSlots — 6 steps.
	require.Len(t, p.Execution.Steps, 6)
}

func TestComposeWithSlotsAndAddons_BadAddon(t *testing.T) {
	g := buildSlotTestGraph()

	base := findSlotBaseWorkflow(g)

	_, err := ComposeWithSlotsAndAddons(base, map[string]string{
		"trip-search": "OptionA",
		"payment":     "CashPayment",
	}, []string{"NonExistent"}, g, ".")
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

	ensureFromDeps(p)

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

	ensureFromDeps(p)

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

	ensureFromDeps(p)
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
