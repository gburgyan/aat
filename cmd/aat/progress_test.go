package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/plan"
	"github.com/gburgyan/aat/validate"
	"github.com/stretchr/testify/assert"
)

var noColorTerm = TerminalInfo{IsTTY: false, Width: 80}

func TestCLIProgressObserver_StepComplete_Success(t *testing.T) {
	var buf bytes.Buffer
	obs := &CLIProgressObserver{out: &buf, term: noColorTerm}

	obs.OnStepComplete(0, 3, engine.StepResult{
		Node:       "searchFlights",
		StatusCode: 200,
		Response:   &adapter.Response{StatusCode: 200},
		Duration:   150 * time.Millisecond,
	})

	output := buf.String()
	assert.Contains(t, output, "[1/3]")
	assert.Contains(t, output, "searchFlights")
	assert.Contains(t, output, "200")
	assert.Contains(t, output, "150ms")
}

func TestCLIProgressObserver_StepComplete_Error(t *testing.T) {
	var buf bytes.Buffer
	obs := &CLIProgressObserver{out: &buf, term: noColorTerm}

	obs.OnStepComplete(1, 3, engine.StepResult{
		Node:  "commitReservation",
		Error: assert.AnError,
	})

	output := buf.String()
	assert.Contains(t, output, "[2/3]")
	assert.Contains(t, output, "commitReservation")
	assert.Contains(t, output, "ERROR:")
}

func TestCLIProgressObserver_StepComplete_WithRetries(t *testing.T) {
	var buf bytes.Buffer
	obs := &CLIProgressObserver{out: &buf, term: noColorTerm}

	obs.OnStepComplete(0, 1, engine.StepResult{
		Node:       "searchFlights",
		Error:      assert.AnError,
		RetryCount: 2,
		ErrorClass: &engine.ErrorClassification{Category: engine.CategoryServer},
	})

	output := buf.String()
	assert.Contains(t, output, "ERROR [server]")
	assert.Contains(t, output, "after 2 retries")
}

func TestCLIProgressObserver_StepComplete_DisplayOutputs(t *testing.T) {
	var buf bytes.Buffer
	obs := &CLIProgressObserver{out: &buf, term: noColorTerm}

	obs.OnStepComplete(0, 1, engine.StepResult{
		Node:       "commitReservation",
		StatusCode: 200,
		Response:   &adapter.Response{StatusCode: 200},
		Duration:   200 * time.Millisecond,
		DisplayOutputs: []engine.DisplayOutput{
			{Label: "PNR", Name: "locator", Value: "ABCDEF"},
			{Label: "Reservation ID", Name: "reservationId", Value: "res-123"},
		},
	})

	output := buf.String()
	assert.Contains(t, output, "PNR: ABCDEF")
	assert.Contains(t, output, "Reservation ID: res-123")
}

func TestCLIProgressObserver_StepComplete_AssertionsFailed(t *testing.T) {
	var buf bytes.Buffer
	obs := &CLIProgressObserver{out: &buf, term: noColorTerm}

	obs.OnStepComplete(0, 1, engine.StepResult{
		Node:       "verify",
		StatusCode: 200,
		Response:   &adapter.Response{StatusCode: 200},
		Duration:   50 * time.Millisecond,
		Validation: &validate.MechanicalResult{Passed: false},
	})

	output := buf.String()
	assert.Contains(t, output, "ASSERTIONS FAILED")
}

func TestCLIProgressObserver_StepComplete_NoResponse(t *testing.T) {
	var buf bytes.Buffer
	obs := &CLIProgressObserver{out: &buf, term: noColorTerm}

	obs.OnStepComplete(0, 1, engine.StepResult{
		Node: "brokenStep",
	})

	output := buf.String()
	assert.Contains(t, output, "(no response)")
}

func TestCLIProgressObserver_CleanupOutput(t *testing.T) {
	var buf bytes.Buffer
	obs := &CLIProgressObserver{out: &buf, term: noColorTerm}

	obs.OnCleanupStart(2)
	obs.OnCleanupStepComplete(0, 2, engine.StepResult{
		Node:       "deleteBooking",
		StatusCode: 204,
		Response:   &adapter.Response{StatusCode: 204},
		Duration:   80 * time.Millisecond,
	})
	obs.OnCleanupStepComplete(1, 2, engine.StepResult{
		Node:  "deleteSession",
		Error: assert.AnError,
	})

	output := buf.String()
	assert.Contains(t, output, "cleanup:")
	assert.Contains(t, output, "deleteBooking")
	assert.Contains(t, output, "204")
	assert.Contains(t, output, "deleteSession")
	assert.Contains(t, output, "ERROR:")
}

