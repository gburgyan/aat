package intent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- FindWorkflowTemplate ---

func TestFindWorkflowTemplate_Match(t *testing.T) {
	g := &graph.Graph{
		Workflows: []graph.Workflow{
			{Name: "Booking", Template: "workflows/booking.yaml"},
			{Name: "Search Only"},
		},
	}

	path, found := FindWorkflowTemplate(g, "Booking")
	assert.True(t, found)
	assert.Equal(t, "workflows/booking.yaml", path)
}

func TestFindWorkflowTemplate_CaseInsensitive(t *testing.T) {
	g := &graph.Graph{
		Workflows: []graph.Workflow{
			{Name: "Full-Payload Booking", Template: "workflows/booking.yaml"},
		},
	}

	path, found := FindWorkflowTemplate(g, "full-payload booking")
	assert.True(t, found)
	assert.Equal(t, "workflows/booking.yaml", path)
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
			{Name: "Booking", Template: "workflows/booking.yaml"},
		},
	}

	_, found := FindWorkflowTemplate(g, "Unknown Workflow")
	assert.False(t, found)
}

// --- LoadWorkflowTemplate ---

func TestLoadWorkflowTemplate_Success(t *testing.T) {
	g, err := graph.ParseFile("testdata/compat/graph.yaml")
	require.NoError(t, err)

	p, err := LoadWorkflowTemplate("workflows/simple.yaml", "testdata", g)
	require.NoError(t, err)
	require.NotNil(t, p)

	assert.NotEmpty(t, p.Execution.Steps)
	assert.NotEmpty(t, p.Execution.Cleanup)
}

func TestLoadWorkflowTemplate_AbsolutePath(t *testing.T) {
	g, err := graph.ParseFile("testdata/compat/graph.yaml")
	require.NoError(t, err)

	absPath, err := filepath.Abs("testdata/workflows/simple.yaml")
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
					{Name: "passengers", Type: "integer", Default: graph.LiteralDefault(1)},
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

// --- Configurable input tests ---

func TestIsInputFed_ConfigurableOptional(t *testing.T) {
	// Configurable optional inputs are NOT fed — they surface to the LLM.
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "carrier", Type: "string", Optional: true, Configurable: true},
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
	assert.Len(t, unfed, 2)
	assert.Contains(t, unfed, "search.origin (string)")
	assert.Contains(t, unfed, "search.carrier (string) [configurable]")
}

func TestIsInputFed_ConfigurableWithDefault(t *testing.T) {
	// Configurable inputs with graph defaults are NOT fed — surface to LLM.
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "passengers", Type: "integer", Optional: true, Configurable: true, Default: graph.LiteralDefault(1)},
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
	assert.Len(t, unfed, 2)
	assert.Contains(t, unfed, "search.origin (string)")
	assert.Contains(t, unfed, "search.passengers (integer) [configurable]")
}

func TestIsInputFed_ConfigurableStructurallyWired(t *testing.T) {
	// Configurable inputs with structural wiring (from ref) ARE fed.
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"process": {
				Name: "process",
				Inputs: []graph.Input{
					{Name: "source", Type: "string", Optional: true, Configurable: true},
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
						"source": {From: "search.contentSource"},
					},
				},
			},
		},
	}

	unfed := UnfedInputsFromTemplate(p, g)
	assert.Empty(t, unfed)
}

// --- isInputFed: locked ---

func TestIsInputFed_LockedIsTrue(t *testing.T) {
	// Locked inputs are always fed — they should never appear in the unfed list.
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
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "search",
					Values: map[string]plan.StepValue{
						"origin":      {FromResolved: "leg1Destination", Locked: true},
						"destination": {FromResolved: "leg1Origin", Locked: true},
					},
				},
			},
		},
	}

	unfed := UnfedInputsFromTemplate(p, g)
	assert.Empty(t, unfed, "locked inputs should not appear as unfed")
}

// --- isInputFed: fromResolved ---

func TestIsInputFed_FromResolvedIsUnfed(t *testing.T) {
	// fromResolved is overridable — it should be treated as unfed so the LLM
	// can override it when user intent conflicts with auto-wiring.
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"process": {
				Name: "process",
				Inputs: []graph.Input{
					{Name: "destination", Type: "string"},
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
						"destination": {FromResolved: "leg1Destination"},
					},
				},
			},
		},
	}

	unfed := UnfedInputsFromTemplate(p, g)
	require.Len(t, unfed, 1)
	assert.Contains(t, unfed[0], "process.destination")
}

