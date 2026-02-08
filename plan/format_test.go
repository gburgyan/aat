package plan

import (
	"strings"
	"testing"
	"time"

	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
)

func TestFormatNarrative(t *testing.T) {
	tests := []struct {
		name     string
		plan     *Plan
		graph    *graph.Graph
		contains []string
		excludes []string
	}{
		{
			name: "minimal plan one step",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{
							Node: "searchFlights",
							Values: map[string]StepValue{
								"origin":      {Default: "DEN"},
								"destination": {Default: "SFO"},
							},
						},
					},
				},
			},
			graph: nil,
			contains: []string{
				"Steps (1):",
				"1. searchFlights",
				`origin: "DEN"`,
				`destination: "SFO"`,
			},
			excludes: []string{
				"Plan:",
				"Goal:",
				"Constraints:",
				"Cleanup:",
				"Verification:",
			},
		},
		{
			name: "full plan with all sections",
			plan: &Plan{
				Metadata: Metadata{
					Created:      time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC),
					Prompt:       "Book cheapest direct flight",
					GraphVersion: "1.0.0",
				},
				Intent: Intent{
					Goal:        "commitBooking",
					Description: "Book the cheapest direct flight",
					Constraints: &Constraints{
						Hard: []Constraint{
							{Name: "directOnly", Description: "Only non-stop flights"},
						},
						Soft: []Constraint{
							{Name: "cheapest", Description: "Prefer lowest price"},
						},
						Free: []string{"Any departure time"},
					},
				},
				Execution: Execution{
					Steps: []Step{
						{
							Node:        "searchFlights",
							Description: "Search for flights",
							Values: map[string]StepValue{
								"origin": {Default: "DEN"},
							},
							Assertions: &Assertions{
								Mechanical: []MechanicalAssertion{
									{Type: "status", Expect: 200},
									{Type: "fieldExists", Path: "$.results"},
								},
								Semantic: []string{"Should contain flights"},
							},
						},
						{
							Node:      "commitBooking",
							DependsOn: []string{"searchFlights"},
							IsGoal:    true,
						},
					},
					Cleanup: []CleanupStep{
						{Node: "ignoreWorkbench", RunOn: "always"},
					},
				},
			},
			graph: nil,
			contains: []string{
				"Plan: Book the cheapest direct flight",
				"Goal: commitBooking",
				"Generated: 2026-02-07 12:00",
				"Graph: 1.0.0",
				"Steps (2):",
				"1. searchFlights — Search for flights",
				`origin: "DEN"`,
				"Assertions: status 200, fieldExists $.results, semantic: Should contain flights",
				"2. commitBooking [GOAL]",
				"Depends on: searchFlights",
				"Constraints:",
				"Hard: directOnly — Only non-stop flights",
				"Soft: cheapest — Prefer lowest price",
				"Free: Any departure time",
				"Cleanup:",
				"- ignoreWorkbench (always)",
			},
		},
		{
			name: "goal annotation with graph description",
			plan: &Plan{
				Intent: Intent{
					Goal: "commitBooking",
				},
				Execution: Execution{
					Steps: []Step{
						{Node: "commitBooking", IsGoal: true},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"commitBooking": {Name: "commitBooking", Description: "Finalize the booking"},
				},
			},
			contains: []string{
				"Goal: commitBooking — Finalize the booking",
				"1. commitBooking [GOAL] — Finalize the booking",
			},
		},
		{
			name: "from references with selection",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{
							Node: "priceOffer",
							Values: map[string]StepValue{
								"offeringId": {
									From: "searchFlights.catalogOfferings",
									Select: &SelectionConfig{
										Strategy: "first",
										Field:    "Identifier.value",
									},
								},
							},
						},
					},
				},
			},
			contains: []string{
				"offeringId: from searchFlights.catalogOfferings (select: first, field: Identifier.value)",
			},
		},
		{
			name: "from reference with filter",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{
							Node: "priceOffer",
							Values: map[string]StepValue{
								"offeringId": {
									From: "searchFlights.offerings",
									Select: &SelectionConfig{
										Strategy: "filter",
										Filter:   "price < 500",
									},
								},
							},
						},
					},
				},
			},
			contains: []string{
				"offeringId: from searchFlights.offerings (select: filter, filter: price < 500)",
			},
		},
		{
			name: "nil graph does not panic",
			plan: &Plan{
				Intent: Intent{Goal: "test"},
				Execution: Execution{
					Steps: []Step{
						{Node: "test"},
					},
				},
			},
			graph: nil,
			contains: []string{
				"Goal: test",
				"1. test",
			},
		},
		{
			name: "empty constraints omitted",
			plan: &Plan{
				Intent: Intent{
					Goal:        "test",
					Constraints: &Constraints{},
				},
				Execution: Execution{
					Steps: []Step{
						{Node: "test"},
					},
				},
			},
			excludes: []string{"Constraints:"},
		},
		{
			name: "verification section",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{Node: "createBooking"},
					},
					Verification: []VerificationStep{
						{Node: "getBooking", Purpose: "Confirm booking exists"},
					},
				},
			},
			contains: []string{
				"Verification:",
				"- getBooking — Confirm booking exists",
			},
		},
		{
			name: "expect failure step",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{
							Node:          "deleteBooking",
							ExpectFailure: &ExpectFailure{Status: []int{404, 410}},
						},
					},
				},
			},
			contains: []string{
				"Expect failure: status 404, 410",
			},
		},
		{
			name: "step description falls back to graph",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{Node: "searchFlights"},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"searchFlights": {Name: "searchFlights", Description: "Search available flights"},
				},
			},
			contains: []string{
				"1. searchFlights — Search available flights",
			},
		},
		{
			name: "metadata only prompt no description",
			plan: &Plan{
				Metadata: Metadata{Prompt: "Find flights to NYC"},
				Execution: Execution{
					Steps: []Step{
						{Node: "search"},
					},
				},
			},
			contains: []string{
				"Plan: Find flights to NYC",
			},
		},
		{
			name: "fieldEquals and predicate assertions",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{
							Node: "test",
							Assertions: &Assertions{
								Mechanical: []MechanicalAssertion{
									{Type: "fieldEquals", Path: "$.status", Value: "active"},
									{Type: "predicate", Expr: "response.count > 0"},
								},
							},
						},
					},
				},
			},
			contains: []string{
				"fieldEquals $.status=active",
				"predicate: response.count > 0",
			},
		},
		{
			name: "cleanup default runOn",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{Node: "test"},
					},
					Cleanup: []CleanupStep{
						{Node: "cleanupNode"},
					},
				},
			},
			contains: []string{
				"- cleanupNode (always)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatNarrative(tt.plan, tt.graph)

			for _, want := range tt.contains {
				assert.Contains(t, result, want, "expected to contain: %s", want)
			}
			for _, noWant := range tt.excludes {
				assert.NotContains(t, result, noWant, "expected NOT to contain: %s", noWant)
			}

			// Basic sanity: result should not be empty
			assert.NotEmpty(t, result)

			// No trailing whitespace on lines
			for _, line := range strings.Split(result, "\n") {
				assert.Equal(t, strings.TrimRight(line, " \t"), line, "line has trailing whitespace: %q", line)
			}
		})
	}
}
