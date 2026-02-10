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

// regressionExpect describes the structural expectations for a regression scenario.
type regressionExpect struct {
	goalNode     string
	stepNodes    []string
	goalStep     string   // IsGoal=true step
	cleanupNodes []string // expected cleanup nodes (nil = don't check)
	deps         map[string][]string
	selections   []string // steps that must have select-based values
	hardCount    int      // -1 = don't check constraints
	softCount    int
	freeCount    int
}

// regressionCase is one entry in the table-driven catalog.
type regressionCase struct {
	name       string
	graphPath  string
	fixtureDir string
	prompt     string
	kbYAML     string // empty = no KB
	fallback   bool   // true = expect heuristic fallback
	expect     regressionExpect
}

func TestInterpret_Regression(t *testing.T) {
	tests := []regressionCase{
		{
			name:       "booking_basic",
			graphPath:  "../graph/testdata/valid/travelport_booking.yaml",
			fixtureDir: "booking_basic",
			prompt:     "book a flight from DEN to SFO",
			expect: regressionExpect{
				goalNode:     "commitBooking",
				stepNodes:    []string{"searchFlights", "createWorkbench", "addOffer", "addTraveler", "commitBooking"},
				goalStep:     "commitBooking",
				cleanupNodes: []string{"ignoreWorkbench"},
				deps: map[string][]string{
					"addOffer":      {"createWorkbench", "searchFlights"},
					"addTraveler":   {"createWorkbench"},
					"commitBooking": {"addOffer", "addTraveler"},
				},
				selections: []string{"addOffer"},
				hardCount:  2,
				softCount:  0,
				freeCount:  4,
			},
		},
		{
			name:       "booking_with_kb",
			graphPath:  "../graph/testdata/valid/travelport_booking.yaml",
			fixtureDir: "booking_with_kb",
			prompt:     "book a business class flight from JFK to LAX",
			kbYAML: `
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
`,
			expect: regressionExpect{
				goalNode:     "commitBooking",
				stepNodes:    []string{"searchFlights", "createWorkbench", "addOffer", "addTraveler", "commitBooking"},
				goalStep:     "commitBooking",
				cleanupNodes: []string{"ignoreWorkbench"},
				deps: map[string][]string{
					"addOffer":      {"createWorkbench", "searchFlights"},
					"commitBooking": {"addOffer", "addTraveler"},
				},
				selections: []string{"addOffer"},
				hardCount:  3,
				softCount:  1,
				freeCount:  2,
			},
		},
		{
			name:       "search_only",
			graphPath:  "../graph/testdata/valid/travelport_booking.yaml",
			fixtureDir: "search_only",
			prompt:     "search for flights from DEN to LAX",
			expect: regressionExpect{
				goalNode:     "searchFlights",
				stepNodes:    []string{"searchFlights"},
				goalStep:     "searchFlights",
				cleanupNodes: nil, // no cleanup for search-only (searchFlights has no cleanup in travelport_booking)
				hardCount:    2,
				softCount:    0,
				freeCount:    4,
			},
		},
		{
			name:       "travel_select_fare",
			graphPath:  "../graph/testdata/valid/travel_flow.yaml",
			fixtureDir: "travel_select_fare",
			prompt:     "search for flights from DEN to ORD and select a fare",
			expect: regressionExpect{
				goalNode:     "selectFare",
				stepNodes:    []string{"searchFlights", "selectFare"},
				goalStep:     "selectFare",
				cleanupNodes: []string{"cancelSearch"},
				deps: map[string][]string{
					"selectFare": {"searchFlights"},
				},
				selections: []string{"selectFare"},
				hardCount:  2,
				softCount:  0,
				freeCount:  3,
			},
		},
		{
			name:       "travel_international",
			graphPath:  "../graph/testdata/valid/travel_flow.yaml",
			fixtureDir: "travel_international",
			prompt:     "search for international flights from JFK to LHR and select a fare",
			expect: regressionExpect{
				goalNode:     "selectFare",
				stepNodes:    []string{"searchFlights", "addPassportInfo", "selectFare"},
				goalStep:     "selectFare",
				cleanupNodes: []string{"cancelSearch"},
				deps: map[string][]string{
					"selectFare": {"searchFlights"},
				},
				selections: []string{"selectFare"},
				hardCount:  2,
				softCount:  0,
				freeCount:  3,
			},
		},
		{
			name:       "auth_get_user",
			graphPath:  "../graph/testdata/valid/with_cycle_breaker.yaml",
			fixtureDir: "auth_get_user",
			prompt:     "get user details",
			expect: regressionExpect{
				goalNode:  "getUser",
				stepNodes: []string{"authenticate", "getUser"},
				goalStep:  "getUser",
				deps: map[string][]string{
					"getUser": {"authenticate"},
				},
				hardCount: 0,
				softCount: 0,
				freeCount: 3,
			},
		},
		{
			name:       "auth_update_user",
			graphPath:  "../graph/testdata/valid/with_cycle_breaker.yaml",
			fixtureDir: "auth_update_user",
			prompt:     "update user name",
			expect: regressionExpect{
				goalNode:  "updateUser",
				stepNodes: []string{"authenticate", "getUser", "updateUser"},
				goalStep:  "updateUser",
				deps: map[string][]string{
					"getUser":    {"authenticate"},
					"updateUser": {"authenticate", "getUser"},
				},
				hardCount: 0,
				softCount: 0,
				freeCount: 4,
			},
		},
		{
			name:       "minimal_single_node",
			graphPath:  "../graph/testdata/valid/minimal.yaml",
			fixtureDir: "minimal_single_node",
			prompt:     "get user info",
			expect: regressionExpect{
				goalNode:  "getUser",
				stepNodes: []string{"getUser"},
				goalStep:  "getUser",
				hardCount: 0,
				softCount: 0,
				freeCount: 1,
			},
		},
		{
			name:       "preferred_path",
			graphPath:  "../graph/testdata/valid/with_multiple_paths.yaml",
			fixtureDir: "preferred_path",
			prompt:     "process data through preferred path",
			expect: regressionExpect{
				goalNode:  "consumer",
				stepNodes: []string{"source", "preferredSource", "consumer"},
				goalStep:  "consumer",
				deps: map[string][]string{
					"preferredSource": {"source"},
					"consumer":        {"preferredSource"},
				},
				hardCount: 0,
				softCount: 0,
				freeCount: 0,
			},
		},
		{
			name:       "goal_fallback",
			graphPath:  "../graph/testdata/valid/travelport_booking.yaml",
			fixtureDir: "goal_fallback",
			prompt:     "book a flight from DEN to SFO",
			fallback:   true,
			expect: regressionExpect{
				goalNode:     "commitBooking",
				stepNodes:    []string{"searchFlights", "createWorkbench", "addOffer", "addTraveler", "commitBooking"},
				goalStep:     "commitBooking",
				cleanupNodes: []string{"ignoreWorkbench"},
				hardCount:    -1, // fallback doesn't set structured constraints
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := loadGraph(t, tt.graphPath)

			var kb *domain.KnowledgeBase
			if tt.kbYAML != "" {
				var err error
				kb, err = domain.Parse([]byte(tt.kbYAML))
				require.NoError(t, err, "failed to parse KB YAML")
			}

			fixtures := loadRegressionFixtures(t, tt.fixtureDir)
			client := &stubClient{
				responses: []string{fixtures.GoalJSON, fixtures.PlanYAML},
			}

			result, err := Interpret(context.Background(), InterpretRequest{
				Prompt: tt.prompt,
				Graph:  g,
				KB:     kb,
				Client: client,
			})

			require.NoError(t, err)
			require.NotNil(t, result.Plan)
			require.NotNil(t, result.GoalAnalysis)
			require.NotNil(t, result.ChainResult)

			// Verify two LLM calls were made
			assert.Len(t, client.calls, 2, "expected exactly 2 LLM calls")

			// Goal analysis
			if tt.fallback {
				assert.Contains(t, result.GoalAnalysis.Description, "Heuristic",
					"expected heuristic fallback")
			}
			assert.Equal(t, tt.expect.goalNode, result.GoalAnalysis.Goal, "goal node mismatch")

			// Plan structure
			assertPlanNodes(t, result.Plan, tt.expect.stepNodes)
			assertGoalStep(t, result.Plan, tt.expect.goalStep)

			// Cleanup
			if tt.expect.cleanupNodes != nil {
				assertCleanupNodes(t, result.Plan, tt.expect.cleanupNodes)
			}

			// Dependencies
			for node, deps := range tt.expect.deps {
				assertStepDeps(t, result.Plan, node, deps)
			}

			// Selections
			for _, node := range tt.expect.selections {
				assertHasSelection(t, result.Plan, node)
			}

			// Constraints
			if tt.expect.hardCount >= 0 {
				assertConstraints(t, result.GoalAnalysis,
					tt.expect.hardCount, tt.expect.softCount, tt.expect.freeCount)
			}

			// Validation
			assertPlanValid(t, result.Plan, g)

			// Metadata
			assert.Equal(t, tt.prompt, result.Plan.Metadata.Prompt, "metadata prompt mismatch")
			assert.Equal(t, g.Version, result.Plan.Metadata.GraphVersion, "metadata version mismatch")

			// KB context in prompts
			if tt.kbYAML != "" {
				require.Len(t, client.calls, 2)
				assert.Contains(t, client.calls[1].Messages[1].Content, "Domain Knowledge",
					"plan prompt should contain domain knowledge when KB is provided")
			}
		})
	}
}

