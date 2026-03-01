package intent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubClient is a test LLM client that returns canned responses in order.
type stubClient struct {
	calls     []*llm.Request
	responses []string
	callIndex int
	err       error
}

func (s *stubClient) Complete(_ context.Context, req *llm.Request) (*llm.Response, error) {
	s.calls = append(s.calls, req)
	if s.err != nil {
		return nil, s.err
	}
	if s.callIndex >= len(s.responses) {
		return &llm.Response{Content: ""}, nil
	}
	resp := s.responses[s.callIndex]
	s.callIndex++
	return &llm.Response{
		Content:      resp,
		Model:        "test-model",
		InputTokens:  100,
		OutputTokens: 50,
		FinishReason: "stop",
	}, nil
}

func TestInterpret_EmptyPrompt(t *testing.T) {
	g, _ := buildTemplateTestGraph(t)
	client := &stubClient{}

	_, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "",
		Graph:  g,
		Client: client,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt is required")
}

func TestInterpret_NilGraph(t *testing.T) {
	client := &stubClient{}

	_, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "test",
		Graph:  nil,
		Client: client,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "graph is required")
}

func TestInterpret_NilClient(t *testing.T) {
	g, _ := buildTemplateTestGraph(t)

	_, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "test",
		Graph:  g,
		Client: nil,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "LLM client is required")
}

func TestInterpret_LLMReturnsGarbage(t *testing.T) {
	g, _ := buildTemplateTestGraph(t)

	client := &stubClient{
		responses: []string{
			// First call: garbage that cannot be parsed as JSON
			"not valid json at all {{{",
		},
	}

	_, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "book an item",
		Graph:  g,
		Client: client,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow selection")
}

func TestInterpret_LLMError(t *testing.T) {
	g, _ := buildTemplateTestGraph(t)

	client := &stubClient{
		err: assert.AnError,
	}

	_, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "book an item",
		Graph:  g,
		Client: client,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow selection")
}

