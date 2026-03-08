package intent

import (
	"strings"
	"testing"
	"time"

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

	contexts := buildInputContexts(p, g, nil, nil, nil)

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

	contexts := buildInputContexts(p, g, nil, nil, nil)

	// Only "origin" should be unfed
	require.Len(t, contexts, 1)
	assert.Equal(t, "origin", contexts[0].InputName)
}

func TestBuildInputContexts_SkipLockedInputs(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search flights", Adapter: "a",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "destination", Type: "string"},
					{Name: "leg2Origin", Type: "string"},
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
						"leg2Origin": {FromResolved: "leg1Destination", Locked: true},
					},
				},
			},
		},
	}

	contexts := buildInputContexts(p, g, nil, nil, nil)

	// leg2Origin is locked — should not appear
	names := make([]string, len(contexts))
	for i, c := range contexts {
		names[i] = c.InputName
	}
	assert.NotContains(t, names, "leg2Origin", "locked input should be excluded")
	assert.Contains(t, names, "origin")
	assert.Contains(t, names, "destination")
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

	contexts := buildInputContexts(p, g, nil, nil, nil)

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

	contexts := buildInputContexts(p, g, kb, nil, nil)

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

	contexts := buildInputContexts(p, g, nil, nil, nil)

	require.Len(t, contexts, 1)
	assert.Empty(t, contexts[0].DomainType)
	assert.Empty(t, contexts[0].PoolValues)
}

func TestBuildInputContexts_NoConstraintMatching(t *testing.T) {
	// Constraint matching from WorkflowSelection was removed — constraints
	// are no longer passed from Call 1 to Call 2. The user's prompt is
	// passed directly to Call 2 instead.
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

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{}},
			},
		},
	}

	contexts := buildInputContexts(p, g, nil, nil, nil)

	require.Len(t, contexts, 2)
	assert.Equal(t, "origin", contexts[0].InputName)
	assert.Equal(t, "destination", contexts[1].InputName)
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

	contexts := buildInputContexts(p, g, nil, nil, nil)

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

	contexts := buildInputContexts(p, g, nil, nil, nil)

	// Both steps should have "name" as unfed
	require.Len(t, contexts, 2)
	assert.Equal(t, "addItem_1", contexts[0].StepID)
	assert.Equal(t, "name", contexts[0].InputName)
	assert.Equal(t, "addItem_2", contexts[1].StepID)
	assert.Equal(t, "name", contexts[1].InputName)
}

// --- Template pool tests ---

func TestBuildInputContexts_PooledInput(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search flights", Adapter: "a",
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
						"origin":      {Pool: []any{"DEN", "SFO", "ORD", "JFK", "LAX"}},
						"destination": {},
					},
				},
			},
		},
	}

	contexts := buildInputContexts(p, g, nil, nil, nil)

	// Both inputs should appear: origin (pooled) and destination (unfed)
	require.Len(t, contexts, 2)

	var originCtx *InputContext
	for i, ic := range contexts {
		if ic.InputName == "origin" {
			originCtx = &contexts[i]
		}
	}
	require.NotNil(t, originCtx)
	assert.True(t, originCtx.HasTemplatePool)
	assert.Equal(t, []string{"DEN", "SFO", "ORD", "JFK", "LAX"}, originCtx.PoolValues)
	assert.Empty(t, originCtx.CurrentDefault) // no default, just pool
}

func TestValidateTargetedResponse_PooledInputOmitted(t *testing.T) {
	// Missing value for a pooled input should NOT produce a missing_value issue.
	resp := &TargetedResponse{
		Values:     map[string]any{"search.destination": "LHR"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{
		"search.origin":      true,
		"search.destination": true,
	}

	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string", HasTemplatePool: true, PoolValues: []string{"DEN", "SFO"}},
		{StepID: "search", InputName: "destination", InputType: "string"},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)
	assert.Empty(t, issues)
}

func TestBuildTargetedPlanPrompt_PoolDisplay(t *testing.T) {
	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string", IsPoolInput: true, HasTemplatePool: true, PoolValues: []string{"DEN", "SFO", "ORD"}},
		{StepID: "search", InputName: "destination", InputType: "string", PoolValues: []string{"LHR", "CDG"}},
	}

	_, user := buildTargetedPlanPrompt(inputContexts, nil, "book a flight", nil, time.Now())

	// Pool-section inputs should now show concrete pool values as reference.
	assert.Contains(t, user, "Pool (random at runtime): DEN, SFO, ORD")
	// Required-section inputs with domain samples should still show "Sample values".
	assert.Contains(t, user, "Sample values: LHR, CDG")
}

func TestBuildTargetedPlanPrompt_PoolSeparateSection(t *testing.T) {
	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string", IsPoolInput: true, HasTemplatePool: true, PoolValues: []string{"DEN", "SFO"}},
		{StepID: "search", InputName: "destination", InputType: "string"},
		{StepID: "search", InputName: "carrier", InputType: "string", IsConfigurable: true},
	}

	_, user := buildTargetedPlanPrompt(inputContexts, nil, "book a flight", nil, time.Now())

	// Pool inputs should be under their own section, not "Inputs That Need Values".
	assert.Contains(t, user, "## Pool Inputs — auto-selected at runtime")
	assert.Contains(t, user, "## Inputs That Need Values")
	assert.Contains(t, user, "## Optional Configuration")

	// origin (pool) should NOT be under "Inputs That Need Values"
	// destination (required) should be under "Inputs That Need Values"
	// carrier (configurable) should be under "Optional Configuration"

	// Verify section ordering: required inputs come before pool inputs
	requiredIdx := strings.Index(user, "## Inputs That Need Values")
	poolIdx := strings.Index(user, "## Pool Inputs — auto-selected")
	configIdx := strings.Index(user, "## Optional Configuration")
	assert.Less(t, requiredIdx, poolIdx)
	assert.Less(t, poolIdx, configIdx)

	// origin should appear in pool section (after pool header)
	originIdx := strings.Index(user, "### search.origin")
	assert.Greater(t, originIdx, poolIdx)

	// destination should appear in required section (before pool header)
	destIdx := strings.Index(user, "### search.destination")
	assert.Greater(t, destIdx, requiredIdx)
	assert.Less(t, destIdx, poolIdx)
}

// --- Configurable input tests ---

func TestBuildInputContexts_ConfigurableGraphDefault(t *testing.T) {
	// Configurable inputs should appear in contexts even with graph defaults,
	// and the graph default should be shown as CurrentDefault.
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name:    "search",
				Adapter: "a",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "passengers", Type: "integer", Optional: true, Configurable: true, Default: graph.LiteralDefault(1)},
					{Name: "carrier", Type: "string", Optional: true, Configurable: true},
				},
			},
		},
	}

	skeleton := &plan.Plan{
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

	contexts := buildInputContexts(skeleton, g, nil, nil, nil)

	// origin (required unfed with literal default) + passengers + carrier (both configurable)
	require.Len(t, contexts, 3)

	// Find the passengers context.
	var passCtx, carrCtx *InputContext
	for i, ic := range contexts {
		if ic.InputName == "passengers" {
			passCtx = &contexts[i]
		}
		if ic.InputName == "carrier" {
			carrCtx = &contexts[i]
		}
	}

	require.NotNil(t, passCtx)
	assert.True(t, passCtx.IsConfigurable)
	assert.Equal(t, "1", passCtx.CurrentDefault) // graph-level default

	require.NotNil(t, carrCtx)
	assert.True(t, carrCtx.IsConfigurable)
	assert.Equal(t, "", carrCtx.CurrentDefault) // no default
}

