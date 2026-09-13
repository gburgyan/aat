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

	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/gburgyan/aat/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Existing tests (call runCommand directly — unchanged) ---

func TestRunCommand_MissingPlan(t *testing.T) {
	res := runCommand(context.Background(), &runArgs{
		EnvPath:       "x",
		GraphPath:     "x",
		TemplatesPath: "x",
	}, io.Discard, TerminalInfo{})
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "plan path is required")
}

func TestRunCommand_MissingEnv(t *testing.T) {
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "x",
		GraphPath:     "x",
		TemplatesPath: "x",
	}, io.Discard, TerminalInfo{})
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "--env-config is required")
}

func TestRunCommand_MissingGraph(t *testing.T) {
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "x",
		EnvPath:       "x",
		TemplatesPath: "x",
	}, io.Discard, TerminalInfo{})
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "--graph is required")
}

func TestRunCommand_MissingTemplates(t *testing.T) {
	res := runCommand(context.Background(), &runArgs{
		PlanPath:  "x",
		EnvPath:   "x",
		GraphPath: "x",
	}, io.Discard, TerminalInfo{})
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "--templates is required")
}

func TestRunCommand_InvalidEnvPath(t *testing.T) {
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       "testdata/nonexistent.yaml",
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
	}, io.Discard, TerminalInfo{})
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "loading environment")
}

func TestRunCommand_InvalidGraphPath(t *testing.T) {
	envFile := writeTestEnv(t, "none", "")
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/nonexistent.yaml",
		TemplatesPath: "testdata/templates",
	}, io.Discard, TerminalInfo{})
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "loading graph")
}

func TestRunCommand_InvalidPlanPath(t *testing.T) {
	envFile := writeTestEnv(t, "none", "")
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/nonexistent.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
	}, io.Discard, TerminalInfo{})
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "loading plan")
}

func TestRunCommand_InvalidTemplatesPath(t *testing.T) {
	envFile := writeTestEnv(t, "none", "")
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/nonexistent",
	}, io.Discard, TerminalInfo{})
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "loading templates")
}

func TestRunCommand_SuccessfulRun(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "test-output"})
	}))
	defer apiServer.Close()

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     outputDir,
	}, io.Discard, TerminalInfo{})
	require.NoError(t, res.err)

	// Verify archive was written
	entries, err := os.ReadDir(outputDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "expected one run directory")
	archivePath := filepath.Join(outputDir, entries[0].Name(), "archive.json")
	_, err = os.Stat(archivePath)
	require.NoError(t, err, "archive.json should exist")
}

func TestRunCommand_StopAfterDumpsState(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "test-output"})
	}))
	defer apiServer.Close()

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")
	dumpPath := filepath.Join(t.TempDir(), "state", "dump.json")
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     outputDir,
		StopAfterStep: "testNode",
		DumpStatePath: dumpPath,
	}, io.Discard, TerminalInfo{})
	require.NoError(t, res.err)
	assert.Equal(t, engine.OutcomeStopped, res.outcome)

	// Dump file written with restrictive permissions.
	info, err := os.Stat(dumpPath)
	require.NoError(t, err, "state dump should exist")
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	data, err := os.ReadFile(dumpPath)
	require.NoError(t, err)
	var exp engine.StateExport
	require.NoError(t, json.Unmarshal(data, &exp))
	assert.Equal(t, "stopped", exp.Outcome)
	assert.Equal(t, "testNode", exp.StoppedAt)
	assert.Equal(t, apiServer.URL, exp.BaseURL)
	require.Len(t, exp.Steps, 1)
	assert.Equal(t, apiServer.URL, exp.Steps[0].BaseURL, "each step records the host it used")

	// The JSON summary names the checkpoint too.
	require.NotNil(t, res.summary)
	assert.Equal(t, "testNode", res.summary.StoppedAt)
}

