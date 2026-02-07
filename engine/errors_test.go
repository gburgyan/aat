package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"syscall"
	"testing"

	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
)

func TestErrorCategoryString(t *testing.T) {
	tests := []struct {
		cat  ErrorCategory
		want string
	}{
		{CategoryTransient, "transient"},
		{CategoryClient, "client"},
		{CategoryAuth, "auth"},
		{CategoryServer, "server"},
		{CategoryAdapter, "adapter"},
		{CategoryNetwork, "network"},
		{CategoryTimeout, "timeout"},
		{ErrorCategory(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cat.String())
		})
	}
}

func TestClassifyStatusCode(t *testing.T) {
	tests := []struct {
		code    int
		wantCat ErrorCategory
		wantErr bool
	}{
		// Success codes
		{200, 0, false},
		{201, 0, false},
		{204, 0, false},
		{301, 0, false},

		// Auth
		{401, CategoryAuth, true},
		{403, CategoryAuth, true},

		// Transient
		{429, CategoryTransient, true},
		{502, CategoryTransient, true},
		{503, CategoryTransient, true},
		{504, CategoryTransient, true},

		// Client
		{400, CategoryClient, true},
		{404, CategoryClient, true},
		{405, CategoryClient, true},
		{409, CategoryClient, true},
		{422, CategoryClient, true},
		{418, CategoryClient, true}, // I'm a teapot → generic client

		// Server
		{500, CategoryServer, true},
		{501, CategoryServer, true},
		{505, CategoryServer, true}, // other 5xx → server
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.code), func(t *testing.T) {
			cat, isErr := classifyStatusCode(tt.code)
			assert.Equal(t, tt.wantErr, isErr)
			if isErr {
				assert.Equal(t, tt.wantCat, cat)
			}
		})
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantCat ErrorCategory
	}{
		{
			name:    "nil error",
			err:     nil,
			wantCat: CategoryAdapter,
		},
		{
			name:    "context deadline exceeded",
			err:     context.DeadlineExceeded,
			wantCat: CategoryTimeout,
		},
		{
			name:    "wrapped deadline exceeded",
			err:     fmt.Errorf("executing request: %w", context.DeadlineExceeded),
			wantCat: CategoryTimeout,
		},
		{
			name: "DNS error",
			err: &net.DNSError{
				Err:  "no such host",
				Name: "api.example.com",
			},
			wantCat: CategoryNetwork,
		},
		{
			name: "wrapped DNS error",
			err: fmt.Errorf("executing HTTP request: %w", &url.Error{
				Op:  "Get",
				URL: "https://api.example.com",
				Err: &net.DNSError{Err: "no such host", Name: "api.example.com"},
			}),
			wantCat: CategoryNetwork,
		},
		{
			name: "connection refused",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &connectionError{msg: "connection refused"},
			},
			wantCat: CategoryTransient,
		},
		{
			name: "connection reset",
			err: &net.OpError{
				Op:  "read",
				Net: "tcp",
				Err: &connectionError{msg: "connection reset by peer"},
			},
			wantCat: CategoryTransient,
		},
		{
			name: "no route to host",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &connectionError{msg: "no route to host"},
			},
			wantCat: CategoryNetwork,
		},
		{
			name:    "generic error",
			err:     errors.New("template rendering failed"),
			wantCat: CategoryAdapter,
		},
		{
			name:    "wrapped generic error",
			err:     fmt.Errorf("building request: %w", errors.New("missing placeholder")),
			wantCat: CategoryAdapter,
		},
		{
			name: "url error wrapping OpError with connection refused",
			err: &url.Error{
				Op:  "Post",
				URL: "http://localhost:9999/api",
				Err: &net.OpError{
					Op:  "dial",
					Net: "tcp",
					Err: &connectionError{msg: "connection refused"},
				},
			},
			wantCat: CategoryTransient,
		},
		{
			name: "net timeout error",
			err: &url.Error{
				Op:  "Post",
				URL: "http://localhost:9999/api",
				Err: &timeoutError{},
			},
			wantCat: CategoryTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat := classifyError(tt.err)
			assert.Equal(t, tt.wantCat, cat)
		})
	}
}

// connectionError is a test helper that implements the error interface.
type connectionError struct {
	msg string
}

func (e *connectionError) Error() string { return e.msg }

// timeoutError is a test helper that implements net.Error with Timeout() = true.
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

func TestClassifyError_SyscallErrors(t *testing.T) {
	// Connection refused via syscall.ECONNREFUSED wrapped in OpError
	err := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: syscall.ECONNREFUSED,
		},
	}
	// The inner OpError.Error() won't contain "connection refused" directly in the outer,
	// but the outer OpError.Err.Error() will.
	cat := classifyError(err)
	// The errors.As finds the outer OpError; its Err.Error() may contain the phrase
	assert.Equal(t, CategoryTransient, cat)
}

