package intent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/llm"
	"github.com/gburgyan/aat/plan"
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

func TestInterpret_BookingFlow(t *testing.T) {
	g := loadTravelportGraph(t)
	goalJSON := loadFixture(t, "testdata/responses/goal_analysis.json")
	planYAML := loadFixture(t, "testdata/responses/booking_plan.yaml")

	client := &stubClient{
		responses: []string{goalJSON, planYAML},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "book a flight from DEN to SFO",
		Graph:  g,
		Client: client,
	})

	require.NoError(t, err)
	require.NotNil(t, result.Plan)
	require.NotNil(t, result.GoalAnalysis)
	require.NotNil(t, result.ChainResult)

	// Check goal was identified
	assert.Equal(t, "commitBooking", result.GoalAnalysis.Goal)

	// Check plan has correct nodes
	nodeNames := make([]string, len(result.Plan.Execution.Steps))
	for i, s := range result.Plan.Execution.Steps {
		nodeNames[i] = s.Node
	}
	assert.Contains(t, nodeNames, "searchFlights")
	assert.Contains(t, nodeNames, "createWorkbench")
	assert.Contains(t, nodeNames, "addOffer")
	assert.Contains(t, nodeNames, "addTraveler")
	assert.Contains(t, nodeNames, "commitBooking")

	// Check cleanup
	require.NotEmpty(t, result.Plan.Execution.Cleanup)
	assert.Equal(t, "ignoreWorkbench", result.Plan.Execution.Cleanup[0].Node)

	// Check metadata was set
	assert.Equal(t, "book a flight from DEN to SFO", result.Plan.Metadata.Prompt)
	assert.Equal(t, "1.0.0", result.Plan.Metadata.GraphVersion)

	// Verify two LLM calls were made
	assert.Len(t, client.calls, 2)

	// First call should contain graph context
	assert.Contains(t, client.calls[0].Messages[1].Content, "searchFlights")
	assert.Contains(t, client.calls[0].Messages[1].Content, "book a flight from DEN to SFO")

	// Second call should contain the skeleton and chain context
	assert.Contains(t, client.calls[1].Messages[1].Content, "Plan Skeleton")
	assert.Contains(t, client.calls[1].Messages[1].Content, "Execution Chain")
}

func TestInterpret_SearchOnly(t *testing.T) {
	g := loadTravelportGraph(t)
	searchGoalJSON := `{
		"goal": "searchFlights",
		"description": "Search for flights from DEN to LAX",
		"conditionContext": {},
		"pathPreferences": {},
		"constraints": {
			"hard": [{"name": "origin", "description": "Must be DEN", "appliesTo": ["searchFlights.origin"]}],
			"soft": [],
			"free": ["departureDate"]
		}
	}`
	planYAML := loadFixture(t, "testdata/responses/search_only.yaml")

	client := &stubClient{
		responses: []string{searchGoalJSON, planYAML},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "search for flights from DEN to LAX",
		Graph:  g,
		Client: client,
	})

	require.NoError(t, err)
	assert.Equal(t, "searchFlights", result.GoalAnalysis.Goal)
	// Only one step for a search-only plan
	assert.Len(t, result.Plan.Execution.Steps, 1)
	assert.Equal(t, "searchFlights", result.Plan.Execution.Steps[0].Node)
}

func TestInterpret_EmptyPrompt(t *testing.T) {
	g := loadTravelportGraph(t)
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
	g := loadTravelportGraph(t)

	_, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "test",
		Graph:  g,
		Client: nil,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "LLM client is required")
}

