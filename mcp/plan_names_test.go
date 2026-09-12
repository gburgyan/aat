package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlanNamesStayInsidePlansDir checks that plan names from tool calls cannot
// read or write files outside the plans directory.
func TestPlanNamesStayInsidePlansDir(t *testing.T) {
	parent := t.TempDir()
	plansDir := filepath.Join(parent, "plans")
	require.NoError(t, os.MkdirAll(plansDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "outside.yaml"), []byte(validPlanYAML()), 0o644))
	srv := newTestServerWithPlans(twoNodeGraph(), plansDir)

	result := callTool(t, srv.handleSavePlan, map[string]any{"name": "../escaped", "yaml": validPlanYAML()})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "invalid plan name")
	assert.NoFileExists(t, filepath.Join(parent, "escaped.yaml"))

	for _, name := range []string{"../outside", filepath.Join(parent, "outside.yaml")} {
		result = callTool(t, srv.handleLoadPlan, map[string]any{"name": name})
		assert.True(t, result.IsError, name)
		assert.Contains(t, resultText(t, result), "invalid plan name", name)

		result = callTool(t, srv.handleExecutePlan, map[string]any{"name": name})
		assert.True(t, result.IsError, name)
		assert.Contains(t, resultText(t, result), "invalid plan name", name)
	}

	result = callTool(t, srv.handleGeneratePlan, map[string]any{"prompt": "book a flight", "save_as": "../escaped"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "invalid plan name")

	// A plan in a subdirectory is still found.
	require.NoError(t, os.MkdirAll(filepath.Join(plansDir, "negative"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(plansDir, "negative", "state.yaml"), []byte(validPlanYAML()), 0o644))
	result = callTool(t, srv.handleLoadPlan, map[string]any{"name": "negative/state"})
	assert.False(t, result.IsError, resultText(t, result))
}

// TestInspectArchive_RejectsRunIDOutsideArchiveDir checks that a run_id cannot
// read an archive outside the archive directory.
func TestInspectArchive_RejectsRunIDOutsideArchiveDir(t *testing.T) {
	parent := t.TempDir()
	archiveDir := filepath.Join(parent, "runs")
	require.NoError(t, os.MkdirAll(archiveDir, 0o755))
	writeTestArchive(t, parent, "leaked", testArchive("passed", testStep("search", 200, 100)))
	srv := newTestServerWithArchives(twoNodeGraph(), archiveDir)

	result := callTool(t, srv.handleInspectArchive, map[string]any{"run_id": "../leaked"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not found")
}
