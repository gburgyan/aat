package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gburgyan/aat/engine"
)

// TestQuietSetupCopy checks that a setup copy's line is hidden exactly when
// the engine says it passed, whatever made it fail: a status, an error body,
// a strict OpenAPI violation, or an expectFailure that got a success.
func TestQuietSetupCopy(t *testing.T) {
	assert.True(t, quietSetupCopy(engine.StepResult{FuzzSetup: "add__fuzz_q_a", StatusCode: 201}))
	assert.True(t, quietSetupCopy(engine.StepResult{FuzzSetup: "add__fuzz_q_a", StatusCode: 409, ExpectFailure: &engine.ExpectFailureResult{Passed: true}}),
		"a copy that failed as expected passed")
	assert.False(t, quietSetupCopy(engine.StepResult{StatusCode: 201}), "not a copy")
	assert.False(t, quietSetupCopy(engine.StepResult{FuzzSetup: "add__fuzz_q_a", StatusCode: 201, FuzzSetupFailed: true}),
		"a copy the engine failed, here on strict OAS, is shown")
}
