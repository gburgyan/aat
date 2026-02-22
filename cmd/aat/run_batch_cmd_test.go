package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gburgyan/aat/archive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- discoverBatchPlans tests ---

func TestDiscoverBatchPlans_AllPlans(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "alpha.yaml"), "execution:\n  steps: []\n")
	writeFile(t, filepath.Join(dir, "beta.yaml"), "execution:\n  steps: []\n")

	entries, source, err := discoverBatchPlans([]string{dir}, "")
	require.NoError(t, err)
	assert.Equal(t, "all", source)
	assert.Len(t, entries, 2)
	assert.Equal(t, "alpha.yaml", entries[0].Name)
	assert.Equal(t, "beta.yaml", entries[1].Name)
}

func TestDiscoverBatchPlans_FilterByPrefix(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "booking"), 0755))
	writeFile(t, filepath.Join(dir, "booking", "one-way.yaml"), "execution:\n  steps: []\n")
	writeFile(t, filepath.Join(dir, "booking", "round-trip.yaml"), "execution:\n  steps: []\n")
	writeFile(t, filepath.Join(dir, "search.yaml"), "execution:\n  steps: []\n")

	entries, source, err := discoverBatchPlans([]string{dir}, "booking")
	require.NoError(t, err)
	assert.Equal(t, "booking", source)
	assert.Len(t, entries, 2)
	for _, e := range entries {
		assert.Contains(t, e.Name, "booking")
	}
}

func TestDiscoverBatchPlans_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "plan.yaml"), "execution:\n  steps: []\n")

	entries, source, err := discoverBatchPlans(nil, dir)
	require.NoError(t, err)
	assert.Equal(t, dir, source)
	assert.Len(t, entries, 1)
}

func TestDiscoverBatchPlans_NoPlanDirs(t *testing.T) {
	_, _, err := discoverBatchPlans(nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plan directories configured")
}

func TestDiscoverBatchPlans_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	entries, source, err := discoverBatchPlans([]string{dir}, "")
	require.NoError(t, err)
	assert.Equal(t, "all", source)
	assert.Empty(t, entries)
}

func TestDiscoverBatchPlans_FilterNoMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "search.yaml"), "execution:\n  steps: []\n")

	entries, source, err := discoverBatchPlans([]string{dir}, "booking")
	require.NoError(t, err)
	assert.Equal(t, "booking", source)
	assert.Empty(t, entries)
}

// --- planBaseName tests ---

func TestPlanBaseName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"complex-card.yaml", "complex-card"},
		{"booking/one-way.yml", "booking/one-way"},
		{"no-extension", "no-extension"},
		{"file.json", "file.json"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, planBaseName(tc.input), "planBaseName(%q)", tc.input)
	}
}

// --- formatDuration tests ---

func TestFormatDuration_Milliseconds(t *testing.T) {
	assert.Equal(t, "450ms", formatDuration(450_000_000)) // 450ms in nanoseconds
}

func TestFormatDuration_Seconds(t *testing.T) {
	assert.Equal(t, "2.5s", formatDuration(2_500_000_000))
}

// --- batchExitCode tests ---

func TestBatchExitCode_AllPassed(t *testing.T) {
	res := &batchResult{
		summary: &BatchSummary{
			Outcome: "passed",
			Summary: BatchStats{TotalPlans: 2, PassedPlans: 2},
		},
	}
	assert.Equal(t, 0, batchExitCode(res))
}

func TestBatchExitCode_AnyFailed(t *testing.T) {
	res := &batchResult{
		summary: &BatchSummary{
			Outcome: "failed",
			Summary: BatchStats{TotalPlans: 2, PassedPlans: 1, FailedPlans: 1},
		},
	}
	assert.Equal(t, 1, batchExitCode(res))
}

func TestBatchExitCode_AnyError(t *testing.T) {
	res := &batchResult{
		summary: &BatchSummary{
			Outcome: "error",
			Summary: BatchStats{TotalPlans: 2, PassedPlans: 1, ErrorPlans: 1},
		},
	}
	assert.Equal(t, exitCodeInfra, batchExitCode(res))
}

