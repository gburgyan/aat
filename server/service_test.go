package server

import (
	"encoding/json"
	"errors"
	"fmt"
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

func makeStepWithID(stepID, node string, status int, durationMs int64) archive.StepRecord {
	s := makeStep(node, status, durationMs)
	s.StepID = stepID
	return s
}

func makeStepFull(node string, status int) archive.StepRecord {
	trueVal := true
	return archive.StepRecord{
		Node:       node,
		StartTime:  time.Date(2026, 2, 10, 14, 30, 5, 0, time.UTC),
		DurationMs: 450,
		Inputs:     map[string]any{"origin": "JFK", "destination": "LAX"},
		Outputs:    map[string]any{"bookingRef": "ABC123"},
		Request: &archive.RequestRecord{
			Method:  "POST",
			URL:     "https://api.example.com/search",
			Headers: map[string]string{"Content-Type": "application/json", "Authorization": "Bearer ***"},
			Body:    json.RawMessage(`{"from":"JFK","to":"LAX"}`),
		},
		Response: &archive.ResponseRecord{
			Status:  status,
			Headers: map[string]string{"X-Request-Id": "req-123", "Content-Type": "application/json"},
			Body:    json.RawMessage(`{"id":"offer-1","price":299}`),
		},
		Validation: &archive.ValidationRecord{
			Passed: true,
			Results: []archive.AssertionRecord{
				{Type: "status", Passed: true, Message: "status 200"},
				{Type: "fieldExists", Passed: true, Message: "field id exists", Path: "id"},
				{Type: "predicate", Passed: false, Message: "price < 100", Expr: "price < 100"},
			},
		},
		Selections: []archive.SelectionRecord{
			{
				InputName:     "offeringId",
				SourceNode:    "SearchOffers",
				SourceField:   "offerings",
				SourceSize:    10,
				FilterExpr:    "price < 500",
				FilteredSize:  3,
				Strategy:      "first",
				SelectedIndex: 0,
				SelectionName: "cheapest",
			},
		},
		Resolutions: []archive.ValueResolutionRecord{
			{
				InputName:    "origin",
				Source:       "pool",
				RawValue:     "JFK",
				FinalValue:   "JFK",
				Constraint:   "len >= 3",
				ConstraintOK: &trueVal,
				PoolIndex:    0,
				PoolSize:     5,
			},
		},
		DisplayOutputs: []archive.DisplayOutputRecord{
			{Label: "Booking Ref", Name: "bookingRef", Value: "ABC123"},
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
	a1.Metadata.Timestamp = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	a2 := makeArchive("run-20260102-100000-aaaa0002", "passed", makeStep("node2", 200, 200))
	a2.Metadata.Timestamp = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	a3 := makeArchive("run-20260103-100000-aaaa0003", "failed", makeStep("node3", 500, 300))
	a3.Metadata.Timestamp = time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC)

	writeArchive(t, dir, a1)
	writeArchive(t, dir, a2)
	writeArchive(t, dir, a3)

	svc := NewArchiveService(dir)
	runs, err := svc.ListRuns(0, false)
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
	runs, err := svc.ListRuns(0, false)
	assert.NoError(t, err)
	assert.Nil(t, runs)
}

func TestListRuns_NotConfigured(t *testing.T) {
	svc := NewArchiveService("")
	_, err := svc.ListRuns(0, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestListRuns_NonexistentDirectory(t *testing.T) {
	svc := NewArchiveService("/tmp/nonexistent-aat-test-dir-12345")
	runs, err := svc.ListRuns(0, false)
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
	runs, err := svc.ListRuns(2, false)
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
	runs, err := svc.ListRuns(0, false)
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
	runs, err := svc.ListRuns(0, false)
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
	runs, err := svc.ListRuns(0, false)
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
	runs, err := svc.ListRuns(0, false)
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
	runs, err := svc.ListRuns(0, false)
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
	runs, err := svc.ListRuns(0, false)
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
	runs, err := svc.ListRuns(0, false)
	require.NoError(t, err)
	assert.Equal(t, "booking flow", runs[0].PlanName)
}

func TestListRuns_NoPlan(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	runs, err := svc.ListRuns(0, false)
	require.NoError(t, err)
	assert.Equal(t, "", runs[0].PlanName)
}

// --- LatestRunID ---

func TestLatestRunID_ReturnsNewest(t *testing.T) {
	dir := t.TempDir()

	a1 := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("n", 200, 100))
	a1.Metadata.Timestamp = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	a3 := makeArchive("run-20260103-100000-aaaa0003", "passed", makeStep("n", 200, 100))
	a3.Metadata.Timestamp = time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC)
	a2 := makeArchive("run-20260102-100000-aaaa0002", "passed", makeStep("n", 200, 100))
	a2.Metadata.Timestamp = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)

	writeArchive(t, dir, a1)
	writeArchive(t, dir, a3)
	writeArchive(t, dir, a2)

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

// --- GetRun ---

func TestGetRun_BasicFields(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed",
		makeStep("search", 200, 100),
		makeStep("book", 200, 200),
		makeStep("confirm", 200, 150),
	)
	a.Result.Error = ""
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	run, err := svc.GetRun("run-20260101-100000-aaaa0001")
	require.NoError(t, err)

	assert.Equal(t, "run-20260101-100000-aaaa0001", run.RunID)
	assert.Equal(t, "passed", run.Outcome)
	assert.Equal(t, int64(450), run.DurationMs)
	assert.Equal(t, 3, run.StepCount)
	assert.Equal(t, 3, run.PassedCount)
	assert.Equal(t, 0, run.FailedCount)
}

func TestGetRun_MetadataFields(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	a.Metadata.Environment = "staging"
	a.Metadata.GraphVersion = "v2.1"
	a.Metadata.ToolVersion = "0.5.0"
	a.Metadata.Plan = &plan.Plan{
		Metadata: plan.Metadata{Prompt: "test prompt"},
	}
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	run, err := svc.GetRun("run-20260101-100000-aaaa0001")
	require.NoError(t, err)

	assert.Equal(t, "staging", run.Environment)
	assert.Equal(t, "v2.1", run.GraphVersion)
	assert.Equal(t, "0.5.0", run.ToolVersion)
	assert.Equal(t, "test prompt", run.PlanName)
}

func TestGetRun_StepSummaries(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed",
		makeStepWithID("step-search", "SearchOffers", 200, 100),
		makeStep("BookOffer", 200, 200),
	)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	run, err := svc.GetRun("run-20260101-100000-aaaa0001")
	require.NoError(t, err)
	require.Len(t, run.Steps, 2)

	assert.Equal(t, "step-search", run.Steps[0].StepID)
	assert.Equal(t, "SearchOffers", run.Steps[0].Node)
	assert.Equal(t, 200, run.Steps[0].Status)
	assert.True(t, run.Steps[0].Passed)

	assert.Equal(t, "BookOffer", run.Steps[1].StepID) // falls back to Node
	assert.Equal(t, "BookOffer", run.Steps[1].Node)
}

func TestGetRun_StepSummary_AssertionCounts(t *testing.T) {
	dir := t.TempDir()

	step := makeStep("node", 200, 100)
	step.Validation = &archive.ValidationRecord{
		Passed: false,
		Results: []archive.AssertionRecord{
			{Type: "status", Passed: true, Message: "status 200"},
			{Type: "fieldExists", Passed: true, Message: "field id exists"},
			{Type: "predicate", Passed: false, Message: "price < 100"},
		},
	}

	a := makeArchive("run-20260101-100000-aaaa0001", "failed", step)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	run, err := svc.GetRun("run-20260101-100000-aaaa0001")
	require.NoError(t, err)
	require.Len(t, run.Steps, 1)

	assert.Equal(t, 3, run.Steps[0].AssertionCount)
	assert.Equal(t, 2, run.Steps[0].AssertionPassedCount)
	assert.False(t, run.Steps[0].Passed)
}