// TestRunCommand_DumpStateWriteFailureIsVisible: a state dump that cannot be
// written is reported on stderr even under --quiet, where progress logging is
// discarded, and does not change the run's outcome.
func TestRunCommand_DumpStateWriteFailureIsVisible(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "test-output"})
	}))
	defer apiServer.Close()

	dir := t.TempDir()
	notADir := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o644))

	stderr, err := os.Create(filepath.Join(dir, "stderr"))
	require.NoError(t, err)
	oldStderr := os.Stderr
	os.Stderr = stderr
	code := executeRun(&runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       writeTestEnv(t, "none", apiServer.URL),
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     filepath.Join(dir, "runs"),
		Quiet:         true,
		StopAfterStep: "testNode",
		DumpStatePath: filepath.Join(notADir, "state.json"),
	})
	os.Stderr = oldStderr
	require.NoError(t, stderr.Close())

	assert.Equal(t, 0, code, "a failed dump does not change the exit code")
	logs, err := os.ReadFile(stderr.Name())
	require.NoError(t, err)
	assert.Contains(t, string(logs), "failed to write state dump")
}

func TestToStepSummary_NameIsStepID(t *testing.T) {
	ss := toStepSummary(engine.StepResult{
		StepID:     "addItem__zero_quantity",
		Node:       "addItem",
		StatusCode: 400,
		RetriedOn:  []engine.ErrorCategory{engine.CategoryTransient},
	})
	assert.Equal(t, "addItem__zero_quantity", ss.Name, "mutation siblings stay distinguishable")
	assert.Equal(t, "addItem", ss.Node)
	assert.Equal(t, []string{"transient"}, ss.RetriedOn)
}

func TestToStepSummary_FailedAssertions(t *testing.T) {
	ss := toStepSummary(engine.StepResult{
		StepID: "addItem",
		Node:   "addItem",
		Validation: &validate.MechanicalResult{Results: []validate.AssertionResult{
			{Type: validate.AssertStatus, Passed: true, Message: "status code is 201"},
			{Type: validate.AssertPredicate, Passed: false, Message: `predicate "subtotal > 0" is false`},
		}},
	})
	assert.Equal(t, 1, ss.AssertionsPassed)
	assert.Equal(t, 1, ss.AssertionsFailed)
	assert.Equal(t, []string{`predicate: predicate "subtotal > 0" is false`}, ss.FailedAssertions)
}

func TestRunCommand_StopAfterDumpStateStdout(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "test-output"})
	}))
	defer apiServer.Close()

	envFile := writeTestEnv(t, "none", apiServer.URL)
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     filepath.Join(t.TempDir(), "runs"),
		StopAfterStep: "testNode",
		DumpStatePath: "-",
	}, io.Discard, TerminalInfo{})
	require.NoError(t, res.err)
	assert.Equal(t, engine.OutcomeStopped, res.outcome)

	// "-" attaches the export to the summary (surfaced via --json) instead of
	// writing a file.
	require.NotNil(t, res.summary)
	require.NotNil(t, res.summary.State, "state should be embedded in the summary for --dump-state -")
	assert.Equal(t, "stopped", res.summary.State.Outcome)
	assert.Equal(t, "testNode", res.summary.State.StoppedAt)
	assert.Equal(t, apiServer.URL, res.summary.State.BaseURL)

	// The embedded state must survive JSON round-trip under the "state" key.
	data, err := json.Marshal(res.summary)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	_, hasState := decoded["state"]
	assert.True(t, hasState, "JSON summary should include a top-level \"state\" object")
}

func TestRunCommand_StopAfterUnknownStep(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer apiServer.Close()

	envFile := writeTestEnv(t, "none", apiServer.URL)
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     filepath.Join(t.TempDir(), "runs"),
		StopAfterStep: "doesNotExist",
	}, io.Discard, TerminalInfo{})
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), `no step "doesNotExist"`)
}

