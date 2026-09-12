package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateErrorDetectionRule_EqualsValue(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"string", "ERROR", ""},
		{"integer", 0, ""},
		{"float", 1.5, ""},
		{"boolean", true, ""},
		{"missing", nil, "equals rule requires a value"},
		{"map", map[string]any{"kind": "x"}, "equals value must be a string, number, or boolean"},
		{"list", []any{"a"}, "equals value must be a string, number, or boolean"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateErrorDetectionRule(ErrorDetectionRule{Path: "status", Rule: "equals", Value: tt.value})
			if tt.want == "" {
				assert.Empty(t, errs)
				return
			}
			assert.Contains(t, errs, tt.want)
		})
	}
}
