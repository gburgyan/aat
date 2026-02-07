package adapter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTemplate(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
		check   func(t *testing.T, tmpl *Template)
	}{
		{
			name: "valid POST with all fields",
			yaml: `
adapter: test.search
protocol: http
request:
  method: POST
  path: /v2/search
  headers:
    Content-Type: application/json
  body: '{"q": "test"}'
response:
  extract:
    id: "$.data.id"
  validate:
    schema: schemas/search.json
`,
			check: func(t *testing.T, tmpl *Template) {
				assert.Equal(t, "test.search", tmpl.Adapter)
				assert.Equal(t, "http", tmpl.Protocol)
				assert.Equal(t, "POST", tmpl.Request.Method)
				assert.Equal(t, "/v2/search", tmpl.Request.Path)
				assert.Equal(t, "application/json", tmpl.Request.Headers["Content-Type"])
				assert.Equal(t, `{"q": "test"}`, tmpl.Request.Body)
				assert.Equal(t, "$.data.id", tmpl.Response.Extract["id"])
				require.NotNil(t, tmpl.Response.Validate)
				assert.Equal(t, "schemas/search.json", tmpl.Response.Validate.Schema)
			},
		},
		{
			name: "valid GET no body",
			yaml: `
adapter: test.status
protocol: http
request:
  method: GET
  path: /v2/status
response:
  extract:
    healthy: "$.healthy"
`,
			check: func(t *testing.T, tmpl *Template) {
				assert.Equal(t, "GET", tmpl.Request.Method)
				assert.Empty(t, tmpl.Request.Body)
				assert.Nil(t, tmpl.Response.Validate)
			},
		},
		{
			name: "minimal defaults protocol",
			yaml: `
adapter: test.minimal
request:
  method: GET
  path: /ping
`,
			check: func(t *testing.T, tmpl *Template) {
				assert.Equal(t, "http", tmpl.Protocol)
				assert.Empty(t, tmpl.Response.Extract)
			},
		},
		{
			name:    "missing adapter",
			yaml:    "request:\n  method: GET\n  path: /test\n",
			wantErr: "adapter",
		},
		{
			name:    "missing method",
			yaml:    "adapter: test\nrequest:\n  path: /test\n",
			wantErr: "request.method",
		},
		{
			name:    "missing path",
			yaml:    "adapter: test\nrequest:\n  method: GET\n",
			wantErr: "request.path",
		},
		{
			name:    "invalid YAML",
			yaml:    "adapter: [unterminated\n  :::\n",
			wantErr: "invalid YAML",
		},
		{
			name:    "unsupported protocol",
			yaml:    "adapter: test\nprotocol: grpc\nrequest:\n  method: GET\n  path: /test\n",
			wantErr: "unsupported protocol",
		},
		{
			name: "validate schema captured",
			yaml: `
adapter: test.validated
request:
  method: POST
  path: /test
response:
  validate:
    schema: schemas/response.json
`,
			check: func(t *testing.T, tmpl *Template) {
				require.NotNil(t, tmpl.Response.Validate)
				assert.Equal(t, "schemas/response.json", tmpl.Response.Validate.Schema)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, err := ParseTemplate([]byte(tt.yaml))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, tmpl)
			}
		})
	}
}

