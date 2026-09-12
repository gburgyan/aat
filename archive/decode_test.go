package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStepRecord_WritesDurationMs(t *testing.T) {
	data, err := json.Marshal(StepRecord{StepID: "s1", Node: "n", DurationMs: 42})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"durationMs":42`)
	assert.NotContains(t, string(data), "duration_ms")
}

// TestRead_LegacyDurationKey reads an archive written before 0.1.0, whose steps
// and cleanup recorded their duration as duration_ms.
func TestRead_LegacyDurationKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"metadata": {"runId": "run-1"},
		"steps": [
			{"stepId": "a", "node": "a", "duration_ms": 17, "inputs": {}},
			{"stepId": "b", "node": "b", "durationMs": 5, "inputs": {}}
		],
		"cleanup": [{"stepId": "c", "node": "c", "duration_ms": 3, "inputs": {}}],
		"result": {"outcome": "passed"}
	}`), 0o644))

	a, err := Read(path)
	require.NoError(t, err)
	require.Len(t, a.Steps, 2)
	assert.Equal(t, int64(17), a.Steps[0].DurationMs)
	assert.Equal(t, int64(5), a.Steps[1].DurationMs)
	require.Len(t, a.Cleanup, 1)
	assert.Equal(t, int64(3), a.Cleanup[0].DurationMs)
}
