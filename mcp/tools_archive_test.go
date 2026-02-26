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

// --- list_recent_failures ---

func TestHandleListRecentFailures_OnlyFailures(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	// Write one passed and one failed archive.
	passed := testArchive("passed", testStep("search", 200, 100))
	passed.Metadata.RunID = "run-20260210-140000-pass0001"
	writeTestArchive(t, dir, "run-20260210-140000-pass0001", passed)

	failed := testArchive("failed", testStep("search", 400, 200))
	failed.Metadata.RunID = "run-20260210-150000-fail0001"
	writeTestArchive(t, dir, "run-20260210-150000-fail0001", failed)

	result := callTool(t, srv.handleListRecentFailures, nil)
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "1 recent failure")
	assert.Contains(t, text, "fail0001")
	assert.NotContains(t, text, "pass0001")
}

// --- get_sample_response ---

func TestHandleGetSampleResponse_WithRunID(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	a := testArchive("passed", testStep("search", 200, 100))
	a.Metadata.RunID = "run-20260210-140000-aaaa0001"
	writeTestArchive(t, dir, "run-20260210-140000-aaaa0001", a)

	result := callTool(t, srv.handleGetSampleResponse, map[string]any{
		"node":   "search",
		"run_id": "run-20260210-140000-aaaa0001",
	})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Sample Response: search")
	assert.Contains(t, text, "run-20260210-140000-aaaa0001")
	assert.Contains(t, text, "**Status:** 200")
	assert.Contains(t, text, "```json")
}

func TestHandleGetSampleResponse_AutoDiscover(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	// Write two archives — should find the newest one.
	a1 := testArchive("passed", testStep("search", 200, 100))
	a1.Metadata.RunID = "run-20260210-140000-aaaa0001"
	writeTestArchive(t, dir, "run-20260210-140000-aaaa0001", a1)

	a2 := testArchive("passed", testStep("search", 200, 150))
	a2.Metadata.RunID = "run-20260210-150000-aaaa0002"
	writeTestArchive(t, dir, "run-20260210-150000-aaaa0002", a2)

	result := callTool(t, srv.handleGetSampleResponse, map[string]any{"node": "search"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "run-20260210-150000-aaaa0002") // newest
	assert.Contains(t, text, "**Status:** 200")
}

func TestHandleGetSampleResponse_NodeNotInArchive_Fallback(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	// Archive has "search" but not "book".
	a := testArchive("passed", testStep("search", 200, 100))
	writeTestArchive(t, dir, "run-20260210-140000-aaaa0001", a)

	// Auto-discover mode: returns fallback, not error.
	result := callTool(t, srv.handleGetSampleResponse, map[string]any{"node": "book"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "No Sample Response Yet")
	assert.Contains(t, text, "inspect_request_template")
}

func TestHandleGetSampleResponse_NoArchives_Fallback(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	// Auto-discover mode: returns fallback, not error.
	result := callTool(t, srv.handleGetSampleResponse, map[string]any{"node": "search"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "No Sample Response Yet")
	assert.Contains(t, text, "inspect_request_template")
}

func TestHandleGetSampleResponse_FallbackShowsOutputs(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	result := callTool(t, srv.handleGetSampleResponse, map[string]any{"node": "search"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Expected Output Shape")
	assert.Contains(t, text, "results")
}

func TestHandleGetSampleResponse_FallbackShowsExtractRules(t *testing.T) {
	dir := t.TempDir()
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name:    "search",
				Adapter: "searchAdapter",
				Outputs: []graph.Output{{Name: "results", Type: "result[]"}},
			},
		},
	}
	g.BuildSatisfierIndex()

	// Register a template with extract rules under the adapter name.
	reg := adapter.NewRegistry()
	_ = reg.Register("searchAdapter", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "searchAdapter",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "POST", Path: "/search"},
		Response: adapter.TemplateResponse{
			Extract: map[string]adapter.ExtractRule{
				"results": {Path: "SearchResponse.data"},
				"count":   {Path: "SearchResponse.count"},
			},
		},
	}))

	ctx := &ServerContext{
		Graph:      g,
		Registry:   reg,
		Manifest:   &ProjectManifest{Name: "test"},
		ArchiveDir: dir,
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleGetSampleResponse, map[string]any{"node": "search"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Extract Rule Paths")
	assert.Contains(t, text, "SearchResponse.data")
	assert.Contains(t, text, "**Response envelope:**")
	assert.Contains(t, text, "`SearchResponse`")
}

func TestHandleGetSampleResponse_FallbackMinimalNode(t *testing.T) {
	dir := t.TempDir()
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"minimal": {Name: "minimal"},
		},
	}
	g.BuildSatisfierIndex()
	srv := newTestServerWithArchives(g, dir)

	result := callTool(t, srv.handleGetSampleResponse, map[string]any{"node": "minimal"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "No Sample Response Yet")
	assert.NotContains(t, text, "Expected Output Shape")
}

func TestHandleGetSampleResponse_NoArchiveDir(t *testing.T) {
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, "")

	result := callTool(t, srv.handleGetSampleResponse, map[string]any{"node": "search"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not configured")
}

func TestHandleGetSampleResponse_UnknownNode(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	result := callTool(t, srv.handleGetSampleResponse, map[string]any{"node": "nonexistent"})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "unknown node")
}

func TestHandleGetSampleResponse_ExtractedOutputs(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	a := testArchive("passed", testStep("search", 200, 100))
	writeTestArchive(t, dir, "run-20260210-140000-aaaa0001", a)

	result := callTool(t, srv.handleGetSampleResponse, map[string]any{"node": "search"})
	assert.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Extracted Outputs")
	assert.Contains(t, text, "output1")
}

func TestHandleGetSampleResponse_RunIDNotFound(t *testing.T) {
	dir := t.TempDir()
	g := twoNodeGraph()
	srv := newTestServerWithArchives(g, dir)

	result := callTool(t, srv.handleGetSampleResponse, map[string]any{
		"node":   "search",
		"run_id": "run-nonexistent",
	})
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not found")
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
