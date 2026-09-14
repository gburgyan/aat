package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/archive"
)

const showBatchID = "batch-20260912-110000-bbbb0002"

// writeShowBatch writes a batch of three permutations of one plan: a run whose
// cleanup failed once and was skipped once, a duplicate that didn't run, and a
// failed run whose cleanup was skipped by its when condition.
func writeShowBatch(t *testing.T, dir string) string {
	t.Helper()
	batchDir := filepath.Join(dir, showBatchID)

	passing := showTestArchive()
	passing.CleanupSkipped = []archive.CleanupSkipRecord{{Node: "cancelPayment", CleanupFor: "checkout", Reason: "released", ReleasedBy: "refund"}}
	writeShowArchive(t, batchDir, "run-20260912-110000-aaaa0001", passing)

	failing := showTestArchive()
	failing.Cleanup = nil
	failing.CleanupSkipped = []archive.CleanupSkipRecord{{Node: "deleteOrder", CleanupFor: "checkout", Reason: "when", When: `status == "open"`}}
	failing.Result = archive.ArchiveResult{Outcome: "failed", DurationMs: 30}
	writeShowArchive(t, batchDir, "run-20260912-110001-aaaa0003", failing)

	b := &archive.BatchArchive{
		Metadata: archive.BatchMetadata{BatchID: showBatchID, Timestamp: time.Date(2026, 9, 12, 11, 0, 0, 0, time.UTC)},
		Runs: []archive.BatchRunEntry{
			{PlanName: "checkout", RunID: "run-20260912-110000-aaaa0001", Outcome: "passed", StepCount: 3, PassedCount: 3, DurationMs: 25, Layers: []string{"card-visa", "currency-usd"}},
			{PlanName: "checkout", Outcome: "skipped", Layers: []string{"card-visa", "currency-eur"}, Skipped: true, DuplicateOf: "checkout [card-visa,currency-usd]"},
			{PlanName: "checkout", RunID: "run-20260912-110001-aaaa0003", Outcome: "failed", StepCount: 3, PassedCount: 2, FailedCount: 1, DurationMs: 30, Layers: []string{"card-amex"}, Error: "step checkout returned status 402"},
		},
		Result: archive.BatchResult{Outcome: "failed", TotalRuns: 3, PassedRuns: 1, FailedRuns: 1, SkippedRuns: 1, TotalDurationMs: 60},
	}
	require.NoError(t, archive.WriteBatch(b, filepath.Join(batchDir, "batch.json")))
	return batchDir
}

