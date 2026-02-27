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
	out       io.Writer
	planName  string
	term      TerminalInfo
	planIndex int // 0-based index into the batch
	planTotal int // total plans in the batch
	total     int
	mode      string
	start     time.Time
}

// NewBatchStreamObserver creates a BatchStreamObserver for a single plan in a sequential batch.
func NewBatchStreamObserver(out io.Writer, planName string, term TerminalInfo, planIndex, planTotal int) *BatchStreamObserver {
	return &BatchStreamObserver{
		out:       out,
		planName:  planName,
		term:      term,
		planIndex: planIndex,
		planTotal: planTotal,
	}
}

func (o *BatchStreamObserver) OnRunStart(total int, mode string) {
	o.total = total
	o.mode = mode
	o.start = time.Now()
	color := o.term.IsTTY
	name := o.planName
	if color {
		name = colorCyan + name + colorReset
	}
	counter := fmt.Sprintf(" [plan %d/%d]", o.planIndex+1, o.planTotal)
	_, _ = fmt.Fprintf(o.out, "  ── %s (%d steps, mode=%s)%s ──\n", name, total, mode, counter)
}

func (o *BatchStreamObserver) OnStepStart(index, total int, step plan.Step) {}

func (o *BatchStreamObserver) OnStepComplete(index, total int, result engine.StepResult) {
	totalStr := fmt.Sprintf("%d", total)
	width := len(totalStr)
	color := o.term.IsTTY
	ncw := nodeColWidth(o.term.Width, 60)
	nodeStr := formatNodeCol(result.Node, ncw, color)
	indent := 6 + width + len(totalStr)
	prefix := fmt.Sprintf("    [%*d/%s] %s", width, index+1, totalStr, nodeStr)

	if result.Error != nil {
		errLabel := colorize("ERROR", colorRed, color)
		if result.RetryCount > 0 {
			cat := errorCategory(result)
			if o.term.Width > 100 && result.StatusCode > 0 {
				_, _ = fmt.Fprintf(o.out, "%s %s [%s] (retried %dx, last status %d)\n", prefix, errLabel, cat, result.RetryCount, result.StatusCode)
			} else {
				_, _ = fmt.Fprintf(o.out, "%s %s [%s] (after %d retries)\n", prefix, errLabel, cat, result.RetryCount)
			}
		} else {
			_, _ = fmt.Fprintf(o.out, "%s %s: %s\n", prefix, errLabel, result.Error)
		}
	} else if result.Response != nil {
		status := colorStatus(result.StatusCode, color)
		durStr := fmt.Sprintf("%dms", result.Duration.Milliseconds())
		if color {
			durStr = colorDim + durStr + colorReset
		}
		validMark := ""
		if result.Validation != nil && !result.Validation.Passed {
			validMark = "  " + colorize("ASSERTIONS FAILED", colorYellow, color)
		}
		_, _ = fmt.Fprintf(o.out, "%s %s  %s%s\n", prefix, status, durStr, validMark)
		for _, do := range result.DisplayOutputs {
			_, _ = fmt.Fprintf(o.out, "%*s  %s: %v\n", indent, "", do.Label, do.Value)
		}
	} else {
		_, _ = fmt.Fprintf(o.out, "%s (no response)\n", prefix)
	}
}

func (o *BatchStreamObserver) OnCleanupStart(total int) {
	if o.term.IsTTY {
		_, _ = fmt.Fprintf(o.out, "    %scleanup:%s\n", colorDim, colorReset)
	} else {
		_, _ = fmt.Fprintln(o.out, "    cleanup:")
	}
}

func (o *BatchStreamObserver) OnCleanupStepComplete(index, total int, result engine.StepResult) {
	color := o.term.IsTTY
	ncw := nodeColWidth(o.term.Width, 58)
	node := fmt.Sprintf("%-*s", ncw, truncateNode(result.Node, ncw))
	prefix := fmt.Sprintf("      %s", node)

	if result.Error != nil {
		errLabel := colorize("ERROR", colorRed, color)
		_, _ = fmt.Fprintf(o.out, "%s %s: %s\n", prefix, errLabel, result.Error)
	} else if result.Response != nil {
		status := colorStatus(result.StatusCode, color)
		durStr := fmt.Sprintf("%dms", result.Duration.Milliseconds())
		if color {
			durStr = colorDim + durStr + colorReset
		}
		_, _ = fmt.Fprintf(o.out, "%s %s  %s\n", prefix, status, durStr)
	} else {
		_, _ = fmt.Fprintf(o.out, "%s (no response)\n", prefix)
	}
}