func TestGetRun_StepSummary_DisplayOutputs(t *testing.T) {
	dir := t.TempDir()

	step := makeStep("node", 200, 100)
	step.DisplayOutputs = []archive.DisplayOutputRecord{
		{Label: "Booking Ref", Name: "bookingRef", Value: "ABC123"},
	}

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", step)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	run, err := svc.GetRun("run-20260101-100000-aaaa0001")
	require.NoError(t, err)
	require.Len(t, run.Steps[0].DisplayOutputs, 1)

	assert.Equal(t, "Booking Ref", run.Steps[0].DisplayOutputs[0].Label)
	assert.Equal(t, "bookingRef", run.Steps[0].DisplayOutputs[0].Name)
	assert.Equal(t, "ABC123", run.Steps[0].DisplayOutputs[0].Value)
}

func TestGetRun_StepSummary_HasFlags(t *testing.T) {
	dir := t.TempDir()

	step := makeStep("node", 200, 100)
	step.Selections = []archive.SelectionRecord{{InputName: "offeringId", Strategy: "first"}}
	step.Resolutions = []archive.ValueResolutionRecord{{InputName: "origin", Source: "pool"}}

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", step)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	run, err := svc.GetRun("run-20260101-100000-aaaa0001")
	require.NoError(t, err)

	assert.True(t, run.Steps[0].HasSelections)
	assert.True(t, run.Steps[0].HasResolutions)
}

func TestGetRun_CleanupSteps(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("search", 200, 100))
	a.Cleanup = []archive.StepRecord{makeStep("cleanup-cancel", 200, 50)}
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	run, err := svc.GetRun("run-20260101-100000-aaaa0001")
	require.NoError(t, err)

	require.Len(t, run.Steps, 1)
	require.Len(t, run.Cleanup, 1)
	assert.True(t, run.Cleanup[0].IsCleanup)
	assert.Equal(t, "cleanup-cancel", run.Cleanup[0].Node)
}

func TestGetRun_NotFound(t *testing.T) {
	dir := t.TempDir()
	svc := NewArchiveService(dir)

	_, err := svc.GetRun("run-nonexistent")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRunNotFound))
}

func TestGetRun_DurationDisplay(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed",
		makeStep("search", 200, 1500),
	)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	run, err := svc.GetRun("run-20260101-100000-aaaa0001")
	require.NoError(t, err)

	assert.Equal(t, "1.5s", run.DurationDisplay)
	assert.Equal(t, "1.5s", run.Steps[0].DurationDisplay)
}

// --- GetStep ---

func TestGetStep_ByNode(t *testing.T) {
	dir := t.TempDir()

	step := makeStep("SearchOffers", 200, 100)
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", step)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	detail, err := svc.GetStep("run-20260101-100000-aaaa0001", "SearchOffers")
	require.NoError(t, err)

	assert.Equal(t, "SearchOffers", detail.StepID)
	assert.Equal(t, "SearchOffers", detail.Node)
	assert.Equal(t, 200, detail.Status)
	assert.True(t, detail.Passed)
	assert.False(t, detail.IsCleanup)
}

func TestGetStep_ByStepID(t *testing.T) {
	dir := t.TempDir()

	step := makeStepWithID("step-search", "SearchOffers", 200, 100)
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", step)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	detail, err := svc.GetStep("run-20260101-100000-aaaa0001", "step-search")
	require.NoError(t, err)

	assert.Equal(t, "step-search", detail.StepID)
	assert.Equal(t, "SearchOffers", detail.Node)
}

func TestGetStep_FullRequest(t *testing.T) {
	dir := t.TempDir()

	step := makeStepFull("SearchOffers", 200)
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", step)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	detail, err := svc.GetStep("run-20260101-100000-aaaa0001", "SearchOffers")
	require.NoError(t, err)

	require.NotNil(t, detail.Request)
	assert.Equal(t, "POST", detail.Request.Method)
	assert.Equal(t, "https://api.example.com/search", detail.Request.URL)
	assert.NotEmpty(t, detail.Request.Body)

	// Headers are sorted
	require.Len(t, detail.Request.Headers, 2)
	assert.Equal(t, "Authorization", detail.Request.Headers[0].Name)
	assert.Equal(t, "Content-Type", detail.Request.Headers[1].Name)
}

func TestGetStep_FullResponse(t *testing.T) {
	dir := t.TempDir()

	step := makeStepFull("SearchOffers", 200)
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", step)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	detail, err := svc.GetStep("run-20260101-100000-aaaa0001", "SearchOffers")
	require.NoError(t, err)

	require.NotNil(t, detail.Response)
	assert.Equal(t, 200, detail.Response.Status)
	assert.NotEmpty(t, detail.Response.Body)

	// Headers are sorted
	require.Len(t, detail.Response.Headers, 2)
	assert.Equal(t, "Content-Type", detail.Response.Headers[0].Name)
	assert.Equal(t, "X-Request-Id", detail.Response.Headers[1].Name)
}

func TestGetStep_Validation(t *testing.T) {
	dir := t.TempDir()

	step := makeStepFull("SearchOffers", 200)
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", step)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	detail, err := svc.GetStep("run-20260101-100000-aaaa0001", "SearchOffers")
	require.NoError(t, err)

	require.NotNil(t, detail.Validation)
	assert.True(t, detail.Validation.Passed)
	require.Len(t, detail.Validation.Results, 3)
	assert.Equal(t, "status", detail.Validation.Results[0].Type)
	assert.True(t, detail.Validation.Results[0].Passed)
	assert.Equal(t, "predicate", detail.Validation.Results[2].Type)
	assert.False(t, detail.Validation.Results[2].Passed)
	assert.Equal(t, "price < 100", detail.Validation.Results[2].Expr)
}

func TestGetStep_Selections(t *testing.T) {
	dir := t.TempDir()

	step := makeStepFull("SearchOffers", 200)
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", step)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	detail, err := svc.GetStep("run-20260101-100000-aaaa0001", "SearchOffers")
	require.NoError(t, err)

	require.Len(t, detail.Selections, 1)
	sel := detail.Selections[0]
	assert.Equal(t, "offeringId", sel.InputName)
	assert.Equal(t, "SearchOffers", sel.SourceNode)
	assert.Equal(t, "offerings", sel.SourceField)
	assert.Equal(t, 10, sel.SourceSize)
	assert.Equal(t, "price < 500", sel.FilterExpr)
	assert.Equal(t, 3, sel.FilteredSize)
	assert.Equal(t, "first", sel.Strategy)
	assert.Equal(t, 0, sel.SelectedIndex)
	assert.Equal(t, "cheapest", sel.SelectionName)
}

func TestGetStep_Resolutions(t *testing.T) {
	dir := t.TempDir()

	step := makeStepFull("SearchOffers", 200)
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", step)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	detail, err := svc.GetStep("run-20260101-100000-aaaa0001", "SearchOffers")
	require.NoError(t, err)

	require.Len(t, detail.Resolutions, 1)
	res := detail.Resolutions[0]
	assert.Equal(t, "origin", res.InputName)
	assert.Equal(t, "pool", res.Source)
	assert.Equal(t, "JFK", res.RawValue)
	assert.Equal(t, "JFK", res.FinalValue)
	assert.Equal(t, "len >= 3", res.Constraint)
	require.NotNil(t, res.ConstraintOK)
	assert.True(t, *res.ConstraintOK)
	assert.Equal(t, 0, res.PoolIndex)
	assert.Equal(t, 5, res.PoolSize)
}


