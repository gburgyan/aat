package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunCommand_MissingPlan(t *testing.T) {
	err := runCommand(context.Background(), &runArgs{
		EnvPath:       "x",
		GraphPath:     "x",
		TemplatesPath: "x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--plan is required")
}

func TestRunCommand_MissingEnv(t *testing.T) {
	err := runCommand(context.Background(), &runArgs{
		PlanPath:      "x",
		GraphPath:     "x",
		TemplatesPath: "x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--env is required")
}

func TestRunCommand_MissingGraph(t *testing.T) {
	err := runCommand(context.Background(), &runArgs{
		PlanPath:      "x",
		EnvPath:       "x",
		TemplatesPath: "x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--graph is required")
}

func TestRunCommand_MissingTemplates(t *testing.T) {
	err := runCommand(context.Background(), &runArgs{
		PlanPath:  "x",
		EnvPath:   "x",
		GraphPath: "x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--templates is required")
}

func TestRunCommand_InvalidEnvPath(t *testing.T) {
	err := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       "testdata/nonexistent.yaml",
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading environment")
}

func TestRunCommand_InvalidGraphPath(t *testing.T) {
	// Create a minimal env file for this test
	envFile := writeTestEnv(t, "none", "")
	err := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/nonexistent.yaml",
		TemplatesPath: "testdata/templates",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading graph")
}

func TestRunCommand_InvalidPlanPath(t *testing.T) {
	envFile := writeTestEnv(t, "none", "")
	err := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/nonexistent.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading plan")
}

func TestRunCommand_InvalidTemplatesPath(t *testing.T) {
	envFile := writeTestEnv(t, "none", "")
	err := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/nonexistent",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading templates")
}

func TestRunCommand_SuccessfulRun(t *testing.T) {
	// Start a mock API server
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"result": "test-output"})
	}))
	defer apiServer.Close()

	// Write env file pointing at mock server
	envFile := writeTestEnv(t, "none", apiServer.URL)

	outputDir := filepath.Join(t.TempDir(), "runs")
	err := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     outputDir,
	})
	require.NoError(t, err)

	// Verify archive was written
	entries, err := os.ReadDir(outputDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "expected one run directory")
	archivePath := filepath.Join(outputDir, entries[0].Name(), "archive.json")
	_, err = os.Stat(archivePath)
	require.NoError(t, err, "archive.json should exist")
}

func TestRunCommand_FailedStep(t *testing.T) {
	// Server returns 500 for all requests
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal"}`))
	}))
	defer apiServer.Close()

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")

	err := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     outputDir,
	})
	require.Error(t, err, "should fail when API returns 500")
}

func TestRunCommand_WithCustomHeaders(t *testing.T) {
	var receivedHeader string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Custom")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	envContent := "environment: test\napiBaseUrl: " + apiServer.URL + "\nauth:\n  type: none\nheaders:\n  X-Custom: test-value\n"
	envFile := filepath.Join(t.TempDir(), "env.yaml")
	require.NoError(t, os.WriteFile(envFile, []byte(envContent), 0644))

	outputDir := filepath.Join(t.TempDir(), "runs")
	err := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     outputDir,
	})
	require.NoError(t, err)
	assert.Equal(t, "test-value", receivedHeader)
}

// writeTestEnv creates a temporary environment YAML file.
func writeTestEnv(t *testing.T, authType, baseURL string) string {
	t.Helper()
	if baseURL == "" {
		baseURL = "https://api.example.com"
	}
	content := "environment: test\napiBaseUrl: " + baseURL + "\nauth:\n  type: " + authType + "\n"
	path := filepath.Join(t.TempDir(), "env.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}
