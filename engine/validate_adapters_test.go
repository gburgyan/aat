package engine

import (
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutputExtractPaths(t *testing.T) {
	g := &graph.Graph{Nodes: map[string]*graph.Node{
		"getCart": {Name: "getCart", Adapter: "getCart", Outputs: []graph.Output{
			{Name: "cartStatus"}, {Name: "firstSku"}, {Name: "lines"}, {Name: "itemCount"},
		}},
		"custom": {Name: "custom", Adapter: "customAdapter", Outputs: []graph.Output{{Name: "result"}}},
	}}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("getCart", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "getCart",
		Response: adapter.TemplateResponse{
			Extract: map[string]adapter.ExtractRule{
				"cartStatus": {Path: "status"},
				"firstSku":   {Path: "$.lines[0].sku"},
				"lines":      {Path: "lines", Fields: map[string]string{"sku": "sku"}},
			},
			Transform: "return outputs",
		},
	})))

	// itemCount has no extract rule but the transform may compute it; the
	// custom node has no template and is left to the name-based check.
	want := map[string]map[string]string{
		"getCart": {"cartStatus": "status", "firstSku": "lines.0.sku", "lines": "lines", "itemCount": ""},
	}
	assert.Equal(t, want, map[string]map[string]string(OutputExtractPaths(g, registry)))
}

func TestOutputExtractPaths_HeaderRule(t *testing.T) {
	g := &graph.Graph{Nodes: map[string]*graph.Node{
		"createCart": {Name: "createCart", Adapter: "createCart", Outputs: []graph.Output{{Name: "cartId"}, {Name: "cartUrl"}}},
	}}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("createCart", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "createCart",
		Response: adapter.TemplateResponse{Extract: map[string]adapter.ExtractRule{
			"cartId":  {Path: "id"},
			"cartUrl": {Header: "Location"},
		}},
	})))

	want := map[string]map[string]string{"createCart": {"cartId": "id", "cartUrl": ""}}
	assert.Equal(t, want, map[string]map[string]string(OutputExtractPaths(g, registry)), "a header output isn't in the body schema")
}

