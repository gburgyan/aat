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
	assert.Contains(t, resultText(t, result), "unknown adapter or node")
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

// --- node name resolution ---

func TestHandleInspectTemplate_NodeNameResolvesToAdapter(t *testing.T) {
	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("searchTmpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "searchTmpl",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "POST", Path: "/api/search"},
	})))

	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"searchFlights": {
				Name:    "searchFlights",
				Adapter: "searchTmpl",
			},
		},
	}
	ctx := &ServerContext{
		Graph:    g,
		Registry: reg,
		Manifest: &ProjectManifest{Name: "test"},
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleInspectTemplate, map[string]any{"adapter": "searchFlights"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	// Should show the note about adapter vs node name.
	assert.Contains(t, text, "adapter \"searchTmpl\"")
	assert.Contains(t, text, "node \"searchFlights\"")
	// Should contain the template content.
	assert.Contains(t, text, "**Method:** POST")
	assert.Contains(t, text, "/api/search")
}

func TestHandleInspectTemplate_NodeNameSameAsAdapter(t *testing.T) {
	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("search", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "search",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "GET", Path: "/search"},
	})))

	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name:    "search",
				Adapter: "search",
			},
		},
	}
	ctx := &ServerContext{
		Graph:    g,
		Registry: reg,
		Manifest: &ProjectManifest{Name: "test"},
	}
	srv := NewServer(ctx)

	// When adapter name == node name, direct adapter lookup wins (no note).
	result := callTool(t, srv.handleInspectTemplate, map[string]any{"adapter": "search"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.NotContains(t, text, "Showing template for adapter")
	assert.Contains(t, text, "**Method:** GET")
}

func TestHandleInspectTemplate_DirectAdapterStillWorks(t *testing.T) {
	reg := adapter.NewRegistry()
	require.NoError(t, reg.Register("myAdapter", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "myAdapter",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "PUT", Path: "/update"},
	})))

	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"someNode": {
				Name:    "someNode",
				Adapter: "myAdapter",
			},
		},
	}
	ctx := &ServerContext{
		Graph:    g,
		Registry: reg,
		Manifest: &ProjectManifest{Name: "test"},
	}
	srv := NewServer(ctx)

	// Direct adapter name lookup should still work without any note.
	result := callTool(t, srv.handleInspectTemplate, map[string]any{"adapter": "myAdapter"})

	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.NotContains(t, text, "Showing template for adapter")
	assert.Contains(t, text, "**Method:** PUT")
}

// --- detectResponseEnvelope ---

func TestDetectResponseEnvelope_CommonRoot(t *testing.T) {
	extract := map[string]adapter.ExtractRule{
		"offerings": {Path: "CatalogProductOfferingsResponse.CatalogProductOfferings"},
		"nextStep":  {Path: "CatalogProductOfferingsResponse.NextSteps"},
	}
	assert.Equal(t, "CatalogProductOfferingsResponse", detectResponseEnvelope(extract))
}

func TestDetectResponseEnvelope_MixedRoots(t *testing.T) {
	extract := map[string]adapter.ExtractRule{
		"offerings": {Path: "CatalogProductOfferingsResponse.data"},
		"contacts":  {Path: "PrimaryContactResponse.data"},
	}
	assert.Equal(t, "", detectResponseEnvelope(extract))
}

func TestDetectResponseEnvelope_SingleRule(t *testing.T) {
	extract := map[string]adapter.ExtractRule{
		"result": {Path: "ReservationResponse.Reservation"},
	}
	assert.Equal(t, "ReservationResponse", detectResponseEnvelope(extract))
}

func TestDetectResponseEnvelope_WithDollarPrefix(t *testing.T) {
	extract := map[string]adapter.ExtractRule{
		"a": {Path: "$.Envelope.data"},
		"b": {Path: "$.Envelope.meta"},
	}
	assert.Equal(t, "Envelope", detectResponseEnvelope(extract))
}

func TestDetectResponseEnvelope_NoDot(t *testing.T) {
	extract := map[string]adapter.ExtractRule{
		"a": {Path: "data"},
		"b": {Path: "data"},
	}
	// Single segment paths — still common root
	assert.Equal(t, "data", detectResponseEnvelope(extract))
}

func TestDetectResponseEnvelope_EmptyPaths(t *testing.T) {
	extract := map[string]adapter.ExtractRule{
		"a": {Path: ""},
		"b": {Path: ""},
	}
	assert.Equal(t, "", detectResponseEnvelope(extract))
}

func TestDetectResponseEnvelope_EmptyExtract(t *testing.T) {
	assert.Equal(t, "", detectResponseEnvelope(map[string]adapter.ExtractRule{}))
}

func TestFormatTemplate_ShowsEnvelopeNote(t *testing.T) {
	tmpl := &adapter.Template{
		Adapter:  "search",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "POST", Path: "/api/search"},
		Response: adapter.TemplateResponse{
			Extract: map[string]adapter.ExtractRule{
				"results": {Path: "SearchResponse.results"},
				"count":   {Path: "SearchResponse.count"},
			},
		},
	}
	text := formatTemplate(tmpl)
	assert.Contains(t, text, "**Response envelope:**")
	assert.Contains(t, text, "`SearchResponse`")
}