func TestBuildTargetedPlanPrompt_ConfigurableSection(t *testing.T) {
	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string"},
		{StepID: "search", InputName: "passengers", InputType: "integer", IsConfigurable: true, CurrentDefault: "1"},
		{StepID: "search", InputName: "carrier", InputType: "string", IsConfigurable: true},
	}

	_, user := buildTargetedPlanPrompt(inputContexts, nil, "book a flight", nil, time.Now())

	// Required inputs in main section.
	assert.Contains(t, user, "## Inputs That Need Values")
	assert.Contains(t, user, "### search.origin (string)")

	// Configurable inputs in separate section.
	assert.Contains(t, user, "## Optional Configuration")
	assert.Contains(t, user, "### search.passengers (integer)")
	assert.Contains(t, user, "### search.carrier (string)")

	// Configurable defaults show "Default: X (used if omitted)".
	assert.Contains(t, user, "Default: 1 (used if omitted)")
}

func TestValidateTargetedResponse_ConfigurableMissing_NoError(t *testing.T) {
	// Missing value for a configurable input should NOT produce a missing_value issue.
	resp := &TargetedResponse{
		Values:     map[string]any{"search.origin": "JFK"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{
		"search.origin":     true,
		"search.passengers": true,
	}

	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string"},
		{StepID: "search", InputName: "passengers", InputType: "integer", IsConfigurable: true, CurrentDefault: "1"},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)
	assert.Empty(t, issues)
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
	// Descriptions are no longer parsed from the LLM response.
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
}

func TestParseTargetedResponse_PartialMaps(t *testing.T) {
	content := `{"values": {"a.b": 42}}`

	resp, err := parseTargetedResponse(content)

	require.NoError(t, err)
	assert.Len(t, resp.Values, 1)
	assert.NotNil(t, resp.Selections)
	assert.NotNil(t, resp.Assertions)
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
		Values:     map[string]any{"search.origin": "JFK", "search.destination": "LHR"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
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
		Values:     map[string]any{"process.itemId": "hallucinated-value"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
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
		Assertions: map[string][]TargetedAssertion{},
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
		Assertions: map[string][]TargetedAssertion{},
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
	}

	applyTargetedResponse(skeleton, resp, map[string]bool{})

	require.NotNil(t, skeleton.Execution.Steps[0].Assertions)
	assert.Len(t, skeleton.Execution.Steps[0].Assertions.Mechanical, 2)
	assert.Equal(t, "status", skeleton.Execution.Steps[0].Assertions.Mechanical[0].Type)
	assert.Equal(t, "fieldExists", skeleton.Execution.Steps[0].Assertions.Mechanical[1].Type)
	assert.Equal(t, "locator", skeleton.Execution.Steps[0].Assertions.Mechanical[1].Path) // $. stripped
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
		Values:     map[string]any{"nonexistent.query": "test"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	// Should not panic or modify anything
	applyTargetedResponse(skeleton, resp, map[string]bool{"nonexistent.query": true})

	// search step should be unchanged
	assert.Empty(t, skeleton.Execution.Steps[0].Values)
}

// --- splitStepInput tests ---

func TestSplitStepInput(t *testing.T) {
	tests := []struct {
		key       string
		stepID    string
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
		{Type: "magic", Path: "$.foo"},               // invalid type
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
		{Type: "fieldEquals", Value: "test"},           // no path
		{Type: "fieldEquals", Path: "$.x", Value: "y"}, // valid, $. stripped
	}

	result := sanitizeAssertions(assertions)

	require.Len(t, result, 1)
	assert.Equal(t, "x", result[0].Path)
}

func TestSanitizeAssertions_StatusNilExpectFiltered(t *testing.T) {
	assertions := []TargetedAssertion{
		{Type: "status"},                       // no expect
		{Type: "status", Expect: float64(200)}, // valid
	}

	result := sanitizeAssertions(assertions)

	require.Len(t, result, 1)
	assert.Equal(t, float64(200), result[0].Expect)
}

func TestSanitizeAssertions_SchemaNilExpectFiltered(t *testing.T) {
	assertions := []TargetedAssertion{
		{Type: "schema"},                         // no expect
		{Type: "schema", Expect: "MySchemaName"}, // valid
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

func TestSanitizeAssertions_RawFlagPreserved(t *testing.T) {
	assertions := []TargetedAssertion{
		{Type: "fieldEquals", Path: "$.nested.count", Value: float64(3), Raw: true},
		{Type: "fieldExists", Path: "id"},
	}

	result := sanitizeAssertions(assertions)

	require.Len(t, result, 2)
	assert.True(t, result[0].Raw)
	assert.Equal(t, "nested.count", result[0].Path) // $. stripped
	assert.False(t, result[1].Raw)
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
				{Type: "predicate"},                                      // completely empty — should be filtered
				{Type: "bogusType", Path: "$.foo"},                       // invalid type — filtered
			},
		},
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

// --- validateTargetedResponse tests ---

func TestValidateTargetedResponse_CleanResponse(t *testing.T) {
	resp := &TargetedResponse{
		Values:     map[string]any{"search.origin": "JFK", "search.destination": "LHR"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{
		"search.origin":      true,
		"search.destination": true,
	}

	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string"},
		{StepID: "search", InputName: "destination", InputType: "string"},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)
	assert.Empty(t, issues)
}

func TestValidateTargetedResponse_MissingValue(t *testing.T) {
	resp := &TargetedResponse{
		Values:     map[string]any{"search.origin": "JFK"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{
		"search.origin":      true,
		"search.destination": true,
	}

	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string"},
		{StepID: "search", InputName: "destination", InputType: "string"},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)

	require.Len(t, issues, 1)
	assert.Equal(t, "search.destination", issues[0].Key)
	assert.Equal(t, "missing_value", issues[0].Kind)
}

func TestValidateTargetedResponse_MissingValueWithDefault(t *testing.T) {
	// If the input has a CurrentDefault, it's not required from the LLM.
	resp := &TargetedResponse{
		Values:     map[string]any{},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{
		"search.origin": true,
	}

	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string", CurrentDefault: "DEN"},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)
	assert.Empty(t, issues)
}

func TestValidateTargetedResponse_NonLiteralObject(t *testing.T) {
	resp := &TargetedResponse{
		Values: map[string]any{
			"search.origin": map[string]any{"from": "setup.origin", "select": "first"},
		},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{"search.origin": true}
	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string"},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)

	require.Len(t, issues, 1)
	assert.Equal(t, "search.origin", issues[0].Key)
	assert.Equal(t, "non_literal_value", issues[0].Kind)
	assert.Contains(t, issues[0].Message, "object")
}

func TestValidateTargetedResponse_NonLiteralArray(t *testing.T) {
	resp := &TargetedResponse{
		Values: map[string]any{
			"search.tags": []any{"a", "b"},
		},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{"search.tags": true}
	inputContexts := []InputContext{
		{StepID: "search", InputName: "tags", InputType: "string"},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)

	require.Len(t, issues, 1)
	assert.Equal(t, "non_literal_value", issues[0].Kind)
	assert.Contains(t, issues[0].Message, "array")
}

func TestValidateTargetedResponse_InvalidStrategy(t *testing.T) {
	resp := &TargetedResponse{
		Values: map[string]any{},
		Selections: map[string]TargetedSelection{
			"process.item": {Strategy: "cheapest"},
		},
		Assertions: map[string][]TargetedAssertion{},
	}

	issues := validateTargetedResponse(resp, map[string]bool{}, nil, nil)

	require.Len(t, issues, 1)
	assert.Equal(t, "process.item", issues[0].Key)
	assert.Equal(t, "invalid_strategy", issues[0].Kind)
	assert.Contains(t, issues[0].Message, "cheapest")
}

func TestValidateTargetedResponse_InvalidFilterSyntax(t *testing.T) {
	resp := &TargetedResponse{
		Values: map[string]any{},
		Selections: map[string]TargetedSelection{
			"process.item": {Strategy: "match", Filter: "price >>== 100"},
		},
		Assertions: map[string][]TargetedAssertion{},
	}

	issues := validateTargetedResponse(resp, map[string]bool{}, nil, nil)

	require.Len(t, issues, 1)
	assert.Equal(t, "invalid_filter_syntax", issues[0].Kind)
}

func TestValidateTargetedResponse_InvalidFilterField(t *testing.T) {
	resp := &TargetedResponse{
		Values: map[string]any{},
		Selections: map[string]TargetedSelection{
			"process.item": {Strategy: "match", Filter: "departureDate == '2026-03-10'"},
		},
		Assertions: map[string][]TargetedAssertion{},
	}

	selContexts := []SelectionContext{
		{
			StepID:        "process",
			SelectionName: "item",
			ElementFields: []string{"carrier (string)", "stops (integer)"},
		},
	}

	issues := validateTargetedResponse(resp, map[string]bool{}, nil, selContexts)

	require.Len(t, issues, 1)
	assert.Equal(t, "invalid_filter_field", issues[0].Kind)
	assert.Contains(t, issues[0].Message, "departureDate")
}

func TestValidateTargetedResponse_ValidFilterField(t *testing.T) {
	resp := &TargetedResponse{
		Values: map[string]any{},
		Selections: map[string]TargetedSelection{
			"process.item": {Strategy: "match", Filter: "carrier == 'UA'"},
		},
		Assertions: map[string][]TargetedAssertion{},
	}

	selContexts := []SelectionContext{
		{
			StepID:        "process",
			SelectionName: "item",
			ElementFields: []string{"carrier (string)", "stops (integer)"},
		},
	}

	issues := validateTargetedResponse(resp, map[string]bool{}, nil, selContexts)
	assert.Empty(t, issues)
}

func TestValidateTargetedResponse_MultipleIssues(t *testing.T) {
	resp := &TargetedResponse{
		Values: map[string]any{
			"search.origin": map[string]any{"from": "x"},
		},
		Selections: map[string]TargetedSelection{
			"process.item": {Strategy: "bogus"},
		},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{
		"search.origin":      true,
		"search.destination": true,
	}

	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string"},
		{StepID: "search", InputName: "destination", InputType: "string"},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)

	// Should find: missing destination, non-literal origin, invalid strategy
	assert.Len(t, issues, 3)

	kinds := map[string]bool{}
	for _, iss := range issues {
		kinds[iss.Kind] = true
	}
	assert.True(t, kinds["missing_value"])
	assert.True(t, kinds["non_literal_value"])
	assert.True(t, kinds["invalid_strategy"])
}

func TestValidateTargetedResponse_WiredInputsIgnored(t *testing.T) {
	// Values for non-unfed inputs should not trigger non_literal_value checks.
	resp := &TargetedResponse{
		Values: map[string]any{
			"process.itemId": map[string]any{"from": "search.id"},
		},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	// itemId is NOT in unfedSet — it's wired.
	unfedSet := map[string]bool{}

	issues := validateTargetedResponse(resp, unfedSet, nil, nil)
	assert.Empty(t, issues)
}

func TestValidateTargetedResponse_ValidStrategiesAccepted(t *testing.T) {
	strategies := []string{"first", "last", "random", "index", "min", "max", "match", "llm", ""}
	for _, s := range strategies {
		resp := &TargetedResponse{
			Values: map[string]any{},
			Selections: map[string]TargetedSelection{
				"step.sel": {Strategy: s},
			},
			Assertions: map[string][]TargetedAssertion{},
		}

		issues := validateTargetedResponse(resp, map[string]bool{}, nil, nil)
		assert.Empty(t, issues, "strategy %q should be valid", s)
	}
}

func TestFormatValidationIssues(t *testing.T) {
	issues := []TargetedValidationIssue{
		{Key: "a.b", Kind: "missing_value", Message: "a.b: missing"},
		{Key: "c.d", Kind: "non_literal_value", Message: "c.d: got object"},
	}

	msgs := formatValidationIssues(issues)

	require.Len(t, msgs, 2)
	assert.Equal(t, "a.b: missing", msgs[0])
	assert.Equal(t, "c.d: got object", msgs[1])
}

// --- Enhanced validation tests (Sub-task D) ---

func TestValidateTargetedResponse_PatternMismatch(t *testing.T) {
	resp := &TargetedResponse{
		Values:     map[string]any{"search.origin": "den"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{"search.origin": true}
	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string", ConstraintPattern: "^[A-Z]{3}$"},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)
	require.Len(t, issues, 1)
	assert.Equal(t, "pattern_mismatch", issues[0].Kind)
	assert.Contains(t, issues[0].Message, "den")
	assert.Contains(t, issues[0].Message, "^[A-Z]{3}$")
}

func TestValidateTargetedResponse_PatternMatch(t *testing.T) {
	resp := &TargetedResponse{
		Values:     map[string]any{"search.origin": "DEN"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{"search.origin": true}
	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string", ConstraintPattern: "^[A-Z]{3}$"},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)
	assert.Empty(t, issues)
}

func TestValidateTargetedResponse_PatternSkipsExpr(t *testing.T) {
	// Expression values should NOT be checked against regex patterns.
	resp := &TargetedResponse{
		Values:     map[string]any{"search.departureDate": "{{today + 7 days}}"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{"search.departureDate": true}
	inputContexts := []InputContext{
		{StepID: "search", InputName: "departureDate", InputType: "date", ConstraintPattern: "^\\d{4}-\\d{2}-\\d{2}$"},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)
	assert.Empty(t, issues)
}

func TestValidateTargetedResponse_InvalidExpression(t *testing.T) {
	resp := &TargetedResponse{
		Values:     map[string]any{"search.departureDate": "{{today +}}"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{"search.departureDate": true}
	inputContexts := []InputContext{
		{StepID: "search", InputName: "departureDate", InputType: "date"},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)
	require.Len(t, issues, 1)
	assert.Equal(t, "invalid_expression", issues[0].Kind)
	assert.Contains(t, issues[0].Message, "{{today +}}")
}

func TestValidateTargetedResponse_LengthViolation(t *testing.T) {
	minLen := 3

	resp := &TargetedResponse{
		Values:     map[string]any{"search.origin": "AB"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{"search.origin": true}
	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string", ConstraintMinLength: &minLen},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)
	require.Len(t, issues, 1)
	assert.Equal(t, "length_violation", issues[0].Kind)
	assert.Contains(t, issues[0].Message, "minimum is 3")
}

func TestValidateTargetedResponse_MaxLengthViolation(t *testing.T) {
	maxLen := 3

	resp := &TargetedResponse{
		Values:     map[string]any{"search.origin": "ABCD"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{"search.origin": true}
	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string", ConstraintMaxLength: &maxLen},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)
	require.Len(t, issues, 1)
	assert.Equal(t, "length_violation", issues[0].Kind)
	assert.Contains(t, issues[0].Message, "maximum is 3")
}

func TestValidateTargetedResponse_LengthSkipsExpr(t *testing.T) {
	minLen := 10

	resp := &TargetedResponse{
		Values:     map[string]any{"search.date": "{{today + 7 days}}"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{"search.date": true}
	inputContexts := []InputContext{
		{StepID: "search", InputName: "date", InputType: "date", ConstraintMinLength: &minLen},
	}

	// Expression values should skip length checks.
	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)
	assert.Empty(t, issues)
}

func TestValidateTargetedResponse_NonStringValuesSkipConstraints(t *testing.T) {
	// Numeric values should not be checked against string constraints.
	resp := &TargetedResponse{
		Values:     map[string]any{"search.count": float64(5)},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	minLen := 3
	unfedSet := map[string]bool{"search.count": true}
	inputContexts := []InputContext{
		{StepID: "search", InputName: "count", InputType: "integer", ConstraintPattern: "^\\d+$", ConstraintMinLength: &minLen},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)
	assert.Empty(t, issues)
}

func TestParseTargetedResponse_WrongPlan(t *testing.T) {
	t.Run("wrong_plan_signal_parsed", func(t *testing.T) {
		content := `{"wrongPlan": {"reason": "User asked about hotels, not flights", "suggested": "Hotel Booking"}}`
		resp, err := parseTargetedResponse(content)
		require.NoError(t, err)
		require.NotNil(t, resp.WrongPlan)
		assert.Equal(t, "User asked about hotels, not flights", resp.WrongPlan.Reason)
		assert.Equal(t, "Hotel Booking", resp.WrongPlan.Suggested)
		// Values/Selections/etc should be empty.
		assert.Empty(t, resp.Values)
		assert.Empty(t, resp.Selections)
	})

	t.Run("wrong_plan_without_suggestion", func(t *testing.T) {
		content := `{"wrongPlan": {"reason": "Workflow mismatch"}}`
		resp, err := parseTargetedResponse(content)
		require.NoError(t, err)
		require.NotNil(t, resp.WrongPlan)
		assert.Equal(t, "Workflow mismatch", resp.WrongPlan.Reason)
		assert.Empty(t, resp.WrongPlan.Suggested)
	})

	t.Run("normal_response_has_nil_wrong_plan", func(t *testing.T) {
		content := `{"values": {"step.input": "value"}}`
		resp, err := parseTargetedResponse(content)
		require.NoError(t, err)
		assert.Nil(t, resp.WrongPlan)
		assert.Contains(t, resp.Values, "step.input")
	})
}

// --- applyTargetedResponse pool clearing tests ---

func TestApplyTargetedResponse_ClearsPoolWhenLLMProvides(t *testing.T) {
	// Template has a pool; LLM provides a specific value.
	// Pool should be cleared — the LLM value is authoritative.
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{
					"origin": {Pool: []any{"DEN", "ORD", "SFO"}},
				}},
			},
		},
	}

	resp := &TargetedResponse{
		Values:     map[string]any{"search.origin": "BNA"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{"search.origin": true}

	applyTargetedResponse(skeleton, resp, unfedSet)

	sv := skeleton.Execution.Steps[0].Values["origin"]
	assert.Equal(t, "BNA", sv.Default)
	assert.Nil(t, sv.Pool)
}

func TestApplyTargetedResponse_ClearsPoolPreservesConstraint(t *testing.T) {
	// Template has pool + constraint; LLM provides a value.
	// Pool should be cleared; constraint should be preserved (it validates the LLM value).
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{
					"origin": {
						Pool:       []any{"DEN", "ORD", "SFO"},
						Constraint: "value != 'LAX'",
					},
				}},
			},
		},
	}

	resp := &TargetedResponse{
		Values:     map[string]any{"search.origin": "BNA"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{"search.origin": true}

	applyTargetedResponse(skeleton, resp, unfedSet)

	sv := skeleton.Execution.Steps[0].Values["origin"]
	assert.Equal(t, "BNA", sv.Default)
	assert.Nil(t, sv.Pool)
	assert.Equal(t, "value != 'LAX'", sv.Constraint)
}

func TestApplyTargetedResponse_NewInputNoExisting(t *testing.T) {
	// When no existing StepValue, should create a fresh one with just Default.
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{}},
			},
		},
	}

	resp := &TargetedResponse{
		Values:     map[string]any{"search.origin": "JFK"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{"search.origin": true}

	applyTargetedResponse(skeleton, resp, unfedSet)

	sv := skeleton.Execution.Steps[0].Values["origin"]
	assert.Equal(t, "JFK", sv.Default)
	assert.Nil(t, sv.Pool)
}

// --- buildInputContexts IsPoolInput tests ---

func TestBuildInputContexts_IsPoolInput(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search", Adapter: "a",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "destination", Type: "string"},
					{Name: "carrier", Type: "string"},
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
						"origin":      {Pool: []any{"DEN", "ORD", "SFO"}},       // pool, no default → IsPoolInput
						"destination": {Default: "LHR"},                         // default, no pool → not IsPoolInput
						"carrier":     {Pool: []any{"UA", "AA"}, Default: "UA"}, // pool + default → not IsPoolInput
					},
				},
			},
		},
	}

	contexts := buildInputContexts(p, g, nil, nil, nil)

	require.Len(t, contexts, 3)

	var originCtx, destCtx, carrierCtx *InputContext
	for i, ic := range contexts {
		switch ic.InputName {
		case "origin":
			originCtx = &contexts[i]
		case "destination":
			destCtx = &contexts[i]
		case "carrier":
			carrierCtx = &contexts[i]
		}
	}

	require.NotNil(t, originCtx)
	assert.True(t, originCtx.IsPoolInput, "pool-only input should be IsPoolInput")
	assert.True(t, originCtx.HasTemplatePool)

	require.NotNil(t, destCtx)
	assert.False(t, destCtx.IsPoolInput, "default-only input should not be IsPoolInput")

	require.NotNil(t, carrierCtx)
	assert.False(t, carrierCtx.IsPoolInput, "pool+default input should not be IsPoolInput")
}

func TestBuildInputContexts_FromResolvedInContexts(t *testing.T) {
	// fromResolved inputs should appear in contexts with the FromResolved
	// field set, so the LLM can override them when user intent conflicts.
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"process": {
				Name: "process", Description: "Process", Adapter: "a",
				Inputs: []graph.Input{
					{Name: "destination", Type: "string"},
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
						"destination": {FromResolved: "leg1Destination"},
						// origin has no value — should also appear in contexts
					},
				},
			},
		},
	}

	contexts := buildInputContexts(p, g, nil, nil, nil)

	// Both inputs should appear: destination (fromResolved) and origin (unfed).
	require.Len(t, contexts, 2)

	var destCtx, originCtx *InputContext
	for i, ic := range contexts {
		switch ic.InputName {
		case "destination":
			destCtx = &contexts[i]
		case "origin":
			originCtx = &contexts[i]
		}
	}

	require.NotNil(t, destCtx)
	assert.Equal(t, "leg1Destination", destCtx.FromResolved)

	require.NotNil(t, originCtx)
	assert.Empty(t, originCtx.FromResolved)
}

func TestBuildInputContexts_GraphLevelFromResolved(t *testing.T) {
	// When a graph input has a fromResolved default (e.g., leg2Origin defaults
	// to leg1Destination), the InputContext.FromResolved field should be
	// populated even when the step template doesn't explicitly set it.
	// This ensures the input is classified as "Auto-Wired" in the prompt,
	// so the LLM doesn't redundantly provide a value.
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search flights", Adapter: "a",
				Inputs: []graph.Input{
					{Name: "leg1Origin", Type: "string", Configurable: true},
					{Name: "leg1Destination", Type: "string", Configurable: true},
					{
						Name: "leg2Origin", Type: "string", Configurable: true,
						Optional: true,
						Default:  &graph.InputDefault{FromResolved: "leg1Destination"},
					},
					{
						Name: "leg2Destination", Type: "string", Configurable: true,
						Optional: true,
						Default: &graph.InputDefault{
							Pool: []any{"SFO", "LAX"},
						},
					},
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
						// Only leg2Destination has step-level fromResolved;
						// leg2Origin relies on graph-level default.
						"leg2Destination": {FromResolved: "leg1Origin"},
					},
				},
			},
		},
	}

	contexts := buildInputContexts(p, g, nil, nil, nil)

	ctxMap := map[string]*InputContext{}
	for i, ic := range contexts {
		ctxMap[ic.InputName] = &contexts[i]
	}

	// leg2Destination: step-level fromResolved
	require.NotNil(t, ctxMap["leg2Destination"])
	assert.Equal(t, "leg1Origin", ctxMap["leg2Destination"].FromResolved)

	// leg2Origin: graph-level fromResolved should be picked up
	require.NotNil(t, ctxMap["leg2Origin"])
	assert.Equal(t, "leg1Destination", ctxMap["leg2Origin"].FromResolved,
		"graph-level fromResolved should populate InputContext.FromResolved")

}

func TestClassifyInputs_AutoWiredBeatsLayerHandled(t *testing.T) {
	// When an input has both FromResolved (auto-wired) and LayerHandled,
	// it should be classified as AutoWired. The structural wiring (e.g.,
	// round-trip return to origin) takes priority over layer-provided pools
	// (e.g., international city pool) to avoid confusing the LLM.
	contexts := []InputContext{
		{StepID: "s1", InputName: "leg1Origin", LayerHandled: true},
		{StepID: "s1", InputName: "leg2Destination", FromResolved: "leg1Origin", LayerHandled: true},
		{StepID: "s1", InputName: "leg2Origin", FromResolved: "leg1Destination"},
		{StepID: "s1", InputName: "cabinPreference", IsConfigurable: true},
	}

	c := classifyInputs(contexts)

	// leg2Destination: has both FromResolved and LayerHandled → AutoWired wins
	require.Len(t, c.AutoWired, 2)
	assert.Equal(t, "leg2Destination", c.AutoWired[0].InputName)
	assert.Equal(t, "leg2Origin", c.AutoWired[1].InputName)

	// leg1Origin: layer-handled only
	require.Len(t, c.LayerHandled, 1)
	assert.Equal(t, "leg1Origin", c.LayerHandled[0].InputName)

	// cabinPreference: configurable
	require.Len(t, c.Configurable, 1)
	assert.Equal(t, "cabinPreference", c.Configurable[0].InputName)
}

// --- fromResolved override tests ---

func TestApplyTargetedResponse_ClearsFromResolved(t *testing.T) {
	// When the LLM overrides a fromResolved input, FromResolved must be
	// cleared so the engine uses the LLM's value instead of auto-wiring.
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search2", Values: map[string]plan.StepValue{
					"leg2Origin": {FromResolved: "leg1Destination"},
				}},
			},
		},
	}

	resp := &TargetedResponse{
		Values:     map[string]any{"search2.leg2Origin": "BNA"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{"search2.leg2Origin": true}

	applyTargetedResponse(skeleton, resp, unfedSet)

	sv := skeleton.Execution.Steps[0].Values["leg2Origin"]
	assert.Equal(t, "BNA", sv.Default)
	assert.Empty(t, sv.FromResolved, "FromResolved should be cleared when LLM overrides")
}

func TestApplyTargetedResponse_FromResolvedPreservedWhenNotOverridden(t *testing.T) {
	// When the LLM doesn't provide a value for a fromResolved input,
	// the FromResolved wiring should remain intact.
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search2", Values: map[string]plan.StepValue{
					"leg2Origin": {FromResolved: "leg1Destination"},
				}},
			},
		},
	}

	resp := &TargetedResponse{
		Values:     map[string]any{}, // LLM doesn't override
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{"search2.leg2Origin": true}

	applyTargetedResponse(skeleton, resp, unfedSet)

	sv := skeleton.Execution.Steps[0].Values["leg2Origin"]
	assert.Equal(t, "leg1Destination", sv.FromResolved, "FromResolved should be preserved when LLM doesn't override")
	assert.Nil(t, sv.Default)
}