func TestRunShow_BatchView(t *testing.T) {
	dir := t.TempDir()
	batchDir := writeShowBatch(t, dir)

	out, _, err := runShow(t, dir, showBatchID, showOptions{})
	require.NoError(t, err)
	assert.Regexp(t, `^batch-20260912-110000-bbbb0002  FAILED  `, out)
	assert.Contains(t, out, "runs: 3, 1 passed, 1 failed, 0 errors, 1 skipped as duplicates\n")
	assert.Regexp(t, `run-20260912-110000-aaaa0001\s+checkout\s+card-visa,currency-usd\s+pass\s+3/3`, out)
	assert.Regexp(t, `-\s+checkout\s+card-visa,currency-eur\s+skip\s+-\s+-\s+duplicate of checkout \[card-visa,currency-usd\]`, out)
	assert.Regexp(t, `run-20260912-110001-aaaa0003\s+checkout\s+card-amex\s+FAIL\s+2/3\s+\S+\s+step checkout returned status 402`, out)
	assert.Regexp(t, `cleanup:\n\s+NODE\s+RAN\s+FAILED\s+RELEASED\s+WHEN\n`, out)
	assert.Regexp(t, `cancelPayment\s+0\s+0\s+1\s+0\n`, out)
	assert.Regexp(t, `deleteCart\s+1\s+1\s+0\s+0\n`, out)
	assert.Regexp(t, `deleteOrder\s+1\s+0\s+0\s+1\n`, out)
	assert.Contains(t, out, "cleanup failures:\n  run-20260912-110000-aaaa0001  deleteCart (for createCart): status 404\n")

	pathOut, _, err := runShow(t, dir, filepath.Join(batchDir, "batch.json"), showOptions{})
	require.NoError(t, err)
	assert.Equal(t, out, pathOut, "a path to batch.json shows the same batch")

	jsonOut, _, err := runShow(t, dir, showBatchID, showOptions{JSON: true})
	require.NoError(t, err)
	var view shownBatch
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &view))
	assert.Equal(t, 3, view.TotalRuns)
	assert.Len(t, view.Runs, 3)
	assert.Equal(t, []shownCleanupNode{
		{Node: "cancelPayment", Released: 1},
		{Node: "deleteCart", Ran: 1, Failed: 1},
		{Node: "deleteOrder", Ran: 1, When: 1},
	}, view.Cleanup)
	require.Len(t, view.CleanupFailures, 1)
	assert.Equal(t, 404, view.CleanupFailures[0].Status)

	_, _, err = runShow(t, dir, showBatchID, showOptions{Step: "checkout"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is a batch, which has no steps of its own")
}

// writeOASBatch writes a batch of three runs: two layer permutations of the
// checkout plan, one clean and one with a violation under strict OpenAPI
// validation, and a smoke run from before the mode was recorded.
func writeOASBatch(t *testing.T, dir string) {
	t.Helper()
	batchDir := filepath.Join(dir, showBatchID)
	valid := &archive.OASPayloadRecord{Valid: true}

	clean := showTestArchive()
	clean.Metadata.OASValidation = "strict"
	clean.Steps[0].OASValidation = &archive.OASValidationRecord{Request: valid, Response: valid}
	clean.Steps[1].OASValidation = &archive.OASValidationRecord{Request: valid, Response: valid}
	writeShowArchive(t, batchDir, "run-20260912-110000-aaaa0001", clean)

	violating := showTestArchive()
	violating.Metadata.OASValidation = "strict"
	violating.Steps[1].OASValidation = &archive.OASValidationRecord{
		Response: &archive.OASPayloadRecord{Errors: []archive.OASSchemaError{{Path: "/total", Message: "expected integer"}}},
	}
	writeShowArchive(t, batchDir, "run-20260912-110001-aaaa0002", violating)

	writeShowArchive(t, batchDir, "run-20260912-110002-aaaa0003", showTestArchive())

	b := &archive.BatchArchive{
		Metadata: archive.BatchMetadata{BatchID: showBatchID},
		Runs: []archive.BatchRunEntry{
			{PlanName: "checkout", RunID: "run-20260912-110000-aaaa0001", Outcome: "passed", StepCount: 3, PassedCount: 3, Layers: []string{"shipping-standard"}},
			{PlanName: "checkout", RunID: "run-20260912-110001-aaaa0002", Outcome: "failed", StepCount: 3, PassedCount: 2, FailedCount: 1, Layers: []string{"shipping-express"}},
			{PlanName: "smoke", RunID: "run-20260912-110002-aaaa0003", Outcome: "passed", StepCount: 3, PassedCount: 3},
		},
		Result: archive.BatchResult{Outcome: "failed", TotalRuns: 3, PassedRuns: 2, FailedRuns: 1},
	}
	require.NoError(t, archive.WriteBatch(b, filepath.Join(batchDir, "batch.json")))
}

// TestRunShow_BatchViewOAS checks the batch view's OpenAPI validation totals,
// summed from its runs' summaries, and a run's oas line.
func TestRunShow_BatchViewOAS(t *testing.T) {
	dir := t.TempDir()
	writeOASBatch(t, dir)

	out, _, err := runShow(t, dir, showBatchID, showOptions{})
	require.NoError(t, err)
	assert.Contains(t, out, "runs: 3, 2 passed, 1 failed, 0 errors\n"+
		"oas: strict, 2 requests and 3 responses validated, 1 violation, recorded by 2 of 3 runs\n\n")

	jsonOut, _, err := runShow(t, dir, showBatchID, showOptions{JSON: true})
	require.NoError(t, err)
	var view shownBatch
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &view))
	assert.Equal(t, &shownOAS{Mode: "strict", ValidatedRequests: 2, ValidatedResponses: 3, Violations: 1, Runs: 2}, view.OAS)

	runOut, _, err := runShow(t, dir, showBatchID+"/run-20260912-110001-aaaa0002", showOptions{})
	require.NoError(t, err)
	assert.Contains(t, runOut, "oas: strict, 0 requests and 1 response validated, 1 violation\n")

	runJSON, _, err := runShow(t, dir, showBatchID+"/run-20260912-110000-aaaa0001", showOptions{JSON: true})
	require.NoError(t, err)
	var list shownRunList
	require.NoError(t, json.Unmarshal([]byte(runJSON), &list))
	assert.Equal(t, &shownOAS{Mode: "strict", ValidatedRequests: 2, ValidatedResponses: 2}, list.OAS)
}

