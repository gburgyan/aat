package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowValidate_MissingGraphFlag(t *testing.T) {
	var buf bytes.Buffer
	code := workflowValidateCommand(&workflowValidateArgs{}, &buf)
	assert.Equal(t, 1, code)
}

func TestWorkflowValidate_NoWorkflows(t *testing.T) {
	// test_graph.yaml has no workflows
	var buf bytes.Buffer
	code := workflowValidateCommand(&workflowValidateArgs{
		GraphPath: "testdata/test_graph.yaml",
	}, &buf)
	assert.Equal(t, 0, code)
	assert.Contains(t, buf.String(), "No workflows found")
}

func TestWorkflowValidate_ValidWorkflows(t *testing.T) {
	dir := t.TempDir()

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

	workflowsDir := filepath.Join(dir, "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
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

	var buf bytes.Buffer
	code := workflowValidateCommand(&workflowValidateArgs{
		GraphPath:    filepath.Join(dir, "graph.yaml"),
		WorkflowsDir: workflowsDir,
	}, &buf)
	assert.Equal(t, 0, code, "output: %s", buf.String())
	assert.Contains(t, buf.String(), "Workflow compatibility")
	assert.Contains(t, buf.String(), "OK")
}

func TestWorkflowValidate_InvalidWorkflowFile(t *testing.T) {
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte(graphContent), 0644))

	workflowsDir := filepath.Join(dir, "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "bad.yaml"), []byte(`execution:
  steps:
    - node: NonexistentNode
      values:
        foo: "bar"
`), 0644))

	var buf bytes.Buffer
	code := workflowValidateCommand(&workflowValidateArgs{
		GraphPath:    filepath.Join(dir, "graph.yaml"),
		WorkflowsDir: workflowsDir,
	}, &buf)
	assert.Equal(t, 1, code, "output: %s", buf.String())
	assert.Contains(t, buf.String(), "FAILED")
}

func TestWorkflowValidate_TemplateSkipsGraphValidation(t *testing.T) {
	dir := t.TempDir()

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

	var buf bytes.Buffer
	code := workflowValidateCommand(&workflowValidateArgs{
		GraphPath:    filepath.Join(dir, "graph.yaml"),
		WorkflowsDir: workflowsDir,
	}, &buf)
	output := buf.String()
	assert.Equal(t, 0, code, "output: %s", output)
	assert.Contains(t, output, "Workflows")
	assert.Contains(t, output, "2 templates")
}

func TestWorkflowValidate_Strict(t *testing.T) {
	// A graph with workflows but no issues — strict should still pass
	dir := t.TempDir()

	graphContent := `version: "1.0.0"
workflows:
  - name: simple
    description: A simple workflow
    template: workflows/simple.yaml
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

	workflowsDir := filepath.Join(dir, "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "simple.yaml"), []byte(`execution:
  steps:
    - node: testNode
      values:
        input1: "hello"
`), 0644))

	var buf bytes.Buffer
	code := workflowValidateCommand(&workflowValidateArgs{
		GraphPath:    filepath.Join(dir, "graph.yaml"),
		WorkflowsDir: workflowsDir,
		Strict:       true,
	}, &buf)
	assert.Equal(t, 0, code, "output: %s", buf.String())
	assert.Contains(t, buf.String(), "OK")
}