func TestApplyTargetedResponse_LockedInputsRejected(t *testing.T) {
	// Locked inputs should not be overridden by the LLM, even if the LLM
	// provides a value for them. Since locked inputs are excluded from unfedSet
	// by isInputFed, the unfedSet check in applyTargetedResponse rejects them.
	skeleton := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{
					"leg2Origin":      {FromResolved: "leg1Destination", Locked: true},
					"leg2Destination": {FromResolved: "leg1Origin", Locked: true},
					"origin":          {Default: "DEN"},
				}},
			},
		},
	}

	resp := &TargetedResponse{
		Values: map[string]any{
			"search.leg2Origin":      "BNA", // should be rejected
			"search.leg2Destination": "BNA", // should be rejected
			"search.origin":          "ORD", // should be accepted
		},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	// Only origin is unfed — locked inputs are excluded by isInputFed.
	unfedSet := map[string]bool{"search.origin": true}

	applyTargetedResponse(skeleton, resp, unfedSet)

	// Locked inputs should be untouched.
	assert.Equal(t, "leg1Destination", skeleton.Execution.Steps[0].Values["leg2Origin"].FromResolved)
	assert.True(t, skeleton.Execution.Steps[0].Values["leg2Origin"].Locked)
	assert.Nil(t, skeleton.Execution.Steps[0].Values["leg2Origin"].Default)

	assert.Equal(t, "leg1Origin", skeleton.Execution.Steps[0].Values["leg2Destination"].FromResolved)
	assert.True(t, skeleton.Execution.Steps[0].Values["leg2Destination"].Locked)
	assert.Nil(t, skeleton.Execution.Steps[0].Values["leg2Destination"].Default)

	// Non-locked input should be overridden.
	assert.Equal(t, "ORD", skeleton.Execution.Steps[0].Values["origin"].Default)
}