func TestGetStep_ErrorClassification(t *testing.T) {
	dir := t.TempDir()

	step := makeStep("node", 500, 100)
	step.Error = "server error"
	step.ErrorClass = &archive.ErrorClassRecord{
		Category:     "server",
		Detail:       "internal server error",
		Action:       "retry",
		RetryAttempt: 2,
	}

	a := makeArchive("run-20260101-100000-aaaa0001", "failed", step)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	detail, err := svc.GetStep("run-20260101-100000-aaaa0001", "node")
	require.NoError(t, err)

	require.NotNil(t, detail.ErrorClassification)
	assert.Equal(t, "server", detail.ErrorClassification.Category)
	assert.Equal(t, "internal server error", detail.ErrorClassification.Detail)
	assert.Equal(t, "retry", detail.ErrorClassification.Action)
	assert.Equal(t, 2, detail.ErrorClassification.RetryAttempt)
}

func TestGetStep_ExpectFailure(t *testing.T) {
	dir := t.TempDir()

	step := makeStep("node", 404, 100)
	step.ExpectFailure = &archive.ExpectFailureRecord{
		Expected: []int{404, 410},
		Actual:   404,
		Passed:   true,
	}

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", step)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	detail, err := svc.GetStep("run-20260101-100000-aaaa0001", "node")
	require.NoError(t, err)

	require.NotNil(t, detail.ExpectFailure)
	assert.Equal(t, []int{404, 410}, detail.ExpectFailure.Expected)
	assert.Equal(t, 404, detail.ExpectFailure.Actual)
	assert.True(t, detail.ExpectFailure.Passed)
}

func TestGetStep_ResponseBodyError(t *testing.T) {
	dir := t.TempDir()

	step := makeStep("node", 200, 100)
	step.ResponseBodyError = &archive.ResponseBodyErrorRecord{
		RulePath: "Errors.0",
		Rule:     "errorDetection",
		Message:  "booking failed",
		Code:     "ERR-001",
		Category: "business",
	}

	a := makeArchive("run-20260101-100000-aaaa0001", "failed", step)
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	detail, err := svc.GetStep("run-20260101-100000-aaaa0001", "node")
	require.NoError(t, err)

	require.NotNil(t, detail.ResponseBodyError)
	assert.Equal(t, "Errors.0", detail.ResponseBodyError.RulePath)
	assert.Equal(t, "errorDetection", detail.ResponseBodyError.Rule)
	assert.Equal(t, "booking failed", detail.ResponseBodyError.Message)
	assert.Equal(t, "ERR-001", detail.ResponseBodyError.Code)
	assert.Equal(t, "business", detail.ResponseBodyError.Category)
}

func TestGetStep_CleanupStep(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("search", 200, 100))
	a.Cleanup = []archive.StepRecord{makeStep("cancel", 200, 50)}
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	detail, err := svc.GetStep("run-20260101-100000-aaaa0001", "cancel")
	require.NoError(t, err)

	assert.Equal(t, "cancel", detail.Node)
	assert.True(t, detail.IsCleanup)
}

func TestGetStep_StepNotFound(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("search", 200, 100))
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	_, err := svc.GetStep("run-20260101-100000-aaaa0001", "nonexistent")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrStepNotFound))
}

func TestGetStep_RunNotFound(t *testing.T) {
	dir := t.TempDir()
	svc := NewArchiveService(dir)

	_, err := svc.GetStep("run-nonexistent", "step1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRunNotFound))
}

// --- helper tests ---

func TestEffectiveStepID(t *testing.T) {
	tests := []struct {
		name     string
		step     archive.StepRecord
		expected string
	}{
		{
			name:     "explicit StepID",
			step:     archive.StepRecord{StepID: "step-1", Node: "SearchOffers"},
			expected: "step-1",
		},
		{
			name:     "fallback to Node",
			step:     archive.StepRecord{Node: "SearchOffers"},
			expected: "SearchOffers",
		},
		{
			name:     "empty StepID falls back",
			step:     archive.StepRecord{StepID: "", Node: "BookOffer"},
			expected: "BookOffer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, effectiveStepID(tt.step))
		})
	}
}

func TestToHeaderEntries(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		expected []HeaderEntry
	}{
		{
			name:     "nil map",
			headers:  nil,
			expected: nil,
		},
		{
			name:     "empty map",
			headers:  map[string]string{},
			expected: nil,
		},
		{
			name: "sorted output",
			headers: map[string]string{
				"X-Request-Id":  "123",
				"Content-Type":  "application/json",
				"Authorization": "Bearer ***",
			},
			expected: []HeaderEntry{
				{Name: "Authorization", Value: "Bearer ***"},
				{Name: "Content-Type", Value: "application/json"},
				{Name: "X-Request-Id", Value: "123"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toHeaderEntries(tt.headers)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		ms       int64
		expected string
	}{
		{0, "0ms"},
		{450, "450ms"},
		{999, "999ms"},
		{1000, "1s"},
		{1500, "1.5s"},
		{2000, "2s"},
		{59999, "60.0s"},
		{60000, "1m"},
		{61000, "1m 1s"},
		{125000, "2m 5s"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, formatDuration(tt.ms))
		})
	}
}

// --- buildExtractions ---

func TestBuildExtractions_NoOutputs(t *testing.T) {
	a := makeArchive("run-1", "passed", makeStep("node", 200, 100))
	nodeSteps := nodeToStepMap(a)
	result := buildExtractions("node", nil, a, nodeSteps)
	assert.Nil(t, result)
}

func TestBuildExtractions_OutputsNoConsumers(t *testing.T) {
	a := makeArchive("run-1", "passed", makeStep("node", 200, 100))
	nodeSteps := nodeToStepMap(a)
	outputs := map[string]any{"bookingRef": "ABC123"}
	result := buildExtractions("node", outputs, a, nodeSteps)

	require.Len(t, result, 1)
	assert.Equal(t, "bookingRef", result[0].Name)
	assert.Equal(t, "ABC123", result[0].Value)
	assert.Nil(t, result[0].Consumers)
}

func TestBuildExtractions_ResolutionConsumers(t *testing.T) {
	step1 := makeStepWithID("search", "SearchOffers", 200, 100)
	step1.Outputs = map[string]any{"offerId": "offer-1"}

	step2 := makeStepWithID("book", "BookOffer", 200, 200)
	step2.Resolutions = []archive.ValueResolutionRecord{
		{InputName: "offeringId", Source: "plan_from", FromStep: "search", FromOutput: "offerId"},
	}

	a := makeArchive("run-1", "passed", step1, step2)
	nodeSteps := nodeToStepMap(a)
	result := buildExtractions("search", step1.Outputs, a, nodeSteps)

	require.Len(t, result, 1)
	assert.Equal(t, "offerId", result[0].Name)
	require.Len(t, result[0].Consumers, 1)
	assert.Equal(t, "book", result[0].Consumers[0].StepID)
	assert.Equal(t, "offeringId", result[0].Consumers[0].InputName)
	assert.Equal(t, "resolution", result[0].Consumers[0].Via)
}

func TestBuildExtractions_SelectionConsumers(t *testing.T) {
	step1 := makeStepWithID("search", "SearchOffers", 200, 100)
	step1.Outputs = map[string]any{"offerings": []any{"a", "b"}}

	step2 := makeStepWithID("book", "BookOffer", 200, 200)
	step2.Selections = []archive.SelectionRecord{
		{InputName: "offeringId", SourceNode: "SearchOffers", SourceField: "offerings", Strategy: "first"},
	}

	a := makeArchive("run-1", "passed", step1, step2)
	nodeSteps := nodeToStepMap(a)
	result := buildExtractions("search", step1.Outputs, a, nodeSteps)

	require.Len(t, result, 1)
	assert.Equal(t, "offerings", result[0].Name)
	require.Len(t, result[0].Consumers, 1)
	assert.Equal(t, "book", result[0].Consumers[0].StepID)
	assert.Equal(t, "offeringId", result[0].Consumers[0].InputName)
	assert.Equal(t, "selection", result[0].Consumers[0].Via)
}

