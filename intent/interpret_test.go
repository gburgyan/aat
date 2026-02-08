package intent

import (
	"context"
	"os"
	"testing"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/domain"
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

	// Second call should contain chain context
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

func TestInterpret_Integration_RealLLM(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("skipping: OPENAI_API_KEY not set")
	}

	// Create real OpenAI client (gpt-4o-mini for cost/speed)
	client, err := llm.NewClient(config.LLMConfig{
		APIKey: config.SecretRef{Source: "literal", Value: apiKey},
		Model:  "gpt-4o-mini",
	})
	require.NoError(t, err)

	// Load graph + domain KB (existing fixtures)
	g := loadTravelportGraph(t)
	kb, err := domain.ParseFile("../domain/testdata/valid/travel.yaml")
	require.NoError(t, err)

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "book a flight from DEN to SFO",
		Graph:  g,
		KB:     kb,
		Client: client,
	})
	require.NoError(t, err)

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
