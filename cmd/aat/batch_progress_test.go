package main

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/plan"
	"github.com/gburgyan/aat/validate"
	"github.com/stretchr/testify/assert"
)

// --- BatchStreamObserver tests ---

func TestBatchStreamObserver_Lifecycle(t *testing.T) {
	var buf bytes.Buffer
	obs := NewBatchStreamObserver(&buf, "roundtrip-booking")

	obs.OnRunStart(3, "strict")
	obs.OnStepStart(0, 3, stepForNode("searchFlights"))
	obs.OnStepComplete(0, 3, engine.StepResult{
		Node:       "searchFlights",
		StatusCode: 200,
		Response:   &adapter.Response{StatusCode: 200},
		Duration:   45 * time.Millisecond,
	})
	obs.OnStepComplete(1, 3, engine.StepResult{
		Node:       "priceOffers",
		StatusCode: 200,
		Response:   &adapter.Response{StatusCode: 200},
		Duration:   120 * time.Millisecond,
	})
	obs.OnStepComplete(2, 3, engine.StepResult{
		Node:       "commitReservation",
		StatusCode: 200,
		Response:   &adapter.Response{StatusCode: 200},
		Duration:   80 * time.Millisecond,
	})
	obs.OnRunComplete(&engine.RunResult{
		Outcome: engine.OutcomePassed,
		Steps: []engine.StepResult{
			{Duration: 45 * time.Millisecond},
			{Duration: 120 * time.Millisecond},
			{Duration: 80 * time.Millisecond},
		},
	})

	output := buf.String()

	// Header
	assert.Contains(t, output, "── roundtrip-booking (3 steps, mode=strict) ──")

	// Steps are indented with 4 spaces
	assert.Contains(t, output, "[1/3]")
	assert.Contains(t, output, "searchFlights")
	assert.Contains(t, output, "200")
	assert.Contains(t, output, "45ms")
	assert.Contains(t, output, "[2/3]")
	assert.Contains(t, output, "priceOffers")
	assert.Contains(t, output, "[3/3]")
	assert.Contains(t, output, "commitReservation")

	// Footer
	assert.Contains(t, output, "── roundtrip-booking: PASSED")
}

func TestBatchStreamObserver_ErrorStep(t *testing.T) {
	var buf bytes.Buffer
	obs := NewBatchStreamObserver(&buf, "test-plan")

	obs.OnRunStart(1, "strict")
	obs.OnStepComplete(0, 1, engine.StepResult{
		Node:  "failNode",
		Error: assert.AnError,
	})
	obs.OnRunComplete(&engine.RunResult{
		Outcome: engine.OutcomeError,
		Error:   assert.AnError,
	})

	output := buf.String()
	assert.Contains(t, output, "ERROR:")
	assert.Contains(t, output, "── test-plan: ERROR")
}

func TestBatchStreamObserver_FailedStep(t *testing.T) {
	var buf bytes.Buffer
	obs := NewBatchStreamObserver(&buf, "test-plan")

	obs.OnRunStart(1, "strict")
	obs.OnStepComplete(0, 1, engine.StepResult{
		Node:       "badNode",
		StatusCode: 400,
		Response:   &adapter.Response{StatusCode: 400},
		Duration:   50 * time.Millisecond,
	})
	obs.OnRunComplete(&engine.RunResult{
		Outcome: engine.OutcomeFailed,
	})

	output := buf.String()
	assert.Contains(t, output, "400")
	assert.Contains(t, output, "── test-plan: FAILED")
}

