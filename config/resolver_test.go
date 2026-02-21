package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeManifest(t *testing.T, dir string) string {
	t.Helper()
	content := `name: test
graph: graph.yaml
templates: templates/
domain: domain.yaml
environment: env.yaml
workflows: workflows/
archives: runs/
traces: traces/
`
	path := filepath.Join(dir, "aat-project.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestResolveProjectPaths_AllExplicit(t *testing.T) {
	overrides := ProjectPaths{
		GraphPath:     "/explicit/graph.yaml",
		TemplatesPath: "/explicit/templates/",
		EnvPath:       "/explicit/env.yaml",
		DomainPath:    "/explicit/domain.yaml",
	}

	result, err := ResolveProjectPaths(overrides)
	require.NoError(t, err)
	assert.Equal(t, "/explicit/graph.yaml", result.GraphPath)
	assert.Equal(t, "/explicit/templates/", result.TemplatesPath)
	assert.Equal(t, "/explicit/env.yaml", result.EnvPath)
	assert.Equal(t, "/explicit/domain.yaml", result.DomainPath)
}

func TestResolveProjectPaths_CWDManifest(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	writeManifest(t, dir)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	result, err := ResolveProjectPaths(ProjectPaths{})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "graph.yaml"), result.GraphPath)
	assert.Equal(t, filepath.Join(dir, "templates"), result.TemplatesPath)
	assert.Equal(t, filepath.Join(dir, "env.yaml"), result.EnvPath)
	assert.Equal(t, filepath.Join(dir, "domain.yaml"), result.DomainPath)
	assert.Equal(t, filepath.Join(dir, "workflows"), result.WorkflowsDir)
	assert.Equal(t, filepath.Join(dir, "runs"), result.ArchiveDir)
	assert.Equal(t, filepath.Join(dir, "traces"), result.TracesDir)
	assert.Equal(t, filepath.Join(dir, "aat-project.yaml"), result.ManifestPath)
}

func TestResolveProjectPaths_EnvVar(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	writeManifest(t, dir)

	// chdir away from the manifest so CWD discovery doesn't find it
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	tmpEmpty := t.TempDir()
	require.NoError(t, os.Chdir(tmpEmpty))

	t.Setenv("AAT_PROJECT", dir)

	result, err := ResolveProjectPaths(ProjectPaths{})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "graph.yaml"), result.GraphPath)
	assert.Equal(t, filepath.Join(dir, "templates"), result.TemplatesPath)
	assert.Equal(t, filepath.Join(dir, "env.yaml"), result.EnvPath)
}

func TestResolveProjectPaths_EnvVarAsFile(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	manifestPath := writeManifest(t, dir)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	tmpEmpty := t.TempDir()
	require.NoError(t, os.Chdir(tmpEmpty))

	t.Setenv("AAT_PROJECT", manifestPath)

	result, err := ResolveProjectPaths(ProjectPaths{})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "graph.yaml"), result.GraphPath)
	assert.Equal(t, manifestPath, result.ManifestPath)
}

func TestResolveProjectPaths_ExplicitOverridesManifest(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	writeManifest(t, dir)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	overrides := ProjectPaths{
		GraphPath: "/override/graph.yaml",
	}

	result, err := ResolveProjectPaths(overrides)
	require.NoError(t, err)
	// Explicit override wins
	assert.Equal(t, "/override/graph.yaml", result.GraphPath)
	// Other fields still from manifest
	assert.Equal(t, filepath.Join(dir, "templates"), result.TemplatesPath)
	assert.Equal(t, filepath.Join(dir, "env.yaml"), result.EnvPath)
}

func TestResolveProjectPaths_ExplicitManifest(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	writeManifest(t, dir)

	// chdir away so CWD discovery doesn't find it
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	tmpEmpty := t.TempDir()
	require.NoError(t, os.Chdir(tmpEmpty))
	t.Setenv("AAT_PROJECT", "")

	result, err := ResolveProjectPaths(ProjectPaths{
		ExplicitManifest: dir,
	})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "graph.yaml"), result.GraphPath)
	assert.Equal(t, filepath.Join(dir, "templates"), result.TemplatesPath)
	assert.Equal(t, filepath.Join(dir, "env.yaml"), result.EnvPath)
	assert.Equal(t, filepath.Join(dir, "domain.yaml"), result.DomainPath)
	assert.Equal(t, filepath.Join(dir, "workflows"), result.WorkflowsDir)
	assert.Equal(t, filepath.Join(dir, "runs"), result.ArchiveDir)
	assert.Equal(t, filepath.Join(dir, "traces"), result.TracesDir)
	assert.Equal(t, filepath.Join(dir, "aat-project.yaml"), result.ManifestPath)
}

func TestResolveProjectPaths_ExplicitManifestOverriddenByFields(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	writeManifest(t, dir)

	// chdir away so CWD discovery doesn't find it
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	tmpEmpty := t.TempDir()
	require.NoError(t, os.Chdir(tmpEmpty))
	t.Setenv("AAT_PROJECT", "")

	result, err := ResolveProjectPaths(ProjectPaths{
		ExplicitManifest: dir,
		GraphPath:        "/override/graph.yaml",
	})
	require.NoError(t, err)
	// Explicit field override wins over manifest
	assert.Equal(t, "/override/graph.yaml", result.GraphPath)
	// Other fields still from manifest
	assert.Equal(t, filepath.Join(dir, "templates"), result.TemplatesPath)
	assert.Equal(t, filepath.Join(dir, "env.yaml"), result.EnvPath)
}

func TestResolveProjectPaths_ExplicitManifestOverridesCWD(t *testing.T) {
	// Create two manifest directories
	cwdDir := t.TempDir()
	cwdDir, err := filepath.EvalSymlinks(cwdDir)
	require.NoError(t, err)
	writeManifest(t, cwdDir)

	explicitDir := t.TempDir()
	explicitDir, err = filepath.EvalSymlinks(explicitDir)
	require.NoError(t, err)
	// Write a different manifest in explicit dir
	content := `name: explicit
graph: explicit-graph.yaml
templates: explicit-templates/
environment: explicit-env.yaml
`
	require.NoError(t, os.WriteFile(filepath.Join(explicitDir, "aat-project.yaml"), []byte(content), 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(cwdDir))
	t.Setenv("AAT_PROJECT", "")

	result, err := ResolveProjectPaths(ProjectPaths{
		ExplicitManifest: explicitDir,
	})
	require.NoError(t, err)
	// Explicit manifest wins over CWD manifest
	assert.Equal(t, filepath.Join(explicitDir, "explicit-graph.yaml"), result.GraphPath)
	assert.Equal(t, filepath.Join(explicitDir, "explicit-templates"), result.TemplatesPath)
	assert.Equal(t, filepath.Join(explicitDir, "explicit-env.yaml"), result.EnvPath)
}

func TestResolveProjectPaths_NothingFound(t *testing.T) {
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	tmpEmpty := t.TempDir()
	require.NoError(t, os.Chdir(tmpEmpty))

	// Clear env var
	t.Setenv("AAT_PROJECT", "")

	result, err := ResolveProjectPaths(ProjectPaths{})
	require.NoError(t, err)
	assert.Empty(t, result.GraphPath)
	assert.Empty(t, result.TemplatesPath)
	assert.Empty(t, result.EnvPath)
	assert.Empty(t, result.ManifestPath)
}
