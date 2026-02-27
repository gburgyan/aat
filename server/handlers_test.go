package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gburgyan/aat/archive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test helpers ---

func newTestServer(archiveDir string) *Server {
	return NewServer(ServerOptions{ArchiveDir: archiveDir})
}

func serveRequest(s *Server, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// --- handleListRuns ---

func TestHandleListRuns_Success(t *testing.T) {
	dir := t.TempDir()
	a1 := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("n1", 200, 100))
	a1.Metadata.Timestamp = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	a2 := makeArchive("run-20260102-100000-aaaa0002", "passed", makeStep("n2", 200, 200))
	a2.Metadata.Timestamp = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	a3 := makeArchive("run-20260103-100000-aaaa0003", "failed", makeStep("n3", 500, 300))
	a3.Metadata.Timestamp = time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC)
	writeArchive(t, dir, a1)
	writeArchive(t, dir, a2)
	writeArchive(t, dir, a3)

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))

	var runs []RunListEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&runs))
	assert.Len(t, runs, 3)
	assert.Equal(t, "run-20260103-100000-aaaa0003", runs[0].RunID)
}

func TestHandleListRuns_Empty(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs")

	assert.Equal(t, http.StatusOK, rec.Code)

	var runs []RunListEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&runs))
	assert.Len(t, runs, 0)
	assert.NotNil(t, runs) // Should be [] not null
}

func TestHandleListRuns_WithLimit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		a := makeArchive(
			"run-2026010"+string(rune('1'+i))+"-100000-aaaa000"+string(rune('1'+i)),
			"passed", makeStep("node", 200, 100),
		)
		writeArchive(t, dir, a)
	}

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs?limit=2")

	assert.Equal(t, http.StatusOK, rec.Code)

	var runs []RunListEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&runs))
	assert.Len(t, runs, 2)
}

func TestHandleListRuns_LimitZero(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		a := makeArchive(
			"run-2026010"+string(rune('1'+i))+"-100000-aaaa000"+string(rune('1'+i)),
			"passed", makeStep("node", 200, 100),
		)
		writeArchive(t, dir, a)
	}

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs?limit=0")

	assert.Equal(t, http.StatusOK, rec.Code)

	var runs []RunListEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&runs))
	assert.Len(t, runs, 3)
}

func TestHandleListRuns_InvalidLimit(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs?limit=abc")

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var errResp apiError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.Equal(t, "invalid_parameter", errResp.Code)
}

func TestHandleListRuns_NegativeLimit(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs?limit=-1")

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var errResp apiError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.Equal(t, "invalid_parameter", errResp.Code)
}

// --- handleLatestRun ---

func TestHandleLatestRun_RedirectToRun(t *testing.T) {
	dir := t.TempDir()
	a1 := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("n", 200, 100))
	a1.Metadata.Timestamp = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	a3 := makeArchive("run-20260103-100000-aaaa0003", "passed", makeStep("n", 200, 100))
	a3.Metadata.Timestamp = time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC)
	writeArchive(t, dir, a1)
	writeArchive(t, dir, a3)

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/latest")

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/api/runs/run-20260103-100000-aaaa0003", rec.Header().Get("Location"))
}

func TestHandleLatestRun_RedirectToBatch(t *testing.T) {
	dir := t.TempDir()

	// Create a run older than the batch.
	a1 := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("n", 200, 100))
	a1.Metadata.Timestamp = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	writeArchive(t, dir, a1)

	// Create a batch newer than the run.
	b := makeBatchArchive("batch-20260102-100000-bbbb0001", "passed",
		archive.BatchRunEntry{PlanName: "test", RunID: "r1", Outcome: "passed", StepCount: 1, PassedCount: 1, DurationMs: 100},
	)
	b.Metadata.Timestamp = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	writeBatchArchive(t, dir, b)

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/latest")

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/api/batches/batch-20260102-100000-bbbb0001", rec.Header().Get("Location"))
}

func TestHandleLatestRun_NoRuns(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/latest")

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var errResp apiError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.Equal(t, "not_found", errResp.Code)
	assert.Contains(t, errResp.Error, "no runs available")
}

// --- handleGetRun ---

func TestHandleGetRun_Success(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, makeArchive("run-20260101-100000-aaaa0001", "passed",
		makeStep("search", 200, 100),
		makeStep("book", 200, 200),
	))

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/run-20260101-100000-aaaa0001")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))

	var run RunDetail
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&run))
	assert.Equal(t, "run-20260101-100000-aaaa0001", run.RunID)
	assert.Equal(t, "passed", run.Outcome)
	assert.Len(t, run.Steps, 2)
}

func TestHandleGetRun_NotFound(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/run-nonexistent")

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var errResp apiError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.Equal(t, "not_found", errResp.Code)
}

