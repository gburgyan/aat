package server

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/gburgyan/aat/intent"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test helpers ---

func makeTrace(traceID, prompt string) *intent.PlanTrace {
	return &intent.PlanTrace{
		TraceID:   traceID,
		Timestamp: time.Date(2026, 2, 15, 10, 30, 0, 0, time.UTC),
		Prompt:    prompt,
		SelectionCall: intent.LLMCallTrace{
			Messages: []intent.MessageTrace{
				{Role: "system", Content: "You are a workflow selector"},
				{Role: "user", Content: prompt},
			},
			RawResponse:  `{"workflow":"roundtrip-booking"}`,
			Model:        "gpt-4",
			InputTokens:  100,
			OutputTokens: 20,
			DurationMs:   500,
			FinishReason: "stop",
		},
		PlanCall: intent.LLMCallTrace{
			Messages: []intent.MessageTrace{
				{Role: "system", Content: "Fill in values"},
				{Role: "user", Content: "plan skeleton"},
			},
			RawResponse:  `{"values":{"origin":"JFK"}}`,
			Model:        "gpt-4",
			InputTokens:  200,
			OutputTokens: 50,
			DurationMs:   800,
			FinishReason: "stop",
		},
		TotalDurationMs: 1500,
	}
}

func writeTrace(t *testing.T, dir string, trace *intent.PlanTrace) {
	t.Helper()
	path := filepath.Join(dir, trace.TraceID, "plan-trace.json")
	err := intent.WritePlanTrace(trace, path)
	require.NoError(t, err)
}

// --- ListTraces ---

func TestListTraces_SortedNewestFirst(t *testing.T) {
	dir := t.TempDir()

	t1 := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	t2 := makeTrace("trace-20260102-100000-aaaa0002", "search hotels")
	t3 := makeTrace("trace-20260103-100000-aaaa0003", "cancel booking")

	writeTrace(t, dir, t1)
	writeTrace(t, dir, t2)
	writeTrace(t, dir, t3)

	svc := NewTraceService(dir)
	traces, err := svc.ListTraces(0)
	require.NoError(t, err)
	require.Len(t, traces, 3)

	assert.Equal(t, "trace-20260103-100000-aaaa0003", traces[0].TraceID)
	assert.Equal(t, "trace-20260102-100000-aaaa0002", traces[1].TraceID)
	assert.Equal(t, "trace-20260101-100000-aaaa0001", traces[2].TraceID)

	assert.Equal(t, "cancel booking", traces[0].Prompt)
}

func TestListTraces_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	svc := NewTraceService(dir)
	traces, err := svc.ListTraces(0)
	assert.NoError(t, err)
	assert.Nil(t, traces)
}

func TestListTraces_NotConfigured(t *testing.T) {
	svc := NewTraceService("")
	traces, err := svc.ListTraces(0)
	assert.NoError(t, err)
	assert.Nil(t, traces)
}

func TestListTraces_NonexistentDirectory(t *testing.T) {
	svc := NewTraceService("/tmp/nonexistent-aat-test-traces-12345")
	traces, err := svc.ListTraces(0)
	assert.NoError(t, err)
	assert.Nil(t, traces)
}

func TestListTraces_Limit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		tr := makeTrace(
			"trace-2026010"+string(rune('1'+i))+"-100000-aaaa000"+string(rune('1'+i)),
			"prompt",
		)
		writeTrace(t, dir, tr)
	}

	svc := NewTraceService(dir)
	traces, err := svc.ListTraces(2)
	require.NoError(t, err)
	assert.Len(t, traces, 2)
}

func TestListTraces_SkipsNonTraceDirs(t *testing.T) {
	dir := t.TempDir()

	tr := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	writeTrace(t, dir, tr)

	// Create non-trace directories
	require.NoError(t, createDir(filepath.Join(dir, "other-dir")))
	require.NoError(t, createDir(filepath.Join(dir, "run-20260101-100000-aaaa0001")))

	svc := NewTraceService(dir)
	traces, err := svc.ListTraces(0)
	require.NoError(t, err)
	assert.Len(t, traces, 1)
	assert.Equal(t, "trace-20260101-100000-aaaa0001", traces[0].TraceID)
}

func TestListTraces_SkipsCorruptTrace(t *testing.T) {
	dir := t.TempDir()

	good := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	writeTrace(t, dir, good)

	// Write corrupt trace
	corruptDir := filepath.Join(dir, "trace-20260102-100000-aaaa0002")
	require.NoError(t, createDir(corruptDir))
	require.NoError(t, writeFile(filepath.Join(corruptDir, "plan-trace.json"), []byte("not json")))

	svc := NewTraceService(dir)
	traces, err := svc.ListTraces(0)
	require.NoError(t, err)
	assert.Len(t, traces, 1)
	assert.Equal(t, "trace-20260101-100000-aaaa0001", traces[0].TraceID)
}