func (o *BatchStreamObserver) OnRunComplete(result *engine.RunResult) {
	dur := time.Since(o.start)
	total := len(result.Steps)
	color := o.term.IsTTY
	name := o.planName
	if color {
		name = colorCyan + name + colorReset
	}
	counter := fmt.Sprintf(" [plan %d/%d]", o.planIndex+1, o.planTotal)
	switch result.Outcome {
	case engine.OutcomePassed:
		_, _ = fmt.Fprintf(o.out, "  ── %s: %s (%d steps, %s)%s ──\n", name, colorOutcome("PASSED", color), total, formatDuration(dur), counter)
	case engine.OutcomeFailed:
		_, _ = fmt.Fprintf(o.out, "  ── %s: %s (%s)%s ──\n", name, colorOutcome("FAILED", color), formatDuration(dur), counter)
	case engine.OutcomeError:
		_, _ = fmt.Fprintf(o.out, "  ── %s: %s (%s)%s ──\n", name, colorOutcome("ERROR", color), formatDuration(dur), counter)
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
	color      bool
	planTotal  int // total plans in the batch
	completed  int // number of plans completed so far
	plans      []*PlanProgressState
	nameWidth  int // max plan name width for alignment
	denomWidth int // max digit width of TotalSteps across plans, for slash-aligned counts
	lines      int // number of lines currently drawn in the progress area
}

// NewProgressRenderer creates a renderer targeting the given writer with terminal width.
func NewProgressRenderer(out io.Writer, termWidth int, color bool, planTotal int) *ProgressRenderer {
	return &ProgressRenderer{
		out:       out,
		termWidth: termWidth,
		color:     color,
		planTotal: planTotal,
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

	r.completed++

	// Erase the progress area
	r.eraseLocked()

	// Print the permanent result line (scrolls up naturally)
	_, _ = fmt.Fprintln(r.out, resultLine)

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
		_, _ = fmt.Fprintf(r.out, "\033[A\033[2K")
	}
	_, _ = fmt.Fprintf(r.out, "\r")
	r.lines = 0
}

// renderLocked draws the progress area for all active plans.
// Must be called with r.mu held.
func (r *ProgressRenderer) renderLocked() {
	if len(r.plans) == 0 {
		return
	}

	// Status bar: "Batch: 5/82 completed, 20 running"
	if r.planTotal > 0 {
		status := fmt.Sprintf("Batch: %d/%d completed, %d running", r.completed, r.planTotal, len(r.plans))
		if r.color {
			status = colorDim + status + colorReset
		}
		_, _ = fmt.Fprintln(r.out, status)
		r.lines++
	}

	for _, p := range r.plans {
		snap := p.Snapshot()
		line := r.formatPlanLine(&snap, r.denomWidth)
		_, _ = fmt.Fprintln(r.out, line)
		r.lines++
	}
}

// formatPlanLine renders a single plan's progress bar.
// Format: "  planName  [####------]  3/7  currentNode"
// denomWidth is the digit width of the largest TotalSteps; both numerator and
// denominator are padded to this width so the slash aligns across plans.
func (r *ProgressRenderer) formatPlanLine(snap *PlanProgressState, denomWidth int) string {
	tw := r.termWidth
	if tw == 0 {
		tw = 80
	}

	// Name column: accommodate actual plan names, scaling cap with terminal width.
	nameCol := r.nameWidth
	if nameCol < 10 {
		nameCol = 10
	}
	maxName := tw / 3
	if maxName < 30 {
		maxName = 30
	}
	if nameCol > maxName {
		nameCol = maxName
	}

	nameStr := formatNodeCol(snap.PlanName, nameCol, r.color)

	if snap.TotalSteps == 0 {
		// OnRunStart not called yet
		return fmt.Sprintf("  %s  [waiting...]", nameStr)
	}

	// Count column: " 3/7 " with both sides padded to denomWidth so slashes align.
	if denomWidth < len(fmt.Sprintf("%d", snap.TotalSteps)) {
		denomWidth = len(fmt.Sprintf("%d", snap.TotalSteps))
	}
	countStr := fmt.Sprintf("%*d/%-*d", denomWidth, snap.CompletedSteps, denomWidth, snap.TotalSteps)

	// Node column: scales with terminal width.
	nodeCol := tw / 6
	if nodeCol < 20 {
		nodeCol = 20
	}
	if nodeCol > 30 {
		nodeCol = 30
	}
	rawNode := truncateNode(snap.CurrentNode, nodeCol)

	// Bar fills remaining space, capped at 1/3 of terminal width.
	fixedOverhead := 2 + nameCol + 3 + 2 + len(countStr) + 2 + nodeCol
	barWidth := tw - fixedOverhead
	if barWidth < 10 {
		barWidth = 10
	}
	maxBar := tw / 3
	if maxBar < 30 {
		maxBar = 30
	}
	if barWidth > maxBar {
		barWidth = maxBar
	}

	filled := 0
	if snap.TotalSteps > 0 {
		filled = snap.CompletedSteps * barWidth / snap.TotalSteps
	}
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("#", filled) + strings.Repeat("-", barWidth-filled)

	// Color the current node name if enabled
	displayNode := rawNode
	if r.color {
		displayNode = colorCyan + rawNode + colorReset
	}

	return fmt.Sprintf("  %s  [%s] %s  %s", nameStr, bar, countStr, displayNode)
}