func TestFormatTemplate_NoEnvelopeNote_MixedRoots(t *testing.T) {
	tmpl := &adapter.Template{
		Adapter:  "search",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "POST", Path: "/api/search"},
		Response: adapter.TemplateResponse{
			Extract: map[string]adapter.ExtractRule{
				"a": {Path: "ResponseA.data"},
				"b": {Path: "ResponseB.data"},
			},
		},
	}
	text := formatTemplate(tmpl)
	assert.NotContains(t, text, "Response envelope")
}

// --- needsPathLegend ---

func TestNeedsPathLegend_WithHash(t *testing.T) {
	extract := map[string]adapter.ExtractRule{
		"items": {Path: "data.items.#.id"},
	}
	assert.True(t, needsPathLegend(extract))
}

func TestNeedsPathLegend_WithArrayIndex(t *testing.T) {
	extract := map[string]adapter.ExtractRule{
		"first": {Path: "data.items.0.id"},
	}
	assert.True(t, needsPathLegend(extract))
}

func TestNeedsPathLegend_WithFlatten(t *testing.T) {
	extract := map[string]adapter.ExtractRule{
		"flat": {Path: "data.nested|@flatten"},
	}
	assert.True(t, needsPathLegend(extract))
}

func TestNeedsPathLegend_InFields(t *testing.T) {
	extract := map[string]adapter.ExtractRule{
		"items": {Path: "data", Fields: map[string]string{
			"id": "items.#.id",
		}},
	}
	assert.True(t, needsPathLegend(extract))
}

func TestNeedsPathLegend_SimplePaths(t *testing.T) {
	extract := map[string]adapter.ExtractRule{
		"results": {Path: "data.results"},
		"count":   {Path: "data.count"},
	}
	assert.False(t, needsPathLegend(extract))
}

func TestFormatTemplate_ShowsPathLegend(t *testing.T) {
	tmpl := &adapter.Template{
		Adapter:  "search",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "POST", Path: "/api/search"},
		Response: adapter.TemplateResponse{
			Extract: map[string]adapter.ExtractRule{
				"items": {Path: "data.items.#.id|@flatten"},
			},
		},
	}
	text := formatTemplate(tmpl)
	assert.Contains(t, text, "**Path notation (gjson):**")
	assert.Contains(t, text, "`#` = iterate all array elements")
	assert.Contains(t, text, "`|@flatten` = flatten nested arrays")
}

func TestFormatTemplate_NoPathLegend_SimplePaths(t *testing.T) {
	tmpl := &adapter.Template{
		Adapter:  "search",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "POST", Path: "/api/search"},
		Response: adapter.TemplateResponse{
			Extract: map[string]adapter.ExtractRule{
				"results": {Path: "data.results"},
			},
		},
	}
	text := formatTemplate(tmpl)
	assert.NotContains(t, text, "Path notation")
}

// --- extractTransformSummary ---

func TestExtractTransformSummary_MultiLine(t *testing.T) {
	script := `-- Resolve GDS ReferenceList cross-references.
-- In GDS mode, CatalogProductOfferings contain only reference strings.
local data = json.decode(raw)`
	result := extractTransformSummary(script)
	assert.Contains(t, result, "Resolve GDS ReferenceList cross-references.")
	assert.Contains(t, result, "reference strings.")
}

func TestExtractTransformSummary_NoComments(t *testing.T) {
	script := `local data = json.decode(raw)
result = data`
	assert.Equal(t, "", extractTransformSummary(script))
}

func TestExtractTransformSummary_SingleLine(t *testing.T) {
	script := `-- Strip wrapper
local data = json.decode(raw)`
	assert.Equal(t, "Strip wrapper", extractTransformSummary(script))
}

func TestExtractTransformSummary_EmptyScript(t *testing.T) {
	assert.Equal(t, "", extractTransformSummary(""))
}

func TestExtractTransformSummary_BlankLineGap(t *testing.T) {
	script := `-- First line

-- Still part of header
local data = json.decode(raw)`
	result := extractTransformSummary(script)
	assert.Contains(t, result, "First line")
	assert.Contains(t, result, "Still part of header")
}

func TestExtractTransformSummary_EmptyCommentLine(t *testing.T) {
	script := `--
-- Real comment
local x = 1`
	result := extractTransformSummary(script)
	assert.Equal(t, "Real comment", result)
}

func TestFormatTemplate_ShowsTransformSummary(t *testing.T) {
	tmpl := &adapter.Template{
		Adapter:  "search",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "POST", Path: "/api/search"},
		Response: adapter.TemplateResponse{
			Transform: "-- Resolve cross-references.\nlocal data = json.decode(raw)\nresult = data",
		},
	}
	text := formatTemplate(tmpl)
	assert.Contains(t, text, "> Resolve cross-references.")
	assert.Contains(t, text, "```lua")
}

func TestFormatTemplate_NoTransformSummary_NoComments(t *testing.T) {
	tmpl := &adapter.Template{
		Adapter:  "search",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "POST", Path: "/api/search"},
		Response: adapter.TemplateResponse{
			Transform: "result = raw\nresult.extra = 'added'",
		},
	}
	text := formatTemplate(tmpl)
	assert.Contains(t, text, "## Transform (Lua)")
	assert.NotContains(t, text, "> ")
}