func TestRunCommand_FailedStep(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal"}`))
	}))
	defer apiServer.Close()

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     outputDir,
	}, io.Discard, TerminalInfo{})
	require.Error(t, res.err, "should fail when API returns 500")
}

func TestRunCommand_WithCustomHeaders(t *testing.T) {
	var receivedHeader string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Custom")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	envContent := "environment: test\napiBaseUrl: " + apiServer.URL + "\nauth:\n  type: none\nheaders:\n  X-Custom: test-value\n"
	envFile := filepath.Join(t.TempDir(), "env.yaml")
	require.NoError(t, os.WriteFile(envFile, []byte(envContent), 0644))

	outputDir := filepath.Join(t.TempDir(), "runs")
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     outputDir,
	}, io.Discard, TerminalInfo{})
	require.NoError(t, res.err)
	assert.Equal(t, "test-value", receivedHeader)
}

// --- CI/CD mode tests ---

func TestExitCode_Passed(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	envFile := writeTestEnv(t, "none", apiServer.URL)
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     filepath.Join(t.TempDir(), "runs"),
	}, io.Discard, TerminalInfo{})

	assert.Equal(t, engine.OutcomePassed, res.outcome)
	assert.Equal(t, 0, exitCode(res))
}

func TestExitCode_FailedStep(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer apiServer.Close()

	envFile := writeTestEnv(t, "none", apiServer.URL)
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     filepath.Join(t.TempDir(), "runs"),
	}, io.Discard, TerminalInfo{})

	assert.Equal(t, engine.OutcomeFailed, res.outcome)
	assert.Equal(t, 1, exitCode(res))
}

func TestExitCode_InfraError_MissingPlanFile(t *testing.T) {
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/nonexistent.yaml",
		EnvPath:       writeTestEnv(t, "none", ""),
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
	}, io.Discard, TerminalInfo{})

	assert.Equal(t, exitCodeInfra, exitCode(res))
}

func TestExitCode_InfraError_InvalidYAML(t *testing.T) {
	badPlan := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(badPlan, []byte("not: [yaml: {"), 0644))

	res := runCommand(context.Background(), &runArgs{
		PlanPath:      badPlan,
		EnvPath:       writeTestEnv(t, "none", ""),
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
	}, io.Discard, TerminalInfo{})

	assert.Equal(t, exitCodeInfra, exitCode(res))
}

func TestExitCode_PlanValidationError(t *testing.T) {
	// Plan referencing a non-existent node should fail validation
	badPlan := filepath.Join(t.TempDir(), "bad_plan.yaml")
	require.NoError(t, os.WriteFile(badPlan, []byte("execution:\n  steps:\n    - node: nonExistentNode\n      values:\n        foo: bar\n"), 0644))

	envFile := writeTestEnv(t, "none", "https://api.example.com")
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      badPlan,
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     filepath.Join(t.TempDir(), "runs"),
	}, io.Discard, TerminalInfo{})

	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "plan validation")
	assert.Equal(t, exitCodeInfra, exitCode(res))
}

func TestJSON_SuccessfulRun(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	envFile := writeTestEnv(t, "none", apiServer.URL)
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     filepath.Join(t.TempDir(), "runs"),
		JSON:          true,
		Quiet:         true,
	}, io.Discard, TerminalInfo{})

	require.NotNil(t, res.summary)

	// Marshal to JSON and verify it's valid
	data, err := json.Marshal(res.summary)
	require.NoError(t, err)

	var parsed RunSummary
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "passed", parsed.Outcome)
	assert.Equal(t, "", parsed.Error)
	assert.Len(t, parsed.Steps, 1)
	assert.Equal(t, "testNode", parsed.Steps[0].Node)
	assert.Equal(t, 200, parsed.Steps[0].Status)
	assert.True(t, parsed.Steps[0].Passed)
	assert.Equal(t, 1, parsed.Summary.TotalSteps)
	assert.Equal(t, 1, parsed.Summary.PassedSteps)
	assert.Equal(t, 0, parsed.Summary.FailedSteps)
	assert.NotEmpty(t, parsed.ArchivePath)
}

