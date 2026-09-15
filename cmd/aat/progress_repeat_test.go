package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gburgyan/aat/engine"
)

func TestRepeatNote(t *testing.T) {
	tests := []struct {
		name   string
		result engine.StepResult
		want   string
	}{
		{name: "not repeated", result: engine.StepResult{}, want: ""},
		{name: "condition held", result: engine.StepResult{Iterations: make([]engine.IterationResult, 5), RepeatStop: engine.RepeatStopUntil}, want: "5 requests"},
		{name: "one request", result: engine.StepResult{Iterations: make([]engine.IterationResult, 1), RepeatStop: engine.RepeatStopUntil}, want: "1 request"},
		{name: "limit reached", result: engine.StepResult{Iterations: make([]engine.IterationResult, 3), RepeatStop: engine.RepeatStopMax}, want: "3 requests, stopped: max"},
		{name: "pages ran out", result: engine.StepResult{Iterations: make([]engine.IterationResult, 2), RepeatStop: engine.RepeatStopExhausted}, want: "2 requests"},
		{name: "cursor came back", result: engine.StepResult{Iterations: make([]engine.IterationResult, 2), RepeatStop: engine.RepeatStopLoop}, want: "2 requests, stopped: loop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, repeatNote(tt.result))
		})
	}
}
