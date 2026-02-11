package engine

import (
	"encoding/json"
	"strings"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/validate"
)

// ToArchive converts an engine RunResult into a serializable Archive.
// The baseURL is prepended to request paths to produce full URLs.
// If secrets is non-nil, matching values in Inputs, Resolutions, etc. are redacted.
func ToArchive(result *RunResult, meta archive.ArchiveMetadata, baseURL string, secrets map[string]bool) *archive.Archive {
	a := &archive.Archive{
		Metadata: meta,
		Result: archive.ArchiveResult{
			Outcome: result.Outcome.String(),
			Error:   errString(result.Error),
		},
	}

	a.Steps = convertStepResults(result.Steps, baseURL, secrets)
	a.Cleanup = convertStepResults(result.CleanupResults, baseURL, secrets)

	return a
}

func convertStepResults(steps []StepResult, baseURL string, secrets map[string]bool) []archive.StepRecord {
	if len(steps) == 0 {
		return nil
	}
	records := make([]archive.StepRecord, len(steps))
	for i, s := range steps {
		records[i] = convertStepResult(s, baseURL, secrets)
	}
	return records
}

func convertStepResult(s StepResult, baseURL string, secrets map[string]bool) archive.StepRecord {
	rec := archive.StepRecord{
		Node:       s.Node,
		StartTime:  s.StartTime,
		DurationMs: s.Duration.Milliseconds(),
		Inputs:     archive.RedactMap(s.Inputs, secrets),
		Outputs:    s.Outputs,
		Error:      errString(s.Error),
		RetryCount: s.RetryCount,
	}

	if s.Request != nil {
		rec.Request = convertRequest(s.Request, baseURL)
	}
	if s.Response != nil {
		rec.Response = convertResponse(s.Response)
	}
	if s.Validation != nil {
		rec.Validation = convertValidation(s.Validation)
	}
	if len(s.Selections) > 0 {
		rec.Selections = convertSelections(s.Selections)
	}
	if len(s.Resolutions) > 0 {
		rec.Resolutions = convertResolutions(s.Resolutions, secrets)
	}
	if len(s.Relaxations) > 0 {
		rec.Relaxations = convertRelaxations(s.Relaxations)
	}
	if s.ErrorClass != nil {
		rec.ErrorClass = convertErrorClass(s.ErrorClass)
	}
	if s.ExpectFailure != nil {
		rec.ExpectFailure = &archive.ExpectFailureRecord{
			Expected: s.ExpectFailure.ExpectedStatuses,
			Actual:   s.ExpectFailure.ActualStatus,
			Passed:   s.ExpectFailure.Passed,
		}
	}

	return rec
}

func convertRequest(req *adapter.Request, baseURL string) *archive.RequestRecord {
	return &archive.RequestRecord{
		Method:  req.Method,
		URL:     baseURL + req.Path,
		Headers: archive.RedactHeaders(req.Headers),
		Body:    toRawMessage(req.Body),
	}
}

func convertResponse(resp *adapter.Response) *archive.ResponseRecord {
	return &archive.ResponseRecord{
		Status:  resp.StatusCode,
		Headers: archive.RedactHeaders(flattenHeaders(resp.Headers)),
		Body:    toRawMessage(resp.Body),
	}
}

func convertValidation(v *validate.MechanicalResult) *archive.ValidationRecord {
	rec := &archive.ValidationRecord{
		Passed: v.Passed,
	}
	for _, r := range v.Results {
		rec.Results = append(rec.Results, archive.AssertionRecord{
			Type:    string(r.Type),
			Passed:  r.Passed,
			Skipped: r.Skipped,
			Message: r.Message,
			Path:    r.Path,
			Expr:    r.Expr,
		})
	}
	return rec
}

