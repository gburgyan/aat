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

func TestRedactValue(t *testing.T) {
	secrets := map[string]bool{"my-secret-key": true, "api-key-123": true}

	tests := []struct {
		name    string
		val     any
		secrets map[string]bool
		want    any
	}{
		{"exact match", "my-secret-key", secrets, "[REDACTED]"},
		{"exact match other", "api-key-123", secrets, "[REDACTED]"},
		{"substring match", "Bearer my-secret-key here", secrets, "Bearer [REDACTED] here"},
		{"no match", "safe-value", secrets, "safe-value"},
		{"non-string int", 42, secrets, 42},
		{"non-string bool", true, secrets, true},
		{"nil value", nil, secrets, nil},
		{"nil secrets", "my-secret-key", nil, "my-secret-key"},
		{"empty secrets", "my-secret-key", map[string]bool{}, "my-secret-key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactValue(tt.val, tt.secrets)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRedactSlice(t *testing.T) {
	secrets := map[string]bool{"secret": true}

	t.Run("redacts matching elements", func(t *testing.T) {
		vals := []any{"secret", "safe", 42, "contains secret value"}
		got := RedactSlice(vals, secrets)
		assert.Equal(t, []any{"[REDACTED]", "safe", 42, "contains [REDACTED] value"}, got)
	})

	t.Run("nil secrets returns original", func(t *testing.T) {
		vals := []any{"secret"}
		got := RedactSlice(vals, nil)
		assert.Equal(t, vals, got)
	})

	t.Run("empty slice returns original", func(t *testing.T) {
		got := RedactSlice([]any{}, secrets)
		assert.Equal(t, []any{}, got)
	})

	t.Run("nil slice returns nil", func(t *testing.T) {
		got := RedactSlice(nil, secrets)
		assert.Nil(t, got)
	})
}

func TestRedactMap(t *testing.T) {
	secrets := map[string]bool{"secret-val": true}

	t.Run("redacts matching values", func(t *testing.T) {
		m := map[string]any{"key1": "secret-val", "key2": "safe", "key3": 42}
		got := RedactMap(m, secrets)
		assert.Equal(t, "[REDACTED]", got["key1"])
		assert.Equal(t, "safe", got["key2"])
		assert.Equal(t, 42, got["key3"])
	})

	t.Run("nil secrets returns original", func(t *testing.T) {
		m := map[string]any{"key": "secret-val"}
		got := RedactMap(m, nil)
		assert.Equal(t, m, got)
	})

	t.Run("nil map returns nil", func(t *testing.T) {
		got := RedactMap(nil, secrets)
		assert.Nil(t, got)
	})
}
