package main

import (
	"bytes"
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

const showRunID = "run-20260912-100000-aaaa0001"

// showTestArchive is a checkout run: two main steps, a verification step, and
// two cleanup steps, one of them failed.
func showTestArchive() *archive.Archive {
	return &archive.Archive{
		Metadata: archive.ArchiveMetadata{
			Version:   "1.0.0",
			RunID:     showRunID,
			Timestamp: time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC),
			Plan: &plan.Plan{
				Intent:    plan.Intent{Goal: "checkout", Description: "Check out a cart"},
				Execution: plan.Execution{Verification: []plan.VerificationStep{{Node: "getOrder"}}},
			},
		},
		Steps: []archive.StepRecord{
			{
				StepID: "createCart", Node: "createCart", DurationMs: 3,
				Request:  &archive.RequestRecord{Method: "POST", URL: "http://localhost:8765/us/v1/carts", Body: json.RawMessage(`{}`)},
				Response: &archive.ResponseRecord{Status: 201, Body: json.RawMessage(`{"cartId": "cart_0001"}`)},
				Outputs:  map[string]any{"cartId": "cart_0001"},
			},
			{
				StepID: "checkout", Node: "checkoutCart", DurationMs: 12,
				Inputs:  map[string]any{"cartId": "cart_0001", "items": []any{"a", "b"}},
				Request: &archive.RequestRecord{Method: "POST", URL: "http://localhost:8765/us/v1/carts/cart_0001/checkout", Body: json.RawMessage(`{"shippingTier": "standard"}`)},
				Response: &archive.ResponseRecord{Status: 201, Body: json.RawMessage(
					`{"orderId": "ord_0001", "lines": [{"sku": "SKU-1", "qty": 1}, {"sku": "SKU-2", "qty": 2, "gift": true}], "total": 10340}`)},
				Outputs: map[string]any{"orderId": "ord_0001", "total": 10340.0},
				Validation: &archive.ValidationRecord{Passed: true, Results: []archive.AssertionRecord{
					{Type: "status", Passed: true, Message: "status code is 201"},
				}},
			},
			{
				StepID: "verify_getOrder", Node: "getOrder", DurationMs: 2,
				Response: &archive.ResponseRecord{Status: 200, Body: json.RawMessage(`{"status": "created"}`)},
			},
		},
		Cleanup: []archive.StepRecord{
			{StepID: "deleteOrder", Node: "deleteOrder", CleanupFor: "checkout", DurationMs: 1, Response: &archive.ResponseRecord{Status: 204}},
			{StepID: "deleteCart", Node: "deleteCart", CleanupFor: "createCart", DurationMs: 1, Response: &archive.ResponseRecord{Status: 404}, Error: "status 404"},
		},
		Result: archive.ArchiveResult{Outcome: "passed", DurationMs: 25},
	}
}

// writeShowArchive writes a at rel under dir and returns the run directory.
func writeShowArchive(t *testing.T, dir, rel string, a *archive.Archive) string {
	t.Helper()
	runDir := filepath.Join(dir, rel)
	require.NoError(t, archive.Write(a, filepath.Join(runDir, "archive.json")))
	return runDir
}

// runShow runs aat run show with archiveDir as the archive directory.
func runShow(t *testing.T, archiveDir, ref string, opts showOptions) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err = runShowCommand(ref, func() (string, error) { return archiveDir, nil }, opts, &out, &errOut)
	return out.String(), errOut.String(), err
}

func TestRunShow_StepList(t *testing.T) {
	dir := t.TempDir()
	writeShowArchive(t, dir, showRunID, showTestArchive())

	out, _, err := runShow(t, dir, "latest", showOptions{})
	require.NoError(t, err)
	assert.Regexp(t, `^run-20260912-100000-aaaa0001  PASSED  \S+\n`, out)
	assert.Contains(t, out, "plan: Check out a cart\n")
	assert.Regexp(t, `(?m)^\s+1\s+createCart\s+createCart\s+201\s+pass\s+\S+\s+cartId$`, out)
	assert.Regexp(t, `(?m)^\s+2\s+checkout\s+checkoutCart\s+201\s+pass\s+\S+\s+orderId, total$`, out)
	assert.Regexp(t, `(?ms)^verification:\n.*^\s+3\s+verify_getOrder\s+getOrder\s+200\s+pass`, out)
	assert.Regexp(t, `(?ms)^cleanup:\n.*^\s+1\s+deleteOrder\s+deleteOrder\s+204\s+pass\s+\S+\s+checkout$`, out)
	assert.Regexp(t, `(?m)^\s+2\s+deleteCart\s+deleteCart\s+404\s+FAIL\s+\S+\s+createCart$`, out)
}

