package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
)

// testArchive builds a minimal archive for testing.
func testArchive(outcome string, steps ...archive.StepRecord) *archive.Archive {
	return &archive.Archive{
		Metadata: archive.ArchiveMetadata{
			Version:     "1.0.0",
			RunID:       "run-20260210-143000-abcd1234",
			Timestamp:   time.Date(2026, 2, 10, 14, 30, 0, 0, time.UTC),
			Plan:        &plan.Plan{Intent: plan.Intent{Goal: "test booking flow"}},
			Environment: "test-env",
		},
		Steps: steps,
		Result: archive.ArchiveResult{
			Outcome: outcome,
		},
	}
}

func testStep(node string, status int, durationMs int64) archive.StepRecord {
	return archive.StepRecord{
		Node:       node,
		DurationMs: durationMs,
		Inputs:     map[string]any{"input1": "val1"},
		Request: &archive.RequestRecord{
			Method: "POST",
			URL:    "https://api.example.com/" + node,
			Body:   json.RawMessage(`{"key":"value"}`),
		},
		Response: &archive.ResponseRecord{
			Status: status,
			Body:   json.RawMessage(`{"result":"ok"}`),
		},
		Outputs: map[string]any{"output1": "out1"},
	}
}

// --- formatDurationMs ---

func TestFormatDurationMs_Milliseconds(t *testing.T) {
	assert.Equal(t, "450ms", formatDurationMs(450))
}

func TestFormatDurationMs_Seconds(t *testing.T) {
	assert.Equal(t, "1.2s", formatDurationMs(1200))
}

func TestFormatDurationMs_Zero(t *testing.T) {
	assert.Equal(t, "0ms", formatDurationMs(0))
}

func TestFormatDurationMs_Negative(t *testing.T) {
	assert.Equal(t, "-450ms", formatDurationMs(-450))
}

func TestFormatDurationMs_LargeValue(t *testing.T) {
	assert.Equal(t, "60.0s", formatDurationMs(60000))
}

// --- truncateBody ---

func TestTruncateBody_ShortJSON(t *testing.T) {
	body := json.RawMessage(`{"key":"value"}`)
	result := truncateBody(body, 2000)
	assert.Contains(t, result, "key")
	assert.Contains(t, result, "value")
	assert.NotContains(t, result, "truncated")
}

func TestTruncateBody_LongJSON(t *testing.T) {
	obj := map[string]string{}
	for i := 0; i < 100; i++ {
		obj["key_with_a_long_name_"+string(rune('a'+i%26))] = "value_with_long_content_that_takes_up_space"
	}
	data, _ := json.Marshal(obj)
	result := truncateBody(json.RawMessage(data), 100)
	assert.Contains(t, result, "truncated")
	assert.LessOrEqual(t, len(result), 200) // 100 + marker
}

func TestTruncateBody_Empty(t *testing.T) {
	assert.Equal(t, "", truncateBody(nil, 2000))
}

func TestTruncateBody_PrettyPrints(t *testing.T) {
	body := json.RawMessage(`{"a":1,"b":2}`)
	result := truncateBody(body, 2000)
	assert.Contains(t, result, "\n") // pretty-printed has newlines
}

// --- suggestNextSteps ---

func TestSuggestNextSteps_KnownCategories(t *testing.T) {
	tests := []struct {
		category string
		contains string
	}{
		{"auth", "credentials"},
		{"client", "inputs"},
		{"server", "server error"},
		{"timeout", "timeout"},
		{"validation", "assertions"},
		{"network", "connectivity"},
	}
	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			result := suggestNextSteps(tt.category)
			assert.NotEmpty(t, result)
			assert.Contains(t, result, tt.contains)
		})
	}
}

func TestSuggestNextSteps_Unknown(t *testing.T) {
	assert.Empty(t, suggestNextSteps("unknown_category"))
}

// --- formatArchiveListEntry ---

func TestFormatArchiveListEntry(t *testing.T) {
	a := testArchive("passed",
		testStep("search", 200, 150),
		testStep("book", 200, 300),
	)
	result := formatArchiveListEntry(a)
	assert.Contains(t, result, "run-20260210-143000-abcd1234")
	assert.Contains(t, result, "passed")
	assert.Contains(t, result, "450ms")
	assert.Contains(t, result, "2 steps")
}

// --- formatArchiveDetail ---

