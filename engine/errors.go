package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

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
			Detail:   statusCodeDetail(result.StatusCode),
		}
	}

	return nil
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

// shouldRetry determines whether a failed step should be retried based on
// the error category, retry configuration, and current attempt number.
func shouldRetry(cat ErrorCategory, config *plan.RetryConfig, attempt int) bool {
	if config == nil {
		return false
	}
	if attempt > config.Max {
		return false
	}

	catName := cat.String()

	// FailOn overrides everything — if the category is in failOn, never retry
	for _, f := range config.FailOn {
		if f == catName {
			return false
		}
	}

	// If On is specified, only retry listed categories
	if len(config.On) > 0 {
		for _, o := range config.On {
			if o == catName {
				return true
			}
		}
		return false
	}

	// No On list — use defaults
	return defaultRetryable(cat)
}
