package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

func fuzzToolServer(t *testing.T) *Server {
	t.Helper()
	min := 1.0
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"addItem": {Name: "addItem", Adapter: "shop.addItem", Inputs: []graph.Input{
			{Name: "quantity", Type: "integer", Default: &graph.InputDefault{Value: 1}, Constraints: &graph.Constraint{Min: &min}},
		}},
	}}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("shop.addItem", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "shop.addItem", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "POST", Path: "/items", Body: `{"quantity": {{quantity}}, "channel": "web"}`},
	})))
	plansDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(plansDir, "add.yaml"),
		[]byte("execution:\n  steps:\n    - id: add\n      node: addItem\n"), 0o644))
	return NewServer(&ServerContext{Graph: g, Registry: registry, Manifest: &ProjectManifest{Name: "test"}, PlanDirs: []string{plansDir}})
}

func TestHandleGenerateFuzzCases(t *testing.T) {
	srv := fuzzToolServer(t)

	result := callTool(t, srv.handleGenerateFuzzCases, map[string]any{"plan": "add", "step": "add", "mode": "negative"})
	require.False(t, result.IsError, resultText(t, result))
	text := resultText(t, result)
	assert.Contains(t, text, "`quantity.below-min` | negative | `quantity=0`")
	assert.Contains(t, text, "`quantity.missing` | negative | `remove body quantity`")
	assert.NotContains(t, text, "| positive |", "mode limits the cases")

	// The YAML block reads back as a fuzz: block.
	start := len("```yaml\n") + strings.Index(text, "```yaml\n")
	end := start + strings.Index(text[start:], "```")
	var block struct {
		Fuzz plan.FuzzSettings `yaml:"fuzz"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(text[start:end]), &block))
	require.NotEmpty(t, block.Fuzz.Pinned)
	for _, p := range block.Fuzz.Pinned {
		assert.Equal(t, plan.FuzzNegative, p.Mode)
	}

	result = callTool(t, srv.handleGenerateFuzzCases, map[string]any{"plan": "add", "step": "addItem", "count": 2, "seed": 7})
	require.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "## 2 fuzz cases for step add (addItem)", "a node name finds its step")

	result = callTool(t, srv.handleGenerateFuzzCases, map[string]any{"plan": "add", "step": "checkout"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), `no step "checkout"`)

	result = callTool(t, srv.handleGenerateFuzzCases, map[string]any{"plan": "add", "step": "add", "mode": "hostile"})
	assert.True(t, result.IsError)
}
