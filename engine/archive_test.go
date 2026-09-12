package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/plan"
	"github.com/gburgyan/aat/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustToArchive calls ToArchive and fails the test on an error.
func mustToArchive(t *testing.T, result *RunResult, meta archive.ArchiveMetadata, baseURL string, secrets map[string]bool) *archive.Archive {
	t.Helper()
	a, err := ToArchive(result, meta, baseURL, secrets)
	require.NoError(t, err)
	return a
}

// TestToArchive_UnredactableIsAnError checks that an archive redaction cannot
// process is withheld rather than returned with its secrets in place.
func TestToArchive_UnredactableIsAnError(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{{
			StepID:  "getQuote",
			Node:    "getQuote",
			Inputs:  map[string]any{"apiKey": "sk-test-0123456789"},
			Outputs: map[string]any{"rate": math.NaN()},
		}},
	}
	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-nan"}
	a, err := ToArchive(result, meta, "", map[string]bool{"sk-test-0123456789": true})
	require.Error(t, err)
	assert.Nil(t, a)
}

func TestToArchive_CleanupFor(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		CleanupResults: []StepResult{
			{StepID: "requestRefund", Node: "requestRefund", CleanupFor: "createOrder"},
			{StepID: "confirmRefund", Node: "confirmRefund", CleanupFor: "requestRefund"},
		},
	}

	a := mustToArchive(t, result, archive.ArchiveMetadata{}, "", nil)

	require.Len(t, a.Cleanup, 2)
	assert.Equal(t, "createOrder", a.Cleanup[0].CleanupFor)
	assert.Equal(t, "requestRefund", a.Cleanup[1].CleanupFor)
}

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

	a := mustToArchive(t, result, meta, "https://api.example.com", nil)

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
	a := mustToArchive(t, result, meta, "https://api.example.com", nil)

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
	a := mustToArchive(t, result, meta, "https://api.example.com", nil)

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
			a := mustToArchive(t, result, meta, "", nil)
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
			a := mustToArchive(t, result, meta, "", nil)

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
	a := mustToArchive(t, result, meta, "", nil)

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
	a := mustToArchive(t, result, meta, "", nil)

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
	a := mustToArchive(t, result, meta, "", nil)

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
	a := mustToArchive(t, result, meta, "", nil)

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
	a := mustToArchive(t, result, meta, "", nil)

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
	a := mustToArchive(t, result, meta, "", nil)

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
	a := mustToArchive(t, result, meta, "", nil)

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
	a := mustToArchive(t, result, meta, "", nil)

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

	a := mustToArchive(t, result, meta, "https://api.example.com", nil)

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
	a := mustToArchive(t, result, meta, "", nil)

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
	a := mustToArchive(t, result, meta, "", secrets)

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
					Message:  "Invalid itinerary ID",
					Code:     "INVALID_INPUT",
					Category: "validation",
				},
			},
		},
		Error: fmt.Errorf("step failed"),
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-body-err"}
	a := mustToArchive(t, result, meta, "", nil)

	require.NotNil(t, a.Steps[0].ResponseBodyError)
	assert.Equal(t, "ErrorResponse.Result.Error", a.Steps[0].ResponseBodyError.RulePath)
	assert.Equal(t, "non-empty", a.Steps[0].ResponseBodyError.Rule)
	assert.Equal(t, "Invalid itinerary ID", a.Steps[0].ResponseBodyError.Message)
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
	a := mustToArchive(t, result, meta, "", nil)

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
	a := mustToArchive(t, result, meta, "", nil)

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
	a := mustToArchive(t, result, meta, "", nil)

	assert.Nil(t, a.Steps[0].DisplayOutputs)

	// Verify omitempty
	data, err := json.Marshal(a.Steps[0])
	require.NoError(t, err)
	assert.NotContains(t, string(data), "displayOutputs")
}

func TestToArchive_OverrideURLTracking(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{
				Node: "searchAir",
				Request: &adapter.Request{
					Method: "POST",
					Path:   "/v2/search",
				},
				Response: &adapter.Response{
					StatusCode: 200,
				},
				ActualBaseURL: "https://override.example.com",
			},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-override"}
	a := mustToArchive(t, result, meta, "https://api.example.com", nil)

	require.NotNil(t, a.Steps[0].Request)
	assert.Equal(t, "https://override.example.com/v2/search", a.Steps[0].Request.URL)
	assert.Equal(t, "https://api.example.com/v2/search", a.Steps[0].Request.OriginalURL)
}

