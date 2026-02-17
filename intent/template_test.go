package intent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedNow() time.Time {
	return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
}

// --- FindWorkflowTemplate ---

func TestFindWorkflowTemplate_Match(t *testing.T) {
	g := &graph.Graph{
		Workflows: []graph.Workflow{
			{Name: "Booking", Template: "plans/booking.yaml"},
			{Name: "Search Only"},
		},
	}

	path, found := FindWorkflowTemplate(g, "Booking")
	assert.True(t, found)
	assert.Equal(t, "plans/booking.yaml", path)
}

func TestFindWorkflowTemplate_CaseInsensitive(t *testing.T) {
	g := &graph.Graph{
		Workflows: []graph.Workflow{
			{Name: "Full-Payload Booking", Template: "plans/booking.yaml"},
		},
	}

	path, found := FindWorkflowTemplate(g, "full-payload booking")
	assert.True(t, found)
	assert.Equal(t, "plans/booking.yaml", path)
}

func TestFindWorkflowTemplate_NoTemplate(t *testing.T) {
	g := &graph.Graph{
		Workflows: []graph.Workflow{
			{Name: "Search Only"},
		},
	}

	_, found := FindWorkflowTemplate(g, "Search Only")
	assert.False(t, found)
}

func TestFindWorkflowTemplate_NoMatch(t *testing.T) {
	g := &graph.Graph{
		Workflows: []graph.Workflow{
			{Name: "Booking", Template: "plans/booking.yaml"},
		},
	}

	_, found := FindWorkflowTemplate(g, "Unknown Workflow")
	assert.False(t, found)
}

// --- LoadWorkflowTemplate ---

func TestLoadWorkflowTemplate_Success(t *testing.T) {
	// Use the existing travelport roundtrip-journey.yaml as a real template.
	g, err := graph.ParseFile("../travelport/graph.yaml")
	require.NoError(t, err)

	p, err := LoadWorkflowTemplate("plans/roundtrip-journey.yaml", "../travelport", g)
	require.NoError(t, err)
	require.NotNil(t, p)

	// Should have steps
	assert.NotEmpty(t, p.Execution.Steps)
	// Should have cleanup
	assert.NotEmpty(t, p.Execution.Cleanup)
}

func TestLoadWorkflowTemplate_AbsolutePath(t *testing.T) {
	g, err := graph.ParseFile("../travelport/graph.yaml")
	require.NoError(t, err)

	absPath, err := filepath.Abs("../travelport/plans/roundtrip-journey.yaml")
	require.NoError(t, err)

	p, err := LoadWorkflowTemplate(absPath, "/nonexistent", g)
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestLoadWorkflowTemplate_MissingFile(t *testing.T) {
	g := &graph.Graph{Nodes: map[string]*graph.Node{}}

	_, err := LoadWorkflowTemplate("nonexistent.yaml", ".", g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading workflow template")
}

func TestLoadWorkflowTemplate_UnknownNode(t *testing.T) {
	// Create a temp plan file that references a node not in the graph.
	dir := t.TempDir()
	planContent := `
execution:
  steps:
    - node: unknownNode
      values:
        foo: bar
`
	planPath := filepath.Join(dir, "bad-template.yaml")
	require.NoError(t, os.WriteFile(planPath, []byte(planContent), 0o644))

	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {Name: "search", Description: "Search"},
		},
	}

	_, err := LoadWorkflowTemplate("bad-template.yaml", dir, g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown node")
	assert.Contains(t, err.Error(), "unknownNode")
}

func TestLoadWorkflowTemplate_UnknownCleanupNode(t *testing.T) {
	dir := t.TempDir()
	planContent := `
execution:
  steps:
    - node: search
      values:
        query: test
  cleanup:
    - node: badCleanup
      runOn: always
`
	planPath := filepath.Join(dir, "bad-cleanup.yaml")
	require.NoError(t, os.WriteFile(planPath, []byte(planContent), 0o644))

	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {Name: "search", Description: "Search"},
		},
	}

	_, err := LoadWorkflowTemplate("bad-cleanup.yaml", dir, g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cleanup references unknown node")
}

// --- ExpandMultiplicity ---