func TestBuildTargetedPlanPrompt_AutoWiredSection(t *testing.T) {
	inputContexts := []InputContext{
		{StepID: "search1", InputName: "origin", InputType: "string"},
		{StepID: "search2", InputName: "leg2Origin", InputType: "string", FromResolved: "leg1Destination", DomainType: "IATA airport code"},
		{StepID: "search1", InputName: "carrier", InputType: "string", IsConfigurable: true},
	}

	_, user := buildTargetedPlanPrompt(inputContexts, nil, "fly from Nashville on both legs", nil, time.Now())

	// Auto-wired section should appear.
	assert.Contains(t, user, "## Auto-Wired Inputs")
	assert.Contains(t, user, "derived at runtime")
	assert.Contains(t, user, "automatically derived from sibling inputs")

	// The auto-wired input should show its source.
	assert.Contains(t, user, "### search2.leg2Origin (string)")
	assert.Contains(t, user, "Auto-wired from: leg1Destination")
	assert.Contains(t, user, "Domain: IATA airport code")

	// Section ordering: required < pool < auto-wired < configurable.
	requiredIdx := strings.Index(user, "## Inputs That Need Values")
	autoWiredIdx := strings.Index(user, "## Auto-Wired Inputs")
	configIdx := strings.Index(user, "## Optional Configuration")
	assert.Greater(t, autoWiredIdx, requiredIdx)
	assert.Less(t, autoWiredIdx, configIdx)

	// The auto-wired input should NOT be in the required section.
	leg2Idx := strings.Index(user, "### search2.leg2Origin")
	assert.Greater(t, leg2Idx, autoWiredIdx)
}

