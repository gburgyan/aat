package engine

import (
	"context"
	"testing"
	"time"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- resolveNodeToStepID tests ---

func TestResolveNodeToStepID(t *testing.T) {
	tests := []struct {
		name     string
		nodeName string
		plan     *plan.Plan
		want     string
	}{
		{
			name:     "direct match",
			nodeName: "createWorkbench",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{Node: "createWorkbench"},
					},
				},
			},
			want: "createWorkbench",
		},
		{
			name:     "prefixed step ID",
			nodeName: "createWorkbench",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{ID: "inc0_createWorkbench", Node: "createWorkbench"},
					},
				},
			},
			want: "inc0_createWorkbench",
		},
		{
			name:     "no match",
			nodeName: "unknownNode",
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
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

// --- InjectGraphDefaultDeps tests ---

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

		p := &plan.Plan{
			Execution: plan.Execution{
				Steps: []plan.Step{
					{Node: "A"},
					{Node: "B"},
				},
			},
		}

		InjectGraphDefaultDeps(p, g)

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

		p := &plan.Plan{
			Execution: plan.Execution{
				Steps: []plan.Step{
					{Node: "A"},
					{
						Node: "B",
						Values: map[string]plan.StepValue{
							"input1": {Default: "explicit_value"},
						},
					},
				},
			},
		}

		InjectGraphDefaultDeps(p, g)

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

		p := &plan.Plan{
			Execution: plan.Execution{
				Steps: []plan.Step{
					// Only B is in the plan, A is not
					{Node: "B"},
				},
			},
		}

		InjectGraphDefaultDeps(p, g)

		// B should NOT depend on A because A is not in the plan
		assert.Empty(t, p.Execution.Steps[0].DependsOn)
	})

	t.Run("nil plan and graph are safe", func(t *testing.T) {
		// Should not panic
		InjectGraphDefaultDeps(nil, nil)
		InjectGraphDefaultDeps(nil, &graph.Graph{})
		InjectGraphDefaultDeps(&plan.Plan{}, nil)
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

		p := &plan.Plan{
			Execution: plan.Execution{
				Steps: []plan.Step{
					{Node: "A"},
					{Node: "B", DependsOn: []string{"A"}},
				},
			},
		}

		InjectGraphDefaultDeps(p, g)

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

// --- resolveGraphDefault tests ---

func TestResolveGraphDefault_Literal(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{
						Name: "currency",
						Type: "string",
						Default: &graph.InputDefault{
							Value: "USD",
						},
					},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{Node: "target"}

	// Use ResolveInputsWithContext which calls resolveGraphDefault at priority 4
	inputs, _, resolutions, err := ResolveInputsWithContext(
		context.Background(), step, g.Nodes["target"], g, state, nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "USD", inputs["currency"])

	// Verify resolution source
	require.Len(t, resolutions, 1)
	assert.Equal(t, "graph_default", resolutions[0].Source)
	assert.Equal(t, "USD", resolutions[0].FinalValue)
	assert.Equal(t, "currency", resolutions[0].InputName)
}

func TestResolveGraphDefault_Pool(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{
						Name: "airport",
						Type: "string",
						Default: &graph.InputDefault{
							Pool: []any{"DEN", "ORD", "ATL"},
						},
					},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{Node: "target"}

	rctx := &ResolveContext{
		Now:  time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC),
		Mode: "strict",
	}

	inputs, _, resolutions, err := ResolveInputsWithContext(
		context.Background(), step, g.Nodes["target"], g, state, rctx,
	)
	require.NoError(t, err)

	// The result should be one of the pool values
	val := inputs["airport"]
	assert.Contains(t, []any{"DEN", "ORD", "ATL"}, val)

	// Verify resolution source
	require.Len(t, resolutions, 1)
	assert.Equal(t, "graph_default", resolutions[0].Source)
}

func TestResolveGraphDefault_PoolWithoutContext(t *testing.T) {
	// Without enhanced context, pool picks the first value
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{
						Name: "airport",
						Type: "string",
						Default: &graph.InputDefault{
							Pool: []any{"DEN", "ORD", "ATL"},
						},
					},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{Node: "target"}

	// nil rctx: should pick first pool value
	inputs, _, resolutions, err := ResolveInputsWithContext(
		context.Background(), step, g.Nodes["target"], g, state, nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "DEN", inputs["airport"])

	require.Len(t, resolutions, 1)
	assert.Equal(t, "graph_default", resolutions[0].Source)
	assert.Equal(t, 0, resolutions[0].PoolIndex)
	assert.Equal(t, 3, resolutions[0].PoolSize)
}

func TestResolveGraphDefault_From(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"searchNode": {
				Name: "searchNode",
				Outputs: []graph.Output{
					{Name: "someOutput", Type: "string"},
				},
			},
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{
						Name: "refValue",
						Type: "string",
						Default: &graph.InputDefault{
							From: "searchNode.someOutput",
						},
					},
				},
			},
		},
	}

	state := NewRunState()
	state.StoreOutputs("searchNode", map[string]any{"someOutput": "found_value"})

	step := plan.Step{
		Node:      "target",
		DependsOn: []string{"searchNode"},
	}

	// Need rctx for plan context in from resolution
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "searchNode"},
				step,
			},
		},
	}
	rctx := &ResolveContext{
		Now:  time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC),
		Mode: "strict",
		Plan: p,
	}

	inputs, _, resolutions, err := ResolveInputsWithContext(
		context.Background(), step, g.Nodes["target"], g, state, rctx,
	)
	require.NoError(t, err)
	assert.Equal(t, "found_value", inputs["refValue"])

	// Verify resolution record
	require.Len(t, resolutions, 1)
	assert.Equal(t, "graph_default", resolutions[0].Source)
	assert.Equal(t, "found_value", resolutions[0].FinalValue)
	assert.Equal(t, "searchNode", resolutions[0].FromStep)
	assert.Equal(t, "someOutput", resolutions[0].FromOutput)
}