// --- handleGetStep ---

func TestHandleGetStep_Success(t *testing.T) {
	dir := t.TempDir()
	step := makeStepFull("SearchOffers", 200)
	writeArchive(t, dir, makeArchive("run-20260101-100000-aaaa0001", "passed", step))

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/run-20260101-100000-aaaa0001/steps/SearchOffers")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))

	var detail StepDetail
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&detail))
	assert.Equal(t, "SearchOffers", detail.StepID)
	assert.Equal(t, "SearchOffers", detail.Node)
	require.NotNil(t, detail.Request)
	assert.Equal(t, "POST", detail.Request.Method)
	require.NotNil(t, detail.Response)
	assert.Equal(t, 200, detail.Response.Status)
}

func TestHandleGetStep_RunNotFound(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/run-nonexistent/steps/step1")

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var errResp apiError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.Equal(t, "not_found", errResp.Code)
}

func TestHandleGetStep_StepNotFound(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, makeArchive("run-20260101-100000-aaaa0001", "passed",
		makeStep("search", 200, 100),
	))

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/run-20260101-100000-aaaa0001/steps/nonexistent")

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var errResp apiError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.Equal(t, "not_found", errResp.Code)
}

// --- health ---

func TestHandleHealth(t *testing.T) {
	s := newTestServer(t.TempDir())
	rec := serveRequest(s, "GET", "/health")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))

	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "ok", resp["status"])
}

// Verify that the empty list response uses a JSON array, not null.
func TestHandleListRuns_EmptyIsArray(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs")

	// Raw body should start with '[' not 'n' (for null)
	body := rec.Body.String()
	assert.Equal(t, "[]\n", body)
}

// --- handleListBatches ---

func TestHandleListBatches_Success(t *testing.T) {
	dir := t.TempDir()

	b := makeBatchArchive("batch-20260201-100000-aaaa0001", "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-1", Outcome: "passed", DurationMs: 100},
	)
	writeBatchArchive(t, dir, b)

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/batches")

	assert.Equal(t, http.StatusOK, rec.Code)

	var batches []BatchListEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&batches))
	assert.Len(t, batches, 1)
	assert.Equal(t, "batch-20260201-100000-aaaa0001", batches[0].BatchID)
}

func TestHandleListBatches_Empty(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/batches")

	assert.Equal(t, http.StatusOK, rec.Code)

	var batches []BatchListEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&batches))
	assert.Len(t, batches, 0)
	assert.NotNil(t, batches)
}

func TestHandleListBatches_EmptyIsArray(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/batches")

	body := rec.Body.String()
	assert.Equal(t, "[]\n", body)
}

// --- handleGetBatch ---

func TestHandleGetBatch_Success(t *testing.T) {
	dir := t.TempDir()

	b := makeBatchArchive("batch-20260201-100000-aaaa0001", "passed",
		archive.BatchRunEntry{PlanName: "roundtrip", RunID: "run-1", Outcome: "passed", StepCount: 5, DurationMs: 1500},
	)
	writeBatchArchive(t, dir, b)

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/batches/batch-20260201-100000-aaaa0001")

	assert.Equal(t, http.StatusOK, rec.Code)

	var detail BatchDetail
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&detail))
	assert.Equal(t, "batch-20260201-100000-aaaa0001", detail.BatchID)
	assert.Equal(t, "passed", detail.Outcome)
	require.Len(t, detail.Runs, 1)
	assert.Equal(t, "roundtrip", detail.Runs[0].PlanName)
}

func TestHandleGetBatch_NotFound(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/batches/batch-nonexistent")

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var errResp apiError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.Equal(t, "not_found", errResp.Code)
}

// --- handleGetRun with batch member ---

func TestHandleGetRun_BatchMember(t *testing.T) {
	dir := t.TempDir()

	batchID := "batch-20260201-100000-aaaa0001"
	b := makeBatchArchive(batchID, "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-20260201-100000-bbbb0001", Outcome: "passed", DurationMs: 100},
	)
	writeBatchArchive(t, dir, b)
	writeBatchMemberArchive(t, dir, batchID,
		makeArchive("run-20260201-100000-bbbb0001", "passed", makeStep("node", 200, 100)),
	)

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/run-20260201-100000-bbbb0001")

	assert.Equal(t, http.StatusOK, rec.Code)

	var run RunDetail
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&run))
	assert.Equal(t, "run-20260201-100000-bbbb0001", run.RunID)
	assert.Equal(t, batchID, run.BatchID)
}

// --- handleGetAttempt ---

