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
	total int // cached from OnRunStart for duration calculation
}

func (o *CLIProgressObserver) OnRunStart(total int, mode string) {
	o.total = total
}

func (o *CLIProgressObserver) OnStepStart(index, total int, step plan.Step) {
	// No-op for CLI v1. Exists for future WebSocket consumers.
}

func (o *CLIProgressObserver) OnStepComplete(index, total int, result engine.StepResult) {
	prefix := fmt.Sprintf("  [%d/%d] %-20s", index+1, total, result.Node)
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
			fmt.Fprintf(o.out, "        %s: %v\n", do.Label, do.Value)
		}
	} else {
		fmt.Fprintf(o.out, "%s (no response)\n", prefix)
	}
}

func (o *CLIProgressObserver) OnCleanupStart(total int) {
	fmt.Fprintln(o.out, "\n  cleanup:")
}

func (o *CLIProgressObserver) OnCleanupStepComplete(index, total int, result engine.StepResult) {
	prefix := fmt.Sprintf("    %-22s", result.Node)
	if result.Error != nil {
		fmt.Fprintf(o.out, "%s ERROR: %s\n", prefix, result.Error)
	} else if result.Response != nil {
		fmt.Fprintf(o.out, "%s %d  %dms\n", prefix, result.StatusCode, result.Duration.Milliseconds())
	} else {
		fmt.Fprintf(o.out, "%s (no response)\n", prefix)
	}
}

func (o *CLIProgressObserver) OnRunComplete(result *engine.RunResult) {
	fmt.Fprintln(o.out)
	total := len(result.Steps)
	switch result.Outcome {
	case engine.OutcomePassed:
		fmt.Fprintf(o.out, "PASSED (%d/%d steps, %s)\n", total, total, observerTotalDuration(result))
	case engine.OutcomeFailed:
		fmt.Fprintf(o.out, "FAILED: %s\n", outcomeMessage(result))
	case engine.OutcomeError:
		fmt.Fprintf(o.out, "ERROR: %s\n", outcomeMessage(result))
	}
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