// --- GetTrace ---

func TestGetTrace_Basic(t *testing.T) {
	dir := t.TempDir()

	tr := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	tr.WorkflowName = "roundtrip-booking"
	writeTrace(t, dir, tr)

	svc := NewTraceService(dir)
	detail, err := svc.GetTrace("trace-20260101-100000-aaaa0001")
	require.NoError(t, err)

	assert.Equal(t, "trace-20260101-100000-aaaa0001", detail.TraceID)
	assert.Equal(t, "book a flight", detail.Prompt)
	assert.Equal(t, "roundtrip-booking", detail.WorkflowName)
	assert.Equal(t, int64(1500), detail.TotalDurationMs)
	assert.Empty(t, detail.Error)
}

func TestGetTrace_LLMCallConversion(t *testing.T) {
	dir := t.TempDir()

	tr := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	writeTrace(t, dir, tr)

	svc := NewTraceService(dir)
	detail, err := svc.GetTrace("trace-20260101-100000-aaaa0001")
	require.NoError(t, err)

	// SelectionCall converted to LLMCallDetail
	require.NotNil(t, detail.SelectionCall)
	assert.Equal(t, "gpt-4", detail.SelectionCall.Model)
	assert.Equal(t, `{"workflow":"roundtrip-booking"}`, detail.SelectionCall.Response)
	assert.Equal(t, 100, detail.SelectionCall.InputTokens)
	assert.Equal(t, 20, detail.SelectionCall.OutputTokens)
	assert.Equal(t, int64(500), detail.SelectionCall.DurationMs)
	assert.Equal(t, "stop", detail.SelectionCall.FinishReason)
	require.Len(t, detail.SelectionCall.Messages, 2)
	assert.Equal(t, "system", detail.SelectionCall.Messages[0].Role)
	assert.Equal(t, "user", detail.SelectionCall.Messages[1].Role)

	// PlanCall converted
	require.NotNil(t, detail.PlanCall)
	assert.Equal(t, "gpt-4", detail.PlanCall.Model)
	assert.Equal(t, int64(800), detail.PlanCall.DurationMs)
}

func TestGetTrace_NotFound(t *testing.T) {
	dir := t.TempDir()
	svc := NewTraceService(dir)

	_, err := svc.GetTrace("trace-nonexistent")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTraceNotFound))
}

func TestGetTrace_PlanYAML(t *testing.T) {
	dir := t.TempDir()

	tr := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	tr.MergedPlan = &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "SearchOffers", Description: "Search"},
			},
		},
	}
	tr.FinalPlan = &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "SearchOffers", Description: "Search"},
				{Node: "BookOffer", Description: "Book"},
			},
		},
	}
	writeTrace(t, dir, tr)

	svc := NewTraceService(dir)
	detail, err := svc.GetTrace("trace-20260101-100000-aaaa0001")
	require.NoError(t, err)

	assert.Contains(t, detail.MergedPlanYAML, "SearchOffers")
	assert.Contains(t, detail.FinalPlanYAML, "BookOffer")
}

func TestGetTrace_Skeleton(t *testing.T) {
	dir := t.TempDir()

	tr := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	tr.Skeleton = &intent.SkeletonTrace{
		YAML:        "steps:\n  - node: SearchOffers\n",
		UnfedInputs: []string{"origin", "destination"},
		DurationMs:  50,
	}
	writeTrace(t, dir, tr)

	svc := NewTraceService(dir)
	detail, err := svc.GetTrace("trace-20260101-100000-aaaa0001")
	require.NoError(t, err)

	require.NotNil(t, detail.Skeleton)
	assert.Contains(t, detail.Skeleton.PlanYAML, "SearchOffers")
	assert.Equal(t, []string{"origin", "destination"}, detail.Skeleton.UnfedInputs)
	assert.Equal(t, int64(50), detail.Skeleton.DurationMs)
}

func TestGetTrace_WithError(t *testing.T) {
	dir := t.TempDir()

	tr := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	tr.Error = "validation failed"
	writeTrace(t, dir, tr)

	svc := NewTraceService(dir)
	detail, err := svc.GetTrace("trace-20260101-100000-aaaa0001")
	require.NoError(t, err)

	assert.Equal(t, "validation failed", detail.Error)
}