func TestFormatArchiveDetail_BasicStructure(t *testing.T) {
	a := testArchive("passed",
		testStep("search", 200, 150),
		testStep("book", 200, 300),
	)
	result := formatArchiveDetail(a)
	assert.Contains(t, result, "# Run: run-20260210-143000-abcd1234")
	assert.Contains(t, result, "**Outcome:** passed")
	assert.Contains(t, result, "test booking flow")
	assert.Contains(t, result, "[1/2] search")
	assert.Contains(t, result, "[2/2] book")
	assert.Contains(t, result, "POST")
}

func TestFormatArchiveDetail_WithError(t *testing.T) {
	a := testArchive("error")
	a.Result.Error = "connection refused"
	result := formatArchiveDetail(a)
	assert.Contains(t, result, "connection refused")
}

func TestFormatArchiveDetail_WithValidation(t *testing.T) {
	step := testStep("search", 200, 100)
	step.Validation = &archive.ValidationRecord{
		Passed: false,
		Results: []archive.AssertionRecord{
			{Type: "status", Passed: true, Message: "status is 200"},
			{Type: "fieldExists", Passed: false, Message: "missing field: results"},
		},
	}
	a := testArchive("failed", step)
	result := formatArchiveDetail(a)
	assert.Contains(t, result, "Assertions")
	assert.Contains(t, result, "status is 200")
	assert.Contains(t, result, "missing field: results")
	assert.Contains(t, result, "**NO**")
}

func TestFormatArchiveDetail_WithSelections(t *testing.T) {
	step := testStep("book", 200, 100)
	step.Selections = []archive.SelectionRecord{
		{
			InputName:     "offerId",
			SourceNode:    "search",
			SourceField:   "results",
			SourceSize:    10,
			Strategy:      "first",
			SelectedIndex: 0,
		},
	}
	a := testArchive("passed", step)
	result := formatArchiveDetail(a)
	assert.Contains(t, result, "Selections")
	assert.Contains(t, result, "offerId")
	assert.Contains(t, result, "search.results")
}

func TestFormatArchiveDetail_WithCleanup(t *testing.T) {
	a := testArchive("passed", testStep("book", 200, 200))
	a.Cleanup = []archive.StepRecord{testStep("cancelBooking", 200, 100)}
	result := formatArchiveDetail(a)
	assert.Contains(t, result, "Cleanup")
	assert.Contains(t, result, "cancelBooking")
}

// --- formatFailureAnalysis ---

func TestFormatFailureAnalysis_PassedRun(t *testing.T) {
	a := testArchive("passed", testStep("search", 200, 100))
	result := formatFailureAnalysis(a)
	assert.Contains(t, result, "passed")
	assert.Contains(t, result, "inspect_archive")
}

func TestFormatFailureAnalysis_FailedStep(t *testing.T) {
	step := testStep("book", 400, 200)
	step.ErrorClass = &archive.ErrorClassRecord{
		Category: "client",
		Detail:   "bad request",
		Action:   "review inputs",
	}
	a := testArchive("failed", testStep("search", 200, 100), step)
	result := formatFailureAnalysis(a)
	assert.Contains(t, result, "Failure Analysis")
	assert.Contains(t, result, "book")
	assert.Contains(t, result, "client")
	assert.Contains(t, result, "Suggested Next Steps")
	assert.Contains(t, result, "inputs")
}

func TestFormatFailureAnalysis_ServerError(t *testing.T) {
	step := testStep("search", 500, 100)
	a := testArchive("failed", step)
	result := formatFailureAnalysis(a)
	assert.Contains(t, result, "server")
}

func TestFormatFailureAnalysis_AuthError(t *testing.T) {
	step := testStep("search", 401, 100)
	a := testArchive("failed", step)
	result := formatFailureAnalysis(a)
	assert.Contains(t, result, "auth")
	assert.Contains(t, result, "credentials")
}

func TestFormatFailureAnalysis_ValidationFailure(t *testing.T) {
	step := testStep("search", 200, 100)
	step.Validation = &archive.ValidationRecord{
		Passed: false,
		Results: []archive.AssertionRecord{
			{Type: "fieldExists", Passed: false, Message: "missing: id"},
		},
	}
	a := testArchive("failed", step)
	result := formatFailureAnalysis(a)
	assert.Contains(t, result, "validation")
	assert.Contains(t, result, "assertions")
}