func TestBatchExitCode_SetupError(t *testing.T) {
	res := &batchResult{setupErr: true}
	assert.Equal(t, exitCodeInfra, batchExitCode(res))
}

func TestBatchExitCode_NilSummary(t *testing.T) {
	res := &batchResult{}
	assert.Equal(t, exitCodeInfra, batchExitCode(res))
}

// --- batchCommand integration tests ---

func TestBatchCommand_NoPlans(t *testing.T) {
	planDir := t.TempDir()
	envFile := writeTestEnv(t, "none", "https://api.example.com")
	outputDir := filepath.Join(t.TempDir(), "runs")

	var buf bytes.Buffer
	res := batchCommand(context.Background(), &batchArgs{
		runArgs: runArgs{
			EnvPath:       envFile,
			GraphPath:     "testdata/test_graph.yaml",
			TemplatesPath: "testdata/templates",
			OutputDir:     outputDir,
		},
		PlanDirs: []string{planDir},
	}, &buf)

	require.NotNil(t, res.summary)
	assert.Equal(t, "passed", res.summary.Outcome)
	assert.Empty(t, res.summary.Runs)
	assert.Equal(t, 0, res.summary.Summary.TotalPlans)
	assert.Contains(t, buf.String(), "no plans found")
}

func TestBatchCommand_SinglePlan_Passed(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	planDir := t.TempDir()
	writeFile(t, filepath.Join(planDir, "test.yaml"), "execution:\n  steps:\n    - node: testNode\n      values:\n        input1: hello\n")

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")

	res := batchCommand(context.Background(), &batchArgs{
		runArgs: runArgs{
			EnvPath:       envFile,
			GraphPath:     "testdata/test_graph.yaml",
			TemplatesPath: "testdata/templates",
			OutputDir:     outputDir,
		},
		PlanDirs: []string{planDir},
	}, io.Discard)

	require.NotNil(t, res.summary)
	assert.Equal(t, "passed", res.summary.Outcome)
	require.Len(t, res.summary.Runs, 1)
	assert.Equal(t, "test", res.summary.Runs[0].PlanName)
	assert.Equal(t, "passed", res.summary.Runs[0].Outcome)
	assert.Equal(t, 1, res.summary.Summary.TotalPlans)
	assert.Equal(t, 1, res.summary.Summary.PassedPlans)
	assert.Equal(t, 0, res.summary.Summary.FailedPlans)
}

func TestBatchCommand_MultiplePlans(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	planDir := t.TempDir()
	writeFile(t, filepath.Join(planDir, "alpha.yaml"), "execution:\n  steps:\n    - node: testNode\n      values:\n        input1: hello\n")
	writeFile(t, filepath.Join(planDir, "beta.yaml"), "execution:\n  steps:\n    - node: testNode\n      values:\n        input1: world\n")

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")

	res := batchCommand(context.Background(), &batchArgs{
		runArgs: runArgs{
			EnvPath:       envFile,
			GraphPath:     "testdata/test_graph.yaml",
			TemplatesPath: "testdata/templates",
			OutputDir:     outputDir,
		},
		PlanDirs: []string{planDir},
	}, io.Discard)

	require.NotNil(t, res.summary)
	assert.Equal(t, "passed", res.summary.Outcome)
	assert.Len(t, res.summary.Runs, 2)
	assert.Equal(t, 2, res.summary.Summary.TotalPlans)
	assert.Equal(t, 2, res.summary.Summary.PassedPlans)
}

func TestBatchCommand_OneFailedPlan(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer apiServer.Close()

	planDir := t.TempDir()
	writeFile(t, filepath.Join(planDir, "fail.yaml"), "execution:\n  steps:\n    - node: testNode\n      values:\n        input1: hello\n")

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")

	res := batchCommand(context.Background(), &batchArgs{
		runArgs: runArgs{
			EnvPath:       envFile,
			GraphPath:     "testdata/test_graph.yaml",
			TemplatesPath: "testdata/templates",
			OutputDir:     outputDir,
		},
		PlanDirs: []string{planDir},
	}, io.Discard)

	require.NotNil(t, res.summary)
	assert.Equal(t, "failed", res.summary.Outcome)
	assert.Equal(t, 1, res.summary.Summary.FailedPlans)
	assert.Equal(t, 1, batchExitCode(res))
}

