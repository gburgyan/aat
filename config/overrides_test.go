package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindAutoOverrides_InCurrentDir(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	overridePath := filepath.Join(dir, AutoOverridesFile)
	require.NoError(t, os.WriteFile(overridePath, []byte("overrides:\n  - match: test\n    baseUrl: http://localhost:8080\n"), 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()

	require.NoError(t, os.Chdir(dir))

	found := FindAutoOverrides()
	assert.Equal(t, overridePath, found)
}

func TestFindAutoOverrides_InParentDir(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	overridePath := filepath.Join(dir, AutoOverridesFile)
	require.NoError(t, os.WriteFile(overridePath, []byte("overrides:\n  - match: test\n    baseUrl: http://localhost:8080\n"), 0644))

	subDir := filepath.Join(dir, "sub", "deep")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()

	require.NoError(t, os.Chdir(subDir))

	found := FindAutoOverrides()
	assert.Equal(t, overridePath, found)
}

func TestFindAutoOverrides_NotFound(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()

	require.NoError(t, os.Chdir(dir))

	found := FindAutoOverrides()
	assert.Empty(t, found)
}

func TestFindAutoOverrides_ReturnsNearest(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	// Create override in parent
	parentOverride := filepath.Join(dir, AutoOverridesFile)
	require.NoError(t, os.WriteFile(parentOverride, []byte("overrides:\n  - match: parent\n    baseUrl: http://parent:8080\n"), 0644))

	// Create override in child
	childDir := filepath.Join(dir, "child")
	require.NoError(t, os.MkdirAll(childDir, 0755))
	childOverride := filepath.Join(childDir, AutoOverridesFile)
	require.NoError(t, os.WriteFile(childOverride, []byte("overrides:\n  - match: child\n    baseUrl: http://child:8080\n"), 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()

	require.NoError(t, os.Chdir(childDir))

	found := FindAutoOverrides()
	assert.Equal(t, childOverride, found)
}

func TestFindAutoOverrides_NoDotFile(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	overridePath := filepath.Join(dir, AutoOverridesFileNoDot)
	require.NoError(t, os.WriteFile(overridePath, []byte("overrides:\n  - match: test\n    baseUrl: http://localhost:8080\n"), 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()

	require.NoError(t, os.Chdir(dir))

	found := FindAutoOverrides()
	assert.Equal(t, overridePath, found)
}

func TestFindAutoOverrides_DotFileWinsOverNoDot(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	dotPath := filepath.Join(dir, AutoOverridesFile)
	noDotPath := filepath.Join(dir, AutoOverridesFileNoDot)
	require.NoError(t, os.WriteFile(dotPath, []byte("overrides:\n  - match: dot\n    baseUrl: http://dot:8080\n"), 0644))
	require.NoError(t, os.WriteFile(noDotPath, []byte("overrides:\n  - match: nodot\n    baseUrl: http://nodot:8080\n"), 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()

	require.NoError(t, os.Chdir(dir))

	found := FindAutoOverrides()
	assert.Equal(t, dotPath, found, "dotfile should take precedence")
}

func TestFindAutoOverrides_NoDotInParent(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	overridePath := filepath.Join(dir, AutoOverridesFileNoDot)
	require.NoError(t, os.WriteFile(overridePath, []byte("overrides:\n  - match: test\n    baseUrl: http://localhost:8080\n"), 0644))

	subDir := filepath.Join(dir, "sub", "deep")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()

	require.NoError(t, os.Chdir(subDir))

	found := FindAutoOverrides()
	assert.Equal(t, overridePath, found)
}
