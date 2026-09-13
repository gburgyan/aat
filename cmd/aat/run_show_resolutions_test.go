package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/archive"
)

// resolvedArchive holds one step that failed while resolving its inputs: two
// resolved, one from a selection, and one that couldn't be.
func resolvedArchive() *archive.Archive {
	price := 12.5
	return &archive.Archive{
		Result: archive.ArchiveResult{Outcome: "error"},
		Steps: []archive.StepRecord{{
			StepID: "addItem", Node: "addItem",
			Error:  `resolving inputs: resolving input "quantity" for node "addItem": required input has no value`,
			Inputs: map[string]any{"sku": "SKU-1", "currency": "USD"},
			Selections: []archive.SelectionRecord{{
				InputName: "sku", SourceNode: "listProducts", SourceField: "products", SourceSize: 3, FilteredSize: 3,
				Strategy: "min", SortField: "price", SortValue: &price, Ties: 1, SelectedIndex: 2,
			}},
			Resolutions: []archive.ValueResolutionRecord{
				{InputName: "sku", Source: "select_edge", FinalValue: "SKU-1", FromStep: "listProducts", FromOutput: "products"},
				{InputName: "currency", Source: "plan_from", FinalValue: "USD", FromStep: "listProducts", FromOutput: "currency"},
				{InputName: "quantity", Source: "error", Error: "required input has no value"},
			},
		}},
	}
}

func TestRunShow_ResolutionsPart(t *testing.T) {
	a := resolvedArchive()
	step, id, _, err := findShownStep(a, "addItem")
	require.NoError(t, err)

	var out, errOut bytes.Buffer
	require.NoError(t, showStepPart(&out, &errOut, step, id, showOptions{Step: "addItem", Part: "resolutions"}))
	var got []map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	require.Len(t, got, 3)
	assert.Equal(t, "select_edge", got[0]["source"])
	assert.Equal(t, "min", got[0]["selection"].(map[string]any)["strategy"])
	assert.Equal(t, "required input has no value", got[2]["error"])

	out.Reset()
	require.NoError(t, showStepPart(&out, &errOut, step, id, showOptions{Step: "addItem", Part: "resolutions", Path: "2.error"}))
	assert.Equal(t, "\"required input has no value\"\n", out.String())
}

func TestRunShow_StepShowsInputSources(t *testing.T) {
	a := resolvedArchive()
	step, id, cleanup, err := findShownStep(a, "addItem")
	require.NoError(t, err)

	var b bytes.Buffer
	require.NoError(t, showStep(&b, step, id, cleanup, showText))
	text := b.String()
	assert.Regexp(t, `\n  currency\s+"USD"\s+plan_from listProducts\.currency\n`, text)
	assert.Regexp(t, `\n  quantity\s+-\s+error required input has no value\n`, text)
	assert.Regexp(t, `\n  sku\s+"SKU-1"\s+select_edge min price of listProducts\.products\[2\]\n`, text)
}

func TestRunShow_StepJSONIncludesResolutions(t *testing.T) {
	a := resolvedArchive()
	step, id, cleanup, err := findShownStep(a, "addItem")
	require.NoError(t, err)

	var b bytes.Buffer
	require.NoError(t, showStep(&b, step, id, cleanup, showJSON))
	var view shownStep
	require.NoError(t, json.Unmarshal(b.Bytes(), &view))
	require.Len(t, view.Resolutions, 3)
	require.NotNil(t, view.Resolutions[0].Selection)
	assert.Equal(t, "price", view.Resolutions[0].Selection.SortField)
	assert.Equal(t, "error", view.Resolutions[2].Source)
}