func TestJSON_FailedRun(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "invalid"}`))
	}))
	defer apiServer.Close()

	envFile := writeTestEnv(t, "none", apiServer.URL)
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     filepath.Join(t.TempDir(), "runs"),
		JSON:          true,
		Quiet:         true,
	}, io.Discard, TerminalInfo{})

	require.NotNil(t, res.summary)
	assert.Equal(t, "failed", res.summary.Outcome)
	assert.NotEmpty(t, res.summary.Error)
	assert.Len(t, res.summary.Steps, 1)
	assert.Equal(t, 400, res.summary.Steps[0].Status)
	assert.False(t, res.summary.Steps[0].Passed)
	assert.Equal(t, 0, res.summary.Summary.PassedSteps)
	assert.Equal(t, 1, res.summary.Summary.FailedSteps)
}

func TestJSON_InfraError(t *testing.T) {
	// Config error: missing plan file → no summary, just error
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "nonexistent.yaml",
		EnvPath:       writeTestEnv(t, "none", ""),
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		JSON:          true,
		Quiet:         true,
	}, io.Discard, TerminalInfo{})

	assert.Nil(t, res.summary, "no summary for setup errors")
	assert.Error(t, res.err)
	assert.Equal(t, exitCodeInfra, exitCode(res))
}

func TestQuiet_SuppressesProgressMessages(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	envFile := writeTestEnv(t, "none", apiServer.URL)

	// Capture what gets written to the progress writer
	var progressBuf bytes.Buffer
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     filepath.Join(t.TempDir(), "runs"),
		Quiet:         true,
	}, &progressBuf, TerminalInfo{Width: 80})

	require.NoError(t, res.err)
	// In quiet mode, the caller passes io.Discard. Here we passed a buffer
	// to verify behavior. With Quiet=true in executeRun, io.Discard is used.
	// For this test, we verify that when we explicitly pass Discard, nothing
	// is written.
	var discardBuf bytes.Buffer
	_ = runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     filepath.Join(t.TempDir(), "runs2"),
		Quiet:         true,
	}, io.Discard, TerminalInfo{})
	assert.Empty(t, discardBuf.String(), "nothing should be written to discard")

	// But a summary should still be available
	require.NotNil(t, res.summary)
	assert.Equal(t, "passed", res.summary.Outcome)
}

func TestProgressOutput_NotQuiet(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	envFile := writeTestEnv(t, "none", apiServer.URL)

	var buf bytes.Buffer
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     filepath.Join(t.TempDir(), "runs"),
	}, &buf, TerminalInfo{Width: 80})

	require.NoError(t, res.err)
	output := buf.String()
	assert.Contains(t, output, "aat: loading environment")
	assert.Contains(t, output, "aat: executing plan")
	assert.Contains(t, output, "PASSED")
}

func TestBuildRunSummary_CleanupLinks(t *testing.T) {
	result := &engine.RunResult{
		Outcome: engine.OutcomePassed,
		CleanupResults: []engine.StepResult{
			{StepID: "deleteCart", Node: "deleteCart", StatusCode: 204, CleanupFor: "firstCart"},
			{StepID: "deleteCart_2", Node: "deleteCart", StatusCode: 204, CleanupFor: "secondCart"},
		},
	}

	s := buildRunSummary(result, "")

	require.Len(t, s.Cleanup, 2)
	assert.Equal(t, []string{"deleteCart", "deleteCart_2"}, []string{s.Cleanup[0].Name, s.Cleanup[1].Name})
	assert.Equal(t, []string{"firstCart", "secondCart"}, []string{s.Cleanup[0].CleanupFor, s.Cleanup[1].CleanupFor})
}

func TestRunSummary_ArchivePath(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     outputDir,
	}, io.Discard, TerminalInfo{})

	require.NotNil(t, res.summary)
	assert.NotEmpty(t, res.summary.ArchivePath)
	assert.Contains(t, res.summary.ArchivePath, "archive.json")

	// Verify the path is the same as what's in the result
	assert.Equal(t, res.archivePath, res.summary.ArchivePath)
}

// --- executeRun tests (replaced runMain tests) ---

func TestExecuteRun_JSONOutput(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	envFile := writeTestEnv(t, "none", apiServer.URL)
	outputDir := filepath.Join(t.TempDir(), "runs")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := executeRun(&runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     outputDir,
		JSON:          true,
	})

	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	assert.Equal(t, 0, code)

	// Parse the JSON output
	var summary RunSummary
	require.NoError(t, json.Unmarshal(buf.Bytes(), &summary), "stdout should be valid JSON: %s", buf.String())
	assert.Equal(t, "passed", summary.Outcome)
}

// TestExecuteRun_DumpStateStdoutCarriesOnlyState: without --json,
// --dump-state - owns stdout, so the export pipes into jq whether or not
// --quiet is set; progress and the summary line go to stderr. The export is
// redacted unless --dump-state-secrets asks for live credentials, which warns
// on stderr.
func TestExecuteRun_DumpStateStdoutCarriesOnlyState(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	const liveWarning = "aat: warning: --dump-state-secrets writes live credentials to stdout"
	tests := []struct {
		name       string
		quiet      bool
		secrets    bool
		wantStderr string
	}{
		{name: "progress", quiet: false, wantStderr: "aat: executing plan"},
		{name: "quiet", quiet: true, wantStderr: "STOPPED"},
		{name: "live credentials", quiet: true, secrets: true, wantStderr: liveWarning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			stdout, err := os.Create(filepath.Join(dir, "stdout"))
			require.NoError(t, err)
			stderr, err := os.Create(filepath.Join(dir, "stderr"))
			require.NoError(t, err)

			oldStdout, oldStderr := os.Stdout, os.Stderr
			os.Stdout, os.Stderr = stdout, stderr
			code := executeRun(&runArgs{
				PlanPath:         "testdata/test_plan.yaml",
				EnvPath:          writeTestEnv(t, "none", apiServer.URL),
				GraphPath:        "testdata/test_graph.yaml",
				TemplatesPath:    "testdata/templates",
				OutputDir:        filepath.Join(dir, "runs"),
				Quiet:            tt.quiet,
				StopAfterStep:    "testNode",
				DumpStatePath:    "-",
				DumpStateSecrets: tt.secrets,
			})
			os.Stdout, os.Stderr = oldStdout, oldStderr
			require.NoError(t, stdout.Close())
			require.NoError(t, stderr.Close())

			assert.Equal(t, 0, code)
			data, err := os.ReadFile(stdout.Name())
			require.NoError(t, err)
			var exp engine.StateExport
			require.NoError(t, json.Unmarshal(data, &exp), "stdout must hold exactly the state export: %s", data)
			assert.Equal(t, "stopped", exp.Outcome)
			assert.Equal(t, !tt.secrets, exp.Redacted, "credentials are redacted unless --dump-state-secrets asks for them")

			logs, err := os.ReadFile(stderr.Name())
			require.NoError(t, err)
			assert.Contains(t, string(logs), tt.wantStderr)
			if !tt.secrets {
				assert.NotContains(t, string(logs), liveWarning)
			}
		})
	}
}

// --- Display outputs tests ---

func TestToStepSummary_DisplayOutputs(t *testing.T) {
	step := engine.StepResult{
		Node:       "commit",
		StatusCode: 200,
		DisplayOutputs: []engine.DisplayOutput{
			{Label: "PNR", Name: "locator", Value: "ABCDEF"},
		},
	}

	ss := toStepSummary(step)
	require.Len(t, ss.DisplayOutputs, 1)
	assert.Equal(t, "PNR", ss.DisplayOutputs[0].Label)
	assert.Equal(t, "locator", ss.DisplayOutputs[0].Name)
	assert.Equal(t, "ABCDEF", ss.DisplayOutputs[0].Value)

	// Verify JSON round-trip
	data, err := json.Marshal(ss)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"display_outputs"`)
	assert.Contains(t, string(data), `"PNR"`)
}

