package engine

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/plan"
	"github.com/gburgyan/aat/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToArchive_BasicConversion(t *testing.T) {
	start := time.Date(2026, 2, 7, 14, 30, 0, 0, time.UTC)
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{
				Node:      "searchAir",
				Inputs:    map[string]any{"origin": "DEN"},
				StartTime: start,
				Duration:  150 * time.Millisecond,
				Request: &adapter.Request{
					Method:  "POST",
					Path:    "/v2/search",
					Headers: map[string]string{"Content-Type": "application/json"},
					Body:    []byte(`{"origin":"DEN"}`),
				},
				Response: &adapter.Response{
					StatusCode: 200,
					Headers:    http.Header{"Content-Type": {"application/json"}},
					Body:       []byte(`{"offers":[]}`),
				},
				Outputs: map[string]any{"offerId": "offer-1"},
			},
		},
	}

	meta := archive.ArchiveMetadata{
		Version:     "1",
		RunID:       "run-20260207-143000-abcd",
		Timestamp:   start,
		Plan:        &plan.Plan{Graph: "booking.yaml"},
		Environment: "test",
	}

	a := ToArchive(result, meta, "https://api.example.com")

	assert.Equal(t, "passed", a.Result.Outcome)
	assert.Empty(t, a.Result.Error)
	require.Len(t, a.Steps, 1)

	step := a.Steps[0]
	assert.Equal(t, "searchAir", step.Node)
	assert.Equal(t, int64(150), step.DurationMs)
	assert.Equal(t, start, step.StartTime)

	require.NotNil(t, step.Request)
	assert.Equal(t, "POST", step.Request.Method)
	assert.Equal(t, "https://api.example.com/v2/search", step.Request.URL)
	assert.JSONEq(t, `{"origin":"DEN"}`, string(step.Request.Body))

	require.NotNil(t, step.Response)
	assert.Equal(t, 200, step.Response.Status)
	assert.JSONEq(t, `{"offers":[]}`, string(step.Response.Body))

	assert.Equal(t, "offer-1", step.Outputs["offerId"])
}

func TestToArchive_NilRequestResponse(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomeError,
		Steps: []StepResult{
			{
				Node:     "failStep",
				Duration: 10 * time.Millisecond,
				Error:    errors.New("resolving inputs: missing required input"),
			},
		},
		Error: errors.New("step failed"),
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-nil-test"}
	a := ToArchive(result, meta, "https://api.example.com")

	assert.Equal(t, "error", a.Result.Outcome)
	assert.Equal(t, "step failed", a.Result.Error)

	require.Len(t, a.Steps, 1)
	assert.Nil(t, a.Steps[0].Request)
	assert.Nil(t, a.Steps[0].Response)
	assert.Equal(t, "resolving inputs: missing required input", a.Steps[0].Error)
}

func TestToArchive_HeaderRedaction(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{
				Node: "step1",
				Request: &adapter.Request{
					Method: "GET",
					Path:   "/test",
					Headers: map[string]string{
						"Authorization": "Bearer secret-token",
						"Content-Type":  "application/json",
					},
				},
				Response: &adapter.Response{
					StatusCode: 200,
					Headers: http.Header{
						"Set-Cookie":   {"session=abc"},
						"Content-Type": {"application/json"},
					},
				},
			},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-redact"}
	a := ToArchive(result, meta, "https://api.example.com")

	assert.Equal(t, "[REDACTED]", a.Steps[0].Request.Headers["Authorization"])
	assert.Equal(t, "application/json", a.Steps[0].Request.Headers["Content-Type"])
	assert.Equal(t, "[REDACTED]", a.Steps[0].Response.Headers["Set-Cookie"])
	assert.Equal(t, "application/json", a.Steps[0].Response.Headers["Content-Type"])
}

func TestToArchive_DurationConversion(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		wantMs   int64
	}{
		{"zero", 0, 0},
		{"milliseconds", 250 * time.Millisecond, 250},
		{"seconds", 2 * time.Second, 2000},
		{"sub_millisecond", 500 * time.Microsecond, 0},
		{"mixed", 1*time.Second + 500*time.Millisecond, 1500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &RunResult{
				Outcome: OutcomePassed,
				Steps:   []StepResult{{Node: "s", Duration: tt.duration}},
			}
			meta := archive.ArchiveMetadata{Version: "1", RunID: "run-dur"}
			a := ToArchive(result, meta, "")
			assert.Equal(t, tt.wantMs, a.Steps[0].DurationMs)
		})
	}
}