func TestExpandMultiplicity_NoOp(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{"q": {Default: "test"}}},
				{Node: "process", DependsOn: []string{"search"}},
			},
		},
	}

	ExpandMultiplicity(p, nil)
	assert.Len(t, p.Execution.Steps, 2)

	ExpandMultiplicity(p, map[string]int{})
	assert.Len(t, p.Execution.Steps, 2)

	ExpandMultiplicity(p, map[string]int{"search": 1})
	assert.Len(t, p.Execution.Steps, 2)
}

func TestExpandMultiplicity_SingleExpansion(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "createWorkbench"},
				{
					Node:      "addTraveler",
					DependsOn: []string{"createWorkbench"},
					Values: map[string]plan.StepValue{
						"workbenchId": {From: "createWorkbench.workbenchId"},
						"givenName":   {Default: "Test"},
						"surname":     {Default: "Traveler"},
					},
				},
				{Node: "commit", DependsOn: []string{"addTraveler"}},
			},
		},
	}

	ExpandMultiplicity(p, map[string]int{"addTraveler": 3})

	// Should now have 5 steps: createWorkbench, addTraveler_1, addTraveler_2, addTraveler_3, commit
	assert.Len(t, p.Execution.Steps, 5)

	assert.Equal(t, "createWorkbench", p.Execution.Steps[0].Node)
	assert.Equal(t, "addTraveler_1", p.Execution.Steps[1].StepID())
	assert.Equal(t, "addTraveler_2", p.Execution.Steps[2].StepID())
	assert.Equal(t, "addTraveler_3", p.Execution.Steps[3].StepID())
	assert.Equal(t, "commit", p.Execution.Steps[4].Node)
}

func TestExpandMultiplicity_DependsOnRewrite(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "setup"},
				{
					Node:      "addTraveler",
					DependsOn: []string{"setup"},
					Values:    map[string]plan.StepValue{"name": {Default: "Alice"}},
				},
				{Node: "commit", DependsOn: []string{"addTraveler"}},
			},
		},
	}

	ExpandMultiplicity(p, map[string]int{"addTraveler": 2})

	// commit should depend on addTraveler_2 (the last copy)
	commitStep := p.Execution.Steps[len(p.Execution.Steps)-1]
	assert.Equal(t, "commit", commitStep.Node)
	assert.Contains(t, commitStep.DependsOn, "addTraveler_2")

	// First copy inherits original deps
	assert.Contains(t, p.Execution.Steps[1].DependsOn, "setup")

	// Second copy depends on first
	assert.Contains(t, p.Execution.Steps[2].DependsOn, "addTraveler_1")
}

func TestExpandMultiplicity_FromRefRewrite(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "addTraveler",
					Values: map[string]plan.StepValue{
						"name": {Default: "Alice"},
					},
				},
				{
					Node:      "downstream",
					DependsOn: []string{"addTraveler"},
					Values: map[string]plan.StepValue{
						"travelerId": {From: "addTraveler.travelerId"},
					},
				},
			},
		},
	}

	ExpandMultiplicity(p, map[string]int{"addTraveler": 2})

	// Downstream from ref should point to last copy
	downstream := p.Execution.Steps[len(p.Execution.Steps)-1]
	assert.Equal(t, "addTraveler_2.travelerId", downstream.Values["travelerId"].From)
}

func TestExpandMultiplicity_ClearsLiterals(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "addTraveler",
					Values: map[string]plan.StepValue{
						"workbenchId": {From: "wb.workbenchId"},
						"givenName":   {Default: "Test"},
						"surname":     {Default: "Traveler"},
					},
				},
			},
		},
	}

	ExpandMultiplicity(p, map[string]int{"addTraveler": 2})

	// First copy keeps literals
	first := p.Execution.Steps[0]
	assert.Equal(t, "Test", first.Values["givenName"].Default)
	assert.Equal(t, "Traveler", first.Values["surname"].Default)

	// Second copy has literals cleared (LLM fills them)
	second := p.Execution.Steps[1]
	assert.Nil(t, second.Values["givenName"].Default)
	assert.Nil(t, second.Values["surname"].Default)

	// But structural wiring is preserved
	assert.Equal(t, "wb.workbenchId", second.Values["workbenchId"].From)
}

