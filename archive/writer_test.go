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

func TestWriteAndRead_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	ts := time.Date(2026, 2, 7, 14, 30, 0, 0, time.UTC)
	original := &Archive{
		Metadata: ArchiveMetadata{
			Version:      "1",
			RunID:        "run-20260207-143000-abcd",
			Timestamp:    ts,
			Plan:         &plan.Plan{Graph: "booking.yaml"},
			Environment:  "test",
			GraphVersion: "1.0",
			ToolVersion:  "0.1.0",
		},
		Steps: []StepRecord{
			{
				Node:       "searchAir",
				StartTime:  ts,
				DurationMs: 150,
				Inputs:     map[string]any{"origin": "DEN", "destination": "LAX"},
				Request: &RequestRecord{
					Method:  "POST",
					URL:     "https://api.example.com/v2/search",
					Headers: map[string]string{"Content-Type": "application/json"},
					Body:    json.RawMessage(`{"origin":"DEN"}`),
				},
				Response: &ResponseRecord{
					Status:  200,
					Headers: map[string]string{"Content-Type": "application/json"},
					Body:    json.RawMessage(`{"offers":[{"id":"offer-1"}]}`),
				},
				Outputs: map[string]any{"offerId": "offer-1"},
			},
		},
		Result: ArchiveResult{
			Outcome: "passed",
		},
	}

	err := Write(original, path)
	require.NoError(t, err)

	loaded, err := Read(path)
	require.NoError(t, err)

	assert.Equal(t, original.Metadata.Version, loaded.Metadata.Version)
	assert.Equal(t, original.Metadata.RunID, loaded.Metadata.RunID)
	assert.Equal(t, original.Metadata.Environment, loaded.Metadata.Environment)
	assert.Equal(t, original.Metadata.Plan.Graph, loaded.Metadata.Plan.Graph)
	assert.Equal(t, original.Result.Outcome, loaded.Result.Outcome)

	require.Len(t, loaded.Steps, 1)
	assert.Equal(t, "searchAir", loaded.Steps[0].Node)
	assert.Equal(t, int64(150), loaded.Steps[0].DurationMs)
	assert.Equal(t, "DEN", loaded.Steps[0].Inputs["origin"])
	assert.Equal(t, "POST", loaded.Steps[0].Request.Method)
	assert.Equal(t, 200, loaded.Steps[0].Response.Status)
}

func TestWrite_CreatesParentDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "archive.json")

	a := &Archive{
		Metadata: ArchiveMetadata{Version: "1", RunID: "run-test-0001"},
		Steps:    []StepRecord{},
		Result:   ArchiveResult{Outcome: "passed"},
	}

	err := Write(a, path)
	require.NoError(t, err)

	_, err = os.Stat(path)
	assert.NoError(t, err)
}

func TestWrite_JSONIsIndented(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	a := &Archive{
		Metadata: ArchiveMetadata{Version: "1", RunID: "run-test-0001"},
		Steps:    []StepRecord{},
		Result:   ArchiveResult{Outcome: "passed"},
	}

	err := Write(a, path)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	// Indented JSON should contain newlines
	assert.Contains(t, string(data), "\n")
	assert.Contains(t, string(data), "  ")
}

func TestRead_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")

	err := os.WriteFile(path, []byte("not json"), 0o644)
	require.NoError(t, err)

	_, err = Read(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshalling archive")
}

func TestRead_FileNotFound(t *testing.T) {
	_, err := Read("/nonexistent/path/archive.json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading archive file")
}

func TestRoundTrip_WithBodyHandling(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")

	a := &Archive{
		Metadata: ArchiveMetadata{Version: "1", RunID: "run-body-test"},
		Steps: []StepRecord{
			{
				Node:   "step1",
				Inputs: map[string]any{},
				Request: &RequestRecord{
					Method: "POST",
					URL:    "https://api.example.com/test",
					Body:   json.RawMessage(`{"key":"value"}`),
				},
				Response: &ResponseRecord{
					Status: 200,
					Body:   json.RawMessage(`{"result":"ok"}`),
				},
			},
		},
		Result: ArchiveResult{Outcome: "passed"},
	}

	err := Write(a, path)
	require.NoError(t, err)

	loaded, err := Read(path)
	require.NoError(t, err)

	// Body should be preserved as valid JSON
	assert.JSONEq(t, `{"key":"value"}`, string(loaded.Steps[0].Request.Body))
	assert.JSONEq(t, `{"result":"ok"}`, string(loaded.Steps[0].Response.Body))
}