func TestResolveGraphDefault_PlanOverrides(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{
						Name: "currency",
						Type: "string",
						Default: &graph.InputDefault{
							Value: "graph_value",
						},
					},
				},
			},
		},
	}

	state := NewRunState()
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"currency": {Default: "plan_value"},
		},
	}

	inputs, _, resolutions, err := ResolveInputsWithContext(
		context.Background(), step, g.Nodes["target"], g, state, nil,
	)
	require.NoError(t, err)

	// Plan value should win over graph default
	assert.Equal(t, "plan_value", inputs["currency"])

	// Verify the resolution source is plan_default, not graph_default
	require.Len(t, resolutions, 1)
	assert.Equal(t, "plan_default", resolutions[0].Source)
	assert.Equal(t, "plan_value", resolutions[0].FinalValue)
}

// --- splitGraphDefaultRef tests ---

func TestSplitGraphDefaultRef(t *testing.T) {
	tests := []struct {
		name      string
		ref       string
		wantNode  string
		wantField string
		wantErr   bool
	}{
		{
			name:      "valid reference",
			ref:       "searchNode.someOutput",
			wantNode:  "searchNode",
			wantField: "someOutput",
		},
		{
			name:    "no dot",
			ref:     "searchNode",
			wantErr: true,
		},
		{
			name:    "empty node",
			ref:     ".someOutput",
			wantErr: true,
		},
		{
			name:    "empty field",
			ref:     "searchNode.",
			wantErr: true,
		},
		{
			name:      "nested field",
			ref:       "node.some.nested.path",
			wantNode:  "node",
			wantField: "some.nested.path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, field, err := splitGraphDefaultRef(tt.ref)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantNode, node)
				assert.Equal(t, tt.wantField, field)
			}
		})
	}
}

// --- splitFromNodeName tests ---

func TestSplitFromNodeName(t *testing.T) {
	assert.Equal(t, "searchNode", splitFromNodeName("searchNode.someOutput"))
	assert.Equal(t, "searchNode", splitFromNodeName("searchNode"))
	assert.Equal(t, "", splitFromNodeName(""))
}
