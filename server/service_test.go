package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test helpers ---

func makeArchive(runID, outcome string, steps ...archive.StepRecord) *archive.Archive {
	return &archive.Archive{
		Metadata: archive.ArchiveMetadata{
			Version:   "1.0.0",
			RunID:     runID,
			Timestamp: time.Date(2026, 2, 10, 14, 30, 0, 0, time.UTC),
		},
		Steps: steps,
		Result: archive.ArchiveResult{
			Outcome: outcome,
		},
	}
}

func makeStep(node string, status int, durationMs int64) archive.StepRecord {
	return archive.StepRecord{
		Node:       node,
		DurationMs: durationMs,
		Inputs:     map[string]any{"input1": "val1"},
		Response: &archive.ResponseRecord{
			Status: status,
			Body:   json.RawMessage(`{"result":"ok"}`),
		},
	}
}

func writeArchive(t *testing.T, dir string, a *archive.Archive) {
	t.Helper()
	path := filepath.Join(dir, a.Metadata.RunID, "archive.json")
	err := archive.Write(a, path)
	require.NoError(t, err)
}

// --- ListRuns ---

func TestListRuns_SortedNewestFirst(t *testing.T) {
	dir := t.TempDir()

	a1 := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node1", 200, 100))
	a2 := makeArchive("run-20260102-100000-aaaa0002", "passed", makeStep("node2", 200, 200))
	a3 := makeArchive("run-20260103-100000-aaaa0003", "failed", makeStep("node3", 500, 300))

	writeArchive(t, dir, a1)
	writeArchive(t, dir, a2)
	writeArchive(t, dir, a3)

	svc := NewArchiveService(dir)
	runs, err := svc.ListRuns(0)
	require.NoError(t, err)
	require.Len(t, runs, 3)

	assert.Equal(t, "run-20260103-100000-aaaa0003", runs[0].RunID)
	assert.Equal(t, "run-20260102-100000-aaaa0002", runs[1].RunID)
	assert.Equal(t, "run-20260101-100000-aaaa0001", runs[2].RunID)

	assert.Equal(t, "failed", runs[0].Outcome)
	assert.Equal(t, 1, runs[0].StepCount)
}

func TestListRuns_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	svc := NewArchiveService(dir)
	runs, err := svc.ListRuns(0)
	assert.NoError(t, err)
	assert.Nil(t, runs)
}

func TestListRuns_NotConfigured(t *testing.T) {
	svc := NewArchiveService("")
	_, err := svc.ListRuns(0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestListRuns_NonexistentDirectory(t *testing.T) {
	svc := NewArchiveService("/tmp/nonexistent-aat-test-dir-12345")
	runs, err := svc.ListRuns(0)
	assert.NoError(t, err)
	assert.Nil(t, runs)
}

func TestListRuns_Limit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		a := makeArchive(
			"run-2026010"+string(rune('1'+i))+"-100000-aaaa000"+string(rune('1'+i)),
			"passed", makeStep("node", 200, 100),
		)
		writeArchive(t, dir, a)
	}

	svc := NewArchiveService(dir)
	runs, err := svc.ListRuns(2)
	require.NoError(t, err)
	assert.Len(t, runs, 2)
}

func TestListRuns_LimitZero(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		a := makeArchive(
			"run-2026010"+string(rune('1'+i))+"-100000-aaaa000"+string(rune('1'+i)),
			"passed", makeStep("node", 200, 100),
		)
		writeArchive(t, dir, a)
	}

	svc := NewArchiveService(dir)
	runs, err := svc.ListRuns(0)
	require.NoError(t, err)
	assert.Len(t, runs, 3)
}

func TestListRuns_SkipsNonRunDirs(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a)

	// Create non-run directories
	require.NoError(t, createDir(filepath.Join(dir, "other-dir")))
	require.NoError(t, createDir(filepath.Join(dir, "traces")))

	svc := NewArchiveService(dir)
	runs, err := svc.ListRuns(0)
	require.NoError(t, err)
	assert.Len(t, runs, 1)
	assert.Equal(t, "run-20260101-100000-aaaa0001", runs[0].RunID)
}

func TestListRuns_SkipsCorruptArchive(t *testing.T) {
	dir := t.TempDir()

	good := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, good)

	// Write corrupt archive
	corruptDir := filepath.Join(dir, "run-20260102-100000-aaaa0002")
	require.NoError(t, createDir(corruptDir))
	require.NoError(t, writeFile(filepath.Join(corruptDir, "archive.json"), []byte("not json")))

	svc := NewArchiveService(dir)
	runs, err := svc.ListRuns(0)
	require.NoError(t, err)
	assert.Len(t, runs, 1)
	assert.Equal(t, "run-20260101-100000-aaaa0001", runs[0].RunID)
}

func TestListRuns_StepCounts(t *testing.T) {
	dir := t.TempDir()

	passStep1 := makeStep("search", 200, 100)
	passStep2 := makeStep("book", 200, 200)
	failStep := makeStep("cancel", 500, 50)
	failStep.Error = "server error"

	a := makeArchive("run-20260101-100000-aaaa0001", "failed", passStep1, passStep2, failStep)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	runs, err := svc.ListRuns(0)
	require.NoError(t, err)
	require.Len(t, runs, 1)

	assert.Equal(t, 3, runs[0].StepCount)
	assert.Equal(t, 2, runs[0].PassedCount)
	assert.Equal(t, 1, runs[0].FailedCount)
}