func TestInterpret_LLMReturnsGarbage(t *testing.T) {
	g := loadTravelportGraph(t)
	malformed := loadFixture(t, "testdata/responses/malformed.txt")

	client := &stubClient{
		responses: []string{
			// First call: garbage → heuristic fallback kicks in
			"not valid json at all {{{",
			// Second call: also garbage
			malformed,
		},
	}

	_, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "book a flight",
		Graph:  g,
		Client: client,
	})

	require.Error(t, err)
	// Should fail at YAML extraction or parsing
	assert.True(t,
		assert.ObjectsAreEqual(true, contains(err.Error(), "extracting YAML")) ||
			assert.ObjectsAreEqual(true, contains(err.Error(), "parsing generated plan")),
		"unexpected error: %v", err)
}

func TestInterpret_GoalFallback_MalformedJSON(t *testing.T) {
	g := loadTravelportGraph(t)
	planYAML := loadFixture(t, "testdata/responses/booking_plan.yaml")

	client := &stubClient{
		responses: []string{
			// First call: malformed JSON → heuristic goal identification
			"this is not json",
			// Second call: valid plan
			planYAML,
		},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "book a flight from DEN to SFO",
		Graph:  g,
		Client: client,
	})

	require.NoError(t, err)
	// Heuristic should have picked a goal
	assert.NotEmpty(t, result.GoalAnalysis.Goal)
	assert.Contains(t, result.GoalAnalysis.Description, "Heuristic")
}

func TestInterpret_GoalFallback_InvalidNodeName(t *testing.T) {
	g := loadTravelportGraph(t)
	planYAML := loadFixture(t, "testdata/responses/booking_plan.yaml")

	client := &stubClient{
		responses: []string{
			// First call: valid JSON but with non-existent node name
			`{"goal": "nonExistentNode", "description": "bad", "conditionContext": {}, "pathPreferences": {}, "constraints": {"hard": [], "soft": [], "free": []}}`,
			// Second call: valid plan
			planYAML,
		},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "book a flight from DEN to SFO",
		Graph:  g,
		Client: client,
	})

	require.NoError(t, err)
	// Should have fallen back to heuristic
	assert.NotEqual(t, "nonExistentNode", result.GoalAnalysis.Goal)
}

func TestInterpret_NilKnowledgeBase(t *testing.T) {
	g := loadTravelportGraph(t)
	goalJSON := loadFixture(t, "testdata/responses/goal_analysis.json")
	planYAML := loadFixture(t, "testdata/responses/booking_plan.yaml")

	client := &stubClient{
		responses: []string{goalJSON, planYAML},
	}

	// KB is nil — should work fine
	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "book a flight from DEN to SFO",
		Graph:  g,
		KB:     nil,
		Client: client,
	})

	require.NoError(t, err)
	require.NotNil(t, result.Plan)

	// Second call should NOT contain domain context
	assert.NotContains(t, client.calls[1].Messages[1].Content, "Domain Knowledge")
}

func TestInterpret_WithKnowledgeBase(t *testing.T) {
	g := loadTravelportGraph(t)

	kbYAML := `
concepts:
  airportCode:
    description: "3-letter IATA airport code"
    applies_to: [origin, destination]
types:
  airportCode:
    description: "IATA airport code"
    format: "3 uppercase letters"
    pool: usAirports
valuePools:
  usAirports:
    description: "US airport codes"
    type: airportCode
    values: [DEN, SFO, LAX, JFK, ORD]
`
	kb, err := loadKBFromYAML(t, kbYAML)
	require.NoError(t, err)

	goalJSON := loadFixture(t, "testdata/responses/goal_analysis.json")
	planYAML := loadFixture(t, "testdata/responses/booking_plan.yaml")

	client := &stubClient{
		responses: []string{goalJSON, planYAML},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "book a flight from DEN to SFO",
		Graph:  g,
		KB:     kb,
		Client: client,
	})

	require.NoError(t, err)
	require.NotNil(t, result.Plan)

	// Second call should contain domain context
	assert.Contains(t, client.calls[1].Messages[1].Content, "Domain Knowledge")
	assert.Contains(t, client.calls[1].Messages[1].Content, "airportCode")
}

