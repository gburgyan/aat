package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/llm"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validPlanYAML returns a minimal valid plan YAML for twoNodeGraph.
func validPlanYAML() string {
	return `intent:
  goal: book
  description: Book a flight
execution:
  steps:
    - node: search
      values:
        origin: "DEN"
        dest: "LAX"
    - node: book
      dependsOn: [search]
      isGoal: true
      values:
        resultId:
          from: search.results
          select:
            strategy: first
`
}

// newTestServerWithWorkflows creates a Server with a WorkflowsDir for testing.
func newTestServerWithWorkflows(g *graph.Graph, workflowsDir string) *Server {
	ctx := &ServerContext{
		Graph:        g,
		Registry:     adapter.NewRegistry(),
		Manifest:     &ProjectManifest{Name: "test"},
		WorkflowsDir: workflowsDir,
	}
	return NewServer(ctx)
}

// --- validate_plan ---

func TestHandleValidatePlan_ValidPlan(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)

	result := callTool(t, srv.handleValidatePlan, map[string]any{"yaml": validPlanYAML()})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "valid")
	assert.Contains(t, text, "2 steps")
	assert.Contains(t, text, "book")
}

func TestHandleValidatePlan_InvalidYAML(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)

	result := callTool(t, srv.handleValidatePlan, map[string]any{"yaml": "not: valid: yaml: ["})
	assert.True(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "YAML parse error")
}

func TestHandleValidatePlan_ValidationErrors(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)

	badPlan := `execution:
  steps:
    - node: nonexistent
`
	result := callTool(t, srv.handleValidatePlan, map[string]any{"yaml": badPlan})
	assert.True(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Validation failed")
	assert.Contains(t, text, "nonexistent")
}

func TestHandleValidatePlan_MissingParam(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)

	result := callTool(t, srv.handleValidatePlan, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

func TestHandleValidatePlan_MissingSteps(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)

	result := callTool(t, srv.handleValidatePlan, map[string]any{"yaml": "intent:\n  goal: book\n"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "YAML parse error")
}

// --- list_saved_plans ---

func TestHandleListSavedPlans_MultiplePlans(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, dir)

	// Write two plans
	require.NoError(t, os.WriteFile(filepath.Join(dir, "alpha.yaml"), []byte(validPlanYAML()), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "beta.yml"), []byte(validPlanYAML()), 0o644))

	result := callTool(t, srv.handleListSavedPlans, nil)
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "2 plan(s)")
	assert.Contains(t, text, "alpha.yaml")
	assert.Contains(t, text, "beta.yml")
	assert.Contains(t, text, "book")
}

func TestHandleListSavedPlans_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, dir)

	result := callTool(t, srv.handleListSavedPlans, nil)
	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "No plans found")
}

func TestHandleListSavedPlans_NoPlansDirConfigured(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, "")

	result := callTool(t, srv.handleListSavedPlans, nil)
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "not configured")
	assert.Contains(t, text, "aat-project.yaml")
}

func TestHandleListSavedPlans_NonexistentDir(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, "/nonexistent/path/plans")

	result := callTool(t, srv.handleListSavedPlans, nil)
	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "empty")
}

func TestHandleListSavedPlans_IgnoresNonYAML(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "plan.yaml"), []byte(validPlanYAML()), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a plan"), 0o644))

	result := callTool(t, srv.handleListSavedPlans, nil)
	text := resultText(t, result)
	assert.Contains(t, text, "1 plan(s)")
	assert.NotContains(t, text, "readme.txt")
}

func TestHandleListSavedPlans_UnparseablePlan(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("not: valid: ["), 0o644))

	result := callTool(t, srv.handleListSavedPlans, nil)
	text := resultText(t, result)
	assert.Contains(t, text, "1 plan(s)")
	assert.Contains(t, text, "bad.yaml")
	assert.Contains(t, text, "parse error")
}

// --- load_plan ---

func TestHandleLoadPlan_Existing(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(validPlanYAML()), 0o644))

	result := callTool(t, srv.handleLoadPlan, map[string]any{"name": "test.yaml"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "```yaml")
	assert.Contains(t, text, "Narrative")
	assert.Contains(t, text, "book")
}

func TestHandleLoadPlan_WithoutExtension(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(validPlanYAML()), 0o644))

	// Should auto-append .yaml
	result := callTool(t, srv.handleLoadPlan, map[string]any{"name": "test"})
	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "book")
}