func TestBatchCommand_MixedResults(t *testing.T) {
	reqCount := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		w.Header().Set("Content-Type", "application/json")
		if reqCount == 1 {
			json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
		} else {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error": "bad"}`))
		}
	}))
	defer apiServer.Close()

	planDir := t.TempDir()
	writeFile(t, filepath.Join(planDir, "alpha.yaml"), "execution:\n  steps:\n    - node: testNode\n      values:\n        input1: hello\n")
	writeFile(t, filepath.Join(planDir, "beta.yaml"), "execution:\n  steps:\n    - node: testNode\n      values:\n        input1: world\n")

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")

	res := batchCommand(context.Background(), &batchArgs{
		runArgs: runArgs{
			EnvPath:       envFile,
			GraphPath:     "testdata/test_graph.yaml",
			TemplatesPath: "testdata/templates",
			OutputDir:     outputDir,
		},
		PlanDirs: []string{planDir},
	}, io.Discard)

	require.NotNil(t, res.summary)
	assert.Equal(t, "failed", res.summary.Outcome)
	assert.Equal(t, 2, res.summary.Summary.TotalPlans)
	assert.Equal(t, 1, res.summary.Summary.PassedPlans)
	assert.Equal(t, 1, res.summary.Summary.FailedPlans)
}

func TestBatchCommand_WritesBatchJSON(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	planDir := t.TempDir()
	writeFile(t, filepath.Join(planDir, "test.yaml"), "execution:\n  steps:\n    - node: testNode\n      values:\n        input1: hello\n")

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")

	res := batchCommand(context.Background(), &batchArgs{
		runArgs: runArgs{
			EnvPath:       envFile,
			GraphPath:     "testdata/test_graph.yaml",
			TemplatesPath: "testdata/templates",
			OutputDir:     outputDir,
		},
		PlanDirs: []string{planDir},
	}, io.Discard)

	require.NotEmpty(t, res.batchDir)

	// Read and validate batch.json
	batchJSONPath := filepath.Join(res.batchDir, "batch.json")
	ba, err := archive.ReadBatch(batchJSONPath)
	require.NoError(t, err)
	assert.Equal(t, "passed", ba.Result.Outcome)
	assert.Equal(t, 1, ba.Result.TotalRuns)
	assert.Len(t, ba.Runs, 1)
	assert.Equal(t, "test", ba.Runs[0].PlanName)
	assert.NotEmpty(t, ba.Metadata.BatchID)
}

func TestBatchCommand_ArchiveStructure(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	planDir := t.TempDir()
	writeFile(t, filepath.Join(planDir, "plan1.yaml"), "execution:\n  steps:\n    - node: testNode\n      values:\n        input1: hello\n")
	writeFile(t, filepath.Join(planDir, "plan2.yaml"), "execution:\n  steps:\n    - node: testNode\n      values:\n        input1: world\n")

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")

	res := batchCommand(context.Background(), &batchArgs{
		runArgs: runArgs{
			EnvPath:       envFile,
			GraphPath:     "testdata/test_graph.yaml",
			TemplatesPath: "testdata/templates",
			OutputDir:     outputDir,
		},
		PlanDirs: []string{planDir},
	}, io.Discard)

	require.NotEmpty(t, res.batchDir)

	// Verify batch directory structure:
	// batchDir/
	//   batch.json
	//   run-YYYYMMDD-HHMMSS-XXXXXXXX/archive.json  (for each plan)
	entries, err := os.ReadDir(res.batchDir)
	require.NoError(t, err)

	// Should have batch.json + 2 run directories
	var runDirs []string
	hasBatchJSON := false
	for _, e := range entries {
		if e.Name() == "batch.json" {
			hasBatchJSON = true
		} else if e.IsDir() {
			runDirs = append(runDirs, e.Name())
		}
	}
	assert.True(t, hasBatchJSON, "batch.json should exist")
	assert.Len(t, runDirs, 2, "should have 2 run directories")

	// Each run dir should contain archive.json
	for _, rd := range runDirs {
		archivePath := filepath.Join(res.batchDir, rd, "archive.json")
		_, err := os.Stat(archivePath)
		assert.NoError(t, err, "archive.json should exist in %s", rd)
	}
}