func TestInterpret_DateInPrompts(t *testing.T) {
	g := loadTravelportGraph(t)
	goalJSON := loadFixture(t, "testdata/responses/goal_analysis.json")
	planYAML := loadFixture(t, "testdata/responses/booking_plan.yaml")

	client := &stubClient{
		responses: []string{goalJSON, planYAML},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "book a flight from DEN to SFO",
		Graph:  g,
		Client: client,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, client.calls, 2)

	// Today's date should appear in both LLM prompts
	today := time.Now().Format("2006-01-02")

	// First call (goal analysis): date in system message
	assert.Contains(t, client.calls[0].Messages[0].Content, today,
		"goal analysis system prompt should contain today's date")

	// Second call (plan generation): date in system message (the rules section)
	assert.Contains(t, client.calls[1].Messages[0].Content, today,
		"plan generation system prompt should contain today's date")
}

func TestInterpret_LLMError(t *testing.T) {
	g := loadTravelportGraph(t)

	client := &stubClient{
		err: assert.AnError,
	}

	_, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "book a flight",
		Graph:  g,
		Client: client,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "goal analysis")
}

func TestHeuristicGoalAnalysis_BookingKeywords(t *testing.T) {
	g := loadTravelportGraph(t)

	ga := heuristicGoalAnalysis("book a flight and commit the booking", g)
	// Should prefer commitBooking (has "book" keyword + is terminal)
	assert.Equal(t, "commitBooking", ga.Goal)
}

