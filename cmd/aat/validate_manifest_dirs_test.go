package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeManifestProject writes a graph and a template that validate, and a
// manifest with the given content, and returns the manifest's path.
func writeManifestProject(t *testing.T, manifest string) string {
	t.Helper()
	dir := t.TempDir()
	graphContent := `version: "1.0.0"
nodes:
  testNode:
    description: test
    adapter: test.adapter
    inputs:
      - name: input1
        type: string
    outputs:
      - name: output1
        type: string
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte(graphContent), 0o644))
	templatesDir := filepath.Join(dir, "templates")
	require.NoError(t, os.MkdirAll(templatesDir, 0o755))
	templateContent := `adapter: test.adapter
protocol: http
request:
  method: POST
  path: /test
  body: |
    {"input1": "{{input1}}"}
response:
  extract:
    output1: "$.result"
`
	require.NoError(t, os.WriteFile(filepath.Join(templatesDir, "test.adapter.yaml"), []byte(templateContent), 0o644))
	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifest), 0o644))
	return manifestPath
}

// TestValidate_ManifestDirsNotYetWritten checks that directories a manifest
// names before anything is written to them are notes, read as empty, with paths
// relative to the manifest.
func TestValidate_ManifestDirsNotYetWritten(t *testing.T) {
	manifestPath := writeManifestProject(t, "name: test-project\ngraph: graph.yaml\ntemplates: templates/\nworkflows: workflows/\nlayers: layers/\nplans: plans/\n")

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifestPath}, &buf)
	out := buf.String()
	assert.Equal(t, 0, code, out)
	assert.Contains(t, out, "note: workflows dir workflows doesn't exist yet; it reads as empty")
	assert.Contains(t, out, "note: layers dir layers doesn't exist yet; it reads as empty")
	assert.Contains(t, out, "note: plans dir plans doesn't exist yet; it reads as empty")
	assert.NotContains(t, out, "FAILED")
}

// TestValidate_ManifestErrorsNameManifestPaths checks that a missing file is
// named relative to the manifest, not to the directory aat runs in.
func TestValidate_ManifestErrorsNameManifestPaths(t *testing.T) {
	manifestPath := writeManifestProject(t, "name: test-project\ngraph: missing.yaml\ntemplates: templates/\n")

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifestPath}, &buf)
	assert.Equal(t, 1, code)
	assert.Contains(t, buf.String(), "graph file not found: missing.yaml\n")
}
