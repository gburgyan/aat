package mcp

import (
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAdapter implements adapter.Adapter but is not a *TemplateAdapter.
type mockAdapter struct{}

func (m *mockAdapter) BuildRequest(_ map[string]any, _ *adapter.EnvironmentConfig) (*adapter.Request, error) {
	return nil, nil
}
func (m *mockAdapter) ExtractOutputs(_ *adapter.Response) (map[string]any, error) { return nil, nil }
func (m *mockAdapter) ValidateInputs(_ map[string]any) *adapter.ValidationResult  { return nil }
func (m *mockAdapter) ValidateResponse(_ *adapter.Response) *adapter.ValidationResult {
	return nil
}

func newTemplateTestServer(reg *adapter.Registry) *Server {
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: reg,
		Manifest: &ProjectManifest{Name: "test"},
	}
	return NewServer(ctx)
}

// --- list_adapters ---

func TestHandleListAdapters_Empty(t *testing.T) {
	reg := adapter.NewRegistry()
	srv := newTemplateTestServer(reg)
	result := callTool(t, srv.handleListAdapters, nil)

	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "No adapters registered")
}

func TestHandleListAdapters_Multiple(t *testing.T) {
	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("zulu", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "zulu", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "GET", Path: "/z"},
	})))
	require.NoError(t, reg.Register("alpha", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "alpha", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "POST", Path: "/a"},
	})))
	require.NoError(t, reg.Register("mike", &mockAdapter{}))

	srv := newTemplateTestServer(reg)
	result := callTool(t, srv.handleListAdapters, nil)

	assert.False(t, result.IsError)
	text := resultText(t, result)

	// All three should appear
	assert.Contains(t, text, "alpha")
	assert.Contains(t, text, "mike")
	assert.Contains(t, text, "zulu")

	// Sorted order: alpha before mike before zulu
	alphaIdx := indexOf(text, "alpha")
	mikeIdx := indexOf(text, "mike")
	zuluIdx := indexOf(text, "zulu")
	assert.Less(t, alphaIdx, mikeIdx)
	assert.Less(t, mikeIdx, zuluIdx)
}

// --- inspect_template ---

func TestHandleInspectTemplate_Valid(t *testing.T) {
	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("search", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "search",
		Protocol: "http",
		Request: adapter.TemplateRequest{
			Method:  "POST",
			Path:    "/api/search",
			Headers: map[string]string{"Content-Type": "application/json", "X-Custom": "value"},
			Body:    `{"query": "{{q}}"}`,
		},
		Response: adapter.TemplateResponse{
			Extract: map[string]adapter.ExtractRule{
				"results": {Path: "$.data.results"},
				"count":   {Path: "$.data.count"},
			},
		},
	})))

	srv := newTemplateTestServer(reg)
	result := callTool(t, srv.handleInspectTemplate, map[string]any{"adapter": "search"})

	assert.False(t, result.IsError)
	text := resultText(t, result)

	assert.Contains(t, text, "# search")
	assert.Contains(t, text, "**Protocol:** http")
	assert.Contains(t, text, "**Method:** POST")
	assert.Contains(t, text, "`/api/search`")
	assert.Contains(t, text, "Content-Type")
	assert.Contains(t, text, "application/json")
	assert.Contains(t, text, "X-Custom")
	assert.Contains(t, text, `{"query": "{{q}}"}`)
	assert.Contains(t, text, "## Extract Rules")
	assert.Contains(t, text, "| count | $.data.count |")
	assert.Contains(t, text, "| results | $.data.results |")
}

func TestHandleInspectTemplate_MinimalTemplate(t *testing.T) {
	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("simple", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "simple",
		Protocol: "http",
		Request: adapter.TemplateRequest{
			Method: "GET",
			Path:   "/health",
		},
	})))

	srv := newTemplateTestServer(reg)
	result := callTool(t, srv.handleInspectTemplate, map[string]any{"adapter": "simple"})

	assert.False(t, result.IsError)
	text := resultText(t, result)

	assert.Contains(t, text, "**Method:** GET")
	assert.Contains(t, text, "`/health`")
	// No headers, body, or extract sections
	assert.NotContains(t, text, "## Headers")
	assert.NotContains(t, text, "## Body")
	assert.NotContains(t, text, "## Extract Rules")
	assert.NotContains(t, text, "## Transform")
	assert.NotContains(t, text, "## Template Inputs")
}

func TestHandleInspectTemplate_WithLuaTransform(t *testing.T) {
	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("withLua", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "withLua",
		Protocol: "http",
		Request: adapter.TemplateRequest{
			Method: "POST",
			Path:   "/api/search",
			Body:   `{"q": "{{query}}"}`,
		},
		Response: adapter.TemplateResponse{
			Extract: map[string]adapter.ExtractRule{
				"raw": {Path: "$.data"},
			},
			Transform: "result = raw\nresult.extra = 'added'",
		},
	})))

	srv := newTemplateTestServer(reg)
	result := callTool(t, srv.handleInspectTemplate, map[string]any{"adapter": "withLua"})

	assert.False(t, result.IsError)
	text := resultText(t, result)

	assert.Contains(t, text, "## Transform (Lua)")
	assert.Contains(t, text, "result = raw")
	assert.Contains(t, text, "result.extra = 'added'")
}

func TestHandleInspectTemplate_WithInputClassification(t *testing.T) {
	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("classified", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "classified",
		Protocol: "http",
		Request: adapter.TemplateRequest{
			Method: "POST",
			Path:   "/api/search",
			Body: `{
  "origin": "{{origin}}",
  "destination": "{{destination}}",
  {{?carrier}}"carrier": "{{carrier}}"{{/carrier}},
  "items": [{{#productIds}}"{{.}}"{{/productIds}}]
}`,
		},
	})))

	srv := newTemplateTestServer(reg)
	result := callTool(t, srv.handleInspectTemplate, map[string]any{"adapter": "classified"})

	assert.False(t, result.IsError)
	text := resultText(t, result)

	assert.Contains(t, text, "## Template Inputs")
	assert.Contains(t, text, "**Required:** destination, origin")
	assert.Contains(t, text, "**Conditional:** carrier")
	assert.Contains(t, text, "**Iterable:** productIds")
}

func TestHandleInspectTemplate_UnknownAdapter(t *testing.T) {
	reg := adapter.NewRegistry()
	srv := newTemplateTestServer(reg)
	result := callTool(t, srv.handleInspectTemplate, map[string]any{"adapter": "nonexistent"})

	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "unknown adapter")
}

func TestHandleInspectTemplate_NonTemplateAdapter(t *testing.T) {
	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("custom", &mockAdapter{}))

	srv := newTemplateTestServer(reg)
	result := callTool(t, srv.handleInspectTemplate, map[string]any{"adapter": "custom"})

	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not template-based")
}

func TestHandleInspectTemplate_MissingParam(t *testing.T) {
	reg := adapter.NewRegistry()
	srv := newTemplateTestServer(reg)
	result := callTool(t, srv.handleInspectTemplate, nil)

	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}