func TestHeuristicGoalAnalysis_SearchKeywords(t *testing.T) {
	g := loadTravelportGraph(t)

	ga := heuristicGoalAnalysis("search for available flights", g)
	// searchFlights should score high on "search" and "flights"
	assert.Equal(t, "searchFlights", ga.Goal)
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

func TestInterpret_RetryOnValidationFailure(t *testing.T) {
	g := buildMismatchGraph()

	goalJSON := `{
		"goal": "process",
		"description": "Process an item",
		"conditionContext": {},
		"pathPreferences": {},
		"constraints": {"hard": [], "soft": [], "free": ["query"]}
	}`

	// The LLM response for the initial plan call — the skeleton already has
	// the broken fromSelection so we just need to return the skeleton as-is
	// (the LLM isn't changing structural fields anyway).
	initialPlanYAML := "```yaml\n" + `
metadata:
  prompt: "process an item"
  graphVersion: "1.0.0"
execution:
  steps:
    - node: search
      values:
        query: "test query"
    - node: process
      dependsOn: [search]
      values:
        itemId:
          fromSelection: item.itemId
        origin:
          fromSelection: item.origin
` + "```\n"

	// The retry response with the corrected field name
	retryPlanYAML := "```yaml\n" + `
metadata:
  prompt: "process an item"
  graphVersion: "1.0.0"
execution:
  steps:
    - node: search
      values:
        query: "test query"
    - node: process
      dependsOn: [search]
      values:
        itemId:
          fromSelection: item.itemId
        origin:
          fromSelection: item.departure
` + "```\n"

	t.Run("retry succeeds", func(t *testing.T) {
		client := &stubClient{
			responses: []string{
				goalJSON,       // Call 1: goal analysis
				initialPlanYAML, // Call 2: plan (skeleton has bad fromSelection)
				retryPlanYAML,   // Call 3: retry with corrected fromSelection
			},
		}

		result, err := Interpret(context.Background(), InterpretRequest{
			Prompt: "process an item",
			Graph:  g,
			Client: client,
		})

		require.NoError(t, err)
		require.NotNil(t, result.Plan)

		// Should have made 3 LLM calls (goal + bad plan + retry)
		assert.Len(t, client.calls, 3)

		// The retry prompt should contain validation error context
		retrySystem := client.calls[2].Messages[0].Content
		assert.Contains(t, retrySystem, "validation errors")
		assert.Contains(t, retrySystem, "elementField")

		// The final plan should have the corrected fromSelection
		for _, step := range result.Plan.Execution.Steps {
			if step.Node == "process" {
				originVal := step.Values["origin"]
				_, field := plan.ParseFromSelection(originVal.FromSelection)
				assert.Equal(t, "departure", field, "origin input should use 'departure' elementField")
			}
		}
	})

	t.Run("retry succeeds with trace", func(t *testing.T) {
		client := &stubClient{
			responses: []string{
				goalJSON,
				initialPlanYAML,
				retryPlanYAML,
			},
		}

		result, err := Interpret(context.Background(), InterpretRequest{
			Prompt:      "process an item",
			Graph:       g,
			Client:      client,
			EnableTrace: true,
		})

		require.NoError(t, err)
		require.NotNil(t, result.Plan)
		require.NotNil(t, result.Trace)

		// Trace should capture the initial validation error
		assert.NotEmpty(t, result.Trace.ValidationErr)
		assert.Contains(t, result.Trace.ValidationErr, "origin")

		// Trace should capture the retry call
		require.NotNil(t, result.Trace.RetryCall)
		assert.NotEmpty(t, result.Trace.RetryCall.RawResponse)
		assert.Empty(t, result.Trace.RetryCall.Error)

		// Retry validation should have succeeded (empty error)
		assert.Empty(t, result.Trace.RetryValidationErr)

		// Final plan should be present
		assert.NotNil(t, result.Trace.FinalPlan)
	})

	t.Run("retry fails returns original error", func(t *testing.T) {
		client := &stubClient{
			responses: []string{
				goalJSON,        // Call 1: goal analysis
				initialPlanYAML, // Call 2: plan with bad fromSelection
				initialPlanYAML, // Call 3: retry still has bad fromSelection
			},
		}

		_, err := Interpret(context.Background(), InterpretRequest{
			Prompt: "process an item",
			Graph:  g,
			Client: client,
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "validating generated plan")
		// Should have attempted 3 calls
		assert.Len(t, client.calls, 3)
	})

	t.Run("retry fails returns trace with retry info", func(t *testing.T) {
		client := &stubClient{
			responses: []string{
				goalJSON,
				initialPlanYAML,
				initialPlanYAML,
			},
		}

		result, err := Interpret(context.Background(), InterpretRequest{
			Prompt:      "process an item",
			Graph:       g,
			Client:      client,
			EnableTrace: true,
		})

		require.Error(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Trace)

		// Both validation errors should be captured
		assert.NotEmpty(t, result.Trace.ValidationErr)
		assert.NotEmpty(t, result.Trace.RetryValidationErr)
		require.NotNil(t, result.Trace.RetryCall)
	})
}

// --- Workflow Template Integration Tests ---

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

func TestInterpret_WorkflowTemplate(t *testing.T) {
	g, graphDir := buildTemplateTestGraph(t)

	// LLM returns goal analysis that selects the workflow
	goalJSON := `{
		"goal": "process",
		"description": "Process an item via workflow",
		"conditionContext": {},
		"pathPreferences": {},
		"constraints": {"hard": [], "soft": [], "free": ["query"]},
		"workflow": "Booking Flow"
	}`

	planYAML := "```yaml\n" + `
execution:
  steps:
    - node: search
      description: "Search for items"
      values:
        query: "real search query"
    - node: process
      dependsOn: [search]
      description: "Process the item"
      values:
        itemId: {from: search.resultId}
      assertions:
        mechanical:
          - type: status
            expect: 200
  cleanup:
    - node: cleanup
      runOn: always
` + "```\n"

	client := &stubClient{
		responses: []string{goalJSON, planYAML},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:   "book an item",
		Graph:    g,
		Client:   client,
		GraphDir: graphDir,
	})

	require.NoError(t, err)
	require.NotNil(t, result.Plan)

	// Should have used the template — only 2 LLM calls (goal + fill)
	assert.Len(t, client.calls, 2)

	// The plan should have 2 steps (from template)
	assert.Len(t, result.Plan.Execution.Steps, 2)
	assert.Equal(t, "search", result.Plan.Execution.Steps[0].Node)
	assert.Equal(t, "process", result.Plan.Execution.Steps[1].Node)

	// ChainResult should be nil (no backward chaining)
	assert.Nil(t, result.ChainResult)

	// GoalAnalysis should have workflow set
	assert.Equal(t, "Booking Flow", result.GoalAnalysis.Workflow)
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

	goalJSON := `{
		"goal": "commit",
		"description": "Add 2 items and commit",
		"conditionContext": {},
		"pathPreferences": {},
		"constraints": {"hard": [], "soft": [], "free": []},
		"workflow": "Multi-Item",
		"repetitions": {"addItem": 2}
	}`

	planYAML := "```yaml\n" + `
execution:
  steps:
    - node: setup
      values:
        config: "test"
    - id: addItem_1
      node: addItem
      dependsOn: [setup]
      values:
        setupId: {from: setup.setupId}
        name: "First Item"
    - id: addItem_2
      node: addItem
      dependsOn: [addItem_1]
      values:
        setupId: {from: setup.setupId}
        name: "Second Item"
    - node: commit
      dependsOn: [addItem_2]
      values:
        setupId: {from: setup.setupId}
` + "```\n"

	client := &stubClient{
		responses: []string{goalJSON, planYAML},
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
}

func TestInterpret_WorkflowFallback_BadName(t *testing.T) {
	// Build a simple graph without the mismatch issue.
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search", Adapter: "a",
				Inputs:  []graph.Input{{Name: "query", Type: "string"}},
				Outputs: []graph.Output{{Name: "resultId", Type: "string"}},
			},
			"process": {
				Name: "process", Description: "Process", Adapter: "b",
				Inputs:  []graph.Input{{Name: "id", Type: "string"}},
				Outputs: []graph.Output{{Name: "result", Type: "string"}},
			},
		},
		Edges: []graph.Edge{
			{From: "search.resultId", To: "process.id"},
		},
	}
	g.BuildEdgeIndex()

	goalJSON := `{
		"goal": "process",
		"description": "Process an item",
		"conditionContext": {},
		"pathPreferences": {},
		"constraints": {"hard": [], "soft": [], "free": ["query"]},
		"workflow": "Nonexistent Workflow"
	}`

	planYAML := "```yaml\n" + `
execution:
  steps:
    - node: search
      values:
        query: "test"
    - node: process
      dependsOn: [search]
      values:
        id: {from: search.resultId}
` + "```\n"

	client := &stubClient{
		responses: []string{goalJSON, planYAML},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:   "process an item",
		Graph:    g,
		Client:   client,
		GraphDir: ".",
	})

	require.NoError(t, err)
	require.NotNil(t, result.Plan)

	// Should have used backward chaining (workflow not found)
	assert.NotNil(t, result.ChainResult)
}

