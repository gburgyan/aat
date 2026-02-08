package plan

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedTime() time.Time {
	return time.Date(2026, 2, 8, 12, 0, 0, 0, time.UTC)
}

func testEnv(key string) string {
	m := map[string]string{
		"MY_VAR":  "my-value",
		"API_KEY": "secret-key-123",
	}
	return m[key]
}

func TestEvalExpr(t *testing.T) {
	tests := []struct {
		name    string
		raw     any
		ctx     ExprContext
		want    any
		wantErr string
	}{
		// --- Non-string passthrough ---
		{
			name: "integer passthrough",
			raw:  42,
			want: 42,
		},
		{
			name: "nil passthrough",
			raw:  nil,
			want: nil,
		},
		{
			name: "bool passthrough",
			raw:  true,
			want: true,
		},
		// --- No expression passthrough ---
		{
			name: "plain string no expression",
			raw:  "DEN",
			want: "DEN",
		},
		{
			name: "string with single brace",
			raw:  "hello {world}",
			want: "hello {world}",
		},
		// --- today ---
		{
			name: "today bare",
			raw:  "{{today}}",
			ctx:  ExprContext{Now: fixedTime()},
			want: "2026-02-08",
		},
		{
			name: "today plus 5 days",
			raw:  "{{today + 5 days}}",
			ctx:  ExprContext{Now: fixedTime()},
			want: "2026-02-13",
		},
		{
			name: "today minus 3 days",
			raw:  "{{today - 3 days}}",
			ctx:  ExprContext{Now: fixedTime()},
			want: "2026-02-05",
		},
		{
			name: "today plus 1 day (singular)",
			raw:  "{{today + 1 day}}",
			ctx:  ExprContext{Now: fixedTime()},
			want: "2026-02-09",
		},
		{
			name: "today with extra spaces",
			raw:  "{{  today  +  10  days  }}",
			ctx:  ExprContext{Now: fixedTime()},
			want: "2026-02-18",
		},
		// --- env ---
		{
			name: "env var present",
			raw:  "{{env.MY_VAR}}",
			ctx:  ExprContext{Env: testEnv},
			want: "my-value",
		},
		{
			name: "env var API_KEY",
			raw:  "{{env.API_KEY}}",
			ctx:  ExprContext{Env: testEnv},
			want: "secret-key-123",
		},
		{
			name:    "env var missing",
			raw:     "{{env.MISSING}}",
			ctx:     ExprContext{Env: testEnv},
			wantErr: "not set or empty",
		},
		// --- reference ---
		{
			name: "bare reference",
			raw:  "{{departureDate}}",
			ctx: ExprContext{
				Values: map[string]any{"departureDate": "2026-03-01"},
			},
			want: "2026-03-01",
		},
		{
			name: "reference plus days",
			raw:  "{{departureDate + 3 days}}",
			ctx: ExprContext{
				Values: map[string]any{"departureDate": "2026-03-01"},
			},
			want: "2026-03-04",
		},
		{
			name: "reference minus days",
			raw:  "{{departureDate - 1 day}}",
			ctx: ExprContext{
				Values: map[string]any{"departureDate": "2026-03-01"},
			},
			want: "2026-02-28",
		},
		{
			name:    "reference not found",
			raw:     "{{unknownRef}}",
			ctx:     ExprContext{},
			wantErr: "not found in resolved values",
		},
		{
			name:    "reference not a date string",
			raw:     "{{departureDate + 1 day}}",
			ctx:     ExprContext{Values: map[string]any{"departureDate": 42}},
			wantErr: "not a date string",
		},
		{
			name:    "reference bad date format",
			raw:     "{{departureDate + 1 day}}",
			ctx:     ExprContext{Values: map[string]any{"departureDate": "not-a-date"}},
			wantErr: "not a valid date",
		},
		// --- Mixed ---
		{
			name: "prefix and suffix with today",
			raw:  "depart-{{today}}-flight",
			ctx:  ExprContext{Now: fixedTime()},
			want: "depart-2026-02-08-flight",
		},
		{
			name: "multiple expressions",
			raw:  "{{today}}-to-{{today + 7 days}}",
			ctx:  ExprContext{Now: fixedTime()},
			want: "2026-02-08-to-2026-02-15",
		},
		{
			name: "expression with env in mixed",
			raw:  "key={{env.API_KEY}}&date={{today}}",
			ctx:  ExprContext{Now: fixedTime(), Env: testEnv},
			want: "key=secret-key-123&date=2026-02-08",
		},
		// --- Errors ---
		{
			name:    "unclosed delimiter",
			raw:     "{{today",
			wantErr: "unclosed expression delimiter",
		},
		{
			name:    "empty expression",
			raw:     "{{}}",
			wantErr: "empty expression",
		},
		{
			name:    "invalid syntax",
			raw:     "{{123abc}}",
			wantErr: "invalid expression syntax",
		},
		{
			name:    "invalid expression base",
			raw:     "{{foo.bar + 1 day}}",
			wantErr: "invalid expression",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvalExpr(tt.raw, tt.ctx)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestContainsExpr(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"plain string", false},
		{"{{today}}", true},
		{"prefix-{{today}}-suffix", true},
		{"{single brace}", false},
		{"", false},
		{"{{", true}, // contains {{ even if malformed
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, ContainsExpr(tt.input))
		})
	}
}

func TestValidateExpr(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"valid today", "{{today}}", false},
		{"valid today plus days", "{{today + 5 days}}", false},
		{"valid env", "{{env.MY_VAR}}", false},
		{"valid reference", "{{departureDate}}", false},
		{"valid reference with arith", "{{departureDate + 3 days}}", false},
		{"valid mixed", "prefix-{{today}}", false},
		{"valid multiple", "{{today}}-to-{{today + 7 days}}", false},
		{"no expressions", "plain string", false},
		{"unclosed", "{{today", true},
		{"empty expr", "{{}}", true},
		{"invalid syntax", "{{123bad}}", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateExpr(tt.raw)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEvalExpr_CrossMonthBoundary(t *testing.T) {
	// Test that date arithmetic handles month boundaries correctly
	ctx := ExprContext{Now: time.Date(2026, 1, 30, 0, 0, 0, 0, time.UTC)}
	got, err := EvalExpr("{{today + 5 days}}", ctx)
	require.NoError(t, err)
	assert.Equal(t, "2026-02-04", got)
}

func TestEvalExpr_CrossYearBoundary(t *testing.T) {
	ctx := ExprContext{Now: time.Date(2025, 12, 29, 0, 0, 0, 0, time.UTC)}
	got, err := EvalExpr("{{today + 5 days}}", ctx)
	require.NoError(t, err)
	assert.Equal(t, "2026-01-03", got)
}

func TestEvalExpr_ReferenceNonStringNonArith(t *testing.T) {
	// A bare reference to a non-string value should return it as-is
	ctx := ExprContext{Values: map[string]any{"count": 42}}
	got, err := EvalExpr("{{count}}", ctx)
	require.NoError(t, err)
	assert.Equal(t, 42, got)
}

func TestEvalExpr_ReferenceInMixedReturnsStringified(t *testing.T) {
	// In mixed mode, non-string values get stringified
	ctx := ExprContext{
		Now:    fixedTime(),
		Values: map[string]any{"count": 42},
	}
	got, err := EvalExpr("items-{{count}}-on-{{today}}", ctx)
	require.NoError(t, err)
	assert.Equal(t, "items-42-on-2026-02-08", got)
}
