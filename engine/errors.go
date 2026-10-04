package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/internal/grpcstatus"
	"github.com/gburgyan/aat/plan"
)

// ErrorCategory classifies step failures for retry and reporting purposes.
type ErrorCategory int

const (
	// CategoryTransient covers 429, 502, 503, 504, connection refused/reset.
	CategoryTransient ErrorCategory = iota
	// CategoryClient covers 400, 404, 405, 409, 422, and other 4xx.
	CategoryClient
	// CategoryAuth covers 401, 403.
	CategoryAuth
	// CategoryServer covers 500, 501.
	CategoryServer
	// CategoryAdapter covers template errors, output extraction, input resolution.
	CategoryAdapter
	// CategoryNetwork covers DNS failure, TLS, no route to host.
	CategoryNetwork
	// CategoryTimeout covers context.DeadlineExceeded and net timeouts.
	CategoryTimeout
	// CategoryResponseError covers errors detected in 2xx response bodies.
	CategoryResponseError
)

// String returns the lowercase category name matching RetryConfig.On/FailOn values.
func (c ErrorCategory) String() string {
	switch c {
	case CategoryTransient:
		return "transient"
	case CategoryClient:
		return "client"
	case CategoryAuth:
		return "auth"
	case CategoryServer:
		return "server"
	case CategoryAdapter:
		return "adapter"
	case CategoryNetwork:
		return "network"
	case CategoryTimeout:
		return "timeout"
	case CategoryResponseError:
		return "response_error"
	default:
		return "unknown"
	}
}

// ErrorClassification captures how a step failure was categorized and what action was taken.
type ErrorClassification struct {
	Category     ErrorCategory
	Detail       string // e.g. "HTTP 503 Service Unavailable"
	Action       string // "retried", "failed", "failed_fast"
	RetryAttempt int    // which attempt (0-indexed)
}

// classifyError inspects a Go error from the HTTP executor and returns the
// appropriate ErrorCategory. It uses errors.Is/errors.As to traverse the
// wrapping chain.
func classifyError(err error) ErrorCategory {
	if err == nil {
		return CategoryAdapter
	}

	// Check for timeout first (context deadline, cancellation, or net timeout)
	if errors.Is(err, context.DeadlineExceeded) {
		return CategoryTimeout
	}
	if errors.Is(err, context.Canceled) {
		return CategoryTimeout
	}

	// Check for net.Error with Timeout()
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return CategoryTimeout
	}

	// Check for DNS errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return CategoryNetwork
	}

	// Check for OpError (connection refused/reset → transient, others → network)
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		msg := opErr.Err.Error()
		if strings.Contains(msg, "connection refused") || strings.Contains(msg, "connection reset") {
			return CategoryTransient
		}
		return CategoryNetwork
	}

	// No recognized network error — treat as adapter error
	return CategoryAdapter
}

// classifyStatusCode maps an HTTP status code to an ErrorCategory.
// Returns (category, true) for error status codes, or (0, false) for success.
func classifyStatusCode(code int) (ErrorCategory, bool) {
	if code < 400 {
		return 0, false
	}

	switch code {
	case 401, 403:
		return CategoryAuth, true
	case 429:
		return CategoryTransient, true
	case 500, 501:
		return CategoryServer, true
	case 502, 503, 504:
		return CategoryTransient, true
	default:
		if code >= 400 && code < 500 {
			return CategoryClient, true
		}
		// Any other 5xx
		return CategoryServer, true
	}
}

// classifyStepResult examines a StepResult and returns an ErrorClassification
// if the step failed. Returns nil for successful steps.
func classifyStepResult(result *StepResult) *ErrorClassification {
	if result.Error != nil {
		cat := classifyError(result.Error)
		return &ErrorClassification{
			Category: cat,
			Detail:   result.Error.Error(),
		}
	}

	if cat, isErr := classifyStatusCode(result.StatusCode); isErr {
		return &ErrorClassification{
			Category: cat,
			Detail:   statusDetail(result.Response, result.StatusCode),
		}
	}

	if result.ResponseBodyError != nil {
		return &ErrorClassification{
			Category: CategoryResponseError,
			Detail:   result.ResponseBodyError.Summary(),
		}
	}

	return nil
}

// statusDetail describes the status a response failed with. A gRPC response is
// described by the code it actually carries and the message the server sent,
// rather than by the HTTP status it maps to, which the caller never wrote and
// would not recognise.
func statusDetail(resp *adapter.Response, code int) string {
	if text, ok := grpcStatusText(resp); ok {
		return text
	}
	return statusCodeDetail(code)
}

// grpcStatusText describes a gRPC response by the code it carries and the
// message the server sent, which is where the reason for an unreachable host
// or a refused call lives. ok is false for an HTTP response, leaving the
// caller to supply its own wording for a status number.
func grpcStatusText(resp *adapter.Response) (string, bool) {
	if resp == nil || resp.GRPC == nil {
		return "", false
	}
	if resp.GRPC.Message != "" {
		return fmt.Sprintf("gRPC %s: %s", resp.GRPC.Name, resp.GRPC.Message), true
	}
	return "gRPC " + resp.GRPC.Name, true
}

