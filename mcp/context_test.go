package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testGraph is a minimal valid graph YAML for testing context building.
const testGraph = `
version: "1.0.0"
nodes:
  getUser:
    description: "Get a user by ID"
    adapter: getUser
    inputs:
      - name: userId
        type: string
    outputs:
      - name: user
        type: string
`

// testTemplate is a minimal valid adapter template for testing.
const testTemplate = `
adapter: getUser
request:
  method: GET
  path: /users/{{userId}}
response:
  extract:
    user: "$.name"
`

// testDomain is a minimal valid domain knowledge YAML.
const testDomain = `
concepts:
  user:
    description: "A user entity"
    applies_to:
      - getUser.userId
types:
  userId:
    description: "A user identifier"
    format: "uuid"
`

func setupTestProject(t *testing.T) (string, *ProjectManifest) {
	t.Helper()
	dir := t.TempDir()

	// Write graph
	graphPath := filepath.Join(dir, "graph.yaml")
	require.NoError(t, os.WriteFile(graphPath, []byte(testGraph), 0644))

	// Write template
	tmplDir := filepath.Join(dir, "templates")
	require.NoError(t, os.MkdirAll(tmplDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(tmplDir, "get_user.yaml"),
		[]byte(testTemplate),
		0644,
	))

	manifest := &ProjectManifest{
		Name:          "test-project",
		GraphPath:     graphPath,
		TemplatesPath: tmplDir,
	}

	return dir, manifest
}

func TestBuildServerContext_GraphAndTemplates(t *testing.T) {
	_, manifest := setupTestProject(t)

	ctx, err := BuildServerContext(manifest)
	require.NoError(t, err)

	assert.NotNil(t, ctx.Graph)
	assert.Len(t, ctx.Graph.Nodes, 1)
	assert.Contains(t, ctx.Graph.Nodes, "getUser")

	assert.NotNil(t, ctx.Registry)
	names := ctx.Registry.Names()
	assert.Contains(t, names, "getUser")

	assert.Nil(t, ctx.KB)
	assert.Empty(t, ctx.OASSpecs)
	assert.Nil(t, ctx.Environment)
	assert.Equal(t, manifest, ctx.Manifest)
}

func TestBuildServerContext_WithDomain(t *testing.T) {
	dir, manifest := setupTestProject(t)

	domainPath := filepath.Join(dir, "domain.yaml")
	require.NoError(t, os.WriteFile(domainPath, []byte(testDomain), 0644))
	manifest.DomainPath = domainPath

	ctx, err := BuildServerContext(manifest)
	require.NoError(t, err)

	require.NotNil(t, ctx.KB)
	assert.Contains(t, ctx.KB.Concepts, "user")
	assert.Contains(t, ctx.KB.Types, "userId")
}

func TestBuildServerContext_InvalidGraph(t *testing.T) {
	dir := t.TempDir()

	graphPath := filepath.Join(dir, "graph.yaml")
	require.NoError(t, os.WriteFile(graphPath, []byte("invalid: {{yaml"), 0644))

	tmplDir := filepath.Join(dir, "templates")
	require.NoError(t, os.MkdirAll(tmplDir, 0755))

	manifest := &ProjectManifest{
		GraphPath:     graphPath,
		TemplatesPath: tmplDir,
	}

	_, err := BuildServerContext(manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading graph")
}

func TestBuildServerContext_InvalidTemplates(t *testing.T) {
	dir := t.TempDir()

	graphPath := filepath.Join(dir, "graph.yaml")
	// Write a valid graph so we get past the graph loading step
	require.NoError(t, os.WriteFile(graphPath, []byte(testGraph), 0644))

	manifest := &ProjectManifest{
		GraphPath:     graphPath,
		TemplatesPath: filepath.Join(dir, "nonexistent"),
	}

	_, err := BuildServerContext(manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading templates")
}

func TestBuildServerContext_InvalidDomain(t *testing.T) {
	_, manifest := setupTestProject(t)

	manifest.DomainPath = filepath.Join(t.TempDir(), "nonexistent", "domain.yaml")

	_, err := BuildServerContext(manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading domain")
}

func TestBuildServerContext_OptionalDirs(t *testing.T) {
	_, manifest := setupTestProject(t)

	manifest.DocsDir = "/some/docs"
	manifest.PlansDir = "/some/plans"
	manifest.ArchiveDir = "/some/archives"

	ctx, err := BuildServerContext(manifest)
	require.NoError(t, err)

	assert.Equal(t, "/some/docs", ctx.DocsDir)
	assert.Equal(t, "/some/plans", ctx.PlansDir)
	assert.Equal(t, "/some/archives", ctx.ArchiveDir)
}

func TestBuildServerContext_WithDocsDir(t *testing.T) {
	dir, manifest := setupTestProject(t)

	docsDir := filepath.Join(dir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "getUser.md"), []byte("# getUser\nUser docs"), 0644))

	manifest.DocsDir = docsDir

	ctx, err := BuildServerContext(manifest)
	require.NoError(t, err)

	assert.Equal(t, docsDir, ctx.DocsDir)
	require.NotNil(t, ctx.NodeDocs)
	assert.Len(t, ctx.NodeDocs, 1)
	assert.Equal(t, "# getUser\nUser docs", ctx.NodeDocs["getUser"])
}

func TestBuildServerContext_DocsDirNonExistent(t *testing.T) {
	_, manifest := setupTestProject(t)

	manifest.DocsDir = filepath.Join(t.TempDir(), "nonexistent")

	ctx, err := BuildServerContext(manifest)
	require.NoError(t, err)

	assert.Equal(t, manifest.DocsDir, ctx.DocsDir)
	assert.Empty(t, ctx.NodeDocs)
}

func TestCollectSpecPaths_NoSpecs(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"a": {Name: "a"},
		},
	}

	paths := collectSpecPaths(g, "/base")
	assert.Empty(t, paths)
}

func TestCollectSpecPaths_GraphLevel(t *testing.T) {
	g := &graph.Graph{
		OAS: "spec.yaml",
		Nodes: map[string]*graph.Node{
			"a": {Name: "a"},
		},
	}

	paths := collectSpecPaths(g, "/base")
	assert.Equal(t, []string{"/base/spec.yaml"}, paths)
}

func TestCollectSpecPaths_NodeLevel(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"a": {
				Name: "a",
				OAS:  &graph.OASRef{OperationID: "op1", Spec: "node-spec.yaml"},
			},
		},
	}

	paths := collectSpecPaths(g, "/base")
	assert.Equal(t, []string{"/base/node-spec.yaml"}, paths)
}

func TestCollectSpecPaths_Deduplication(t *testing.T) {
	g := &graph.Graph{
		OAS: "spec.yaml",
		Nodes: map[string]*graph.Node{
			"a": {
				Name: "a",
				OAS:  &graph.OASRef{OperationID: "op1", Spec: "spec.yaml"},
			},
			"b": {
				Name: "b",
				OAS:  &graph.OASRef{OperationID: "op2"},
			},
		},
	}

	paths := collectSpecPaths(g, "/base")
	// "spec.yaml" should appear only once
	assert.Len(t, paths, 1)
	assert.Equal(t, "/base/spec.yaml", paths[0])
}

func TestCollectSpecPaths_AbsoluteSpec(t *testing.T) {
	g := &graph.Graph{
		OAS: "/absolute/spec.yaml",
		Nodes: map[string]*graph.Node{
			"a": {Name: "a"},
		},
	}

	paths := collectSpecPaths(g, "/base")
	assert.Equal(t, []string{"/absolute/spec.yaml"}, paths)
}
