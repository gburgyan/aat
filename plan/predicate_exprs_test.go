package plan

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/internal/predicate"
)

func TestEvalPredicateWithExprs(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	ectx := ExprContext{Now: now, Values: map[string]any{"quantity": 3, "sku": "SKU-1"}}

	tests := []struct {
		name    string
		expr    string
		fields  map[string]any
		want    bool
		wantErr string
	}{
		{name: "a date", expr: `deliveryDate == "{{today + 3 days}}"`, fields: map[string]any{"deliveryDate": "2026-09-16"}, want: true},
		{name: "a number from an input", expr: `quantity == "{{quantity}}"`, fields: map[string]any{"quantity": 3.0}, want: true},
		{name: "an integer timestamp", expr: `createdAt >= "{{unixtime - 1 hours}}"`, fields: map[string]any{"createdAt": float64(now.Unix())}, want: true},
		{name: "text and a plain literal", expr: `sku == "{{sku}}" && status == "open"`, fields: map[string]any{"sku": "SKU-1", "status": "open"}, want: true},
		{name: "mixed text", expr: `note == "shipped {{today}}"`, fields: map[string]any{"note": "shipped 2026-09-13"}, want: true},
		{name: "false", expr: `quantity == "{{quantity}}"`, fields: map[string]any{"quantity": 4.0}},
		{name: "an expression that can't be evaluated", expr: `sku == "{{missingInput}}"`, fields: map[string]any{"sku": "SKU-1"}, wantErr: "evaluating"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvalPredicateWithExprs(tt.expr, tt.fields, ectx)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	literal, err := predicate.Eval(`deliveryDate == "{{today + 3 days}}"`, map[string]any{"deliveryDate": "2026-09-16"})
	require.NoError(t, err)
	assert.False(t, literal, "predicate.Eval keeps the literal as text")
}

func TestValidatePredicateExprs(t *testing.T) {
	assert.NoError(t, ValidatePredicateExprs(`total > "{{amount}}"`))
	assert.NoError(t, ValidatePredicateExprs(`status == "open"`))
	err := ValidatePredicateExprs(`deliveryDate == "{{today +}}"`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"{{today +}}"`)
}
