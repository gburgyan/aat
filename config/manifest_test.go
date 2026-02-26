package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadManifest_Valid(t *testing.T) {
	dir := t.TempDir()
	content := `
name: test-project
description: A test project
tags: [api, testing]
graph: graph.yaml
templates: templates/
domain: domain.yaml
docs: docs/
workflows: workflows/
archives: runs/
traces: traces/
environment: env.yaml
`
	path := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	m, err := LoadManifest(path)
	require.NoError(t, err)

	assert.Equal(t, "test-project", m.Name)
	assert.Equal(t, "A test project", m.Description)
	assert.Equal(t, []string{"api", "testing"}, m.Tags)

	// Paths should be resolved relative to manifest dir
	assert.Equal(t, filepath.Join(dir, "graph.yaml"), m.GraphPath)
	assert.Equal(t, filepath.Join(dir, "templates"), m.TemplatesPath)
	assert.Equal(t, filepath.Join(dir, "domain.yaml"), m.DomainPath)
	assert.Equal(t, filepath.Join(dir, "docs"), m.DocsDir)
	assert.Equal(t, filepath.Join(dir, "workflows"), m.WorkflowsDir)
	assert.Equal(t, filepath.Join(dir, "runs"), m.ArchiveDir)
	assert.Equal(t, filepath.Join(dir, "traces"), m.TracesDir)
	assert.Equal(t, filepath.Join(dir, "env.yaml"), m.EnvPath)
}

func TestLoadManifest_MinimalValid(t *testing.T) {
	dir := t.TempDir()
	content := `
graph: graph.yaml
templates: templates/
`
	path := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	m, err := LoadManifest(path)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "graph.yaml"), m.GraphPath)
	assert.Equal(t, filepath.Join(dir, "templates"), m.TemplatesPath)
	assert.Empty(t, m.DomainPath)
	assert.Empty(t, m.DocsDir)
	assert.Empty(t, m.WorkflowsDir)
	assert.Empty(t, m.ArchiveDir)
	assert.Empty(t, m.TracesDir)
	assert.Empty(t, m.EnvPath)
}

func TestLoadManifest_MissingGraph(t *testing.T) {
	dir := t.TempDir()
	content := `
templates: templates/
`
	path := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	_, err := LoadManifest(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "graph")
}

func TestLoadManifest_MissingTemplates(t *testing.T) {
	dir := t.TempDir()
	content := `
graph: graph.yaml
`
	path := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	_, err := LoadManifest(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "templates")
}

func TestLoadManifest_AbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	content := `
graph: /absolute/path/graph.yaml
templates: /absolute/path/templates/
`
	path := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	m, err := LoadManifest(path)
	require.NoError(t, err)

	// Absolute paths should remain unchanged
	assert.Equal(t, "/absolute/path/graph.yaml", m.GraphPath)
	assert.Equal(t, "/absolute/path/templates/", m.TemplatesPath)
}

func TestLoadManifest_FileNotFound(t *testing.T) {
	_, err := LoadManifest("/nonexistent/aat-project.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading manifest")
}

func TestLoadManifest_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(path, []byte("{{invalid"), 0644))

	_, err := LoadManifest(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing manifest")
}

func TestFindManifest_Found(t *testing.T) {
	// Create a temp dir with aat-project.yaml
	dir := t.TempDir()
	// Resolve symlinks (macOS: /var -> /private/var) so paths match os.Getwd()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte("graph: g.yaml\ntemplates: t/\n"), 0644))

	// Create a subdirectory and chdir into it
	subDir := filepath.Join(dir, "sub", "deep")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()

	require.NoError(t, os.Chdir(subDir))

	found, err := FindManifest()
	require.NoError(t, err)
	assert.Equal(t, manifestPath, found)
}

func TestFindManifest_InCurrentDir(t *testing.T) {
	dir := t.TempDir()
	// Resolve symlinks (macOS: /var -> /private/var) so paths match os.Getwd()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	manifestPath := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte("graph: g.yaml\ntemplates: t/\n"), 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()

	require.NoError(t, os.Chdir(dir))

	found, err := FindManifest()
	require.NoError(t, err)
	assert.Equal(t, manifestPath, found)
}

func TestLoadManifest_PlanDirsScalar(t *testing.T) {
	dir := t.TempDir()
	content := `
graph: graph.yaml
templates: templates/
plans: plans/
`
	path := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	m, err := LoadManifest(path)
	require.NoError(t, err)

	require.Len(t, m.PlanDirs, 1)
	assert.Equal(t, filepath.Join(dir, "plans"), m.PlanDirs[0])
}

func TestLoadManifest_PlanDirsList(t *testing.T) {
	dir := t.TempDir()
	content := `
graph: graph.yaml
templates: templates/
plans:
  - plans/
  - /ext/plans
`
	path := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	m, err := LoadManifest(path)
	require.NoError(t, err)

	require.Len(t, m.PlanDirs, 2)
	assert.Equal(t, filepath.Join(dir, "plans"), m.PlanDirs[0])
	assert.Equal(t, "/ext/plans", m.PlanDirs[1]) // absolute path unchanged
}

func TestLoadManifest_PlanDirsOmitted(t *testing.T) {
	dir := t.TempDir()
	content := `
graph: graph.yaml
templates: templates/
`
	path := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	m, err := LoadManifest(path)
	require.NoError(t, err)

	assert.Empty(t, m.PlanDirs)
}

func TestLoadManifest_OASPathsScalar(t *testing.T) {
	dir := t.TempDir()
	content := `
graph: graph.yaml
templates: templates/
oas: spec.yaml
`
	path := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	m, err := LoadManifest(path)
	require.NoError(t, err)

	require.Len(t, m.OASPaths, 1)
	assert.Equal(t, filepath.Join(dir, "spec.yaml"), m.OASPaths[0])
}

func TestLoadManifest_OASPathsList(t *testing.T) {
	dir := t.TempDir()
	content := `
graph: graph.yaml
templates: templates/
oas:
  - spec.yaml
  - /absolute/other-spec.json
`
	path := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	m, err := LoadManifest(path)
	require.NoError(t, err)

	require.Len(t, m.OASPaths, 2)
	assert.Equal(t, filepath.Join(dir, "spec.yaml"), m.OASPaths[0])
	assert.Equal(t, "/absolute/other-spec.json", m.OASPaths[1]) // absolute unchanged
}

func TestLoadManifest_OASPathsOmitted(t *testing.T) {
	dir := t.TempDir()
	content := `
graph: graph.yaml
templates: templates/
`
	path := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	m, err := LoadManifest(path)
	require.NoError(t, err)

	assert.Empty(t, m.OASPaths)
}

func TestResolvePath(t *testing.T) {
	tests := []struct {
		name     string
		baseDir  string
		path     string
		expected string
	}{
		{
			name:     "relative path",
			baseDir:  "/project",
			path:     "graph.yaml",
			expected: "/project/graph.yaml",
		},
		{
			name:     "nested relative path",
			baseDir:  "/project",
			path:     "sub/graph.yaml",
			expected: "/project/sub/graph.yaml",
		},
		{
			name:     "absolute path unchanged",
			baseDir:  "/project",
			path:     "/other/graph.yaml",
			expected: "/other/graph.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolvePath(tt.baseDir, tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}
