package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- loadNodeDocs ---

func TestLoadNodeDocs_ValidDir(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {Name: "search"},
			"book":   {Name: "book"},
		},
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "search.md"), []byte("# Search\nSearch docs"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "book.md"), []byte("# Book\nBook docs"), 0644))

	docs, err := loadNodeDocs(dir, g)
	require.NoError(t, err)
	assert.Len(t, docs, 2)
	assert.Equal(t, "# Search\nSearch docs", docs["search"])
	assert.Equal(t, "# Book\nBook docs", docs["book"])
}

func TestLoadNodeDocs_PartialCoverage(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {Name: "search"},
			"book":   {Name: "book"},
		},
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "search.md"), []byte("search doc"), 0644))

	docs, err := loadNodeDocs(dir, g)
	require.NoError(t, err)
	assert.Len(t, docs, 1)
	assert.Contains(t, docs, "search")
	assert.NotContains(t, docs, "book")
}

func TestLoadNodeDocs_NonMatchingFilesIgnored(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {Name: "search"},
		},
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "search.md"), []byte("doc"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unknown.md"), []byte("ignored"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not md"), 0644))

	docs, err := loadNodeDocs(dir, g)
	require.NoError(t, err)
	assert.Len(t, docs, 1)
	assert.Contains(t, docs, "search")
}

func TestLoadNodeDocs_NonExistentDir(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {Name: "search"},
		},
	}

	docs, err := loadNodeDocs(filepath.Join(t.TempDir(), "nonexistent"), g)
	require.NoError(t, err)
	assert.Empty(t, docs)
}

func TestLoadNodeDocs_EmptyDir(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {Name: "search"},
		},
	}
	dir := t.TempDir()

	docs, err := loadNodeDocs(dir, g)
	require.NoError(t, err)
	assert.Empty(t, docs)
}

func TestLoadNodeDocs_CaseSensitiveMatching(t *testing.T) {
	// Node names with different casing should only match exact file names.
	// On case-insensitive filesystems (macOS), we test that the code uses
	// the file-system entry name for matching, not a case-folded lookup.
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"SearchFlights": {Name: "SearchFlights"},
			"searchflights": {Name: "searchflights"},
		},
	}
	dir := t.TempDir()
	// Only write one cased file — the code matches against the entry name
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SearchFlights.md"), []byte("doc content"), 0644))

	docs, err := loadNodeDocs(dir, g)
	require.NoError(t, err)
	// At minimum, the PascalCase node should match
	assert.Contains(t, docs, "SearchFlights")
	assert.Equal(t, "doc content", docs["SearchFlights"])
}

// --- mergeNodeDoc ---

func TestMergeNodeDoc_Full(t *testing.T) {
	node := &graph.Node{
		Name:        "search",
		Description: "Search flights",
		Inputs:      []graph.Input{{Name: "origin", Type: "string"}},
		Outputs:     []graph.Output{{Name: "results", Type: "result[]"}},
	}
	g := &graph.Graph{Nodes: map[string]*graph.Node{"search": node}}

	result := mergeNodeDoc(node, g, "Custom documentation content.", "GET /api/search\n")

	assert.Contains(t, result, "# search")
	assert.Contains(t, result, "Search flights")
	assert.Contains(t, result, "## Documentation")
	assert.Contains(t, result, "Custom documentation content.")
	assert.Contains(t, result, "## API Specification")
	assert.Contains(t, result, "GET /api/search")
}

func TestMergeNodeDoc_NoDocContent(t *testing.T) {
	node := &graph.Node{Name: "search"}
	g := &graph.Graph{Nodes: map[string]*graph.Node{"search": node}}

	result := mergeNodeDoc(node, g, "", "GET /api/search\n")

	assert.Contains(t, result, "# search")
	assert.NotContains(t, result, "## Documentation")
	assert.Contains(t, result, "## API Specification")
}

func TestMergeNodeDoc_NoOAS(t *testing.T) {
	node := &graph.Node{Name: "search"}
	g := &graph.Graph{Nodes: map[string]*graph.Node{"search": node}}

	result := mergeNodeDoc(node, g, "Some docs.", "")

	assert.Contains(t, result, "# search")
	assert.Contains(t, result, "## Documentation")
	assert.NotContains(t, result, "## API Specification")
}

func TestMergeNodeDoc_NothingExtra(t *testing.T) {
	node := &graph.Node{Name: "search"}
	g := &graph.Graph{Nodes: map[string]*graph.Node{"search": node}}

	result := mergeNodeDoc(node, g, "", "")

	assert.Contains(t, result, "# search")
	assert.NotContains(t, result, "## Documentation")
	assert.NotContains(t, result, "## API Specification")
}

// --- generateDocStub ---

func TestGenerateDocStub_WithInputsOutputs(t *testing.T) {
	node := &graph.Node{
		Name:        "search",
		Description: "Search for flights",
		Inputs:      []graph.Input{{Name: "origin", Type: "string"}, {Name: "dest", Type: "string"}},
		Outputs:     []graph.Output{{Name: "results", Type: "result[]"}},
	}
	g := &graph.Graph{Nodes: map[string]*graph.Node{"search": node}}

	result := generateDocStub(node, g, "")

	assert.Contains(t, result, "# search")
	assert.Contains(t, result, "Search for flights")
	assert.Contains(t, result, "## Inputs")
	assert.Contains(t, result, "origin")
	assert.Contains(t, result, "## Outputs")
	assert.Contains(t, result, "results")
	assert.Contains(t, result, "## Overview")
	assert.Contains(t, result, "## Usage Notes")
	assert.Contains(t, result, "## Examples")
}

func TestGenerateDocStub_WithOAS(t *testing.T) {
	node := &graph.Node{Name: "search", Description: "Search"}
	g := &graph.Graph{Nodes: map[string]*graph.Node{"search": node}}

	result := generateDocStub(node, g, "GET /api/search\nReturns search results.\n")

	assert.Contains(t, result, "## API Details")
	assert.Contains(t, result, "GET /api/search")
}

func TestGenerateDocStub_MinimalNode(t *testing.T) {
	node := &graph.Node{Name: "minimal"}
	g := &graph.Graph{Nodes: map[string]*graph.Node{"minimal": node}}

	result := generateDocStub(node, g, "")

	assert.Contains(t, result, "# minimal")
	assert.Contains(t, result, "_TODO: Add a description")
	assert.NotContains(t, result, "## Inputs")
	assert.NotContains(t, result, "## Outputs")
	assert.NotContains(t, result, "## API Details")
	assert.Contains(t, result, "## Overview")
	assert.Contains(t, result, "## Usage Notes")
	assert.Contains(t, result, "## Examples")
}