// TestRunShow_PlanInBatch names a batch's run by its plan: a plan with one run
// resolves, and one with several is an error that lists each with its layers.
func TestRunShow_PlanInBatch(t *testing.T) {
	dir := t.TempDir()
	writeOASBatch(t, dir)

	out, _, err := runShow(t, dir, showBatchID+"/smoke", showOptions{})
	require.NoError(t, err)
	assert.Regexp(t, `^`+showBatchID+`/run-20260912-110002-aaaa0003  PASSED`, out)
	assert.NotContains(t, out, "oas:", "a run from before the mode was recorded")

	_, _, err = runShow(t, dir, showBatchID+"/checkout", showOptions{Step: "checkout"})
	require.ErrorIs(t, err, archive.ErrAmbiguousRun)
	assert.EqualError(t, err, `plan "checkout" ran as 2 runs in batch `+showBatchID+`; name one of them:
  `+showBatchID+`/run-20260912-110000-aaaa0001  layers shipping-standard
  `+showBatchID+`/run-20260912-110001-aaaa0002  layers shipping-express`)

	_, _, err = runShow(t, dir, showBatchID+"/full-lifecycle", showOptions{})
	require.ErrorIs(t, err, archive.ErrRunNotFound)
	assert.Contains(t, err.Error(), "batch-ID/plan-name")
}

func TestRunShow_PartAcrossSteps(t *testing.T) {
	dir := t.TempDir()
	writeShowArchive(t, dir, showRunID, showTestArchive())

	out, _, err := runShow(t, dir, "latest", showOptions{Part: "response", Path: "orderId"})
	require.NoError(t, err)
	assert.Equal(t, "STEP      NODE          VALUE\ncheckout  checkoutCart  \"ord_0001\"\n", out)

	jsonOut, _, err := runShow(t, dir, "latest", showOptions{Part: "outputs", JSON: true, Compact: true})
	require.NoError(t, err)
	assert.JSONEq(t, `[
		{"step_id": "createCart", "node": "createCart", "value": {"cartId": "cart_0001"}},
		{"step_id": "checkout", "node": "checkoutCart", "value": {"orderId": "ord_0001", "total": 10340}}
	]`, jsonOut)

	_, _, err = runShow(t, dir, "latest", showOptions{Part: "response", Path: "nothing.here"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--path nothing.here matches nothing in the response body of any step")
}

// TestRunShow_StepJSONNamesValidation checks that a step's --json names its
// assertion results validation, shaped as in archive.json.
func TestRunShow_StepJSONNamesValidation(t *testing.T) {
	dir := t.TempDir()
	writeShowArchive(t, dir, showRunID, showTestArchive())

	out, _, err := runShow(t, dir, "latest", showOptions{Step: "checkout", JSON: true})
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &doc))
	assert.NotContains(t, doc, "assertions")
	assert.Equal(t, map[string]any{
		"passed":  true,
		"results": []any{map[string]any{"type": "status", "passed": true, "message": "status code is 201"}},
	}, doc["validation"])
}