func TestBatchStreamObserver_AssertionsFailed(t *testing.T) {
	var buf bytes.Buffer
	obs := NewBatchStreamObserver(&buf, "test-plan")

	obs.OnRunStart(1, "strict")
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

func TestBatchStreamObserver_WithRetries(t *testing.T) {
	var buf bytes.Buffer
	obs := NewBatchStreamObserver(&buf, "test-plan")

	obs.OnRunStart(1, "strict")
	obs.OnStepComplete(0, 1, engine.StepResult{
		Node:       "flakyNode",
		Error:      assert.AnError,
		RetryCount: 3,
		ErrorClass: &engine.ErrorClassification{Category: engine.CategoryServer},
	})

	output := buf.String()
	assert.Contains(t, output, "ERROR [server]")
	assert.Contains(t, output, "after 3 retries")
}

func TestBatchStreamObserver_CleanupOutput(t *testing.T) {
	var buf bytes.Buffer
	obs := NewBatchStreamObserver(&buf, "test-plan")

	obs.OnRunStart(1, "strict")
	obs.OnCleanupStart(1)
	obs.OnCleanupStepComplete(0, 1, engine.StepResult{
		Node:       "deleteBooking",
		StatusCode: 204,
		Response:   &adapter.Response{StatusCode: 204},
		Duration:   30 * time.Millisecond,
	})

	output := buf.String()
	assert.Contains(t, output, "cleanup:")
	assert.Contains(t, output, "deleteBooking")
	assert.Contains(t, output, "204")
}

func TestBatchStreamObserver_CleanupError(t *testing.T) {
	var buf bytes.Buffer
	obs := NewBatchStreamObserver(&buf, "test-plan")

	obs.OnCleanupStart(1)
	obs.OnCleanupStepComplete(0, 1, engine.StepResult{
		Node:  "deleteBooking",
		Error: assert.AnError,
	})

	output := buf.String()
	assert.Contains(t, output, "deleteBooking")
	assert.Contains(t, output, "ERROR:")
}

func TestBatchStreamObserver_NoResponse(t *testing.T) {
	var buf bytes.Buffer
	obs := NewBatchStreamObserver(&buf, "test-plan")

	obs.OnRunStart(1, "strict")
	obs.OnStepComplete(0, 1, engine.StepResult{
		Node: "brokenStep",
	})

	output := buf.String()
	assert.Contains(t, output, "(no response)")
}

func TestBatchStreamObserver_DisplayOutputs(t *testing.T) {
	var buf bytes.Buffer
	obs := NewBatchStreamObserver(&buf, "test-plan")

	obs.OnRunStart(1, "strict")
	obs.OnStepComplete(0, 1, engine.StepResult{
		Node:       "commitReservation",
		StatusCode: 200,
		Response:   &adapter.Response{StatusCode: 200},
		Duration:   100 * time.Millisecond,
		DisplayOutputs: []engine.DisplayOutput{
			{Label: "PNR", Name: "locator", Value: "ABC123"},
		},
	})

	output := buf.String()
	assert.Contains(t, output, "PNR: ABC123")
}

// --- PlanProgressState tests ---

func TestPlanProgressState_Update(t *testing.T) {
	s := &PlanProgressState{PlanName: "test", TotalSteps: 5}
	s.Update(3, "commitReservation")

	snap := s.Snapshot()
	assert.Equal(t, 3, snap.CompletedSteps)
	assert.Equal(t, "commitReservation", snap.CurrentNode)
	assert.False(t, snap.Done)
}

func TestPlanProgressState_Complete(t *testing.T) {
	s := &PlanProgressState{PlanName: "test", TotalSteps: 5}
	s.Complete("passed", 1234)

	snap := s.Snapshot()
	assert.True(t, snap.Done)
	assert.Equal(t, "passed", snap.Outcome)
	assert.Equal(t, int64(1234), snap.DurationMs)
}

func TestPlanProgressState_ConcurrentAccess(t *testing.T) {
	s := &PlanProgressState{PlanName: "test", TotalSteps: 100}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.Update(n, "node")
			_ = s.Snapshot()
		}(i)
	}
	wg.Wait()

	// Should not panic or race
	snap := s.Snapshot()
	assert.Equal(t, "test", snap.PlanName)
}

// --- ProgressRenderer tests ---

func TestProgressRenderer_AddPlan_Waiting(t *testing.T) {
	var buf bytes.Buffer
	r := NewProgressRenderer(&buf, 80)

	state := &PlanProgressState{PlanName: "search-plan", PlanIndex: 0}
	r.AddPlan(state)

	output := buf.String()
	assert.Contains(t, output, "search-plan")
	assert.Contains(t, output, "[waiting...]")
}

