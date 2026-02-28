package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gburgyan/aat/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- matchVisualizers ---

func TestMatchVisualizers_BodyContains(t *testing.T) {
	defs := []config.VisualizerDef{
		{ID: "flight", Name: "Flight", Match: config.VisualizerMatch{BodyContains: "CatalogProductOfferingsResponse"}},
		{ID: "hotel", Name: "Hotel", Match: config.VisualizerMatch{BodyContains: "HotelResponse"}},
	}

	body := json.RawMessage(`{"CatalogProductOfferingsResponse":{"offers":[]},"transactionId":"abc"}`)
	hits := matchVisualizers(defs, "SearchNode", body)
	require.Len(t, hits, 1)
	assert.Equal(t, "flight", hits[0].ID)
}

func TestMatchVisualizers_NodeFilter(t *testing.T) {
	defs := []config.VisualizerDef{
		{ID: "res", Name: "Reservation", Match: config.VisualizerMatch{Node: "CreateReservation"}},
	}

	body := json.RawMessage(`{"ReservationResponse":{}}`)

	// Matching node
	hits := matchVisualizers(defs, "CreateReservation", body)
	require.Len(t, hits, 1)
	assert.Equal(t, "res", hits[0].ID)

	// Non-matching node
	hits = matchVisualizers(defs, "DeleteReservation", body)
	assert.Empty(t, hits)
}

func TestMatchVisualizers_CombinedMatch(t *testing.T) {
	defs := []config.VisualizerDef{
		{ID: "flight", Name: "Flight", Match: config.VisualizerMatch{
			Node:         "CatalogProductOfferings",
			BodyContains: "CatalogProductOfferingsResponse",
		}},
	}

	body := json.RawMessage(`{"CatalogProductOfferingsResponse":{}}`)

	// Both match
	hits := matchVisualizers(defs, "CatalogProductOfferings", body)
	require.Len(t, hits, 1)

	// Node doesn't match
	hits = matchVisualizers(defs, "OtherNode", body)
	assert.Empty(t, hits)

	// Body doesn't match
	hits = matchVisualizers(defs, "CatalogProductOfferings", json.RawMessage(`{"Other":{}}`))
	assert.Empty(t, hits)
}

func TestMatchVisualizers_EmptyDefs(t *testing.T) {
	body := json.RawMessage(`{"Foo":{}}`)
	hits := matchVisualizers(nil, "Node", body)
	assert.Nil(t, hits)
}

func TestMatchVisualizers_EmptyBody(t *testing.T) {
	defs := []config.VisualizerDef{
		{ID: "test", Name: "Test", Match: config.VisualizerMatch{BodyContains: "Foo"}},
	}
	hits := matchVisualizers(defs, "Node", nil)
	assert.Nil(t, hits)
}

func TestMatchVisualizers_NoMatchCriteria(t *testing.T) {
	defs := []config.VisualizerDef{
		{ID: "empty", Name: "Empty", Match: config.VisualizerMatch{}},
	}
	body := json.RawMessage(`{"Foo":{}}`)
	hits := matchVisualizers(defs, "Node", body)
	assert.Empty(t, hits)
}

// --- handleGetVisualizer ---

func TestHandleGetVisualizer_Success(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "visualizers.yaml"), `
visualizers:
  - id: test-viz
    name: Test Viz
    file: test.html
    match:
      node: Foo
`)
	writeTestFile(t, filepath.Join(dir, "test.html"), "<html><body>Hello</body></html>")

	s := NewServer(ServerOptions{
		ArchiveDir:    t.TempDir(),
		VisualizerDir: dir,
	})

	rec := serveRequest(s, "GET", "/api/visualizers/test-viz")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "connect-src 'none'")
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "script-src 'unsafe-inline'")
	assert.Equal(t, "<html><body>Hello</body></html>", rec.Body.String())
}