// statusCodeDetail returns a human-readable description for an HTTP status code.
func statusCodeDetail(code int) string {
	switch code {
	case 400:
		return "HTTP 400 Bad Request"
	case 401:
		return "HTTP 401 Unauthorized"
	case 403:
		return "HTTP 403 Forbidden"
	case 404:
		return "HTTP 404 Not Found"
	case 405:
		return "HTTP 405 Method Not Allowed"
	case 409:
		return "HTTP 409 Conflict"
	case 422:
		return "HTTP 422 Unprocessable Entity"
	case 429:
		return "HTTP 429 Too Many Requests"
	case 500:
		return "HTTP 500 Internal Server Error"
	case 501:
		return "HTTP 501 Not Implemented"
	case 502:
		return "HTTP 502 Bad Gateway"
	case 503:
		return "HTTP 503 Service Unavailable"
	case 504:
		return "HTTP 504 Gateway Timeout"
	default:
		if code >= 400 && code < 500 {
			return fmt.Sprintf("HTTP %d Client Error", code)
		}
		return fmt.Sprintf("HTTP %d Server Error", code)
	}
}

// defaultRetryable returns true for error categories that are retryable by default
// when no explicit On list is configured.
func defaultRetryable(cat ErrorCategory) bool {
	switch cat {
	case CategoryTransient, CategoryTimeout, CategoryServer:
		return true
	default:
		return false
	}
}

// retryFailure is a failed attempt as retry rules read it.
type retryFailure struct {
	category ErrorCategory
	// status is the status the attempt's response stands for (see
	// StepResult.FailureStatus); 0 when there was no response.
	status int
	// grpcName is the gRPC status name, "" for HTTP and for a body error's
	// status.
	grpcName string
}

// statusCategory returns the category of the status an error a 200's body
// reports was given: such a response_error is retried, and matched by
// category rules, as that status would be. ok is false for any other failure.
func (f retryFailure) statusCategory() (ErrorCategory, bool) {
	if f.category != CategoryResponseError || f.status < 400 {
		return 0, false
	}
	cat, _ := classifyStatusCode(f.status)
	return cat, true
}

// shouldRetry determines whether a failed step should be retried based on
// the failure, the retry configuration, and the current attempt number. Rules
// in On/FailOn are category names (e.g. "transient"), HTTP status codes
// written as integers (e.g. 503), or gRPC status names (e.g. "UNAVAILABLE").
// An error a 200's body reports matches response_error, and, when it was
// given a status, that status and its category too.
func shouldRetry(f retryFailure, config *plan.RetryConfig, attempt int) bool {
	if config == nil {
		return false
	}
	if attempt > config.Max {
		return false
	}

	// FailOn overrides everything — if any rule matches, never retry
	for _, rule := range config.FailOn {
		if retryRuleMatches(rule, f) {
			return false
		}
	}

	// If On is specified, only retry when a rule matches
	if len(config.On) > 0 {
		for _, rule := range config.On {
			if retryRuleMatches(rule, f) {
				return true
			}
		}
		return false
	}

	// No On list — use defaults
	if cat, ok := f.statusCategory(); ok {
		return defaultRetryable(cat)
	}
	return defaultRetryable(f.category)
}

// retryRuleMatches reports whether a single retry rule matches a failure.
// Numeric rules compare against the HTTP status code, which a gRPC status maps
// to; a gRPC status name matches that status alone, since several share one
// HTTP status; other rules compare (case-insensitively) against the error
// category name, or the category of the status a body error was given.
func retryRuleMatches(rule string, f retryFailure) bool {
	rule = strings.TrimSpace(rule)
	if code, err := strconv.Atoi(rule); err == nil {
		return f.status != 0 && code == f.status
	}
	if code, ok := grpcstatus.CodeByName(rule); ok {
		got, isGRPC := grpcstatus.CodeByName(f.grpcName)
		return f.grpcName != "" && isGRPC && got == code
	}
	if cat, ok := f.statusCategory(); ok && strings.EqualFold(rule, cat.String()) {
		return true
	}
	return strings.EqualFold(rule, f.category.String())
}

// grpcStatusName returns a gRPC response's status name, and "" for an HTTP one.
func grpcStatusName(resp *adapter.Response) string {
	if resp == nil || resp.GRPC == nil {
		return ""
	}
	return resp.GRPC.Name
}

// ActualStatusText renders the status a step came back with, naming a gRPC
// code rather than the HTTP status it maps to. Several gRPC statuses share
// one HTTP status, so the name says what the number cannot.
func ActualStatusText(resp *adapter.Response, code int) string {
	if name := grpcStatusName(resp); name != "" {
		return name
	}
	return strconv.Itoa(code)
}

// StatusText renders the status a step came back with, as ActualStatusText
// does, and what its body made of it: "200, body error" for an error the body
// reports, or "200, body error as 500" for one given a status.
func (r *StepResult) StatusText() string {
	text := ActualStatusText(r.Response, r.StatusCode)
	switch rbe := r.ResponseBodyError; {
	case rbe != nil && rbe.Status != 0:
		text += fmt.Sprintf(", body error as %d", rbe.Status)
	case rbe != nil:
		text += ", body error"
	}
	return text
}

// failureStatusText describes the status a step failed with, for the message
// that ends a run. A gRPC step names its code and the server's message, which
// is where the reason for an unreachable host or a refused call lives; an HTTP
// step reads as it always has.
func failureStatusText(resp *adapter.Response, code int) string {
	if text, ok := grpcStatusText(resp); ok {
		return text
	}
	return fmt.Sprintf("status %d", code)
}