func TestSubstitutePlaceholders(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		inputs  map[string]any
		config  *EnvironmentConfig
		want    string
		wantErr string
	}{
		{
			name:   "simple from inputs",
			tmpl:   "hello {{name}}",
			inputs: map[string]any{"name": "world"},
			want:   "hello world",
		},
		{
			name:   "multiple placeholders",
			tmpl:   "{{origin}} to {{destination}}",
			inputs: map[string]any{"origin": "JFK", "destination": "LAX"},
			want:   "JFK to LAX",
		},
		{
			name:   "config values fallback",
			tmpl:   "Bearer {{auth.token}}",
			inputs: map[string]any{},
			config: &EnvironmentConfig{Values: map[string]string{"auth.token": "abc123"}},
			want:   "Bearer abc123",
		},
		{
			name:   "input takes precedence over config",
			tmpl:   "{{key}}",
			inputs: map[string]any{"key": "from-input"},
			config: &EnvironmentConfig{Values: map[string]string{"key": "from-config"}},
			want:   "from-input",
		},
		{
			name:    "unresolved placeholder",
			tmpl:    "{{missing}}",
			inputs:  map[string]any{},
			wantErr: "missing",
		},
		{
			name:    "multiple unresolved lists all",
			tmpl:    "{{foo}} and {{bar}}",
			inputs:  map[string]any{},
			wantErr: "foo, bar",
		},
		{
			name:   "whitespace in placeholder",
			tmpl:   "{{ name }}",
			inputs: map[string]any{"name": "trimmed"},
			want:   "trimmed",
		},
		{
			name:   "no placeholders passthrough",
			tmpl:   "literal string",
			inputs: map[string]any{},
			want:   "literal string",
		},
		{
			name:   "non-string input int",
			tmpl:   "count={{count}}",
			inputs: map[string]any{"count": 42},
			want:   "count=42",
		},
		{
			name:   "non-string input bool",
			tmpl:   "active={{active}}",
			inputs: map[string]any{"active": true},
			want:   "active=true",
		},
		{
			name:   "nil config with all inputs present",
			tmpl:   "{{key}}",
			inputs: map[string]any{"key": "value"},
			config: nil,
			want:   "value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := substitutePlaceholders(tt.tmpl, tt.inputs, tt.config)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeJSONPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"$.data.flights", "data.flights"},
		{"data.flights", "data.flights"},
		{"$.items[0].id", "items.0.id"},
		{"$", ""},
		{"$.array[0][1].field", "array.0.1.field"},
		{"name", "name"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeJSONPath(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTemplateAdapter_BuildRequest(t *testing.T) {
	t.Run("POST with body substitution", func(t *testing.T) {
		a := NewTemplateAdapter(Template{
			Request: TemplateRequest{
				Method: "POST",
				Path:   "/v2/search",
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: `{"origin": "{{origin}}"}`,
			},
		})

		req, err := a.BuildRequest(
			map[string]any{"origin": "JFK"},
			&EnvironmentConfig{},
		)
		require.NoError(t, err)
		assert.Equal(t, "POST", req.Method)
		assert.Equal(t, "/v2/search", req.Path)
		assert.Equal(t, `{"origin": "JFK"}`, string(req.Body))
	})

	t.Run("GET without body", func(t *testing.T) {
		a := NewTemplateAdapter(Template{
			Request: TemplateRequest{
				Method: "GET",
				Path:   "/v2/status",
			},
		})

		req, err := a.BuildRequest(map[string]any{}, nil)
		require.NoError(t, err)
		assert.Equal(t, "GET", req.Method)
		assert.Nil(t, req.Body)
	})

	t.Run("header merging config then template", func(t *testing.T) {
		a := NewTemplateAdapter(Template{
			Request: TemplateRequest{
				Method: "GET",
				Path:   "/test",
				Headers: map[string]string{
					"X-Custom":     "template-value",
					"Content-Type": "application/json",
				},
			},
		})

		config := &EnvironmentConfig{
			Headers: map[string]string{
				"Authorization": "Bearer token",
				"Content-Type":  "text/plain",
			},
		}

		req, err := a.BuildRequest(map[string]any{}, config)
		require.NoError(t, err)
		assert.Equal(t, "Bearer token", req.Headers["Authorization"])
		assert.Equal(t, "application/json", req.Headers["Content-Type"]) // template wins
		assert.Equal(t, "template-value", req.Headers["X-Custom"])
	})

	t.Run("nil config", func(t *testing.T) {
		a := NewTemplateAdapter(Template{
			Request: TemplateRequest{
				Method:  "GET",
				Path:    "/test",
				Headers: map[string]string{"X-Custom": "value"},
			},
		})

		req, err := a.BuildRequest(map[string]any{}, nil)
		require.NoError(t, err)
		assert.Equal(t, "value", req.Headers["X-Custom"])
	})

	t.Run("path placeholder substitution", func(t *testing.T) {
		a := NewTemplateAdapter(Template{
			Request: TemplateRequest{
				Method: "POST",
				Path:   "/v2/offers/{{offeringId}}/price",
			},
		})

		req, err := a.BuildRequest(map[string]any{"offeringId": "offer-123"}, nil)
		require.NoError(t, err)
		assert.Equal(t, "/v2/offers/offer-123/price", req.Path)
	})

	t.Run("missing input error", func(t *testing.T) {
		a := NewTemplateAdapter(Template{
			Request: TemplateRequest{
				Method: "POST",
				Path:   "/v2/search",
				Body:   `{"origin": "{{origin}}"}`,
			},
		})

		_, err := a.BuildRequest(map[string]any{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "origin")
	})
}

func TestTemplateAdapter_ExtractOutputs(t *testing.T) {
	t.Run("scalar extraction", func(t *testing.T) {
		a := NewTemplateAdapter(Template{
			Response: TemplateResponse{
				Extract: map[string]string{
					"searchId": "$.data.searchId",
				},
			},
		})

		resp := &Response{
			StatusCode: 200,
			Body:       []byte(`{"data": {"searchId": "abc-123"}}`),
		}

		outputs, err := a.ExtractOutputs(resp)
		require.NoError(t, err)
		assert.Equal(t, "abc-123", outputs["searchId"])
	})

	t.Run("array extraction", func(t *testing.T) {
		a := NewTemplateAdapter(Template{
			Response: TemplateResponse{
				Extract: map[string]string{
					"flights": "$.data.flights",
				},
			},
		})

		resp := &Response{
			StatusCode: 200,
			Body:       []byte(`{"data": {"flights": [{"id": 1}, {"id": 2}]}}`),
		}

		outputs, err := a.ExtractOutputs(resp)
		require.NoError(t, err)
		flights, ok := outputs["flights"].([]any)
		require.True(t, ok, "expected []any, got %T", outputs["flights"])
		assert.Len(t, flights, 2)
	})

	t.Run("multiple outputs", func(t *testing.T) {
		a := NewTemplateAdapter(Template{
			Response: TemplateResponse{
				Extract: map[string]string{
					"id":   "$.data.id",
					"name": "$.data.name",
				},
			},
		})

		resp := &Response{
			StatusCode: 200,
			Body:       []byte(`{"data": {"id": "x", "name": "test"}}`),
		}

		outputs, err := a.ExtractOutputs(resp)
		require.NoError(t, err)
		assert.Equal(t, "x", outputs["id"])
		assert.Equal(t, "test", outputs["name"])
	})

	t.Run("missing path error", func(t *testing.T) {
		a := NewTemplateAdapter(Template{
			Response: TemplateResponse{
				Extract: map[string]string{
					"missing": "$.does.not.exist",
				},
			},
		})

		resp := &Response{
			StatusCode: 200,
			Body:       []byte(`{"data": {}}`),
		}

		_, err := a.ExtractOutputs(resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("non-JSON response", func(t *testing.T) {
		a := NewTemplateAdapter(Template{
			Response: TemplateResponse{
				Extract: map[string]string{
					"id": "$.id",
				},
			},
		})

		resp := &Response{
			StatusCode: 200,
			Body:       []byte(`not json at all`),
		}

		_, err := a.ExtractOutputs(resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not valid JSON")
	})

	t.Run("empty extract map", func(t *testing.T) {
		a := NewTemplateAdapter(Template{
			Response: TemplateResponse{},
		})

		resp := &Response{
			StatusCode: 200,
			Body:       []byte(`{"data": "irrelevant"}`),
		}

		outputs, err := a.ExtractOutputs(resp)
		require.NoError(t, err)
		assert.Empty(t, outputs)
	})

	t.Run("bare path without dollar prefix", func(t *testing.T) {
		a := NewTemplateAdapter(Template{
			Response: TemplateResponse{
				Extract: map[string]string{
					"id": "data.id",
				},
			},
		})

		resp := &Response{
			StatusCode: 200,
			Body:       []byte(`{"data": {"id": "bare-path"}}`),
		}

		outputs, err := a.ExtractOutputs(resp)
		require.NoError(t, err)
		assert.Equal(t, "bare-path", outputs["id"])
	})
}

func TestTemplateAdapter_ValidateInputs(t *testing.T) {
	a := NewTemplateAdapter(Template{})
	assert.Nil(t, a.ValidateInputs(map[string]any{"key": "value"}))
}

func TestTemplateAdapter_ValidateResponse(t *testing.T) {
	a := NewTemplateAdapter(Template{})
	assert.Nil(t, a.ValidateResponse(&Response{StatusCode: 200}))
}

func TestLoadTemplates(t *testing.T) {
	t.Run("valid directory", func(t *testing.T) {
		registry := NewRegistry()
		count, err := LoadTemplates("testdata/templates/valid", registry)
		require.NoError(t, err)
		assert.Equal(t, 3, count)

		names := registry.Names()
		assert.Contains(t, names, "travelport.searchFlights")
		assert.Contains(t, names, "travelport.getStatus")
		assert.Contains(t, names, "travelport.priceOffer")
	})

	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		registry := NewRegistry()
		count, err := LoadTemplates(dir, registry)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("invalid template error identifies file", func(t *testing.T) {
		registry := NewRegistry()
		_, err := LoadTemplates("testdata/templates/invalid", registry)
		require.Error(t, err)
		// Should mention the file that failed
		assert.Contains(t, err.Error(), ".yaml")
	})

	t.Run("non-existent directory", func(t *testing.T) {
		registry := NewRegistry()
		_, err := LoadTemplates("testdata/templates/does_not_exist", registry)
		require.Error(t, err)
	})

	t.Run("duplicate adapter names", func(t *testing.T) {
		dir := t.TempDir()

		tmplYAML := []byte("adapter: duplicate.name\nrequest:\n  method: GET\n  path: /test\n")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.yaml"), tmplYAML, 0644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "b.yaml"), tmplYAML, 0644))

		registry := NewRegistry()
		_, err := LoadTemplates(dir, registry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})
}

func TestParseTemplateFile(t *testing.T) {
	t.Run("valid file", func(t *testing.T) {
		tmpl, err := ParseTemplateFile("testdata/templates/valid/search_flights.yaml")
		require.NoError(t, err)
		assert.Equal(t, "travelport.searchFlights", tmpl.Adapter)
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := ParseTemplateFile("testdata/templates/nonexistent.yaml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading template file")
	})
}
