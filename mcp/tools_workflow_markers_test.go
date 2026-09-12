package mcp

import (
	"testing"

	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
)

func TestClassifyStepValue_AutowireMarkers(t *testing.T) {
	tests := []struct {
		name   string
		sv     plan.StepValue
		source string
		ref    string
	}{
		{name: "AUTOWIRE", sv: plan.StepValue{Default: "AUTOWIRE"}, source: "AUTOWIRE", ref: "(needs wiring)"},
		{name: "legacy PLACEHOLDER", sv: plan.StepValue{Default: "PLACEHOLDER"}, source: "AUTOWIRE", ref: "(needs wiring)"},
		{name: "optional", sv: plan.StepValue{Default: "AUTOWIRE?"}, source: "AUTOWIRE?", ref: "(optional; needs wiring)"},
		{name: "literal", sv: plan.StepValue{Default: "cart-1"}, source: "literal", ref: "cart-1"},
		{name: "reference", sv: plan.StepValue{From: "createCart.cartId"}, source: "from", ref: "createCart.cartId"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, ref := classifyStepValue(tt.sv)
			assert.Equal(t, tt.source, source)
			assert.Equal(t, tt.ref, ref)
		})
	}
}
