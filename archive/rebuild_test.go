package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRebuildSummaries_Basic(t *testing.T) {
	dir := t.TempDir()

	// Create a run directory with archive.json but no summary.json
	runDir := filepath.Join(dir, "run-test-001")
	require.NoError(t, os.MkdirAll(runDir, 0o755))

	a := &Archive{
		Metadata: ArchiveMetadata{
			Version:   "1",
			RunID:     "run-test-001",
			Timestamp: time.Date(2026, 2, 28, 10, 0, 0, 0, time.UTC),
		},
		Steps: []StepRecord{
			{Node: "step1", DurationMs: 100, Inputs: map[string]any{},
				OASValidation: &OASValidationRecord{
					Response: &OASPayloadRecord{Valid: false, Errors: []OASSchemaError{{Path: "/a", Message: "bad"}}},
				}},
		},
		Result: ArchiveResult{Outcome: "passed"},
	}
	data, err := json.MarshalIndent(a, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "archive.json"), data, 0o644))

	var logs []string
	logf := func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}

	result, err := RebuildSummaries(dir, logf)
	require.NoError(t, err)

	assert.Equal(t, 1, result.Rebuilt)
	assert.Equal(t, 0, result.Errors)

	// Verify rebuilt summary has issues
	s, err := ReadSummary(filepath.Join(runDir, "summary.json"))
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"oas": 1}, s.Issues)
}

func TestRebuildSummaries_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()

	runDir := filepath.Join(dir, "run-test-002")
	require.NoError(t, os.MkdirAll(runDir, 0o755))

	// Write an old summary without issues
	oldSummary := &RunSummary{RunID: "run-test-002", Outcome: "passed"}
	require.NoError(t, WriteSummary(oldSummary, filepath.Join(runDir, "summary.json")))

	// Write archive with OAS errors
	a := &Archive{
		Metadata: ArchiveMetadata{RunID: "run-test-002"},
		Steps: []StepRecord{
			{Node: "step1", DurationMs: 100, Inputs: map[string]any{},
				OASValidation: &OASValidationRecord{
					Request: &OASPayloadRecord{Valid: false, Errors: []OASSchemaError{{Path: "/x", Message: "err"}, {Path: "/y", Message: "err2"}}},
				}},
		},
		Result: ArchiveResult{Outcome: "passed"},
	}
	data, err := json.MarshalIndent(a, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "archive.json"), data, 0o644))

	result, err := RebuildSummaries(dir, func(string, ...any) {})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Rebuilt)

	// Verify rebuilt summary now has issues
	s, err := ReadSummary(filepath.Join(runDir, "summary.json"))
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"oas": 2}, s.Issues)
}

func TestRebuildSummaries_BatchNested(t *testing.T) {
	dir := t.TempDir()

	// Create a batch directory with nested run directories
	batchDir := filepath.Join(dir, "batch-test-001")
	runDir := filepath.Join(batchDir, "run-nested-001")
	require.NoError(t, os.MkdirAll(runDir, 0o755))

	// Write batch.json to identify as batch
	batchJSON := `{"metadata":{"batchId":"batch-test-001"},"runs":[],"result":{"outcome":"passed"}}`
	require.NoError(t, os.WriteFile(filepath.Join(batchDir, "batch.json"), []byte(batchJSON), 0o644))

	// Write nested archive
	a := &Archive{
		Metadata: ArchiveMetadata{RunID: "run-nested-001"},
		Steps: []StepRecord{
			{Node: "step1", DurationMs: 50, Inputs: map[string]any{}},
		},
		Result: ArchiveResult{Outcome: "passed"},
	}
	data, err := json.MarshalIndent(a, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "archive.json"), data, 0o644))

	result, err := RebuildSummaries(dir, func(string, ...any) {})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Rebuilt)

	// Verify summary was written
	s, err := ReadSummary(filepath.Join(runDir, "summary.json"))
	require.NoError(t, err)
	assert.Equal(t, "run-nested-001", s.RunID)
	assert.Nil(t, s.Issues) // no OAS errors
}

func TestRebuildSummaries_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	result, err := RebuildSummaries(dir, func(string, ...any) {})
	require.NoError(t, err)

	assert.Equal(t, 0, result.Rebuilt)
	assert.Equal(t, 0, result.Errors)
}

func TestRebuildSummaries_SkipsNonRunDirs(t *testing.T) {
	dir := t.TempDir()

	// Directory without archive.json
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "random-dir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "random-dir", "notes.txt"), []byte("hello"), 0o644))

	result, err := RebuildSummaries(dir, func(string, ...any) {})
	require.NoError(t, err)

	assert.Equal(t, 0, result.Rebuilt)
	assert.Equal(t, 1, result.Skipped)
}