func TestExpandMultiplicity_PreservesWiring(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "addTraveler",
					Values: map[string]plan.StepValue{
						"workbenchId": {From: "wb.workbenchId"},
						"id":         {FromSelection: "sel.id"},
					},
					Selections: map[string]plan.StepSelection{
						"sel": {From: "search.items", Strategy: "first"},
					},
				},
			},
		},
	}

	ExpandMultiplicity(p, map[string]int{"addTraveler": 2})

	// Both copies should have the structural wiring preserved
	for _, step := range p.Execution.Steps {
		assert.Equal(t, "wb.workbenchId", step.Values["workbenchId"].From)
		assert.Equal(t, "sel.id", step.Values["id"].FromSelection)
		assert.NotEmpty(t, step.Selections)
	}
}

func TestExpandMultiplicity_MultiNode(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "addTraveler", Values: map[string]plan.StepValue{"name": {Default: "A"}}},
				{Node: "addBag", Values: map[string]plan.StepValue{"type": {Default: "checked"}}},
			},
		},
	}

	ExpandMultiplicity(p, map[string]int{"addTraveler": 2, "addBag": 3})

	// Should have 2 + 3 = 5 steps
	assert.Len(t, p.Execution.Steps, 5)

	ids := make([]string, len(p.Execution.Steps))
	for i, s := range p.Execution.Steps {
		ids[i] = s.StepID()
	}
	assert.Contains(t, ids, "addTraveler_1")
	assert.Contains(t, ids, "addTraveler_2")
	assert.Contains(t, ids, "addBag_1")
	assert.Contains(t, ids, "addBag_2")
	assert.Contains(t, ids, "addBag_3")
}

// --- UnfedInputsFromTemplate ---

func TestUnfedInputsFromTemplate_AllWired(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "destination", Type: "string"},
				},
			},
		},
	}

	t.Run("from refs are wired", func(t *testing.T) {
		p := &plan.Plan{
			Execution: plan.Execution{
				Steps: []plan.Step{
					{
						Node: "search",
						Values: map[string]plan.StepValue{
							"origin":      {From: "other.output"},
							"destination": {FromSelection: "sel.field"},
						},
					},
				},
			},
		}
		unfed := UnfedInputsFromTemplate(p, g)
		assert.Empty(t, unfed)
	})

	t.Run("literal defaults are overrideable", func(t *testing.T) {
		p := &plan.Plan{
			Execution: plan.Execution{
				Steps: []plan.Step{
					{
						Node: "search",
						Values: map[string]plan.StepValue{
							"origin":      {Default: "DEN"},
							"destination": {Default: "SFO"},
						},
					},
				},
			},
		}
		unfed := UnfedInputsFromTemplate(p, g)
		assert.Len(t, unfed, 2, "literal defaults should be overrideable")
		assert.Contains(t, unfed, "search.origin (string)")
		assert.Contains(t, unfed, "search.destination (string)")
	})
}

func TestUnfedInputsFromTemplate_MissingValues(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "destination", Type: "string"},
					{Name: "date", Type: "date"},
				},
			},
		},
	}
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "search",
					Values: map[string]plan.StepValue{
						"origin": {Default: "DEN"},
						// destination and date are missing
					},
				},
			},
		},
	}

	unfed := UnfedInputsFromTemplate(p, g)
	// All three are unfed: origin has a literal default (overrideable), destination and date have nothing.
	assert.Len(t, unfed, 3)
	assert.Contains(t, unfed, "search.origin (string)")
	assert.Contains(t, unfed, "search.destination (string)")
	assert.Contains(t, unfed, "search.date (date)")
}

func TestUnfedInputsFromTemplate_OptionalSkipped(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "cabin", Type: "string", Optional: true},
				},
			},
		},
	}
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "search",
					Values: map[string]plan.StepValue{
						"origin": {Default: "DEN"},
					},
				},
			},
		},
	}

	unfed := UnfedInputsFromTemplate(p, g)
	// origin has a literal default (overrideable), cabin is optional (fed)
	assert.Len(t, unfed, 1)
	assert.Contains(t, unfed, "search.origin (string)")
}

func TestUnfedInputsFromTemplate_GraphDefaultSkipped(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "passengers", Type: "integer", Default: 1},
				},
			},
		},
	}
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "search",
					Values: map[string]plan.StepValue{
						"origin": {Default: "DEN"},
					},
				},
			},
		},
	}

	unfed := UnfedInputsFromTemplate(p, g)
	// origin has a literal default (overrideable), passengers has a graph-level default (fed)
	assert.Len(t, unfed, 1)
	assert.Contains(t, unfed, "search.origin (string)")
}

