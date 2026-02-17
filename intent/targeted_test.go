package intent

import (
	"testing"

	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- buildInputContexts tests ---

func TestBuildInputContexts_BasicTypes(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search flights", Adapter: "a",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "destination", Type: "string"},
					{Name: "departureDate", Type: "date"},
				},
			},
		},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{}},
			},
		},
	}

	contexts := buildInputContexts(p, g, nil, nil)

	require.Len(t, contexts, 3)
	assert.Equal(t, "search", contexts[0].StepID)
	assert.Equal(t, "origin", contexts[0].InputName)
	assert.Equal(t, "string", contexts[0].InputType)
	assert.Equal(t, "Search flights", contexts[0].NodeDesc)
	assert.False(t, contexts[0].IsDate)

	// departureDate should be flagged as date
	assert.Equal(t, "departureDate", contexts[2].InputName)
	assert.True(t, contexts[2].IsDate)
}

func TestBuildInputContexts_SkipFedInputs(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"process": {
				Name: "process", Description: "Process", Adapter: "a",
				Inputs: []graph.Input{
					{Name: "itemId", Type: "string"},
					{Name: "origin", Type: "string"},
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
						"itemId": {From: "search.resultId"}, // fed via from ref
					},
				},
			},
		},
	}

	contexts := buildInputContexts(p, g, nil, nil)

	// Only "origin" should be unfed
	require.Len(t, contexts, 1)
	assert.Equal(t, "origin", contexts[0].InputName)
}

func TestBuildInputContexts_LiteralDefaultsOverrideable(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search flights", Adapter: "a",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "destination", Type: "string"},
					{Name: "departureDate", Type: "date"},
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
						"origin":        {Default: "DEN"},
						"destination":   {Default: "SFO"},
						"departureDate": {Default: "{{today + 14 days}}"},
					},
				},
			},
		},
	}

	contexts := buildInputContexts(p, g, nil, nil)

	// All three inputs have literal defaults — they should be included as overrideable.
	require.Len(t, contexts, 3)

	assert.Equal(t, "origin", contexts[0].InputName)
	assert.Equal(t, "DEN", contexts[0].CurrentDefault)

	assert.Equal(t, "destination", contexts[1].InputName)
	assert.Equal(t, "SFO", contexts[1].CurrentDefault)

	assert.Equal(t, "departureDate", contexts[2].InputName)
	assert.Equal(t, "{{today + 14 days}}", contexts[2].CurrentDefault)
	assert.True(t, contexts[2].IsDate)
}

func TestBuildInputContexts_DomainKBEnrichment(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search", Adapter: "a",
				Inputs: []graph.Input{
					{Name: "origin", Type: "airportCode"},
				},
			},
		},
	}

	kb := &domain.KnowledgeBase{
		Types: map[string]*domain.TypeDef{
			"airportCode": {
				Name:        "airportCode",
				Description: "IATA 3-letter airport code",
				Format:      "^[A-Z]{3}$",
				Pool:        "airports",
			},
		},
		ValuePools: map[string]*domain.ValuePool{
			"airports": {
				Name:   "airports",
				Values: []string{"JFK", "LAX", "LHR"},
			},
		},
		Concepts: map[string]*domain.Concept{},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{}},
			},
		},
	}

	contexts := buildInputContexts(p, g, kb, nil)

	require.Len(t, contexts, 1)
	assert.Equal(t, "IATA 3-letter airport code", contexts[0].DomainType)
	assert.Equal(t, "^[A-Z]{3}$", contexts[0].Format)
	assert.Len(t, contexts[0].PoolValues, 3)
}

func TestBuildInputContexts_NilKB(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search", Adapter: "a",
				Inputs: []graph.Input{
					{Name: "query", Type: "customType"},
				},
			},
		},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{}},
			},
		},
	}

	contexts := buildInputContexts(p, g, nil, nil)

	require.Len(t, contexts, 1)
	assert.Empty(t, contexts[0].DomainType)
	assert.Empty(t, contexts[0].PoolValues)
}

