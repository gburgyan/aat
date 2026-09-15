package predicate

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEval_DecimalStringOrdering(t *testing.T) {
	tests := []struct {
		name   string
		expr   string
		fields map[string]any
		want   bool
	}{
		{name: "more digits is more", expr: `total > "999.50"`, fields: map[string]any{"total": "1000.00"}, want: true},
		{name: "a shorter fraction", expr: `total < "221.8"`, fields: map[string]any{"total": "221.78"}, want: true},
		{name: "an equal amount is at or above", expr: `total >= "221.78"`, fields: map[string]any{"total": "221.780"}, want: true},
		{name: "negative amounts", expr: `refund < "0"`, fields: map[string]any{"refund": "-12.50"}, want: true},
		{name: "equality stays exact text", expr: `total == "1.00"`, fields: map[string]any{"total": "1.0"}, want: false},
		{name: "dates order as text", expr: `day < "2026-10-01"`, fields: map[string]any{"day": "2026-09-15"}, want: true},
		{name: "an exponent orders as text", expr: `n > "9"`, fields: map[string]any{"n": "1e3"}, want: false},
		{name: "text orders as text", expr: `carrier < "BB"`, fields: map[string]any{"carrier": "AA"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Eval(tt.expr, tt.fields)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEvalExpanding_JSONNumberIsANumber(t *testing.T) {
	expand := func(string) (any, bool, error) { return json.Number("13066"), true, nil }

	got, err := EvalExpanding(`total == "{{checkout.total}}"`, map[string]any{"total": float64(13066)}, expand)

	require.NoError(t, err)
	assert.True(t, got, "an extracted number compares with a number field")
}