func TestToArchive_BodyHandling(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		wantNil  bool
		wantJSON string
	}{
		{"nil body", nil, true, ""},
		{"empty body", []byte{}, true, ""},
		{"valid json", []byte(`{"key":"val"}`), false, `{"key":"val"}`},
		{"non-json body", []byte("plain text"), false, `"plain text"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &RunResult{
				Outcome: OutcomePassed,
				Steps: []StepResult{{
					Node: "s",
					Request: &adapter.Request{
						Method: "POST",
						Path:   "/test",
						Body:   tt.body,
					},
					Response: &adapter.Response{
						StatusCode: 200,
						Body:       tt.body,
					},
				}},
			}
			meta := archive.ArchiveMetadata{Version: "1", RunID: "run-body"}
			a := ToArchive(result, meta, "")

			if tt.wantNil {
				assert.Nil(t, a.Steps[0].Request.Body)
				assert.Nil(t, a.Steps[0].Response.Body)
			} else {
				assert.JSONEq(t, tt.wantJSON, string(a.Steps[0].Request.Body))
				assert.JSONEq(t, tt.wantJSON, string(a.Steps[0].Response.Body))
			}
		})
	}
}

func TestToArchive_ValidationConversion(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomeFailed,
		Steps: []StepResult{
			{
				Node: "step1",
				Validation: &validate.MechanicalResult{
					Passed: false,
					Results: []validate.AssertionResult{
						{Type: validate.AssertStatus, Passed: true, Message: "status code is 200"},
						{Type: validate.AssertFieldExists, Passed: false, Message: `field "id" not found`, Path: "id"},
						{Type: validate.AssertPredicate, Passed: true, Message: `predicate "x > 0" is true`, Expr: "x > 0"},
					},
				},
			},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-val"}
	a := ToArchive(result, meta, "")

	require.NotNil(t, a.Steps[0].Validation)
	assert.False(t, a.Steps[0].Validation.Passed)
	require.Len(t, a.Steps[0].Validation.Results, 3)

	assert.Equal(t, "status", a.Steps[0].Validation.Results[0].Type)
	assert.True(t, a.Steps[0].Validation.Results[0].Passed)

	assert.Equal(t, "fieldExists", a.Steps[0].Validation.Results[1].Type)
	assert.False(t, a.Steps[0].Validation.Results[1].Passed)
	assert.Equal(t, "id", a.Steps[0].Validation.Results[1].Path)

	assert.Equal(t, "predicate", a.Steps[0].Validation.Results[2].Type)
	assert.Equal(t, "x > 0", a.Steps[0].Validation.Results[2].Expr)
}

func TestToArchive_SelectionConversion(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{
				Node: "step1",
				Selections: []SelectionDecision{
					{
						InputName:     "offerId",
						SourceNode:    "searchAir",
						SourceField:   "offers",
						SourceSize:    5,
						FilterExpr:    "price < 500",
						FilteredSize:  3,
						Strategy:      "first",
						SelectedIndex: 0,
					},
				},
			},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-sel"}
	a := ToArchive(result, meta, "")

	require.Len(t, a.Steps[0].Selections, 1)
	sel := a.Steps[0].Selections[0]
	assert.Equal(t, "offerId", sel.InputName)
	assert.Equal(t, "searchAir", sel.SourceNode)
	assert.Equal(t, 5, sel.SourceSize)
	assert.Equal(t, "price < 500", sel.FilterExpr)
	assert.Equal(t, 3, sel.FilteredSize)
	assert.Equal(t, "first", sel.Strategy)
}

func TestToArchive_ErrorClassConversion(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomeError,
		Steps: []StepResult{
			{
				Node:  "step1",
				Error: errors.New("connection refused"),
				ErrorClass: &ErrorClassification{
					Category:     CategoryTransient,
					Detail:       "connection refused",
					Action:       "retried",
					RetryAttempt: 2,
				},
				RetryCount: 3,
			},
		},
		Error: errors.New("step failed"),
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-err"}
	a := ToArchive(result, meta, "")

	require.NotNil(t, a.Steps[0].ErrorClass)
	assert.Equal(t, "transient", a.Steps[0].ErrorClass.Category)
	assert.Equal(t, "connection refused", a.Steps[0].ErrorClass.Detail)
	assert.Equal(t, "retried", a.Steps[0].ErrorClass.Action)
	assert.Equal(t, 2, a.Steps[0].ErrorClass.RetryAttempt)
	assert.Equal(t, 3, a.Steps[0].RetryCount)
}

func TestToArchive_CleanupSteps(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{Node: "createBooking"},
		},
		CleanupResults: []StepResult{
			{Node: "cancelBooking", Duration: 50 * time.Millisecond},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-cleanup"}
	a := ToArchive(result, meta, "")

	require.Len(t, a.Steps, 1)
	require.Len(t, a.Cleanup, 1)
	assert.Equal(t, "cancelBooking", a.Cleanup[0].Node)
	assert.Equal(t, int64(50), a.Cleanup[0].DurationMs)
}

func TestToArchive_EmptyResult(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomeError,
		Error:   errors.New("validation failed"),
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-empty"}
	a := ToArchive(result, meta, "")

	assert.Nil(t, a.Steps)
	assert.Nil(t, a.Cleanup)
	assert.Equal(t, "error", a.Result.Outcome)
}

func TestToArchive_FlattenHeaders(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{
				Node: "step1",
				Request: &adapter.Request{
					Method: "GET",
					Path:   "/test",
				},
				Response: &adapter.Response{
					StatusCode: 200,
					Headers: http.Header{
						"X-Custom": {"val1", "val2"},
					},
				},
			},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-flatten"}
	a := ToArchive(result, meta, "")

	assert.Equal(t, "val1, val2", a.Steps[0].Response.Headers["X-Custom"])
}

func TestToArchive_NilNilError(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{Node: "step1"},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-noerr"}
	a := ToArchive(result, meta, "")

	assert.Empty(t, a.Result.Error)
	assert.Empty(t, a.Steps[0].Error)
}

func TestToRawMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		wantNil  bool
		wantJSON string
	}{
		{"nil", nil, true, ""},
		{"empty", []byte{}, true, ""},
		{"valid json object", []byte(`{"k":"v"}`), false, `{"k":"v"}`},
		{"valid json array", []byte(`[1,2,3]`), false, `[1,2,3]`},
		{"plain text", []byte("hello world"), false, `"hello world"`},
		{"invalid json", []byte(`{broken`), false, `"{broken"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toRawMessage(tt.input)
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.JSONEq(t, tt.wantJSON, string(got))
			}
		})
	}
}

