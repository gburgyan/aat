package plan

import (
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstantiate(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name  string
		plan  *Plan
		graph *graph.Graph
		check func(t *testing.T, result *Plan)
	}{
		{
			name: "graph default merged when plan value absent",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{Node: "searchAir", Values: map[string]StepValue{
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
			check: func(t *testing.T, result *Plan) {
				require.Len(t, result.Execution.Steps, 1)
				vals := result.Execution.Steps[0].Values
				assert.Equal(t, "DEN", vals["origin"].Default)
				assert.Equal(t, "JFK", vals["destination"].Default)
			},
		},
		{
			name: "plan value takes priority",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{Node: "searchAir", Values: map[string]StepValue{
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
			check: func(t *testing.T, result *Plan) {
				vals := result.Execution.Steps[0].Values
				assert.Equal(t, "LAX", vals["origin"].Default, "plan value should not be overwritten")
			},
		},
		{
			name: "empty StepValue preserved (blocks graph default)",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{Node: "searchAir", Values: map[string]StepValue{
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
			check: func(t *testing.T, result *Plan) {
				vals := result.Execution.Steps[0].Values
				sv := vals["returnDate"]
				assert.True(t, sv.IsEmpty(), "empty StepValue should be preserved as-is")
			},
		},
		{
			name: "optional input with no default omitted",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{Node: "searchAir", Values: map[string]StepValue{}},
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
			check: func(t *testing.T, result *Plan) {
				vals := result.Execution.Steps[0].Values
				_, exists := vals["seatPref"]
				assert.False(t, exists, "optional input without default should not appear")
			},
		},
		{
			name: "pool + constraint + strategy converted",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{Node: "searchAir", Values: map[string]StepValue{}},
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
			check: func(t *testing.T, result *Plan) {
				sv := result.Execution.Steps[0].Values["origin"]
				assert.Equal(t, []any{"DEN", "JFK", "LAX"}, sv.Pool)
				require.NotNil(t, sv.PoolStrategy)
				assert.Equal(t, "random", *sv.PoolStrategy)
				assert.Equal(t, "len(value) == 3", sv.Constraint)
			},
		},
		{
			name: "fromResolved reference converted",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{Node: "searchAir", Values: map[string]StepValue{}},
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
			check: func(t *testing.T, result *Plan) {
				sv := result.Execution.Steps[0].Values["returnOrigin"]
				assert.Equal(t, "destination", sv.FromResolved)
			},
		},
		{
			name: "from + select converted",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{Node: "createOffer", Values: map[string]StepValue{}},
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
			check: func(t *testing.T, result *Plan) {
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
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{ID: "inc0_searchAir", Node: "searchAir", Values: map[string]StepValue{
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
			check: func(t *testing.T, result *Plan) {
				vals := result.Execution.Steps[0].Values
				assert.Equal(t, "DEN", vals["origin"].Default)
				assert.Equal(t, "LAX", vals["destination"].Default)
			},
		},
		{
			name: "unknown node in step skipped gracefully",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{Node: "nonexistent", Values: map[string]StepValue{
							"foo": {Default: "bar"},
						}},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{},
			},
			check: func(t *testing.T, result *Plan) {
				require.Len(t, result.Execution.Steps, 1)
				assert.Equal(t, "bar", result.Execution.Steps[0].Values["foo"].Default)
			},
		},
		{
			name: "cleanup steps passed through unchanged",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{Node: "searchAir", Values: map[string]StepValue{}},
					},
					Cleanup: []CleanupStep{
						{Node: "deleteBooking", RunOn: "always"},
					},
				},
			},
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"searchAir": {Name: "searchAir", Inputs: []graph.Input{}},
				},
			},
			check: func(t *testing.T, result *Plan) {
				require.Len(t, result.Execution.Cleanup, 1)
				assert.Equal(t, "deleteBooking", result.Execution.Cleanup[0].Node)
				assert.Equal(t, "always", result.Execution.Cleanup[0].RunOn)
			},
		},
		{
			name: "deep copy isolation",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{Node: "searchAir", Values: map[string]StepValue{
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
			check: func(t *testing.T, result *Plan) {
				// Mutate the instantiated plan
				result.Execution.Steps[0].Values["origin"] = StepValue{Default: "MUTATED"}
				result.Execution.Steps[0].Values["extra"] = StepValue{Default: "NEW"}
			},
		},
		{
			name:  "nil plan returns nil",
			plan:  nil,
			graph: &graph.Graph{Nodes: map[string]*graph.Node{}},
			check: func(t *testing.T, result *Plan) {
				assert.Nil(t, result)
			},
		},
		{
			name: "nil graph returns nil",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{{Node: "a"}},
				},
			},
			graph: nil,
			check: func(t *testing.T, result *Plan) {
				assert.Nil(t, result)
			},
		},
		{
			name: "nil Values map initialized",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
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
			check: func(t *testing.T, result *Plan) {
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

			result := Instantiate(tt.plan, tt.graph)
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

// --- resolveNodeToStepID tests ---

func TestResolveNodeToStepID(t *testing.T) {
	tests := []struct {
		name     string
		nodeName string
		plan     *Plan
		want     string
	}{
		{
			name:     "direct match",
			nodeName: "createWorkbench",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{Node: "createWorkbench"},
					},
				},
			},
			want: "createWorkbench",
		},
		{
			name:     "prefixed step ID",
			nodeName: "createWorkbench",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{ID: "inc0_createWorkbench", Node: "createWorkbench"},
					},
				},
			},
			want: "inc0_createWorkbench",
		},
		{
			name:     "no match",
			nodeName: "unknownNode",
			plan: &Plan{
				Execution: Execution{
					Steps: []Step{
						{Node: "createWorkbench"},
						{Node: "searchFlights"},
					},
				},
			},
			want: "unknownNode",
		},
		{
			name:     "nil plan",
			nodeName: "createWorkbench",
			plan:     nil,
			want:     "createWorkbench",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveNodeToStepID(tt.nodeName, tt.plan)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- injectGraphDefaultDeps tests ---

func TestInjectGraphDefaultDeps(t *testing.T) {
	t.Run("adds implicit dependency from graph default from ref", func(t *testing.T) {
		g := &graph.Graph{
			Version: "1.0.0",
			Nodes: map[string]*graph.Node{
				"A": {
					Name: "A",
					Outputs: []graph.Output{
						{Name: "output1", Type: "string"},
					},
				},
				"B": {
					Name: "B",
					Inputs: []graph.Input{
						{
							Name: "input1",
							Type: "string",
							Default: &graph.InputDefault{
								From: "A.output1",
							},
						},
					},
				},
			},
		}

		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{Node: "A"},
					{Node: "B"},
				},
			},
		}

		injectGraphDefaultDeps(p, g, nil)

		// B should now depend on A
		assert.Contains(t, p.Execution.Steps[1].DependsOn, "A")
	})

	t.Run("plan provides explicit value - no injection", func(t *testing.T) {
		g := &graph.Graph{
			Version: "1.0.0",
			Nodes: map[string]*graph.Node{
				"A": {
					Name: "A",
					Outputs: []graph.Output{
						{Name: "output1", Type: "string"},
					},
				},
				"B": {
					Name: "B",
					Inputs: []graph.Input{
						{
							Name: "input1",
							Type: "string",
							Default: &graph.InputDefault{
								From: "A.output1",
							},
						},
					},
				},
			},
		}

		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{Node: "A"},
					{
						Node: "B",
						Values: map[string]StepValue{
							"input1": {Default: "explicit_value"},
						},
					},
				},
			},
		}

		injectGraphDefaultDeps(p, g, nil)

		// B should NOT depend on A because plan provides explicit value
		assert.Empty(t, p.Execution.Steps[1].DependsOn)
	})

	t.Run("referenced node not in plan - no injection", func(t *testing.T) {
		g := &graph.Graph{
			Version: "1.0.0",
			Nodes: map[string]*graph.Node{
				"A": {
					Name: "A",
					Outputs: []graph.Output{
						{Name: "output1", Type: "string"},
					},
				},
				"B": {
					Name: "B",
					Inputs: []graph.Input{
						{
							Name: "input1",
							Type: "string",
							Default: &graph.InputDefault{
								From: "A.output1",
							},
						},
					},
				},
			},
		}

		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					// Only B is in the plan, A is not
					{Node: "B"},
				},
			},
		}

		injectGraphDefaultDeps(p, g, nil)

		// B should NOT depend on A because A is not in the plan
		assert.Empty(t, p.Execution.Steps[0].DependsOn)
	})

	t.Run("nil plan and graph are safe", func(t *testing.T) {
		// Should not panic
		injectGraphDefaultDeps(nil, nil, nil)
		injectGraphDefaultDeps(nil, &graph.Graph{}, nil)
		injectGraphDefaultDeps(&Plan{}, nil, nil)
	})

	t.Run("does not duplicate existing dependency", func(t *testing.T) {
		g := &graph.Graph{
			Version: "1.0.0",
			Nodes: map[string]*graph.Node{
				"A": {
					Name: "A",
					Outputs: []graph.Output{
						{Name: "output1", Type: "string"},
					},
				},
				"B": {
					Name: "B",
					Inputs: []graph.Input{
						{
							Name: "input1",
							Type: "string",
							Default: &graph.InputDefault{
								From: "A.output1",
							},
						},
					},
				},
			},
		}

		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{Node: "A"},
					{Node: "B", DependsOn: []string{"A"}},
				},
			},
		}

		injectGraphDefaultDeps(p, g, nil)

		// B should still have exactly one "A" dependency
		count := 0
		for _, dep := range p.Execution.Steps[1].DependsOn {
			if dep == "A" {
				count++
			}
		}
		assert.Equal(t, 1, count)
	})
}

