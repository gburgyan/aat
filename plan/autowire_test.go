package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAutowireMarker(t *testing.T) {
	tests := []struct {
		name     string
		sv       StepValue
		marker   bool
		optional bool
	}{
		{name: "AUTOWIRE", sv: StepValue{Default: "AUTOWIRE"}, marker: true},
		{name: "optional AUTOWIRE?", sv: StepValue{Default: "AUTOWIRE?"}, marker: true, optional: true},
		{name: "legacy PLACEHOLDER", sv: StepValue{Default: "PLACEHOLDER"}, marker: true},
		{name: "lowercase", sv: StepValue{Default: "autowire"}},
		{name: "other punctuation", sv: StepValue{Default: "AUTOWIRE!"}},
		{name: "non-string", sv: StepValue{Default: 1}},
		{name: "from reference", sv: StepValue{From: "createCart.cartId"}},
		{name: "empty", sv: StepValue{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			marker, optional := AutowireMarker(tt.sv)
			assert.Equal(t, tt.marker, marker)
			assert.Equal(t, tt.optional, optional)
		})
	}
}
