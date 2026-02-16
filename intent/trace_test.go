package intent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWritePlanTrace_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan-trace.json")

	trace := &PlanTrace{
		TraceID: "trace-20260208-120000-aabbccdd",
		Prompt:  "test prompt",
		SelectionCall: LLMCallTrace{
			Messages: []MessageTrace{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "usr"},
			},
			Temperature: 0.1,
			RawResponse: "response content",
			Model:       "test-model",
			DurationMs:  42,
		},
		TotalDurationMs: 100,
	}

	err := WritePlanTrace(trace, path)
	require.NoError(t, err)

	// Read back and verify JSON structure.
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var decoded PlanTrace
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "trace-20260208-120000-aabbccdd", decoded.TraceID)
	assert.Equal(t, "test prompt", decoded.Prompt)
	assert.Equal(t, "test-model", decoded.SelectionCall.Model)
	assert.Equal(t, int64(42), decoded.SelectionCall.DurationMs)
	assert.Len(t, decoded.SelectionCall.Messages, 2)
	assert.Equal(t, "system", decoded.SelectionCall.Messages[0].Role)
	assert.Equal(t, int64(100), decoded.TotalDurationMs)
}

func TestWritePlanTrace_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "trace-123", "plan-trace.json")

	trace := &PlanTrace{
		TraceID: "trace-test",
		Prompt:  "deep path test",
	}

	err := WritePlanTrace(trace, path)
	require.NoError(t, err)

	_, err = os.Stat(path)
	assert.NoError(t, err, "file should exist at deep path")
}

func TestGenerateTraceID_Format(t *testing.T) {
	id := generateTraceID()

	// Should match: trace-YYYYMMDD-HHMMSS-XXXXXXXX
	pattern := `^trace-\d{8}-\d{6}-[0-9a-f]{8}$`
	matched, err := regexp.MatchString(pattern, id)
	require.NoError(t, err)
	assert.True(t, matched, "trace ID %q should match pattern %s", id, pattern)
}

func TestGenerateTraceID_Unique(t *testing.T) {
	ids := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id := generateTraceID()
		assert.False(t, ids[id], "duplicate trace ID: %s", id)
		ids[id] = true
	}
}
