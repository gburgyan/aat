package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePlanWritePath_BareName(t *testing.T) {
	path, err := ResolvePlanWritePath([]string{"/plans"}, "one-way")
	require.NoError(t, err)
	assert.Equal(t, "/plans/one-way.yaml", path)
}

func TestResolvePlanWritePath_SubdirName(t *testing.T) {
	path, err := ResolvePlanWritePath([]string{"/plans"}, "booking/one-way")
	require.NoError(t, err)
	assert.Equal(t, "/plans/booking/one-way.yaml", path)
}

func TestResolvePlanWritePath_WithExtension(t *testing.T) {
	path, err := ResolvePlanWritePath([]string{"/plans"}, "test.yaml")
	require.NoError(t, err)
	assert.Equal(t, "/plans/test.yaml", path)
}

func TestResolvePlanWritePath_WithYmlExtension(t *testing.T) {
	path, err := ResolvePlanWritePath([]string{"/plans"}, "test.yml")
	require.NoError(t, err)
	assert.Equal(t, "/plans/test.yml", path)
}

func TestResolvePlanWritePath_UsesFirstDir(t *testing.T) {
	path, err := ResolvePlanWritePath([]string{"/first", "/second"}, "test")
	require.NoError(t, err)
	assert.Equal(t, "/first/test.yaml", path)
}

func TestResolvePlanWritePath_NoDirs(t *testing.T) {
	_, err := ResolvePlanWritePath(nil, "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plan directories configured")
}

func TestFindPlan_ExactMatch(t *testing.T) {
	dir := t.TempDir()
	planFile := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(planFile, []byte("intent: {}"), 0644))

	found, err := FindPlan([]string{dir}, "test.yaml")
	require.NoError(t, err)
	assert.Equal(t, planFile, found)
}

func TestFindPlan_BareName(t *testing.T) {
	dir := t.TempDir()
	planFile := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(planFile, []byte("intent: {}"), 0644))

	found, err := FindPlan([]string{dir}, "test")
	require.NoError(t, err)
	assert.Equal(t, planFile, found)
}

func TestFindPlan_YmlExtension(t *testing.T) {
	dir := t.TempDir()
	planFile := filepath.Join(dir, "test.yml")
	require.NoError(t, os.WriteFile(planFile, []byte("intent: {}"), 0644))

	found, err := FindPlan([]string{dir}, "test")
	require.NoError(t, err)
	assert.Equal(t, planFile, found)
}

func TestFindPlan_SubdirName(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "booking")
	require.NoError(t, os.MkdirAll(subdir, 0755))
	planFile := filepath.Join(subdir, "one-way.yaml")
	require.NoError(t, os.WriteFile(planFile, []byte("intent: {}"), 0644))

	found, err := FindPlan([]string{dir}, "booking/one-way")
	require.NoError(t, err)
	assert.Equal(t, planFile, found)
}

func TestFindPlan_MultiDir(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	planFile := filepath.Join(dir2, "test.yaml")
	require.NoError(t, os.WriteFile(planFile, []byte("intent: {}"), 0644))

	found, err := FindPlan([]string{dir1, dir2}, "test")
	require.NoError(t, err)
	assert.Equal(t, planFile, found)
}

func TestFindPlan_FirstDirWins(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	planFile1 := filepath.Join(dir1, "test.yaml")
	require.NoError(t, os.WriteFile(planFile1, []byte("intent: {}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir2, "test.yaml"), []byte("intent: {}"), 0644))

	found, err := FindPlan([]string{dir1, dir2}, "test")
	require.NoError(t, err)
	assert.Equal(t, planFile1, found)
}

func TestFindPlan_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	planFile := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(planFile, []byte("intent: {}"), 0644))

	found, err := FindPlan(nil, planFile) // no dirs needed for absolute path
	require.NoError(t, err)
	assert.Equal(t, planFile, found)
}

func TestFindPlan_AbsolutePathNotFound(t *testing.T) {
	_, err := FindPlan(nil, "/nonexistent/test.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestFindPlan_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := FindPlan([]string{dir}, "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestFindPlan_NoDirs(t *testing.T) {
	_, err := FindPlan(nil, "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plan directories configured")
}

func TestListPlans_RecursiveWalk(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "booking"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "simple.yaml"), []byte(""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "booking", "one-way.yaml"), []byte(""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "booking", "round-trip.yml"), []byte(""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte(""), 0644)) // non-YAML

	entries, err := ListPlans([]string{dir})
	require.NoError(t, err)
	require.Len(t, entries, 3)

	// Sorted by name
	assert.Equal(t, "booking/one-way.yaml", entries[0].Name)
	assert.Equal(t, "booking/round-trip.yml", entries[1].Name)
	assert.Equal(t, "simple.yaml", entries[2].Name)

	for _, e := range entries {
		assert.Equal(t, dir, e.Dir)
		assert.NotEmpty(t, e.FullPath)
	}
}

func TestListPlans_Dedup(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir1, "test.yaml"), []byte("dir1"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir2, "test.yaml"), []byte("dir2"), 0644))

	entries, err := ListPlans([]string{dir1, dir2})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, dir1, entries[0].Dir) // first dir wins
}

func TestListPlans_MissingDirSkipped(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(""), 0644))

	entries, err := ListPlans([]string{"/nonexistent", dir})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "test.yaml", entries[0].Name)
}

func TestListPlans_Empty(t *testing.T) {
	dir := t.TempDir()
	entries, err := ListPlans([]string{dir})
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestListPlans_NoDirs(t *testing.T) {
	entries, err := ListPlans(nil)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
