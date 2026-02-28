package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