// --- splitFromNodeName tests ---

func TestSplitFromNodeName(t *testing.T) {
	assert.Equal(t, "searchNode", splitFromNodeName("searchNode.someOutput"))
	assert.Equal(t, "searchNode", splitFromNodeName("searchNode"))
	assert.Equal(t, "", splitFromNodeName(""))
}

// --- translateFromRef tests ---

func TestTranslateFromRef(t *testing.T) {
	t.Run("simple plan - no translation needed", func(t *testing.T) {
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{Node: "searchAir"},
				},
			},
		}
		result := translateFromRef("searchAir.offerings", p)
		assert.Equal(t, "searchAir.offerings", result)
	})

	t.Run("composed plan - translates node to step ID", func(t *testing.T) {
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{ID: "inc0_searchAir", Node: "searchAir"},
				},
			},
		}
		result := translateFromRef("searchAir.offerings", p)
		assert.Equal(t, "inc0_searchAir.offerings", result)
	})

	t.Run("node not in plan - unchanged", func(t *testing.T) {
		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{Node: "otherNode"},
				},
			},
		}
		result := translateFromRef("searchAir.offerings", p)
		assert.Equal(t, "searchAir.offerings", result)
	})

	t.Run("empty ref", func(t *testing.T) {
		p := &Plan{}
		result := translateFromRef("", p)
		assert.Equal(t, "", result)
	})
}