func TestBuildInputContexts_ConstraintMatching(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search", Adapter: "a",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "destination", Type: "string"},
				},
			},
		},
	}

	ws := &WorkflowSelection{
		Constraints: ConstraintSet{
			Hard: []ConstraintInfo{
				{Name: "departure city", Description: "from New York", AppliesTo: []string{"search.origin"}},
			},
			Soft: []ConstraintInfo{
				{Name: "arrival city", Description: "prefer London", AppliesTo: []string{"destination"}},
			},
		},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{}},
			},
		},
	}

	contexts := buildInputContexts(p, g, nil, ws)

	require.Len(t, contexts, 2)

	// origin should have the hard constraint
	assert.Len(t, contexts[0].Constraints, 1)
	assert.Contains(t, contexts[0].Constraints[0], "[hard]")
	assert.Contains(t, contexts[0].Constraints[0], "from New York")

	// destination should have the soft constraint (matched by bare input name)
	assert.Len(t, contexts[1].Constraints, 1)
	assert.Contains(t, contexts[1].Constraints[0], "[soft]")
	assert.Contains(t, contexts[1].Constraints[0], "prefer London")
}

func TestBuildInputContexts_GraphConstraints(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search", Adapter: "a",
				Inputs: []graph.Input{
					{
						Name: "passengerCount",
						Type: "integer",
						Constraints: &graph.Constraint{
							Description: "Number of passengers",
							Pattern:     "^[1-9]$",
						},
					},
				},
			},
		},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{}},
			},
		},
	}

	contexts := buildInputContexts(p, g, nil, nil)

	require.Len(t, contexts, 1)
	assert.Contains(t, contexts[0].GraphConstr, "Number of passengers")
	assert.Contains(t, contexts[0].GraphConstr, "^[1-9]$")
}

func TestBuildInputContexts_Multiplicity(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"addItem": {
				Name: "addItem", Description: "Add item", Adapter: "a",
				Inputs: []graph.Input{
					{Name: "name", Type: "string"},
					{Name: "setupId", Type: "string"},
				},
			},
		},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{ID: "addItem_1", Node: "addItem", Values: map[string]plan.StepValue{
					"setupId": {From: "setup.setupId"},
				}},
				{ID: "addItem_2", Node: "addItem", Values: map[string]plan.StepValue{
					"setupId": {From: "setup.setupId"},
				}},
			},
		},
	}

	contexts := buildInputContexts(p, g, nil, nil)

	// Both steps should have "name" as unfed
	require.Len(t, contexts, 2)
	assert.Equal(t, "addItem_1", contexts[0].StepID)
	assert.Equal(t, "name", contexts[0].InputName)
	assert.Equal(t, "addItem_2", contexts[1].StepID)
	assert.Equal(t, "name", contexts[1].InputName)
}

// --- buildSelectionContexts tests ---

func TestBuildSelectionContexts_NamedSelection(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Adapter: "a",
				Outputs: []graph.Output{
					{
						Name: "items",
						Type: "item[]",
						ElementFields: []graph.Field{
							{Name: "itemId", Type: "string"},
							{Name: "price", Type: "money"},
						},
					},
				},
			},
			"process": {
				Name: "process", Adapter: "b",
				Inputs: []graph.Input{
					{Name: "itemId", Type: "string"},
					{Name: "price", Type: "money"},
				},
			},
		},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "process",
					Selections: map[string]plan.StepSelection{
						"item": {From: "search.items", Strategy: "first"},
					},
					Values: map[string]plan.StepValue{
						"itemId": {FromSelection: "item.itemId"},
						"price":  {FromSelection: "item.price"},
					},
				},
			},
		},
	}

	contexts := buildSelectionContexts(p, g)

	require.Len(t, contexts, 1)
	assert.Equal(t, "process", contexts[0].StepID)
	assert.Equal(t, "item", contexts[0].SelectionName)
	assert.Equal(t, "search.items", contexts[0].Source)
	assert.Equal(t, "first", contexts[0].CurrentStrategy)
	assert.True(t, contexts[0].IsNamed)
	assert.Contains(t, contexts[0].ElementFields, "itemId (string)")
	assert.Contains(t, contexts[0].ElementFields, "price (money)")
	assert.Len(t, contexts[0].FeedsInto, 2)
}

