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
func ToArchive(result *RunResult, meta archive.ArchiveMetadata, baseURL string) *archive.Archive {
	a := &archive.Archive{
		Metadata: meta,
		Result: archive.ArchiveResult{
			Outcome: result.Outcome.String(),
			Error:   errString(result.Error),
		},
	}

	a.Steps = convertStepResults(result.Steps, baseURL)
	a.Cleanup = convertStepResults(result.CleanupResults, baseURL)

	return a
}

func convertStepResults(steps []StepResult, baseURL string) []archive.StepRecord {
	if len(steps) == 0 {
		return nil
	}
	records := make([]archive.StepRecord, len(steps))
	for i, s := range steps {
		records[i] = convertStepResult(s, baseURL)
	}
	return records
}

func convertStepResult(s StepResult, baseURL string) archive.StepRecord {
	rec := archive.StepRecord{
		Node:       s.Node,
		StartTime:  s.StartTime,
		DurationMs: s.Duration.Milliseconds(),
		Inputs:     s.Inputs,
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
	if s.ErrorClass != nil {
		rec.ErrorClass = convertErrorClass(s.ErrorClass)
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
