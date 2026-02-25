package engine

import (
	"encoding/json"
	"errors"
	"fmt"
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

	a := ToArchive(result, meta, "https://api.example.com", nil)

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
	a := ToArchive(result, meta, "https://api.example.com", nil)

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
	a := ToArchive(result, meta, "https://api.example.com", nil)

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
			a := ToArchive(result, meta, "", nil)
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
			a := ToArchive(result, meta, "", nil)

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
	a := ToArchive(result, meta, "", nil)

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

func TestToArchive_ValidationSkippedPassthrough(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{
				Node: "step1",
				Validation: &validate.MechanicalResult{
					Passed: true,
					Results: []validate.AssertionResult{
						{Type: validate.AssertStatus, Passed: true, Message: "status code is 200"},
						{Type: validate.AssertSchema, Passed: true, Skipped: true, Message: "schema validation not yet implemented"},
					},
				},
			},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-skipped"}
	a := ToArchive(result, meta, "", nil)

	require.NotNil(t, a.Steps[0].Validation)
	require.Len(t, a.Steps[0].Validation.Results, 2)

	// Status assertion: not skipped
	assert.False(t, a.Steps[0].Validation.Results[0].Skipped)

	// Schema assertion: skipped
	assert.True(t, a.Steps[0].Validation.Results[1].Skipped)
	assert.True(t, a.Steps[0].Validation.Results[1].Passed)

	// Verify omitempty: non-skipped assertion should not have "skipped" in JSON
	data, err := json.Marshal(a.Steps[0].Validation.Results[0])
	require.NoError(t, err)
	assert.NotContains(t, string(data), "skipped")

	// Skipped assertion should have "skipped":true
	data, err = json.Marshal(a.Steps[0].Validation.Results[1])
	require.NoError(t, err)
	assert.Contains(t, string(data), `"skipped":true`)
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
	a := ToArchive(result, meta, "", nil)

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
	a := ToArchive(result, meta, "", nil)

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
	a := ToArchive(result, meta, "", nil)

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
	a := ToArchive(result, meta, "", nil)

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
	a := ToArchive(result, meta, "", nil)

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
	a := ToArchive(result, meta, "", nil)

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

	a := ToArchive(result, meta, "https://api.example.com", nil)

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

func TestToArchive_ResolutionConversion(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{
				Node: "step1",
				Resolutions: []ValueResolution{
					{
						InputName:  "origin",
						Source:     "plan_default",
						RawValue:   "DEN",
						FinalValue: "DEN",
						PoolIndex:  -1,
					},
					{
						InputName:  "date",
						Source:     "expression",
						RawValue:   "{{today + 5 days}}",
						FinalValue: "2026-02-13",
						Expression: "{{today + 5 days}}",
						PoolIndex:  -1,
					},
					{
						InputName:  "sessionId",
						Source:     "edge",
						FinalValue: "sess-123",
						FromStep:   "login",
						FromOutput: "sessionId",
						PoolIndex:  -1,
					},
					{
						InputName:    "code",
						Source:       "fallback_pool",
						RawValue:     "GOOD",
						FinalValue:   "GOOD",
						Constraint:   "value != 'BAD'",
						ConstraintOK: true,
						PoolIndex:    1,
						PoolSize:     3,
						Tried:        []any{"BAD"},
					},
				},
			},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-res"}
	a := ToArchive(result, meta, "", nil)

	require.Len(t, a.Steps[0].Resolutions, 4)

	// Plan default
	r0 := a.Steps[0].Resolutions[0]
	assert.Equal(t, "origin", r0.InputName)
	assert.Equal(t, "plan_default", r0.Source)
	assert.Equal(t, "DEN", r0.FinalValue)
	assert.Nil(t, r0.ConstraintOK) // no constraint → nil pointer

	// Expression
	r1 := a.Steps[0].Resolutions[1]
	assert.Equal(t, "expression", r1.Source)
	assert.Equal(t, "{{today + 5 days}}", r1.Expression)
	assert.Equal(t, "2026-02-13", r1.FinalValue)

	// Edge
	r2 := a.Steps[0].Resolutions[2]
	assert.Equal(t, "edge", r2.Source)
	assert.Equal(t, "login", r2.FromStep)
	assert.Equal(t, "sessionId", r2.FromOutput)

	// Fallback pool
	r3 := a.Steps[0].Resolutions[3]
	assert.Equal(t, "fallback_pool", r3.Source)
	assert.Equal(t, 1, r3.PoolIndex)
	assert.Equal(t, 3, r3.PoolSize)
	require.NotNil(t, r3.ConstraintOK)
	assert.True(t, *r3.ConstraintOK)
	require.Len(t, r3.Tried, 1)
}

func TestToArchive_SecretRedaction(t *testing.T) {
	secrets := map[string]bool{"my-secret-token": true}

	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{
				Node:   "step1",
				Inputs: map[string]any{"apiKey": "my-secret-token", "origin": "DEN"},
				Resolutions: []ValueResolution{
					{
						InputName:  "apiKey",
						Source:     "plan_default",
						RawValue:   "my-secret-token",
						FinalValue: "my-secret-token",
						PoolIndex:  -1,
					},
					{
						InputName:  "code",
						Source:     "fallback_pool",
						RawValue:   "GOOD",
						FinalValue: "GOOD",
						PoolIndex:  1,
						Tried:      []any{"my-secret-token", "other"},
					},
				},
			},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-secrets"}
	a := ToArchive(result, meta, "", secrets)

	// Inputs should be redacted
	assert.Equal(t, "[REDACTED]", a.Steps[0].Inputs["apiKey"])
	assert.Equal(t, "DEN", a.Steps[0].Inputs["origin"])

	// Resolutions should be redacted
	assert.Equal(t, "[REDACTED]", a.Steps[0].Resolutions[0].RawValue)
	assert.Equal(t, "[REDACTED]", a.Steps[0].Resolutions[0].FinalValue)

	// Tried values should be redacted
	assert.Equal(t, "[REDACTED]", a.Steps[0].Resolutions[1].Tried[0])
	assert.Equal(t, "other", a.Steps[0].Resolutions[1].Tried[1])
}

func TestToArchive_ResponseBodyErrorConversion(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomeFailed,
		Steps: []StepResult{
			{
				Node:       "step1",
				StatusCode: 200,
				ResponseBodyError: &ResponseBodyError{
					RulePath: "ErrorResponse.Result.Error",
					Rule:     "non-empty",
					Message:  "Invalid workbench ID",
					Code:     "INVALID_INPUT",
					Category: "validation",
				},
			},
		},
		Error: fmt.Errorf("step failed"),
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-body-err"}
	a := ToArchive(result, meta, "", nil)

	require.NotNil(t, a.Steps[0].ResponseBodyError)
	assert.Equal(t, "ErrorResponse.Result.Error", a.Steps[0].ResponseBodyError.RulePath)
	assert.Equal(t, "non-empty", a.Steps[0].ResponseBodyError.Rule)
	assert.Equal(t, "Invalid workbench ID", a.Steps[0].ResponseBodyError.Message)
	assert.Equal(t, "INVALID_INPUT", a.Steps[0].ResponseBodyError.Code)
	assert.Equal(t, "validation", a.Steps[0].ResponseBodyError.Category)

	// Verify round-trip through JSON
	data, err := json.MarshalIndent(a, "", "  ")
	require.NoError(t, err)

	var loaded archive.Archive
	err = json.Unmarshal(data, &loaded)
	require.NoError(t, err)

	require.NotNil(t, loaded.Steps[0].ResponseBodyError)
	assert.Equal(t, "INVALID_INPUT", loaded.Steps[0].ResponseBodyError.Code)
}

func TestToArchive_NoResponseBodyError(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps:   []StepResult{{Node: "step1"}},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-no-body-err"}
	a := ToArchive(result, meta, "", nil)

	assert.Nil(t, a.Steps[0].ResponseBodyError)

	// Verify omitempty
	data, err := json.Marshal(a.Steps[0])
	require.NoError(t, err)
	assert.NotContains(t, string(data), "responseBodyError")
}

func TestToArchive_DisplayOutputConversion(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{
				Node: "commit",
				DisplayOutputs: []DisplayOutput{
					{Label: "PNR", Name: "locator", Value: "ABCDEF"},
					{Label: "Reservation ID", Name: "reservationId", Value: "res-123"},
				},
			},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-display"}
	a := ToArchive(result, meta, "", nil)

	require.Len(t, a.Steps[0].DisplayOutputs, 2)
	assert.Equal(t, "PNR", a.Steps[0].DisplayOutputs[0].Label)
	assert.Equal(t, "locator", a.Steps[0].DisplayOutputs[0].Name)
	assert.Equal(t, "ABCDEF", a.Steps[0].DisplayOutputs[0].Value)
	assert.Equal(t, "Reservation ID", a.Steps[0].DisplayOutputs[1].Label)
	assert.Equal(t, "res-123", a.Steps[0].DisplayOutputs[1].Value)

	// Verify round-trip
	data, err := json.MarshalIndent(a, "", "  ")
	require.NoError(t, err)

	var loaded archive.Archive
	require.NoError(t, json.Unmarshal(data, &loaded))

	require.Len(t, loaded.Steps[0].DisplayOutputs, 2)
	assert.Equal(t, "PNR", loaded.Steps[0].DisplayOutputs[0].Label)
	assert.Equal(t, "ABCDEF", loaded.Steps[0].DisplayOutputs[0].Value)
}

func TestToArchive_NoDisplayOutputs(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps:   []StepResult{{Node: "step1"}},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-no-display"}
	a := ToArchive(result, meta, "", nil)

	assert.Nil(t, a.Steps[0].DisplayOutputs)

	// Verify omitempty
	data, err := json.Marshal(a.Steps[0])
	require.NoError(t, err)
	assert.NotContains(t, string(data), "displayOutputs")
}

func TestToArchive_NilSecretsNoRedaction(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{
				Node:   "step1",
				Inputs: map[string]any{"apiKey": "secret-value"},
			},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-nosecrets"}
	a := ToArchive(result, meta, "", nil)

	// Without secrets, values pass through unchanged
	assert.Equal(t, "secret-value", a.Steps[0].Inputs["apiKey"])
}