func TestValidateTargetedResponse_FromResolvedMissing_NoError(t *testing.T) {
	// Missing value for a fromResolved input should NOT produce a missing_value issue.
	resp := &TargetedResponse{
		Values:     map[string]any{"search1.origin": "BNA"},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	unfedSet := map[string]bool{
		"search1.origin":     true,
		"search2.leg2Origin": true,
	}

	inputContexts := []InputContext{
		{StepID: "search1", InputName: "origin", InputType: "string"},
		{StepID: "search2", InputName: "leg2Origin", InputType: "string", FromResolved: "leg1Destination"},
	}

	issues := validateTargetedResponse(resp, unfedSet, inputContexts, nil)
	assert.Empty(t, issues)
}

// --- InputDescription tests ---

func TestBuildInputContexts_InputDescription(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search flights", Adapter: "a",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string", Description: "Departure airport/city code (3-letter IATA)"},
					{Name: "destination", Type: "string"},
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

	contexts := buildInputContexts(p, g, nil, nil, nil)

	require.Len(t, contexts, 2)
	assert.Equal(t, "Departure airport/city code (3-letter IATA)", contexts[0].InputDescription)
	assert.Empty(t, contexts[1].InputDescription)
}

func TestBuildTargetedPlanPrompt_InputDescription(t *testing.T) {
	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string", InputDescription: "Departure airport/city code (3-letter IATA)"},
		{StepID: "search", InputName: "destination", InputType: "string"},
	}

	_, user := buildTargetedPlanPrompt(inputContexts, nil, "book a flight", nil, time.Now())

	assert.Contains(t, user, "Purpose: Departure airport/city code (3-letter IATA)")
	// Input without description should not have a Purpose line.
	destIdx := strings.Index(user, "### search.destination")
	nextSection := strings.Index(user[destIdx:], "\n\n")
	destBlock := user[destIdx : destIdx+nextSection]
	assert.NotContains(t, destBlock, "Purpose:")
}

