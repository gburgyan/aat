package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/plan"
)

// CLIProgressObserver implements engine.ProgressObserver for interactive CLI output.
// It prints each step result as it completes.
type CLIProgressObserver struct {
	out   io.Writer
	term  TerminalInfo
	total int // cached from OnRunStart for the ABORTED and STOPPED counts
}

func (o *CLIProgressObserver) OnRunStart(total int) {
	o.total = total
}

func (o *CLIProgressObserver) OnStepStart(index, total int, step plan.Step) {
	// No-op for CLI v1. Exists for future WebSocket consumers.
}

func (o *CLIProgressObserver) OnStepComplete(index, total int, result engine.StepResult) {
	writeStepResult(o.out, "  ", index, total, result, o.term)
}

func (o *CLIProgressObserver) OnCleanupStart(total int) {
	_, _ = fmt.Fprintln(o.out)
	writeCleanupHeader(o.out, "  ", o.term.IsTTY)
}

func (o *CLIProgressObserver) OnCleanupStepComplete(index, total int, result engine.StepResult) {
	writeCleanupResult(o.out, "    ", result, o.term)
}

// OnCleanupSkipped implements engine.CleanupSkipObserver.
func (o *CLIProgressObserver) OnCleanupSkipped(skip engine.CleanupSkip) {
	writeCleanupSkip(o.out, "    ", skip, o.term)
}

func (o *CLIProgressObserver) OnRunComplete(result *engine.RunResult) {
	color := o.term.IsTTY
	_, _ = fmt.Fprintln(o.out)
	total := len(result.Steps)
	planned := max(o.total, total) // steps the run meant to execute, for ABORTED and STOPPED
	elapsed := formatDuration(result.Elapsed())
	switch result.Outcome {
	case engine.OutcomePassed:
		_, _ = fmt.Fprintf(o.out, "%s (%d/%d steps, %s)\n", colorOutcome("PASSED", color), total, total, elapsed)
	case engine.OutcomeFailed:
		_, _ = fmt.Fprintf(o.out, "%s: %s\n", colorOutcome("FAILED", color), outcomeMessage(result))
	case engine.OutcomeError:
		_, _ = fmt.Fprintf(o.out, "%s: %s\n", colorOutcome("ERROR", color), outcomeMessage(result))
	case engine.OutcomeAborted:
		_, _ = fmt.Fprintf(o.out, "%s (%d/%d steps, %s)\n", colorOutcome("ABORTED", color), total, planned, elapsed)
	case engine.OutcomeStopped:
		_, _ = fmt.Fprintf(o.out, "%s at %q (%d/%d steps, %s)\n", colorOutcome("STOPPED", color), result.StoppedAt, total, planned, elapsed)
	}
	writeOASTotal(o.out, "", result.Steps, color)
}

// OnRetryStart implements RetryNotifier for plan-level retries.
func (o *CLIProgressObserver) OnRetryStart(attempt, maxAttempts int) {
	label := colorize(fmt.Sprintf("retry %d/%d", attempt, maxAttempts), colorYellow, o.term.IsTTY)
	_, _ = fmt.Fprintf(o.out, "\n%s\n", label)
}

// writeStepResult prints one completed step: a line with its position, label,
// status, duration, and marks, then its display outputs and failed assertions
// indented beneath the label. lead is the indent before "[i/n]": the plan
// observer uses two spaces and the sequential batch observer four, so both
// print the same lines.
func writeStepResult(w io.Writer, lead string, index, total int, result engine.StepResult, term TerminalInfo) {
	color := term.IsTTY
	totalStr := strconv.Itoa(total)
	label := stepLabel(result, nodeColWidth(term.Width, 60), color)
	prefix := fmt.Sprintf("%s[%*d/%s] %s", lead, len(totalStr), index+1, totalStr, label)
	// Sub-lines start under the label: lead, "[", the index, "/", the total, "] ".
	indent := strings.Repeat(" ", len(lead)+4+2*len(totalStr))

	switch {
	case result.Error != nil:
		_, _ = fmt.Fprintf(w, "%s %s: %s%s\n", prefix, colorize("ERROR", colorRed, color), result.Error, stepMarks(result, color))
	case result.Response != nil:
		duration := colorize(formatDuration(result.Duration), colorDim, color)
		_, _ = fmt.Fprintf(w, "%s %s  %s%s\n", prefix, colorStatus(result.StatusCode, color), duration, stepMarks(result, color))
		for _, do := range result.DisplayOutputs {
			_, _ = fmt.Fprintf(w, "%s%s: %v\n", indent, do.Label, do.Value)
		}
		for _, msg := range failedAssertions(result.Validation) {
			_, _ = fmt.Fprintf(w, "%s%s\n", indent, colorize(msg, colorYellow, color))
		}
		for _, msg := range archive.SelectionTieWarnings(engine.SelectionRecords(result.Selections)) {
			_, _ = fmt.Fprintf(w, "%s%s\n", indent, colorize("warning: "+msg, colorYellow, color))
		}
	default:
		_, _ = fmt.Fprintf(w, "%s (no response)\n", prefix)
	}
}