func TestUnfedInputsFromTemplate_FromWired(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"process": {
				Name: "process",
				Inputs: []graph.Input{
					{Name: "id", Type: "string"},
				},
			},
		},
	}
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "process",
					Values: map[string]plan.StepValue{
						"id": {From: "search.resultId"},
					},
				},
			},
		},
	}

	unfed := UnfedInputsFromTemplate(p, g)
	assert.Empty(t, unfed)
}

func TestUnfedInputsFromTemplate_UsesStepID(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"addTraveler": {
				Name: "addTraveler",
				Inputs: []graph.Input{
					{Name: "name", Type: "string"},
				},
			},
		},
	}
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					ID:   "addTraveler_1",
					Node: "addTraveler",
					// name is missing
				},
			},
		},
	}

	unfed := UnfedInputsFromTemplate(p, g)
	require.Len(t, unfed, 1)
	assert.Contains(t, unfed[0], "addTraveler_1.name")
}

// --- MergeLLMValuesWithIDs ---

func TestMergeLLMValuesWithIDs_MatchesByStepID(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					ID:   "traveler_1",
					Node: "addTraveler",
					Values: map[string]plan.StepValue{
						"workbenchId": {From: "wb.id"},
					},
				},
				{
					ID:   "traveler_2",
					Node: "addTraveler",
					Values: map[string]plan.StepValue{
						"workbenchId": {From: "wb.id"},
					},
				},
			},
		},
	}

	llmPlan := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					ID:          "traveler_1",
					Node:        "addTraveler",
					Description: "First traveler",
					Values: map[string]plan.StepValue{
						"givenName": {Default: "Alice"},
						"surname":   {Default: "Smith"},
					},
				},
				{
					ID:          "traveler_2",
					Node:        "addTraveler",
					Description: "Second traveler",
					Values: map[string]plan.StepValue{
						"givenName": {Default: "Bob"},
						"surname":   {Default: "Jones"},
					},
				},
			},
		},
	}

	MergeLLMValuesWithIDs(skeleton, llmPlan, nil)

	// Both steps should have descriptions
	assert.Equal(t, "First traveler", skeleton.Execution.Steps[0].Description)
	assert.Equal(t, "Second traveler", skeleton.Execution.Steps[1].Description)

	// First step should have Alice
	assert.Equal(t, "Alice", skeleton.Execution.Steps[0].Values["givenName"].Default)
	// Second step should have Bob
	assert.Equal(t, "Bob", skeleton.Execution.Steps[1].Values["givenName"].Default)

	// Structural wiring should be preserved
	assert.Equal(t, "wb.id", skeleton.Execution.Steps[0].Values["workbenchId"].From)
	assert.Equal(t, "wb.id", skeleton.Execution.Steps[1].Values["workbenchId"].From)
}

func TestMergeLLMValuesWithIDs_NoIDFallsBackToNode(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node:   "search",
					Values: map[string]plan.StepValue{},
				},
			},
		},
	}

	llmPlan := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node:        "search",
					Description: "Search for flights",
					Values: map[string]plan.StepValue{
						"origin": {Default: "DEN"},
					},
				},
			},
		},
	}

	MergeLLMValuesWithIDs(skeleton, llmPlan, nil)

	assert.Equal(t, "Search for flights", skeleton.Execution.Steps[0].Description)
	assert.Equal(t, "DEN", skeleton.Execution.Steps[0].Values["origin"].Default)
}

func TestMergeLLMValuesWithIDs_RejectsHallucinatedLiterals(t *testing.T) {
	// Skeleton has addPayment with "from" wiring for some fields.
	// The LLM hallucinates a literal for "fopIdentifierValue" which is NOT
	// in the skeleton (it will be auto-wired from a graph edge at runtime).
	// With an unfed set that doesn't include it, the hallucinated value
	// should be rejected.
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "addPayment",
					Values: map[string]plan.StepValue{
						"workbenchId": {From: "createWorkbench.workbenchId"},
						"amount":      {From: "priceOffer.totalPrice"},
					},
				},
			},
		},
	}

	llmPlan := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "addPayment",
					Values: map[string]plan.StepValue{
						"fopIdentifierValue": {Default: "CASH12345"},  // hallucinated
						"offerId":            {Default: "OFFER67890"}, // hallucinated
					},
				},
			},
		},
	}

	// Unfed set does NOT include addPayment.fopIdentifierValue or addPayment.offerId.
	unfed := map[string]bool{}

	MergeLLMValuesWithIDs(skeleton, llmPlan, unfed)

	// Hallucinated values should NOT be added.
	_, hasFop := skeleton.Execution.Steps[0].Values["fopIdentifierValue"]
	_, hasOffer := skeleton.Execution.Steps[0].Values["offerId"]
	assert.False(t, hasFop, "fopIdentifierValue should not be added")
	assert.False(t, hasOffer, "offerId should not be added")

	// Existing wiring should be preserved.
	assert.Equal(t, "createWorkbench.workbenchId", skeleton.Execution.Steps[0].Values["workbenchId"].From)
}