func TestInterpret_NoWorkflow(t *testing.T) {
	// Build a simple graph without the mismatch issue.
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Description: "Search", Adapter: "a",
				Inputs:  []graph.Input{{Name: "query", Type: "string"}},
				Outputs: []graph.Output{{Name: "resultId", Type: "string"}},
			},
			"process": {
				Name: "process", Description: "Process", Adapter: "b",
				Inputs:  []graph.Input{{Name: "id", Type: "string"}},
				Outputs: []graph.Output{{Name: "result", Type: "string"}},
			},
		},
		Edges: []graph.Edge{
			{From: "search.resultId", To: "process.id"},
		},
	}
	g.BuildEdgeIndex()

	goalJSON := `{
		"goal": "process",
		"description": "Process an item",
		"conditionContext": {},
		"pathPreferences": {},
		"constraints": {"hard": [], "soft": [], "free": ["query"]}
	}`

	planYAML := "```yaml\n" + `
execution:
  steps:
    - node: search
      values:
        query: "test"
    - node: process
      dependsOn: [search]
      values:
        id: {from: search.resultId}
` + "```\n"

	client := &stubClient{
		responses: []string{goalJSON, planYAML},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "process an item",
		Graph:  g,
		Client: client,
	})

	require.NoError(t, err)
	require.NotNil(t, result.Plan)

	// Should have used backward chaining (no workflow field)
	assert.NotNil(t, result.ChainResult)
	assert.Empty(t, result.GoalAnalysis.Workflow)
}

