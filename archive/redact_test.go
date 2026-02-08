package archive

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactHeaders(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    map[string]string
	}{
		{
			name:    "nil headers",
			headers: nil,
			want:    nil,
		},
		{
			name:    "empty headers",
			headers: map[string]string{},
			want:    map[string]string{},
		},
		{
			name: "no sensitive headers",
			headers: map[string]string{
				"Content-Type": "application/json",
				"Accept":       "application/json",
			},
			want: map[string]string{
				"Content-Type": "application/json",
				"Accept":       "application/json",
			},
		},
		{
			name: "authorization redacted",
			headers: map[string]string{
				"Authorization": "Bearer secret-token",
				"Content-Type":  "application/json",
			},
			want: map[string]string{
				"Authorization": "[REDACTED]",
				"Content-Type":  "application/json",
			},
		},
		{
			name: "case insensitive matching",
			headers: map[string]string{
				"AUTHORIZATION": "Bearer secret",
				"X-Api-Key":     "key-123",
				"x-auth-token":  "tok-456",
			},
			want: map[string]string{
				"AUTHORIZATION": "[REDACTED]",
				"X-Api-Key":     "[REDACTED]",
				"x-auth-token":  "[REDACTED]",
			},
		},
		{
			name: "cookie and set-cookie redacted",
			headers: map[string]string{
				"Cookie":     "session=abc",
				"Set-Cookie": "session=abc; Path=/",
			},
			want: map[string]string{
				"Cookie":     "[REDACTED]",
				"Set-Cookie": "[REDACTED]",
			},
		},
		{
			name: "proxy-authorization redacted",
			headers: map[string]string{
				"Proxy-Authorization": "Basic abc123",
			},
			want: map[string]string{
				"Proxy-Authorization": "[REDACTED]",
			},
		},
		{
			name: "mixed sensitive and non-sensitive",
			headers: map[string]string{
				"Authorization": "Bearer token",
				"Content-Type":  "application/json",
				"X-Request-Id":  "req-123",
				"X-API-KEY":     "secret",
			},
			want: map[string]string{
				"Authorization": "[REDACTED]",
				"Content-Type":  "application/json",
				"X-Request-Id":  "req-123",
				"X-API-KEY":     "[REDACTED]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactHeaders(tt.headers)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRedactHeaders_DoesNotMutateInput(t *testing.T) {
	original := map[string]string{
		"Authorization": "Bearer secret",
		"Content-Type":  "application/json",
	}
	RedactHeaders(original)
	assert.Equal(t, "Bearer secret", original["Authorization"])
}
