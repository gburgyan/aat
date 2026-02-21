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
	// chdir to an empty directory and clear env so no manifest is discovered
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)

	t.Setenv("AAT_PROJECT", "")

	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{}, &buf)
	assert.Equal(t, 1, code)
	assert.Contains(t, buf.String(), "FAILED")
	assert.Contains(t, buf.String(), "no manifest found")
}

func TestValidate_ExplicitManifestNotFound(t *testing.T) {
	// chdir to an empty directory so CWD walk-up doesn't find a manifest.
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)

	t.Setenv("AAT_PROJECT", "")

	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))

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
	workflowsDir := filepath.Join(dir, "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	planContent := `execution:
  steps:
    - node: testNode
      values:
        input1: "hello"
`
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "good.yaml"), []byte(planContent), 0644))

	// Create manifest
	manifestContent := `
name: test-project
graph: graph.yaml
templates: templates/
workflows: workflows/
`
	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0644))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifestPath}, &buf)
	assert.Equal(t, 0, code, "output: %s", buf.String())
	assert.Contains(t, buf.String(), "Workflows")
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
	workflowsDir := filepath.Join(dir, "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "good.yaml"), []byte(`execution:
  steps:
    - node: testNode
      values:
        input1: "hello"
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "bad.yaml"), []byte(`execution:
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
workflows: workflows/
`
	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0644))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifestPath}, &buf)
	assert.Equal(t, 1, code, "output: %s", buf.String())
	assert.Contains(t, buf.String(), "Workflows")
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

func TestValidate_WorkflowTemplateSkipsGraphValidation(t *testing.T) {
	dir := t.TempDir()

	// Create graph with a workflow referencing a template
	graphContent := `version: "1.0.0"
workflows:
  - name: book-flight
    description: Book a flight
    template: workflows/booking.yaml
  - name: add-seat
    kind: addon
    description: Add a seat
    template: workflows/seat-addon.yaml
    after: CreateReservation
nodes:
  SearchAir:
    description: search
    adapter: search.adapter
    inputs:
      - name: origin
        type: string
    outputs:
      - name: offerId
        type: string
  CreateReservation:
    description: reserve
    adapter: reserve.adapter
    inputs:
      - name: offerId
        type: string
        from: SearchAir.offerId
    outputs:
      - name: reservationId
        type: string
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte(graphContent), 0644))

	// Create templates dir with outputs matching the graph
	templatesDir := filepath.Join(dir, "templates")
	require.NoError(t, os.MkdirAll(templatesDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(templatesDir, "search.adapter.yaml"), []byte(`adapter: search.adapter
protocol: http
request:
  method: POST
  path: /search
  body: "{}"
response:
  extract:
    offerId: "$.offerId"
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(templatesDir, "reserve.adapter.yaml"), []byte(`adapter: reserve.adapter
protocol: http
request:
  method: POST
  path: /reserve
  body: "{}"
response:
  extract:
    reservationId: "$.reservationId"
`), 0644))

	// Create plans dir with workflow templates that have missing inputs
	// (offerId is not provided — it would be wired by ComposeWithAddons)
	workflowsDir := filepath.Join(dir, "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// seat-addon.yaml: workflow template — intentionally missing offerId
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "seat-addon.yaml"), []byte(`execution:
  steps:
    - node: CreateReservation
      values:
        offerId: "AUTOWIRE"
`), 0644))

	// booking.yaml: base workflow template
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "booking.yaml"), []byte(`execution:
  steps:
    - node: SearchAir
      values:
        origin: "JFK"
    - node: CreateReservation
      dependsOn: [SearchAir]
      values:
        offerId:
          from: SearchAir.offerId
`), 0644))

	// Create manifest
	manifestContent := `
name: test-project
graph: graph.yaml
templates: templates/
workflows: workflows/
`
	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0644))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifestPath}, &buf)
	output := buf.String()
	assert.Equal(t, 0, code, "output: %s", output)
	assert.Contains(t, output, "Workflows")
	assert.Contains(t, output, "2 templates")
	assert.Contains(t, output, "PASSED")
}

func TestValidate_NonTemplatePlanStillValidated(t *testing.T) {
	dir := t.TempDir()

	// Create graph with a workflow referencing only one template
	graphContent := `version: "1.0.0"
workflows:
  - name: book-flight
    description: Book a flight
    template: workflows/booking.yaml
nodes:
  SearchAir:
    description: search
    adapter: search.adapter
    inputs:
      - name: origin
        type: string
    outputs:
      - name: offerId
        type: string
  CreateReservation:
    description: reserve
    adapter: reserve.adapter
    inputs:
      - name: offerId
        type: string
        from: SearchAir.offerId
    outputs:
      - name: reservationId
        type: string
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte(graphContent), 0644))

	// Create templates dir with outputs matching the graph
	templatesDir := filepath.Join(dir, "templates")
	require.NoError(t, os.MkdirAll(templatesDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(templatesDir, "search.adapter.yaml"), []byte(`adapter: search.adapter
protocol: http
request:
  method: POST
  path: /search
  body: "{}"
response:
  extract:
    offerId: "$.offerId"
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(templatesDir, "reserve.adapter.yaml"), []byte(`adapter: reserve.adapter
protocol: http
request:
  method: POST
  path: /reserve
  body: "{}"
response:
  extract:
    reservationId: "$.reservationId"
`), 0644))

	// Create plans dir
	workflowsDir := filepath.Join(dir, "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// booking.yaml: referenced as workflow template — skipped for graph validation
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "booking.yaml"), []byte(`execution:
  steps:
    - node: SearchAir
      values:
        origin: "JFK"
    - node: CreateReservation
      dependsOn: [SearchAir]
      values:
        offerId:
          from: SearchAir.offerId
`), 0644))

	// bad-standalone.yaml: NOT referenced by any workflow — should be graph-validated and fail
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "bad-standalone.yaml"), []byte(`execution:
  steps:
    - node: NonexistentNode
      values:
        foo: "bar"
`), 0644))

	// Create manifest
	manifestContent := `
name: test-project
graph: graph.yaml
templates: templates/
workflows: workflows/
`
	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0644))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifestPath}, &buf)
	output := buf.String()
	assert.Equal(t, 1, code, "output: %s", output)
	assert.Contains(t, output, "Workflows")
	assert.Contains(t, output, "FAILED")
	assert.Contains(t, output, "bad-standalone.yaml")
}
