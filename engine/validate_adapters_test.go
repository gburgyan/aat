package engine

import (
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAdapterOutputs(t *testing.T) {
	tests := []struct {
		name        string
		graph       *graph.Graph
		setup       func(*adapter.Registry)
		wantErr     bool
		wantErrors  []string // substrings to check in individual errors
		wantErrType bool     // expect *AdapterValidationError
	}{
		{
			name: "all outputs match",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"getPet": {
						Name:    "getPet",
						Adapter: "getPet",
						Outputs: []graph.Output{
							{Name: "petId", Type: "string"},
							{Name: "name", Type: "string"},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "getPet",
					Protocol: "http",
					Request:  adapter.TemplateRequest{Method: "GET", Path: "/pets/{{petId}}"},
					Response: adapter.TemplateResponse{
						Extract: map[string]string{
							"petId": "id",
							"name":  "name",
						},
					},
				}
				r.Register("getPet", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr: false,
		},
		{
			name: "graph output missing from template extract",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"getPet": {
						Name:    "getPet",
						Adapter: "getPet",
						Outputs: []graph.Output{
							{Name: "petId", Type: "string"},
							{Name: "status", Type: "string"},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "getPet",
					Protocol: "http",
					Request:  adapter.TemplateRequest{Method: "GET", Path: "/pets/{{petId}}"},
					Response: adapter.TemplateResponse{
						Extract: map[string]string{
							"petId": "id",
						},
					},
				}
				r.Register("getPet", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr:     true,
			wantErrType: true,
			wantErrors: []string{
				`node "getPet": graph declares output "status" but template does not extract it`,
			},
		},
		{
			name: "template extracts output not in graph",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"createPet": {
						Name:    "createPet",
						Adapter: "createPet",
						Outputs: []graph.Output{
							{Name: "petId", Type: "string"},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "createPet",
					Protocol: "http",
					Request:  adapter.TemplateRequest{Method: "POST", Path: "/pets"},
					Response: adapter.TemplateResponse{
						Extract: map[string]string{
							"petId":   "id",
							"unknown": "foo.bar",
						},
					},
				}
				r.Register("createPet", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr:     true,
			wantErrType: true,
			wantErrors: []string{
				`node "createPet": template extracts "unknown" but graph does not declare this output`,
			},
		},
		{
			name: "both directions in one node",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"updatePet": {
						Name:    "updatePet",
						Adapter: "updatePet",
						Outputs: []graph.Output{
							{Name: "petId", Type: "string"},
							{Name: "missing", Type: "string"},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "updatePet",
					Protocol: "http",
					Request:  adapter.TemplateRequest{Method: "PUT", Path: "/pets/{{petId}}"},
					Response: adapter.TemplateResponse{
						Extract: map[string]string{
							"petId": "id",
							"extra": "extra.field",
						},
					},
				}
				r.Register("updatePet", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr:     true,
			wantErrType: true,
			wantErrors: []string{
				`node "updatePet": graph declares output "missing" but template does not extract it`,
				`node "updatePet": template extracts "extra" but graph does not declare this output`,
			},
		},
		{
			name: "multiple nodes with mismatches",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"alpha": {
						Name:    "alpha",
						Adapter: "alpha",
						Outputs: []graph.Output{
							{Name: "x", Type: "string"},
							{Name: "ghost", Type: "string"},
						},
					},
					"beta": {
						Name:    "beta",
						Adapter: "beta",
						Outputs: []graph.Output{
							{Name: "y", Type: "string"},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmplA := adapter.Template{
					Adapter:  "alpha",
					Protocol: "http",
					Request:  adapter.TemplateRequest{Method: "GET", Path: "/a"},
					Response: adapter.TemplateResponse{
						Extract: map[string]string{"x": "x"},
					},
				}
				r.Register("alpha", adapter.NewTemplateAdapter(tmplA))

				tmplB := adapter.Template{
					Adapter:  "beta",
					Protocol: "http",
					Request:  adapter.TemplateRequest{Method: "GET", Path: "/b"},
					Response: adapter.TemplateResponse{
						Extract: map[string]string{
							"y":     "y",
							"bonus": "bonus",
						},
					},
				}
				r.Register("beta", adapter.NewTemplateAdapter(tmplB))
			},
			wantErr:     true,
			wantErrType: true,
			wantErrors: []string{
				`node "alpha": graph declares output "ghost" but template does not extract it`,
				`node "beta": template extracts "bonus" but graph does not declare this output`,
			},
		},
		{
			name: "non-template adapter skipped",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"custom": {
						Name:    "custom",
						Adapter: "custom.impl",
						Outputs: []graph.Output{
							{Name: "result", Type: "string"},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				// Register a non-template adapter (stubAdapter).
				r.Register("custom.impl", &stubAdapter{
					method:   "GET",
					path:     "/custom",
					response: map[string]any{"result": "ok"},
				})
			},
			wantErr: false,
		},
		{
			name: "node with empty outputs skipped",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"deletePet": {
						Name:    "deletePet",
						Adapter: "deletePet",
						Outputs: nil, // no outputs
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "deletePet",
					Protocol: "http",
					Request:  adapter.TemplateRequest{Method: "DELETE", Path: "/pets/{{petId}}"},
					Response: adapter.TemplateResponse{},
				}
				r.Register("deletePet", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr: false,
		},
		{
			name: "adapter not in registry skipped",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"missing": {
						Name:    "missing",
						Adapter: "doesNotExist",
						Outputs: []graph.Output{
							{Name: "id", Type: "string"},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				// Don't register anything — adapter not found.
			},
			wantErr: false, // not-found is skipped (same as non-template)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := adapter.NewRegistry()
			tt.setup(registry)

			err := ValidateAdapterOutputs(tt.graph, registry)
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}

			require.Error(t, err)

			if tt.wantErrType {
				var valErr *AdapterValidationError
				require.ErrorAs(t, err, &valErr)
				for _, want := range tt.wantErrors {
					assert.Contains(t, valErr.Errors, want)
				}
				assert.Len(t, valErr.Errors, len(tt.wantErrors))
			}
		})
	}
}

func TestAdapterValidationError_Error(t *testing.T) {
	err := &AdapterValidationError{
		Errors: []string{
			`node "a": graph declares output "x" but template does not extract it`,
			`node "b": template extracts "y" but graph does not declare this output`,
		},
	}
	msg := err.Error()
	assert.Contains(t, msg, "adapter output validation failed:")
	assert.Contains(t, msg, `node "a"`)
	assert.Contains(t, msg, `node "b"`)
}