func TestBuildSelectionContexts_InlineSelection(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Adapter: "a",
				Outputs: []graph.Output{
					{
						Name: "items",
						Type: "item[]",
						ElementFields: []graph.Field{
							{Name: "itemId", Type: "string"},
						},
					},
				},
			},
		},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "process",
					Values: map[string]plan.StepValue{
						"itemId": {
							From:   "search.items",
							Select: &plan.SelectionConfig{Strategy: "match", Filter: "active == true"},
						},
					},
				},
			},
		},
	}

	contexts := buildSelectionContexts(p, g)

	require.Len(t, contexts, 1)
	assert.Equal(t, "itemId", contexts[0].SelectionName)
	assert.False(t, contexts[0].IsNamed)
	assert.Equal(t, "match", contexts[0].CurrentStrategy)
	assert.Equal(t, []string{"itemId"}, contexts[0].FeedsInto)
}

func TestBuildSelectionContexts_Empty(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "simple", Values: map[string]plan.StepValue{
					"query": {Default: "test"},
				}},
			},
		},
	}

	contexts := buildSelectionContexts(p, g)
	assert.Empty(t, contexts)
}

// --- buildCompactPlanFlow tests ---

func TestBuildCompactPlanFlow(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {Name: "search", Description: "Search for flights", Outputs: []graph.Output{
				{Name: "offerings", Type: "array"},
				{Name: "searchId", Type: "string"},
			}},
			"process": {Name: "process", Description: "Process booking", Outputs: []graph.Output{
				{Name: "locator", Type: "string"},
			}},
			"cleanup": {Name: "cleanup", Description: "Clean up"},
		},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search"},
				{Node: "process", DependsOn: []string{"search"}, IsGoal: true},
			},
			Cleanup: []plan.CleanupStep{
				{Node: "cleanup", RunOn: "always"},
			},
		},
	}

	flow := buildCompactPlanFlow(p, g)

	assert.Contains(t, flow, "1. search")
	assert.Contains(t, flow, "Search for flights")
	assert.Contains(t, flow, "Outputs: offerings, searchId")
	assert.Contains(t, flow, "2. process")
	assert.Contains(t, flow, "(depends on: search)")
	assert.Contains(t, flow, "[GOAL]")
	assert.Contains(t, flow, "Outputs: locator")
	assert.Contains(t, flow, "Cleanup: cleanup (always)")
}

func TestBuildCompactPlanFlow_WithStepID(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"addItem": {Name: "addItem", Description: "Add an item"},
		},
	}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{ID: "addItem_1", Node: "addItem"},
				{ID: "addItem_2", Node: "addItem", DependsOn: []string{"addItem_1"}},
			},
		},
	}

	flow := buildCompactPlanFlow(p, g)

	assert.Contains(t, flow, "addItem_1 [node: addItem]")
	assert.Contains(t, flow, "addItem_2 [node: addItem]")
}

// --- parseTargetedResponse tests ---

func TestParseTargetedResponse_ValidJSON(t *testing.T) {
	content := `{
		"values": {
			"search.origin": "JFK",
			"search.departureDate": "{{today + 30 days}}"
		},
		"selections": {
			"addOffer.catalogOffering": {
				"strategy": "match",
				"filter": "stops == 0"
			}
		},
		"assertions": {
			"commitBooking": [
				{"type": "status", "expect": 200},
				{"type": "fieldExists", "path": "$.locator"}
			]
		},
		"descriptions": {
			"search": "Search for flights from JFK"
		}
	}`

	resp, err := parseTargetedResponse(content)

	require.NoError(t, err)
	assert.Equal(t, "JFK", resp.Values["search.origin"])
	assert.Equal(t, "{{today + 30 days}}", resp.Values["search.departureDate"])
	assert.Equal(t, "match", resp.Selections["addOffer.catalogOffering"].Strategy)
	assert.Equal(t, "stops == 0", resp.Selections["addOffer.catalogOffering"].Filter)
	assert.Len(t, resp.Assertions["commitBooking"], 2)
	assert.Equal(t, "Search for flights from JFK", resp.Descriptions["search"])
}

