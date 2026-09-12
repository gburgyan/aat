package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every loader of project YAML rejects keys that no field accepts and names
// the file the key is in.

func TestLoadManifest_UnknownKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aat-project.yaml")
	writeTestFile(t, path, "name: shop\ngraph: graph.yaml\ntemplates: templates/\nplan: plans/\n")

	_, err := LoadManifest(path)
	require.Error(t, err)
	assert.Equal(t, path+`: line 4: unknown key "plan" in project manifest (did you mean "plans"?)`, err.Error())
}

func TestLoadEnvironment_UnknownKeys(t *testing.T) {
	t.Run("legacy file, removed setting", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "env.yaml")
		writeTestFile(t, path, "environment: dev\napiBaseUrl: https://dev.example.com\nauth:\n  type: none\nsettings:\n  defaultRetries: 1\n")

		_, err := LoadEnvironment(path)
		require.Error(t, err)
		assert.Equal(t, path+`: line 6: unknown key "defaultRetries" in runtime settings (valid keys: minRequestInterval, oasValidation)`, err.Error())
	})

	t.Run("multi-environment file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "env.yaml")
		writeTestFile(t, path, "environments:\n  dev:\n    apiBaseURL: https://dev.example.com\n    auth:\n      type: none\n")

		_, err := LoadNamedEnvironment(path, "dev")
		require.Error(t, err)
		assert.Equal(t, path+`: line 3: unknown key "apiBaseURL" in environment (did you mean "apiBaseUrl"?)`, err.Error())

		_, err = ListEnvironments(path)
		assert.ErrorContains(t, err, `unknown key "apiBaseURL"`)
	})

	t.Run("include file names the include", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "env.yaml")
		writeTestFile(t, path, "include:\n  - secrets.yaml\nenvironments:\n  dev:\n    apiBaseUrl: https://dev.example.com\n    auth:\n      type: none\n")
		writeTestFile(t, filepath.Join(dir, "secrets.yaml"), "environments:\n  dev:\n    secret: x\n")

		_, err := LoadNamedEnvironment(path, "dev")
		require.Error(t, err)
		assert.Contains(t, err.Error(), filepath.Join(dir, "secrets.yaml")+`: line 3: unknown key "secret" in environment`)
	})
}

func TestLoadOverlayFile_UnknownKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overlay.yaml")
	writeTestFile(t, path, "overrides:\n  - match: payment*\n    baseUrl: http://localhost:9000\n    header:\n      X-Debug: \"1\"\n")

	_, err := LoadOverlayFile(path)
	require.Error(t, err)
	assert.Equal(t, path+`: line 4: unknown key "header" in host override (did you mean "headers"?)`, err.Error())

	// The environment probe that runs first stays lenient.
	env, err := PeekOverlayEnvironment(path)
	require.NoError(t, err)
	assert.Empty(t, env)
}

func TestLoadVisualizers_UnknownKey(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "visualizers.yaml"), "visualizers:\n  - id: receipt\n    file: receipt.html\n    matches:\n      node: paymentCharge\n")

	_, err := LoadVisualizers(dir)
	require.Error(t, err)
	assert.Equal(t, filepath.Join(dir, "visualizers.yaml")+`: line 4: unknown key "matches" in visualizer (did you mean "match"?)`, err.Error())
}

func TestLoadUserConfig_ToleratesUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeTestFile(t, path, "default_project: /work/shop\nfrom_a_newer_aat: true\n")

	cfg, err := loadUserConfigFrom(path)
	require.NoError(t, err)
	assert.Equal(t, "/work/shop", cfg.DefaultProject)
}

func TestResolveProjectPaths_BrokenManifest(t *testing.T) {
	isolateUserConfig(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	good := t.TempDir()
	writeManifest(t, good)
	broken := t.TempDir()
	brokenPath := filepath.Join(broken, "aat-project.yaml")
	writeTestFile(t, brokenPath, "name: shop\ngraph: graph.yaml\ntemplates: templates/\nplan: plans/\n")

	t.Run("a broken CWD manifest is an error", func(t *testing.T) {
		require.NoError(t, os.Chdir(broken))
		t.Setenv("AAT_PROJECT", good)

		_, err := ResolveProjectPaths(ProjectPaths{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown key "plan"`)
	})

	t.Run("a higher level that loads wins", func(t *testing.T) {
		require.NoError(t, os.Chdir(t.TempDir()))
		t.Setenv("AAT_PROJECT", broken)

		result, err := ResolveProjectPaths(ProjectPaths{ExplicitManifest: good})
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(good, "aat-project.yaml"), result.ManifestPath)
	})

	t.Run("a missing --manifest is an error", func(t *testing.T) {
		require.NoError(t, os.Chdir(good))
		t.Setenv("AAT_PROJECT", "")
		missing := filepath.Join(t.TempDir(), "aat-project.yaml")

		_, err := ResolveProjectPaths(ProjectPaths{ExplicitManifest: missing})
		assert.EqualError(t, err, "manifest not found: "+missing)
	})

	t.Run("a missing manifest is not an error", func(t *testing.T) {
		require.NoError(t, os.Chdir(t.TempDir()))
		t.Setenv("AAT_PROJECT", filepath.Join(t.TempDir(), "gone"))

		result, err := ResolveProjectPaths(ProjectPaths{})
		require.NoError(t, err)
		assert.Empty(t, result.ManifestPath)
	})
}

// isolateUserConfig points the user config directory at an empty temp dir, so
// a default_project on the machine running the tests cannot leak in.
func isolateUserConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", home)
}