func TestHandleGetAttempt_Success(t *testing.T) {
	dir := t.TempDir()
	runID := "run-20260201-100000-bbbb0001"

	finalArchive := makeArchive(runID, "passed", makeStep("node", 200, 100))
	finalArchive.Metadata.Attempt = 2
	finalArchive.Metadata.TotalAttempts = 2
	writeArchive(t, dir, finalArchive)

	attemptArchive := makeArchive(runID, "failed", makeStep("node", 500, 80))
	attemptArchive.Metadata.Attempt = 1
	attemptArchive.Metadata.TotalAttempts = 2
	attemptArchive.Result.Error = "step failed"
	require.NoError(t, archive.Write(attemptArchive,
		dir+"/"+runID+"/attempt-01.json"))

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/"+runID+"/attempts/1")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))

	var detail RunDetail
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&detail))
	assert.Equal(t, runID, detail.RunID)
	assert.Equal(t, "failed", detail.Outcome)
	assert.Equal(t, 1, detail.Attempt)
}

func TestHandleGetAttempt_InvalidAttempt(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, makeArchive("run-20260201-100000-bbbb0001", "passed", makeStep("node", 200, 100)))

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/run-20260201-100000-bbbb0001/attempts/abc")

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var errResp apiError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.Equal(t, "invalid_parameter", errResp.Code)
}

func TestHandleGetAttempt_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, makeArchive("run-20260201-100000-bbbb0001", "passed", makeStep("node", 200, 100)))

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/run-20260201-100000-bbbb0001/attempts/99")

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var errResp apiError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.Equal(t, "not_found", errResp.Code)
}

// --- handleGetAttemptStep ---

func TestHandleGetAttemptStep_Success(t *testing.T) {
	dir := t.TempDir()
	runID := "run-20260201-100000-bbbb0001"

	// Write final archive.
	finalArchive := makeArchive(runID, "passed", makeStep("node", 200, 100))
	finalArchive.Metadata.Attempt = 2
	finalArchive.Metadata.TotalAttempts = 2
	writeArchive(t, dir, finalArchive)

	// Write attempt-01 with a step that has status 500.
	attemptStep := makeStep("node", 500, 80)
	attemptStep.Error = "step failed"
	attemptArchive := makeArchive(runID, "failed", attemptStep)
	attemptArchive.Metadata.Attempt = 1
	attemptArchive.Metadata.TotalAttempts = 2
	require.NoError(t, archive.Write(attemptArchive,
		dir+"/"+runID+"/attempt-01.json"))

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/"+runID+"/attempts/1/steps/node")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))

	var detail StepDetail
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&detail))
	assert.Equal(t, "node", detail.StepID)
	assert.Equal(t, 500, detail.Status)
	assert.Equal(t, "step failed", detail.Error)
}

func TestHandleGetAttemptStep_InvalidAttempt(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, makeArchive("run-20260201-100000-bbbb0001", "passed", makeStep("node", 200, 100)))

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/run-20260201-100000-bbbb0001/attempts/abc/steps/node")

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var errResp apiError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.Equal(t, "invalid_parameter", errResp.Code)
}

func TestHandleGetAttemptStep_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, makeArchive("run-20260201-100000-bbbb0001", "passed", makeStep("node", 200, 100)))

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/run-20260201-100000-bbbb0001/attempts/99/steps/node")

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var errResp apiError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.Equal(t, "not_found", errResp.Code)
}

// --- handleListRuns with saved filter ---

func TestHandleListRuns_SavedFilter(t *testing.T) {
	dir := t.TempDir()
	a1 := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("n1", 200, 100))
	a1.Metadata.Timestamp = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	a2 := makeArchive("run-20260102-100000-aaaa0002", "passed", makeStep("n2", 200, 200))
	a2.Metadata.Timestamp = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	writeArchive(t, dir, a1)
	writeArchive(t, dir, a2)

	svc := NewArchiveService(dir)
	_, err := svc.RenameRun("run-20260101-100000-aaaa0001", "saved-run")
	require.NoError(t, err)

	s := newTestServer(dir)

	// Without saved filter — returns all runs.
	rec := serveRequest(s, "GET", "/api/runs")
	assert.Equal(t, http.StatusOK, rec.Code)
	var allRuns []RunListEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&allRuns))
	assert.Len(t, allRuns, 2)

	// With saved=true filter — only named runs.
	rec = serveRequest(s, "GET", "/api/runs?saved=true")
	assert.Equal(t, http.StatusOK, rec.Code)
	var savedRuns []RunListEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&savedRuns))
	require.Len(t, savedRuns, 1)
	assert.Equal(t, "saved-run", savedRuns[0].RunID)
}

