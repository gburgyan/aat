package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/plan"
)

// BatchStreamObserver implements engine.ProgressObserver for sequential batch mode.
// It wraps the existing CLIProgressObserver formatting but adds plan header/footer lines.
type BatchStreamObserver struct {
	out      io.Writer
	planName string
	total    int
	mode     string
	start    time.Time
}

// NewBatchStreamObserver creates a BatchStreamObserver for a single plan in a sequential batch.
func NewBatchStreamObserver(out io.Writer, planName string) *BatchStreamObserver {
	return &BatchStreamObserver{
		out:      out,
		planName: planName,
	}
}

func (o *BatchStreamObserver) OnRunStart(total int, mode string) {
	o.total = total
	o.mode = mode
	o.start = time.Now()
	fmt.Fprintf(o.out, "  ── %s (%d steps, mode=%s) ──\n", o.planName, total, mode)
}

func (o *BatchStreamObserver) OnStepStart(index, total int, step plan.Step) {}

func (o *BatchStreamObserver) OnStepComplete(index, total int, result engine.StepResult) {
	totalStr := fmt.Sprintf("%d", total)
	width := len(totalStr)
	indent := 6 + width + len(totalStr)
	prefix := fmt.Sprintf("    [%*d/%s] %-20s", width, index+1, totalStr, result.Node)
	if result.Error != nil {
		if result.RetryCount > 0 {
			fmt.Fprintf(o.out, "%s ERROR [%s] (after %d retries)\n", prefix, errorCategory(result), result.RetryCount)
		} else {
			fmt.Fprintf(o.out, "%s ERROR: %s\n", prefix, result.Error)
		}
	} else if result.Response != nil {
		status := fmt.Sprintf("%d", result.StatusCode)
		validMark := ""
		if result.Validation != nil && !result.Validation.Passed {
			validMark = "  ASSERTIONS FAILED"
		}
		fmt.Fprintf(o.out, "%s %s  %dms%s\n", prefix, status, result.Duration.Milliseconds(), validMark)
		for _, do := range result.DisplayOutputs {
			fmt.Fprintf(o.out, "%*s  %s: %v\n", indent, "", do.Label, do.Value)
		}
	} else {
		fmt.Fprintf(o.out, "%s (no response)\n", prefix)
	}
}

func (o *BatchStreamObserver) OnCleanupStart(total int) {
	fmt.Fprintln(o.out, "    cleanup:")
}

func (o *BatchStreamObserver) OnCleanupStepComplete(index, total int, result engine.StepResult) {
	prefix := fmt.Sprintf("      %-22s", result.Node)
	if result.Error != nil {
		fmt.Fprintf(o.out, "%s ERROR: %s\n", prefix, result.Error)
	} else if result.Response != nil {
		fmt.Fprintf(o.out, "%s %d  %dms\n", prefix, result.StatusCode, result.Duration.Milliseconds())
	} else {
		fmt.Fprintf(o.out, "%s (no response)\n", prefix)
	}
}

func (o *BatchStreamObserver) OnRunComplete(result *engine.RunResult) {
	dur := time.Since(o.start)
	total := len(result.Steps)
	switch result.Outcome {
	case engine.OutcomePassed:
		fmt.Fprintf(o.out, "  ── %s: PASSED (%d steps, %s) ──\n", o.planName, total, formatDuration(dur))
	case engine.OutcomeFailed:
		fmt.Fprintf(o.out, "  ── %s: FAILED (%s) ──\n", o.planName, formatDuration(dur))
	case engine.OutcomeError:
		fmt.Fprintf(o.out, "  ── %s: ERROR (%s) ──\n", o.planName, formatDuration(dur))
	}
}

// PlanProgressState tracks one plan's progress for the parallel renderer.
type PlanProgressState struct {
	mu             sync.Mutex
	PlanName       string
	PlanIndex      int
	TotalSteps     int
	CompletedSteps int
	CurrentNode    string
	StartTime      time.Time
	Done           bool
	Outcome        string
	DurationMs     int64
}

