package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadUserConfigFrom_FileNotFound(t *testing.T) {
	cfg, err := loadUserConfigFrom("/nonexistent/config.yaml")
	require.NoError(t, err, "missing file should not be an error")
	assert.Equal(t, &UserConfig{}, cfg)
}

func TestLoadUserConfigFrom_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("default_project: /home/user/myproject\n"), 0644))

	cfg, err := loadUserConfigFrom(path)
	require.NoError(t, err)
	assert.Equal(t, "/home/user/myproject", cfg.DefaultProject)
}

func TestLoadUserConfigFrom_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("{{bad yaml"), 0644))

	_, err := loadUserConfigFrom(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing user config")
}

func TestUserConfigPath_IsAbsolute(t *testing.T) {
	path, err := UserConfigPath()
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(path), "config path should be absolute: %s", path)
	assert.Contains(t, path, "aat")
	assert.Contains(t, path, "config.yaml")
}