func TestProgressRenderer_Refresh_WithProgress(t *testing.T) {
	var buf bytes.Buffer
	r := NewProgressRenderer(&buf, 80)

	state := &PlanProgressState{
		PlanName:       "booking-plan",
		PlanIndex:      0,
		TotalSteps:     10,
		CompletedSteps: 5,
		CurrentNode:    "priceOffers",
		StartTime:      time.Now(),
	}
	r.AddPlan(state)

	// Clear buffer to only see the refresh output
	buf.Reset()
	state.Update(7, "commitReservation")
	r.Refresh()

	output := buf.String()
	// Should contain ANSI escape codes for erase + re-render
	assert.Contains(t, output, "\033[A") // cursor up
	assert.Contains(t, output, "booking-plan")
	assert.Contains(t, output, "7/10")
	assert.Contains(t, output, "commitReservation")
}

func TestProgressRenderer_CompletePlan(t *testing.T) {
	var buf bytes.Buffer
	r := NewProgressRenderer(&buf, 80)

	state := &PlanProgressState{
		PlanName:   "test-plan",
		TotalSteps: 3,
	}
	r.AddPlan(state)
	buf.Reset()

	r.CompletePlan(state, "  test-plan  PASSED (3 steps, 150ms)")

	output := buf.String()
	assert.Contains(t, output, "test-plan  PASSED")

	// After completing the only plan, active list should be empty
	r.mu.Lock()
	assert.Empty(t, r.plans)
	r.mu.Unlock()
}

func TestProgressRenderer_MultiplePlans(t *testing.T) {
	var buf bytes.Buffer
	r := NewProgressRenderer(&buf, 80)

	s1 := &PlanProgressState{PlanName: "plan-a", TotalSteps: 5, CompletedSteps: 2, CurrentNode: "step2"}
	s2 := &PlanProgressState{PlanName: "plan-b", TotalSteps: 3, CompletedSteps: 1, CurrentNode: "step1"}
	r.AddPlan(s1)
	r.AddPlan(s2)

	output := buf.String()
	assert.Contains(t, output, "plan-a")
	assert.Contains(t, output, "plan-b")

	// Complete plan-a, should only show plan-b afterwards
	buf.Reset()
	r.CompletePlan(s1, "  plan-a  PASSED")

	output = buf.String()
	assert.Contains(t, output, "plan-a  PASSED")
	// Remaining progress area should show plan-b
	assert.Contains(t, output, "plan-b")
}

func TestProgressRenderer_Finish(t *testing.T) {
	var buf bytes.Buffer
	r := NewProgressRenderer(&buf, 80)

	state := &PlanProgressState{PlanName: "test", TotalSteps: 1}
	r.AddPlan(state)
	buf.Reset()

	r.Finish()

	output := buf.String()
	// Should contain ANSI erase codes
	assert.Contains(t, output, "\033[A")
}

func TestProgressRenderer_FinishEmpty(t *testing.T) {
	var buf bytes.Buffer
	r := NewProgressRenderer(&buf, 80)

	// Finish with no plans should not produce any output
	r.Finish()
	assert.Empty(t, buf.String())
}

func TestProgressRenderer_FormatPlanLine_Bar(t *testing.T) {
	r := &ProgressRenderer{termWidth: 80, nameWidth: 15}

	line := r.formatPlanLine(PlanProgressState{
		PlanName:       "my-plan",
		TotalSteps:     10,
		CompletedSteps: 5,
		CurrentNode:    "currentStep",
	}, 0)

	assert.Contains(t, line, "my-plan")
	assert.Contains(t, line, "5/10")
	assert.Contains(t, line, "currentStep")
	assert.Contains(t, line, "#")
	assert.Contains(t, line, "-")
}

func TestProgressRenderer_FormatPlanLine_WaitingState(t *testing.T) {
	r := &ProgressRenderer{termWidth: 80, nameWidth: 15}

	line := r.formatPlanLine(PlanProgressState{
		PlanName:   "pending-plan",
		TotalSteps: 0,
	}, 0)

	assert.Contains(t, line, "pending-plan")
	assert.Contains(t, line, "[waiting...]")
}

func TestProgressRenderer_FormatPlanLine_LongName(t *testing.T) {
	r := &ProgressRenderer{termWidth: 80, nameWidth: 15}

	line := r.formatPlanLine(PlanProgressState{
		PlanName:       "very-long-plan-name-that-exceeds-column",
		TotalSteps:     5,
		CompletedSteps: 3,
		CurrentNode:    "node",
	}, 0)

	// Name should be truncated
	assert.Contains(t, line, "~")
	assert.Contains(t, line, "3/5")
}

