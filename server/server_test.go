package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_FullLifecycle(t *testing.T) {
	dir := t.TempDir()

	// Create 2 archives with different timestamps for deterministic sort order.
	a1 := makeArchive("run-20260101-100000-aaaa0001", "passed",
		makeStepFull("SearchOffers", 200),
	)
	a1.Metadata.Timestamp = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	a2 := makeArchive("run-20260102-100000-aaaa0002", "failed",
		makeStep("BookOffer", 500, 200),
	)
	a2.Metadata.Timestamp = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	a2.Steps[0].Error = "server error"
	writeArchive(t, dir, a1)
	writeArchive(t, dir, a2)

	s := newTestServer(dir)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := ts.Client()
	// Don't follow redirects so we can check Location header
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	// List runs
	resp, err := client.Get(ts.URL + "/api/runs")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))

	var runs []RunListEntry
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&runs))
	require.Len(t, runs, 2)
	assert.Equal(t, "run-20260102-100000-aaaa0002", runs[0].RunID)

	// Get run
	resp, err = client.Get(ts.URL + "/api/runs/run-20260101-100000-aaaa0001")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var run RunDetail
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&run))
	assert.Equal(t, "run-20260101-100000-aaaa0001", run.RunID)
	assert.Equal(t, "passed", run.Outcome)
	require.Len(t, run.Steps, 1)

	// Get step
	resp, err = client.Get(ts.URL + "/api/runs/run-20260101-100000-aaaa0001/steps/SearchOffers")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var step StepDetail
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&step))
	assert.Equal(t, "SearchOffers", step.Node)
	require.NotNil(t, step.Request)
	assert.Equal(t, "POST", step.Request.Method)
	require.NotNil(t, step.Response)
	assert.Equal(t, 200, step.Response.Status)

	// Latest redirect
	resp, err = client.Get(ts.URL + "/api/runs/latest")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, "/api/runs/run-20260102-100000-aaaa0002", resp.Header.Get("Location"))

	// Not found
	resp, err = client.Get(ts.URL + "/api/runs/run-nonexistent")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// Health
	resp, err = client.Get(ts.URL + "/health")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var health map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&health))
	assert.Equal(t, "ok", health["status"])
}

// --- static serving & SPA fallback ---

func TestStaticFileServing(t *testing.T) {
	s := newTestServer(t.TempDir())
	rec := serveRequest(s, "GET", "/")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "<html")
}

func TestSPAFallback(t *testing.T) {
	s := newTestServer(t.TempDir())
	rec := serveRequest(s, "GET", "/runs/some-id")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "<html")
}

func TestAPINotCaughtBySPA(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("n", 200, 100)))
	s := newTestServer(dir)
	rec := serveRequest(s, "GET", "/api/runs")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
}

func TestHealthNotCaughtBySPA(t *testing.T) {
	s := newTestServer(t.TempDir())
	rec := serveRequest(s, "GET", "/health")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
}

// --- dev proxy ---

func TestDevProxy_NoVite(t *testing.T) {
	s := NewServer(ServerOptions{
		ArchiveDir: t.TempDir(),
		DevMode:    true,
		ViteURL:    "http://127.0.0.1:1", // unreachable port
	})
	rec := serveRequest(s, "GET", "/")

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "Vite dev server")
}

func TestDevProxy_WithMockVite(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>vite page</body></html>"))
	}))
	defer mock.Close()

	s := NewServer(ServerOptions{
		ArchiveDir: t.TempDir(),
		DevMode:    true,
		ViteURL:    mock.URL,
	})
	rec := serveRequest(s, "GET", "/")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "vite page")
}

func TestDevProxy_APINotProxied(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("from vite"))
	}))
	defer mock.Close()

	dir := t.TempDir()
	writeArchive(t, dir, makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("n", 200, 100)))
	s := NewServer(ServerOptions{
		ArchiveDir: dir,
		DevMode:    true,
		ViteURL:    mock.URL,
	})
	rec := serveRequest(s, "GET", "/api/runs")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
}

// --- integration ---

func TestIntegration_ListenAndShutdown(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("n", 200, 100)))

	s := NewServer(ServerOptions{Port: 0, ArchiveDir: dir})

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.ListenAndServe()
	}()

	// Wait for server to be ready
	require.Eventually(t, func() bool {
		return s.Addr() != ""
	}, 2*time.Second, 10*time.Millisecond, "server did not start")

	// Verify we can reach it
	resp, err := http.Get("http://" + s.Addr() + "/health")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, s.Shutdown(ctx))

	// ListenAndServe should return ErrServerClosed
	err = <-errCh
	assert.ErrorIs(t, err, http.ErrServerClosed)
}