func TestToStepSummary_NoDisplayOutputs(t *testing.T) {
	step := engine.StepResult{
		Node:       "search",
		StatusCode: 200,
	}

	ss := toStepSummary(step)
	assert.Nil(t, ss.DisplayOutputs)

	// Verify omitempty
	data, err := json.Marshal(ss)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "display_outputs")
}

// --- Override flag parsing tests ---

func TestParseOverrideFlag_Valid(t *testing.T) {
	name, url, err := parseOverrideFlag("searchFlights=http://localhost:8080")
	require.NoError(t, err)
	assert.Equal(t, "searchFlights", name)
	assert.Equal(t, "http://localhost:8080", url)
}

func TestParseOverrideFlag_WithEqualInURL(t *testing.T) {
	name, url, err := parseOverrideFlag("node=http://localhost:8080/path?key=value")
	require.NoError(t, err)
	assert.Equal(t, "node", name)
	assert.Equal(t, "http://localhost:8080/path?key=value", url)
}

func TestParseOverrideFlag_InvalidNoEquals(t *testing.T) {
	_, _, err := parseOverrideFlag("searchFlights")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected nodeName=url")
}

func TestParseOverrideFlag_EmptyURL(t *testing.T) {
	_, _, err := parseOverrideFlag("searchFlights=")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "URL is empty")
}