func TestBuildExtractions_MixedConsumers(t *testing.T) {
	step1 := makeStepWithID("search", "SearchOffers", 200, 100)
	step1.Outputs = map[string]any{"offerId": "offer-1", "offerings": []any{"a"}}

	step2 := makeStepWithID("book", "BookOffer", 200, 200)
	step2.Resolutions = []archive.ValueResolutionRecord{
		{InputName: "offeringId", Source: "plan_from", FromStep: "search", FromOutput: "offerId"},
	}
	step2.Selections = []archive.SelectionRecord{
		{InputName: "productRef", SourceNode: "SearchOffers", SourceField: "offerings", Strategy: "first"},
	}

	a := makeArchive("run-1", "passed", step1, step2)
	nodeSteps := nodeToStepMap(a)
	result := buildExtractions("search", step1.Outputs, a, nodeSteps)

	// Sorted alphabetically: offerId, offerings
	require.Len(t, result, 2)
	assert.Equal(t, "offerId", result[0].Name)
	require.Len(t, result[0].Consumers, 1)
	assert.Equal(t, "resolution", result[0].Consumers[0].Via)

	assert.Equal(t, "offerings", result[1].Name)
	require.Len(t, result[1].Consumers, 1)
	assert.Equal(t, "selection", result[1].Consumers[0].Via)
}

// --- findPlanStepYAML ---

func TestFindPlanStepYAML_NilPlan(t *testing.T) {
	a := makeArchive("run-1", "passed", makeStep("node", 200, 100))
	result := findPlanStepYAML(a, "node", "node", false)
	assert.Equal(t, "", result)
}

func TestFindPlanStepYAML_MatchMainStep(t *testing.T) {
	a := makeArchive("run-1", "passed", makeStep("SearchOffers", 200, 100))
	a.Metadata.Plan = &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "SearchOffers", Description: "Search for flights"},
			},
		},
	}
	result := findPlanStepYAML(a, "SearchOffers", "SearchOffers", false)
	assert.Contains(t, result, "node: SearchOffers")
	assert.Contains(t, result, "description: Search for flights")
}

func TestFindPlanStepYAML_MatchStepWithID(t *testing.T) {
	a := makeArchive("run-1", "passed", makeStepWithID("search_leg1", "SearchOffers", 200, 100))
	a.Metadata.Plan = &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{ID: "search_leg1", Node: "SearchOffers", Description: "Leg 1"},
			},
		},
	}
	result := findPlanStepYAML(a, "search_leg1", "SearchOffers", false)
	assert.Contains(t, result, "id: search_leg1")
	assert.Contains(t, result, "node: SearchOffers")
}

func TestFindPlanStepYAML_MatchCleanupStep(t *testing.T) {
	a := makeArchive("run-1", "passed", makeStep("search", 200, 100))
	a.Cleanup = []archive.StepRecord{makeStep("cancelWorkbench", 200, 50)}
	a.Metadata.Plan = &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search"},
			},
			Cleanup: []plan.CleanupStep{
				{Node: "cancelWorkbench", RunOn: "always"},
			},
		},
	}
	result := findPlanStepYAML(a, "cancelWorkbench", "cancelWorkbench", true)
	assert.Contains(t, result, "node: cancelWorkbench")
	assert.Contains(t, result, "runOn: always")
}

func TestFindPlanStepYAML_NoMatch(t *testing.T) {
	a := makeArchive("run-1", "passed", makeStep("node", 200, 100))
	a.Metadata.Plan = &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "OtherNode"},
			},
		},
	}
	result := findPlanStepYAML(a, "node", "node", false)
	assert.Equal(t, "", result)
}

// --- GetStep integration: extractions + plan YAML ---

func TestGetStep_ExtractionsAndPlanYAML(t *testing.T) {
	dir := t.TempDir()

	step1 := makeStepWithID("search", "SearchOffers", 200, 100)
	step1.Outputs = map[string]any{"offerId": "offer-1"}

	step2 := makeStepWithID("book", "BookOffer", 200, 200)
	step2.Resolutions = []archive.ValueResolutionRecord{
		{InputName: "offeringId", Source: "plan_from", FromStep: "search", FromOutput: "offerId"},
	}

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", step1, step2)
	a.Metadata.Plan = &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{ID: "search", Node: "SearchOffers", Description: "Search"},
				{ID: "book", Node: "BookOffer", Description: "Book"},
			},
		},
	}
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	detail, err := svc.GetStep("run-20260101-100000-aaaa0001", "search")
	require.NoError(t, err)

	// Extractions populated
	require.Len(t, detail.Extractions, 1)
	assert.Equal(t, "offerId", detail.Extractions[0].Name)
	assert.Equal(t, "offer-1", detail.Extractions[0].Value)
	require.Len(t, detail.Extractions[0].Consumers, 1)
	assert.Equal(t, "book", detail.Extractions[0].Consumers[0].StepID)

	// Plan YAML populated
	assert.Contains(t, detail.PlanStepYAML, "node: SearchOffers")
}

// --- batch test helpers ---

func makeBatchArchive(batchID, outcome string, runs ...archive.BatchRunEntry) *archive.BatchArchive {
	passed, failed, errCount := 0, 0, 0
	var totalDur int64
	for _, r := range runs {
		switch r.Outcome {
		case "passed":
			passed++
		case "failed":
			failed++
		case "error":
			errCount++
		}
		totalDur += r.DurationMs
	}
	return &archive.BatchArchive{
		Metadata: archive.BatchMetadata{
			Version:     "1.0.0",
			BatchID:     batchID,
			Timestamp:   time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC),
			Source:      "all",
			ToolVersion: "0.6.0",
		},
		Runs: runs,
		Result: archive.BatchResult{
			Outcome:         outcome,
			TotalRuns:       len(runs),
			PassedRuns:      passed,
			FailedRuns:      failed,
			ErrorRuns:       errCount,
			TotalDurationMs: totalDur,
		},
	}
}

func writeBatchArchive(t *testing.T, dir string, b *archive.BatchArchive) {
	t.Helper()
	path := filepath.Join(dir, b.Metadata.BatchID, "batch.json")
	err := archive.WriteBatch(b, path)
	require.NoError(t, err)
}

func writeBatchMemberArchive(t *testing.T, dir string, batchID string, a *archive.Archive) {
	t.Helper()
	path := filepath.Join(dir, batchID, a.Metadata.RunID, "archive.json")
	err := archive.Write(a, path)
	require.NoError(t, err)
}

// --- ListBatches ---

func TestListBatches_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	svc := NewArchiveService(dir)
	batches, err := svc.ListBatches(0, false)
	assert.NoError(t, err)
	assert.Nil(t, batches)
}

func TestListBatches_SortedNewestFirst(t *testing.T) {
	dir := t.TempDir()

	b1 := makeBatchArchive("batch-20260201-100000-aaaa0001", "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-1", Outcome: "passed", DurationMs: 100},
	)
	b1.Metadata.Timestamp = time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	b2 := makeBatchArchive("batch-20260202-100000-aaaa0002", "failed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-2", Outcome: "failed", DurationMs: 200},
	)
	b2.Metadata.Timestamp = time.Date(2026, 2, 2, 10, 0, 0, 0, time.UTC)
	writeBatchArchive(t, dir, b1)
	writeBatchArchive(t, dir, b2)

	svc := NewArchiveService(dir)
	batches, err := svc.ListBatches(0, false)
	require.NoError(t, err)
	require.Len(t, batches, 2)

	assert.Equal(t, "batch-20260202-100000-aaaa0002", batches[0].BatchID)
	assert.Equal(t, "batch-20260201-100000-aaaa0001", batches[1].BatchID)
	assert.Equal(t, "failed", batches[0].Outcome)
	assert.Equal(t, 1, batches[0].TotalRuns)
}

