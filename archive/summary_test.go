package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRunSummary_AllPassed(t *testing.T) {
	a := &Archive{
		Metadata: ArchiveMetadata{
			RunID:     "run-20260222-100000-abcd",
			Timestamp: time.Date(2026, 2, 22, 10, 0, 0, 0, time.UTC),
			Plan: &plan.Plan{
				Metadata: plan.Metadata{Prompt: "book a flight"},
			},
		},
		Steps: []StepRecord{
			{Node: "search", DurationMs: 100, Inputs: map[string]any{},
				Response: &ResponseRecord{Status: 200},
				Validation: &ValidationRecord{Passed: true}},
			{Node: "book", DurationMs: 200, Inputs: map[string]any{},
				Response: &ResponseRecord{Status: 200},
				Validation: &ValidationRecord{Passed: true}},
		},
		Cleanup: []StepRecord{
			{Node: "cancel", DurationMs: 50, Inputs: map[string]any{}},
		},
		Result: ArchiveResult{Outcome: "passed"},
	}

	s := BuildRunSummary(a)

	assert.Equal(t, "run-20260222-100000-abcd", s.RunID)
	assert.Equal(t, "passed", s.Outcome)
	assert.Equal(t, 2, s.StepCount)
	assert.Equal(t, 2, s.PassedCount)
	assert.Equal(t, 0, s.FailedCount)
	assert.Equal(t, int64(350), s.DurationMs) // 100 + 200 + 50 (cleanup)
	assert.Equal(t, "book a flight", s.PlanName)
}

func TestBuildRunSummary_MixedPassFail(t *testing.T) {
	a := &Archive{
		Metadata: ArchiveMetadata{
			RunID:     "run-mixed",
			Timestamp: time.Date(2026, 2, 22, 10, 0, 0, 0, time.UTC),
		},
		Steps: []StepRecord{
			{Node: "step1", DurationMs: 100, Inputs: map[string]any{},
				Validation: &ValidationRecord{Passed: true}},
			{Node: "step2", DurationMs: 200, Inputs: map[string]any{},
				Validation: &ValidationRecord{Passed: false}},
			{Node: "step3", DurationMs: 300, Inputs: map[string]any{},
				Error: "connection refused"},
		},
		Result: ArchiveResult{Outcome: "failed"},
	}

	s := BuildRunSummary(a)

	assert.Equal(t, 3, s.StepCount)
	assert.Equal(t, 1, s.PassedCount)
	assert.Equal(t, 2, s.FailedCount)
	assert.Equal(t, int64(600), s.DurationMs)
	assert.Equal(t, "failed", s.Outcome)
}

func TestBuildRunSummary_ExpectFailure(t *testing.T) {
	a := &Archive{
		Metadata: ArchiveMetadata{RunID: "run-ef"},
		Steps: []StepRecord{
			{Node: "authCheck", DurationMs: 50, Inputs: map[string]any{},
				ExpectFailure: &ExpectFailureRecord{Expected: []int{401}, Actual: 401, Passed: true}},
			{Node: "badCheck", DurationMs: 50, Inputs: map[string]any{},
				ExpectFailure: &ExpectFailureRecord{Expected: []int{401}, Actual: 200, Passed: false}},
		},
		Result: ArchiveResult{Outcome: "failed"},
	}

	s := BuildRunSummary(a)

	assert.Equal(t, 1, s.PassedCount)
	assert.Equal(t, 1, s.FailedCount)
}

func TestBuildRunSummary_ResponseBodyError(t *testing.T) {
	a := &Archive{
		Metadata: ArchiveMetadata{RunID: "run-rbe"},
		Steps: []StepRecord{
			{Node: "step1", DurationMs: 100, Inputs: map[string]any{},
				ResponseBodyError: &ResponseBodyErrorRecord{
					RulePath: "errors", Rule: "errors.#", Message: "has errors",
				}},
		},
		Result: ArchiveResult{Outcome: "failed"},
	}

	s := BuildRunSummary(a)

	assert.Equal(t, 0, s.PassedCount)
	assert.Equal(t, 1, s.FailedCount)
}

func TestBuildRunSummary_PlanNameFallback(t *testing.T) {
	tests := []struct {
		name     string
		plan     *plan.Plan
		expected string
	}{
		{
			name:     "nil plan",
			plan:     nil,
			expected: "",
		},
		{
			name:     "prompt",
			plan:     &plan.Plan{Metadata: plan.Metadata{Prompt: "test prompt"}},
			expected: "test prompt",
		},
		{
			name: "description fallback",
			plan: &plan.Plan{
				Intent: plan.Intent{Description: "test description"},
			},
			expected: "test description",
		},
		{
			name: "goal fallback",
			plan: &plan.Plan{
				Intent: plan.Intent{Goal: "test goal"},
			},
			expected: "test goal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Archive{
				Metadata: ArchiveMetadata{RunID: "run-test", Plan: tt.plan},
				Result:   ArchiveResult{Outcome: "passed"},
			}
			s := BuildRunSummary(a)
			assert.Equal(t, tt.expected, s.PlanName)
		})
	}
}

func TestBuildRunSummary_Attempts(t *testing.T) {
	a := &Archive{
		Metadata: ArchiveMetadata{
			RunID:         "run-retry",
			Attempt:       2,
			TotalAttempts: 3,
		},
		Result: ArchiveResult{Outcome: "passed"},
	}

	s := BuildRunSummary(a)

	assert.Equal(t, 2, s.Attempt)
	assert.Equal(t, 3, s.TotalAttempts)
}

