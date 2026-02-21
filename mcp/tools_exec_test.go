package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- execute_plan precondition guards ---

func TestHandleExecutePlan_MissingName(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServer(g)

	result := callTool(t, srv.handleExecutePlan, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

func TestHandleExecutePlan_NoWorkflowsDirConfigured(t *testing.T) {
	g := twoNodeGraph()
	ctx := &ServerContext{
		Graph:       g,
		Registry:    adapter.NewRegistry(),
		Manifest:    &ProjectManifest{Name: "test"},
		Environment: &config.Environment{Name: "test"},
		ArchiveDir:  t.TempDir(),
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleExecutePlan, map[string]any{"name": "test"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "workflows directory not configured")
}

func TestHandleExecutePlan_NoEnvironmentConfigured(t *testing.T) {
	g := twoNodeGraph()
	ctx := &ServerContext{
		Graph:    g,
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
		WorkflowsDir: t.TempDir(),
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleExecutePlan, map[string]any{"name": "test"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "no environment configured")
}

func TestHandleExecutePlan_NoArchiveDirConfigured(t *testing.T) {
	g := twoNodeGraph()
	ctx := &ServerContext{
		Graph:       g,
		Registry:    adapter.NewRegistry(),
		Manifest:    &ProjectManifest{Name: "test"},
		WorkflowsDir:    t.TempDir(),
		Environment: &config.Environment{Name: "test"},
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleExecutePlan, map[string]any{"name": "test"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "archive directory not configured")
}

func TestHandleExecutePlan_PlanNotFound(t *testing.T) {
	plansDir := t.TempDir()
	g := twoNodeGraph()
	ctx := &ServerContext{
		Graph:       g,
		Registry:    adapter.NewRegistry(),
		Manifest:    &ProjectManifest{Name: "test"},
		WorkflowsDir:    plansDir,
		ArchiveDir:  t.TempDir(),
		Environment: &config.Environment{Name: "test", Auth: config.AuthConfig{Type: "none"}},
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleExecutePlan, map[string]any{"name": "nonexistent"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "loading plan")
}

func TestHandleExecutePlan_InvalidMode(t *testing.T) {
	plansDir := t.TempDir()
	g := twoNodeGraph()

	require.NoError(t, os.WriteFile(filepath.Join(plansDir, "test.yaml"), []byte(validPlanYAML()), 0o644))

	ctx := &ServerContext{
		Graph:       g,
		Registry:    adapter.NewRegistry(),
		Manifest:    &ProjectManifest{Name: "test"},
		WorkflowsDir:    plansDir,
		ArchiveDir:  t.TempDir(),
		Environment: &config.Environment{Name: "test", Auth: config.AuthConfig{Type: "none"}},
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleExecutePlan, map[string]any{"name": "test", "mode": "turbo"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "invalid mode")
}

func TestHandleExecutePlan_InvalidPlan(t *testing.T) {
	plansDir := t.TempDir()
	g := twoNodeGraph()

	require.NoError(t, os.WriteFile(filepath.Join(plansDir, "bad.yaml"), []byte(`execution:
  steps:
    - node: nonexistent
`), 0o644))

	ctx := &ServerContext{
		Graph:       g,
		Registry:    adapter.NewRegistry(),
		Manifest:    &ProjectManifest{Name: "test"},
		WorkflowsDir:    plansDir,
		ArchiveDir:  t.TempDir(),
		Environment: &config.Environment{Name: "test", Auth: config.AuthConfig{Type: "none"}},
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleExecutePlan, map[string]any{"name": "bad"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "validation failed")
}

// --- formatExecutionSummary ---

func TestFormatExecutionSummary_PassedRun(t *testing.T) {
	result := &engine.RunResult{
		Outcome: engine.OutcomePassed,
		Steps: []engine.StepResult{
			{
				Node:       "search",
				StatusCode: 200,
				Duration:   150 * time.Millisecond,
				Response:   &adapter.Response{StatusCode: 200},
			},
			{
				Node:       "book",
				StatusCode: 201,
				Duration:   300 * time.Millisecond,
				Response:   &adapter.Response{StatusCode: 201},
			},
		},
	}

	text := formatExecutionSummary(result, "run-test-001", config.ModeStrict)
	assert.Contains(t, text, "passed")
	assert.Contains(t, text, "run-test-001")
	assert.Contains(t, text, "strict")
	assert.Contains(t, text, "search")
	assert.Contains(t, text, "book")
	assert.Contains(t, text, "200")
	assert.Contains(t, text, "201")
	assert.Contains(t, text, "450ms")
	assert.Contains(t, text, "inspect_archive")
}

func TestFormatExecutionSummary_FailedRun(t *testing.T) {
	result := &engine.RunResult{
		Outcome: engine.OutcomeFailed,
		Error:   assert.AnError,
		Steps: []engine.StepResult{
			{
				Node:       "search",
				StatusCode: 500,
				Duration:   100 * time.Millisecond,
				Response:   &adapter.Response{StatusCode: 500},
			},
		},
	}

	text := formatExecutionSummary(result, "run-test-002", config.ModeLean)
	assert.Contains(t, text, "failed")
	assert.Contains(t, text, "500")
	assert.Contains(t, text, "lean")
}

func TestFormatExecutionSummary_WithCleanup(t *testing.T) {
	result := &engine.RunResult{
		Outcome: engine.OutcomePassed,
		Steps: []engine.StepResult{
			{Node: "book", StatusCode: 200, Duration: 200 * time.Millisecond, Response: &adapter.Response{StatusCode: 200}},
		},
		CleanupResults: []engine.StepResult{
			{Node: "cancelBooking", StatusCode: 200, Duration: 100 * time.Millisecond, Response: &adapter.Response{StatusCode: 200}},
		},
	}

	text := formatExecutionSummary(result, "run-test-003", config.ModeStrict)
	assert.Contains(t, text, "Cleanup")
	assert.Contains(t, text, "cancelBooking")
}

func TestFormatExecutionSummary_ErrorRun(t *testing.T) {
	result := &engine.RunResult{
		Outcome: engine.OutcomeError,
		Error:   assert.AnError,
		Steps: []engine.StepResult{
			{Node: "search", Error: assert.AnError, Duration: 50 * time.Millisecond},
		},
	}

	text := formatExecutionSummary(result, "run-test-004", config.ModeStrict)
	assert.Contains(t, text, "error")
	assert.Contains(t, text, "ERROR")
}