func TestListBatches_Limit(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 5; i++ {
		b := makeBatchArchive(
			fmt.Sprintf("batch-2026020%d-100000-aaaa000%d", i+1, i+1),
			"passed",
			archive.BatchRunEntry{PlanName: "plan", RunID: fmt.Sprintf("run-%d", i), Outcome: "passed", DurationMs: 100},
		)
		writeBatchArchive(t, dir, b)
	}

	svc := NewArchiveService(dir)
	batches, err := svc.ListBatches(2, false)
	require.NoError(t, err)
	assert.Len(t, batches, 2)
}

func TestListBatches_SkipsCorruptBatchJSON(t *testing.T) {
	dir := t.TempDir()

	good := makeBatchArchive("batch-20260201-100000-aaaa0001", "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-1", Outcome: "passed", DurationMs: 100},
	)
	writeBatchArchive(t, dir, good)

	// Write corrupt batch.json
	corruptDir := filepath.Join(dir, "batch-20260202-100000-aaaa0002")
	require.NoError(t, createDir(corruptDir))
	require.NoError(t, writeFile(filepath.Join(corruptDir, "batch.json"), []byte("not json")))

	svc := NewArchiveService(dir)
	batches, err := svc.ListBatches(0, false)
	require.NoError(t, err)
	assert.Len(t, batches, 1)
	assert.Equal(t, "batch-20260201-100000-aaaa0001", batches[0].BatchID)
}

func TestListBatches_NotConfigured(t *testing.T) {
	svc := NewArchiveService("")
	_, err := svc.ListBatches(0, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestListBatches_Fields(t *testing.T) {
	dir := t.TempDir()

	b := makeBatchArchive("batch-20260201-100000-aaaa0001", "failed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-1", Outcome: "passed", DurationMs: 100},
		archive.BatchRunEntry{PlanName: "plan2", RunID: "run-2", Outcome: "failed", DurationMs: 200},
	)
	writeBatchArchive(t, dir, b)

	svc := NewArchiveService(dir)
	batches, err := svc.ListBatches(0, false)
	require.NoError(t, err)
	require.Len(t, batches, 1)

	entry := batches[0]
	assert.Equal(t, "failed", entry.Outcome)
	assert.Equal(t, 2, entry.TotalRuns)
	assert.Equal(t, 1, entry.PassedRuns)
	assert.Equal(t, 1, entry.FailedRuns)
	assert.Equal(t, 0, entry.ErrorRuns)
	assert.Equal(t, int64(300), entry.TotalDurationMs)
	assert.Equal(t, "all", entry.Source)
	assert.Equal(t, "0.6.0", entry.ToolVersion)
}

// --- GetBatch ---

func TestGetBatch_ReturnsDetail(t *testing.T) {
	dir := t.TempDir()

	b := makeBatchArchive("batch-20260201-100000-aaaa0001", "passed",
		archive.BatchRunEntry{PlanName: "roundtrip", RunID: "run-1", Outcome: "passed", StepCount: 5, PassedCount: 5, DurationMs: 1500},
		archive.BatchRunEntry{PlanName: "oneway", RunID: "run-2", Outcome: "passed", StepCount: 3, PassedCount: 3, DurationMs: 800},
	)
	writeBatchArchive(t, dir, b)

	svc := NewArchiveService(dir)
	detail, err := svc.GetBatch("batch-20260201-100000-aaaa0001")
	require.NoError(t, err)

	assert.Equal(t, "batch-20260201-100000-aaaa0001", detail.BatchID)
	assert.Equal(t, "passed", detail.Outcome)
	assert.Equal(t, 2, detail.TotalRuns)
	assert.Equal(t, "2.3s", detail.DurationDisplay)
	require.Len(t, detail.Runs, 2)
	assert.Equal(t, "roundtrip", detail.Runs[0].PlanName)
	assert.Equal(t, "run-1", detail.Runs[0].RunID)
	assert.Equal(t, 5, detail.Runs[0].StepCount)
}

func TestGetBatch_NotFound(t *testing.T) {
	dir := t.TempDir()
	svc := NewArchiveService(dir)

	_, err := svc.GetBatch("batch-nonexistent")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBatchNotFound))
}

// --- loadArchive fallthrough to batch members ---

func TestLoadArchive_FindsStandaloneRun(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	loaded, err := svc.loadArchive("run-20260101-100000-aaaa0001")
	require.NoError(t, err)
	assert.Equal(t, "run-20260101-100000-aaaa0001", loaded.Metadata.RunID)
}

func TestLoadArchive_FindsBatchMemberRun(t *testing.T) {
	dir := t.TempDir()

	batchID := "batch-20260201-100000-aaaa0001"
	b := makeBatchArchive(batchID, "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-20260201-100000-bbbb0001", Outcome: "passed", DurationMs: 100},
	)
	writeBatchArchive(t, dir, b)

	memberArchive := makeArchive("run-20260201-100000-bbbb0001", "passed", makeStep("node", 200, 100))
	writeBatchMemberArchive(t, dir, batchID, memberArchive)

	svc := NewArchiveService(dir)
	loaded, err := svc.loadArchive("run-20260201-100000-bbbb0001")
	require.NoError(t, err)
	assert.Equal(t, "run-20260201-100000-bbbb0001", loaded.Metadata.RunID)
}

func TestLoadArchiveWithContext_ReturnsBatchID(t *testing.T) {
	dir := t.TempDir()

	batchID := "batch-20260201-100000-aaaa0001"
	b := makeBatchArchive(batchID, "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-20260201-100000-bbbb0001", Outcome: "passed", DurationMs: 100},
	)
	writeBatchArchive(t, dir, b)

	memberArchive := makeArchive("run-20260201-100000-bbbb0001", "passed", makeStep("node", 200, 100))
	writeBatchMemberArchive(t, dir, batchID, memberArchive)

	svc := NewArchiveService(dir)
	loaded, gotBatchID, err := svc.loadArchiveWithContext("run-20260201-100000-bbbb0001")
	require.NoError(t, err)
	assert.Equal(t, "run-20260201-100000-bbbb0001", loaded.Metadata.RunID)
	assert.Equal(t, batchID, gotBatchID)
}

func TestLoadArchiveWithContext_StandaloneHasNoBatchID(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	_, gotBatchID, err := svc.loadArchiveWithContext("run-20260101-100000-aaaa0001")
	require.NoError(t, err)
	assert.Equal(t, "", gotBatchID)
}

func TestLoadArchive_RunNotFoundAnywhere(t *testing.T) {
	dir := t.TempDir()
	svc := NewArchiveService(dir)

	_, err := svc.loadArchive("run-nonexistent")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRunNotFound))
}

// --- GetRun with batchId ---

func TestGetRun_BatchMemberHasBatchID(t *testing.T) {
	dir := t.TempDir()

	batchID := "batch-20260201-100000-aaaa0001"
	b := makeBatchArchive(batchID, "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-20260201-100000-bbbb0001", Outcome: "passed", DurationMs: 100},
	)
	writeBatchArchive(t, dir, b)

	memberArchive := makeArchive("run-20260201-100000-bbbb0001", "passed", makeStep("node", 200, 100))
	writeBatchMemberArchive(t, dir, batchID, memberArchive)

	svc := NewArchiveService(dir)
	run, err := svc.GetRun("run-20260201-100000-bbbb0001")
	require.NoError(t, err)
	assert.Equal(t, batchID, run.BatchID)
}