func convertSelections(sels []SelectionDecision) []archive.SelectionRecord {
	records := make([]archive.SelectionRecord, len(sels))
	for i, s := range sels {
		rec := archive.SelectionRecord{
			InputName:     s.InputName,
			SourceNode:    s.SourceNode,
			SourceField:   s.SourceField,
			SourceSize:    s.SourceSize,
			FilterExpr:    s.FilterExpr,
			FilteredSize:  s.FilteredSize,
			Strategy:      s.Strategy,
			SelectedIndex: s.SelectedIndex,
			SelectionName: s.SelectionName,
			FilterRelaxed: s.FilterRelaxed,
		}
		if s.LLMCall != nil {
			rec.LLMCall = convertLLMCall(s.LLMCall)
		}
		records[i] = rec
	}
	return records
}

func convertRelaxations(relaxations []RelaxationRecord) []archive.RelaxationArchiveRecord {
	records := make([]archive.RelaxationArchiveRecord, len(relaxations))
	for i, r := range relaxations {
		records[i] = archive.RelaxationArchiveRecord{
			ConstraintName: r.ConstraintName,
			InputRef:       r.InputRef,
			Reason:         r.Reason,
			Depth:          r.Depth,
		}
	}
	return records
}

func convertErrorClass(ec *ErrorClassification) *archive.ErrorClassRecord {
	return &archive.ErrorClassRecord{
		Category:     ec.Category.String(),
		Detail:       ec.Detail,
		Action:       ec.Action,
		RetryAttempt: ec.RetryAttempt,
	}
}

func convertResolutions(resolutions []ValueResolution, secrets map[string]bool) []archive.ValueResolutionRecord {
	records := make([]archive.ValueResolutionRecord, len(resolutions))
	for i, r := range resolutions {
		rec := archive.ValueResolutionRecord{
			InputName:         r.InputName,
			Source:            r.Source,
			RawValue:          archive.RedactValue(r.RawValue, secrets),
			FinalValue:        archive.RedactValue(r.FinalValue, secrets),
			FromStep:          r.FromStep,
			FromOutput:        r.FromOutput,
			Expression:        r.Expression,
			Constraint:        r.Constraint,
			PoolIndex:         r.PoolIndex,
			PoolSize:          r.PoolSize,
			Tried:             archive.RedactSlice(r.Tried, secrets),
			Relaxed:           r.Relaxed,
			RelaxedConstraint: r.RelaxedConstraint,
		}
		if r.Constraint != "" {
			ok := r.ConstraintOK
			rec.ConstraintOK = &ok
		}
		if r.LLMCall != nil {
			rec.LLMCall = convertLLMCall(r.LLMCall)
		}
		records[i] = rec
	}
	return records
}

func convertLLMCall(call *LLMCallRecord) *archive.LLMCallRecord {
	rec := &archive.LLMCallRecord{
		Model:        call.Model,
		Response:     call.Response,
		InputTokens:  call.InputTokens,
		OutputTokens: call.OutputTokens,
		DurationMs:   call.DurationMs,
		FinishReason: call.FinishReason,
		Error:        call.Error,
	}
	for _, m := range call.Messages {
		rec.Messages = append(rec.Messages, archive.LLMMessageRecord{
			Role:    m.Role,
			Content: m.Content,
		})
	}
	return rec
}

// errString returns the error message or empty string for nil.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// flattenHeaders converts http.Header (map[string][]string) to map[string]string
// by joining multiple values with ", ".
func flattenHeaders(h map[string][]string) map[string]string {
	if h == nil {
		return nil
	}
	flat := make(map[string]string, len(h))
	for k, vs := range h {
		flat[k] = strings.Join(vs, ", ")
	}
	return flat
}

// toRawMessage converts a byte slice to json.RawMessage.
// If the bytes are valid JSON, they are used directly. Otherwise they are
// marshalled as a JSON string. Nil or empty input returns nil.
func toRawMessage(data []byte) json.RawMessage {
	if len(data) == 0 {
		return nil
	}
	if json.Valid(data) {
		return json.RawMessage(data)
	}
	// Not valid JSON — encode as a JSON string
	encoded, _ := json.Marshal(string(data))
	return json.RawMessage(encoded)
}