func TestFlattenHeaders(t *testing.T) {
	tests := []struct {
		name string
		h    map[string][]string
		want map[string]string
	}{
		{"nil", nil, nil},
		{"empty", map[string][]string{}, map[string]string{}},
		{
			"single values",
			map[string][]string{"Content-Type": {"application/json"}},
			map[string]string{"Content-Type": "application/json"},
		},
		{
			"multiple values",
			map[string][]string{"Accept": {"text/html", "application/json"}},
			map[string]string{"Accept": "text/html, application/json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := flattenHeaders(tt.h)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestToArchive_FullRoundTrip(t *testing.T) {
	// Verify the archive produced by ToArchive can be serialized and deserialized
	start := time.Date(2026, 2, 7, 14, 30, 0, 0, time.UTC)
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{
				Node:      "step1",
				Inputs:    map[string]any{"key": "val"},
				StartTime: start,
				Duration:  100 * time.Millisecond,
				Request: &adapter.Request{
					Method: "POST",
					Path:   "/api/test",
					Body:   []byte(`{"data":"test"}`),
				},
				Response: &adapter.Response{
					StatusCode: 200,
					Body:       []byte(`{"result":"ok"}`),
				},
				Outputs: map[string]any{"id": "123"},
			},
		},
	}

	meta := archive.ArchiveMetadata{
		Version:   "1",
		RunID:     "run-roundtrip",
		Timestamp: start,
	}

	a := ToArchive(result, meta, "https://api.example.com")

	data, err := json.MarshalIndent(a, "", "  ")
	require.NoError(t, err)

	var loaded archive.Archive
	err = json.Unmarshal(data, &loaded)
	require.NoError(t, err)

	assert.Equal(t, "passed", loaded.Result.Outcome)
	assert.Equal(t, "run-roundtrip", loaded.Metadata.RunID)
	require.Len(t, loaded.Steps, 1)
	assert.Equal(t, "https://api.example.com/api/test", loaded.Steps[0].Request.URL)
}