// writeCleanupHeader prints the line that opens the cleanup section.
func writeCleanupHeader(w io.Writer, lead string, color bool) {
	_, _ = fmt.Fprintf(w, "%s%s\n", lead, colorize("cleanup:", colorDim, color))
}

// writeCleanupResult prints one cleanup step: its label, then its status and
// duration or its error.
func writeCleanupResult(w io.Writer, lead string, result engine.StepResult, term TerminalInfo) {
	color := term.IsTTY
	prefix := lead + stepLabel(result, nodeColWidth(term.Width, 58), false)
	switch {
	case result.Error != nil:
		_, _ = fmt.Fprintf(w, "%s %s: %s\n", prefix, colorize("ERROR", colorRed, color), result.Error)
	case result.Response != nil:
		duration := colorize(formatDuration(result.Duration), colorDim, color)
		_, _ = fmt.Fprintf(w, "%s %s  %s\n", prefix, colorStatus(result.StatusCode, color), duration)
	default:
		_, _ = fmt.Fprintf(w, "%s (no response)\n", prefix)
	}
}

// writeCleanupSkip prints a registered cleanup that did not run because it was
// no longer needed: its node, why, and the step it was for, as in
// "voidPayment skipped: released by capturePayment (for createPayment)".
func writeCleanupSkip(w io.Writer, lead string, skip engine.CleanupSkip, term TerminalInfo) {
	label := stepLabel(engine.StepResult{StepID: skip.Node, Node: skip.Node}, nodeColWidth(term.Width, 58), false)
	reason := archive.CleanupSkipRecord(skip).Description()
	_, _ = fmt.Fprintf(w, "%s%s %s: %s (for %s)\n", lead, label, colorize("skipped", colorDim, term.IsTTY), reason, skip.CleanupFor)
}

// writeOASTotal prints the run's count of OpenAPI violations, when it has any.
func writeOASTotal(w io.Writer, lead string, steps []engine.StepResult, color bool) {
	if n := oasWarningCount(steps); n > 0 {
		_, _ = fmt.Fprintf(w, "%sOAS: %s\n", lead, colorize(fmt.Sprintf("%d warning(s)", n), colorYellow, color))
	}
}

// stepLabel renders a step's name for a progress line, padded to width visible
// columns. It is the step ID, which --stop-after, dependsOn, and the archive
// use, followed by the node in parentheses when the two differ and both fit:
// "checkout (checkoutCart)". A result without a step ID shows its node.
func stepLabel(result engine.StepResult, width int, color bool) string {
	id := resultStepID(result)
	if result.Node == "" || result.Node == id || len(id)+len(result.Node)+3 > width {
		return formatNodeCol(id, width, color)
	}
	suffix := " (" + result.Node + ")"
	pad := strings.Repeat(" ", width-len(id)-len(suffix))
	if !color {
		return id + suffix + pad
	}
	return colorCyan + id + colorReset + colorDim + suffix + colorReset + pad
}

// resultStepID returns the step ID of a result, or its node for a result that
// carries no ID.
func resultStepID(result engine.StepResult) string {
	if result.StepID != "" {
		return result.StepID
	}
	return result.Node
}

// stepMarks renders the notes after a step's status and duration: retries,
// failed assertions, and OpenAPI violations.
func stepMarks(result engine.StepResult, color bool) string {
	marks := ""
	if note := retryNote(result); note != "" {
		marks += "  " + colorize(note, colorYellow, color)
	}
	if result.Validation != nil && !result.Validation.Passed {
		marks += "  " + colorize("ASSERTIONS FAILED", colorYellow, color)
	}
	if result.OASValidation != nil && result.OASValidation.HasErrors() {
		marks += "  " + colorize(fmt.Sprintf("OAS: %d warning(s)", result.OASValidation.ErrorCount()), colorYellow, color)
	}
	if note := oasSkipNote(result); note != "" {
		marks += "  " + colorize(note, colorYellow, color)
	}
	return marks
}

// oasSkipNote names the parts of a step that OAS validation left unvalidated,
// such as a request body type the validator doesn't read.
func oasSkipNote(result engine.StepResult) string {
	v := result.OASValidation
	if v == nil {
		return ""
	}
	request := v.Request != nil && v.Request.Skipped
	response := v.Response != nil && v.Response.Skipped
	switch {
	case request && response:
		return "OAS: not validated"
	case request:
		return "OAS: request not validated"
	case response:
		return "OAS: response not validated"
	}
	return ""
}

// oasWarningCount totals the OpenAPI violations across steps.
func oasWarningCount(steps []engine.StepResult) int {
	n := 0
	for _, step := range steps {
		if step.OASValidation != nil {
			n += step.OASValidation.ErrorCount()
		}
	}
	return n
}

// retryNote summarizes the retries behind a step, such as
// "retried 2x: transient". It is empty when the step did not retry.
func retryNote(result engine.StepResult) string {
	if result.RetryCount == 0 {
		return ""
	}
	var categories []string
	seen := make(map[string]bool)
	for _, c := range result.RetriedOn {
		if name := c.String(); !seen[name] {
			seen[name] = true
			categories = append(categories, name)
		}
	}
	note := fmt.Sprintf("retried %dx", result.RetryCount)
	if len(categories) > 0 {
		note += ": " + strings.Join(categories, ", ")
	}
	return note
}