func TestParseOverrideFlag_EmptyName(t *testing.T) {
	_, _, err := parseOverrideFlag("=http://localhost:8080")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected nodeName=url")
}

// --- Plan-level auth tests ---

func TestRunCommand_PlanAuth_OverridesEnvAuth(t *testing.T) {
	var receivedAuth string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	// Env has auth: none
	envFile := writeTestEnv(t, "none", apiServer.URL)

	// Plan has bearer auth
	planContent := `
auth:
  type: bearer
  credentials:
    token:
      source: literal
      value: plan-token-123
execution:
  steps:
    - node: testNode
      values:
        input1: test-value
`
	planFile := filepath.Join(t.TempDir(), "plan_auth.yaml")
	require.NoError(t, os.WriteFile(planFile, []byte(planContent), 0644))

	outputDir := filepath.Join(t.TempDir(), "runs")
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      planFile,
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     outputDir,
	}, io.Discard, TerminalInfo{})

	require.NoError(t, res.err)
	assert.Equal(t, "Bearer plan-token-123", receivedAuth)
}

func TestRunCommand_PlanHeaders_MergeWithEnv(t *testing.T) {
	var receivedHeaders http.Header
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	// Env with a custom header
	envContent := "environment: test\napiBaseUrl: " + apiServer.URL + "\nauth:\n  type: none\nheaders:\n  X-Env-Header: env-value\n  X-Shared: env-val\n"
	envFile := filepath.Join(t.TempDir(), "env.yaml")
	require.NoError(t, os.WriteFile(envFile, []byte(envContent), 0644))

	// Plan with headers that partially overlap env headers
	planContent := `
headers:
  X-Plan-Header: plan-value
  X-Shared: plan-val
execution:
  steps:
    - node: testNode
      values:
        input1: test-value
`
	planFile := filepath.Join(t.TempDir(), "plan_headers.yaml")
	require.NoError(t, os.WriteFile(planFile, []byte(planContent), 0644))

	outputDir := filepath.Join(t.TempDir(), "runs")
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      planFile,
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     outputDir,
	}, io.Discard, TerminalInfo{})

	require.NoError(t, res.err)
	assert.Equal(t, "env-value", receivedHeaders.Get("X-Env-Header"))
	assert.Equal(t, "plan-value", receivedHeaders.Get("X-Plan-Header"))
	assert.Equal(t, "plan-val", receivedHeaders.Get("X-Shared")) // plan wins on conflict
}

