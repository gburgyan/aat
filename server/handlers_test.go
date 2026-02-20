package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
	writeArchive(t, dir, makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("n1", 200, 100)))
	writeArchive(t, dir, makeArchive("run-20260102-100000-aaaa0002", "passed", makeStep("n2", 200, 200)))
	writeArchive(t, dir, makeArchive("run-20260103-100000-aaaa0003", "failed", makeStep("n3", 500, 300)))

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

func TestHandleLatestRun_Redirect(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("n", 200, 100)))
	writeArchive(t, dir, makeArchive("run-20260103-100000-aaaa0003", "passed", makeStep("n", 200, 100)))

	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs/latest")

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/api/runs/run-20260103-100000-aaaa0003", rec.Header().Get("Location"))
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
