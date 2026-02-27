package oas

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/gburgyan/aat/graph"
	valerrors "github.com/pb33f/libopenapi-validator/errors"
)

// ValidationResult captures OAS validation for a single step's request and response.
type ValidationResult struct {
	OperationID string         // from the node's OAS ref
	Request     *PayloadResult // nil if request not validated (no body, non-JSON, etc.)
	Response    *PayloadResult // nil if response not validated
	Skipped     bool
	SkipReason  string
}

// PayloadResult captures validation for one direction (request or response).
type PayloadResult struct {
	Valid               bool
	Errors              []SchemaError
	CompilationWarnings []string // messages for schemas that couldn't be compiled (library limitation)
}

// SchemaError is a single OAS validation error.
type SchemaError struct {
	Path     string // JSON path or parameter location
	Message  string // human-readable description
	HowToFix string // from libopenapi-validator (optional)
}

// HasErrors returns true if request or response has validation errors.
func (r *ValidationResult) HasErrors() bool {
	if r == nil {
		return false
	}
	if r.Request != nil && !r.Request.Valid {
		return true
	}
	if r.Response != nil && !r.Response.Valid {
		return true
	}
	return false
}

// ErrorCount returns the total number of errors across both directions.
func (r *ValidationResult) ErrorCount() int {
	if r == nil {
		return 0
	}
	count := 0
	if r.Request != nil {
		count += len(r.Request.Errors)
	}
	if r.Response != nil {
		count += len(r.Response.Errors)
	}
	return count
}

// ValidateStep validates a step's request and response against the OAS spec.
// Returns nil when no OAS ref is configured for the node or specs aren't loaded.
func ValidateStep(
	node *graph.Node,
	graphOAS string,
	cache *SpecCache,
	method string,
	path string,
	reqHeaders map[string]string,
	reqBody []byte,
	respStatusCode int,
	respHeaders http.Header,
	respBody []byte,
) *ValidationResult {
	if node.OAS == nil {
		return nil
	}

	operationID := node.OAS.OperationID
	if operationID == "" {
		return nil
	}

	// Resolve the spec for this node
	specRef := ResolveNodeSpec(node, graphOAS)
	if specRef == "" {
		return &ValidationResult{
			OperationID: operationID,
			Skipped:     true,
			SkipReason:  "no OAS spec path configured",
		}
	}

	entry := cache.Get(specRef)
	if entry == nil {
		return &ValidationResult{
			OperationID: operationID,
			Skipped:     true,
			SkipReason:  "spec not loaded: " + specRef,
		}
	}

	// Find the operation to get the spec path and path item
	foundMethod, specPath, pathItem, _, err := FindOperation(entry.Model, operationID)
	if err != nil {
		return &ValidationResult{
			OperationID: operationID,
			Skipped:     true,
			SkipReason:  err.Error(),
		}
	}

	// Use the method from the spec if the caller didn't provide one
	if method == "" {
		method = foundMethod
	}

	// Build the http.Request for validation.
	// The validator matches paths using the spec's path template, so we use specPath.
	httpReq, err := buildHTTPRequest(method, specPath, reqHeaders, reqBody)
	if err != nil {
		return &ValidationResult{
			OperationID: operationID,
			Skipped:     true,
			SkipReason:  "failed to build request: " + err.Error(),
		}
	}

	// Use the cached validator from the SpecEntry (created once per spec).
	v := entry.Validator

	result := &ValidationResult{OperationID: operationID}

	// Validate request using WithPathItem to bypass path matching
	if len(reqBody) > 0 && isJSON(reqBody) {
		_, reqErrs := v.ValidateHttpRequestSyncWithPathItem(httpReq, pathItem, specPath)
		result.Request = convertValidationErrors(reqErrs)
	}

	// Validate response using the response body sub-validator with PathItem
	if respBody != nil {
		httpResp := buildHTTPResponse(respStatusCode, respHeaders, respBody)
		_, respErrs := v.GetResponseBodyValidator().ValidateResponseBodyWithPathItem(httpReq, httpResp, pathItem, specPath)
		result.Response = convertValidationErrors(respErrs)
	}

	return result
}

// buildHTTPRequest constructs an http.Request for the validator.
func buildHTTPRequest(method, path string, headers map[string]string, body []byte) (*http.Request, error) {
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, path, bodyReader)
	if err != nil {
		return nil, err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// Ensure content-type is set for JSON bodies
	if len(body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

// buildHTTPResponse constructs an http.Response for the validator.
func buildHTTPResponse(statusCode int, headers http.Header, body []byte) *http.Response {
	resp := &http.Response{
		StatusCode: statusCode,
		Header:     headers,
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
	if resp.Header == nil {
		resp.Header = make(http.Header)
	}
	if resp.Header.Get("Content-Type") == "" && isJSON(body) {
		resp.Header.Set("Content-Type", "application/json")
	}
	return resp
}

// isCompilationError returns true if the validation error represents a schema
// compilation failure rather than an actual validation finding. These occur when
// the library can't compile a JSON Schema (e.g., circular refs, complex allOf chains).
func isCompilationError(e *valerrors.ValidationError) bool {
	if strings.HasSuffix(e.Message, "failed schema compilation") ||
		strings.HasSuffix(e.Message, "failed schema rendering") {
		return true
	}
	for _, sve := range e.SchemaValidationErrors {
		if strings.Contains(sve.Reason, "failed schema compilation") ||
			strings.Contains(sve.Reason, "failed schema rendering") {
			return true
		}
	}
	return false
}

// convertValidationErrors converts libopenapi-validator errors to our types.
// Schema compilation failures are separated into CompilationWarnings rather than
// being counted as validation errors, since they represent library limitations.
func convertValidationErrors(errs []*valerrors.ValidationError) *PayloadResult {
	if len(errs) == 0 {
		return &PayloadResult{Valid: true}
	}

	var schemaErrors []SchemaError
	var compilationWarnings []string

	for _, e := range errs {
		// Filter out schema compilation failures — these are library limitations,
		// not real validation findings.
		if isCompilationError(e) {
			compilationWarnings = append(compilationWarnings, e.Message)
			continue
		}

		if len(e.SchemaValidationErrors) > 0 {
			for _, sve := range e.SchemaValidationErrors {
				se := SchemaError{
					Message: sve.Reason,
				}
				if sve.FieldPath != "" {
					se.Path = sve.FieldPath
				} else if len(sve.InstancePath) > 0 {
					se.Path = "/" + strings.Join(sve.InstancePath, "/")
				}
				schemaErrors = append(schemaErrors, se)
			}
		} else {
			se := SchemaError{
				Message:  e.Message,
				HowToFix: e.HowToFix,
			}
			if e.SpecPath != "" {
				se.Path = e.SpecPath
			}
			schemaErrors = append(schemaErrors, se)
		}
	}

	return &PayloadResult{
		Valid:               len(schemaErrors) == 0 && len(compilationWarnings) == 0,
		Errors:              schemaErrors,
		CompilationWarnings: compilationWarnings,
	}
}

// isJSON returns true if the data looks like JSON (starts with { or [).
func isJSON(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return false
	}
	return trimmed[0] == '{' || trimmed[0] == '['
}
