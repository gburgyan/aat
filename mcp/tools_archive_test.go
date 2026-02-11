package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTestArchive writes an archive JSON file to the given directory.
func writeTestArchive(t *testing.T, archiveDir, runID string, a *archive.Archive) {
	t.Helper()
	path := filepath.Join(archiveDir, runID, "archive.json")
	require.NoError(t, archive.Write(a, path))
}

// newTestServerWithArchives creates a Server with an ArchiveDir for testing.
func newTestServerWithArchives(g *graph.Graph, archiveDir string) *Server {
	ctx := &ServerContext{
		Graph:      g,
		Registry:   adapter.NewRegistry(),
		Manifest:   &ProjectManifest{Name: "test"},
		ArchiveDir: archiveDir,
	}
	return NewServer(ctx)
}

// --- list_archives ---

func TestHandleListArchives_MultipleRuns(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	a1 := testArchive("passed", testStep("search", 200, 100))
	a1.Metadata.RunID = "run-20260210-140000-aaaa0001"
	writeTestArchive(t, dir, "run-20260210-140000-aaaa0001", a1)

	a2 := testArchive("failed", testStep("search", 400, 200))
	a2.Metadata.RunID = "run-20260210-150000-aaaa0002"
	writeTestArchive(t, dir, "run-20260210-150000-aaaa0002", a2)

	result := callTool(t, srv.handleListArchives, nil)
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "2 archive(s)")
	assert.Contains(t, text, "run-20260210-150000-aaaa0002")
	assert.Contains(t, text, "run-20260210-140000-aaaa0001")
	// Newest first
	idx1 := indexOf(text, "run-20260210-150000-aaaa0002")
	idx2 := indexOf(text, "run-20260210-140000-aaaa0001")
	assert.Less(t, idx1, idx2, "newest archive should appear first")
}

func TestHandleListArchives_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	result := callTool(t, srv.handleListArchives, nil)
	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "No archives found")
}

func TestHandleListArchives_NotConfigured(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, "")

	result := callTool(t, srv.handleListArchives, nil)
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "not configured")
	assert.Contains(t, text, "aat-project.yaml")
}

func TestHandleListArchives_NonexistentDir(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, "/nonexistent/path/archives")

	result := callTool(t, srv.handleListArchives, nil)
	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "empty")
}

func TestHandleListArchives_WithLimit(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	for i := 0; i < 5; i++ {
		runID := "run-20260210-14000" + string(rune('0'+i)) + "-aaaa000" + string(rune('0'+i))
		a := testArchive("passed", testStep("search", 200, 100))
		a.Metadata.RunID = runID
		writeTestArchive(t, dir, runID, a)
	}

	result := callTool(t, srv.handleListArchives, map[string]any{"limit": float64(2)})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "2 archive(s)")
}

func TestHandleListArchives_IgnoresNonRunDirs(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	// Create a non-run directory
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "not-a-run"), 0o755))

	// Create a valid run
	a := testArchive("passed", testStep("search", 200, 100))
	writeTestArchive(t, dir, "run-20260210-140000-aaaa0001", a)

	result := callTool(t, srv.handleListArchives, nil)
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "1 archive(s)")
	assert.NotContains(t, text, "not-a-run")
}

func TestHandleListArchives_CorruptArchive(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	// Write corrupt JSON
	runDir := filepath.Join(dir, "run-20260210-140000-corrupt1")
	require.NoError(t, os.MkdirAll(runDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "archive.json"), []byte("not json"), 0o644))

	result := callTool(t, srv.handleListArchives, nil)
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "run-20260210-140000-corrupt1")
	assert.Contains(t, text, "parse error")
}

// --- inspect_archive ---