func TestInterpret_UnknownWorkflow(t *testing.T) {
	g, _ := buildTemplateTestGraph(t)

	wsJSON := `{"workflow": "Nonexistent Workflow", "description": "test", "constraints": {"hard": [], "soft": [], "free": []}}`

	client := &stubClient{
		responses: []string{wsJSON},
	}

	_, err := Interpret(context.Background(), InterpretRequest{
		Prompt:   "book an item",
		Graph:    g,
		Client:   client,
		GraphDir: ".",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown or template-less workflow")
	assert.Contains(t, err.Error(), "Nonexistent Workflow")
}

func TestInterpret_NoWorkflowSelected(t *testing.T) {
	g, _ := buildTemplateTestGraph(t)

	// LLM returns empty workflow name
	wsJSON := `{"workflow": "", "description": "confused", "constraints": {"hard": [], "soft": [], "free": []}}`

	client := &stubClient{
		responses: []string{wsJSON},
	}

	_, err := Interpret(context.Background(), InterpretRequest{
		Prompt:   "book an item",
		Graph:    g,
		Client:   client,
		GraphDir: ".",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no workflow selected")
}

func TestInterpret_WorkflowTemplate(t *testing.T) {
	g, graphDir := buildTemplateTestGraph(t)

	wsJSON := `{"workflow": "Booking Flow", "description": "Book an item", "constraints": {"hard": [], "soft": [], "free": ["query"]}}`

	targetedJSON := `{
		"values": {
			"search.query": "real search query"
		},
		"assertions": {
			"process": [
				{"type": "status", "expect": 200}
			]
		},
		"descriptions": {
			"search": "Search for items",
			"process": "Process the item"
		}
	}`

	client := &stubClient{
		responses: []string{wsJSON, targetedJSON},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:   "book an item",
		Graph:    g,
		Client:   client,
		GraphDir: graphDir,
	})

	require.NoError(t, err)
	require.NotNil(t, result.Plan)

	// Should have used the template — only 2 LLM calls (selection + fill)
	assert.Len(t, client.calls, 2)

	// The plan should have 2 steps (from template)
	assert.Len(t, result.Plan.Execution.Steps, 2)
	assert.Equal(t, "search", result.Plan.Execution.Steps[0].Node)
	assert.Equal(t, "process", result.Plan.Execution.Steps[1].Node)

	// WorkflowSelection should be populated
	require.NotNil(t, result.WorkflowSelection)
	assert.Equal(t, "Booking Flow", result.WorkflowSelection.Workflow)
	assert.Equal(t, "Book an item", result.WorkflowSelection.Description)
}

func TestInterpret_NilKnowledgeBase(t *testing.T) {
	g, graphDir := buildTemplateTestGraph(t)

	wsJSON := `{"workflow": "Booking Flow", "description": "Book an item", "constraints": {"hard": [], "soft": [], "free": ["query"]}}`

	targetedJSON := `{
		"values": {
			"search.query": "test"
		}
	}`

	client := &stubClient{
		responses: []string{wsJSON, targetedJSON},
	}

	// KB is nil — should work fine
	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:   "book an item",
		Graph:    g,
		KB:       nil,
		Client:   client,
		GraphDir: graphDir,
	})

	require.NoError(t, err)
	require.NotNil(t, result.Plan)

	// Second call should NOT contain Domain context in the input context blocks
	// (no KB means no domain enrichment)
	assert.NotContains(t, client.calls[1].Messages[1].Content, "Domain:")
}

func TestInterpret_WithKnowledgeBase(t *testing.T) {
	// Build a graph where an unfed input has a domain type with a value pool.
	dir := t.TempDir()

	// Template with an unfed "origin" input (no default).
	templateContent := `
execution:
  steps:
    - node: search
      values: {}
    - node: process
      dependsOn: [search]
      values:
        itemId: {from: search.resultId}
  cleanup:
    - node: cleanup
      runOn: always
`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "workflows"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflows", "booking.yaml"), []byte(templateContent), 0o644))

	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{Name: "Booking Flow", Template: "workflows/booking.yaml"},
		},
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search for items", Adapter: "a",
				Inputs:  []graph.Input{{Name: "origin", Type: "airportCode"}},
				Outputs: []graph.Output{{Name: "resultId", Type: "string"}},
			},
			"process": {
				Name: "process", Description: "Process selected item", Adapter: "b",
				Inputs:  []graph.Input{{Name: "itemId", Type: "string"}},
				Outputs: []graph.Output{{Name: "result", Type: "string"}},
			},
			"cleanup": {
				Name: "cleanup", Description: "Clean up", Adapter: "c",
				Inputs: []graph.Input{}, Outputs: []graph.Output{},
			},
		},
	}

	kbYAML := `
concepts:
  airportCode:
    description: "IATA airport code"
    applies_to: [origin, destination]
types:
  airportCode:
    description: "IATA 3-letter airport code"
    format: "^[A-Z]{3}$"
    pool: airports
valuePools:
  airports:
    description: "Airport codes"
    type: airportCode
    values: [JFK, LAX, LHR, SYD, ORD]
`
	kb, err := loadKBFromYAML(t, kbYAML)
	require.NoError(t, err)

	wsJSON := `{"workflow": "Booking Flow", "description": "Book an item", "constraints": {"hard": [], "soft": [], "free": ["origin"]}}`

	targetedJSON := `{
		"values": {
			"search.origin": "JFK"
		}
	}`

	client := &stubClient{
		responses: []string{wsJSON, targetedJSON},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:   "book a flight from JFK",
		Graph:    g,
		KB:       kb,
		Client:   client,
		GraphDir: dir,
	})

	require.NoError(t, err)
	require.NotNil(t, result.Plan)

	// Second call should contain per-input domain enrichment for origin
	userMsg := client.calls[1].Messages[1].Content
	assert.Contains(t, userMsg, "search.origin")
	assert.Contains(t, userMsg, "IATA airport code")
}

