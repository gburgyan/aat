package intent

import (
	"context"
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
		GoalCall: LLMCallTrace{
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
	assert.Equal(t, "test-model", decoded.GoalCall.Model)
	assert.Equal(t, int64(42), decoded.GoalCall.DurationMs)
	assert.Len(t, decoded.GoalCall.Messages, 2)
	assert.Equal(t, "system", decoded.GoalCall.Messages[0].Role)
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

func TestInterpret_WithTrace(t *testing.T) {
	g := loadTravelportGraph(t)
	goalJSON := loadFixture(t, "testdata/responses/goal_analysis.json")
	planYAML := loadFixture(t, "testdata/responses/booking_plan.yaml")

	client := &stubClient{
		responses: []string{goalJSON, planYAML},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:      "book a flight from DEN to SFO",
		Graph:       g,
		Client:      client,
		EnableTrace: true,
	})

	require.NoError(t, err)
	require.NotNil(t, result.Plan)
	require.NotNil(t, result.Trace)

	trace := result.Trace

	// Trace ID
	assert.NotEmpty(t, trace.TraceID)
	assert.Contains(t, trace.TraceID, "trace-")

	// Prompt
	assert.Equal(t, "book a flight from DEN to SFO", trace.Prompt)

	// Goal call
	assert.NotEmpty(t, trace.GoalCall.Messages)
	assert.Equal(t, "test-model", trace.GoalCall.Model)
	assert.Equal(t, 100, trace.GoalCall.InputTokens)
	assert.Equal(t, 50, trace.GoalCall.OutputTokens)
	assert.NotEmpty(t, trace.GoalCall.RawResponse)
	assert.Equal(t, 0.1, trace.GoalCall.Temperature)

	// Goal analysis
	assert.NotNil(t, trace.GoalAnalysis)
	assert.Equal(t, "commitBooking", trace.GoalAnalysis.Goal)
	assert.False(t, trace.GoalFallback)

	// Chain result
	require.NotNil(t, trace.ChainResult)
	assert.NotEmpty(t, trace.ChainResult.Nodes)
	assert.NotEmpty(t, trace.ChainResult.Edges)

	// Skeleton
	require.NotNil(t, trace.Skeleton)
	assert.NotNil(t, trace.Skeleton.Plan)
	assert.NotEmpty(t, trace.Skeleton.YAML)

	// Plan call
	assert.NotEmpty(t, trace.PlanCall.Messages)
	assert.Equal(t, "test-model", trace.PlanCall.Model)
	assert.Equal(t, 0.2, trace.PlanCall.Temperature)

	// LLM YAML
	assert.NotEmpty(t, trace.LLMPlanYAML)

	// Merged and final plans
	assert.NotNil(t, trace.MergedPlan)
	assert.NotNil(t, trace.FinalPlan)

	// Timing
	assert.True(t, trace.TotalDurationMs >= 0)

	// No error
	assert.Empty(t, trace.Error)
	assert.Empty(t, trace.ValidationErr)
}

func TestInterpret_WithoutTrace(t *testing.T) {
	g := loadTravelportGraph(t)
	goalJSON := loadFixture(t, "testdata/responses/goal_analysis.json")
	planYAML := loadFixture(t, "testdata/responses/booking_plan.yaml")

	client := &stubClient{
		responses: []string{goalJSON, planYAML},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt: "book a flight from DEN to SFO",
		Graph:  g,
		Client: client,
		// EnableTrace defaults to false
	})

	require.NoError(t, err)
	require.NotNil(t, result.Plan)
	assert.Nil(t, result.Trace, "Trace should be nil when EnableTrace is false")
}

func TestInterpret_TraceOnError(t *testing.T) {
	g := loadTravelportGraph(t)
	goalJSON := loadFixture(t, "testdata/responses/goal_analysis.json")
	malformed := loadFixture(t, "testdata/responses/malformed.txt")

	client := &stubClient{
		responses: []string{
			goalJSON,
			// Second call returns garbage — no YAML to extract
			malformed,
		},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:      "book a flight from DEN to SFO",
		Graph:       g,
		Client:      client,
		EnableTrace: true,
	})

	require.Error(t, err)
	require.NotNil(t, result, "result should be non-nil even on error when tracing")
	require.NotNil(t, result.Trace, "trace should be present on error")

	trace := result.Trace

	// Goal call was successful — should be captured.
	assert.NotEmpty(t, trace.GoalCall.Messages)
	assert.NotNil(t, trace.GoalAnalysis)
	assert.Equal(t, "commitBooking", trace.GoalAnalysis.Goal)

	// Chain was successful.
	assert.NotNil(t, trace.ChainResult)

	// Skeleton was built.
	assert.NotNil(t, trace.Skeleton)

	// Plan call completed (returned garbage but not an error).
	assert.NotEmpty(t, trace.PlanCall.RawResponse)

	// Error captured.
	assert.NotEmpty(t, trace.Error)
	assert.True(t, trace.TotalDurationMs >= 0)
}

func TestInterpret_TraceFallback(t *testing.T) {
	g := loadTravelportGraph(t)
	planYAML := loadFixture(t, "testdata/responses/booking_plan.yaml")

	client := &stubClient{
		responses: []string{
			// First call: unparseable JSON → heuristic fallback
			"this is not json at all",
			// Second call: valid plan
			planYAML,
		},
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:      "book a flight from DEN to SFO",
		Graph:       g,
		Client:      client,
		EnableTrace: true,
	})

	require.NoError(t, err)
	require.NotNil(t, result.Trace)

	assert.True(t, result.Trace.GoalFallback, "GoalFallback should be true when heuristic was used")
	assert.NotNil(t, result.Trace.GoalAnalysis)
	assert.Contains(t, result.Trace.GoalAnalysis.Description, "Heuristic")
}

func TestInterpret_TraceOnLLMError(t *testing.T) {
	g := loadTravelportGraph(t)

	client := &stubClient{
		err: assert.AnError,
	}

	result, err := Interpret(context.Background(), InterpretRequest{
		Prompt:      "book a flight",
		Graph:       g,
		Client:      client,
		EnableTrace: true,
	})

	require.Error(t, err)
	require.NotNil(t, result, "result should be non-nil even on error")
	require.NotNil(t, result.Trace)

	trace := result.Trace

	// Goal call should have captured the error.
	assert.NotEmpty(t, trace.GoalCall.Error)
	assert.NotEmpty(t, trace.GoalCall.Messages, "prompts should be captured even on error")

	// Pipeline error.
	assert.NotEmpty(t, trace.Error)
	assert.Contains(t, trace.Error, "goal analysis")
}