func TestListRuns_DurationIncludesCleanup(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("search", 200, 100))
	a.Cleanup = []archive.StepRecord{makeStep("cleanup", 200, 50)}
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	runs, err := svc.ListRuns(0)
	require.NoError(t, err)
	require.Len(t, runs, 1)

	assert.Equal(t, int64(150), runs[0].DurationMs)
	// StepCount should NOT include cleanup
	assert.Equal(t, 1, runs[0].StepCount)
}

func TestListRuns_PlanNameFromPrompt(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	a.Metadata.Plan = &plan.Plan{
		Metadata: plan.Metadata{Prompt: "book a flight from rome to new york"},
		Intent:   plan.Intent{Description: "booking flow", Goal: "test booking"},
	}
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	runs, err := svc.ListRuns(0)
	require.NoError(t, err)
	assert.Equal(t, "book a flight from rome to new york", runs[0].PlanName)
}

func TestListRuns_PlanNameFallback(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	a.Metadata.Plan = &plan.Plan{
		Intent: plan.Intent{Description: "booking flow", Goal: "test booking"},
	}
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	runs, err := svc.ListRuns(0)
	require.NoError(t, err)
	assert.Equal(t, "booking flow", runs[0].PlanName)
}

func TestListRuns_NoPlan(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	runs, err := svc.ListRuns(0)
	require.NoError(t, err)
	assert.Equal(t, "", runs[0].PlanName)
}

// --- LatestRunID ---

func TestLatestRunID_ReturnsNewest(t *testing.T) {
	dir := t.TempDir()

	writeArchive(t, dir, makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("n", 200, 100)))
	writeArchive(t, dir, makeArchive("run-20260103-100000-aaaa0003", "passed", makeStep("n", 200, 100)))
	writeArchive(t, dir, makeArchive("run-20260102-100000-aaaa0002", "passed", makeStep("n", 200, 100)))

	svc := NewArchiveService(dir)
	id, err := svc.LatestRunID()
	require.NoError(t, err)
	assert.Equal(t, "run-20260103-100000-aaaa0003", id)
}

func TestLatestRunID_Empty(t *testing.T) {
	dir := t.TempDir()
	svc := NewArchiveService(dir)
	id, err := svc.LatestRunID()
	assert.NoError(t, err)
	assert.Equal(t, "", id)
}

// --- stepPassed ---

func TestStepPassed(t *testing.T) {
	tests := []struct {
		name   string
		step   archive.StepRecord
		passed bool
	}{
		{
			name:   "clean step",
			step:   makeStep("node", 200, 100),
			passed: true,
		},
		{
			name: "step with error",
			step: archive.StepRecord{
				Node:  "node",
				Error: "something broke",
			},
			passed: false,
		},
		{
			name: "validation failed",
			step: archive.StepRecord{
				Node:       "node",
				Validation: &archive.ValidationRecord{Passed: false},
			},
			passed: false,
		},
		{
			name: "validation passed",
			step: archive.StepRecord{
				Node:       "node",
				Validation: &archive.ValidationRecord{Passed: true},
			},
			passed: true,
		},
		{
			name: "expect failure not passed",
			step: archive.StepRecord{
				Node:          "node",
				ExpectFailure: &archive.ExpectFailureRecord{Passed: false},
			},
			passed: false,
		},
		{
			name: "expect failure passed",
			step: archive.StepRecord{
				Node:          "node",
				ExpectFailure: &archive.ExpectFailureRecord{Passed: true},
			},
			passed: true,
		},
		{
			name: "response body error",
			step: archive.StepRecord{
				Node:              "node",
				ResponseBodyError: &archive.ResponseBodyErrorRecord{Message: "error in body"},
			},
			passed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.passed, stepPassed(tt.step))
		})
	}
}

// --- extractPlanName ---

func TestExtractPlanName(t *testing.T) {
	tests := []struct {
		name     string
		archive  *archive.Archive
		expected string
	}{
		{
			name:     "nil plan",
			archive:  makeArchive("run-1", "passed"),
			expected: "",
		},
		{
			name: "prompt set",
			archive: &archive.Archive{
				Metadata: archive.ArchiveMetadata{
					Plan: &plan.Plan{
						Metadata: plan.Metadata{Prompt: "book a flight"},
						Intent:   plan.Intent{Description: "desc", Goal: "goal"},
					},
				},
			},
			expected: "book a flight",
		},
		{
			name: "description fallback",
			archive: &archive.Archive{
				Metadata: archive.ArchiveMetadata{
					Plan: &plan.Plan{
						Intent: plan.Intent{Description: "booking flow", Goal: "goal"},
					},
				},
			},
			expected: "booking flow",
		},
		{
			name: "goal fallback",
			archive: &archive.Archive{
				Metadata: archive.ArchiveMetadata{
					Plan: &plan.Plan{
						Intent: plan.Intent{Goal: "test booking"},
					},
				},
			},
			expected: "test booking",
		},
		{
			name: "all empty",
			archive: &archive.Archive{
				Metadata: archive.ArchiveMetadata{
					Plan: &plan.Plan{},
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractPlanName(tt.archive))
		})
	}
}

// --- file helpers ---

func createDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