func TestIsInputFed_PoolOnlyNotFed(t *testing.T) {
	// Pool-only inputs (no fromResolved) should NOT be fed — they need to be shown.
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
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
						"origin": {Pool: []any{"DEN", "ORD", "SFO"}},
					},
				},
			},
		},
	}

	unfed := UnfedInputsFromTemplate(p, g)
	assert.Len(t, unfed, 1)
	assert.Contains(t, unfed[0], "search.origin")
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
			{Name: "Booking", Template: "workflows/booking.yaml", Description: "Full booking"},
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

func TestBuildWorkflowSelectionPrompt_CompactMenu(t *testing.T) {
	// Menu is now in the user prompt, not the system prompt.
	menu := "## Workflows\n\n- **Booking**: Full booking flow\n\n## Addons\n\n- **Ancillary** (splices after: addTraveler): Add ancillary services\n"

	system, user := buildWorkflowSelectionPrompt(menu, "book a flight", nil)

	// System prompt has format instructions, not workflow details.
	assert.Contains(t, system, "workflow")
	assert.Contains(t, system, "addons")
	assert.Contains(t, system, "layers")
	assert.NotContains(t, system, "Booking")
	assert.NotContains(t, system, "Ancillary")

	// User prompt contains the menu and the user intent.
	assert.Contains(t, user, "## Available Workflows")
	assert.Contains(t, user, "Booking")
	assert.Contains(t, user, "Ancillary")
	assert.Contains(t, user, "splices after: addTraveler")
	assert.Contains(t, user, "## User Intent")
	assert.Contains(t, user, "book a flight")
}

func TestBuildWorkflowSelectionPrompt_StructuredSections(t *testing.T) {
	system, _ := buildWorkflowSelectionPrompt("menu", "test", nil)

	// 1a: System prompt has clearly delimited sections.
	assert.Contains(t, system, "## Output Format")
	assert.Contains(t, system, "## Decision Procedure")
	assert.Contains(t, system, "## Rules")

	// 1b: Explicit classification framing.
	assert.Contains(t, system, "workflow classifier")
	assert.Contains(t, system, "classify the user's testing intent")

	// 1c: Stronger JSON enforcement.
	assert.Contains(t, system, "Return ONLY valid JSON")
	assert.Contains(t, system, "Do not include markdown fencing")

	// 1d: Decision procedure steps.
	assert.Contains(t, system, "1. Identify the user's testing goal")
	assert.Contains(t, system, "6. Return the JSON result")

	// 1e: Tighter addon rule (positive framing).
	assert.Contains(t, system, "Include addons only when the user explicitly requests")
	assert.Contains(t, system, "Omit \"addons\" if no addons apply")

	// 1f: No date generation rule.
	assert.NotContains(t, system, "Today's date")
	assert.NotContains(t, system, "7 days in the future")

	// 1g: Description format specified.
	assert.Contains(t, system, "1-2 sentence summary of the test scenario")

	// Should NOT contain constraint classification instructions.
	assert.NotContains(t, system, "Hard constraints")
	assert.NotContains(t, system, "Soft constraints")
	assert.NotContains(t, system, "Free parameters")
	assert.NotContains(t, system, "appliesTo")
}

func TestBuildWorkflowSelectionPrompt_WithPreSelectedLayers(t *testing.T) {
	system, _ := buildWorkflowSelectionPrompt("menu", "test", []string{"european", "amex"})

	assert.Contains(t, system, "ALREADY selected")
	assert.Contains(t, system, "european, amex")
	assert.Contains(t, system, "do not re-select")
}

func TestBuildWorkflowSelectionPrompt_NoPreSelectedLayers(t *testing.T) {
	system, _ := buildWorkflowSelectionPrompt("menu", "test", nil)
	assert.NotContains(t, system, "ALREADY selected")
}

func TestBuildWorkflowSelectionPrompt_LayersInJSON(t *testing.T) {
	system, _ := buildWorkflowSelectionPrompt("menu", "test", nil)
	// The JSON template should include the "layers" field.
	assert.Contains(t, system, `"layers"`)
}