func TestMergeLLMValuesWithIDs_AcceptsUnfedLiterals(t *testing.T) {
	// Skeleton has searchFlights with no values. The LLM provides literals
	// for origin and destination which ARE in the unfed set.
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
						"extraField":  {Default: "hallucinated"}, // not unfed
					},
				},
			},
		},
	}

	unfed := map[string]bool{
		"searchFlights.origin":      true,
		"searchFlights.destination": true,
	}

	MergeLLMValuesWithIDs(skeleton, llmPlan, unfed)

	assert.Equal(t, "DEN", skeleton.Execution.Steps[0].Values["origin"].Default)
	assert.Equal(t, "SFO", skeleton.Execution.Steps[0].Values["destination"].Default)
	_, hasExtra := skeleton.Execution.Steps[0].Values["extraField"]
	assert.False(t, hasExtra, "hallucinated extraField should not be added")
}

func TestMergeLLMValuesWithIDs_NilUnfedAcceptsAll(t *testing.T) {
	// When unfed is nil (legacy behavior), all new values are accepted.
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node:   "search",
					Values: map[string]plan.StepValue{},
				},
			},
		},
	}

	llmPlan := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "search",
					Values: map[string]plan.StepValue{
						"origin": {Default: "DEN"},
					},
				},
			},
		},
	}

	MergeLLMValuesWithIDs(skeleton, llmPlan, nil)

	assert.Equal(t, "DEN", skeleton.Execution.Steps[0].Values["origin"].Default)
}

// --- FormatGraph [template] marker ---

func TestFormatGraph_WorkflowTemplateMarker(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{Name: "Booking", Template: "plans/booking.yaml", Description: "Full booking"},
			{Name: "Search Only", Description: "Just search"},
		},
		Nodes: map[string]*graph.Node{},
	}

	result := FormatGraph(g)
	assert.Contains(t, result, "**Booking** [template]: Full booking")
	assert.Contains(t, result, "**Search Only**: Just search")
	assert.NotContains(t, result, "Search Only** [template]")
}

// --- buildWorkflowSelectionPrompt tests ---

func TestBuildWorkflowSelectionPrompt_WithWorkflows(t *testing.T) {
	g := &graph.Graph{
		Workflows: []graph.Workflow{
			{Name: "Booking", Template: "plans/booking.yaml", Description: "Full booking flow"},
			{Name: "Ancillary", Kind: "addon", Template: "plans/ancillary.yaml", After: "addTraveler", Description: "Add ancillary services"},
		},
	}

	system, _ := buildWorkflowSelectionPrompt("graph context", "book a flight", g, fixedNow())
	assert.Contains(t, system, "workflow")
	assert.Contains(t, system, "repetitions")
	assert.Contains(t, system, "addons")
	assert.Contains(t, system, "Booking")
	assert.Contains(t, system, "Ancillary")
	assert.Contains(t, system, "splices after: addTraveler")
}

func TestBuildWorkflowSelectionPrompt_NoTemplateWorkflowsSkipped(t *testing.T) {
	g := &graph.Graph{
		Workflows: []graph.Workflow{
			{Name: "Booking"}, // no template — should be skipped
		},
	}

	system, _ := buildWorkflowSelectionPrompt("graph context", "book a flight", g, fixedNow())
	assert.NotContains(t, system, "Booking")
}

func TestBuildWorkflowSelectionPrompt_NilGraph(t *testing.T) {
	assert.Panics(t, func() {
		buildWorkflowSelectionPrompt("graph context", "test", nil, fixedNow())
	})
}