func TestRunShow_StepListJSON(t *testing.T) {
	dir := t.TempDir()
	writeShowArchive(t, dir, showRunID, showTestArchive())

	out, _, err := runShow(t, dir, showRunID, showOptions{JSON: true})
	require.NoError(t, err)
	var list shownRunList
	require.NoError(t, json.Unmarshal([]byte(out), &list), out)
	assert.Equal(t, showRunID, list.Run)
	assert.Equal(t, "passed", list.Outcome)
	require.Len(t, list.Steps, 3)
	assert.Equal(t, []string{"orderId", "total"}, list.Steps[1].Outputs)
	assert.False(t, list.Steps[1].Verification)
	assert.True(t, list.Steps[2].Verification)
	require.Len(t, list.Cleanup, 2)
	assert.Equal(t, "checkout", list.Cleanup[0].CleanupFor)
	assert.False(t, list.Cleanup[1].Passed)
	assert.Contains(t, out, `"step_id": "checkout"`, "snake_case keys, as in every --json document")
}

func TestRunShow_Step(t *testing.T) {
	dir := t.TempDir()
	writeShowArchive(t, dir, showRunID, showTestArchive())

	for _, name := range []string{"checkout", "checkoutCart"} {
		t.Run(name, func(t *testing.T) {
			out, _, err := runShow(t, dir, "latest", showOptions{Step: name})
			require.NoError(t, err)
			assert.Regexp(t, `^step checkout \(node checkoutCart\)\n`, out)
			assert.Contains(t, out, "\nPOST http://localhost:8765/us/v1/carts/cart_0001/checkout\n")
			assert.Contains(t, out, "\nstatus 201  pass  ")
			assert.Regexp(t, `(?m)^  items\s+\[2 items\]$`, out)
			assert.Regexp(t, `(?m)^  orderId\s+"ord_0001"$`, out)
			assert.Regexp(t, `(?m)^  total\s+10340$`, out)
			assert.Contains(t, out, "assertions: 1 passed, 0 failed\n")
			assert.Regexp(t, `(?m)^response body: \d+ bytes$`, out)
		})
	}

	out, _, err := runShow(t, dir, "latest", showOptions{Step: "deleteCart"})
	require.NoError(t, err)
	assert.Regexp(t, `^cleanup step deleteCart, for createCart\n`, out)
	assert.Contains(t, out, "status 404  FAIL")
	assert.Contains(t, out, "error: status 404\n")

	out, _, err = runShow(t, dir, "latest", showOptions{Step: "checkout", JSON: true})
	require.NoError(t, err)
	var step shownStep
	require.NoError(t, json.Unmarshal([]byte(out), &step), out)
	assert.Equal(t, "checkoutCart", step.Node)
	assert.Equal(t, 201, step.Status)
	assert.Equal(t, "ord_0001", step.Outputs["orderId"])
}