func TestParseTargetedResponse_JSONFencing(t *testing.T) {
	content := "```json\n{\"values\": {\"a.b\": \"c\"}}\n```"

	resp, err := parseTargetedResponse(content)

	require.NoError(t, err)
	assert.Equal(t, "c", resp.Values["a.b"])
}

func TestParseTargetedResponse_Malformed(t *testing.T) {
	_, err := parseTargetedResponse("not valid json {{{")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing targeted response")
}

func TestParseTargetedResponse_EmptyMaps(t *testing.T) {
	content := `{}`

	resp, err := parseTargetedResponse(content)

	require.NoError(t, err)
	assert.NotNil(t, resp.Values)
	assert.NotNil(t, resp.Selections)
	assert.NotNil(t, resp.Assertions)
	assert.NotNil(t, resp.Descriptions)
}

func TestParseTargetedResponse_PartialMaps(t *testing.T) {
	content := `{"values": {"a.b": 42}}`

	resp, err := parseTargetedResponse(content)

	require.NoError(t, err)
	assert.Len(t, resp.Values, 1)
	assert.NotNil(t, resp.Selections)
	assert.NotNil(t, resp.Assertions)
	assert.NotNil(t, resp.Descriptions)
}

// --- applyTargetedResponse tests ---

func TestApplyTargetedResponse_ValuesApplied(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{}},
			},
		},
	}

	resp := &TargetedResponse{
		Values:       map[string]any{"search.origin": "JFK", "search.destination": "LHR"},
		Selections:   map[string]TargetedSelection{},
		Assertions:   map[string][]TargetedAssertion{},
		Descriptions: map[string]string{},
	}

	unfedSet := map[string]bool{
		"search.origin":      true,
		"search.destination": true,
	}

	applyTargetedResponse(skeleton, resp, unfedSet)

	assert.Equal(t, "JFK", skeleton.Execution.Steps[0].Values["origin"].Default)
	assert.Equal(t, "LHR", skeleton.Execution.Steps[0].Values["destination"].Default)
}

func TestApplyTargetedResponse_WiredInputsRejected(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "process", Values: map[string]plan.StepValue{
					"itemId": {From: "search.resultId"},
				}},
			},
		},
	}

	resp := &TargetedResponse{
		Values:       map[string]any{"process.itemId": "hallucinated-value"},
		Selections:   map[string]TargetedSelection{},
		Assertions:   map[string][]TargetedAssertion{},
		Descriptions: map[string]string{},
	}

	// itemId is NOT in unfedSet — it's wired
	unfedSet := map[string]bool{}

	applyTargetedResponse(skeleton, resp, unfedSet)

	// The from ref should be preserved, not overwritten
	assert.Equal(t, "search.resultId", skeleton.Execution.Steps[0].Values["itemId"].From)
	assert.Nil(t, skeleton.Execution.Steps[0].Values["itemId"].Default)
}

func TestApplyTargetedResponse_NamedSelectionOverride(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "process",
					Selections: map[string]plan.StepSelection{
						"item": {From: "search.items", Strategy: "first"},
					},
					Values: map[string]plan.StepValue{},
				},
			},
		},
	}

	resp := &TargetedResponse{
		Values: map[string]any{},
		Selections: map[string]TargetedSelection{
			"process.item": {Strategy: "match", Filter: "active == true"},
		},
		Assertions:   map[string][]TargetedAssertion{},
		Descriptions: map[string]string{},
	}

	applyTargetedResponse(skeleton, resp, map[string]bool{})

	sel := skeleton.Execution.Steps[0].Selections["item"]
	assert.Equal(t, "match", sel.Strategy)
	assert.Equal(t, "active == true", sel.Filter)
	assert.Equal(t, "search.items", sel.From) // preserved
}