// --- Node group header tests ---

func TestBuildTargetedPlanPrompt_NodeGroupHeaders(t *testing.T) {
	inputContexts := []InputContext{
		{StepID: "searchFlights", InputName: "origin", InputType: "string", NodeDesc: "Search for flights"},
		{StepID: "searchFlights", InputName: "destination", InputType: "string", NodeDesc: "Search for flights"},
		{StepID: "addOffer", InputName: "offerId", InputType: "string", NodeDesc: "Add offer to cart"},
	}

	_, user := buildTargetedPlanPrompt(inputContexts, nil, "book a flight", nil, time.Now())

	// Node headers should appear once per node.
	assert.Contains(t, user, "**searchFlights** — Search for flights")
	assert.Contains(t, user, "**addOffer** — Add offer to cart")

	// Header should appear only once per section even with multiple inputs.
	count := strings.Count(user, "**searchFlights** — Search for flights")
	assert.Equal(t, 1, count)
}

func TestBuildTargetedPlanPrompt_NodeGroupHeaders_AcrossSections(t *testing.T) {
	// A node may appear in multiple sections — header shows once per section.
	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string", NodeDesc: "Search flights"},
		{StepID: "search", InputName: "airportCode", InputType: "string", NodeDesc: "Search flights", IsPoolInput: true, HasTemplatePool: true, PoolValues: []string{"DEN"}},
	}

	_, user := buildTargetedPlanPrompt(inputContexts, nil, "book a flight", nil, time.Now())

	// Header should appear in both required and pool sections.
	count := strings.Count(user, "**search** — Search flights")
	assert.Equal(t, 2, count)
}

func TestBuildTargetedPlanPrompt_NodeGroupHeaders_NoDesc(t *testing.T) {
	// Nodes without a description should not get a header.
	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string", NodeDesc: ""},
	}

	_, user := buildTargetedPlanPrompt(inputContexts, nil, "book a flight", nil, time.Now())

	assert.NotContains(t, user, "**search**")
}