// Update safely updates the plan's progress after a step completes.
func (s *PlanProgressState) Update(completedSteps int, currentNode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CompletedSteps = completedSteps
	s.CurrentNode = currentNode
}

// Complete marks the plan as done with a final outcome.
func (s *PlanProgressState) Complete(outcome string, durationMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Done = true
	s.Outcome = outcome
	s.DurationMs = durationMs
}

// Snapshot returns a copy of the current state for rendering.
func (s *PlanProgressState) Snapshot() PlanProgressState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return PlanProgressState{
		PlanName:       s.PlanName,
		PlanIndex:      s.PlanIndex,
		TotalSteps:     s.TotalSteps,
		CompletedSteps: s.CompletedSteps,
		CurrentNode:    s.CurrentNode,
		StartTime:      s.StartTime,
		Done:           s.Done,
		Outcome:        s.Outcome,
		DurationMs:     s.DurationMs,
	}
}

// ParallelProgressObserver implements engine.ProgressObserver for one plan in parallel mode.
// It updates its PlanProgressState and signals the renderer to refresh.
type ParallelProgressObserver struct {
	state    *PlanProgressState
	renderer *ProgressRenderer
}

// NewParallelProgressObserver creates an observer for a single plan in parallel mode.
func NewParallelProgressObserver(state *PlanProgressState, renderer *ProgressRenderer) *ParallelProgressObserver {
	return &ParallelProgressObserver{state: state, renderer: renderer}
}

func (o *ParallelProgressObserver) OnRunStart(total int, mode string) {
	o.state.mu.Lock()
	o.state.TotalSteps = total
	o.state.StartTime = time.Now()
	o.state.mu.Unlock()
	o.renderer.updateCountWidth(total)
	o.renderer.Refresh()
}

func (o *ParallelProgressObserver) OnStepStart(index, total int, step plan.Step) {
	o.state.Update(index, step.Node)
	o.renderer.Refresh()
}

func (o *ParallelProgressObserver) OnStepComplete(index, total int, result engine.StepResult) {
	o.state.Update(index+1, result.Node)
	o.renderer.Refresh()
}

func (o *ParallelProgressObserver) OnCleanupStart(total int) {}
func (o *ParallelProgressObserver) OnCleanupStepComplete(index, total int, result engine.StepResult) {
}

func (o *ParallelProgressObserver) OnRunComplete(result *engine.RunResult) {
	dur := time.Since(o.state.StartTime)
	o.state.Complete(result.Outcome.String(), dur.Milliseconds())
}

// ProgressRenderer draws Docker-style multi-line progress bars for concurrent plans.
// It uses ANSI escape codes to redraw the progress area in place.
type ProgressRenderer struct {
	mu         sync.Mutex
	out        io.Writer
	termWidth  int
	plans      []*PlanProgressState
	nameWidth  int // max plan name width for alignment
	denomWidth int // max digit width of TotalSteps across plans, for slash-aligned counts
	lines      int // number of lines currently drawn in the progress area
}

// NewProgressRenderer creates a renderer targeting the given writer with terminal width.
func NewProgressRenderer(out io.Writer, termWidth int) *ProgressRenderer {
	return &ProgressRenderer{
		out:       out,
		termWidth: termWidth,
	}
}

// AddPlan registers a new plan for rendering.
func (r *ProgressRenderer) AddPlan(state *PlanProgressState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plans = append(r.plans, state)
	if len(state.PlanName) > r.nameWidth {
		r.nameWidth = len(state.PlanName)
	}
	r.eraseLocked()
	r.renderLocked()
}

