package engine

import (
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstantiatePlan(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name     string
		plan     *plan.Plan
		graph    *graph.Graph
		check    func(t *testing.T, result *plan.Plan)
	}{
		{
			name: "graph default merged when plan value absent",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{Node: "searchAir", Values: map[string]plan.StepValue{
							"origin": {Default: "DEN"},
						}},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"searchAir": {
						Name: "searchAir",
						Inputs: []graph.Input{
							{Name: "origin", Type: "string"},
							{Name: "destination", Type: "string", Default: graph.LiteralDefault("JFK")},
						},
					},
				},
			},
			check: func(t *testing.T, result *plan.Plan) {
				require.Len(t, result.Execution.Steps, 1)
				vals := result.Execution.Steps[0].Values
				assert.Equal(t, "DEN", vals["origin"].Default)
				assert.Equal(t, "JFK", vals["destination"].Default)
			},
		},
		{
			name: "plan value takes priority",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{Node: "searchAir", Values: map[string]plan.StepValue{
							"origin": {Default: "LAX"},
						}},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"searchAir": {
						Name: "searchAir",
						Inputs: []graph.Input{
							{Name: "origin", Type: "string", Default: graph.LiteralDefault("DEN")},
						},
					},
				},
			},
			check: func(t *testing.T, result *plan.Plan) {
				vals := result.Execution.Steps[0].Values
				assert.Equal(t, "LAX", vals["origin"].Default, "plan value should not be overwritten")
			},
		},
		{
			name: "empty StepValue preserved (blocks graph default)",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{Node: "searchAir", Values: map[string]plan.StepValue{
							"returnDate": {}, // explicitly empty
						}},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"searchAir": {
						Name: "searchAir",
						Inputs: []graph.Input{
							{Name: "returnDate", Type: "date", Default: graph.LiteralDefault("2025-12-25")},
						},
					},
				},
			},
			check: func(t *testing.T, result *plan.Plan) {
				vals := result.Execution.Steps[0].Values
				sv := vals["returnDate"]
				assert.True(t, sv.IsEmpty(), "empty StepValue should be preserved as-is")
			},
		},
		{
			name: "optional input with no default omitted",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{Node: "searchAir", Values: map[string]plan.StepValue{}},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"searchAir": {
						Name: "searchAir",
						Inputs: []graph.Input{
							{Name: "seatPref", Type: "string", Optional: true},
						},
					},
				},
			},
			check: func(t *testing.T, result *plan.Plan) {
				vals := result.Execution.Steps[0].Values
				_, exists := vals["seatPref"]
				assert.False(t, exists, "optional input without default should not appear")
			},
		},
		{
			name: "pool + constraint + strategy converted",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{Node: "searchAir", Values: map[string]plan.StepValue{}},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"searchAir": {
						Name: "searchAir",
						Inputs: []graph.Input{
							{Name: "origin", Type: "string", Default: &graph.InputDefault{
								Pool:         []any{"DEN", "JFK", "LAX"},
								PoolStrategy: strPtr("random"),
								Constraint:   "len(value) == 3",
							}},
						},
					},
				},
			},
			check: func(t *testing.T, result *plan.Plan) {
				sv := result.Execution.Steps[0].Values["origin"]
				assert.Equal(t, []any{"DEN", "JFK", "LAX"}, sv.Pool)
				require.NotNil(t, sv.PoolStrategy)
				assert.Equal(t, "random", *sv.PoolStrategy)
				assert.Equal(t, "len(value) == 3", sv.Constraint)
			},
		},
		{
			name: "fromResolved reference converted",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{Node: "searchAir", Values: map[string]plan.StepValue{}},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"searchAir": {
						Name: "searchAir",
						Inputs: []graph.Input{
							{Name: "returnOrigin", Type: "string", Default: &graph.InputDefault{
								FromResolved: "destination",
							}},
						},
					},
				},
			},
			check: func(t *testing.T, result *plan.Plan) {
				sv := result.Execution.Steps[0].Values["returnOrigin"]
				assert.Equal(t, "destination", sv.FromResolved)
			},
		},
		{
			name: "from + select converted",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{Node: "createOffer", Values: map[string]plan.StepValue{}},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"createOffer": {
						Name: "createOffer",
						Inputs: []graph.Input{
							{Name: "offeringId", Type: "string", Default: &graph.InputDefault{
								From: "searchAir.offerings",
								Select: &graph.InputDefaultSelect{
									Strategy:  "first",
									Field:     "id",
									Filter:    "price < 500",
									SortField: "price",
								},
							}},
						},
					},
				},
			},
			check: func(t *testing.T, result *plan.Plan) {
				sv := result.Execution.Steps[0].Values["offeringId"]
				assert.Equal(t, "searchAir.offerings", sv.From)
				require.NotNil(t, sv.Select)
				assert.Equal(t, "first", sv.Select.Strategy)
				assert.Equal(t, "id", sv.Select.Field)
				assert.Equal(t, "price < 500", sv.Select.Filter)
				assert.Equal(t, "price", sv.Select.SortField)
			},
		},
		{
			name: "prefixed step IDs work (composed plans)",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{ID: "inc0_searchAir", Node: "searchAir", Values: map[string]plan.StepValue{
							"origin": {Default: "DEN"},
						}},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"searchAir": {
						Name: "searchAir",
						Inputs: []graph.Input{
							{Name: "origin", Type: "string"},
							{Name: "destination", Type: "string", Default: graph.LiteralDefault("LAX")},
						},
					},
				},
			},
			check: func(t *testing.T, result *plan.Plan) {
				vals := result.Execution.Steps[0].Values
				assert.Equal(t, "DEN", vals["origin"].Default)
				assert.Equal(t, "LAX", vals["destination"].Default)
			},
		},
		{
			name: "unknown node in step skipped gracefully",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{Node: "nonexistent", Values: map[string]plan.StepValue{
							"foo": {Default: "bar"},
						}},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{},
			},
			check: func(t *testing.T, result *plan.Plan) {
				require.Len(t, result.Execution.Steps, 1)
				assert.Equal(t, "bar", result.Execution.Steps[0].Values["foo"].Default)
			},
		},
		{
			name: "cleanup steps passed through unchanged",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{Node: "searchAir", Values: map[string]plan.StepValue{}},
					},
					Cleanup: []plan.CleanupStep{
						{Node: "deleteBooking", RunOn: "always"},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"searchAir": {Name: "searchAir", Inputs: []graph.Input{}},
				},
			},
			check: func(t *testing.T, result *plan.Plan) {
				require.Len(t, result.Execution.Cleanup, 1)
				assert.Equal(t, "deleteBooking", result.Execution.Cleanup[0].Node)
				assert.Equal(t, "always", result.Execution.Cleanup[0].RunOn)
			},
		},
		{
			name: "deep copy isolation",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{Node: "searchAir", Values: map[string]plan.StepValue{
							"origin": {Default: "DEN"},
						}},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"searchAir": {
						Name: "searchAir",
						Inputs: []graph.Input{
							{Name: "origin", Type: "string"},
							{Name: "destination", Type: "string", Default: graph.LiteralDefault("JFK")},
						},
					},
				},
			},
			check: func(t *testing.T, result *plan.Plan) {
				// Mutate the instantiated plan
				result.Execution.Steps[0].Values["origin"] = plan.StepValue{Default: "MUTATED"}
				result.Execution.Steps[0].Values["extra"] = plan.StepValue{Default: "NEW"}
			},
		},
		{
			name: "nil plan returns nil",
			plan:  nil,
			graph: &graph.Graph{Nodes: map[string]*graph.Node{}},
			check: func(t *testing.T, result *plan.Plan) {
				assert.Nil(t, result)
			},
		},
		{
			name: "nil graph returns nil",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{{Node: "a"}},
				},
			},
			graph: nil,
			check: func(t *testing.T, result *plan.Plan) {
				assert.Nil(t, result)
			},
		},
		{
			name: "nil Values map initialized",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{Node: "searchAir"},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"searchAir": {
						Name: "searchAir",
						Inputs: []graph.Input{
							{Name: "origin", Type: "string", Default: graph.LiteralDefault("DEN")},
						},
					},
				},
			},
			check: func(t *testing.T, result *plan.Plan) {
				require.NotNil(t, result.Execution.Steps[0].Values)
				assert.Equal(t, "DEN", result.Execution.Steps[0].Values["origin"].Default)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture original plan state for isolation check
			var origOrigin any
			if tt.name == "deep copy isolation" && tt.plan != nil {
				origOrigin = tt.plan.Execution.Steps[0].Values["origin"].Default
			}

			result := InstantiatePlan(tt.plan, tt.graph)
			tt.check(t, result)

			// Verify deep copy isolation: original must not have been mutated
			if tt.name == "deep copy isolation" {
				assert.Equal(t, origOrigin, tt.plan.Execution.Steps[0].Values["origin"].Default,
					"original plan should not be mutated")
				_, hasExtra := tt.plan.Execution.Steps[0].Values["extra"]
				assert.False(t, hasExtra, "original plan should not have 'extra' key")
			}
		})
	}
}