func TestApplyTargetedResponse_InlineSelectionOverride(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "process",
					Values: map[string]plan.StepValue{
						"itemId": {
							From:   "search.items",
							Select: &plan.SelectionConfig{Strategy: "first", Field: "itemId"},
						},
					},
				},
			},
		},
	}

	resp := &TargetedResponse{
		Values: map[string]any{},
		Selections: map[string]TargetedSelection{
			"process.itemId": {Strategy: "min", SortField: "price"},
		},
		Assertions:   map[string][]TargetedAssertion{},
		Descriptions: map[string]string{},
	}

	applyTargetedResponse(skeleton, resp, map[string]bool{})

	sv := skeleton.Execution.Steps[0].Values["itemId"]
	assert.Equal(t, "min", sv.Select.Strategy)
	assert.Equal(t, "price", sv.Select.SortField)
	assert.Equal(t, "search.items", sv.From) // preserved
}

func TestApplyTargetedResponse_Assertions(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "commit", Values: map[string]plan.StepValue{}},
			},
		},
	}

	resp := &TargetedResponse{
		Values:     map[string]any{},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{
			"commit": {
				{Type: "status", Expect: float64(200)},
				{Type: "fieldExists", Path: "$.locator"},
			},
		},
		Descriptions: map[string]string{},
	}

	applyTargetedResponse(skeleton, resp, map[string]bool{})

	require.NotNil(t, skeleton.Execution.Steps[0].Assertions)
	assert.Len(t, skeleton.Execution.Steps[0].Assertions.Mechanical, 2)
	assert.Equal(t, "status", skeleton.Execution.Steps[0].Assertions.Mechanical[0].Type)
	assert.Equal(t, "fieldExists", skeleton.Execution.Steps[0].Assertions.Mechanical[1].Type)
	assert.Equal(t, "locator", skeleton.Execution.Steps[0].Assertions.Mechanical[1].Path) // $. stripped
}

func TestApplyTargetedResponse_Descriptions(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{}},
				{Node: "process", Values: map[string]plan.StepValue{}},
			},
		},
	}

	resp := &TargetedResponse{
		Values:     map[string]any{},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
		Descriptions: map[string]string{
			"search":  "Search for flights from JFK to LHR",
			"process": "Process the selected flight",
		},
	}

	applyTargetedResponse(skeleton, resp, map[string]bool{})

	assert.Equal(t, "Search for flights from JFK to LHR", skeleton.Execution.Steps[0].Description)
	assert.Equal(t, "Process the selected flight", skeleton.Execution.Steps[1].Description)
}

func TestApplyTargetedResponse_UnknownStepIDSkipped(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{}},
			},
		},
	}

	resp := &TargetedResponse{
		Values:       map[string]any{"nonexistent.query": "test"},
		Selections:   map[string]TargetedSelection{},
		Assertions:   map[string][]TargetedAssertion{},
		Descriptions: map[string]string{"nonexistent": "should be skipped"},
	}

	// Should not panic or modify anything
	applyTargetedResponse(skeleton, resp, map[string]bool{"nonexistent.query": true})

	// search step should be unchanged
	assert.Empty(t, skeleton.Execution.Steps[0].Description)
	assert.Empty(t, skeleton.Execution.Steps[0].Values)
}

// --- splitStepInput tests ---

func TestSplitStepInput(t *testing.T) {
	tests := []struct {
		key      string
		stepID   string
		inputName string
	}{
		{"search.origin", "search", "origin"},
		{"addItem_1.name", "addItem_1", "name"},
		{"noDot", "noDot", ""},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			stepID, inputName := splitStepInput(tt.key)
			assert.Equal(t, tt.stepID, stepID)
			assert.Equal(t, tt.inputName, inputName)
		})
	}
}

// --- sanitizeAssertions tests ---

