package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
)

func TestValidate_NoManifestFound(t *testing.T) {
	// chdir to an empty directory and clear env so no manifest is discovered
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()

	t.Setenv("AAT_PROJECT", "")

	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{}, &buf)
	assert.Equal(t, 2, code)
	assert.Contains(t, buf.String(), "FAILED")
	assert.Contains(t, buf.String(), "no manifest found")
}

func TestValidate_ExplicitManifestNotFound(t *testing.T) {
	// chdir to an empty directory so CWD walk-up doesn't find a manifest.
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()

	t.Setenv("AAT_PROJECT", "")

	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{
		ManifestPath: "/nonexistent/aat-project.yaml",
	}, &buf)
	assert.Equal(t, 2, code)
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
	assert.Contains(t, buf.String(), "(1 file)")
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
	assert.Contains(t, output, "(1 node)")
	assert.Contains(t, output, "Adapter outputs:")
	assert.Contains(t, output, "(1 template)")
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

func TestValidate_WithRecipePlan(t *testing.T) {
	dir := t.TempDir()

	// Create graph with a workflow
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
    outputs:
      - name: reservationId
        type: string
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte(graphContent), 0644))

	// Create templates
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

	// Create workflow template
	workflowsDir := filepath.Join(dir, "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "booking.yaml"), []byte(`execution:
  steps:
    - node: SearchAir
      values:
        origin: ""
    - node: CreateReservation
      dependsOn: [SearchAir]
      values:
        offerId:
          from: SearchAir.offerId
`), 0644))

	// Create plans dir with a recipe
	plansDir := filepath.Join(dir, "plans")
	require.NoError(t, os.MkdirAll(plansDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(plansDir, "recipe.yaml"), []byte(`kind: recipe
metadata:
  prompt: "book a flight from JFK"
selection:
  workflow: book-flight
overrides:
  values:
    SearchAir.origin: "JFK"
`), 0644))

	// Create manifest
	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(`
name: test-project
graph: graph.yaml
templates: templates/
workflows: workflows/
plans:
  - plans/
`), 0644))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifestPath}, &buf)
	output := buf.String()
	assert.Equal(t, 0, code, "output: %s", output)
	assert.Contains(t, output, "Plans")
	assert.Contains(t, output, "1 recipe")
	assert.Contains(t, output, "PASSED")
}

func TestValidate_WithRecipePlan_BadWorkflow(t *testing.T) {
	dir := t.TempDir()

	// Create a simple graph (no workflows)
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
	require.NoError(t, os.WriteFile(filepath.Join(templatesDir, "test.adapter.yaml"), []byte(`adapter: test.adapter
protocol: http
request:
  method: POST
  path: /test
  body: "{}"
response:
  extract:
    output1: "$.result"
`), 0644))

	// Create plans dir with a recipe referencing a nonexistent workflow
	plansDir := filepath.Join(dir, "plans")
	require.NoError(t, os.MkdirAll(plansDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(plansDir, "bad-recipe.yaml"), []byte(`kind: recipe
selection:
  workflow: nonexistent-workflow
`), 0644))

	// Create manifest
	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(`
name: test-project
graph: graph.yaml
templates: templates/
plans:
  - plans/
`), 0644))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifestPath}, &buf)
	output := buf.String()
	assert.Equal(t, 1, code, "output: %s", output)
	assert.Contains(t, output, "Plans")
	assert.Contains(t, output, "FAILED")
	assert.Contains(t, output, "bad-recipe.yaml")
	assert.Contains(t, output, "reconstituting recipe")
}

func TestValidate_WithMixedPlansAndRecipes(t *testing.T) {
	dir := t.TempDir()

	// Create graph with a workflow
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
    outputs:
      - name: reservationId
        type: string
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte(graphContent), 0644))

	// Create templates
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

	// Create workflow template
	workflowsDir := filepath.Join(dir, "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "booking.yaml"), []byte(`execution:
  steps:
    - node: SearchAir
      values:
        origin: ""
    - node: CreateReservation
      dependsOn: [SearchAir]
      values:
        offerId:
          from: SearchAir.offerId
`), 0644))

	// Create plans dir with both a regular plan and a recipe
	plansDir := filepath.Join(dir, "plans")
	require.NoError(t, os.MkdirAll(plansDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(plansDir, "regular.yaml"), []byte(`execution:
  steps:
    - node: SearchAir
      values:
        origin: "LAX"
    - node: CreateReservation
      dependsOn: [SearchAir]
      values:
        offerId:
          from: SearchAir.offerId
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(plansDir, "recipe.yaml"), []byte(`kind: recipe
selection:
  workflow: book-flight
overrides:
  values:
    SearchAir.origin: "JFK"
`), 0644))

	// Create manifest
	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(`
name: test-project
graph: graph.yaml
templates: templates/
workflows: workflows/
plans:
  - plans/
`), 0644))

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifestPath}, &buf)
	output := buf.String()
	assert.Equal(t, 0, code, "output: %s", output)
	assert.Contains(t, output, "Plans")
	assert.Contains(t, output, "(2 files, 1 recipe)")
	assert.Contains(t, output, "PASSED")
}

