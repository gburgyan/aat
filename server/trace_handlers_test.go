package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test helpers ---

func newTestServerWithTraces(archiveDir, tracesDir string) *Server {
	return NewServer(ServerOptions{ArchiveDir: archiveDir, TracesDir: tracesDir})
}

// --- handleListTraces ---

func TestHandleListTraces_Success(t *testing.T) {
	archiveDir := t.TempDir()
	tracesDir := t.TempDir()

	writeTrace(t, tracesDir, makeTrace("trace-20260101-100000-aaaa0001", "book a flight"))
	writeTrace(t, tracesDir, makeTrace("trace-20260102-100000-aaaa0002", "search hotels"))

	s := newTestServerWithTraces(archiveDir, tracesDir)
	rec := serveRequest(s, "GET", "/api/traces")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))

	var traces []TraceListEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&traces))
	assert.Len(t, traces, 2)
	assert.Equal(t, "trace-20260102-100000-aaaa0002", traces[0].TraceID)
}

func TestHandleListTraces_Empty(t *testing.T) {
	archiveDir := t.TempDir()
	tracesDir := t.TempDir()

	s := newTestServerWithTraces(archiveDir, tracesDir)
	rec := serveRequest(s, "GET", "/api/traces")

	assert.Equal(t, http.StatusOK, rec.Code)

	var traces []TraceListEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&traces))
	assert.Len(t, traces, 0)
	assert.NotNil(t, traces) // Should be [] not null
}

func TestHandleListTraces_EmptyIsArray(t *testing.T) {
	archiveDir := t.TempDir()
	tracesDir := t.TempDir()

	s := newTestServerWithTraces(archiveDir, tracesDir)
	rec := serveRequest(s, "GET", "/api/traces")

	body := rec.Body.String()
	assert.Equal(t, "[]\n", body)
}

func TestHandleListTraces_WithLimit(t *testing.T) {
	archiveDir := t.TempDir()
	tracesDir := t.TempDir()

	for i := 0; i < 5; i++ {
		tr := makeTrace(
			"trace-2026010"+string(rune('1'+i))+"-100000-aaaa000"+string(rune('1'+i)),
			"prompt",
		)
		writeTrace(t, tracesDir, tr)
	}

	s := newTestServerWithTraces(archiveDir, tracesDir)
	rec := serveRequest(s, "GET", "/api/traces?limit=2")

	assert.Equal(t, http.StatusOK, rec.Code)

	var traces []TraceListEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&traces))
	assert.Len(t, traces, 2)
}

func TestHandleListTraces_InvalidLimit(t *testing.T) {
	archiveDir := t.TempDir()
	tracesDir := t.TempDir()

	s := newTestServerWithTraces(archiveDir, tracesDir)
	rec := serveRequest(s, "GET", "/api/traces?limit=abc")

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var errResp apiError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.Equal(t, "invalid_parameter", errResp.Code)
}

// --- handleGetTrace ---

func TestHandleGetTrace_Success(t *testing.T) {
	archiveDir := t.TempDir()
	tracesDir := t.TempDir()

	tr := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	tr.WorkflowName = "roundtrip-booking"
	writeTrace(t, tracesDir, tr)

	s := newTestServerWithTraces(archiveDir, tracesDir)
	rec := serveRequest(s, "GET", "/api/traces/trace-20260101-100000-aaaa0001")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))

	var detail TraceDetail
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&detail))
	assert.Equal(t, "trace-20260101-100000-aaaa0001", detail.TraceID)
	assert.Equal(t, "book a flight", detail.Prompt)
	assert.Equal(t, "roundtrip-booking", detail.WorkflowName)
	require.NotNil(t, detail.SelectionCall)
	assert.Equal(t, "gpt-4", detail.SelectionCall.Model)
}

func TestHandleGetTrace_NotFound(t *testing.T) {
	archiveDir := t.TempDir()
	tracesDir := t.TempDir()

	s := newTestServerWithTraces(archiveDir, tracesDir)
	rec := serveRequest(s, "GET", "/api/traces/trace-nonexistent")

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var errResp apiError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.Equal(t, "not_found", errResp.Code)
}
