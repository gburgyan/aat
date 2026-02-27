package main

import (
	"fmt"
	"io"
	"time"

	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/plan"
)

// CLIProgressObserver implements engine.ProgressObserver for interactive CLI output.
// It prints each step result as it completes, matching the format of printRunSummary.
type CLIProgressObserver struct {
	out   io.Writer
	term  TerminalInfo
	total int // cached from OnRunStart for duration calculation
}

func (o *CLIProgressObserver) OnRunStart(total int, mode string) {
	o.total = total
}

func (o *CLIProgressObserver) OnStepStart(index, total int, step plan.Step) {
	// No-op for CLI v1. Exists for future WebSocket consumers.
}

func (o *CLIProgressObserver) OnStepComplete(index, total int, result engine.StepResult) {
	totalStr := fmt.Sprintf("%d", total)
	width := len(totalStr)
	color := o.term.IsTTY

	// Dynamic node column width (overhead=60 gives ncw=20 at 80-col terminal)
	ncw := nodeColWidth(o.term.Width, 60)
	nodeStr := formatNodeCol(result.Node, ncw, color)

	// indent = "  [" (3) + width + "/" (1) + len(totalStr) + "] " (2)
	indent := 6 + width + len(totalStr)
	prefix := fmt.Sprintf("  [%*d/%s] %s", width, index+1, totalStr, nodeStr)

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
			_, _ = fmt.Fprintf(o.out, "%*s%s: %v\n", indent, "", do.Label, do.Value)
		}
	} else {
		_, _ = fmt.Fprintf(o.out, "%s (no response)\n", prefix)
	}
}

func (o *CLIProgressObserver) OnCleanupStart(total int) {
	if o.term.IsTTY {
		_, _ = fmt.Fprintf(o.out, "\n  %scleanup:%s\n", colorDim, colorReset)
	} else {
		_, _ = fmt.Fprintln(o.out, "\n  cleanup:")
	}
}

func (o *CLIProgressObserver) OnCleanupStepComplete(index, total int, result engine.StepResult) {
	color := o.term.IsTTY
	ncw := nodeColWidth(o.term.Width, 58)
	node := fmt.Sprintf("%-*s", ncw, truncateNode(result.Node, ncw))
	prefix := fmt.Sprintf("    %s", node)

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

func (o *CLIProgressObserver) OnRunComplete(result *engine.RunResult) {
	color := o.term.IsTTY
	_, _ = fmt.Fprintln(o.out)
	total := len(result.Steps)
	switch result.Outcome {
	case engine.OutcomePassed:
		_, _ = fmt.Fprintf(o.out, "%s (%d/%d steps, %s)\n", colorOutcome("PASSED", color), total, total, observerTotalDuration(result))
	case engine.OutcomeFailed:
		_, _ = fmt.Fprintf(o.out, "%s: %s\n", colorOutcome("FAILED", color), outcomeMessage(result))
	case engine.OutcomeError:
		_, _ = fmt.Fprintf(o.out, "%s: %s\n", colorOutcome("ERROR", color), outcomeMessage(result))
	}
}

// OnRetryStart implements RetryNotifier for plan-level retries.
func (o *CLIProgressObserver) OnRetryStart(attempt, maxAttempts int) {
	color := o.term.IsTTY
	label := fmt.Sprintf("retry %d/%d", attempt, maxAttempts)
	if color {
		label = colorYellow + label + colorReset
	}
	_, _ = fmt.Fprintf(o.out, "\n%s\n", label)
}

// observerTotalDuration sums the duration of all steps and cleanup.
func observerTotalDuration(result *engine.RunResult) string {
	var total time.Duration
	for _, s := range result.Steps {
		total += s.Duration
	}
	for _, s := range result.CleanupResults {
		total += s.Duration
	}
	if total < time.Second {
		return fmt.Sprintf("%dms", total.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", total.Seconds())
}