func TestToArchive_NoOverride(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{
				Node: "searchAir",
				Request: &adapter.Request{
					Method: "POST",
					Path:   "/v2/search",
				},
				Response: &adapter.Response{
					StatusCode: 200,
				},
				ActualBaseURL: "https://api.example.com",
			},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-no-override"}
	a := mustToArchive(t, result, meta, "https://api.example.com", nil)

	require.NotNil(t, a.Steps[0].Request)
	assert.Equal(t, "https://api.example.com/v2/search", a.Steps[0].Request.URL)
	assert.Empty(t, a.Steps[0].Request.OriginalURL)

	// Verify omitempty
	data, err := json.Marshal(a.Steps[0].Request)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "originalUrl")
}

func TestToArchive_PathRewriteOnly(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{
				Node: "searchAir",
				Request: &adapter.Request{
					Method: "POST",
					Path:   "/v3/search", // rewritten path
				},
				Response: &adapter.Response{
					StatusCode: 200,
				},
				ActualBaseURL: "https://api.example.com",
				OriginalPath:  "/v2/search",
			},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-rewrite"}
	a := mustToArchive(t, result, meta, "https://api.example.com", nil)

	require.NotNil(t, a.Steps[0].Request)
	assert.Equal(t, "https://api.example.com/v3/search", a.Steps[0].Request.URL)
	assert.Equal(t, "https://api.example.com/v2/search", a.Steps[0].Request.OriginalURL)
}

func TestToArchive_OverridePlusPathRewrite(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{
				Node: "searchAir",
				Request: &adapter.Request{
					Method: "POST",
					Path:   "/v3/search", // rewritten path
				},
				Response: &adapter.Response{
					StatusCode: 200,
				},
				ActualBaseURL: "https://override.example.com",
				OriginalPath:  "/v2/search",
			},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-both"}
	a := mustToArchive(t, result, meta, "https://api.example.com", nil)

	require.NotNil(t, a.Steps[0].Request)
	// URL shows what was actually hit
	assert.Equal(t, "https://override.example.com/v3/search", a.Steps[0].Request.URL)
	// OriginalURL shows what would have been hit without override/rewrite
	assert.Equal(t, "https://api.example.com/v2/search", a.Steps[0].Request.OriginalURL)
}

func TestToArchive_EmptyActualBaseURL(t *testing.T) {
	// Backwards compatibility: old code paths that don't set ActualBaseURL
	result := &RunResult{
		Outcome: OutcomePassed,
		Steps: []StepResult{
			{
				Node: "searchAir",
				Request: &adapter.Request{
					Method: "POST",
					Path:   "/v2/search",
				},
				Response: &adapter.Response{
					StatusCode: 200,
				},
				// ActualBaseURL intentionally empty
			},
		},
	}

	meta := archive.ArchiveMetadata{Version: "1", RunID: "run-compat"}
	a := mustToArchive(t, result, meta, "https://api.example.com", nil)

	require.NotNil(t, a.Steps[0].Request)
	// Falls back to defaultBaseURL
	assert.Equal(t, "https://api.example.com/v2/search", a.Steps[0].Request.URL)
	assert.Empty(t, a.Steps[0].Request.OriginalURL)
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
	a := mustToArchive(t, result, meta, "", nil)

	// Without secrets, values pass through unchanged
	assert.Equal(t, "secret-value", a.Steps[0].Inputs["apiKey"])
}

