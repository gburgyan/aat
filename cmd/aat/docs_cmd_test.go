package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocsGenerateCommand_SingleFile(t *testing.T) {
	tmp := t.TempDir()
	outputPath := filepath.Join(tmp, "workflow.md")

	args := &docsArgs{
		GraphPath:   "../../graph/testdata/valid/travelport_booking.yaml",
		NodeDocsDir: filepath.Join(tmp, "nonexistent-docs"),
		OutputPath:  outputPath,
	}

	err := docsGenerateCommand(args)
	require.NoError(t, err)

	content, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	assert.Contains(t, string(content), "# API Workflow")
	assert.Contains(t, string(content), "7 nodes")
	assert.Contains(t, string(content), "```mermaid")
	assert.Contains(t, string(content), "### searchFlights")
}

func TestDocsGenerateCommand_Stdout(t *testing.T) {
	args := &docsArgs{
		GraphPath:   "../../graph/testdata/valid/travelport_booking.yaml",
		NodeDocsDir: "nonexistent-dir",
		OutputPath:  "-",
	}

	// Redirect stdout to capture output
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := docsGenerateCommand(args)
	require.NoError(t, err)

	_ = w.Close()
	os.Stdout = old

	buf := make([]byte, 64*1024)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	assert.Contains(t, output, "# API Workflow")
}

func TestDocsGenerateCommand_WithDomain(t *testing.T) {
	tmp := t.TempDir()
	outputPath := filepath.Join(tmp, "workflow.md")

	args := &docsArgs{
		GraphPath:   "../../graph/testdata/valid/travelport_booking.yaml",
		DomainPath:  "../../domain/testdata/valid/travel.yaml",
		NodeDocsDir: filepath.Join(tmp, "nonexistent-docs"),
		OutputPath:  outputPath,
	}

	err := docsGenerateCommand(args)
	require.NoError(t, err)

	content, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	// Domain enrichment should populate Examples column
	assert.Contains(t, string(content), "| Examples |")
}

func TestDocsGenerateCommand_SplitMode(t *testing.T) {
	tmp := t.TempDir()
	outputDir := filepath.Join(tmp, "docs")

	args := &docsArgs{
		GraphPath:   "../../graph/testdata/valid/travelport_booking.yaml",
		NodeDocsDir: filepath.Join(tmp, "nonexistent-docs"),
		OutputPath:  outputDir,
		Split:       true,
	}

	err := docsGenerateCommand(args)
	require.NoError(t, err)

	// Check index.md exists
	indexContent, err := os.ReadFile(filepath.Join(outputDir, "index.md"))
	require.NoError(t, err)
	assert.Contains(t, string(indexContent), "# API Workflow")

	// Check per-node files exist
	for _, name := range []string{"searchFlights", "priceOffer", "createWorkbench", "addOffer", "addTraveler", "commitBooking", "ignoreWorkbench"} {
		nodePath := filepath.Join(outputDir, "nodes", name+".md")
		_, err := os.Stat(nodePath)
		assert.NoError(t, err, "missing node file: %s", name)
	}
}

func TestDocsGenerateCommand_SplitDefaultDir(t *testing.T) {
	tmp := t.TempDir()
	// When split is used with default output (workflow.md), it should use docs/api
	outputDir := filepath.Join(tmp, "docs", "api")

	args := &docsArgs{
		GraphPath:   "../../graph/testdata/valid/travelport_booking.yaml",
		NodeDocsDir: filepath.Join(tmp, "nonexistent-docs"),
		OutputPath:  outputDir,
		Split:       true,
	}

	err := docsGenerateCommand(args)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(outputDir, "index.md"))
	assert.NoError(t, err)
}

func TestDocsGenerateCommand_WithNodeDocs(t *testing.T) {
	tmp := t.TempDir()
	outputPath := filepath.Join(tmp, "workflow.md")

	// Create a node docs directory with a file
	nodeDocsDir := filepath.Join(tmp, "nodedocs")
	require.NoError(t, os.MkdirAll(nodeDocsDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(nodeDocsDir, "searchFlights.md"),
		[]byte("Custom docs for searchFlights node."),
		0644,
	))

	args := &docsArgs{
		GraphPath:   "../../graph/testdata/valid/travelport_booking.yaml",
		NodeDocsDir: nodeDocsDir,
		OutputPath:  outputPath,
	}

	err := docsGenerateCommand(args)
	require.NoError(t, err)

	content, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	assert.Contains(t, string(content), "Custom docs for searchFlights node.")
}

func TestDocsGenerateCommand_InvalidGraph(t *testing.T) {
	args := &docsArgs{
		GraphPath:   "nonexistent.yaml",
		NodeDocsDir: "nonexistent-dir",
		OutputPath:  "out.md",
	}

	err := docsGenerateCommand(args)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "loading graph")
}

func TestDocsGenerateCommand_InvalidDomain(t *testing.T) {
	args := &docsArgs{
		GraphPath:   "../../graph/testdata/valid/minimal.yaml",
		DomainPath:  "nonexistent-domain.yaml",
		NodeDocsDir: "nonexistent-dir",
		OutputPath:  "out.md",
	}

	err := docsGenerateCommand(args)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "loading domain")
}

func TestDocsGenerateCommand_CustomTitle(t *testing.T) {
	tmp := t.TempDir()
	outputPath := filepath.Join(tmp, "workflow.md")

	args := &docsArgs{
		GraphPath:   "../../graph/testdata/valid/travelport_booking.yaml",
		NodeDocsDir: filepath.Join(tmp, "nonexistent-docs"),
		OutputPath:  outputPath,
		Title:       "Travelport Booking Workflow",
	}

	err := docsGenerateCommand(args)
	require.NoError(t, err)

	content, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	assert.Contains(t, string(content), "# Travelport Booking Workflow")
}

func TestBuildExamples_NilKB(t *testing.T) {
	g, err := graph.ParseFile("../../graph/testdata/valid/minimal.yaml")
	require.NoError(t, err)
	result := buildExamples(g, nil)
	assert.Nil(t, result)
}
