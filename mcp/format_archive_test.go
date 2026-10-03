package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestFormatArchiveDetail_WithIterations(t *testing.T) {
	step := testStep("getExport", 200, 600)
	for i := 1; i <= 25; i++ {
		it := archive.IterationRecord{
			Index:      i,
			DurationMs: 20,
			Response:   &archive.ResponseRecord{Status: 200},
			Outputs:    map[string]any{"status": "running", "rows": []any{}, "note": "a|b"},
		}
		if i == 25 {
			it.Outputs["status"] = "complete"
			it.UntilMet = true
		}
		step.Iterations = append(step.Iterations, it)
	}
	step.Iterations[3].Error = "status 503"
	step.RepeatStop = "until"

	result := formatArchiveDetail(testArchive("passed", step))
	assert.Contains(t, result, "**Requests:** 25 (stopped: until)")
	assert.Contains(t, result, "| # | Status | Time | Until | Outputs |")
	assert.Regexp(t, `\| 1 \| 200 \| \S+ \|  \| note="a\\\|b", status="running" \|`, result, "scalar outputs, with the table's pipe escaped")
	assert.Regexp(t, `\| 4 \| 200 \| \S+ \|  \| error: status 503 \|`, result)
	assert.Contains(t, result, "| 19 | 200 |")
	assert.NotContains(t, result, "| 20 | 200 |")
	assert.Contains(t, result, "| … | | | | 5 more |")
	assert.Regexp(t, `\| 25 \| 200 \| \S+ \| met \| note="a\\\|b", status="complete" \|`, result)
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
		Expected: plan.HTTPStatuses([]int{400}),
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

// TestFindFailedSteps_Fuzz checks that a fuzz case is failed only when its
// finding failed the run, and that setup copies, which never fail it, are
// left out.
func TestFindFailedSteps_Fuzz(t *testing.T) {
	refused := testStep("addItem", 400, 10)
	refused.StepID, refused.Fuzz = "add__fuzz_quantity_below_min", &archive.FuzzRecord{ID: "quantity.below-min"}
	accepted := testStep("listProducts", 200, 10)
	accepted.StepID = "list__fuzz_category_not_in_enum"
	accepted.Fuzz = &archive.FuzzRecord{ID: "category.not-in-enum", Target: "list", Mode: "negative", Finding: "accepted-invalid", Fails: true}
	failedCopy := testStep("createCart", 503, 10)
	failedCopy.FuzzSetup, failedCopy.FuzzSetupFailed = "add__fuzz_quantity_below_min", true

	failed := findFailedSteps([]archive.StepRecord{testStep("listProducts", 200, 10), refused, failedCopy, accepted})
	require.Len(t, failed, 1)
	assert.Equal(t, "list__fuzz_category_not_in_enum", failed[0].StepID)

	out := formatFailureAnalysis(testArchive("failed", testStep("listProducts", 200, 10), refused, failedCopy, accepted))
	assert.Contains(t, out, "**Fuzz case:** `category.not-in-enum` on step list, judged as negative: **accepted-invalid**")
	assert.Contains(t, out, "- **fuzz:**")
	assert.NotContains(t, out, "- **client:**", "a refused case is not the failure")
}

func TestFindFailedSteps_AllPassed(t *testing.T) {
	steps := []archive.StepRecord{
		testStep("ok1", 200, 100),
		testStep("ok2", 201, 100),
	}
	failed := findFailedSteps(steps)
	assert.Empty(t, failed)
}

func TestFormatArchiveDetail_FormBody(t *testing.T) {
	step := testStep("createPaymentIntent", 200, 120)
	body, err := json.Marshal("amount=2000&metadata[source]=aat-stripe&name=AAT+Stripe")
	assert.NoError(t, err)
	step.Request.Headers = map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	step.Request.Body = body

	result := formatArchiveDetail(testArchive("passed", step))
	assert.Contains(t, result, "**Request Body** (form):")
	assert.Contains(t, result, "amount=2000\nmetadata[source]=aat-stripe\nname=AAT Stripe")
	assert.NotContains(t, result, `&`, "not the one escaped string the archive holds")
}