func TestWriteReadSummary_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "summary.json")

	original := &RunSummary{
		RunID:       "run-20260222-100000-abcd",
		Timestamp:   time.Date(2026, 2, 22, 10, 0, 0, 0, time.UTC),
		Outcome:     "passed",
		StepCount:   3,
		PassedCount: 2,
		FailedCount: 1,
		DurationMs:  1500,
		PlanName:    "book a flight",
	}

	err := WriteSummary(original, path)
	require.NoError(t, err)

	loaded, err := ReadSummary(path)
	require.NoError(t, err)

	assert.Equal(t, original.RunID, loaded.RunID)
	assert.Equal(t, original.Timestamp, loaded.Timestamp)
	assert.Equal(t, original.Outcome, loaded.Outcome)
	assert.Equal(t, original.StepCount, loaded.StepCount)
	assert.Equal(t, original.PassedCount, loaded.PassedCount)
	assert.Equal(t, original.FailedCount, loaded.FailedCount)
	assert.Equal(t, original.DurationMs, loaded.DurationMs)
	assert.Equal(t, original.PlanName, loaded.PlanName)
}

func TestReadSummary_FileNotFound(t *testing.T) {
	_, err := ReadSummary("/nonexistent/path/summary.json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading summary file")
}

func TestReadSummary_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "summary.json")
	err := os.WriteFile(path, []byte("not json"), 0o644)
	require.NoError(t, err)

	_, err = ReadSummary(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshalling summary")
}

func TestWriteSummary_CreatesParentDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "summary.json")

	s := &RunSummary{RunID: "test", Outcome: "passed"}
	err := WriteSummary(s, path)
	require.NoError(t, err)

	_, err = os.Stat(path)
	assert.NoError(t, err)
}

func TestWriteSummary_JSONIsIndented(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "summary.json")

	s := &RunSummary{RunID: "test", Outcome: "passed"}
	err := WriteSummary(s, path)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "\n")
	assert.Contains(t, string(data), "  ")
}

func TestBuildRunSummary_NoSteps(t *testing.T) {
	a := &Archive{
		Metadata: ArchiveMetadata{RunID: "run-empty"},
		Result:   ArchiveResult{Outcome: "passed"},
	}

	s := BuildRunSummary(a)

	assert.Equal(t, 0, s.StepCount)
	assert.Equal(t, 0, s.PassedCount)
	assert.Equal(t, 0, s.FailedCount)
	assert.Equal(t, int64(0), s.DurationMs)
}

func TestStepRecordPassed(t *testing.T) {
	tests := []struct {
		name     string
		step     StepRecord
		expected bool
	}{
		{
			name:     "passing step",
			step:     StepRecord{Validation: &ValidationRecord{Passed: true}},
			expected: true,
		},
		{
			name:     "step with error",
			step:     StepRecord{Error: "failed"},
			expected: false,
		},
		{
			name:     "failed validation",
			step:     StepRecord{Validation: &ValidationRecord{Passed: false}},
			expected: false,
		},
		{
			name:     "failed expect-failure",
			step:     StepRecord{ExpectFailure: &ExpectFailureRecord{Passed: false}},
			expected: false,
		},
		{
			name:     "passed expect-failure",
			step:     StepRecord{ExpectFailure: &ExpectFailureRecord{Passed: true}},
			expected: true,
		},
		{
			name:     "response body error",
			step:     StepRecord{ResponseBodyError: &ResponseBodyErrorRecord{RulePath: "errors"}},
			expected: false,
		},
		{
			name:     "no validation or errors",
			step:     StepRecord{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, stepRecordPassed(tt.step))
		})
	}
}

func TestWriteArchive_CreatesSummaryJSON(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "run-test-summary")
	archivePath := filepath.Join(runDir, "archive.json")

	a := &Archive{
		Metadata: ArchiveMetadata{
			Version:   "1",
			RunID:     "run-test-summary",
			Timestamp: time.Date(2026, 2, 22, 10, 0, 0, 0, time.UTC),
			Plan:      &plan.Plan{Metadata: plan.Metadata{Prompt: "test prompt"}},
		},
		Steps: []StepRecord{
			{Node: "step1", DurationMs: 100, Inputs: map[string]any{},
				Response:   &ResponseRecord{Status: 200},
				Validation: &ValidationRecord{Passed: true}},
			{Node: "step2", DurationMs: 200, Inputs: map[string]any{},
				Response:   &ResponseRecord{Status: 500},
				Validation: &ValidationRecord{Passed: false}},
		},
		Result: ArchiveResult{Outcome: "failed"},
	}

	err := Write(a, archivePath)
	require.NoError(t, err)

	// Verify archive.json exists.
	_, err = os.Stat(archivePath)
	require.NoError(t, err)

	// Verify summary.json exists alongside it.
	summaryPath := filepath.Join(runDir, "summary.json")
	_, err = os.Stat(summaryPath)
	require.NoError(t, err)

	// Verify summary contents.
	data, err := os.ReadFile(summaryPath)
	require.NoError(t, err)

	var summary RunSummary
	err = json.Unmarshal(data, &summary)
	require.NoError(t, err)

	assert.Equal(t, "run-test-summary", summary.RunID)
	assert.Equal(t, "failed", summary.Outcome)
	assert.Equal(t, 2, summary.StepCount)
	assert.Equal(t, 1, summary.PassedCount)
	assert.Equal(t, 1, summary.FailedCount)
	assert.Equal(t, int64(300), summary.DurationMs)
	assert.Equal(t, "test prompt", summary.PlanName)
}
