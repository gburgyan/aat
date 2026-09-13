package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFingerprint_GeneratedExpressionsAreNotEvaluated checks that batch dedup
// sees a generated value's expression rather than a value it generated, so the
// same permutation fingerprints the same way every time.
func TestFingerprint_GeneratedExpressionsAreNotEvaluated(t *testing.T) {
	build := func() *Plan {
		return &Plan{Execution: Execution{Steps: []Step{{
			Node:   "createOrder",
			Values: map[string]StepValue{"ref": {Default: "order-{{uuid}}"}},
		}}}}
	}

	first := Fingerprint(build())
	assert.NotEmpty(t, first)
	assert.Equal(t, first, Fingerprint(build()))
}
