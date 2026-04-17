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

func TestPeekOverlayEnvironment_Present(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overlay.yaml")
	require.NoError(t, os.WriteFile(path, []byte("environment: dev\noverrides:\n  - match: '*'\n    baseUrl: http://localhost:8080\n"), 0644))

	env, err := PeekOverlayEnvironment(path)
	require.NoError(t, err)
	assert.Equal(t, "dev", env)
}

func TestPeekOverlayEnvironment_Absent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overlay.yaml")
	require.NoError(t, os.WriteFile(path, []byte("overrides:\n  - match: '*'\n    baseUrl: http://localhost:8080\n"), 0644))

	env, err := PeekOverlayEnvironment(path)
	require.NoError(t, err)
	assert.Empty(t, env)
}

func TestPeekOverlayEnvironment_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overlay.yaml")
	require.NoError(t, os.WriteFile(path, []byte(""), 0644))

	env, err := PeekOverlayEnvironment(path)
	require.NoError(t, err)
	assert.Empty(t, env)
}

func TestPeekOverlayEnvironment_IgnoresOtherFields(t *testing.T) {
	// PeekOverlayEnvironment should not run full validation — even an overlay
	// with fields that would fail LoadOverlayFile (e.g. missing match) should
	// still return the environment.
	dir := t.TempDir()
	path := filepath.Join(dir, "overlay.yaml")
	require.NoError(t, os.WriteFile(path, []byte("environment: qa\noverrides:\n  - baseUrl: http://localhost\n"), 0644))

	env, err := PeekOverlayEnvironment(path)
	require.NoError(t, err)
	assert.Equal(t, "qa", env)
}

func TestPeekOverlayEnvironment_MissingFile(t *testing.T) {
	env, err := PeekOverlayEnvironment(filepath.Join(t.TempDir(), "nope.yaml"))
	assert.Error(t, err)
	assert.Empty(t, env)
}

func TestPeekOverlayEnvironment_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overlay.yaml")
	require.NoError(t, os.WriteFile(path, []byte("environment: [unclosed"), 0644))

	_, err := PeekOverlayEnvironment(path)
	assert.Error(t, err)
}

func TestLoadOverlayFile_WithEnvironment(t *testing.T) {
	// Full load should round-trip the environment field alongside existing fields.
	dir := t.TempDir()
	path := filepath.Join(dir, "overlay.yaml")
	require.NoError(t, os.WriteFile(path, []byte("environment: staging\nheaders:\n  X-Debug: local\noverrides:\n  - match: '*'\n    baseUrl: http://localhost:8080\n"), 0644))

	overlay, err := LoadOverlayFile(path)
	require.NoError(t, err)
	assert.Equal(t, "staging", overlay.Environment)
	assert.Equal(t, "local", overlay.Headers["X-Debug"])
	require.Len(t, overlay.Overrides, 1)
	assert.Equal(t, "*", overlay.Overrides[0].Match)
}

func TestLoadOverlayFile_WithValuesAndExpectFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overlay.yaml")
	body := "" +
		"overrides:\n" +
		"  - match: createBooking\n" +
		"    values:\n" +
		"      lastName: \"\"\n" +
		"      age: -1\n" +
		"    expectFailure:\n" +
		"      status: [400, 422]\n" +
		"      description: invalid payload\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0644))

	overlay, err := LoadOverlayFile(path)
	require.NoError(t, err)
	require.Len(t, overlay.Overrides, 1)
	ov := overlay.Overrides[0]
	assert.Equal(t, "createBooking", ov.Match)
	assert.Equal(t, "", ov.Values["lastName"])
	assert.EqualValues(t, -1, ov.Values["age"])
	require.NotNil(t, ov.ExpectFailure)
	assert.Equal(t, []int{400, 422}, ov.ExpectFailure.Status)
	assert.Equal(t, "invalid payload", ov.ExpectFailure.Description)
}

func TestLoadOverlayFile_RejectsExpectFailureBelow400(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overlay.yaml")
	body := "" +
		"overrides:\n" +
		"  - match: createBooking\n" +
		"    expectFailure:\n" +
		"      status: [200]\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0644))

	_, err := LoadOverlayFile(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), ">= 400")
}

func TestLoadOverlayFile_RejectsEmptyExpectFailureStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overlay.yaml")
	body := "" +
		"overrides:\n" +
		"  - match: createBooking\n" +
		"    expectFailure:\n" +
		"      description: missing status\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0644))

	_, err := LoadOverlayFile(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one status")
}