func TestGetRun_StandaloneHasNoBatchID(t *testing.T) {
	dir := t.TempDir()

	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	run, err := svc.GetRun("run-20260101-100000-aaaa0001")
	require.NoError(t, err)
	assert.Equal(t, "", run.BatchID)
}

// --- loadAttemptSummaries ---

func TestLoadAttemptSummaries_ReadsAttemptFromMetadata(t *testing.T) {
	dir := t.TempDir()
	batchID := "batch-20260201-100000-aaaa0001"
	runID := "run-20260201-100000-bbbb0001"
	runDir := filepath.Join(dir, batchID, runID)
	require.NoError(t, createDir(runDir))

	// Write attempt files with correct Attempt metadata.
	for i := 1; i <= 4; i++ {
		a := makeArchive(runID, "failed", makeStep("node", 500, 100))
		a.Metadata.Attempt = i
		a.Metadata.TotalAttempts = 5
		path := filepath.Join(runDir, fmt.Sprintf("attempt-%02d.json", i))
		require.NoError(t, archive.Write(a, path))
	}

	svc := NewArchiveService(dir)
	attempts := svc.loadAttemptSummaries(runID, batchID)

	require.Len(t, attempts, 4)
	for i, att := range attempts {
		assert.Equal(t, i+1, att.Attempt, "attempt %d should have number %d", i, i+1)
		assert.Equal(t, fmt.Sprintf("attempt-%02d.json", i+1), att.FileName)
		assert.Equal(t, "failed", att.Outcome)
	}
}

func TestLoadAttemptSummaries_SortOrder(t *testing.T) {
	dir := t.TempDir()
	runID := "run-20260201-100000-bbbb0001"
	runDir := filepath.Join(dir, runID)
	require.NoError(t, createDir(runDir))

	// Write in reverse order to verify sorting by Attempt metadata.
	for _, i := range []int{3, 1, 2} {
		a := makeArchive(runID, "failed", makeStep("node", 500, 100))
		a.Metadata.Attempt = i
		a.Metadata.TotalAttempts = 4
		path := filepath.Join(runDir, fmt.Sprintf("attempt-%02d.json", i))
		require.NoError(t, archive.Write(a, path))
	}

	svc := NewArchiveService(dir)
	attempts := svc.loadAttemptSummaries(runID, "")

	require.Len(t, attempts, 3)
	assert.Equal(t, 1, attempts[0].Attempt)
	assert.Equal(t, 2, attempts[1].Attempt)
	assert.Equal(t, 3, attempts[2].Attempt)
}

func TestLoadAttemptSummaries_Empty(t *testing.T) {
	dir := t.TempDir()
	runID := "run-20260201-100000-bbbb0001"
	runDir := filepath.Join(dir, runID)
	require.NoError(t, createDir(runDir))

	svc := NewArchiveService(dir)
	attempts := svc.loadAttemptSummaries(runID, "")
	assert.Nil(t, attempts)
}

// --- GetAttempt ---

func TestGetAttempt_LoadsCorrectFile(t *testing.T) {
	dir := t.TempDir()
	runID := "run-20260201-100000-bbbb0001"

	// Write the final archive.
	finalArchive := makeArchive(runID, "passed", makeStep("node", 200, 100))
	finalArchive.Metadata.Attempt = 3
	finalArchive.Metadata.TotalAttempts = 3
	writeArchive(t, dir, finalArchive)

	// Write attempt-02.json in the run directory.
	attemptArchive := makeArchive(runID, "failed", makeStep("node", 500, 80))
	attemptArchive.Metadata.Attempt = 2
	attemptArchive.Metadata.TotalAttempts = 3
	attemptArchive.Result.Error = "step failed"
	attemptPath := filepath.Join(dir, runID, "attempt-02.json")
	require.NoError(t, archive.Write(attemptArchive, attemptPath))

	svc := NewArchiveService(dir)
	detail, err := svc.GetAttempt(runID, 2)
	require.NoError(t, err)

	assert.Equal(t, runID, detail.RunID)
	assert.Equal(t, "failed", detail.Outcome)
	assert.Equal(t, 2, detail.Attempt)
	assert.Equal(t, 3, detail.TotalAttempts)
	assert.Equal(t, "", detail.BatchID)
}

func TestGetAttempt_NotFound(t *testing.T) {
	dir := t.TempDir()
	runID := "run-20260201-100000-bbbb0001"

	// Write the final archive only — no attempt files.
	writeArchive(t, dir, makeArchive(runID, "passed", makeStep("node", 200, 100)))

	svc := NewArchiveService(dir)
	_, err := svc.GetAttempt(runID, 99)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRunNotFound))
}

func TestGetAttempt_RunNotFound(t *testing.T) {
	dir := t.TempDir()
	svc := NewArchiveService(dir)

	_, err := svc.GetAttempt("run-nonexistent", 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRunNotFound))
}

func TestGetAttempt_BatchMember(t *testing.T) {
	dir := t.TempDir()
	batchID := "batch-20260201-100000-aaaa0001"
	runID := "run-20260201-100000-bbbb0001"

	// Write batch and member archives.
	b := makeBatchArchive(batchID, "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: runID, Outcome: "passed", DurationMs: 100},
	)
	writeBatchArchive(t, dir, b)
	memberArchive := makeArchive(runID, "passed", makeStep("node", 200, 100))
	memberArchive.Metadata.Attempt = 2
	memberArchive.Metadata.TotalAttempts = 2
	writeBatchMemberArchive(t, dir, batchID, memberArchive)

	// Write attempt-01 inside the batch member directory.
	attemptArchive := makeArchive(runID, "failed", makeStep("node", 500, 80))
	attemptArchive.Metadata.Attempt = 1
	attemptArchive.Metadata.TotalAttempts = 2
	attemptPath := filepath.Join(dir, batchID, runID, "attempt-01.json")
	require.NoError(t, archive.Write(attemptArchive, attemptPath))

	svc := NewArchiveService(dir)
	detail, err := svc.GetAttempt(runID, 1)
	require.NoError(t, err)

	assert.Equal(t, runID, detail.RunID)
	assert.Equal(t, "failed", detail.Outcome)
	assert.Equal(t, 1, detail.Attempt)
	assert.Equal(t, batchID, detail.BatchID)
}

// --- GetAttemptStep ---

func TestGetAttemptStep_Success(t *testing.T) {
	dir := t.TempDir()
	runID := "run-20260201-100000-bbbb0001"

	// Write the final archive so loadArchiveWithContext can find the run.
	finalArchive := makeArchive(runID, "passed", makeStep("node", 200, 100))
	finalArchive.Metadata.Attempt = 2
	finalArchive.Metadata.TotalAttempts = 2
	writeArchive(t, dir, finalArchive)

	// Write attempt-01.json with different step data.
	attemptStep := makeStep("node", 500, 80)
	attemptStep.Error = "step failed"
	attemptArchive := makeArchive(runID, "failed", attemptStep)
	attemptArchive.Metadata.Attempt = 1
	attemptArchive.Metadata.TotalAttempts = 2
	attemptPath := filepath.Join(dir, runID, "attempt-01.json")
	require.NoError(t, archive.Write(attemptArchive, attemptPath))

	svc := NewArchiveService(dir)
	detail, err := svc.GetAttemptStep(runID, 1, "node")
	require.NoError(t, err)

	assert.Equal(t, "node", detail.StepID)
	assert.Equal(t, "node", detail.Node)
	assert.Equal(t, 500, detail.Status)
	assert.Equal(t, "step failed", detail.Error)
}

