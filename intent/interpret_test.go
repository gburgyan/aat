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

func loadFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
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

func TestInterpret_WorkflowWithRepetitions(t *testing.T) {
	dir := t.TempDir()

	// Template with a step that can be repeated
	templateContent := `
execution:
  steps:
    - node: setup
      values:
        config: default
    - node: addItem
      dependsOn: [setup]
      values:
        setupId: {from: setup.setupId}
        name: "placeholder"
    - node: commit
      dependsOn: [addItem]
      values:
        setupId: {from: setup.setupId}
`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "plans"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plans", "multi.yaml"), []byte(templateContent), 0o644))

	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{Name: "Multi-Item", Template: "plans/multi.yaml"},
		},
		Nodes: map[string]*graph.Node{
			"setup":   {Name: "setup", Description: "Setup", Adapter: "a", Inputs: []graph.Input{{Name: "config", Type: "string"}}, Outputs: []graph.Output{{Name: "setupId", Type: "string"}}},
			"addItem": {Name: "addItem", Description: "Add item", Adapter: "b", Inputs: []graph.Input{{Name: "setupId", Type: "string"}, {Name: "name", Type: "string"}}, Outputs: []graph.Output{{Name: "itemId", Type: "string"}}},
			"commit":  {Name: "commit", Description: "Commit", Adapter: "c", Inputs: []graph.Input{{Name: "setupId", Type: "string"}}, Outputs: []graph.Output{{Name: "result", Type: "string"}}},
		},
		Edges: []graph.Edge{
			{From: "setup.setupId", To: "addItem.setupId"},
			{From: "setup.setupId", To: "commit.setupId"},
		},
	}
	g.BuildEdgeIndex()

	wsJSON := `{"workflow": "Multi-Item", "description": "Add 2 items and commit", "repetitions": {"addItem": 2}, "constraints": {"hard": [], "soft": [], "free": []}}`

	// Targeted JSON response — provide values for the unfed inputs after expansion.
	// After ExpandMultiplicity, addItem_1 keeps "placeholder" from template,
	// addItem_2 has it cleared. Both "name" inputs are unfed since the template
	// value "placeholder" is a literal default that qualifies as fed.
	// Actually: setup.config has default "default" from template (fed),
	// addItem_1.name has "placeholder" (fed), addItem_2.name is cleared (unfed).
	targetedJSON := `{
		"values": {
			"addItem_2.name": "Second Item"
		},
		"descriptions": {
			"setup": "Set up the workspace",
			"addItem_1": "Add the first item",
			"addItem_2": "Add the second item",
			"commit": "Commit changes"
		}
	}`

	client := &stubClient{
		responses: []string{wsJSON, targetedJSON},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:   "add 2 items and commit",
		Graph:    g,
		Client:   client,
		GraphDir: dir,
	})

	require.NoError(t, err)
	require.NotNil(t, result.Plan)

	// Should have 4 steps: setup, addItem_1, addItem_2, commit
	assert.Len(t, result.Plan.Execution.Steps, 4)

	// Check repetition IDs
	ids := make([]string, len(result.Plan.Execution.Steps))
	for i, s := range result.Plan.Execution.Steps {
		ids[i] = s.StepID()
	}
	assert.Contains(t, ids, "addItem_1")
	assert.Contains(t, ids, "addItem_2")

	// WorkflowSelection should have repetitions
	require.NotNil(t, result.WorkflowSelection)
	assert.Equal(t, "Multi-Item", result.WorkflowSelection.Workflow)
	assert.Equal(t, 2, result.WorkflowSelection.Repetitions["addItem"])
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
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "plans"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plans", "booking.yaml"), []byte(templateContent), 0o644))

	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{Name: "Booking Flow", Template: "plans/booking.yaml"},
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
		Edges: []graph.Edge{
			{From: "search.resultId", To: "process.itemId"},
		},
	}
	g.BuildEdgeIndex()

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

// contains is a helper for checking if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
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
		Edges: []graph.Edge{
			{From: "search.items", To: "process.itemId", Select: true},
			{From: "search.items", To: "process.origin", Select: true},
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
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "plans"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plans", "booking.yaml"), []byte(templateContent), 0o644))

	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{Name: "Booking Flow", Template: "plans/booking.yaml"},
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
		Edges: []graph.Edge{
			{From: "search.resultId", To: "process.itemId"},
		},
	}
	g.BuildEdgeIndex()

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
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "plans"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plans", "retry.yaml"), []byte(templateContent), 0o644))

	g := buildMismatchGraph()
	g.Workflows = []graph.Workflow{
		{Name: "Retry Flow", Template: "plans/retry.yaml"},
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
	// the broken fromSelection and re-runs PostProcess, which normalizes the
	// origin input via fixSelectionConfigs (since a select edge exists in the
	// graph). This creates a from+select config that validates successfully.
	t.Run("retry fixes broken fromSelection via normalization", func(t *testing.T) {
		client := &stubClient{
			responses: []string{
				wsJSON,       // Call 1: workflow selection
				targetedJSON, // Call 2: targeted fill (broken fromSelection in template)
				targetedJSON, // Call 3: retry (PostProcess normalizes blanked origin)
			},
		}

		result, err := Interpret(context.Background(), InterpretRequest{
			Prompt:   "process an item",
			Graph:    g,
			Client:   client,
			GraphDir: graphDir,
		})

		require.NoError(t, err)
		require.NotNil(t, result.Plan)

		// Should have made 3 LLM calls (selection + targeted + retry)
		assert.Len(t, client.calls, 3)

		// The "origin" input should be wired via from+select after normalization.
		for _, step := range result.Plan.Execution.Steps {
			if step.Node == "process" {
				sv := step.Values["origin"]
				assert.NotEmpty(t, sv.From, "origin should have from ref after normalization")
			}
		}
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
	assert.Equal(t, "plans/booking.yaml", result.Trace.TemplatePath)

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