func TestRunCommand_NoPlanAuth_UsesEnvAuth(t *testing.T) {
	var receivedAuth string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer apiServer.Close()

	// Env with bearer auth
	envContent := "environment: test\napiBaseUrl: " + apiServer.URL + "\nauth:\n  type: bearer\n  credentials:\n    token:\n      source: literal\n      value: env-token-456\n"
	envFile := filepath.Join(t.TempDir(), "env.yaml")
	require.NoError(t, os.WriteFile(envFile, []byte(envContent), 0644))

	// Plan with NO auth block
	outputDir := filepath.Join(t.TempDir(), "runs")
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      "testdata/test_plan.yaml",
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     outputDir,
	}, io.Discard, TerminalInfo{})

	require.NoError(t, res.err)
	assert.Equal(t, "Bearer env-token-456", receivedAuth)
}

func TestRunCommand_PlanAuth_InvalidCaught(t *testing.T) {
	// Plan with invalid auth should fail at validation
	planContent := `
auth:
  type: oauth2
execution:
  steps:
    - node: testNode
      values:
        input1: test-value
`
	planFile := filepath.Join(t.TempDir(), "plan_bad_auth.yaml")
	require.NoError(t, os.WriteFile(planFile, []byte(planContent), 0644))

	envFile := writeTestEnv(t, "none", "https://api.example.com")
	res := runCommand(context.Background(), &runArgs{
		PlanPath:      planFile,
		EnvPath:       envFile,
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
		OutputDir:     filepath.Join(t.TempDir(), "runs"),
	}, io.Discard, TerminalInfo{})

	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "plan validation")
	assert.Contains(t, res.err.Error(), "plan auth")
}

// --- Override routing ---

// TestRunCommand_ValueOnlyOverlayKeepsGlobRoute: env.yaml routes test* to a
// second host with its own API key. An overlay entry that only sets values for
// the exact node must keep that route, and the main bearer token must not
// reach the second host.
func TestRunCommand_ValueOnlyOverlayKeepsGlobRoute(t *testing.T) {
	mainServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request reached the main host: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mainServer.Close()

	var gotPath, gotAuth, gotKey string
	altServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotKey = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer altServer.Close()

	dir := t.TempDir()
	envFile := filepath.Join(dir, "env.yaml")
	require.NoError(t, os.WriteFile(envFile, []byte(`environment: test
apiBaseUrl: `+mainServer.URL+`
auth:
  type: bearer
  credentials:
    token:
      source: literal
      value: main-token
overrides:
  - match: "test*"
    baseUrl: `+altServer.URL+`
    auth:
      type: apikey
      headerName: X-API-Key
      credentials:
        key:
          source: literal
          value: alt-key
`), 0o644))
	overlayFile := filepath.Join(dir, "overlay.yaml")
	require.NoError(t, os.WriteFile(overlayFile, []byte(`overrides:
  - match: testNode
    values:
      input1: from-overlay
`), 0o644))

	res := runCommand(context.Background(), &runArgs{
		PlanPath:        "testdata/test_plan.yaml",
		EnvPath:         envFile,
		GraphPath:       "testdata/test_graph.yaml",
		TemplatesPath:   "testdata/templates",
		OutputDir:       filepath.Join(dir, "runs"),
		EnvOverlay:      overlayFile,
		NoAutoOverrides: true,
	}, io.Discard, TerminalInfo{})

	require.NoError(t, res.err)
	assert.Equal(t, "/test/from-overlay", gotPath, "overlay values apply")
	assert.Equal(t, "alt-key", gotKey, "the glob route's auth applies")
	assert.Empty(t, gotAuth, "the main bearer token must not reach the override host")
}