// --- InstantiateAndValidate tests ---

func TestInstantiateAndValidate(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		g := &graph.Graph{
			Version: "1.0.0",
			Nodes: map[string]*graph.Node{
				"searchAir": {
					Name: "searchAir",
					Inputs: []graph.Input{
						{Name: "origin", Type: "string"},
						{Name: "destination", Type: "string", Default: graph.LiteralDefault("JFK")},
					},
				},
			},
		}

		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{Node: "searchAir", Values: map[string]StepValue{
						"origin": {Default: "DEN"},
					}},
				},
			},
		}

		result, err := InstantiateAndValidate(p, g)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "DEN", result.Execution.Steps[0].Values["origin"].Default)
		assert.Equal(t, "JFK", result.Execution.Steps[0].Values["destination"].Default)
	})

	t.Run("validation error", func(t *testing.T) {
		g := &graph.Graph{
			Version: "1.0.0",
			Nodes: map[string]*graph.Node{
				"searchAir": {
					Name: "searchAir",
					Inputs: []graph.Input{
						{Name: "origin", Type: "string"}, // required, no default
					},
				},
			},
		}

		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{Node: "searchAir", Values: map[string]StepValue{}},
				},
			},
		}

		result, err := InstantiateAndValidate(p, g)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "required input")
	})

	t.Run("nil plan", func(t *testing.T) {
		g := &graph.Graph{Nodes: map[string]*graph.Node{}}
		result, err := InstantiateAndValidate(nil, g)
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("from ref translation in composed plan", func(t *testing.T) {
		g := &graph.Graph{
			Version: "1.0.0",
			Nodes: map[string]*graph.Node{
				"searchAir": {
					Name: "searchAir",
					Inputs: []graph.Input{
						{Name: "origin", Type: "string"},
					},
					Outputs: []graph.Output{
						{Name: "offerings", Type: "offering[]"},
					},
				},
				"createOffer": {
					Name: "createOffer",
					Inputs: []graph.Input{
						{Name: "offeringId", Type: "string", Default: &graph.InputDefault{
							From: "searchAir.offerings",
							Select: &graph.InputDefaultSelect{
								Strategy: "first",
								Field:    "id",
							},
						}},
					},
				},
			},
		}

		p := &Plan{
			Execution: Execution{
				Steps: []Step{
					{ID: "inc0_searchAir", Node: "searchAir", Values: map[string]StepValue{
						"origin": {Default: "DEN"},
					}},
					{ID: "inc0_createOffer", Node: "createOffer", DependsOn: []string{"inc0_searchAir"}},
				},
			},
		}

		result, err := InstantiateAndValidate(p, g)
		require.NoError(t, err)
		require.NotNil(t, result)

		// The from ref should be translated to use the step ID
		sv := result.Execution.Steps[1].Values["offeringId"]
		assert.Equal(t, "inc0_searchAir.offerings", sv.From)
	})
}
