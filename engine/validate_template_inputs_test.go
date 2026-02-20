package engine

import (
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateTemplateInputs(t *testing.T) {
	tests := []struct {
		name       string
		graph      *graph.Graph
		setup      func(*adapter.Registry)
		wantErr    bool
		wantErrMsg string // substring to check
	}{
		{
			name: "all required placeholders satisfied by required inputs",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"createPet": {
						Name:    "createPet",
						Adapter: "createPet",
						Inputs: []graph.Input{
							{Name: "name", Type: "string"},
							{Name: "species", Type: "string"},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "createPet",
					Protocol: "http",
					Request: adapter.TemplateRequest{
						Method: "POST",
						Path:   "/pets",
						Body:   `{"name": "{{name}}", "species": "{{species}}"}`,
					},
				}
				r.Register("createPet", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr: false,
		},
		{
			name: "optional input with default pool passes",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"createPet": {
						Name:    "createPet",
						Adapter: "createPet",
						Inputs: []graph.Input{
							{Name: "name", Type: "string"},
							{Name: "color", Type: "string", Optional: true, Default: &graph.InputDefault{
								Pool: []any{"red", "blue", "green"},
							}},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "createPet",
					Protocol: "http",
					Request: adapter.TemplateRequest{
						Method: "POST",
						Path:   "/pets",
						Body:   `{"name": "{{name}}", "color": "{{color}}"}`,
					},
				}
				r.Register("createPet", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr: false,
		},
		{
			name: "optional input with default value passes",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"createPet": {
						Name:    "createPet",
						Adapter: "createPet",
						Inputs: []graph.Input{
							{Name: "name", Type: "string"},
							{Name: "color", Type: "string", Optional: true, Default: &graph.InputDefault{
								Value: "brown",
							}},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "createPet",
					Protocol: "http",
					Request: adapter.TemplateRequest{
						Method: "POST",
						Path:   "/pets",
						Body:   `{"name": "{{name}}", "color": "{{color}}"}`,
					},
				}
				r.Register("createPet", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr: false,
		},
		{
			name: "required placeholder for optional input with no default fails",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"createPet": {
						Name:    "createPet",
						Adapter: "createPet",
						Inputs: []graph.Input{
							{Name: "name", Type: "string"},
							{Name: "color", Type: "string", Optional: true},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "createPet",
					Protocol: "http",
					Request: adapter.TemplateRequest{
						Method: "POST",
						Path:   "/pets",
						Body:   `{"name": "{{name}}", "color": "{{color}}"}`,
					},
				}
				r.Register("createPet", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr:    true,
			wantErrMsg: `node "createPet": template requires placeholder "color" but graph input is optional with no default`,
		},
		{
			name: "non-template adapter skipped",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"customNode": {
						Name:    "customNode",
						Adapter: "customAdapter",
						Inputs: []graph.Input{
							{Name: "data", Type: "string", Optional: true},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				// Don't register anything — simulates a non-template adapter
			},
			wantErr: false,
		},
		{
			name: "placeholder not in graph inputs skipped (env value)",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"callAPI": {
						Name:    "callAPI",
						Adapter: "callAPI",
						Inputs: []graph.Input{
							{Name: "id", Type: "string"},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "callAPI",
					Protocol: "http",
					Request: adapter.TemplateRequest{
						Method: "GET",
						Path:   "/api/{{workbenchId}}/items/{{id}}",
					},
				}
				r.Register("callAPI", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr: false,
		},
		{
			name: "conditional placeholder for optional input passes",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"createOrder": {
						Name:    "createOrder",
						Adapter: "createOrder",
						Inputs: []graph.Input{
							{Name: "item", Type: "string"},
							{Name: "note", Type: "string", Optional: true},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "createOrder",
					Protocol: "http",
					Request: adapter.TemplateRequest{
						Method: "POST",
						Path:   "/orders",
						Body:   `{"item": "{{item}}"{{?note}}, "note": "{{note}}"{{/note}}}`,
					},
				}
				r.Register("createOrder", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr: false,
		},
		{
			name: "multiple errors collected",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"createPet": {
						Name:    "createPet",
						Adapter: "createPet",
						Inputs: []graph.Input{
							{Name: "name", Type: "string"},
							{Name: "color", Type: "string", Optional: true},
							{Name: "breed", Type: "string", Optional: true},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "createPet",
					Protocol: "http",
					Request: adapter.TemplateRequest{
						Method: "POST",
						Path:   "/pets",
						Body:   `{"name": "{{name}}", "color": "{{color}}", "breed": "{{breed}}"}`,
					},
				}
				r.Register("createPet", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr:    true,
			wantErrMsg: "template input validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := adapter.NewRegistry()
			tt.setup(registry)

			err := ValidateTemplateInputs(tt.graph, registry)
			if tt.wantErr {
				require.Error(t, err)
				var valErr *TemplateInputValidationError
				require.ErrorAs(t, err, &valErr)
				if tt.wantErrMsg != "" {
					assert.Contains(t, err.Error(), tt.wantErrMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateTemplateInputsForPlan(t *testing.T) {
	tests := []struct {
		name       string
		graph      *graph.Graph
		setup      func(*adapter.Registry)
		plan       *plan.Plan
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "plan step provides value for optional input passes",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"createPet": {
						Name:    "createPet",
						Adapter: "createPet",
						Inputs: []graph.Input{
							{Name: "name", Type: "string"},
							{Name: "color", Type: "string", Optional: true},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "createPet",
					Protocol: "http",
					Request: adapter.TemplateRequest{
						Method: "POST",
						Path:   "/pets",
						Body:   `{"name": "{{name}}", "color": "{{color}}"}`,
					},
				}
				r.Register("createPet", adapter.NewTemplateAdapter(tmpl))
			},
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{
							Node: "createPet",
							Values: map[string]plan.StepValue{
								"color": {Default: "blue"},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "plan step without value for optional input fails",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"createPet": {
						Name:    "createPet",
						Adapter: "createPet",
						Inputs: []graph.Input{
							{Name: "name", Type: "string"},
							{Name: "color", Type: "string", Optional: true},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "createPet",
					Protocol: "http",
					Request: adapter.TemplateRequest{
						Method: "POST",
						Path:   "/pets",
						Body:   `{"name": "{{name}}", "color": "{{color}}"}`,
					},
				}
				r.Register("createPet", adapter.NewTemplateAdapter(tmpl))
			},
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{
							Node: "createPet",
						},
					},
				},
			},
			wantErr:    true,
			wantErrMsg: `node "createPet": template requires placeholder "color"`,
		},
		{
			name: "only plan-used nodes checked",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"createPet": {
						Name:    "createPet",
						Adapter: "createPet",
						Inputs: []graph.Input{
							{Name: "name", Type: "string"},
						},
					},
					"brokenNode": {
						Name:    "brokenNode",
						Adapter: "brokenNode",
						Inputs: []graph.Input{
							{Name: "data", Type: "string", Optional: true},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl1 := adapter.Template{
					Adapter:  "createPet",
					Protocol: "http",
					Request: adapter.TemplateRequest{
						Method: "POST",
						Path:   "/pets",
						Body:   `{"name": "{{name}}"}`,
					},
				}
				r.Register("createPet", adapter.NewTemplateAdapter(tmpl1))
				// brokenNode has unconditional placeholder for optional input
				tmpl2 := adapter.Template{
					Adapter:  "brokenNode",
					Protocol: "http",
					Request: adapter.TemplateRequest{
						Method: "POST",
						Path:   "/broken",
						Body:   `{"data": "{{data}}"}`,
					},
				}
				r.Register("brokenNode", adapter.NewTemplateAdapter(tmpl2))
			},
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps: []plan.Step{
						{Node: "createPet"},
					},
				},
			},
			wantErr: false, // brokenNode not in plan, not checked
		},
		{
			name: "cleanup step with issue is caught",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"cleanup": {
						Name:    "cleanup",
						Adapter: "cleanup",
						Inputs: []graph.Input{
							{Name: "id", Type: "string"},
							{Name: "reason", Type: "string", Optional: true},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "cleanup",
					Protocol: "http",
					Request: adapter.TemplateRequest{
						Method: "DELETE",
						Path:   "/items/{{id}}?reason={{reason}}",
					},
				}
				r.Register("cleanup", adapter.NewTemplateAdapter(tmpl))
			},
			plan: &plan.Plan{
				Execution: plan.Execution{
					Steps:   []plan.Step{},
					Cleanup: []plan.CleanupStep{{Node: "cleanup"}},
				},
			},
			wantErr:    true,
			wantErrMsg: `node "cleanup": template requires placeholder "reason"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := adapter.NewRegistry()
			tt.setup(registry)

			err := ValidateTemplateInputsForPlan(tt.graph, registry, tt.plan)
			if tt.wantErr {
				require.Error(t, err)
				var valErr *TemplateInputValidationError
				require.ErrorAs(t, err, &valErr)
				if tt.wantErrMsg != "" {
					assert.Contains(t, err.Error(), tt.wantErrMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