func TestGetAttemptStep_StepNotFound(t *testing.T) {
	dir := t.TempDir()
	runID := "run-20260201-100000-bbbb0001"

	finalArchive := makeArchive(runID, "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, finalArchive)

	attemptArchive := makeArchive(runID, "failed", makeStep("node", 500, 80))
	attemptPath := filepath.Join(dir, runID, "attempt-01.json")
	require.NoError(t, archive.Write(attemptArchive, attemptPath))

	svc := NewArchiveService(dir)
	_, err := svc.GetAttemptStep(runID, 1, "nonexistent")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrStepNotFound))
}

func TestGetAttemptStep_AttemptNotFound(t *testing.T) {
	dir := t.TempDir()
	runID := "run-20260201-100000-bbbb0001"

	finalArchive := makeArchive(runID, "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, finalArchive)

	svc := NewArchiveService(dir)
	_, err := svc.GetAttemptStep(runID, 99, "node")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRunNotFound))
}

// --- naming helpers ---

func TestIsNamed(t *testing.T) {
	tests := []struct {
		dirName string
		want    bool
	}{
		{"run-20260101-100000-aaaa0001", false},
		{"batch-20260101-100000-aaaa0001", false},
		{"my-debug-session", true},
		{"!run-20260101-100000-aaaa0001", true},
		{"!my-custom-name", true},
		{"flight-booking-test", true},
	}
	for _, tt := range tests {
		t.Run(tt.dirName, func(t *testing.T) {
			assert.Equal(t, tt.want, isNamed(tt.dirName))
		})
	}
}

func TestDisplayName(t *testing.T) {
	tests := []struct {
		dirName string
		want    string
	}{
		{"run-20260101-100000-aaaa0001", ""},
		{"batch-20260101-100000-aaaa0001", ""},
		{"my-debug-session", "my-debug-session"},
		{"!run-20260101-100000-aaaa0001", "run-20260101-100000-aaaa0001"},
		{"!my-custom-name", "my-custom-name"},
		{"flight-booking-test", "flight-booking-test"},
	}
	for _, tt := range tests {
		t.Run(tt.dirName, func(t *testing.T) {
			assert.Equal(t, tt.want, displayName(tt.dirName))
		})
	}
}

func TestIsRunDir(t *testing.T) {
	dir := t.TempDir()

	// Create a run dir with archive.json.
	runDir := filepath.Join(dir, "my-run")
	require.NoError(t, createDir(runDir))
	require.NoError(t, writeFile(filepath.Join(runDir, "archive.json"), []byte("{}")))

	// Create a batch dir with only batch.json.
	batchDir := filepath.Join(dir, "my-batch")
	require.NoError(t, createDir(batchDir))
	require.NoError(t, writeFile(filepath.Join(batchDir, "batch.json"), []byte("{}")))

	// Create an empty dir.
	emptyDir := filepath.Join(dir, "empty")
	require.NoError(t, createDir(emptyDir))

	assert.True(t, isRunDir(runDir))
	assert.False(t, isRunDir(batchDir))
	assert.False(t, isRunDir(emptyDir))
}

func TestIsBatchDir(t *testing.T) {
	dir := t.TempDir()

	runDir := filepath.Join(dir, "my-run")
	require.NoError(t, createDir(runDir))
	require.NoError(t, writeFile(filepath.Join(runDir, "archive.json"), []byte("{}")))

	batchDir := filepath.Join(dir, "my-batch")
	require.NoError(t, createDir(batchDir))
	require.NoError(t, writeFile(filepath.Join(batchDir, "batch.json"), []byte("{}")))

	assert.False(t, isBatchDir(runDir))
	assert.True(t, isBatchDir(batchDir))
}

func TestValidateDirName(t *testing.T) {
	assert.NoError(t, validateDirName("my-run"))
	assert.NoError(t, validateDirName("flight-booking"))
	assert.NoError(t, validateDirName(""))
	assert.Error(t, validateDirName("path/with/slash"))
	assert.Error(t, validateDirName("path\\with\\backslash"))
	assert.Error(t, validateDirName("null\x00byte"))
	assert.Error(t, validateDirName("."))
	assert.Error(t, validateDirName(".."))
}

// --- RenameRun / UnnameRun ---

func TestRenameRun_CustomName(t *testing.T) {
	dir := t.TempDir()
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	newRef, err := svc.RenameRun("run-20260101-100000-aaaa0001", "my-debug-run")
	require.NoError(t, err)
	assert.Equal(t, "my-debug-run", newRef)

	// Old dir should not exist.
	_, err = os.Stat(filepath.Join(dir, "run-20260101-100000-aaaa0001"))
	assert.True(t, os.IsNotExist(err))

	// New dir should exist with archive.json.
	assert.True(t, isRunDir(filepath.Join(dir, "my-debug-run")))
}

func TestRenameRun_NameStartingWithRunPrefix(t *testing.T) {
	dir := t.TempDir()
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	newRef, err := svc.RenameRun("run-20260101-100000-aaaa0001", "run-custom-name")
	require.NoError(t, err)
	assert.Equal(t, "!run-custom-name", newRef)

	assert.True(t, isRunDir(filepath.Join(dir, "!run-custom-name")))
}

func TestRenameRun_SaveWithoutNaming(t *testing.T) {
	dir := t.TempDir()
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	newRef, err := svc.RenameRun("run-20260101-100000-aaaa0001", "")
	require.NoError(t, err)
	assert.Equal(t, "!run-20260101-100000-aaaa0001", newRef)

	assert.True(t, isRunDir(filepath.Join(dir, "!run-20260101-100000-aaaa0001")))
}

func TestRenameRun_Collision(t *testing.T) {
	dir := t.TempDir()
	a1 := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a1)

	// Create a conflicting directory.
	require.NoError(t, createDir(filepath.Join(dir, "my-run")))

	svc := NewArchiveService(dir)
	_, err := svc.RenameRun("run-20260101-100000-aaaa0001", "my-run")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestRenameRun_NotFound(t *testing.T) {
	dir := t.TempDir()
	svc := NewArchiveService(dir)

	_, err := svc.RenameRun("run-nonexistent", "my-run")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRunNotFound))
}

func TestRenameRun_InvalidName(t *testing.T) {
	dir := t.TempDir()
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	_, err := svc.RenameRun("run-20260101-100000-aaaa0001", "path/slash")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path separators")
}

func TestRenameRun_NoopSameRef(t *testing.T) {
	dir := t.TempDir()
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a)

	// Rename to a name that produces the same directory name (e.g. the ! prefix already exists).
	svc := NewArchiveService(dir)
	// Save without naming first.
	newRef, err := svc.RenameRun("run-20260101-100000-aaaa0001", "")
	require.NoError(t, err)
	assert.Equal(t, "!run-20260101-100000-aaaa0001", newRef)

	// Now try to save without naming again — should be a no-op.
	newRef, err = svc.RenameRun("!run-20260101-100000-aaaa0001", "")
	require.NoError(t, err)
	assert.Equal(t, "!run-20260101-100000-aaaa0001", newRef)
}

func TestUnnameRun_RestoresOriginalID(t *testing.T) {
	dir := t.TempDir()
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	// First rename.
	newRef, err := svc.RenameRun("run-20260101-100000-aaaa0001", "my-run")
	require.NoError(t, err)
	assert.Equal(t, "my-run", newRef)

	// Now unname.
	restoredRef, err := svc.UnnameRun("my-run")
	require.NoError(t, err)
	assert.Equal(t, "run-20260101-100000-aaaa0001", restoredRef)

	// Original dir should exist again.
	assert.True(t, isRunDir(filepath.Join(dir, "run-20260101-100000-aaaa0001")))
}