func TestGetTrace_WorkflowSelection(t *testing.T) {
	dir := t.TempDir()

	tr := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	tr.WorkflowSelection = &intent.WorkflowSelection{
		Workflow:    "roundtrip-booking",
		Description: "Book a roundtrip flight",
	}
	writeTrace(t, dir, tr)

	svc := NewTraceService(dir)
	detail, err := svc.GetTrace("trace-20260101-100000-aaaa0001")
	require.NoError(t, err)

	require.NotNil(t, detail.WorkflowSelection)
	// It's passed through as any, so check via JSON round-trip
	data, err := json.Marshal(detail.WorkflowSelection)
	require.NoError(t, err)
	assert.Contains(t, string(data), "roundtrip-booking")
}

func TestGetTrace_WrongPlanFields(t *testing.T) {
	dir := t.TempDir()

	tr := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	tr.WrongPlanSignal = &intent.WrongPlanSignal{
		Reason:    "workflow doesn't support international",
		Suggested: "international-booking",
	}
	tr.WrongPlanCall = &intent.LLMCallTrace{
		Messages:    []intent.MessageTrace{{Role: "system", Content: "detect"}},
		RawResponse: "wrong plan detected",
		Model:       "gpt-4",
		DurationMs:  200,
	}
	tr.ReselectionCall = &intent.LLMCallTrace{
		Messages:    []intent.MessageTrace{{Role: "system", Content: "reselect"}},
		RawResponse: `{"workflow":"international-booking"}`,
		Model:       "gpt-4",
		DurationMs:  300,
	}
	writeTrace(t, dir, tr)

	svc := NewTraceService(dir)
	detail, err := svc.GetTrace("trace-20260101-100000-aaaa0001")
	require.NoError(t, err)

	require.NotNil(t, detail.WrongPlanSignal)
	require.NotNil(t, detail.WrongPlanCall)
	assert.Equal(t, "gpt-4", detail.WrongPlanCall.Model)
	require.NotNil(t, detail.ReselectionCall)
	assert.Equal(t, "gpt-4", detail.ReselectionCall.Model)
}

func TestGetTrace_TemplateFields(t *testing.T) {
	dir := t.TempDir()

	tr := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	tr.WorkflowName = "roundtrip-booking"
	tr.TemplatePath = "workflows/roundtrip-booking.yaml"
	tr.Repetitions = map[string]int{"leg": 2}
	tr.TemplateExpanded = &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "SearchOffers", Description: "Leg 1"},
				{Node: "SearchOffers", Description: "Leg 2"},
			},
		},
	}
	writeTrace(t, dir, tr)

	svc := NewTraceService(dir)
	detail, err := svc.GetTrace("trace-20260101-100000-aaaa0001")
	require.NoError(t, err)

	assert.Equal(t, "roundtrip-booking", detail.WorkflowName)
	assert.Equal(t, "workflows/roundtrip-booking.yaml", detail.TemplatePath)
	require.NotNil(t, detail.Repetitions)
	assert.Contains(t, detail.TemplateExpandedYAML, "Leg 1")
}

// --- countLLMCalls ---

func TestCountLLMCalls(t *testing.T) {
	tests := []struct {
		name     string
		trace    *intent.PlanTrace
		expected int
	}{
		{
			name:     "basic two calls",
			trace:    makeTrace("trace-1", "prompt"),
			expected: 2, // SelectionCall + PlanCall
		},
		{
			name: "with retry and reselection",
			trace: func() *intent.PlanTrace {
				tr := makeTrace("trace-1", "prompt")
				tr.RetryCall = &intent.LLMCallTrace{
					Messages: []intent.MessageTrace{{Role: "system", Content: "retry"}},
				}
				tr.WrongPlanCall = &intent.LLMCallTrace{
					Messages: []intent.MessageTrace{{Role: "system", Content: "wrong plan"}},
				}
				tr.ReselectionCall = &intent.LLMCallTrace{
					Messages: []intent.MessageTrace{{Role: "system", Content: "reselect"}},
				}
				return tr
			}(),
			expected: 5,
		},
		{
			name: "empty messages not counted",
			trace: &intent.PlanTrace{
				SelectionCall: intent.LLMCallTrace{}, // no messages
				PlanCall:      intent.LLMCallTrace{},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, countLLMCalls(tt.trace))
		})
	}
}

// --- traceLLMCallToDetail ---

func TestTraceLLMCallToDetail_Nil(t *testing.T) {
	result := traceLLMCallPtrToDetail(nil)
	assert.Nil(t, result)
}

func TestTraceLLMCallToDetail_Full(t *testing.T) {
	call := &intent.LLMCallTrace{
		Messages: []intent.MessageTrace{
			{Role: "system", Content: "sys prompt"},
			{Role: "user", Content: "user prompt"},
		},
		RawResponse:  "response text",
		Model:        "gpt-4",
		InputTokens:  100,
		OutputTokens: 25,
		DurationMs:   500,
		FinishReason: "stop",
	}

	detail := traceLLMCallToDetail(call)
	require.NotNil(t, detail)
	assert.Equal(t, "gpt-4", detail.Model)
	assert.Equal(t, "response text", detail.Response)
	assert.Equal(t, 100, detail.InputTokens)
	assert.Equal(t, 25, detail.OutputTokens)
	assert.Equal(t, int64(500), detail.DurationMs)
	assert.Equal(t, "stop", detail.FinishReason)
	require.Len(t, detail.Messages, 2)
	assert.Equal(t, "system", detail.Messages[0].Role)
	assert.Equal(t, "sys prompt", detail.Messages[0].Content)
}