func TestHandleGetVisualizer_NotFound(t *testing.T) {
	s := NewServer(ServerOptions{ArchiveDir: t.TempDir()})
	rec := serveRequest(s, "GET", "/api/visualizers/nonexistent")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleGetVisualizer_CSPHeaders(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "visualizers.yaml"), `
visualizers:
  - id: csp-test
    name: CSP Test
    file: csp.html
    match:
      node: Test
`)
	writeTestFile(t, filepath.Join(dir, "csp.html"), "<html></html>")

	s := NewServer(ServerOptions{
		ArchiveDir:    t.TempDir(),
		VisualizerDir: dir,
	})

	rec := serveRequest(s, "GET", "/api/visualizers/csp-test")
	csp := rec.Header().Get("Content-Security-Policy")
	assert.Contains(t, csp, "default-src 'none'")
	assert.Contains(t, csp, "script-src 'unsafe-inline'")
	assert.Contains(t, csp, "style-src 'unsafe-inline'")
	assert.Contains(t, csp, "img-src https: data:")
	assert.Contains(t, csp, "connect-src 'none'")
}

// --- StepDetail enrichment ---

func TestEnrichStepVisualizers(t *testing.T) {
	s := &Server{
		visualizers: []config.VisualizerDef{
			{ID: "flight", Name: "Flight Search", Match: config.VisualizerMatch{BodyContains: "CatalogProductOfferingsResponse"}},
		},
	}

	step := &StepDetail{
		Node: "SearchOffers",
		Response: &ResponseDetail{
			Status: 200,
			Body:   json.RawMessage(`{"CatalogProductOfferingsResponse":{"offers":[]}}`),
		},
	}

	s.enrichStepVisualizers(step)
	require.Len(t, step.Visualizers, 1)
	assert.Equal(t, "flight", step.Visualizers[0].ID)
	assert.Equal(t, "Flight Search", step.Visualizers[0].Name)
}

func TestEnrichStepVisualizers_NoMatch(t *testing.T) {
	s := &Server{
		visualizers: []config.VisualizerDef{
			{ID: "flight", Name: "Flight", Match: config.VisualizerMatch{BodyContains: "CatalogProductOfferingsResponse"}},
		},
	}

	step := &StepDetail{
		Node: "BookOffer",
		Response: &ResponseDetail{
			Status: 200,
			Body:   json.RawMessage(`{"ReservationResponse":{}}`),
		},
	}

	s.enrichStepVisualizers(step)
	assert.Nil(t, step.Visualizers)
}

func TestEnrichStepVisualizers_NoVisualizers(t *testing.T) {
	s := &Server{}

	step := &StepDetail{
		Node: "SearchOffers",
		Response: &ResponseDetail{
			Status: 200,
			Body:   json.RawMessage(`{"CatalogProductOfferingsResponse":{}}`),
		},
	}

	s.enrichStepVisualizers(step)
	assert.Nil(t, step.Visualizers)
}

// --- Integration: step endpoint returns visualizers ---

// --- helpers ---

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestGetStep_WithVisualizers(t *testing.T) {
	archiveDir := t.TempDir()
	vizDir := t.TempDir()

	writeTestFile(t, filepath.Join(vizDir, "visualizers.yaml"), `
visualizers:
  - id: flight
    name: Flight Search
    file: flight.html
    match:
      bodyContains: CatalogProductOfferingsResponse
`)
	writeTestFile(t, filepath.Join(vizDir, "flight.html"), "<html></html>")

	a := makeArchive("run-20260101-100000-aaaa0001", "passed",
		makeStepFull("SearchOffers", 200),
	)
	// Set a response body that matches.
	a.Steps[0].Response.Body = json.RawMessage(`{"CatalogProductOfferingsResponse":{"offers":[]}}`)
	writeArchive(t, archiveDir, a)

	s := NewServer(ServerOptions{
		ArchiveDir:    archiveDir,
		VisualizerDir: vizDir,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/runs/run-20260101-100000-aaaa0001/steps/SearchOffers", nil)
	s.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var step StepDetail
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&step))
	require.Len(t, step.Visualizers, 1)
	assert.Equal(t, "flight", step.Visualizers[0].ID)
	assert.Equal(t, "Flight Search", step.Visualizers[0].Name)
}