func TestInputDefaultToStepValue(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	t.Run("literal value", func(t *testing.T) {
		d := &graph.InputDefault{Value: "hello"}
		sv := inputDefaultToStepValue(d)
		assert.Equal(t, "hello", sv.Default)
		assert.False(t, sv.IsEmpty())
	})

	t.Run("pool with strategy", func(t *testing.T) {
		d := &graph.InputDefault{
			Pool:         []any{"a", "b", "c"},
			PoolStrategy: strPtr("random"),
		}
		sv := inputDefaultToStepValue(d)
		assert.Equal(t, []any{"a", "b", "c"}, sv.Pool)
		require.NotNil(t, sv.PoolStrategy)
		assert.Equal(t, "random", *sv.PoolStrategy)
		// Mutating the copy should not affect the original
		sv.Pool[0] = "mutated"
		assert.Equal(t, "a", d.Pool[0])
	})

	t.Run("from with select", func(t *testing.T) {
		d := &graph.InputDefault{
			From: "node.field",
			Select: &graph.InputDefaultSelect{
				Strategy: "min",
				Field:    "price",
				Filter:   "active == true",
				Index:    2,
				Prompt:   "pick cheapest",
			},
		}
		sv := inputDefaultToStepValue(d)
		assert.Equal(t, "node.field", sv.From)
		require.NotNil(t, sv.Select)
		assert.Equal(t, "min", sv.Select.Strategy)
		assert.Equal(t, "price", sv.Select.Field)
		assert.Equal(t, "active == true", sv.Select.Filter)
		assert.Equal(t, 2, sv.Select.Index)
		assert.Equal(t, "pick cheapest", sv.Select.Prompt)
	})
}
