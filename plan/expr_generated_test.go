package plan

import (
	"bytes"
	"errors"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// uuidPattern matches a lowercase version 4 UUID.
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// zeros returns a reader of n zero bytes.
func zeros(n int) *bytes.Reader {
	return bytes.NewReader(make([]byte, n))
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

func TestEvalExpr_GeneratedValues(t *testing.T) {
	now := fixedTime()
	tests := []struct {
		name string
		raw  string
		ctx  ExprContext
		want any
	}{
		{"uuid from zero bytes", "{{uuid}}", ExprContext{Random: zeros(16)}, "00000000-0000-4000-8000-000000000000"},
		{"random from zero bytes", "{{random 8}}", ExprContext{Random: zeros(8)}, "00000000"},
		{"random skips bytes of 252 and up", "{{random 1}}", ExprContext{Random: bytes.NewReader([]byte{252, 253, 254, 255, 37})}, "1"},
		{"random maps bytes onto digits and letters", "{{random 3}}", ExprContext{Random: bytes.NewReader([]byte{10, 35, 36})}, "az0"},
		{"mixed text", "order-{{random 4}}", ExprContext{Random: zeros(4)}, "order-0000"},
		{"now", "{{now}}", ExprContext{Now: now}, "2026-02-08T12:00:00Z"},
		{"now plus minutes", "{{now + 90 minutes}}", ExprContext{Now: now}, "2026-02-08T13:30:00Z"},
		{"now minus days", "{{ now - 2 days }}", ExprContext{Now: now}, "2026-02-06T12:00:00Z"},
		{"a singular unit", "{{now + 1 hour}}", ExprContext{Now: now}, "2026-02-08T13:00:00Z"},
		{"unixtime is an integer", "{{unixtime}}", ExprContext{Now: now}, now.Unix()},
		{"unixtime minus hours", "{{unixtime - 1 hours}}", ExprContext{Now: now}, now.Unix() - 3600},
		{"unixtime plus seconds", "{{unixtime + 30 seconds}}", ExprContext{Now: now}, now.Unix() + 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvalExpr(tt.raw, tt.ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestEvalExpr_GeneratedValuesDiffer checks the real random source: each
// expression, and each occurrence in one value, generates its own value.
func TestEvalExpr_GeneratedValuesDiffer(t *testing.T) {
	first, err := EvalExpr("{{uuid}}", ExprContext{})
	require.NoError(t, err)
	second, err := EvalExpr("{{uuid}}", ExprContext{})
	require.NoError(t, err)
	assert.Regexp(t, uuidPattern, first)
	assert.NotEqual(t, first, second)

	pair, err := EvalExpr("{{random 16}}/{{random 16}}", ExprContext{})
	require.NoError(t, err)
	parts := regexp.MustCompile(`^([0-9a-z]{16})/([0-9a-z]{16})$`).FindStringSubmatch(pair.(string))
	require.Len(t, parts, 3, "got %q", pair)
	assert.NotEqual(t, parts[1], parts[2])
}

func TestEvalExpr_GeneratedValueReaderError(t *testing.T) {
	for _, raw := range []string{"{{uuid}}", "{{random 4}}"} {
		_, err := EvalExpr(raw, ExprContext{Random: failingReader{}})
		require.Error(t, err, raw)
		assert.Contains(t, err.Error(), "no entropy")
	}
}

func TestValidateExpr_GeneratedValues(t *testing.T) {
	for _, valid := range []string{
		"{{uuid}}", "{{random 1}}", "{{random 64}}", "{{now}}", "{{now + 3 seconds}}",
		"{{unixtime - 3600 seconds}}", "{{unixtime + 2 days}}", "ref-{{uuid}}",
	} {
		assert.NoError(t, ValidateExpr(valid), valid)
	}

	tests := []struct {
		raw     string
		wantErr string
	}{
		{"{{random 0}}", "length from 1 to 64"},
		{"{{random 65}}", "length from 1 to 64"},
		{"{{random x}}", "length from 1 to 64"},
		{"{{today + 2 hours}}", "for a time use {{now + 2 hours}} or {{unixtime + 2 hours}}"},
		{"{{uuid + 1 days}}", "uuid takes no offset"},
		{"{{random + 5 minutes}}", "random takes no offset"},
		{"{{now + 2 fortnights}}", `unknown time unit "fortnights"`},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			err := ValidateExpr(tt.raw)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestEvalExpr_RandomWithoutLength(t *testing.T) {
	_, err := EvalExpr("{{random}}", ExprContext{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{{random N}}")

	got, err := EvalExpr("{{random}}", ExprContext{Values: map[string]any{"random": "an input"}})
	require.NoError(t, err)
	assert.Equal(t, "an input", got, "an input named random still resolves")
}