func TestSanitizeAssertions_ValidAssertions(t *testing.T) {
	assertions := []TargetedAssertion{
		{Type: "status", Expect: float64(200)},
		{Type: "fieldExists", Path: "$.locator"},
		{Type: "fieldEquals", Path: "$.status", Value: "confirmed"},
		{Type: "predicate", Expr: "locator != \"\""},
	}

	result := sanitizeAssertions(assertions)

	require.Len(t, result, 4)
	assert.Equal(t, "status", result[0].Type)
	assert.Equal(t, float64(200), result[0].Expect)
	assert.Equal(t, "fieldExists", result[1].Type)
	assert.Equal(t, "locator", result[1].Path) // $. stripped
	assert.Equal(t, "fieldEquals", result[2].Type)
	assert.Equal(t, "status", result[2].Path) // $. stripped
	assert.Equal(t, "predicate", result[3].Type)
	assert.Equal(t, "locator != \"\"", result[3].Expr)
}

func TestSanitizeAssertions_UnknownTypeFiltered(t *testing.T) {
	assertions := []TargetedAssertion{
		{Type: "status", Expect: float64(200)},
		{Type: "responseTime", Expect: float64(500)}, // invalid type
		{Type: "magic", Path: "$.foo"},                // invalid type
	}

	result := sanitizeAssertions(assertions)

	require.Len(t, result, 1)
	assert.Equal(t, "status", result[0].Type)
}

func TestSanitizeAssertions_PredicateExprInExpect(t *testing.T) {
	// Common LLM mistake: putting expression in "expect" instead of "expr"
	assertions := []TargetedAssertion{
		{Type: "predicate", Expect: "locator != \"\"", Expr: ""},
	}

	result := sanitizeAssertions(assertions)

	require.Len(t, result, 1)
	assert.Equal(t, "locator != \"\"", result[0].Expr)
	assert.Nil(t, result[0].Expect) // expect cleared
}

func TestSanitizeAssertions_PredicateEmptyExprFiltered(t *testing.T) {
	// No expr and expect isn't a string — skip
	assertions := []TargetedAssertion{
		{Type: "predicate", Expect: float64(42)},
		{Type: "predicate"}, // completely empty
	}

	result := sanitizeAssertions(assertions)

	assert.Empty(t, result)
}

func TestSanitizeAssertions_FieldExistsEmptyPathFiltered(t *testing.T) {
	assertions := []TargetedAssertion{
		{Type: "fieldExists"},
		{Type: "fieldExists", Path: ""},
		{Type: "fieldExists", Path: "$.locator"}, // valid, $. stripped
	}

	result := sanitizeAssertions(assertions)

	require.Len(t, result, 1)
	assert.Equal(t, "locator", result[0].Path)
}

func TestSanitizeAssertions_FieldEqualsEmptyPathFiltered(t *testing.T) {
	assertions := []TargetedAssertion{
		{Type: "fieldEquals", Value: "test"},            // no path
		{Type: "fieldEquals", Path: "$.x", Value: "y"},  // valid, $. stripped
	}

	result := sanitizeAssertions(assertions)

	require.Len(t, result, 1)
	assert.Equal(t, "x", result[0].Path)
}

func TestSanitizeAssertions_StatusNilExpectFiltered(t *testing.T) {
	assertions := []TargetedAssertion{
		{Type: "status"},                    // no expect
		{Type: "status", Expect: float64(200)}, // valid
	}

	result := sanitizeAssertions(assertions)

	require.Len(t, result, 1)
	assert.Equal(t, float64(200), result[0].Expect)
}

func TestSanitizeAssertions_SchemaNilExpectFiltered(t *testing.T) {
	assertions := []TargetedAssertion{
		{Type: "schema"},                           // no expect
		{Type: "schema", Expect: "MySchemaName"},   // valid
	}

	result := sanitizeAssertions(assertions)

	require.Len(t, result, 1)
	assert.Equal(t, "MySchemaName", result[0].Expect)
}