// CompletePlan prints a permanent result line for a completed plan and re-renders
// the progress area without it.
func (r *ProgressRenderer) CompletePlan(state *PlanProgressState, resultLine string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Erase the progress area
	r.eraseLocked()

	// Print the permanent result line (scrolls up naturally)
	fmt.Fprintln(r.out, resultLine)

	// Remove the completed plan from active list
	active := r.plans[:0]
	for _, p := range r.plans {
		if p != state {
			active = append(active, p)
		}
	}
	r.plans = active

	// Re-render remaining active plans
	r.renderLocked()
}

// updateCountWidth updates the cached denominator digit width if a new plan's
// total steps are wider. Must be called when TotalSteps becomes known.
func (r *ProgressRenderer) updateCountWidth(totalSteps int) {
	dw := len(fmt.Sprintf("%d", totalSteps))
	r.mu.Lock()
	if dw > r.denomWidth {
		r.denomWidth = dw
	}
	r.mu.Unlock()
}

// Refresh redraws the progress area. Safe for concurrent calls.
func (r *ProgressRenderer) Refresh() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eraseLocked()
	r.renderLocked()
}

// Finish erases the progress area. Call after all plans are done.
func (r *ProgressRenderer) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eraseLocked()
}

// eraseLocked moves the cursor up and clears the progress area lines.
// Must be called with r.mu held.
func (r *ProgressRenderer) eraseLocked() {
	if r.lines == 0 {
		return
	}
	// Move cursor up r.lines and clear each line
	for i := 0; i < r.lines; i++ {
		fmt.Fprintf(r.out, "\033[A\033[2K")
	}
	fmt.Fprintf(r.out, "\r")
	r.lines = 0
}

// renderLocked draws the progress area for all active plans.
// Must be called with r.mu held.
func (r *ProgressRenderer) renderLocked() {
	if len(r.plans) == 0 {
		return
	}

	for _, p := range r.plans {
		snap := p.Snapshot()
		line := r.formatPlanLine(snap, r.denomWidth)
		fmt.Fprintln(r.out, line)
		r.lines++
	}
}

// formatPlanLine renders a single plan's progress bar.
// Format: "  planName  [####------]  3/7  currentNode"
// denomWidth is the digit width of the largest TotalSteps; both numerator and
// denominator are padded to this width so the slash aligns across plans.
func (r *ProgressRenderer) formatPlanLine(snap PlanProgressState, denomWidth int) string {
	nameCol := r.nameWidth
	if nameCol < 10 {
		nameCol = 10
	}
	if nameCol > 30 {
		nameCol = 30
	}

	name := snap.PlanName
	if len(name) > nameCol {
		name = name[:nameCol-1] + "~"
	}

	if snap.TotalSteps == 0 {
		// OnRunStart not called yet
		return fmt.Sprintf("  %-*s  [waiting...]", nameCol, name)
	}

	// Count column: " 3/7 " with both sides padded to denomWidth so slashes align.
	if denomWidth < len(fmt.Sprintf("%d", snap.TotalSteps)) {
		denomWidth = len(fmt.Sprintf("%d", snap.TotalSteps))
	}
	countStr := fmt.Sprintf("%*d/%-*d", denomWidth, snap.CompletedSteps, denomWidth, snap.TotalSteps)

	// Node column — truncate if needed
	nodeCol := 20
	node := snap.CurrentNode
	if len(node) > nodeCol {
		node = node[:nodeCol-1] + "~"
	}

	// Calculate bar width:
	// "  " (2) + name (nameCol) + "  [" (3) + bar + "] " (2) + count + "  " (2) + node
	overhead := 2 + nameCol + 3 + 2 + len(countStr) + 2 + len(node)
	barWidth := r.termWidth - overhead
	if barWidth < 10 {
		barWidth = 10
	}
	if barWidth > 40 {
		barWidth = 40
	}

	filled := 0
	if snap.TotalSteps > 0 {
		filled = snap.CompletedSteps * barWidth / snap.TotalSteps
	}
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("#", filled) + strings.Repeat("-", barWidth-filled)

	return fmt.Sprintf("  %-*s  [%s] %s  %s", nameCol, name, bar, countStr, node)
}
