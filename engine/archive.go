package engine

import (
	"encoding/json"
	"strings"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/graph/oas"
	"github.com/gburgyan/aat/plan"
	"github.com/gburgyan/aat/validate"
)

// ToArchive converts an engine RunResult into a serializable Archive.
// The baseURL is prepended to request paths to produce full URLs.
// Credential headers and the archived plans' literal credentials are always
// redacted. Every known secret in secrets is then redacted wherever it appears
// (see archive.Redact); the result shares nothing with the run result. An error
// means the archive could not be redacted and must not be written.
func ToArchive(result *RunResult, meta archive.ArchiveMetadata, baseURL string, secrets map[string]bool) (*archive.Archive, error) {
	meta.Plan = redactPlan(meta.Plan)
	a := &archive.Archive{
		Metadata: meta,
		Result: archive.ArchiveResult{
			Outcome:    result.Outcome.String(),
			Error:      errString(result.Error),
			DurationMs: result.Elapsed().Milliseconds(),
		},
	}

	a.Steps = convertStepResults(result.Steps, baseURL)
	a.Cleanup = convertStepResults(result.CleanupResults, baseURL)
	a.CleanupSkipped = convertCleanupSkips(result.CleanupSkipped)
	a.Metadata.InstantiatedPlan = redactPlan(result.InstantiatedPlan)

	// Redact fails only on a value encoding/json cannot marshal. The archive is
	// withheld then: returned unredacted, it would carry the secrets redaction
	// exists to remove.
	return archive.Redact(a, secrets)
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

func convertCleanupSkips(skips []CleanupSkip) []archive.CleanupSkipRecord {
	if len(skips) == 0 {
		return nil
	}
	records := make([]archive.CleanupSkipRecord, len(skips))
	for i, s := range skips {
		records[i] = archive.CleanupSkipRecord(s)
	}
	return records
}

func convertStepResult(s StepResult, baseURL string) archive.StepRecord {
	rec := archive.StepRecord{
		StepID:          s.StepID,
		Node:            s.Node,
		StartTime:       s.StartTime,
		DurationMs:      s.Duration.Milliseconds(),
		Inputs:          s.Inputs,
		Outputs:         s.Outputs,
		TransformScript: s.TransformScript,
		Error:           errString(s.Error),
		RetryCount:      s.RetryCount,
		CleanupFor:      s.CleanupFor,
		WhenError:       s.WhenError,
	}
	for _, c := range s.RetriedOn {
		rec.RetriedOn = append(rec.RetriedOn, c.String())
	}

	if s.Request != nil {
		rec.Request = convertRequest(s.Request, s.ActualBaseURL, baseURL, s.OriginalPath)
	}
	if s.Response != nil {
		rec.Response = convertResponse(s.Response)
	}
	if s.Validation != nil {
		rec.Validation = convertValidation(s.Validation)
	}
	if len(s.Selections) > 0 {
		rec.Selections = SelectionRecords(s.Selections)
	}
	if len(s.Resolutions) > 0 {
		rec.Resolutions = convertResolutions(s.Resolutions)
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
	if len(s.Iterations) > 0 {
		rec.Iterations = make([]archive.IterationRecord, len(s.Iterations))
		for i, it := range s.Iterations {
			rec.Iterations[i] = convertIteration(it, baseURL)
		}
		rec.RepeatStop = s.RepeatStop
	}

	return rec
}

// convertIteration converts one request of a repeated step.
func convertIteration(it IterationResult, baseURL string) archive.IterationRecord {
	rec := archive.IterationRecord{
		Index:      it.Index,
		StartTime:  it.StartTime,
		DurationMs: it.Duration.Milliseconds(),
		Outputs:    it.Outputs,
		UntilMet:   it.UntilMet,
		RetryCount: it.RetryCount,
		Error:      errString(it.Error),
	}
	if it.Request != nil {
		rec.Request = convertRequest(it.Request, it.ActualBaseURL, baseURL, it.OriginalPath)
	}
	if it.Response != nil {
		rec.Response = convertResponse(it.Response)
	}
	if it.OASValidation != nil {
		rec.OASValidation = convertOASValidation(it.OASValidation)
	}
	return rec
}

func convertRequest(req *adapter.Request, actualBaseURL, defaultBaseURL, originalPath string) *archive.RequestRecord {
	// Use the actual executor base URL if available, fall back to default
	effectiveBase := actualBaseURL
	if effectiveBase == "" {
		effectiveBase = defaultBaseURL
	}
	actualURL := joinArchivedURL(effectiveBase, req.Path)

	rec := &archive.RequestRecord{
		Method:  req.Method,
		URL:     actualURL,
		Headers: archive.RedactHeaders(req.Headers),
		Body:    toRawMessage(req.Body),
	}

	// Compute what the URL would have been without the override/rewrite
	origPath := req.Path
	if originalPath != "" {
		origPath = originalPath
	}
	originalURL := joinArchivedURL(defaultBaseURL, origPath)
	if originalURL != actualURL {
		rec.OriginalURL = originalURL
	}

	return rec
}

// joinArchivedURL returns the URL a request for path on base goes to, joined
// as the executor joins it; a pair that does not parse is recorded as written.
func joinArchivedURL(base, path string) string {
	if joined, err := adapter.JoinURL(base, path); err == nil {
		return joined
	}
	return base + path
}

func convertResponse(resp *adapter.Response) *archive.ResponseRecord {
	return &archive.ResponseRecord{
		Status:  resp.StatusCode,
		Headers: archive.RedactHeaders(flattenHeaders(resp.Headers)),
		Body:    toRawMessage(resp.Body),
	}
}

// redactPlan returns p with its auth credentials' literal values and its
// credential headers redacted, copying what it changes so the caller's plan is
// untouched. Environment-variable references keep their variable names, which
// are not secret.
func redactPlan(p *plan.Plan) *plan.Plan {
	if p == nil || (p.Auth == nil && len(p.Headers) == 0) {
		return p
	}
	cp := *p
	if p.Auth != nil {
		auth := *p.Auth
		auth.Credentials = make(map[string]config.SecretRef, len(p.Auth.Credentials))
		for name, ref := range p.Auth.Credentials {
			if ref.Value != "" {
				ref.Value = archive.Redacted
			}
			auth.Credentials[name] = ref
		}
		cp.Auth = &auth
	}
	cp.Headers = archive.RedactHeaders(p.Headers)
	return &cp
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
			Raw:     r.Raw,
		})
	}
	return rec
}

// SelectionRecords converts a step's selection decisions to archive records,
// for the archive and for tie warnings (see archive.SelectionTieWarnings).
func SelectionRecords(sels []SelectionDecision) []archive.SelectionRecord {
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
			Field:         s.Field,
			SortField:     s.SortField,
			SortValue:     s.SortValue,
			Ties:          s.Ties,
			OnTie:         s.OnTie,
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

func convertResolutions(resolutions []ValueResolution) []archive.ValueResolutionRecord {
	records := make([]archive.ValueResolutionRecord, len(resolutions))
	for i, r := range resolutions {
		rec := archive.ValueResolutionRecord{
			InputName:  r.InputName,
			Source:     r.Source,
			RawValue:   r.RawValue,
			FinalValue: r.FinalValue,
			FromStep:   r.FromStep,
			FromOutput: r.FromOutput,
			FromInput:  r.FromInput,
			Expression: r.Expression,
			Constraint: r.Constraint,
			PoolIndex:  r.PoolIndex,
			PoolSize:   r.PoolSize,
			Tried:      r.Tried,
			Error:      r.Error,
			Layer:      r.Layer,
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
		Skipped:             p.Skipped,
		SkipReason:          p.SkipReason,
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