func TestFormatFailureAnalysis_ExpectFailurePassed_NotListed(t *testing.T) {
	step := testStep("badRequest", 400, 50)
	step.ExpectFailure = &archive.ExpectFailureRecord{
		Expected: []int{400},
		Actual:   400,
		Passed:   true,
	}
	a := testArchive("passed", step)
	a.Result.Outcome = "passed"
	result := formatFailureAnalysis(a)
	assert.Contains(t, result, "passed")
	assert.NotContains(t, result, "Failed Steps")
}

// --- formatArchiveDiff ---

func TestFormatArchiveDiff_BasicComparison(t *testing.T) {
	a1 := testArchive("passed",
		testStep("search", 200, 150),
		testStep("book", 200, 300),
	)
	a2 := testArchive("failed",
		testStep("search", 200, 200),
		testStep("book", 500, 400),
	)
	a2.Metadata.RunID = "run-20260210-144000-efgh5678"

	result := formatArchiveDiff(a1, a2)
	assert.Contains(t, result, "Archive Diff")
	assert.Contains(t, result, "run-20260210-143000-abcd1234")
	assert.Contains(t, result, "run-20260210-144000-efgh5678")
	assert.Contains(t, result, "passed")
	assert.Contains(t, result, "failed")
	assert.Contains(t, result, "search")
	assert.Contains(t, result, "book")
	assert.Contains(t, result, "Status changed")
}

func TestFormatArchiveDiff_AddedStep(t *testing.T) {
	a1 := testArchive("passed", testStep("search", 200, 100))
	a2 := testArchive("passed",
		testStep("search", 200, 100),
		testStep("book", 200, 200),
	)
	a2.Metadata.RunID = "run-20260210-144000-efgh5678"

	result := formatArchiveDiff(a1, a2)
	assert.Contains(t, result, "added")
}

func TestFormatArchiveDiff_RemovedStep(t *testing.T) {
	a1 := testArchive("passed",
		testStep("search", 200, 100),
		testStep("book", 200, 200),
	)
	a2 := testArchive("passed", testStep("search", 200, 100))
	a2.Metadata.RunID = "run-20260210-144000-efgh5678"

	result := formatArchiveDiff(a1, a2)
	assert.Contains(t, result, "removed")
}

// --- matchSteps ---

func TestMatchSteps_ExactMatch(t *testing.T) {
	s1 := []archive.StepRecord{{Node: "a"}, {Node: "b"}}
	s2 := []archive.StepRecord{{Node: "a"}, {Node: "b"}}
	pairs := matchSteps(s1, s2)
	assert.Len(t, pairs, 2)
	assert.Equal(t, "a", pairs[0].node)
	assert.NotNil(t, pairs[0].s1)
	assert.NotNil(t, pairs[0].s2)
}

func TestMatchSteps_DuplicateNodes(t *testing.T) {
	s1 := []archive.StepRecord{{Node: "a"}, {Node: "a"}}
	s2 := []archive.StepRecord{{Node: "a"}, {Node: "a"}}
	pairs := matchSteps(s1, s2)
	assert.Len(t, pairs, 2)
	// Each s1 step should match a distinct s2 step
	assert.NotNil(t, pairs[0].s2)
	assert.NotNil(t, pairs[1].s2)
}

// --- totalDurationMs ---

func TestTotalDurationMs(t *testing.T) {
	a := testArchive("passed",
		testStep("a", 200, 100),
		testStep("b", 200, 250),
	)
	a.Cleanup = []archive.StepRecord{testStep("c", 200, 50)}
	assert.Equal(t, int64(400), totalDurationMs(a))
}

// --- findFailedSteps ---

func TestFindFailedSteps_MixedResults(t *testing.T) {
	steps := []archive.StepRecord{
		testStep("ok", 200, 100),
		testStep("bad", 400, 100),
		{Node: "err", Error: "connection refused", DurationMs: 50},
	}
	failed := findFailedSteps(steps)
	assert.Len(t, failed, 2)
	assert.Equal(t, "bad", failed[0].Node)
	assert.Equal(t, "err", failed[1].Node)
}

func TestFindFailedSteps_AllPassed(t *testing.T) {
	steps := []archive.StepRecord{
		testStep("ok1", 200, 100),
		testStep("ok2", 201, 100),
	}
	failed := findFailedSteps(steps)
	assert.Empty(t, failed)
}