func TestUnnameRun_AlreadyUnnamed(t *testing.T) {
	dir := t.TempDir()
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	restoredRef, err := svc.UnnameRun("run-20260101-100000-aaaa0001")
	require.NoError(t, err)
	assert.Equal(t, "run-20260101-100000-aaaa0001", restoredRef)
}

func TestUnnameRun_NotFound(t *testing.T) {
	dir := t.TempDir()
	svc := NewArchiveService(dir)

	_, err := svc.UnnameRun("nonexistent")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRunNotFound))
}

// --- RenameBatch / UnnameBatch ---

func TestRenameBatch_CustomName(t *testing.T) {
	dir := t.TempDir()
	b := makeBatchArchive("batch-20260201-100000-aaaa0001", "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-1", Outcome: "passed", DurationMs: 100},
	)
	writeBatchArchive(t, dir, b)

	svc := NewArchiveService(dir)
	newRef, err := svc.RenameBatch("batch-20260201-100000-aaaa0001", "nightly-regression")
	require.NoError(t, err)
	assert.Equal(t, "nightly-regression", newRef)

	assert.True(t, isBatchDir(filepath.Join(dir, "nightly-regression")))
}

func TestRenameBatch_NameStartingWithBatchPrefix(t *testing.T) {
	dir := t.TempDir()
	b := makeBatchArchive("batch-20260201-100000-aaaa0001", "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-1", Outcome: "passed", DurationMs: 100},
	)
	writeBatchArchive(t, dir, b)

	svc := NewArchiveService(dir)
	newRef, err := svc.RenameBatch("batch-20260201-100000-aaaa0001", "batch-special")
	require.NoError(t, err)
	assert.Equal(t, "!batch-special", newRef)
}

func TestUnnameBatch_RestoresOriginalID(t *testing.T) {
	dir := t.TempDir()
	b := makeBatchArchive("batch-20260201-100000-aaaa0001", "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-1", Outcome: "passed", DurationMs: 100},
	)
	writeBatchArchive(t, dir, b)

	svc := NewArchiveService(dir)
	newRef, err := svc.RenameBatch("batch-20260201-100000-aaaa0001", "nightly")
	require.NoError(t, err)

	restoredRef, err := svc.UnnameBatch(newRef)
	require.NoError(t, err)
	assert.Equal(t, "batch-20260201-100000-aaaa0001", restoredRef)
}

// --- ListRuns with savedOnly ---

func TestListRuns_SavedOnly(t *testing.T) {
	dir := t.TempDir()

	// Create two unnamed runs.
	a1 := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	a1.Metadata.Timestamp = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	a2 := makeArchive("run-20260102-100000-aaaa0002", "passed", makeStep("node", 200, 100))
	a2.Metadata.Timestamp = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	writeArchive(t, dir, a1)
	writeArchive(t, dir, a2)

	svc := NewArchiveService(dir)

	// No saved runs yet.
	runs, err := svc.ListRuns(0, true)
	require.NoError(t, err)
	assert.Nil(t, runs)

	// Rename one run.
	_, err = svc.RenameRun("run-20260101-100000-aaaa0001", "saved-run")
	require.NoError(t, err)

	// Now savedOnly should return 1.
	runs, err = svc.ListRuns(0, true)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "saved-run", runs[0].RunID)
	assert.Equal(t, "saved-run", runs[0].Name)

	// All runs should return 2.
	runs, err = svc.ListRuns(0, false)
	require.NoError(t, err)
	assert.Len(t, runs, 2)
}

func TestListBatches_SavedOnly(t *testing.T) {
	dir := t.TempDir()

	b1 := makeBatchArchive("batch-20260201-100000-aaaa0001", "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-1", Outcome: "passed", DurationMs: 100},
	)
	b1.Metadata.Timestamp = time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	b2 := makeBatchArchive("batch-20260202-100000-aaaa0002", "failed",
		archive.BatchRunEntry{PlanName: "plan2", RunID: "run-2", Outcome: "failed", DurationMs: 200},
	)
	b2.Metadata.Timestamp = time.Date(2026, 2, 2, 10, 0, 0, 0, time.UTC)
	writeBatchArchive(t, dir, b1)
	writeBatchArchive(t, dir, b2)

	svc := NewArchiveService(dir)

	// No saved batches yet.
	batches, err := svc.ListBatches(0, true)
	require.NoError(t, err)
	assert.Nil(t, batches)

	// Rename one batch.
	_, err = svc.RenameBatch("batch-20260201-100000-aaaa0001", "nightly")
	require.NoError(t, err)

	// Now savedOnly should return 1.
	batches, err = svc.ListBatches(0, true)
	require.NoError(t, err)
	require.Len(t, batches, 1)
	assert.Equal(t, "nightly", batches[0].BatchID)
	assert.Equal(t, "nightly", batches[0].Name)
}

// --- Named run/batch discovery in list ---

func TestListRuns_NamedRunsDetectedByContent(t *testing.T) {
	dir := t.TempDir()

	// Create a run under a custom name (no run- prefix).
	customDir := filepath.Join(dir, "my-custom-run")
	require.NoError(t, createDir(customDir))
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	require.NoError(t, archive.Write(a, filepath.Join(customDir, "archive.json")))

	svc := NewArchiveService(dir)
	runs, err := svc.ListRuns(0, false)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "my-custom-run", runs[0].RunID)
	assert.Equal(t, "my-custom-run", runs[0].Name)
}

func TestListRuns_BangPrefixedRunDetected(t *testing.T) {
	dir := t.TempDir()

	// Create a run under !-prefixed name.
	bangDir := filepath.Join(dir, "!run-20260101-100000-aaaa0001")
	require.NoError(t, createDir(bangDir))
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	require.NoError(t, archive.Write(a, filepath.Join(bangDir, "archive.json")))

	svc := NewArchiveService(dir)
	runs, err := svc.ListRuns(0, false)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "!run-20260101-100000-aaaa0001", runs[0].RunID)
	assert.Equal(t, "run-20260101-100000-aaaa0001", runs[0].Name)
}

// --- GetRun/GetBatch set Name ---

func TestGetRun_SetsNameForNamedRun(t *testing.T) {
	dir := t.TempDir()
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	_, err := svc.RenameRun("run-20260101-100000-aaaa0001", "my-saved-run")
	require.NoError(t, err)

	run, err := svc.GetRun("my-saved-run")
	require.NoError(t, err)
	assert.Equal(t, "my-saved-run", run.RunID)
	assert.Equal(t, "my-saved-run", run.Name)
}

func TestGetRun_EmptyNameForUnnamed(t *testing.T) {
	dir := t.TempDir()
	a := makeArchive("run-20260101-100000-aaaa0001", "passed", makeStep("node", 200, 100))
	writeArchive(t, dir, a)

	svc := NewArchiveService(dir)
	run, err := svc.GetRun("run-20260101-100000-aaaa0001")
	require.NoError(t, err)
	assert.Equal(t, "", run.Name)
}

func TestGetBatch_SetsNameForNamedBatch(t *testing.T) {
	dir := t.TempDir()
	b := makeBatchArchive("batch-20260201-100000-aaaa0001", "passed",
		archive.BatchRunEntry{PlanName: "plan1", RunID: "run-1", Outcome: "passed", DurationMs: 100},
	)
	writeBatchArchive(t, dir, b)

	svc := NewArchiveService(dir)
	_, err := svc.RenameBatch("batch-20260201-100000-aaaa0001", "nightly")
	require.NoError(t, err)

	detail, err := svc.GetBatch("nightly")
	require.NoError(t, err)
	assert.Equal(t, "nightly", detail.BatchID)
	assert.Equal(t, "nightly", detail.Name)
}

// --- file helpers ---

func createDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