func TestSanitizeAssertions_PredicateInvalidExprFiltered(t *testing.T) {
	assertions := []TargetedAssertion{
		// Contains $. prefix which fails the predicate parser
		{Type: "predicate", Expr: "$.status == 'CONFIRMED'"},
		// Contains unsupported function call
		{Type: "predicate", Expr: "size(items) > 0"},
		// Valid predicate
		{Type: "predicate", Expr: "status == 'CONFIRMED'"},
	}

	result := sanitizeAssertions(assertions)

	require.Len(t, result, 1)
	assert.Equal(t, "status == 'CONFIRMED'", result[0].Expr)
}

func TestSanitizeAssertions_FieldPathsStripped(t *testing.T) {
	assertions := []TargetedAssertion{
		{Type: "fieldExists", Path: "$.locator"},
		{Type: "fieldEquals", Path: "$.status", Value: "CONFIRMED"},
		{Type: "fieldExists", Path: "pnrLocator"}, // no prefix
	}

	result := sanitizeAssertions(assertions)

	require.Len(t, result, 3)
	assert.Equal(t, "locator", result[0].Path)
	assert.Equal(t, "status", result[1].Path)
	assert.Equal(t, "pnrLocator", result[2].Path)
}

func TestSanitizeAssertions_AllFilteredReturnsNil(t *testing.T) {
	assertions := []TargetedAssertion{
		{Type: "invalid"},
		{Type: "predicate"}, // empty expr
		{Type: "status"},    // nil expect
	}

	result := sanitizeAssertions(assertions)
	assert.Nil(t, result)
}

func TestApplyTargetedResponse_AssertionsSanitized(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "commit", Values: map[string]plan.StepValue{}},
			},
		},
	}

	resp := &TargetedResponse{
		Values:     map[string]any{},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{
			"commit": {
				{Type: "status", Expect: float64(200)},
				{Type: "predicate", Expect: "locator != \"\"", Expr: ""}, // expr in expect
				{Type: "predicate"}, // completely empty — should be filtered
				{Type: "bogusType", Path: "$.foo"}, // invalid type — filtered
			},
		},
		Descriptions: map[string]string{},
	}

	applyTargetedResponse(skeleton, resp, map[string]bool{})

	require.NotNil(t, skeleton.Execution.Steps[0].Assertions)
	mechanical := skeleton.Execution.Steps[0].Assertions.Mechanical
	require.Len(t, mechanical, 2)
	assert.Equal(t, "status", mechanical[0].Type)
	assert.Equal(t, "predicate", mechanical[1].Type)
	assert.Equal(t, "locator != \"\"", mechanical[1].Expr)
	assert.Nil(t, mechanical[1].Expect)
}

func TestApplyTargetedResponse_AllAssertionsFilteredNoAssertionsSet(t *testing.T) {
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "commit", Values: map[string]plan.StepValue{}},
			},
		},
	}

	resp := &TargetedResponse{
		Values:     map[string]any{},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{
			"commit": {
				{Type: "invalid"},
				{Type: "predicate"}, // empty
			},
		},
		Descriptions: map[string]string{},
	}

	applyTargetedResponse(skeleton, resp, map[string]bool{})

	// Assertions struct should NOT be created when all assertions are filtered
	assert.Nil(t, skeleton.Execution.Steps[0].Assertions)
}

// --- resolveElementFields tests ---

func TestResolveElementFields(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search",
				Outputs: []graph.Output{
					{
						Name: "items",
						Type: "item[]",
						ElementFields: []graph.Field{
							{Name: "id", Type: "string"},
							{Name: "carrier", Type: "string"},
						},
					},
				},
			},
		},
	}

	fields := resolveElementFields(g, "search.items")
	assert.Equal(t, []string{"id (string)", "carrier (string)"}, fields)

	// Unknown source
	fields = resolveElementFields(g, "unknown.items")
	assert.Nil(t, fields)

	// No dot
	fields = resolveElementFields(g, "nodot")
	assert.Nil(t, fields)
}