func TestRoundTrip_WithNilBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nil-body.json")

	a := &Archive{
		Metadata: ArchiveMetadata{Version: "1", RunID: "run-nil-body"},
		Steps: []StepRecord{
			{
				Node:   "step1",
				Inputs: map[string]any{},
				Request: &RequestRecord{
					Method: "GET",
					URL:    "https://api.example.com/test",
				},
				Response: &ResponseRecord{
					Status: 200,
				},
			},
		},
		Result: ArchiveResult{Outcome: "passed"},
	}

	err := Write(a, path)
	require.NoError(t, err)

	loaded, err := Read(path)
	require.NoError(t, err)

	assert.Nil(t, loaded.Steps[0].Request.Body)
	assert.Nil(t, loaded.Steps[0].Response.Body)
}

func TestRoundTrip_WithCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cleanup.json")

	a := &Archive{
		Metadata: ArchiveMetadata{Version: "1", RunID: "run-cleanup"},
		Steps: []StepRecord{
			{Node: "createBooking", Inputs: map[string]any{}},
		},
		Cleanup: []StepRecord{
			{Node: "cancelBooking", Inputs: map[string]any{}},
		},
		Result: ArchiveResult{Outcome: "passed"},
	}

	err := Write(a, path)
	require.NoError(t, err)

	loaded, err := Read(path)
	require.NoError(t, err)

	require.Len(t, loaded.Cleanup, 1)
	assert.Equal(t, "cancelBooking", loaded.Cleanup[0].Node)
}

func TestRoundTrip_WithValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "validation.json")

	a := &Archive{
		Metadata: ArchiveMetadata{Version: "1", RunID: "run-validation"},
		Steps: []StepRecord{
			{
				Node:   "step1",
				Inputs: map[string]any{},
				Validation: &ValidationRecord{
					Passed: true,
					Results: []AssertionRecord{
						{Type: "status", Passed: true, Message: "status code is 200"},
						{Type: "fieldExists", Passed: true, Message: `field "id" exists`, Path: "id"},
					},
				},
			},
		},
		Result: ArchiveResult{Outcome: "passed"},
	}

	err := Write(a, path)
	require.NoError(t, err)

	loaded, err := Read(path)
	require.NoError(t, err)

	require.NotNil(t, loaded.Steps[0].Validation)
	assert.True(t, loaded.Steps[0].Validation.Passed)
	require.Len(t, loaded.Steps[0].Validation.Results, 2)
	assert.Equal(t, "status", loaded.Steps[0].Validation.Results[0].Type)
	assert.Equal(t, "id", loaded.Steps[0].Validation.Results[1].Path)
}

func TestRoundTrip_WithError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "error.json")

	a := &Archive{
		Metadata: ArchiveMetadata{Version: "1", RunID: "run-error"},
		Steps: []StepRecord{
			{
				Node:   "failStep",
				Inputs: map[string]any{},
				Error:  "connection refused",
				ErrorClass: &ErrorClassRecord{
					Category: "transient",
					Detail:   "connection refused",
					Action:   "retried",
				},
				RetryCount: 2,
			},
		},
		Result: ArchiveResult{
			Outcome: "error",
			Error:   "step \"failStep\" failed",
		},
	}

	err := Write(a, path)
	require.NoError(t, err)

	loaded, err := Read(path)
	require.NoError(t, err)

	assert.Equal(t, "error", loaded.Result.Outcome)
	assert.Equal(t, "connection refused", loaded.Steps[0].Error)
	require.NotNil(t, loaded.Steps[0].ErrorClass)
	assert.Equal(t, "transient", loaded.Steps[0].ErrorClass.Category)
	assert.Equal(t, 2, loaded.Steps[0].RetryCount)
}