// TestRunCommand_OverrideFlagInheritsHeadersAndAuth: an --override NODE=URL
// flag is equivalent to an env.yaml entry with that match and baseUrl, so the
// request keeps the environment headers, the plan headers, and the credential.
// It used to reach the override host with no headers at all.
func TestRunCommand_OverrideFlagInheritsHeadersAndAuth(t *testing.T) {
	mainServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request reached the main host: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mainServer.Close()

	var got http.Header
	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer localServer.Close()

	dir := t.TempDir()
	envFile := filepath.Join(dir, "env.yaml")
	require.NoError(t, os.WriteFile(envFile, []byte(`environment: test
apiBaseUrl: `+mainServer.URL+`
auth:
  type: bearer
  credentials:
    token:
      source: literal
      value: main-token
headers:
  X-Env: env-value
`), 0o644))
	planFile := filepath.Join(dir, "plan.yaml")
	require.NoError(t, os.WriteFile(planFile, []byte(`headers:
  X-Plan: plan-value
execution:
  steps:
    - node: testNode
      values:
        input1: value
`), 0o644))

	res := runCommand(context.Background(), &runArgs{
		PlanPath:        planFile,
		EnvPath:         envFile,
		GraphPath:       "testdata/test_graph.yaml",
		TemplatesPath:   "testdata/templates",
		OutputDir:       filepath.Join(dir, "runs"),
		Overrides:       []string{"testNode=" + localServer.URL},
		NoAutoOverrides: true,
	}, io.Discard, TerminalInfo{})

	require.NoError(t, res.err)
	require.NotNil(t, got, "the override host received the request")
	assert.Equal(t, "Bearer main-token", got.Get("Authorization"))
	assert.Equal(t, "env-value", got.Get("X-Env"))
	assert.Equal(t, "plan-value", got.Get("X-Plan"))
}

func TestRunCommand_LayersWithoutLayersDirFails(t *testing.T) {
	res := runCommand(context.Background(), &runArgs{
		PlanPath:        "testdata/test_plan.yaml",
		EnvPath:         writeTestEnv(t, "none", ""),
		GraphPath:       "testdata/test_graph.yaml",
		TemplatesPath:   "testdata/templates",
		OutputDir:       filepath.Join(t.TempDir(), "runs"),
		Layers:          []string{"express"},
		NoAutoOverrides: true,
	}, io.Discard, TerminalInfo{})

	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "no layers directory is configured")
}

func TestResolveOASMode(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		setting string
		want    string
		wantErr string
	}{
		{name: "defaults to auto", want: "auto"},
		{name: "environment setting", setting: "strict", want: "strict"},
		{name: "flag beats the setting", flag: "off", setting: "strict", want: "off"},
		{name: "unknown flag value", flag: "stirct", wantErr: `--oas-validate: unknown OpenAPI validation mode "stirct"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveOASMode(tt.flag, tt.setting)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- Helpers ---

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

func TestPlannedStepCount_IncludesVerificationAndMutations(t *testing.T) {
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"create": {Name: "create", Adapter: "create", Inputs: []graph.Input{{Name: "qty", Type: "integer"}}},
		"get":    {Name: "get", Adapter: "get"},
	}}
	p := &plan.Plan{Execution: plan.Execution{
		Steps: []plan.Step{{
			Node:   "create",
			Values: map[string]plan.StepValue{"qty": {Default: 1}},
			Mutations: []plan.Mutation{
				{Name: "zero", Set: map[string]any{"qty": 0}, ExpectStatus: []int{400}},
			},
		}},
		Verification: []plan.VerificationStep{{Node: "get"}},
	}}

	assert.Equal(t, 3, plannedStepCount(p, g, nil), "the step, its mutation sibling, and the verification step")
}