func TestClassifyStepResult(t *testing.T) {
	tests := []struct {
		name    string
		result  StepResult
		wantNil bool
		wantCat ErrorCategory
	}{
		{
			name:    "success",
			result:  StepResult{StatusCode: 200},
			wantNil: true,
		},
		{
			name:    "error result",
			result:  StepResult{Error: errors.New("adapter error")},
			wantCat: CategoryAdapter,
		},
		{
			name:    "503 status",
			result:  StepResult{StatusCode: 503},
			wantCat: CategoryTransient,
		},
		{
			name:    "401 status",
			result:  StepResult{StatusCode: 401},
			wantCat: CategoryAuth,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cls := classifyStepResult(&tt.result)
			if tt.wantNil {
				assert.Nil(t, cls)
			} else {
				assert.NotNil(t, cls)
				assert.Equal(t, tt.wantCat, cls.Category)
			}
		})
	}
}

func TestShouldRetry(t *testing.T) {
	tests := []struct {
		name    string
		cat     ErrorCategory
		config  *plan.RetryConfig
		attempt int
		want    bool
	}{
		{
			name:    "nil config",
			cat:     CategoryTransient,
			config:  nil,
			attempt: 0,
			want:    false,
		},
		{
			name:    "max exhausted",
			cat:     CategoryTransient,
			config:  &plan.RetryConfig{Max: 2},
			attempt: 3,
			want:    false,
		},
		{
			name:    "transient retryable by default",
			cat:     CategoryTransient,
			config:  &plan.RetryConfig{Max: 3},
			attempt: 0,
			want:    true,
		},
		{
			name:    "timeout retryable by default",
			cat:     CategoryTimeout,
			config:  &plan.RetryConfig{Max: 3},
			attempt: 0,
			want:    true,
		},
		{
			name:    "server retryable by default",
			cat:     CategoryServer,
			config:  &plan.RetryConfig{Max: 3},
			attempt: 0,
			want:    true,
		},
		{
			name:    "client not retryable by default",
			cat:     CategoryClient,
			config:  &plan.RetryConfig{Max: 3},
			attempt: 0,
			want:    false,
		},
		{
			name:    "auth not retryable by default",
			cat:     CategoryAuth,
			config:  &plan.RetryConfig{Max: 3},
			attempt: 0,
			want:    false,
		},
		{
			name:    "adapter not retryable by default",
			cat:     CategoryAdapter,
			config:  &plan.RetryConfig{Max: 3},
			attempt: 0,
			want:    false,
		},
		{
			name:    "explicit On list includes category",
			cat:     CategoryClient,
			config:  &plan.RetryConfig{Max: 3, On: []string{"client"}},
			attempt: 0,
			want:    true,
		},
		{
			name:    "explicit On list excludes category",
			cat:     CategoryTransient,
			config:  &plan.RetryConfig{Max: 3, On: []string{"client"}},
			attempt: 0,
			want:    false,
		},
		{
			name:    "FailOn overrides On",
			cat:     CategoryServer,
			config:  &plan.RetryConfig{Max: 3, On: []string{"server"}, FailOn: []string{"server"}},
			attempt: 0,
			want:    false,
		},
		{
			name:    "FailOn overrides default",
			cat:     CategoryTransient,
			config:  &plan.RetryConfig{Max: 3, FailOn: []string{"transient"}},
			attempt: 0,
			want:    false,
		},
		{
			name:    "attempt within max",
			cat:     CategoryTransient,
			config:  &plan.RetryConfig{Max: 3},
			attempt: 2,
			want:    true,
		},
		{
			name:    "attempt at max",
			cat:     CategoryTransient,
			config:  &plan.RetryConfig{Max: 3},
			attempt: 3,
			want:    true,
		},
		{
			name:    "attempt exceeds max",
			cat:     CategoryTransient,
			config:  &plan.RetryConfig{Max: 3},
			attempt: 4,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRetry(tt.cat, tt.config, tt.attempt)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultRetryable(t *testing.T) {
	assert.True(t, defaultRetryable(CategoryTransient))
	assert.True(t, defaultRetryable(CategoryTimeout))
	assert.True(t, defaultRetryable(CategoryServer))
	assert.False(t, defaultRetryable(CategoryClient))
	assert.False(t, defaultRetryable(CategoryAuth))
	assert.False(t, defaultRetryable(CategoryAdapter))
	assert.False(t, defaultRetryable(CategoryNetwork))
}

func TestStatusCodeDetail(t *testing.T) {
	// Spot-check known codes
	assert.Equal(t, "HTTP 503 Service Unavailable", statusCodeDetail(503))
	assert.Equal(t, "HTTP 401 Unauthorized", statusCodeDetail(401))

	// Unknown 4xx
	assert.Contains(t, statusCodeDetail(418), "418")
	assert.Contains(t, statusCodeDetail(418), "Client Error")

	// Unknown 5xx
	assert.Contains(t, statusCodeDetail(599), "599")
	assert.Contains(t, statusCodeDetail(599), "Server Error")
}