// --- Pool values hidden tests ---

func TestBuildTargetedPlanPrompt_PoolValuesShown(t *testing.T) {
	inputContexts := []InputContext{
		{
			StepID: "search", InputName: "origin", InputType: "string",
			InputDescription: "Departure airport code",
			IsPoolInput:      true, HasTemplatePool: true,
			PoolValues:  []string{"DEN", "ORD", "ATL", "DFW", "IAH"},
			GraphConstr: "IATA airport/city code (pattern: ^[A-Z]{3}$)",
		},
	}

	_, user := buildTargetedPlanPrompt(inputContexts, nil, "book a flight", nil, time.Now())

	// Pool values should now appear as reference for user intent mapping.
	assert.Contains(t, user, "Pool (random at runtime): DEN, ORD, ATL, DFW, IAH")

	// Purpose and Validation should still appear.
	assert.Contains(t, user, "Purpose: Departure airport code")
	assert.Contains(t, user, "Validation: IATA airport/city code (pattern: ^[A-Z]{3}$)")

	// Pool reference guidance should appear.
	assert.Contains(t, user, "reference samples for mapping user intent")
}

// --- Pool date hint conditioning ---

func TestBuildTargetedPlanPrompt_PoolDateHintSoft(t *testing.T) {
	inputContexts := []InputContext{
		{StepID: "search", InputName: "departureDate", InputType: "date", IsDate: true, IsPoolInput: true, HasTemplatePool: true},
		{StepID: "search", InputName: "returnDate", InputType: "date", IsDate: true},
	}

	_, user := buildTargetedPlanPrompt(inputContexts, nil, "book a flight", nil, time.Now())

	// Pool date input should have softer hint.
	assert.Contains(t, user, "Date field — if providing a value, use {{today + N days}} syntax")

	// Non-pool date input should have the standard hint.
	assert.Contains(t, user, "Date field — use {{today + N days}}")
}

// --- System prompt pool guidance ---

func TestBuildTargetedPlanPrompt_SystemPromptPoolGuidance(t *testing.T) {
	system, _ := buildTargetedPlanPrompt(nil, nil, "test", nil, time.Now())

	// System prompt should contain pool-specific guidance.
	assert.Contains(t, system, "Pool inputs auto-select at runtime")
	assert.Contains(t, system, "include a pool input only when the user specifies")
}

// --- User's Prompt section rename ---

func TestBuildTargetedPlanPrompt_UserPromptSection(t *testing.T) {
	_, user := buildTargetedPlanPrompt(nil, nil, "book a flight from JFK", nil, time.Now())

	assert.Contains(t, user, "## User's Prompt")
	assert.NotContains(t, user, "## User Intent")
	assert.Contains(t, user, "book a flight from JFK")
}

// --- Layer-handled input tests ---

func TestBuildInputContexts_LayerHandled(t *testing.T) {
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

	layerTouched := map[string]bool{
		"search.origin":      true,
		"search.destination": true,
	}

	contexts := buildInputContexts(p, g, nil, layerTouched, nil)
	require.Len(t, contexts, 3)

	// origin and destination should be layer-handled.
	var originCtx, destCtx, dateCtx *InputContext
	for i, ic := range contexts {
		switch ic.InputName {
		case "origin":
			originCtx = &contexts[i]
		case "destination":
			destCtx = &contexts[i]
		case "departureDate":
			dateCtx = &contexts[i]
		}
	}

	require.NotNil(t, originCtx)
	assert.True(t, originCtx.LayerHandled)

	require.NotNil(t, destCtx)
	assert.True(t, destCtx.LayerHandled)

	require.NotNil(t, dateCtx)
	assert.False(t, dateCtx.LayerHandled)
}

func TestBuildInputContexts_NilLayerOverrides(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search", Adapter: "a",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
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

	contexts := buildInputContexts(p, g, nil, nil, nil)
	require.Len(t, contexts, 1)
	assert.False(t, contexts[0].LayerHandled)
}

func TestBuildInputContexts_LayerHandledWithPoolValues(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search flights", Adapter: "a",
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
				{Node: "search", Values: map[string]plan.StepValue{}},
			},
		},
	}

	layerTouched := map[string]bool{
		"search.origin":      true,
		"search.destination": true,
	}
	layerDefaults := map[string]*graph.InputDefault{
		"search.origin": {
			Pool: []any{"ORD", "MDW", "RFD"},
		},
		"search.destination": {
			Pool: []any{"CDG", "LHR", "FRA"},
		},
	}

	contexts := buildInputContexts(p, g, nil, layerTouched, layerDefaults)
	require.Len(t, contexts, 2)

	var originCtx, destCtx *InputContext
	for i, ic := range contexts {
		switch ic.InputName {
		case "origin":
			originCtx = &contexts[i]
		case "destination":
			destCtx = &contexts[i]
		}
	}

	require.NotNil(t, originCtx)
	assert.True(t, originCtx.LayerHandled)
	assert.True(t, originCtx.HasTemplatePool)
	assert.Equal(t, []string{"ORD", "MDW", "RFD"}, originCtx.PoolValues)

	require.NotNil(t, destCtx)
	assert.True(t, destCtx.LayerHandled)
	assert.True(t, destCtx.HasTemplatePool)
	assert.Equal(t, []string{"CDG", "LHR", "FRA"}, destCtx.PoolValues)
}

func TestBuildTargetedPlanPrompt_LayerHandledSection(t *testing.T) {
	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string", LayerHandled: true,
			PoolValues: []string{"ORD", "MDW"}, HasTemplatePool: true},
		{StepID: "search", InputName: "destination", InputType: "string", LayerHandled: true},
		{StepID: "search", InputName: "departureDate", InputType: "date", IsDate: true},
	}

	_, user := buildTargetedPlanPrompt(inputContexts, nil, "book a flight", nil, time.Now())

	// Layer-handled inputs should appear in their own section with full context blocks.
	assert.Contains(t, user, "## Layer-Handled Inputs")
	assert.Contains(t, user, "### search.origin (string)")
	assert.Contains(t, user, "### search.destination (string)")
	assert.Contains(t, user, "Pool (random at runtime): ORD, MDW")

	// Guard-rails should be present.
	assert.Contains(t, user, "Include one only when the user's prompt specifies a concrete value")
	assert.Contains(t, user, "reference samples for mapping user intent")

	// departureDate should appear in "Inputs That Need Values".
	assert.Contains(t, user, "## Inputs That Need Values")
	assert.Contains(t, user, "### search.departureDate (date)")
}