func TestInterpret_DateInPrompts(t *testing.T) {
	g, graphDir := buildTemplateTestGraph(t)

	wsJSON := `{"workflow": "Booking Flow", "description": "Book an item", "constraints": {"hard": [], "soft": [], "free": ["query"]}}`

	targetedJSON := `{
		"values": {
			"search.query": "test"
		}
	}`

	client := &stubClient{
		responses: []string{wsJSON, targetedJSON},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:   "book an item",
		Graph:    g,
		Client:   client,
		GraphDir: graphDir,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, client.calls, 2)

	// Today's date should appear in both LLM prompts
	today := time.Now().Format("2006-01-02")

	// First call (workflow selection): date in system message
	assert.Contains(t, client.calls[0].Messages[0].Content, today,
		"workflow selection system prompt should contain today's date")

	// Second call (targeted plan generation): date in system message
	assert.Contains(t, client.calls[1].Messages[0].Content, today,
		"targeted plan generation system prompt should contain today's date")
}

func TestStripJSONFencing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no fencing", `{"goal": "test"}`, `{"goal": "test"}`},
		{"json fencing", "```json\n{\"goal\": \"test\"}\n```", `{"goal": "test"}`},
		{"generic fencing", "```\n{\"goal\": \"test\"}\n```", `{"goal": "test"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripJSONFencing(tt.input))
		})
	}
}

// loadKBFromYAML is a test helper to parse a KB from inline YAML.
func loadKBFromYAML(t *testing.T, yamlStr string) (*domain.KnowledgeBase, error) {
	t.Helper()
	return domain.Parse([]byte(yamlStr))
}

// buildMismatchGraph creates a minimal graph where an input name ("origin")
// doesn't match any elementField name on the source output. This triggers
// the lookupElementFieldPath fallback, producing a broken fromSelection
// that validation catches — exercising the retry path.
func buildMismatchGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"search": {
				Name:        "search",
				Description: "Search for items",
				Adapter:     "searchAdapter",
				Inputs: []graph.Input{
					{Name: "query", Type: "string"},
				},
				Outputs: []graph.Output{
					{
						Name: "items",
						Type: "item[]",
						ElementFields: []graph.Field{
							{Name: "itemId", Type: "string"},
							{Name: "departure", Type: "string"},
							{Name: "arrival", Type: "string"},
						},
					},
				},
			},
			"process": {
				Name:        "process",
				Description: "Process selected item",
				Adapter:     "processAdapter",
				Inputs: []graph.Input{
					{Name: "itemId", Type: "string"},
					// "origin" doesn't match any elementField — triggers fallback
					{Name: "origin", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "result", Type: "string"},
				},
			},
		},
	}
}

// buildTemplateTestGraph creates a minimal graph with a workflow template.
// The template plan is written to a temp directory, and graphDir is returned.
func buildTemplateTestGraph(t *testing.T) (*graph.Graph, string) {
	t.Helper()

	dir := t.TempDir()

	// Write a simple plan template
	templateContent := `
execution:
  steps:
    - node: search
      values:
        query: "placeholder"
    - node: process
      dependsOn: [search]
      values:
        itemId: {from: search.resultId}
  cleanup:
    - node: cleanup
      runOn: always
`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "workflows"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflows", "booking.yaml"), []byte(templateContent), 0o644))

	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{Name: "Booking Flow", Template: "workflows/booking.yaml"},
			{Name: "No Template Flow"},
		},
		Nodes: map[string]*graph.Node{
			"search": {
				Name:        "search",
				Description: "Search for items",
				Adapter:     "searchAdapter",
				Inputs: []graph.Input{
					{Name: "query", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "resultId", Type: "string"},
				},
			},
			"process": {
				Name:        "process",
				Description: "Process selected item",
				Adapter:     "processAdapter",
				Inputs: []graph.Input{
					{Name: "itemId", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "result", Type: "string"},
				},
			},
			"cleanup": {
				Name:        "cleanup",
				Description: "Clean up",
				Adapter:     "cleanupAdapter",
				Inputs:      []graph.Input{},
				Outputs:     []graph.Output{},
			},
		},
	}

	return g, dir
}

// buildRetryTestGraph creates a graph with a workflow template whose template
// contains a broken fromSelection reference. This exercises the retry path
// in Interpret: the first plan generation produces a validation error,
// and the retry provides the corrected fromSelection.
func buildRetryTestGraph(t *testing.T) (*graph.Graph, string) {
	t.Helper()

	dir := t.TempDir()

	// Template with a fromSelection that will be correct after PostProcess
	// sets up selection configs. The "origin" input is fed by a select edge
	// from search.items, but "origin" does not match any elementField.
	// PostProcess/fixSelectionConfigs will create fromSelection with "origin"
	// as the field name (because lookupElementFieldPath falls back to inputName),
	// which validation will catch since "origin" is not a valid elementField.
	templateContent := `
execution:
  steps:
    - node: search
      values:
        query: "placeholder"
    - node: process
      dependsOn: [search]
      values:
        itemId:
          fromSelection: item.itemId
        origin:
          fromSelection: item.origin
      selections:
        item:
          from: search.items
          strategy: first
`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "workflows"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflows", "retry.yaml"), []byte(templateContent), 0o644))

	g := buildMismatchGraph()
	g.Workflows = []graph.Workflow{
		{Name: "Retry Flow", Template: "workflows/retry.yaml"},
	}

	return g, dir
}

func TestInterpret_RetryOnValidationFailure(t *testing.T) {
	g, graphDir := buildRetryTestGraph(t)

	wsJSON := `{"workflow": "Retry Flow", "description": "Process an item", "constraints": {"hard": [], "soft": [], "free": ["query"]}}`

	targetedJSON := `{
		"values": {
			"search.query": "test query"
		}
	}`

	// The template has a broken fromSelection (item.origin where "origin"
	// isn't a valid elementField). Validation catches this. The retry blanks
	// the broken fromSelection and re-runs PostProcess. Since there are no
	// edges to backfill from, the retry returns nil (validation still fails).
	t.Run("retry_returns_nil_when_broken_fromSelection_cannot_be_fixed", func(t *testing.T) {
		client := &stubClient{
			responses: []string{
				wsJSON,       // Call 1: workflow selection
				targetedJSON, // Call 2: targeted fill (broken fromSelection in template)
				targetedJSON, // Call 3: retry (still can't fix)
			},
		}

		result, err := Interpret(context.Background(), InterpretRequest{
			Prompt:   "process an item",
			Graph:    g,
			Client:   client,
			GraphDir: graphDir,
		})

		// Retry returns the original plan (before retry) since retry couldn't fix it.
		// The pipeline falls through to the pre-retry result.
		require.NoError(t, err)
		require.NotNil(t, result.Plan)

		// Should have made 3 LLM calls (selection + targeted + retry)
		assert.Len(t, client.calls, 3)
	})

	t.Run("retry trace captures validation error and retry", func(t *testing.T) {
		client := &stubClient{
			responses: []string{wsJSON, targetedJSON, targetedJSON},
		}

		result, err := Interpret(context.Background(), InterpretRequest{
			Prompt:      "process an item",
			Graph:       g,
			Client:      client,
			GraphDir:    graphDir,
			EnableTrace: true,
		})

		require.NoError(t, err)
		require.NotNil(t, result.Trace)

		// Trace should capture the initial validation error
		assert.NotEmpty(t, result.Trace.ValidationErr)
		assert.Contains(t, result.Trace.ValidationErr, "origin")

		// Trace should capture the retry call
		require.NotNil(t, result.Trace.RetryCall)

		// Retry should have succeeded (empty retry validation error)
		assert.Empty(t, result.Trace.RetryValidationErr)

		// Targeted response should be in the trace
		require.NotNil(t, result.Trace.TargetedResponse)

		// Final plan should be present
		assert.NotNil(t, result.Trace.FinalPlan)
	})
}

func TestInterpret_WorkflowTrace(t *testing.T) {
	g, graphDir := buildTemplateTestGraph(t)

	wsJSON := `{"workflow": "Booking Flow", "description": "Process an item via workflow", "constraints": {"hard": [], "soft": [], "free": ["query"]}}`

	targetedJSON := `{
		"values": {
			"search.query": "test"
		}
	}`

	client := &stubClient{
		responses: []string{wsJSON, targetedJSON},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:      "book an item",
		Graph:       g,
		Client:      client,
		GraphDir:    graphDir,
		EnableTrace: true,
	})

	require.NoError(t, err)
	require.NotNil(t, result.Trace)

	// Trace should capture workflow metadata
	assert.Equal(t, "Booking Flow", result.Trace.WorkflowName)
	assert.Equal(t, "workflows/booking.yaml", result.Trace.TemplatePath)

	// WorkflowSelection should be in the trace
	require.NotNil(t, result.Trace.WorkflowSelection)
	assert.Equal(t, "Booking Flow", result.Trace.WorkflowSelection.Workflow)

	// Skeleton should be present
	assert.NotNil(t, result.Trace.Skeleton)

	// Selection call should be captured
	assert.NotEmpty(t, result.Trace.SelectionCall.RawResponse)

	// Plan call should be captured
	assert.NotEmpty(t, result.Trace.PlanCall.RawResponse)

	// Targeted response should be in the trace
	require.NotNil(t, result.Trace.TargetedResponse)
	assert.Contains(t, result.Trace.TargetedResponse.Values, "search.query")

	// Final plan should be present
	assert.NotNil(t, result.Trace.FinalPlan)
}

// --- validateWorkflowSelection tests ---

func TestValidateWorkflowSelection_Valid(t *testing.T) {
	g, _ := buildTemplateTestGraph(t)

	ws := &WorkflowSelection{
		Workflow:    "Booking Flow",
		Description: "Book an item",
	}

	errs := validateWorkflowSelection(ws, g, nil)
	assert.Empty(t, errs)
}

func TestValidateWorkflowSelection_EmptyWorkflow(t *testing.T) {
	g, _ := buildTemplateTestGraph(t)

	ws := &WorkflowSelection{Workflow: ""}

	errs := validateWorkflowSelection(ws, g, nil)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "empty")
}

func TestValidateWorkflowSelection_UnknownWorkflow(t *testing.T) {
	g, _ := buildTemplateTestGraph(t)

	ws := &WorkflowSelection{Workflow: "Nonexistent"}

	errs := validateWorkflowSelection(ws, g, nil)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "not found")
	assert.Contains(t, errs[0], "Nonexistent")
}

func TestValidateWorkflowSelection_TemplateLessWorkflow(t *testing.T) {
	g, _ := buildTemplateTestGraph(t)

	ws := &WorkflowSelection{Workflow: "No Template Flow"}

	errs := validateWorkflowSelection(ws, g, nil)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "no template")
}

func TestValidateWorkflowSelection_NonAddonAsAddon(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{Name: "Main", Template: "workflows/main.yaml"},
			{Name: "Extra", Template: "workflows/extra.yaml"}, // not an addon
		},
		Nodes: map[string]*graph.Node{},
	}

	ws := &WorkflowSelection{
		Workflow: "Main",
		Addons:   []string{"Extra"},
	}

	errs := validateWorkflowSelection(ws, g, nil)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "not an addon")
}

func TestValidateWorkflowSelection_DuplicateAddon(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{Name: "Main", Template: "workflows/main.yaml"},
			{Name: "Seat", Kind: "addon", Template: "workflows/seat.yaml", After: graph.AfterSpec{"book"}},
		},
		Nodes: map[string]*graph.Node{},
	}

	ws := &WorkflowSelection{
		Workflow: "Main",
		Addons:   []string{"Seat", "Seat"},
	}

	errs := validateWorkflowSelection(ws, g, nil)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "duplicate")
}

func TestValidateWorkflowSelection_AddonSelectedAsBase(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{Name: "Main", Template: "workflows/main.yaml"},
			{Name: "Seat", Kind: "addon", Template: "workflows/seat.yaml", After: graph.AfterSpec{"book"}},
		},
		Nodes: map[string]*graph.Node{},
	}

	ws := &WorkflowSelection{Workflow: "Seat"}

	errs := validateWorkflowSelection(ws, g, nil)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "addon, not a base workflow")
}

func TestValidateWorkflowSelection_UnknownLayer(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{Name: "Main", Template: "workflows/main.yaml"},
		},
		Nodes: map[string]*graph.Node{},
	}
	available := map[string]*graph.Layer{
		"european": {Name: "european", Description: "European airports"},
		"premium":  {Name: "premium", Description: "Premium cabin"},
	}

	ws := &WorkflowSelection{
		Workflow: "Main",
		Layers:   []string{"nonexistent"},
	}

	errs := validateWorkflowSelection(ws, g, available)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "nonexistent")
	assert.Contains(t, errs[0], "not found")
}

func TestValidateWorkflowSelection_DuplicateLayer(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{Name: "Main", Template: "workflows/main.yaml"},
		},
		Nodes: map[string]*graph.Node{},
	}
	available := map[string]*graph.Layer{
		"european": {Name: "european", Description: "European airports"},
	}

	ws := &WorkflowSelection{
		Workflow: "Main",
		Layers:   []string{"european", "european"},
	}

	errs := validateWorkflowSelection(ws, g, available)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "duplicate")
}

func TestValidateWorkflowSelection_ValidLayers(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{Name: "Main", Template: "workflows/main.yaml"},
		},
		Nodes: map[string]*graph.Node{},
	}
	available := map[string]*graph.Layer{
		"european": {Name: "european", Description: "European airports"},
		"premium":  {Name: "premium", Description: "Premium cabin"},
	}

	ws := &WorkflowSelection{
		Workflow: "Main",
		Layers:   []string{"european", "premium"},
	}

	errs := validateWorkflowSelection(ws, g, available)
	assert.Empty(t, errs)
}

func TestValidateWorkflowSelection_LayersSkippedWhenNilAvailable(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{Name: "Main", Template: "workflows/main.yaml"},
		},
		Nodes: map[string]*graph.Node{},
	}

	// LLM returns layers, but availableLayers is nil — skip validation.
	ws := &WorkflowSelection{
		Workflow: "Main",
		Layers:   []string{"anything"},
	}

	errs := validateWorkflowSelection(ws, g, nil)
	assert.Empty(t, errs)
}

func TestSelectWorkflow_RetryOnInvalid(t *testing.T) {
	g, _ := buildTemplateTestGraph(t)

	// First response: invalid workflow name. Second response: valid.
	client := &stubClient{
		responses: []string{
			`{"workflow": "Nonexistent", "description": "bad pick"}`,
			`{"workflow": "Booking Flow", "description": "Book an item"}`,
		},
	}

	sr, err := selectWorkflow(context.Background(), client, "menu", "book an item", g, nil, nil, fixedNow())

	require.NoError(t, err)
	assert.Equal(t, "Booking Flow", sr.Selection.Workflow)
	// Should have retried.
	assert.NotNil(t, sr.RetriedFrom)
	assert.Equal(t, "Nonexistent", sr.RetriedFrom.Selection.Workflow)

	// Should have made 2 LLM calls.
	assert.Len(t, client.calls, 2)
}

func TestSelectWorkflow_NoRetryWhenValid(t *testing.T) {
	g, _ := buildTemplateTestGraph(t)

	client := &stubClient{
		responses: []string{
			`{"workflow": "Booking Flow", "description": "Book an item"}`,
		},
	}

	sr, err := selectWorkflow(context.Background(), client, "menu", "book an item", g, nil, nil, fixedNow())

	require.NoError(t, err)
	assert.Equal(t, "Booking Flow", sr.Selection.Workflow)
	assert.Nil(t, sr.RetriedFrom)
	assert.Len(t, client.calls, 1)
}

// --- Wrong Plan Escape tests ---

// buildTwoWorkflowTestGraph creates a graph with two workflows (Flight Booking
// and Hotel Booking) for testing wrong-plan reselection.
func buildTwoWorkflowTestGraph(t *testing.T) (*graph.Graph, string) {
	t.Helper()

	dir := t.TempDir()

	flightTemplate := `
execution:
  steps:
    - node: searchFlights
      values:
        query: "placeholder"
    - node: bookFlight
      dependsOn: [searchFlights]
      values:
        flightId: {from: searchFlights.flightId}
`
	hotelTemplate := `
execution:
  steps:
    - node: findHotel
      values:
        city: "placeholder"
    - node: bookHotel
      dependsOn: [findHotel]
      values:
        hotelId: {from: findHotel.hotelId}
`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "workflows"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflows", "flights.yaml"), []byte(flightTemplate), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflows", "hotels.yaml"), []byte(hotelTemplate), 0o644))

	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{Name: "Flight Booking", Template: "workflows/flights.yaml"},
			{Name: "Hotel Booking", Template: "workflows/hotels.yaml"},
		},
		Nodes: map[string]*graph.Node{
			"searchFlights": {
				Name: "searchFlights", Description: "Search flights", Adapter: "a",
				Inputs:  []graph.Input{{Name: "query", Type: "string"}},
				Outputs: []graph.Output{{Name: "flightId", Type: "string"}},
			},
			"bookFlight": {
				Name: "bookFlight", Description: "Book a flight", Adapter: "b",
				Inputs:  []graph.Input{{Name: "flightId", Type: "string"}},
				Outputs: []graph.Output{{Name: "confirmation", Type: "string"}},
			},
			"findHotel": {
				Name: "findHotel", Description: "Find hotels", Adapter: "c",
				Inputs:  []graph.Input{{Name: "city", Type: "string"}},
				Outputs: []graph.Output{{Name: "hotelId", Type: "string"}},
			},
			"bookHotel": {
				Name: "bookHotel", Description: "Book a hotel", Adapter: "d",
				Inputs:  []graph.Input{{Name: "hotelId", Type: "string"}},
				Outputs: []graph.Output{{Name: "confirmation", Type: "string"}},
			},
		},
	}

	return g, dir
}

func TestInterpret_WrongPlan_Reselection(t *testing.T) {
	g, graphDir := buildTwoWorkflowTestGraph(t)

	// LLM call sequence:
	// 1. Call 1: selects Flight Booking (wrong)
	// 2. Call 2: returns wrongPlan signal
	// 3. Call 1 (reselection): selects Hotel Booking (correct)
	// 4. Call 2: returns values for hotel workflow
	client := &stubClient{
		responses: []string{
			`{"workflow": "Flight Booking", "description": "Book a flight"}`,
			`{"wrongPlan": {"reason": "User asked about hotels, not flights", "suggested": "Hotel Booking"}}`,
			`{"workflow": "Hotel Booking", "description": "Book a hotel"}`,
			`{"values": {"findHotel.city": "Paris"}, "descriptions": {"findHotel": "Find hotels in Paris", "bookHotel": "Book the hotel"}}`,
		},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:   "book a hotel in Paris",
		Graph:    g,
		Client:   client,
		GraphDir: graphDir,
	})

	require.NoError(t, err)
	require.NotNil(t, result.Plan)

	// Should have made 4 LLM calls.
	assert.Len(t, client.calls, 4)

	// Plan should use hotel nodes.
	assert.Len(t, result.Plan.Execution.Steps, 2)
	assert.Equal(t, "findHotel", result.Plan.Execution.Steps[0].Node)
	assert.Equal(t, "bookHotel", result.Plan.Execution.Steps[1].Node)

	// WorkflowSelection should reflect the reselected workflow.
	require.NotNil(t, result.WorkflowSelection)
	assert.Equal(t, "Hotel Booking", result.WorkflowSelection.Workflow)

	// Reselection call should contain the wrong-plan feedback.
	reselCall := client.calls[2] // 3rd call = reselection
	assert.Contains(t, reselCall.Messages[1].Content, "doesn't match the intent")
	assert.Contains(t, reselCall.Messages[1].Content, "Hotel Booking")
}

func TestInterpret_WrongPlan_Trace(t *testing.T) {
	g, graphDir := buildTwoWorkflowTestGraph(t)

	client := &stubClient{
		responses: []string{
			`{"workflow": "Flight Booking", "description": "Book a flight"}`,
			`{"wrongPlan": {"reason": "Wrong workflow", "suggested": "Hotel Booking"}}`,
			`{"workflow": "Hotel Booking", "description": "Book a hotel"}`,
			`{"values": {"findHotel.city": "NYC"}}`,
		},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:      "book a hotel in NYC",
		Graph:       g,
		Client:      client,
		GraphDir:    graphDir,
		EnableTrace: true,
	})

	require.NoError(t, err)
	require.NotNil(t, result.Trace)

	// Wrong-plan signal should be captured.
	require.NotNil(t, result.Trace.WrongPlanSignal)
	assert.Equal(t, "Wrong workflow", result.Trace.WrongPlanSignal.Reason)
	assert.Equal(t, "Hotel Booking", result.Trace.WrongPlanSignal.Suggested)

	// The original Call 2 (that returned wrongPlan) should be saved.
	require.NotNil(t, result.Trace.WrongPlanCall)
	assert.NotEmpty(t, result.Trace.WrongPlanCall.RawResponse)

	// Reselection call should be captured.
	require.NotNil(t, result.Trace.ReselectionCall)
	assert.NotEmpty(t, result.Trace.ReselectionCall.RawResponse)

	// Final plan call should be the second Call 2 (with hotel values).
	assert.NotEmpty(t, result.Trace.PlanCall.RawResponse)

	// Final workflow should be Hotel Booking.
	assert.Equal(t, "Hotel Booking", result.Trace.WorkflowName)
}

func TestInterpret_WrongPlan_MaxOneRetry(t *testing.T) {
	g, graphDir := buildTwoWorkflowTestGraph(t)

	// Both Call 2 responses return wrongPlan — pipeline should not loop.
	client := &stubClient{
		responses: []string{
			`{"workflow": "Flight Booking", "description": "Book a flight"}`,
			`{"wrongPlan": {"reason": "Wrong workflow"}}`,
			`{"workflow": "Hotel Booking", "description": "Book a hotel"}`,
			`{"wrongPlan": {"reason": "Still wrong"}}`,
		},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:   "do something exotic",
		Graph:    g,
		Client:   client,
		GraphDir: graphDir,
	})

	// Should succeed (with template defaults) — no infinite loop.
	require.NoError(t, err)
	require.NotNil(t, result.Plan)

	// Exactly 4 calls — no additional reselection loop.
	assert.Len(t, client.calls, 4)

	// The plan should use the second workflow (Hotel Booking).
	assert.Equal(t, "findHotel", result.Plan.Execution.Steps[0].Node)
}

func TestInterpret_WrongPlan_WithoutSuggestion(t *testing.T) {
	g, graphDir := buildTwoWorkflowTestGraph(t)

	// Wrong plan without a suggestion — reselection should still work.
	client := &stubClient{
		responses: []string{
			`{"workflow": "Flight Booking", "description": "Book a flight"}`,
			`{"wrongPlan": {"reason": "This is a flight workflow but user wants hotels"}}`,
			`{"workflow": "Hotel Booking", "description": "Book a hotel"}`,
			`{"values": {"findHotel.city": "London"}}`,
		},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:   "book a hotel in London",
		Graph:    g,
		Client:   client,
		GraphDir: graphDir,
	})

	require.NoError(t, err)
	require.NotNil(t, result.Plan)
	assert.Len(t, client.calls, 4)
	assert.Equal(t, "Hotel Booking", result.WorkflowSelection.Workflow)
}