// TestInterpret_Regression_RealLLM runs a subset of scenarios against a real LLM.
// Gated on OPENAI_API_KEY. Uses structural-only assertions with retries since
// LLM output is non-deterministic and may occasionally produce invalid plans.
func TestInterpret_Regression_RealLLM(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("skipping: OPENAI_API_KEY not set")
	}

	client, err := llm.NewClient(config.LLMConfig{
		APIKey: config.SecretRef{Source: "literal", Value: apiKey},
		Model:  "gpt-5.2",
	})
	require.NoError(t, err)

	const maxAttempts = 3

	type realLLMCase struct {
		name       string
		graphPath  string
		prompt     string
		goalNode   string
		mustSteps  []string // steps that must be present
		hasCleanup bool
	}

	tests := []realLLMCase{
		{
			name:       "booking_flow",
			graphPath:  "../graph/testdata/valid/travelport_booking.yaml",
			prompt:     "book a flight from DEN to SFO",
			goalNode:   "commitBooking",
			mustSteps:  []string{"searchFlights", "commitBooking"},
			hasCleanup: true,
		},
		{
			name:       "search_only",
			graphPath:  "../graph/testdata/valid/travelport_booking.yaml",
			prompt:     "search for flights from DEN to LAX",
			goalNode:   "searchFlights",
			mustSteps:  []string{"searchFlights"},
			hasCleanup: false,
		},
		{
			name:      "preferred_path",
			graphPath: "../graph/testdata/valid/with_multiple_paths.yaml",
			prompt:    "process data through the preferred path to consumer",
			goalNode:  "consumer",
			mustSteps: []string{"source", "consumer"},
		},
		{
			name:      "auth_get_user",
			graphPath: "../graph/testdata/valid/with_cycle_breaker.yaml",
			prompt:    "authenticate and get user details for user 123",
			goalNode:  "getUser",
			mustSteps: []string{"authenticate", "getUser"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := loadGraph(t, tt.graphPath)

			var result *InterpretResult
			var lastErr error
			for attempt := 1; attempt <= maxAttempts; attempt++ {
				result, lastErr = Interpret(context.Background(), InterpretRequest{
					Prompt: tt.prompt,
					Graph:  g,
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
			require.NotNil(t, result.Plan)

			// Goal
			assert.Equal(t, tt.goalNode, result.GoalAnalysis.Goal,
				"goal node mismatch")

			// Must-have steps
			nodes := stepNodes(result.Plan)
			for _, must := range tt.mustSteps {
				assert.Contains(t, nodes, must,
					"plan should include step %s", must)
			}

			// Cleanup
			if tt.hasCleanup {
				assert.NotEmpty(t, result.Plan.Execution.Cleanup,
					"expected cleanup steps")
			}

			// Validation
			assertPlanValid(t, result.Plan, g)

			// Log for inspection
			t.Logf("Goal: %s", result.GoalAnalysis.Goal)
			t.Logf("Steps (%d): %v", len(result.Plan.Execution.Steps), nodes)
			t.Logf("Cleanup: %d steps", len(result.Plan.Execution.Cleanup))
		})
	}
}