func TestHandleInspectArchive_Valid(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	a := testArchive("passed", testStep("search", 200, 100), testStep("book", 201, 250))
	writeTestArchive(t, dir, "run-20260210-143000-abcd1234", a)

	result := callTool(t, srv.handleInspectArchive, map[string]any{"run_id": "run-20260210-143000-abcd1234"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "# Run: run-20260210-143000-abcd1234")
	assert.Contains(t, text, "search")
	assert.Contains(t, text, "book")
	assert.Contains(t, text, "POST")
}

func TestHandleInspectArchive_NotFound(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	result := callTool(t, srv.handleInspectArchive, map[string]any{"run_id": "run-nonexistent"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not found")
}

func TestHandleInspectArchive_MissingParam(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, t.TempDir())

	result := callTool(t, srv.handleInspectArchive, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

func TestHandleInspectArchive_NotConfigured(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, "")

	result := callTool(t, srv.handleInspectArchive, map[string]any{"run_id": "run-test"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not configured")
}

// --- analyze_failure ---

func TestHandleAnalyzeFailure_FailedRun(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	step := testStep("book", 400, 200)
	step.ErrorClass = &archive.ErrorClassRecord{
		Category: "client",
		Detail:   "bad request",
		Action:   "review inputs",
	}
	a := testArchive("failed", testStep("search", 200, 100), step)
	writeTestArchive(t, dir, "run-20260210-143000-fail0001", a)

	result := callTool(t, srv.handleAnalyzeFailure, map[string]any{"run_id": "run-20260210-143000-fail0001"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Failure Analysis")
	assert.Contains(t, text, "book")
	assert.Contains(t, text, "client")
	assert.Contains(t, text, "Suggested Next Steps")
}

func TestHandleAnalyzeFailure_PassedRun(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	a := testArchive("passed", testStep("search", 200, 100))
	writeTestArchive(t, dir, "run-20260210-143000-pass0001", a)

	result := callTool(t, srv.handleAnalyzeFailure, map[string]any{"run_id": "run-20260210-143000-pass0001"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "passed")
	assert.Contains(t, text, "inspect_archive")
}

func TestHandleAnalyzeFailure_NotFound(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	result := callTool(t, srv.handleAnalyzeFailure, map[string]any{"run_id": "run-nonexistent"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not found")
}

func TestHandleAnalyzeFailure_MissingParam(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, t.TempDir())

	result := callTool(t, srv.handleAnalyzeFailure, nil)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

func TestHandleAnalyzeFailure_NotConfigured(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, "")

	result := callTool(t, srv.handleAnalyzeFailure, map[string]any{"run_id": "run-test"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not configured")
}

// --- diff_archives ---

func TestHandleDiffArchives_BasicDiff(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	a1 := testArchive("passed", testStep("search", 200, 100), testStep("book", 200, 200))
	a1.Metadata.RunID = "run-20260210-140000-diff0001"
	writeTestArchive(t, dir, "run-20260210-140000-diff0001", a1)

	a2 := testArchive("failed", testStep("search", 200, 150), testStep("book", 500, 300))
	a2.Metadata.RunID = "run-20260210-150000-diff0002"
	writeTestArchive(t, dir, "run-20260210-150000-diff0002", a2)

	result := callTool(t, srv.handleDiffArchives, map[string]any{
		"run_id_1": "run-20260210-140000-diff0001",
		"run_id_2": "run-20260210-150000-diff0002",
	})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Archive Diff")
	assert.Contains(t, text, "passed")
	assert.Contains(t, text, "failed")
	assert.Contains(t, text, "search")
	assert.Contains(t, text, "book")
	assert.Contains(t, text, "Status changed")
}

func TestHandleDiffArchives_SameRunID(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	result := callTool(t, srv.handleDiffArchives, map[string]any{
		"run_id_1": "run-same",
		"run_id_2": "run-same",
	})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "cannot diff an archive with itself")
}

func TestHandleDiffArchives_NotFound(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	a := testArchive("passed", testStep("search", 200, 100))
	writeTestArchive(t, dir, "run-20260210-140000-exist001", a)

	result := callTool(t, srv.handleDiffArchives, map[string]any{
		"run_id_1": "run-20260210-140000-exist001",
		"run_id_2": "run-nonexistent",
	})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not found")
}

func TestHandleDiffArchives_MissingParam(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, t.TempDir())

	result := callTool(t, srv.handleDiffArchives, map[string]any{"run_id_1": "run-1"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "missing required parameter")
}

func TestHandleDiffArchives_NotConfigured(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, "")

	result := callTool(t, srv.handleDiffArchives, map[string]any{
		"run_id_1": "run-1",
		"run_id_2": "run-2",
	})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not configured")
}

// --- loadArchive ---

func TestLoadArchive_Valid(t *testing.T) {
	dir := t.TempDir()
	a := testArchive("passed")
	writeTestArchive(t, dir, "run-test", a)

	loaded, err := loadArchive(dir, "run-test")
	require.NoError(t, err)
	assert.Equal(t, "passed", loaded.Result.Outcome)
}

func TestLoadArchive_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := loadArchive(dir, "run-nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestLoadArchive_Corrupt(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "run-corrupt")
	require.NoError(t, os.MkdirAll(runDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "archive.json"), []byte("{invalid"), 0o644))

	_, err := loadArchive(dir, "run-corrupt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading archive")
}

// --- Helper: ensure json import is used ---

func TestWriteTestArchive_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := testArchive("passed", testStep("search", 200, 100))
	original.Steps[0].Request.Body = json.RawMessage(`{"origin":"DEN"}`)
	writeTestArchive(t, dir, "run-roundtrip", original)

	loaded, err := loadArchive(dir, "run-roundtrip")
	require.NoError(t, err)
	assert.Equal(t, "passed", loaded.Result.Outcome)
	assert.Len(t, loaded.Steps, 1)
	assert.Equal(t, "search", loaded.Steps[0].Node)
}