func TestHandleLoadPlan_Missing(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, dir)

	result := callTool(t, srv.handleLoadPlan, map[string]any{"name": "nonexistent"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not found")
}

func TestHandleLoadPlan_NoPlansDirConfigured(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, "")

	result := callTool(t, srv.handleLoadPlan, map[string]any{"name": "test"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not configured")
}

func TestHandleLoadPlan_MissingParam(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)

	result := callTool(t, srv.handleLoadPlan, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

// --- save_plan ---

func TestHandleSavePlan_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, dir)

	// Save
	result := callTool(t, srv.handleSavePlan, map[string]any{
		"name": "roundtrip",
		"yaml": validPlanYAML(),
	})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "saved")
	assert.Contains(t, text, "roundtrip.yaml")

	// Load back
	result = callTool(t, srv.handleLoadPlan, map[string]any{"name": "roundtrip"})
	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "book")
}

func TestHandleSavePlan_InvalidYAMLRejected(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, dir)

	result := callTool(t, srv.handleSavePlan, map[string]any{
		"name": "bad",
		"yaml": "not: valid: yaml: [",
	})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "YAML parse error")

	// File should not exist
	_, err := os.Stat(filepath.Join(dir, "bad.yaml"))
	assert.True(t, os.IsNotExist(err))
}

func TestHandleSavePlan_ValidationErrorsRejected(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, dir)

	badPlan := `execution:
  steps:
    - node: nonexistent
`
	result := callTool(t, srv.handleSavePlan, map[string]any{
		"name": "bad",
		"yaml": badPlan,
	})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "Validation failed")

	// File should not exist
	_, err := os.Stat(filepath.Join(dir, "bad.yaml"))
	assert.True(t, os.IsNotExist(err))
}

func TestHandleSavePlan_NoPlansDirConfigured(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, "")

	result := callTool(t, srv.handleSavePlan, map[string]any{
		"name": "test",
		"yaml": validPlanYAML(),
	})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not configured")
}

func TestHandleSavePlan_MissingNameParam(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)

	result := callTool(t, srv.handleSavePlan, map[string]any{"yaml": validPlanYAML()})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

func TestHandleSavePlan_MissingYAMLParam(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, dir)

	result := callTool(t, srv.handleSavePlan, map[string]any{"name": "test"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

func TestHandleSavePlan_CreatesDir(t *testing.T) {
	base := t.TempDir()
	plansDir := filepath.Join(base, "nested", "plans")
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, plansDir)

	result := callTool(t, srv.handleSavePlan, map[string]any{
		"name": "test",
		"yaml": validPlanYAML(),
	})
	assert.False(t, result.IsError)

	// File should exist in the nested directory
	_, err := os.Stat(filepath.Join(plansDir, "test.yaml"))
	assert.NoError(t, err)
}

func TestHandleSavePlan_WithYMLExtension(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithWorkflows(g, dir)

	result := callTool(t, srv.handleSavePlan, map[string]any{
		"name": "test.yml",
		"yaml": validPlanYAML(),
	})
	assert.False(t, result.IsError)

	// Should use the .yml extension as given
	_, err := os.Stat(filepath.Join(dir, "test.yml"))
	assert.NoError(t, err)
}

// --- generate_plan ---

// stubLLMClient is a test LLM client that returns canned responses in order.
type stubLLMClient struct {
	responses []*llm.Response
	calls     int
}

func (c *stubLLMClient) Complete(_ context.Context, _ *llm.Request) (*llm.Response, error) {
	if c.calls >= len(c.responses) {
		return nil, fmt.Errorf("stub: no more canned responses (call %d)", c.calls)
	}
	resp := c.responses[c.calls]
	c.calls++
	return resp, nil
}

func TestHandleGeneratePlan_NoEnvironment(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g) // no environment

	result := callTool(t, srv.handleGeneratePlan, map[string]any{"prompt": "book a flight"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "no environment configured")
}

func TestHandleGeneratePlan_NoLLMConfig(t *testing.T) {
	g := twoNodeGraph()
	ctx := &ServerContext{
		Graph:       g,
		Registry:    adapter.NewRegistry(),
		Manifest:    &ProjectManifest{Name: "test"},
		Environment: &config.Environment{},
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleGeneratePlan, map[string]any{"prompt": "book a flight"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "no LLM configuration")
}

func TestHandleGeneratePlan_MissingParam(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)

	result := callTool(t, srv.handleGeneratePlan, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

// --- resolveWorkflowPath ---

func TestResolveWorkflowPath_AppendsYAML(t *testing.T) {
	assert.Equal(t, "/plans/test.yaml", resolveWorkflowPath("/plans", "test"))
}

func TestResolveWorkflowPath_KeepsYAMLExtension(t *testing.T) {
	assert.Equal(t, "/plans/test.yaml", resolveWorkflowPath("/plans", "test.yaml"))
}

func TestResolveWorkflowPath_KeepsYMLExtension(t *testing.T) {
	assert.Equal(t, "/plans/test.yml", resolveWorkflowPath("/plans", "test.yml"))
}

// callToolWithHandler is a variant of callTool that accepts separate handler and error check.
func callToolWithHandler(t *testing.T, handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) (*mcp.CallToolResult, error) {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	return handler(context.Background(), req)
}
