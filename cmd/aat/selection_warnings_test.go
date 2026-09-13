package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/engine"
)

const tieWarning = `input "sku": 2 of 5 elements from listProducts.products tie for min price at 12.5; picked index 0`

func tiedDecision() engine.SelectionDecision {
	price := 12.5
	return engine.SelectionDecision{
		InputName: "sku", SourceNode: "listProducts", SourceField: "products", SourceSize: 5, FilteredSize: 5,
		Strategy: "min", SortField: "price", SortValue: &price, Ties: 2,
	}
}

func TestRunShow_StepWarnings(t *testing.T) {
	a := &archive.Archive{
		Result: archive.ArchiveResult{Outcome: "passed"},
		Steps: []archive.StepRecord{{
			StepID: "addItem", Node: "addItem",
			Selections: engine.SelectionRecords([]engine.SelectionDecision{tiedDecision()}),
		}},
	}
	step, id, cleanup, err := findShownStep(a, "addItem")
	require.NoError(t, err)

	var text bytes.Buffer
	require.NoError(t, showStep(&text, step, id, cleanup, showText))
	assert.Contains(t, text.String(), "warnings:\n  "+tieWarning)

	var js bytes.Buffer
	require.NoError(t, showStep(&js, step, id, cleanup, showJSON))
	var view shownStep
	require.NoError(t, json.Unmarshal(js.Bytes(), &view))
	require.Len(t, view.Warnings, 1)
	assert.Contains(t, view.Warnings[0], tieWarning)
}

func TestRunSummary_SelectionWarnings(t *testing.T) {
	result := &engine.RunResult{Steps: []engine.StepResult{{
		StepID: "addItem", Node: "addItem", StatusCode: 200,
		Selections: []engine.SelectionDecision{tiedDecision()},
	}}}

	s := buildRunSummary(result, "")
	require.Len(t, s.Steps, 1)
	require.Len(t, s.Steps[0].Warnings, 1)
	assert.Contains(t, s.Steps[0].Warnings[0], tieWarning)
}

func TestWriteStepResult_SelectionWarning(t *testing.T) {
	var b bytes.Buffer
	writeStepResult(&b, "  ", 0, 1, engine.StepResult{
		StepID: "addItem", Node: "addItem", StatusCode: 200, Response: &adapter.Response{StatusCode: 200},
		Selections: []engine.SelectionDecision{tiedDecision()},
	}, TerminalInfo{Width: 100})
	assert.Contains(t, b.String(), "warning: "+tieWarning)
}
