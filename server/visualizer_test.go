package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- LoadVisualizers ---

func TestLoadVisualizers_EmptyDir(t *testing.T) {
	defs, err := LoadVisualizers("")
	require.NoError(t, err)
	assert.Nil(t, defs)
}

func TestLoadVisualizers_NoManifest(t *testing.T) {
	dir := t.TempDir()
	defs, err := LoadVisualizers(dir)
	require.NoError(t, err)
	assert.Nil(t, defs)
}

func TestLoadVisualizers_ValidManifest(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "visualizers.yaml"), `
visualizers:
  - id: flight-search
    name: Flight Search Results
    file: flight-search.html
    match:
      bodyContains: CatalogProductOfferingsResponse
  - id: reservation
    name: Reservation Detail
    file: reservation.html
    match:
      node: CreateReservation
`)
	writeTestFile(t, filepath.Join(dir, "flight-search.html"), "<html>flight</html>")
	writeTestFile(t, filepath.Join(dir, "reservation.html"), "<html>reservation</html>")

	defs, err := LoadVisualizers(dir)
	require.NoError(t, err)
	require.Len(t, defs, 2)

	assert.Equal(t, "flight-search", defs[0].ID)
	assert.Equal(t, "Flight Search Results", defs[0].Name)
	assert.Equal(t, "flight-search.html", defs[0].File)
	assert.Equal(t, "CatalogProductOfferingsResponse", defs[0].Match.BodyContains)

	assert.Equal(t, "reservation", defs[1].ID)
	assert.Equal(t, "CreateReservation", defs[1].Match.Node)
}

func TestLoadVisualizers_MissingID(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "visualizers.yaml"), `
visualizers:
  - name: Test
    file: test.html
    match:
      node: Foo
`)
	writeTestFile(t, filepath.Join(dir, "test.html"), "<html></html>")

	_, err := LoadVisualizers(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required field: id")
}

func TestLoadVisualizers_MissingFile(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "visualizers.yaml"), `
visualizers:
  - id: test
    file: missing.html
    match:
      node: Foo
`)

	_, err := LoadVisualizers(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestLoadVisualizers_DefaultName(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "visualizers.yaml"), `
visualizers:
  - id: test-viz
    file: test.html
    match:
      node: Foo
`)
	writeTestFile(t, filepath.Join(dir, "test.html"), "<html></html>")

	defs, err := LoadVisualizers(dir)
	require.NoError(t, err)
	require.Len(t, defs, 1)
	assert.Equal(t, "test-viz", defs[0].Name) // defaults to ID
}

// --- matchVisualizers ---

func TestMatchVisualizers_BodyContains(t *testing.T) {
	defs := []VisualizerDef{
		{ID: "flight", Name: "Flight", Match: VisualizerMatch{BodyContains: "CatalogProductOfferingsResponse"}},
		{ID: "hotel", Name: "Hotel", Match: VisualizerMatch{BodyContains: "HotelResponse"}},
	}

	body := json.RawMessage(`{"CatalogProductOfferingsResponse":{"offers":[]},"transactionId":"abc"}`)
	hits := matchVisualizers(defs, "SearchNode", body)
	require.Len(t, hits, 1)
	assert.Equal(t, "flight", hits[0].ID)
}

func TestMatchVisualizers_NodeFilter(t *testing.T) {
	defs := []VisualizerDef{
		{ID: "res", Name: "Reservation", Match: VisualizerMatch{Node: "CreateReservation"}},
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
	defs := []VisualizerDef{
		{ID: "flight", Name: "Flight", Match: VisualizerMatch{
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
	defs := []VisualizerDef{
		{ID: "test", Name: "Test", Match: VisualizerMatch{BodyContains: "Foo"}},
	}
	hits := matchVisualizers(defs, "Node", nil)
	assert.Nil(t, hits)
}

func TestMatchVisualizers_NoMatchCriteria(t *testing.T) {
	defs := []VisualizerDef{
		{ID: "empty", Name: "Empty", Match: VisualizerMatch{}},
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
		visualizers: []VisualizerDef{
			{ID: "flight", Name: "Flight Search", Match: VisualizerMatch{BodyContains: "CatalogProductOfferingsResponse"}},
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
		visualizers: []VisualizerDef{
			{ID: "flight", Name: "Flight", Match: VisualizerMatch{BodyContains: "CatalogProductOfferingsResponse"}},
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

// --- helpers ---

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
