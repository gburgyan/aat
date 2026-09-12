package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBatchSummaryJSONKeys checks the snake_case batch ID of the aat run batch
// --json summary, which an error document for a batch that never started
// leaves out.
func TestBatchSummaryJSONKeys(t *testing.T) {
	data, err := json.Marshal(&BatchSummary{Outcome: "passed", BatchID: "batch-20260911-120000-abcdef01", Runs: []BatchRunResult{}})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"batch_id":"batch-20260911-120000-abcdef01"`)
	assert.NotContains(t, string(data), "batchId")

	data, err = json.Marshal(&BatchSummary{Outcome: "error", Error: "no plans", Runs: []BatchRunResult{}})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "batch_id")
	assert.Contains(t, string(data), `"error":"no plans"`)
}