func TestProgressRenderer_FormatPlanLine_Complete(t *testing.T) {
	r := &ProgressRenderer{termWidth: 80, nameWidth: 10}

	line := r.formatPlanLine(PlanProgressState{
		PlanName:       "doneplan",
		TotalSteps:     4,
		CompletedSteps: 4,
		CurrentNode:    "lastStep",
	}, 0)

	assert.Contains(t, line, "4/4")
	// Bar should be all filled (no unfilled '-' characters in the bar)
	// Extract the bar portion between [ and ]
	start := strings.Index(line, "[")
	end := strings.Index(line, "]")
	if assert.Greater(t, end, start, "should have bar brackets") {
		bar := line[start+1 : end]
		assert.NotContains(t, bar, "-", "bar should be fully filled")
	}
}

func TestProgressRenderer_FormatPlanLine_CountAlignment(t *testing.T) {
	r := &ProgressRenderer{termWidth: 100, nameWidth: 15}

	// Two plans with different total steps: 9 vs 15.
	// denomWidth=2 (width of "15"), so counts render as " 7/9 " and " 6/15".
	denomWidth := 2

	line9 := r.formatPlanLine(PlanProgressState{
		PlanName:       "plan-short",
		TotalSteps:     9,
		CompletedSteps: 7,
		CurrentNode:    "addPayment",
	}, denomWidth)

	line15 := r.formatPlanLine(PlanProgressState{
		PlanName:       "plan-long",
		TotalSteps:     15,
		CompletedSteps: 6,
		CurrentNode:    "addTraveler",
	}, denomWidth)

	// The slashes should be at the same column.
	slashPos9 := strings.Index(line9, "/")
	slashPos15 := strings.Index(line15, "/")
	assert.Equal(t, slashPos9, slashPos15, "slashes should be aligned at the same column")

	// The node names should start at the same column.
	posShort := strings.Index(line9, "addPayment")
	posLong := strings.Index(line15, "addTraveler")
	assert.Equal(t, posShort, posLong, "step names should be aligned at the same column")
}

// --- ParallelProgressObserver tests ---

func TestParallelProgressObserver_OnRunStart(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewProgressRenderer(&buf, 80)
	state := &PlanProgressState{PlanName: "test-plan"}
	renderer.AddPlan(state)

	obs := NewParallelProgressObserver(state, renderer)
	obs.OnRunStart(5, "lean")

	snap := state.Snapshot()
	assert.Equal(t, 5, snap.TotalSteps)
}

func TestParallelProgressObserver_StepUpdatesState(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewProgressRenderer(&buf, 80)
	state := &PlanProgressState{PlanName: "test-plan", TotalSteps: 5}
	renderer.AddPlan(state)

	obs := NewParallelProgressObserver(state, renderer)
	obs.OnStepComplete(2, 5, engine.StepResult{
		Node:       "priceOffers",
		StatusCode: 200,
		Response:   &adapter.Response{StatusCode: 200},
		Duration:   100 * time.Millisecond,
	})

	snap := state.Snapshot()
	assert.Equal(t, 3, snap.CompletedSteps)
	assert.Equal(t, "priceOffers", snap.CurrentNode)
}

func TestParallelProgressObserver_OnRunComplete(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewProgressRenderer(&buf, 80)
	state := &PlanProgressState{
		PlanName:   "test-plan",
		TotalSteps: 3,
		StartTime:  time.Now().Add(-500 * time.Millisecond),
	}
	renderer.AddPlan(state)

	obs := NewParallelProgressObserver(state, renderer)
	obs.OnRunComplete(&engine.RunResult{
		Outcome: engine.OutcomePassed,
		Steps:   []engine.StepResult{{}, {}, {}},
	})

	snap := state.Snapshot()
	assert.True(t, snap.Done)
	assert.Equal(t, "passed", snap.Outcome)
	assert.Greater(t, snap.DurationMs, int64(0))
}