func TestBatchCommand_SetupError(t *testing.T) {
	planDir := t.TempDir()
	writeFile(t, filepath.Join(planDir, "test.yaml"), "execution:\n  steps:\n    - node: testNode\n      values:\n        input1: hello\n")

	res := batchCommand(context.Background(), &batchArgs{
		runArgs: runArgs{
			EnvPath:       "nonexistent.yaml",
			GraphPath:     "testdata/test_graph.yaml",
			TemplatesPath: "testdata/templates",
			OutputDir:     filepath.Join(t.TempDir(), "runs"),
		},
		PlanDirs: []string{planDir},
	}, io.Discard)

	assert.True(t, res.setupErr)
	assert.Error(t, res.err)
	assert.Equal(t, exitCodeInfra, batchExitCode(res))
}

func TestBatchCommand_InvalidPlan(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	planDir := t.TempDir()
	// Plan references a node that doesn't exist
	writeFile(t, filepath.Join(planDir, "bad.yaml"), "execution:\n  steps:\n    - node: nonExistentNode\n      values:\n        foo: bar\n")

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")

	res := batchCommand(context.Background(), &batchArgs{
		runArgs: runArgs{
			EnvPath:       envFile,
			GraphPath:     "testdata/test_graph.yaml",
			TemplatesPath: "testdata/templates",
			OutputDir:     outputDir,
		},
		PlanDirs: []string{planDir},
	}, io.Discard)

	require.NotNil(t, res.summary)
	assert.Equal(t, "error", res.summary.Outcome)
	assert.Equal(t, 1, res.summary.Summary.ErrorPlans)
	require.Len(t, res.summary.Runs, 1)
	assert.Equal(t, "error", res.summary.Runs[0].Outcome)
	assert.NotEmpty(t, res.summary.Runs[0].Error)
}

// --- executeBatch output mode tests ---

func TestExecuteBatch_JSONOutput(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	planDir := t.TempDir()
	writeFile(t, filepath.Join(planDir, "test.yaml"), "execution:\n  steps:\n    - node: testNode\n      values:\n        input1: hello\n")

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := executeBatch(&batchArgs{
		runArgs: runArgs{
			EnvPath:       envFile,
			GraphPath:     "testdata/test_graph.yaml",
			TemplatesPath: "testdata/templates",
			OutputDir:     outputDir,
			JSON:          true,
		},
		PlanDirs: []string{planDir},
	})

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	assert.Equal(t, 0, code)

	// Parse the JSON output
	var summary BatchSummary
	require.NoError(t, json.Unmarshal(buf.Bytes(), &summary), "stdout should be valid JSON: %s", buf.String())
	assert.Equal(t, "passed", summary.Outcome)
	assert.Len(t, summary.Runs, 1)
}

func TestExecuteBatch_QuietOutput(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	planDir := t.TempDir()
	writeFile(t, filepath.Join(planDir, "mytest.yaml"), "execution:\n  steps:\n    - node: testNode\n      values:\n        input1: hello\n")

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := executeBatch(&batchArgs{
		runArgs: runArgs{
			EnvPath:       envFile,
			GraphPath:     "testdata/test_graph.yaml",
			TemplatesPath: "testdata/templates",
			OutputDir:     outputDir,
			Quiet:         true,
		},
		PlanDirs: []string{planDir},
	})

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	assert.Equal(t, 0, code)
	output := buf.String()
	assert.Contains(t, output, "mytest: PASSED")
	assert.Contains(t, output, "Batch: 1/1 PASSED")
}

