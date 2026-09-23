package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gburgyan/aat/engine"
)

func TestQuietSetupCopy(t *testing.T) {
	copyOf := func(r engine.StepResult) engine.StepResult {
		r.FuzzSetup = "add__fuzz_q_a"
		return r
	}
	assert.True(t, quietSetupCopy(copyOf(engine.StepResult{StatusCode: 201})))
	assert.False(t, quietSetupCopy(engine.StepResult{StatusCode: 201}), "not a copy")
	assert.False(t, quietSetupCopy(copyOf(engine.StepResult{StatusCode: 409})))
	assert.False(t, quietSetupCopy(copyOf(engine.StepResult{StatusCode: 200, ResponseBodyError: &engine.ResponseBodyError{}})),
		"a copy whose body is an error failed")
	assert.True(t, quietSetupCopy(copyOf(engine.StepResult{StatusCode: 409, ExpectFailure: &engine.ExpectFailureResult{Passed: true}})),
		"a copy that failed as expected passed")
	assert.False(t, quietSetupCopy(copyOf(engine.StepResult{StatusCode: 201, ExpectFailure: &engine.ExpectFailureResult{}})))
}
