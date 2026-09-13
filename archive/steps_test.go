package archive

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStepID(t *testing.T) {
	assert.Equal(t, "checkout", StepID(StepRecord{StepID: "checkout", Node: "checkoutCart"}))
	assert.Equal(t, "deleteCart", StepID(StepRecord{Node: "deleteCart"}), "a record without an ID uses its node")
}

func TestCleanupStepIDs(t *testing.T) {
	tests := []struct {
		name  string
		steps []StepRecord
		want  []string
	}{
		{name: "unique", steps: []StepRecord{{Node: "a"}, {Node: "b"}}, want: []string{"a", "b"}},
		{name: "repeated", steps: []StepRecord{{Node: "x"}, {Node: "x"}, {Node: "x"}}, want: []string{"x", "x_2", "x_3"}},
		{
			name:  "mixed",
			steps: []StepRecord{{Node: "a"}, {Node: "b"}, {Node: "a"}, {StepID: "c", Node: "z"}, {Node: "b"}},
			want:  []string{"a", "b", "a_2", "c", "b_2"},
		},
		{name: "empty", steps: nil, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CleanupStepIDs(tt.steps))
		})
	}
}

func TestFindStep(t *testing.T) {
	a := &Archive{
		Steps: []StepRecord{
			{StepID: "checkout", Node: "checkoutCart"},
			{Node: "getOrder"},
		},
		Cleanup: []StepRecord{
			{StepID: "deleteCart", Node: "deleteCart"},
			{Node: "deleteCart"},
		},
	}

	step, cleanup := FindStep(a, "checkout")
	require.NotNil(t, step)
	assert.Equal(t, "checkoutCart", step.Node)
	assert.False(t, cleanup)

	step, cleanup = FindStep(a, "getOrder")
	require.NotNil(t, step, "a record without an ID is found by its node")
	assert.False(t, cleanup)

	step, cleanup = FindStep(a, "deleteCart_2")
	require.NotNil(t, step)
	assert.Same(t, &a.Cleanup[1], step, "the second cleanup step with the same ID")
	assert.True(t, cleanup)

	step, _ = FindStep(a, "checkoutCart")
	assert.Nil(t, step, "a node name is not a step ID")
}

func TestStepPassed(t *testing.T) {
	tests := []struct {
		name     string
		step     StepRecord
		expected bool
	}{
		{name: "passing step", step: StepRecord{Validation: &ValidationRecord{Passed: true}}, expected: true},
		{name: "step with error", step: StepRecord{Error: "failed"}, expected: false},
		{name: "failed validation", step: StepRecord{Validation: &ValidationRecord{Passed: false}}, expected: false},
		{name: "failed expect-failure", step: StepRecord{ExpectFailure: &ExpectFailureRecord{Passed: false}}, expected: false},
		{name: "passed expect-failure", step: StepRecord{ExpectFailure: &ExpectFailureRecord{Passed: true}}, expected: true},
		{name: "response body error", step: StepRecord{ResponseBodyError: &ResponseBodyErrorRecord{RulePath: "errors"}}, expected: false},
		{name: "no validation or errors", step: StepRecord{}, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, StepPassed(tt.step))
		})
	}
}