// --- BatchSummary JSON serialization ---

func TestBatchSummary_JSONRoundTrip(t *testing.T) {
	summary := BatchSummary{
		Outcome: "failed",
		BatchID: "batch-20260222-120000-abcdef01",
		Runs: []BatchRunResult{
			{
				PlanName:    "alpha",
				Outcome:     "passed",
				StepCount:   3,
				PassedSteps: 3,
				DurationMs:  150,
			},
			{
				PlanName:    "beta",
				Outcome:     "failed",
				StepCount:   2,
				PassedSteps: 1,
				FailedSteps: 1,
				DurationMs:  200,
				Error:       "step failed",
			},
		},
		Summary: BatchStats{
			TotalPlans:  2,
			PassedPlans: 1,
			FailedPlans: 1,
			DurationMs:  350,
		},
		ArchivePath: "/tmp/runs/batch-xxx",
	}

	data, err := json.Marshal(summary)
	require.NoError(t, err)

	var parsed BatchSummary
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, summary.Outcome, parsed.Outcome)
	assert.Equal(t, summary.BatchID, parsed.BatchID)
	assert.Len(t, parsed.Runs, 2)
	assert.Equal(t, "alpha", parsed.Runs[0].PlanName)
	assert.Equal(t, "failed", parsed.Runs[1].Outcome)
	assert.Equal(t, 2, parsed.Summary.TotalPlans)
	assert.Equal(t, summary.ArchivePath, parsed.ArchivePath)
}

func TestBatchCommand_FilterBySubdir(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	planDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(planDir, "booking"), 0755))
	writeFile(t, filepath.Join(planDir, "booking", "oneway.yaml"), "execution:\n  steps:\n    - node: testNode\n      values:\n        input1: hello\n")
	writeFile(t, filepath.Join(planDir, "search.yaml"), "execution:\n  steps:\n    - node: testNode\n      values:\n        input1: world\n")

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")

	res := batchCommand(context.Background(), &batchArgs{
		runArgs: runArgs{
			EnvPath:       envFile,
			GraphPath:     "testdata/test_graph.yaml",
			TemplatesPath: "testdata/templates",
			OutputDir:     outputDir,
		},
		PlanDirs:   []string{planDir},
		FilterPath: "booking",
	}, io.Discard)

	require.NotNil(t, res.summary)
	assert.Equal(t, "passed", res.summary.Outcome)
	// Only the booking plan should have been run
	require.Len(t, res.summary.Runs, 1)
	assert.Contains(t, res.summary.Runs[0].PlanName, "oneway")
}

// --- Batch archive types tests ---

func TestGenerateBatchID_Format(t *testing.T) {
	id := archive.GenerateBatchID()
	assert.Regexp(t, `^batch-\d{8}-\d{6}-[0-9a-f]{8}$`, id)
}

func TestBatchArchive_WriteAndRead(t *testing.T) {
	ba := &archive.BatchArchive{
		Metadata: archive.BatchMetadata{
			Version: "1.0.0",
			BatchID: "batch-20260222-120000-abcdef01",
			Source:  "all",
		},
		Runs: []archive.BatchRunEntry{
			{
				PlanName:    "test",
				RunID:       "run-20260222-120001-abcdef01",
				Outcome:     "passed",
				StepCount:   1,
				PassedCount: 1,
				DurationMs:  100,
			},
		},
		Result: archive.BatchResult{
			Outcome:         "passed",
			TotalRuns:       1,
			PassedRuns:      1,
			TotalDurationMs: 100,
		},
	}

	path := filepath.Join(t.TempDir(), "batch.json")
	require.NoError(t, archive.WriteBatch(ba, path))

	loaded, err := archive.ReadBatch(path)
	require.NoError(t, err)
	assert.Equal(t, ba.Metadata.BatchID, loaded.Metadata.BatchID)
	assert.Equal(t, "passed", loaded.Result.Outcome)
	assert.Len(t, loaded.Runs, 1)
	assert.Equal(t, "test", loaded.Runs[0].PlanName)
}

// --- Helpers ---

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}