func TestCLIProgressObserver_RunComplete_Passed(t *testing.T) {
	var buf bytes.Buffer
	obs := &CLIProgressObserver{out: &buf, term: noColorTerm}

	obs.OnRunComplete(&engine.RunResult{
		Outcome: engine.OutcomePassed,
		Steps: []engine.StepResult{
			{Duration: 100 * time.Millisecond},
			{Duration: 200 * time.Millisecond},
		},
	})

	output := buf.String()
	assert.Contains(t, output, "PASSED (2/2 steps, 300ms)")
}

func TestCLIProgressObserver_RunComplete_Failed(t *testing.T) {
	var buf bytes.Buffer
	obs := &CLIProgressObserver{out: &buf, term: noColorTerm}

	obs.OnRunComplete(&engine.RunResult{
		Outcome: engine.OutcomeFailed,
		Error:   assert.AnError,
		Steps:   []engine.StepResult{{Duration: 100 * time.Millisecond}},
	})

	output := buf.String()
	assert.Contains(t, output, "FAILED:")
}

func TestCLIProgressObserver_RunComplete_Error(t *testing.T) {
	var buf bytes.Buffer
	obs := &CLIProgressObserver{out: &buf, term: noColorTerm}

	obs.OnRunComplete(&engine.RunResult{
		Outcome: engine.OutcomeError,
		Error:   assert.AnError,
	})

	output := buf.String()
	assert.Contains(t, output, "ERROR:")
}

func TestCLIProgressObserver_OnStepStart_NoOp(t *testing.T) {
	var buf bytes.Buffer
	obs := &CLIProgressObserver{out: &buf, term: noColorTerm}

	obs.OnStepStart(0, 3, plan.Step{Node: "test"})

	assert.Empty(t, buf.String(), "OnStepStart should be a no-op for CLI")
}

func TestCLIProgressObserver_OnRunStart_CachesTotal(t *testing.T) {
	obs := &CLIProgressObserver{term: noColorTerm}
	obs.OnRunStart(5, "lean")
	assert.Equal(t, 5, obs.total)
}

func TestCLIProgressObserver_StepComplete_WithColor(t *testing.T) {
	var buf bytes.Buffer
	obs := &CLIProgressObserver{out: &buf, term: TerminalInfo{IsTTY: true, Width: 80}}

	obs.OnStepComplete(0, 3, engine.StepResult{
		Node:       "searchFlights",
		StatusCode: 200,
		Response:   &adapter.Response{StatusCode: 200},
		Duration:   150 * time.Millisecond,
	})

	output := buf.String()
	// Should contain ANSI codes for cyan node and green status
	assert.Contains(t, output, colorCyan)
	assert.Contains(t, output, colorGreen)
	assert.Contains(t, output, colorReset)
	assert.Contains(t, output, "searchFlights")
	assert.Contains(t, output, "200")
}

func TestCLIProgressObserver_WideTerminal_NodeTruncation(t *testing.T) {
	var buf bytes.Buffer
	// Narrow terminal: node column = max(15, 50-60) = 15
	obs := &CLIProgressObserver{out: &buf, term: TerminalInfo{IsTTY: false, Width: 50}}

	obs.OnStepComplete(0, 1, engine.StepResult{
		Node:       "veryLongNodeNameThatExceedsColumn",
		StatusCode: 200,
		Response:   &adapter.Response{StatusCode: 200},
		Duration:   50 * time.Millisecond,
	})

	output := buf.String()
	assert.Contains(t, output, "~")
	assert.Contains(t, output, "200")
}

func TestCLIProgressObserver_WideTerminal_RetryDetail(t *testing.T) {
	var buf bytes.Buffer
	obs := &CLIProgressObserver{out: &buf, term: TerminalInfo{IsTTY: false, Width: 120}}

	obs.OnStepComplete(0, 1, engine.StepResult{
		Node:       "searchFlights",
		Error:      assert.AnError,
		RetryCount: 2,
		StatusCode: 502,
		ErrorClass: &engine.ErrorClassification{Category: engine.CategoryServer},
	})

	output := buf.String()
	assert.Contains(t, output, "retried 2x")
	assert.Contains(t, output, "last status 502")
}