func TestInterpret_WorkflowTrace(t *testing.T) {
	g, graphDir := buildTemplateTestGraph(t)

	goalJSON := `{
		"goal": "process",
		"description": "Process an item via workflow",
		"conditionContext": {},
		"pathPreferences": {},
		"constraints": {"hard": [], "soft": [], "free": ["query"]},
		"workflow": "Booking Flow"
	}`

	planYAML := "```yaml\n" + `
execution:
  steps:
    - node: search
      values:
        query: "test"
    - node: process
      dependsOn: [search]
      values:
        itemId: {from: search.resultId}
  cleanup:
    - node: cleanup
      runOn: always
` + "```\n"

	client := &stubClient{
		responses: []string{goalJSON, planYAML},
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

	// No chain result since template was used
	assert.Nil(t, result.Trace.ChainResult)

	// Skeleton should be present
	assert.NotNil(t, result.Trace.Skeleton)

	// Final plan should be present
	assert.NotNil(t, result.Trace.FinalPlan)
}

func TestInterpret_Integration_RealLLM(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("skipping: OPENAI_API_KEY not set")
	}

	// Create real OpenAI client
	client, err := llm.NewClient(config.LLMConfig{
		APIKey: config.SecretRef{Source: "literal", Value: apiKey},
		Model:  "gpt-5.2",
	})
	require.NoError(t, err)

	// Load graph + domain KB (existing fixtures)
	g := loadTravelportGraph(t)
	kb, err := domain.ParseFile("../domain/testdata/valid/travel.yaml")
	require.NoError(t, err)

	const maxAttempts = 3
	var result *InterpretResult
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, lastErr = Interpret(context.Background(), InterpretRequest{
			Prompt: "book a flight from DEN to SFO",
			Graph:  g,
			KB:     kb,
			Client: client,
		})
		if lastErr == nil {
			break
		}
		if attempt < maxAttempts {
			t.Logf("attempt %d/%d failed: %v — retrying", attempt, maxAttempts, lastErr)
		}
	}
	require.NoError(t, lastErr, "all %d attempts failed", maxAttempts)

	// Structural assertions (LLM output varies, check shape not exact values)
	assert.Equal(t, "commitBooking", result.GoalAnalysis.Goal)
	require.NotNil(t, result.Plan)
	require.NotEmpty(t, result.Plan.Execution.Steps)

	// Should have the key booking flow nodes
	nodeSet := map[string]bool{}
	for _, s := range result.Plan.Execution.Steps {
		nodeSet[s.Node] = true
	}
	assert.True(t, nodeSet["searchFlights"], "plan should include searchFlights")
	assert.True(t, nodeSet["commitBooking"], "plan should include commitBooking")

	// Cleanup should be present (PostProcess ensures this)
	assert.NotEmpty(t, result.Plan.Execution.Cleanup)

	// Metadata should be populated
	assert.Equal(t, "book a flight from DEN to SFO", result.Plan.Metadata.Prompt)
	assert.Equal(t, "1.0.0", result.Plan.Metadata.GraphVersion)

	// Log the plan for manual inspection
	t.Logf("Goal: %s", result.GoalAnalysis.Goal)
	t.Logf("Steps: %d", len(result.Plan.Execution.Steps))
	for i, s := range result.Plan.Execution.Steps {
		t.Logf("  %d: %s (dependsOn: %v)", i, s.Node, s.DependsOn)
	}
	t.Logf("Cleanup: %d steps", len(result.Plan.Execution.Cleanup))
}
