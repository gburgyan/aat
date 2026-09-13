package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/engine"
)

func TestRunSummary_CleanupSkipped(t *testing.T) {
	result := &engine.RunResult{
		Outcome: engine.OutcomePassed,
		CleanupResults: []engine.StepResult{{
			StepID: "confirmCancel", Node: "confirmCancel", StatusCode: 200,
			CleanupFor: "cancelOrder", WhenError: "when state == \"open\": unknown field \"state\"",
		}},
		CleanupSkipped: []engine.CleanupSkip{{
			Node: "voidPayment", CleanupFor: "createPayment", Reason: engine.CleanupSkipReleased, ReleasedBy: "capturePayment",
		}},
	}

	s := buildRunSummary(result, "")
	require.Len(t, s.CleanupSkipped, 1)
	assert.Equal(t, CleanupSkipSummary{Node: "voidPayment", CleanupFor: "createPayment", Reason: "released", ReleasedBy: "capturePayment"}, s.CleanupSkipped[0])
	require.Len(t, s.Cleanup, 1)
	assert.Equal(t, result.CleanupResults[0].WhenError, s.Cleanup[0].WhenError)

	out, err := json.Marshal(s)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"cleanup_skipped":[{"node":"voidPayment","cleanup_for":"createPayment","reason":"released","released_by":"capturePayment"}]`)
}

func TestRunShow_CleanupSkipped(t *testing.T) {
	a := &archive.Archive{
		Result: archive.ArchiveResult{Outcome: "passed"},
		Steps:  []archive.StepRecord{{StepID: "createPayment", Node: "createPayment"}},
		CleanupSkipped: []archive.CleanupSkipRecord{
			{Node: "voidPayment", CleanupFor: "createPayment", Reason: "released", ReleasedBy: "capturePayment"},
			{Node: "cancelOrder", CleanupFor: "createOrder", Reason: "when", When: `status == "open"`},
		},
	}

	var text bytes.Buffer
	require.NoError(t, showRun(&text, a, shownRun{}, false))
	assert.Contains(t, text.String(), "\ncleanup skipped:\n")
	assert.Regexp(t, `voidPayment\s+createPayment\s+released by capturePayment\n`, text.String())
	assert.Regexp(t, `cancelOrder\s+createOrder\s+when status == "open" is false\n`, text.String())

	var js bytes.Buffer
	require.NoError(t, showRun(&js, a, shownRun{}, true))
	var list shownRunList
	require.NoError(t, json.Unmarshal(js.Bytes(), &list))
	assert.Equal(t, []CleanupSkipSummary{
		{Node: "voidPayment", CleanupFor: "createPayment", Reason: "released", ReleasedBy: "capturePayment"},
		{Node: "cancelOrder", CleanupFor: "createOrder", Reason: "when", When: `status == "open"`},
	}, list.CleanupSkipped)
}

func TestWriteCleanupSkip(t *testing.T) {
	var b bytes.Buffer
	writeCleanupSkip(&b, "    ", engine.CleanupSkip{
		Node: "voidPayment", CleanupFor: "createPayment", Reason: engine.CleanupSkipReleased, ReleasedBy: "capturePayment",
	}, TerminalInfo{Width: 100})
	assert.Regexp(t, `^    voidPayment\s+skipped: released by capturePayment \(for createPayment\)\n$`, b.String())
}
