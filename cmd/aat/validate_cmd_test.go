package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_NoManifestFound(t *testing.T) {
	// chdir to an empty directory so FindManifest fails
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)

	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{}, &buf)
	assert.Equal(t, 1, code)
	assert.Contains(t, buf.String(), "FAILED")
	assert.Contains(t, buf.String(), "aat-project.yaml not found")
}

func TestValidate_ExplicitManifestNotFound(t *testing.T) {
	var buf bytes.Buffer
	code := validateCommand(&validateArgs{
		ManifestPath: "/nonexistent/aat-project.yaml",
	}, &buf)
	assert.Equal(t, 1, code)
	assert.Contains(t, buf.String(), "FAILED")
}

func TestValidate_ManifestWithMissingGraphFile(t *testing.T) {
	dir := t.TempDir()
	// Write a manifest referencing files that don't exist on disk
	content := `
name: test-project
graph: graph.yaml
templates: templates/
`
	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(content), 0644))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifestPath}, &buf)
	assert.Equal(t, 1, code)
	assert.Contains(t, buf.String(), "Manifest")
	assert.Contains(t, buf.String(), "FAILED")
	assert.Contains(t, buf.String(), "graph file not found")
}

func TestValidate_ManifestWithMissingTemplatesDir(t *testing.T) {
	dir := t.TempDir()
	// Create graph file but not templates
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte(graphContent), 0644))

	content := `
name: test-project
graph: graph.yaml
templates: templates/
`
	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(content), 0644))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifestPath}, &buf)
	assert.Equal(t, 1, code)
	assert.Contains(t, buf.String(), "templates dir not found")
}

func TestValidate_MinimalValidProject(t *testing.T) {
	dir := t.TempDir()

	// Create graph
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte(graphContent), 0644))

	// Create templates dir with a matching template
	templatesDir := filepath.Join(dir, "templates")
	require.NoError(t, os.MkdirAll(templatesDir, 0755))
	templateContent := `adapter: test.adapter
protocol: http
request:
  method: POST
  path: /test
  body: |
    {"input1": "{{.input1}}"}
response:
  extract:
    output1: "$.result"
`
	require.NoError(t, os.WriteFile(filepath.Join(templatesDir, "test.adapter.yaml"), []byte(templateContent), 0644))

	// Create manifest
	manifestContent := `
name: test-project
graph: graph.yaml
templates: templates/
`
	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0644))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifestPath}, &buf)
	assert.Equal(t, 0, code, "output: %s", buf.String())
	assert.Contains(t, buf.String(), "PASSED")
	assert.Contains(t, buf.String(), "Manifest")
	assert.Contains(t, buf.String(), "Graph structure")
}

func TestValidate_WithPlansDir_AllValid(t *testing.T) {
	dir := t.TempDir()

	// Create graph
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte(graphContent), 0644))

	// Create templates dir
	templatesDir := filepath.Join(dir, "templates")
	require.NoError(t, os.MkdirAll(templatesDir, 0755))
	templateContent := `adapter: test.adapter
protocol: http
request:
  method: POST
  path: /test
  body: |
    {"input1": "{{.input1}}"}
response:
  extract:
    output1: "$.result"
`
	require.NoError(t, os.WriteFile(filepath.Join(templatesDir, "test.adapter.yaml"), []byte(templateContent), 0644))

	// Create plans dir with a valid plan
	plansDir := filepath.Join(dir, "plans")
	require.NoError(t, os.MkdirAll(plansDir, 0755))
	planContent := `execution:
  steps:
    - node: testNode
      values:
        input1: "hello"
`
	require.NoError(t, os.WriteFile(filepath.Join(plansDir, "good.yaml"), []byte(planContent), 0644))

	// Create manifest
	manifestContent := `
name: test-project
graph: graph.yaml
templates: templates/
plans: plans/
`
	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0644))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifestPath}, &buf)
	assert.Equal(t, 0, code, "output: %s", buf.String())
	assert.Contains(t, buf.String(), "Plans")
	assert.Contains(t, buf.String(), "(1 files)")
	assert.Contains(t, buf.String(), "PASSED")
}

func TestValidate_WithPlansDir_OneInvalid(t *testing.T) {
	dir := t.TempDir()

	// Create graph
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte(graphContent), 0644))

	// Create templates dir
	templatesDir := filepath.Join(dir, "templates")
	require.NoError(t, os.MkdirAll(templatesDir, 0755))
	templateContent := `adapter: test.adapter
protocol: http
request:
  method: POST
  path: /test
  body: |
    {"input1": "{{.input1}}"}
response:
  extract:
    output1: "$.result"
`
	require.NoError(t, os.WriteFile(filepath.Join(templatesDir, "test.adapter.yaml"), []byte(templateContent), 0644))

	// Create plans dir with one valid and one invalid plan
	plansDir := filepath.Join(dir, "plans")
	require.NoError(t, os.MkdirAll(plansDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(plansDir, "good.yaml"), []byte(`execution:
  steps:
    - node: testNode
      values:
        input1: "hello"
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(plansDir, "bad.yaml"), []byte(`execution:
  steps:
    - node: nonexistentNode
      values:
        input1: "hello"
`), 0644))

	// Create manifest
	manifestContent := `
name: test-project
graph: graph.yaml
templates: templates/
plans: plans/
`
	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0644))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifestPath}, &buf)
	assert.Equal(t, 1, code, "output: %s", buf.String())
	assert.Contains(t, buf.String(), "Plans")
	assert.Contains(t, buf.String(), "FAILED")
	assert.Contains(t, buf.String(), "bad.yaml")
}

func TestValidate_OutputFormat(t *testing.T) {
	dir := t.TempDir()

	// Create a minimal valid project
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte(graphContent), 0644))

	templatesDir := filepath.Join(dir, "templates")
	require.NoError(t, os.MkdirAll(templatesDir, 0755))
	templateContent := `adapter: test.adapter
protocol: http
request:
  method: POST
  path: /test
  body: |
    {"input1": "{{.input1}}"}
response:
  extract:
    output1: "$.result"
`
	require.NoError(t, os.WriteFile(filepath.Join(templatesDir, "test.adapter.yaml"), []byte(templateContent), 0644))

	manifestContent := `
name: my-project
graph: graph.yaml
templates: templates/
`
	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0644))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifestPath}, &buf)
	assert.Equal(t, 0, code)

	output := buf.String()
	// Check alignment and content
	assert.Contains(t, output, "Manifest:")
	assert.Contains(t, output, "(project: my-project)")
	assert.Contains(t, output, "Graph structure:")
	assert.Contains(t, output, "(1 nodes)")
	assert.Contains(t, output, "Adapter outputs:")
	assert.Contains(t, output, "(1 templates)")
	assert.Contains(t, output, "Project validation: PASSED")
}