func TestValidateAdapterOutputs_DefaultShape(t *testing.T) {
	g := &graph.Graph{Nodes: map[string]*graph.Node{
		"listOrders": {Name: "listOrders", Adapter: "listOrders", Outputs: []graph.Output{
			{Name: "orderIds", Type: "string[]"}, {Name: "nextCursor", Type: "string"},
		}},
	}}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("listOrders", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "listOrders",
		Response: adapter.TemplateResponse{Extract: map[string]adapter.ExtractRule{
			"orderIds":   {Path: "orders.#.id", Default: ""},
			"nextCursor": {Path: "meta.after", Default: ""},
		}},
	})))

	err := ValidateAdapterOutputs(g, registry)

	require.Error(t, err)
	assert.Equal(t, "adapter output validation failed:\n"+
		`  - node "listOrders": output "orderIds" default: a single value, where string[] takes a list`, err.Error())
}

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
						Extract: map[string]adapter.ExtractRule{
							"petId": {Path: "id"},
							"name":  {Path: "name"},
						},
					},
				}
				_ = r.Register("getPet", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr: false,
		},
		{
			name: "output computed by a transform",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"summarize": {
						Name:    "summarize",
						Adapter: "summarize",
						Outputs: []graph.Output{{Name: "total", Type: "integer"}},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "summarize",
					Protocol: "http",
					Request:  adapter.TemplateRequest{Method: "GET", Path: "/items"},
					Response: adapter.TemplateResponse{
						Transform: `return { total = #json_path("items") }`,
					},
				}
				_ = r.Register("summarize", adapter.NewTemplateAdapter(tmpl))
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
						Extract: map[string]adapter.ExtractRule{
							"petId": {Path: "id"},
						},
					},
				}
				_ = r.Register("getPet", adapter.NewTemplateAdapter(tmpl))
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
						Extract: map[string]adapter.ExtractRule{
							"petId":   {Path: "id"},
							"unknown": {Path: "foo.bar"},
						},
					},
				}
				_ = r.Register("createPet", adapter.NewTemplateAdapter(tmpl))
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
						Extract: map[string]adapter.ExtractRule{
							"petId": {Path: "id"},
							"extra": {Path: "extra.field"},
						},
					},
				}
				_ = r.Register("updatePet", adapter.NewTemplateAdapter(tmpl))
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
						Extract: map[string]adapter.ExtractRule{"x": {Path: "x"}},
					},
				}
				_ = r.Register("alpha", adapter.NewTemplateAdapter(tmplA))

				tmplB := adapter.Template{
					Adapter:  "beta",
					Protocol: "http",
					Request:  adapter.TemplateRequest{Method: "GET", Path: "/b"},
					Response: adapter.TemplateResponse{
						Extract: map[string]adapter.ExtractRule{
							"y":     {Path: "y"},
							"bonus": {Path: "bonus"},
						},
					},
				}
				_ = r.Register("beta", adapter.NewTemplateAdapter(tmplB))
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
				_ = r.Register("custom.impl", &stubAdapter{
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
				_ = r.Register("deletePet", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr: false,
		},
		{
			name: "elementField mismatch without transform",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"search": {
						Name:    "search",
						Adapter: "search",
						Outputs: []graph.Output{
							{
								Name: "items",
								Type: "array",
								ElementFields: []graph.Field{
									{Name: "id"},
									{Name: "name"},
									{Name: "extra"}, // not in template
								},
							},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "search",
					Protocol: "http",
					Request:  adapter.TemplateRequest{Method: "GET", Path: "/search"},
					Response: adapter.TemplateResponse{
						Extract: map[string]adapter.ExtractRule{
							"items": {
								Path: "items",
								Fields: map[string]string{
									"id":   "id",
									"name": "name",
								},
							},
						},
					},
				}
				_ = r.Register("search", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr:     true,
			wantErrType: true,
			wantErrors: []string{
				`node "search" output "items": graph elementField "extra" has no corresponding template field`,
			},
		},
		{
			name: "elementField mismatch relaxed with transform",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"search": {
						Name:    "search",
						Adapter: "search",
						Outputs: []graph.Output{
							{
								Name: "items",
								Type: "array",
								ElementFields: []graph.Field{
									{Name: "id"},
									{Name: "name"},
									{Name: "extra"}, // not in template fields, but transform may add it
								},
							},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "search",
					Protocol: "http",
					Request:  adapter.TemplateRequest{Method: "GET", Path: "/search"},
					Response: adapter.TemplateResponse{
						Extract: map[string]adapter.ExtractRule{
							"items": {
								Path: "items",
								Fields: map[string]string{
									"id":   "id",
									"name": "name",
								},
							},
						},
						Transform: `return outputs`, // has a transform
					},
				}
				_ = r.Register("search", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr: false, // no error — transform may populate "extra"
		},
		{
			name: "template field not in graph still errors with transform",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"search": {
						Name:    "search",
						Adapter: "search",
						Outputs: []graph.Output{
							{
								Name: "items",
								Type: "array",
								ElementFields: []graph.Field{
									{Name: "id"},
								},
							},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "search",
					Protocol: "http",
					Request:  adapter.TemplateRequest{Method: "GET", Path: "/search"},
					Response: adapter.TemplateResponse{
						Extract: map[string]adapter.ExtractRule{
							"items": {
								Path: "items",
								Fields: map[string]string{
									"id":     "id",
									"orphan": "orphan_path", // not in graph
								},
							},
						},
						Transform: `return outputs`,
					},
				}
				_ = r.Register("search", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr:     true,
			wantErrType: true,
			wantErrors: []string{
				`node "search" output "items": template field "orphan" has no corresponding graph elementField`,
			},
		},
		{
			name: "optional output without extract rule passes",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"itinerary": {
						Name:    "itinerary",
						Adapter: "itinerary",
						Outputs: []graph.Output{
							{Name: "id", Type: "string"},
							{Name: "maybeFop", Type: "string", Optional: true},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "itinerary",
					Protocol: "http",
					Request:  adapter.TemplateRequest{Method: "POST", Path: "/itinerary"},
					Response: adapter.TemplateResponse{
						Extract: map[string]adapter.ExtractRule{
							"id": {Path: "id"},
							// No extract rule for maybeFop — optional, so OK
						},
					},
				}
				_ = r.Register("itinerary", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr: false,
		},
		{
			name: "non-optional output without extract rule still fails",
			graph: &graph.Graph{
				Nodes: map[string]*graph.Node{
					"itinerary": {
						Name:    "itinerary",
						Adapter: "itinerary",
						Outputs: []graph.Output{
							{Name: "id", Type: "string"},
							{Name: "requiredFop", Type: "string"},
						},
					},
				},
			},
			setup: func(r *adapter.Registry) {
				tmpl := adapter.Template{
					Adapter:  "itinerary",
					Protocol: "http",
					Request:  adapter.TemplateRequest{Method: "POST", Path: "/itinerary"},
					Response: adapter.TemplateResponse{
						Extract: map[string]adapter.ExtractRule{
							"id": {Path: "id"},
						},
					},
				}
				_ = r.Register("itinerary", adapter.NewTemplateAdapter(tmpl))
			},
			wantErr:     true,
			wantErrType: true,
			wantErrors: []string{
				`node "itinerary": graph declares output "requiredFop" but template does not extract it`,
			},
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

func TestValidateAdapterOutputsForPlan(t *testing.T) {
	// Graph has two nodes: "good" (template matches) and "broken" (template mismatch).
	// The plan only uses "good", so validation should pass.
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"good": {
				Name:    "good",
				Adapter: "good",
				Outputs: []graph.Output{
					{Name: "result", Type: "string"},
				},
			},
			"broken": {
				Name:    "broken",
				Adapter: "broken",
				Outputs: []graph.Output{
					{Name: "x", Type: "string"},
					{Name: "missing", Type: "string"},
				},
			},
		},
	}

	registry := adapter.NewRegistry()
	_ = registry.Register("good", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "good",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "GET", Path: "/good"},
		Response: adapter.TemplateResponse{
			Extract: map[string]adapter.ExtractRule{"result": {Path: "result"}},
		},
	}))
	_ = registry.Register("broken", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "broken",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "GET", Path: "/broken"},
		Response: adapter.TemplateResponse{
			Extract: map[string]adapter.ExtractRule{"x": {Path: "x"}},
			// Missing "missing" — would fail full validation
		},
	}))

	t.Run("plan uses only good node", func(t *testing.T) {
		p := &plan.Plan{
			Execution: plan.Execution{
				Steps: []plan.Step{
					{Node: "good"},
				},
			},
		}
		err := ValidateAdapterOutputsForPlan(g, registry, p)
		assert.NoError(t, err)
	})

	t.Run("plan uses broken node fails", func(t *testing.T) {
		p := &plan.Plan{
			Execution: plan.Execution{
				Steps: []plan.Step{
					{Node: "broken"},
				},
			},
		}
		err := ValidateAdapterOutputsForPlan(g, registry, p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing")
	})

	t.Run("full validation catches broken", func(t *testing.T) {
		err := ValidateAdapterOutputs(g, registry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing")
	})

	t.Run("cleanup node included", func(t *testing.T) {
		p := &plan.Plan{
			Execution: plan.Execution{
				Steps: []plan.Step{
					{Node: "good"},
				},
				Cleanup: []plan.CleanupStep{
					{Node: "broken", RunOn: "always"},
				},
			},
		}
		err := ValidateAdapterOutputsForPlan(g, registry, p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing")
	})
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