func TestHandleListBatches_SavedFilter(t *testing.T) {
	dir := t.TempDir()
	b1 := makeBatchArchive("batch-20260201-100000-aaaa0001", "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-1", Outcome: "passed", DurationMs: 100},
	)
	b1.Metadata.Timestamp = time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	b2 := makeBatchArchive("batch-20260202-100000-aaaa0002", "failed",
		archive.BatchRunEntry{PlanName: "plan2", RunID: "run-2", Outcome: "failed", DurationMs: 200},
	)
	b2.Metadata.Timestamp = time.Date(2026, 2, 2, 10, 0, 0, 0, time.UTC)
	writeBatchArchive(t, dir, b1)
	writeBatchArchive(t, dir, b2)

	svc := NewArchiveService(dir)
	_, err := svc.RenameBatch("batch-20260201-100000-aaaa0001", "nightly")
	require.NoError(t, err)

	s := newTestServer(dir)

	// Without filter — all batches.
	rec := serveRequest(s, "GET", "/api/batches")
	assert.Equal(t, http.StatusOK, rec.Code)
	var allBatches []BatchListEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&allBatches))
	assert.Len(t, allBatches, 2)

	// With saved=true.
	rec = serveRequest(s, "GET", "/api/batches?saved=true")
	assert.Equal(t, http.StatusOK, rec.Code)
	var savedBatches []BatchListEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&savedBatches))
	require.Len(t, savedBatches, 1)
	assert.Equal(t, "nightly", savedBatches[0].BatchID)
}

// --- handleRenameRun / handleUnnameRun ---

func TestHandleRenameRun_Success(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("n", 200, 100)))

	s := newTestServer(dir)
	body := strings.NewReader(`{"name":"my-run"}`)
	req := httptest.NewRequest("PUT", "/api/runs/run-20260101-100000-aaaa0001/name", body)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp RenameResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "my-run", resp.Ref)
	assert.Equal(t, "my-run", resp.Name)
}

func TestHandleRenameRun_NotFound(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(dir)
	body := strings.NewReader(`{"name":"my-run"}`)
	req := httptest.NewRequest("PUT", "/api/runs/run-nonexistent/name", body)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleRenameRun_InvalidBody(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("n", 200, 100)))

	s := newTestServer(dir)
	body := strings.NewReader(`not json`)
	req := httptest.NewRequest("PUT", "/api/runs/run-20260101-100000-aaaa0001/name", body)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleUnnameRun_Success(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("n", 200, 100)))

	// First rename.
	svc := NewArchiveService(dir)
	newRef, err := svc.RenameRun("run-20260101-100000-aaaa0001", "my-run")
	require.NoError(t, err)

	s := newTestServer(dir)
	req := httptest.NewRequest("DELETE", "/api/runs/"+newRef+"/name", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp RenameResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "run-20260101-100000-aaaa0001", resp.Ref)
	assert.Equal(t, "", resp.Name)
}

// --- handleRenameBatch / handleUnnameBatch ---

func TestHandleRenameBatch_Success(t *testing.T) {
	dir := t.TempDir()
	b := makeBatchArchive("batch-20260201-100000-aaaa0001", "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-1", Outcome: "passed", DurationMs: 100},
	)
	writeBatchArchive(t, dir, b)

	s := newTestServer(dir)
	body := strings.NewReader(`{"name":"nightly"}`)
	req := httptest.NewRequest("PUT", "/api/batches/batch-20260201-100000-aaaa0001/name", body)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp RenameResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "nightly", resp.Ref)
	assert.Equal(t, "nightly", resp.Name)
}

func TestHandleUnnameBatch_Success(t *testing.T) {
	dir := t.TempDir()
	b := makeBatchArchive("batch-20260201-100000-aaaa0001", "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-1", Outcome: "passed", DurationMs: 100},
	)
	writeBatchArchive(t, dir, b)

	svc := NewArchiveService(dir)
	newRef, err := svc.RenameBatch("batch-20260201-100000-aaaa0001", "nightly")
	require.NoError(t, err)

	s := newTestServer(dir)
	req := httptest.NewRequest("DELETE", "/api/batches/"+newRef+"/name", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp RenameResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "batch-20260201-100000-aaaa0001", resp.Ref)
	assert.Equal(t, "", resp.Name)
}

// Verify unknown archive dirs at the handler level don't crash.
func TestHandleListRuns_MissingArchiveDir(t *testing.T) {
	dir := t.TempDir()

	// Write one archive so we know the dir structure works, then point at a subdir that doesn't exist
	s := newTestServer(dir + "/nonexistent")
	rec := serveRequest(s, "GET", "/api/runs")

	// The service returns nil for nonexistent dirs, handler normalizes to []
	assert.Equal(t, http.StatusOK, rec.Code)

	var runs []RunListEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&runs))
	assert.Len(t, runs, 0)
}