func TestRunShow_StepNotFound(t *testing.T) {
	dir := t.TempDir()
	a := showTestArchive()
	a.Steps = append(a.Steps, archive.StepRecord{StepID: "checkoutAgain", Node: "checkoutCart"})
	writeShowArchive(t, dir, showRunID, a)

	_, _, err := runShow(t, dir, "latest", showOptions{Step: "nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no step "nope" in the run; its steps are createCart, checkout, verify_getOrder, checkoutAgain, deleteOrder, deleteCart`)

	_, _, err = runShow(t, dir, "latest", showOptions{Step: "checkoutCart"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "node checkoutCart ran as 2 steps, so name one with --step: checkout, checkoutAgain")
}

func TestRunShow_Parts(t *testing.T) {
	dir := t.TempDir()
	writeShowArchive(t, dir, showRunID, showTestArchive())

	tests := []struct {
		name string
		opts showOptions
		want string // the JSON printed, compared as JSON
	}{
		{name: "request", opts: showOptions{Part: "request"}, want: `{"shippingTier": "standard"}`},
		{name: "response", opts: showOptions{Part: "response"}, want: `{"orderId": "ord_0001", "lines": [{"sku": "SKU-1", "qty": 1}, {"sku": "SKU-2", "qty": 2, "gift": true}], "total": 10340}`},
		{name: "response path", opts: showOptions{Part: "response", Path: "lines.1.sku"}, want: `"SKU-2"`},
		{name: "every element", opts: showOptions{Part: "response", Path: "lines.#.sku"}, want: `["SKU-1", "SKU-2"]`},
		{name: "JSONPath form", opts: showOptions{Part: "response", Path: "$.lines[0].qty"}, want: `1`},
		{name: "inputs path", opts: showOptions{Part: "inputs", Path: "items"}, want: `["a", "b"]`},
		{name: "outputs", opts: showOptions{Part: "outputs"}, want: `{"orderId": "ord_0001", "total": 10340}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.Step = "checkout"
			out, _, err := runShow(t, dir, "latest", tt.opts)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, out)
		})
	}
}

func TestRunShow_Shape(t *testing.T) {
	dir := t.TempDir()
	writeShowArchive(t, dir, showRunID, showTestArchive())

	out, _, err := runShow(t, dir, "latest", showOptions{Step: "checkout", Part: "response", Shape: true})
	require.NoError(t, err)
	assert.Regexp(t, `(?m)^orderId\s+string\s+"ord_0001"$`, out)
	assert.Regexp(t, `(?m)^lines\s+array\s+2 items$`, out)
	assert.Regexp(t, `(?m)^lines\.#\.gift\s+boolean\s+in 1 of 2\s+true$`, out)

	out, _, err = runShow(t, dir, "latest", showOptions{Step: "checkout", Part: "response", Path: "lines", Shape: true, JSON: true})
	require.NoError(t, err)
	var lines []archive.ShapeLine
	require.NoError(t, json.Unmarshal([]byte(out), &lines), out)
	require.NotEmpty(t, lines)
	assert.Equal(t, archive.ShapeLine{Path: "@this", Type: "array", Items: "2"}, lines[0])
}

func TestRunShow_PartErrors(t *testing.T) {
	dir := t.TempDir()
	writeShowArchive(t, dir, showRunID, showTestArchive())

	_, _, err := runShow(t, dir, "latest", showOptions{Step: "checkout", Part: "response", Path: "nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--path nope matches nothing in the response body of step checkout; its top-level keys are orderId, lines, total")

	_, _, err = runShow(t, dir, "latest", showOptions{Step: "verify_getOrder", Part: "request"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "step verify_getOrder has no request body")
}

func TestRunShow_MaxBytes(t *testing.T) {
	dir := t.TempDir()
	writeShowArchive(t, dir, showRunID, showTestArchive())

	out, errOut, err := runShow(t, dir, "latest", showOptions{Step: "checkout", Part: "response", MaxBytes: 20})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(out), 21)
	assert.Contains(t, errOut, "aat: output cut at 20 of ")
	assert.Contains(t, errOut, "--max-bytes")
}

// TestRunShow_Paths: a run directory, an archive.json, and an exported .aar
// file are read directly, without an archive directory.
func TestRunShow_Paths(t *testing.T) {
	dir := t.TempDir()
	runDir := writeShowArchive(t, dir, showRunID, showTestArchive())
	require.NoError(t, archive.Write(showTestArchive(), filepath.Join(runDir, "attempt-01.json")))

	aar := filepath.Join(t.TempDir(), "run.aar")
	f, err := os.Create(aar)
	require.NoError(t, err)
	require.NoError(t, archive.ExportDir(runDir, f))
	require.NoError(t, f.Close())

	noArchiveDir := func() (string, error) {
		t.Fatal("a path needs no archive directory")
		return "", nil
	}
	for _, ref := range []string{runDir, filepath.Join(runDir, "archive.json"), aar} {
		var out bytes.Buffer
		require.NoError(t, runShowCommand(ref, noArchiveDir, showOptions{}, &out, &bytes.Buffer{}), ref)
		assert.Contains(t, out.String(), "checkoutCart", ref)
	}

	var out bytes.Buffer
	require.NoError(t, runShowCommand(filepath.Join(runDir, "archive.json"), noArchiveDir, showOptions{}, &out, &bytes.Buffer{}))
	assert.Contains(t, out.String(), "other attempts: attempt-01.json\n")
}

func TestRunShow_Batch(t *testing.T) {
	dir := t.TempDir()
	batchDir := filepath.Join(dir, "batch-20260912-100000-bbbb0001")
	writeShowArchive(t, batchDir, showRunID, showTestArchive())
	require.NoError(t, os.WriteFile(filepath.Join(batchDir, "batch.json"), []byte(`{}`), 0o644))

	out, _, err := runShow(t, dir, "batch-20260912-100000-bbbb0001/"+showRunID, showOptions{})
	require.NoError(t, err)
	assert.Regexp(t, `^batch-20260912-100000-bbbb0001/run-20260912-100000-aaaa0001  PASSED`, out)

	out, _, err = runShow(t, dir, batchDir, showOptions{})
	require.NoError(t, err)
	assert.Contains(t, out, "runs: 0, 0 passed, 0 failed, 0 errors")
}

func TestRunShow_RunNotFound(t *testing.T) {
	_, _, err := runShow(t, t.TempDir(), "run-missing", showOptions{})
	require.ErrorIs(t, err, archive.ErrRunNotFound)
	assert.Contains(t, err.Error(), "a run is latest, a run ID, batch-ID/run-ID, or a path")
}
