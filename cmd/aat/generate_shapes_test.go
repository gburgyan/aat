package main

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/graph/oas"
)

// TestGenerateCommand_RequestShapesRender checks that a scaffold with a list
// query parameter, cookies, a form body, an allOf body, and a HEAD operation
// loads, passes OAS validation, and sends what the spec describes.
func TestGenerateCommand_RequestShapesRender(t *testing.T) {
	dir := t.TempDir()
	graphOut := filepath.Join(dir, "graph.yaml")
	templatesOut := filepath.Join(dir, "templates")
	require.NoError(t, generateCommand(&generateArgs{
		OASPath:         "testdata/oas/request_shapes.yaml",
		OutputGraph:     graphOut,
		OutputTemplates: templatesOut,
	}))

	g, err := graph.ParseFile(graphOut)
	require.NoError(t, err)
	registry := adapter.NewRegistry()
	_, err = adapter.LoadTemplates(templatesOut, registry)
	require.NoError(t, err)
	require.NoError(t, engine.ValidateTemplateInputs(g, registry))

	specPath := g.OAS
	if !filepath.IsAbs(specPath) {
		specPath = filepath.Join(dir, specPath)
	}
	validator := oas.NewValidator()
	require.NoError(t, validator.LoadSpec(g.OAS, specPath))
	assert.Empty(t, validator.Validate(g).Issues, "the generated graph matches the spec")

	build := func(t *testing.T, name string, inputs map[string]any) *adapter.Request {
		t.Helper()
		tmpl, err := adapter.ParseTemplateFile(filepath.Join(templatesOut, name+".yaml"))
		require.NoError(t, err)
		req, err := adapter.NewTemplateAdapter(*tmpl).BuildRequest(inputs, &adapter.EnvironmentConfig{})
		require.NoError(t, err)
		return req
	}

	t.Run("list query parameter and cookies", func(t *testing.T) {
		req := build(t, "searchProducts", map[string]any{"q": "tent", "tags": []any{"camp", "4 season"}, "sessionId": "s1"})
		u, err := url.Parse(req.Path)
		require.NoError(t, err)
		assert.Equal(t, url.Values{"q": {"tent"}, "tags": {"camp", "4 season"}}, u.Query())
		assert.Equal(t, "sessionId=s1", req.Headers["Cookie"])

		req = build(t, "searchProducts", map[string]any{"q": "tent", "sessionId": "s1", "theme": "dark"})
		assert.Equal(t, "sessionId=s1; theme=dark", req.Headers["Cookie"])
	})

	t.Run("form body", func(t *testing.T) {
		req := build(t, "requestRefund", map[string]any{"orderId": "o 1", "reasons": []any{"late", "a&b"}})
		assert.Equal(t, "application/x-www-form-urlencoded", req.Headers["Content-Type"])
		form, err := url.ParseQuery(string(req.Body))
		require.NoError(t, err, "body: %s", req.Body)
		assert.Equal(t, url.Values{"orderId": {"o 1"}, "reasons": {"late", "a&b"}}, form)
	})

	t.Run("allOf body", func(t *testing.T) {
		req := build(t, "createCoupon", map[string]any{"code": "SAVE10", "percentOff": 10})
		assert.Equal(t, "application/json", req.Headers["Content-Type"])
		var body map[string]any
		require.NoError(t, json.Unmarshal(req.Body, &body), "body: %s", req.Body)
		assert.Equal(t, map[string]any{"code": "SAVE10", "percentOff": float64(10)}, body)
	})

	t.Run("HEAD operation", func(t *testing.T) {
		req := build(t, "headHealth", map[string]any{})
		assert.Equal(t, "HEAD", req.Method)
		assert.Empty(t, req.Body)
	})
}