func TestProgressRenderer_AddPlan_NoLineInflation(t *testing.T) {
	var buf bytes.Buffer
	r := NewProgressRenderer(&buf, 80)

	// Add 5 plans in quick succession (simulating parallel goroutines).
	// Before the fix, r.lines would be 1+2+3+4+5 = 15 instead of 5.
	for i := 0; i < 5; i++ {
		s := &PlanProgressState{
			PlanName:   "plan-" + string(rune('a'+i)),
			PlanIndex:  i,
			TotalSteps: 10,
		}
		r.AddPlan(s)
	}

	r.mu.Lock()
	assert.Equal(t, 5, r.lines, "r.lines should equal the number of active plans, not accumulate")
	assert.Equal(t, 5, len(r.plans))
	r.mu.Unlock()
}

// --- Race condition test ---

func TestProgressRenderer_ConcurrentRefresh(t *testing.T) {
	var buf bytes.Buffer
	r := NewProgressRenderer(&buf, 80)

	for i := 0; i < 5; i++ {
		s := &PlanProgressState{
			PlanName:   strings.Repeat("p", i+1),
			TotalSteps: 10,
		}
		r.AddPlan(s)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Refresh()
		}()
	}
	wg.Wait()

	// Should not panic or race — run with -race to verify
}

// --- buildPlanResult tests ---

func TestBuildPlanResult_WithSummary(t *testing.T) {
	res := &runResult{
		summary: &RunSummary{
			Outcome: "passed",
			Summary: SummaryStats{TotalSteps: 3, PassedSteps: 3, DurationMs: 150},
		},
		archivePath: "/tmp/runs/run-123/archive.json",
	}

	br, be := buildPlanResult("test-plan", "", res)
	assert.Equal(t, "test-plan", br.PlanName)
	assert.Equal(t, "passed", br.Outcome)
	assert.Equal(t, 3, br.StepCount)
	assert.Equal(t, 3, br.PassedSteps)
	assert.Equal(t, int64(150), br.DurationMs)
	assert.Equal(t, "/tmp/runs/run-123/archive.json", br.ArchivePath)

	assert.Equal(t, "test-plan", be.PlanName)
	assert.Equal(t, "passed", be.Outcome)
	assert.Equal(t, "run-123", be.RunID)
}

func TestBuildPlanResult_WithPermutation(t *testing.T) {
	res := &runResult{
		summary: &RunSummary{
			Outcome: "passed",
			Summary: SummaryStats{TotalSteps: 2, PassedSteps: 2, DurationMs: 100},
		},
		archivePath: "/tmp/runs/run-456/archive.json",
		layers:      []string{"near-term", "european"},
	}

	br, be := buildPlanResult("test-plan", "european", res)
	assert.Equal(t, "test-plan", br.PlanName)
	assert.Equal(t, "european", br.Permutation)
	assert.Equal(t, []string{"near-term", "european"}, br.Layers)
	assert.Equal(t, "european", be.Permutation)
	assert.Equal(t, []string{"near-term", "european"}, be.Layers)
}

func TestBuildPlanResult_WithError(t *testing.T) {
	res := &runResult{
		err: assert.AnError,
	}

	br, be := buildPlanResult("bad-plan", "", res)
	assert.Equal(t, "error", br.Outcome)
	assert.Contains(t, br.Error, "assert.AnError")
	assert.Equal(t, "error", be.Outcome)
}

func TestBuildPlanResult_NilSummary(t *testing.T) {
	res := &runResult{}

	br, be := buildPlanResult("nil-plan", "", res)
	assert.Equal(t, "error", br.Outcome)
	assert.Equal(t, "error", be.Outcome)
}

// --- formatBatchResultLine tests ---

func TestFormatBatchResultLine(t *testing.T) {
	tests := []struct {
		name     string
		br       BatchRunResult
		contains string
	}{
		{
			name:     "passed",
			br:       BatchRunResult{Outcome: "passed", StepCount: 3, DurationMs: 150},
			contains: "PASSED (3 steps, 150ms)",
		},
		{
			name:     "failed",
			br:       BatchRunResult{Outcome: "failed", StepCount: 2, DurationMs: 200},
			contains: "FAILED (2 steps, 200ms)",
		},
		{
			name:     "error",
			br:       BatchRunResult{Outcome: "error", Error: "something went wrong"},
			contains: "ERROR: something went wrong",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			line := formatBatchResultLine("my-plan", tc.br)
			assert.Contains(t, line, "my-plan")
			assert.Contains(t, line, tc.contains)
		})
	}
}

// helper to create a step for OnStepStart
func stepForNode(node string) plan.Step {
	return plan.Step{Node: node}
}
