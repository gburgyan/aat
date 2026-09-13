package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/graph/oas"
)

func TestStepMarks_OASNotValidated(t *testing.T) {
	skipped := &oas.PayloadResult{Skipped: true, SkipReason: "application/xml request bodies are not validated"}
	validated := &oas.PayloadResult{Valid: true}

	tests := []struct {
		name string
		v    *oas.ValidationResult
		want string
	}{
		{"request skipped", &oas.ValidationResult{Request: skipped, Response: validated}, "OAS: request not validated"},
		{"response skipped", &oas.ValidationResult{Response: skipped}, "OAS: response not validated"},
		{"both skipped", &oas.ValidationResult{Request: skipped, Response: skipped}, "OAS: not validated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			marks := stepMarks(engine.StepResult{OASValidation: tt.v}, false)
			assert.Contains(t, marks, tt.want)
			assert.NotContains(t, marks, "warning(s)", "a skipped payload has no violations to count")
		})
	}

	assert.Empty(t, stepMarks(engine.StepResult{OASValidation: &oas.ValidationResult{Response: validated}}, false))
}