// TestExamplePetstore_ValidatesStrict keeps the smallest example honest: it
// must pass strict validation, which needs no network.
func TestExamplePetstore_ValidatesStrict(t *testing.T) {
	var out bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: "../../examples/petstore/aat-project.yaml", Strict: true}, &out)
	assert.Equal(t, 0, code, out.String())
}

// TestValidateWorkflows_WalksSubdirectories: slot options and addons usually
// live in subdirectories of workflows/, and must be checked there too.
func TestValidateWorkflows_WalksSubdirectories(t *testing.T) {
	dir := t.TempDir()
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"search": {Name: "search", Adapter: "search"},
	}}
	write := func(rel, content string) string {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		abs, err := filepath.Abs(path)
		require.NoError(t, err)
		return abs
	}
	slot := write("slots/option.yaml", "execution:\n  steps:\n    - node: search\n")
	write("addons/broken.yaml", "execution: [not, a, mapping\n")

	result := validateWorkflows(dir, g, map[string]bool{slot: true})

	assert.Equal(t, 2, result.Total, "files in subdirectories are counted")
	assert.Equal(t, 1, result.Templates, "the referenced slot option is recognized as a template")
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0], filepath.Join("addons", "broken.yaml"))
}

// TestValidate_UnknownKeysNameFileLineAndKey checks that bare aat validate
// reports an unknown key in every kind of project file it loads, each with its
// file (relative to the working directory), line, and suggestion.
func TestValidate_UnknownKeysNameFileLineAndKey(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"aat-project.yaml":             "name: typos\ngraph: graph.yaml\ntemplates: templates/\ndomain: domain.yaml\nvisualizers: visualizers/\nplans: plans/\n",
		"graph.yaml":                   "version: \"1.0.0\"\nnodes:\n  getItem:\n    adapter: getItem\n    inputs:\n      - name: id\n        type: string\n    outputs:\n      - name: name\n        type: string\n",
		"templates/getItem.yaml":       "adapter: getItem\nrequest:\n  method: GET\n  path: /items/{{id}}\nresponse:\n  extract:\n    name: $.name\n",
		"domain.yaml":                  "concepts:\n  item:\n    description: A thing\nvaluePool:\n  ids: [a]\n",
		"visualizers/visualizers.yaml": "visualizers:\n  - id: item\n    file: item.html\n    matches:\n      node: getItem\n",
		"visualizers/item.html":        "<html></html>",
		"plans/smoke.yaml":             "execution:\n  steps:\n    - node: getItem\n      values:\n        id: {fromSelecton: item.id}\n",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	t.Chdir(dir)
	t.Setenv("AAT_PROJECT", "")

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{}, &buf)
	out := buf.String()
	assert.Equal(t, 1, code, out)
	assert.Contains(t, out, `domain.yaml: line 4: unknown key "valuePool" in knowledge base (did you mean "valuePools"?)`)
	assert.Contains(t, out, filepath.Join("visualizers", "visualizers.yaml")+`: line 4: unknown key "matches" in visualizer (did you mean "match"?)`)
	assert.Contains(t, out, filepath.Join("plans", "smoke.yaml")+`: line 5: unknown key "fromSelecton" in step value (did you mean "fromSelection"?)`)
}

func TestValidate_BrokenManifestIsReported(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "aat-project.yaml"),
		[]byte("name: typos\ngraph: graph.yaml\ntemplates: templates/\nplan: plans/\n"), 0o644))
	t.Chdir(dir)
	t.Setenv("AAT_PROJECT", "")

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{}, &buf)
	assert.Equal(t, 1, code)
	assert.Contains(t, buf.String(), `line 4: unknown key "plan" in project manifest (did you mean "plans"?)`)
	assert.NotContains(t, buf.String(), "no manifest found")
}

// TestValidate_WarningsShownWithoutStrict checks that OpenAPI warnings are
// printed and pass without --strict, and fail with it.
func TestValidate_WarningsShownWithoutStrict(t *testing.T) {
	dir := t.TempDir()
	spec, err := os.ReadFile(filepath.Join("testdata", "oas", "optional_params.yaml"))
	require.NoError(t, err)
	files := map[string]string{
		"aat-project.yaml": "name: warn\ngraph: graph.yaml\ntemplates: templates/\n",
		"spec.yaml":        string(spec),
		"graph.yaml": "version: \"1.0.0\"\noas: spec.yaml\nnodes:\n  listWidgets:\n    adapter: listWidgets\n" +
			"    oas: {operationId: listWidgets}\n    outputs:\n      - name: count\n        type: integer\n",
		"templates/listWidgets.yaml": "adapter: listWidgets\nrequest:\n  method: GET\n  path: /widgets\nresponse:\n  extract:\n    count: count\n",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	manifest := filepath.Join(dir, "aat-project.yaml")

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifest}, &buf)
	assert.Equal(t, 0, code, buf.String())
	assert.Regexp(t, `OAS validation:\s+WARN`, buf.String())
	assert.Contains(t, buf.String(), `output "count" not found in OAS 2xx response schema`)
	assert.Contains(t, buf.String(), "PASSED with warnings in 1 section (--strict fails on them)")

	buf.Reset()
	code = validateCommand(&validateArgs{ManifestPath: manifest, Strict: true}, &buf)
	assert.Equal(t, 1, code, buf.String())
	assert.Regexp(t, `OAS validation:\s+FAILED`, buf.String())
}