func TestBuildTargetedPlanPrompt_LayerHandledExcludedFromRequired(t *testing.T) {
	// All inputs are layer-handled — "Inputs That Need Values" should not appear.
	inputContexts := []InputContext{
		{StepID: "search", InputName: "origin", InputType: "string", LayerHandled: true},
		{StepID: "search", InputName: "destination", InputType: "string", LayerHandled: true},
	}

	_, user := buildTargetedPlanPrompt(inputContexts, nil, "book a flight", nil, time.Now())

	assert.Contains(t, user, "## Layer-Handled Inputs")
	assert.Contains(t, user, "### search.origin (string)")
	assert.Contains(t, user, "### search.destination (string)")
	assert.NotContains(t, user, "## Inputs That Need Values")
}

func TestBuildTargetedPlanPrompt_SelectedConfiguration(t *testing.T) {
	ws := &WorkflowSelection{
		Workflow:    "Booking",
		Description: "book a round trip in business class",
		Choices:     map[string]string{"trip-search": "Round-Trip", "payment": "Cash"},
		Addons:      []string{"Seat Selection"},
		Layers:      []string{"european", "premium"},
	}

	_, user := buildTargetedPlanPrompt(nil, nil, "book a flight", ws, time.Now())

	assert.Contains(t, user, "## Selected Configuration")
	assert.Contains(t, user, "Workflow: Booking (book a round trip in business class)")
	assert.Contains(t, user, "- Seat Selection")
	assert.Contains(t, user, "- european")
	assert.Contains(t, user, "- premium")
}

func TestBuildTargetedPlanPrompt_SelectedConfigurationMinimal(t *testing.T) {
	ws := &WorkflowSelection{
		Workflow:    "Search Only",
		Description: "search for flights",
	}

	_, user := buildTargetedPlanPrompt(nil, nil, "search flights", ws, time.Now())

	assert.Contains(t, user, "## Selected Configuration")
	assert.Contains(t, user, "Workflow: Search Only (search for flights)")
	assert.NotContains(t, user, "Choices:")
	assert.NotContains(t, user, "Addons:")
	assert.NotContains(t, user, "Layers:")
}

func TestBuildTargetedPlanPrompt_NilWorkflowSelection(t *testing.T) {
	_, user := buildTargetedPlanPrompt(nil, nil, "test", nil, time.Now())

	assert.NotContains(t, user, "## Selected Configuration")
	assert.NotContains(t, user, "## Layer-Handled Inputs")
}

func TestBuildTargetedPlanPrompt_SystemPromptLayerMention(t *testing.T) {
	system, _ := buildTargetedPlanPrompt(nil, nil, "test", nil, time.Now())
	assert.Contains(t, system, "data layers")
}

func TestBuildTargetedPlanPrompt_WrongPlanPoolGuardrails(t *testing.T) {
	system, _ := buildTargetedPlanPrompt(nil, nil, "test", nil, time.Now())

	// Simplified wrong workflow section — adapting values is the LLM's normal job.
	assert.Contains(t, system, "Adapting values, pools, and layer data to match user intent is your normal job")
	assert.Contains(t, system, "fundamental domain mismatches")
}

func TestBuildTargetedPlanPrompt_WrongPlanPresentByDefault(t *testing.T) {
	system, _ := buildTargetedPlanPrompt(nil, nil, "test", nil, time.Now())

	assert.Contains(t, system, "## Wrong Workflow")
	assert.Contains(t, system, "wrongPlan")
}

func TestBuildTargetedPlanPrompt_SuppressWrongPlan(t *testing.T) {
	system, _ := buildTargetedPlanPrompt(nil, nil, "test", nil, time.Now(), withSuppressWrongPlan())

	assert.NotContains(t, system, "## Wrong Workflow")
	assert.NotContains(t, system, "wrongPlan")
}

func TestBuildTargetedPlanPrompt_SelectedConfigurationWithDescriptions(t *testing.T) {
	g := &graph.Graph{
		Workflows: []graph.Workflow{
			{Name: "Booking", Description: "Book a round trip", Template: "t.yaml", Slots: []graph.SlotDef{
				{Name: "trip-search", Description: "How to search for and price flights", Options: []string{"Round-Trip"}, Default: "Round-Trip"},
			}},
			{Name: "Round-Trip", Kind: "slot", Description: "Two-leg roundtrip search", Template: "rt.yaml"},
			{Name: "Seat Selection", Kind: "addon", Description: "Add seat assignment", Template: "seat.yaml", After: graph.AfterSpec{"book"}},
		},
	}
	layers := map[string]*graph.Layer{
		"european": {Name: "european", Description: "European routes with CDG/LHR/FRA airports"},
	}
	ws := &WorkflowSelection{
		Workflow:    "Booking",
		Description: "book a round trip",
		Choices:     map[string]string{"trip-search": "Round-Trip"},
		Addons:      []string{"Seat Selection"},
		Layers:      []string{"european"},
	}

	_, user := buildTargetedPlanPrompt(nil, nil, "book a flight", ws, time.Now(),
		withGraph(g), withAvailableLayers(layers))

	// Choice should have both slot description and option description.
	assert.Contains(t, user, "trip-search (How to search for and price flights) → Round-Trip: Two-leg roundtrip search")
	// Addon should have its description.
	assert.Contains(t, user, "Seat Selection: Add seat assignment")
	// Layer should have its description.
	assert.Contains(t, user, "european: European routes with CDG/LHR/FRA airports")
}

func TestBuildTargetedPlanPrompt_SelectedConfigurationNoGraph(t *testing.T) {
	// Without graph/layers, descriptions are omitted (bare names).
	ws := &WorkflowSelection{
		Workflow:    "Booking",
		Description: "book a flight",
		Choices:     map[string]string{"trip-search": "Round-Trip"},
		Addons:      []string{"Seat Selection"},
		Layers:      []string{"european"},
	}

	_, user := buildTargetedPlanPrompt(nil, nil, "book a flight", ws, time.Now())

	assert.Contains(t, user, "- trip-search → Round-Trip\n")
	assert.Contains(t, user, "- Seat Selection\n")
	assert.Contains(t, user, "- european\n")
	// No descriptions appended.
	assert.NotContains(t, user, "Round-Trip:")
	assert.NotContains(t, user, "Seat Selection:")
	assert.NotContains(t, user, "european:")
}

func TestBuildTargetedPlanPrompt_SelectedConfigurationSlotDescriptionMissing(t *testing.T) {
	// Base workflow has no SlotDef for the chosen slot — should fall back to option-only format.
	g := &graph.Graph{
		Workflows: []graph.Workflow{
			{Name: "Booking", Description: "Book a round trip", Template: "t.yaml"},
			{Name: "Round-Trip", Kind: "slot", Description: "Two-leg roundtrip search", Template: "rt.yaml"},
		},
	}
	ws := &WorkflowSelection{
		Workflow:    "Booking",
		Description: "book a round trip",
		Choices:     map[string]string{"trip-search": "Round-Trip"},
	}

	_, user := buildTargetedPlanPrompt(nil, nil, "book a flight", ws, time.Now(),
		withGraph(g))

	// Option description present, no slot description parenthetical.
	assert.Contains(t, user, "- trip-search → Round-Trip: Two-leg roundtrip search")
	assert.NotContains(t, user, "trip-search (")
}
