package engine

import (
	"encoding/json"
	"strings"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/graph/oas"
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
	a.Metadata.InstantiatedPlan = result.InstantiatedPlan

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
		StepID:          s.StepID,
		Node:            s.Node,
		StartTime:       s.StartTime,
		DurationMs:      s.Duration.Milliseconds(),
		Inputs:          archive.RedactMap(s.Inputs, secrets),
		Outputs:         s.Outputs,
		TransformScript: s.TransformScript,
		Error:           errString(s.Error),
		RetryCount:      s.RetryCount,
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
	if len(s.DisplayOutputs) > 0 {
		rec.DisplayOutputs = make([]archive.DisplayOutputRecord, len(s.DisplayOutputs))
		for i, do := range s.DisplayOutputs {
			rec.DisplayOutputs[i] = archive.DisplayOutputRecord{
				Label: do.Label,
				Name:  do.Name,
				Value: do.Value,
			}
		}
	}
	if s.ResponseBodyError != nil {
		rec.ResponseBodyError = &archive.ResponseBodyErrorRecord{
			RulePath: s.ResponseBodyError.RulePath,
			Rule:     s.ResponseBodyError.Rule,
			Message:  s.ResponseBodyError.Message,
			Code:     s.ResponseBodyError.Code,
			Category: s.ResponseBodyError.Category,
		}
	}
	if s.OASValidation != nil {
		rec.OASValidation = convertOASValidation(s.OASValidation)
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
		records[i] = archive.SelectionRecord{
			InputName:     s.InputName,
			SourceNode:    s.SourceNode,
			SourceField:   s.SourceField,
			SourceSize:    s.SourceSize,
			FilterExpr:    s.FilterExpr,
			FilteredSize:  s.FilteredSize,
			Strategy:      s.Strategy,
			SelectedIndex: s.SelectedIndex,
			SelectionName: s.SelectionName,
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
			InputName:  r.InputName,
			Source:     r.Source,
			RawValue:   archive.RedactValue(r.RawValue, secrets),
			FinalValue: archive.RedactValue(r.FinalValue, secrets),
			FromStep:   r.FromStep,
			FromOutput: r.FromOutput,
			FromInput:  r.FromInput,
			Expression: r.Expression,
			Constraint: r.Constraint,
			PoolIndex:  r.PoolIndex,
			PoolSize:   r.PoolSize,
			Tried:      archive.RedactSlice(r.Tried, secrets),
		}
		if r.Constraint != "" {
			ok := r.ConstraintOK
			rec.ConstraintOK = &ok
		}
		records[i] = rec
	}
	return records
}

func convertOASValidation(v *oas.ValidationResult) *archive.OASValidationRecord {
	rec := &archive.OASValidationRecord{
		OperationID: v.OperationID,
		Skipped:     v.Skipped,
		SkipReason:  v.SkipReason,
	}
	if v.Request != nil {
		rec.Request = convertOASPayload(v.Request)
	}
	if v.Response != nil {
		rec.Response = convertOASPayload(v.Response)
	}
	return rec
}

func convertOASPayload(p *oas.PayloadResult) *archive.OASPayloadRecord {
	rec := &archive.OASPayloadRecord{
		Valid:               p.Valid,
		CompilationWarnings: p.CompilationWarnings,
	}
	for _, e := range p.Errors {
		rec.Errors = append(rec.Errors, archive.OASSchemaError{
			Path:    e.Path,
			Message: e.Message,
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