// TestToArchive_RedactsCredentialsEverywhere covers the places a credential
// used to survive: an API key under a custom header name, and literal
// credentials and auth headers in the archived plans.
func TestToArchive_RedactsCredentialsEverywhere(t *testing.T) {
	secrets := map[string]bool{"shop-key-123": true, "plan-token-456": true}
	p := &plan.Plan{
		Auth: &config.AuthConfig{Type: "bearer", Credentials: map[string]config.SecretRef{
			"token": {Source: "literal", Value: "plan-token-456"},
			"other": {Source: "env", Var: "OTHER_TOKEN"},
		}},
		Headers:   map[string]string{"Authorization": "Bearer plan-token-456", "X-Client": "aat"},
		Execution: plan.Execution{Steps: []plan.Step{{Node: "step1"}}},
	}
	result := &RunResult{
		Outcome:          OutcomePassed,
		InstantiatedPlan: p,
		Steps: []StepResult{{
			Node: "step1",
			Request: &adapter.Request{Method: "GET", Path: "/items", Headers: map[string]string{
				"X-Shop-Token": "shop-key-123",
				"Accept":       "application/json",
			}},
			Response: &adapter.Response{StatusCode: 200, Headers: http.Header{"X-Echo-Key": {"shop-key-123"}}},
		}},
	}

	a := mustToArchive(t, result, archive.ArchiveMetadata{Version: "1", RunID: "run-creds", Plan: p}, "https://api.example.com", secrets)

	assert.Equal(t, "[REDACTED]", a.Steps[0].Request.Headers["X-Shop-Token"])
	assert.Equal(t, "application/json", a.Steps[0].Request.Headers["Accept"])
	assert.Equal(t, "[REDACTED]", a.Steps[0].Response.Headers["X-Echo-Key"])

	for name, archived := range map[string]*plan.Plan{"plan": a.Metadata.Plan, "instantiated plan": a.Metadata.InstantiatedPlan} {
		assert.Equal(t, "[REDACTED]", archived.Auth.Credentials["token"].Value, name)
		assert.Equal(t, "OTHER_TOKEN", archived.Auth.Credentials["other"].Var, name)
		assert.Equal(t, "[REDACTED]", archived.Headers["Authorization"], name)
		assert.Equal(t, "aat", archived.Headers["X-Client"], name)
	}
	body, err := json.Marshal(a)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "shop-key-123")
	assert.NotContains(t, string(body), "plan-token-456")
	assert.Equal(t, "plan-token-456", p.Auth.Credentials["token"].Value, "the run's plan is not modified")
}

// TestToArchive_RedactsSecretsFromRequestData checks that a known secret is
// redacted wherever run data carries it — the URL, request and response
// bodies, outputs, errors, assertion and OpenAPI messages, and the plan — while
// a short secret is redacted only as a whole value, and the run result itself
// is not modified.
func TestToArchive_RedactsSecretsFromRequestData(t *testing.T) {
	const key = "shop-key-123456"
	secrets := map[string]bool{key: true, "demo": true}
	p := &plan.Plan{Execution: plan.Execution{Steps: []plan.Step{{
		Node:   "checkout",
		Values: map[string]plan.StepValue{"note": {Default: "sent with " + key}},
	}}}}
	outputs := map[string]any{"receipt": "RCPT for " + key, "total": 10340, "customer": "demo@example.com"}
	result := &RunResult{
		Outcome:          OutcomeFailed,
		Error:            fmt.Errorf("step %q returned status 402 for key %s", "checkout", key),
		InstantiatedPlan: p,
		Steps: []StepResult{{
			StepID: "checkout",
			Node:   "checkout",
			Inputs: map[string]any{"password": "demo", "email": "demo@example.com"},
			Request: &adapter.Request{
				Method: "POST",
				Path:   "/carts/" + key + "/checkout?apiKey=" + key,
				Body:   []byte(`{"apiKey":"` + key + `","quantity":2,"email":"demo@example.com"}`),
			},
			Response: &adapter.Response{StatusCode: 402, Body: []byte("payment refused for " + key)},
			Outputs:  outputs,
			DisplayOutputs: []DisplayOutput{
				{Label: "Receipt", Name: "receipt", Value: "RCPT for " + key},
			},
			Error: errors.New("request with " + key + " failed"),
			Validation: &validate.MechanicalResult{Results: []validate.AssertionResult{
				{Type: validate.AssertFieldEquals, Message: "field apiKey: expected x, got " + key},
			}},
		}},
	}

	a := mustToArchive(t, result, archive.ArchiveMetadata{Version: "1", RunID: "run-data", Plan: p}, "https://api.example.com", secrets)

	data, err := json.Marshal(a)
	require.NoError(t, err)
	assert.NotContains(t, string(data), key)
	step := a.Steps[0]
	assert.Equal(t, "https://api.example.com/carts/[REDACTED]/checkout?apiKey=[REDACTED]", step.Request.URL)
	assert.JSONEq(t, `{"apiKey":"[REDACTED]","quantity":2,"email":"demo@example.com"}`, string(step.Request.Body))
	assert.Equal(t, `"payment refused for [REDACTED]"`, string(step.Response.Body))
	assert.Equal(t, "[REDACTED]", step.Inputs["password"], "a short secret is redacted as a whole value")
	assert.Equal(t, "demo@example.com", step.Inputs["email"], "but not inside other data")
	assert.Equal(t, "demo@example.com", step.Outputs["customer"])
	assert.Equal(t, "RCPT for [REDACTED]", step.Outputs["receipt"])
	assert.Equal(t, "checkout", step.StepID)

	assert.Equal(t, "RCPT for "+key, outputs["receipt"], "the run result is not modified")
	assert.Equal(t, "sent with "+key, p.Execution.Steps[0].Values["note"].Default)
}