// --- planToYAML ---

func TestPlanToYAML_Nil(t *testing.T) {
	assert.Equal(t, "", planToYAML(nil))
}

func TestPlanToYAML_NonNil(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "SearchOffers"},
			},
		},
	}
	result := planToYAML(p)
	assert.Contains(t, result, "SearchOffers")
}

// --- traceToListEntry ---

func TestTraceToListEntry(t *testing.T) {
	tr := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	tr.WorkflowName = "roundtrip-booking"

	entry := traceToListEntry(tr)

	assert.Equal(t, "trace-20260101-100000-aaaa0001", entry.TraceID)
	assert.Equal(t, "book a flight", entry.Prompt)
	assert.Equal(t, "roundtrip-booking", entry.WorkflowName)
	assert.Equal(t, int64(1500), entry.TotalDurationMs)
	assert.False(t, entry.HasError)
	assert.Equal(t, 2, entry.LLMCallCount)
}

func TestTraceToListEntry_WithError(t *testing.T) {
	tr := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	tr.Error = "something went wrong"

	entry := traceToListEntry(tr)
	assert.True(t, entry.HasError)
}

// --- ListTraces with HasError in list ---

func TestListTraces_HasErrorFlag(t *testing.T) {
	dir := t.TempDir()

	good := makeTrace("trace-20260101-100000-aaaa0001", "good trace")
	writeTrace(t, dir, good)

	bad := makeTrace("trace-20260102-100000-aaaa0002", "bad trace")
	bad.Error = "validation failed"
	writeTrace(t, dir, bad)

	svc := NewTraceService(dir)
	traces, err := svc.ListTraces(0)
	require.NoError(t, err)
	require.Len(t, traces, 2)

	// Newest first
	assert.True(t, traces[0].HasError)  // bad trace
	assert.False(t, traces[1].HasError) // good trace
}

func TestListTraces_LLMCallCount(t *testing.T) {
	dir := t.TempDir()

	tr := makeTrace("trace-20260101-100000-aaaa0001", "prompt")
	tr.RetryCall = &intent.LLMCallTrace{
		Messages: []intent.MessageTrace{{Role: "system", Content: "retry"}},
	}
	writeTrace(t, dir, tr)

	svc := NewTraceService(dir)
	traces, err := svc.ListTraces(0)
	require.NoError(t, err)
	require.Len(t, traces, 1)
	assert.Equal(t, 3, traces[0].LLMCallCount) // selection + plan + retry
}

func TestGetTrace_RetryCall(t *testing.T) {
	dir := t.TempDir()

	tr := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	tr.ValidationErr = "step X has unresolved input"
	tr.RetryCall = &intent.LLMCallTrace{
		Messages:    []intent.MessageTrace{{Role: "system", Content: "retry"}},
		RawResponse: `{"values":{"origin":"LAX"}}`,
		Model:       "gpt-4",
		DurationMs:  600,
	}
	tr.RetryValidationErr = "still broken"
	writeTrace(t, dir, tr)

	svc := NewTraceService(dir)
	detail, err := svc.GetTrace("trace-20260101-100000-aaaa0001")
	require.NoError(t, err)

	assert.Equal(t, "step X has unresolved input", detail.ValidationErr)
	require.NotNil(t, detail.RetryCall)
	assert.Equal(t, "gpt-4", detail.RetryCall.Model)
	assert.Equal(t, "still broken", detail.RetryValidationErr)
}

func TestGetTrace_SelectionRetryCall(t *testing.T) {
	dir := t.TempDir()

	tr := makeTrace("trace-20260101-100000-aaaa0001", "book a flight")
	tr.SelectionRetryCall = &intent.LLMCallTrace{
		Messages:    []intent.MessageTrace{{Role: "system", Content: "retry selection"}},
		RawResponse: `{"workflow":"oneway-booking"}`,
		Model:       "gpt-4",
		DurationMs:  400,
	}
	writeTrace(t, dir, tr)

	svc := NewTraceService(dir)
	detail, err := svc.GetTrace("trace-20260101-100000-aaaa0001")
	require.NoError(t, err)

	require.NotNil(t, detail.SelectionRetryCall)
	assert.Equal(t, "gpt-4", detail.SelectionRetryCall.Model)
}
