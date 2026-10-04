package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/gburgyan/aat/engine"
)

// fuzzNote marks a fuzz step's progress line with its mode and finding: red
// when the finding fails the run, yellow for a warning, dim when the response
// was what the case called for.
func fuzzNote(result engine.StepResult, color bool) string {
	f := result.Fuzz
	if f == nil {
		return ""
	}
	judged := f.Case.Mode
	if f.JudgedAs != "" && f.JudgedAs != judged {
		judged += "→" + f.JudgedAs
	}
	label := "fuzz " + f.Case.ID + " (" + judged + ")"
	if f.Setup == engine.SetupRebuilt {
		label += ", setup rebuilt"
	}
	mode := colorize(label, colorDim, color)
	switch {
	case f.Fails:
		return mode + " " + colorize(strings.ToUpper(f.Finding), colorRed, color)
	case f.Finding != "":
		return mode + " " + colorize(f.Finding, colorYellow, color)
	default:
		return mode
	}
}

// quietSetupCopy reports whether a progress line would be a fuzz case's
// copy of a setup step that passed: those lines are noise, and the fuzz
// summary counts them. A copy that failed is shown, since its case wasn't
// sent.
func quietSetupCopy(r engine.StepResult) bool {
	return r.FuzzSetup != "" && !r.FuzzSetupFailed
}

// countedSteps counts a run's steps as its progress lines number them: every
// step but the copies of setup steps made for fuzz cases.
func countedSteps(steps []engine.StepResult) int {
	n := 0
	for _, s := range steps {
		if s.FuzzSetup == "" {
			n++
		}
	}
	return n
}

// writeFuzzWarnings prints the problems with how a run fuzzed.
func writeFuzzWarnings(w io.Writer, lead string, warnings []string, color bool) {
	for _, msg := range warnings {
		_, _ = fmt.Fprintf(w, "%s%s\n", lead, colorize("warning: "+msg, colorYellow, color))
	}
}

// writeFuzzSummary prints a run's fuzz cases after its outcome: the count by
// finding, then each case with a finding, failing ones first, with the value
// it sent and the status it got back.
func writeFuzzSummary(w io.Writer, lead string, steps []engine.StepResult, color bool) {
	fs := engine.SummarizeFuzz(steps)
	if fs == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "%sFuzz: %s\n", lead, engine.DescribeFuzz(fs))

	for _, failing := range []bool{true, false} {
		for _, s := range steps {
			f := s.Fuzz
			if f == nil || f.Finding == "" || f.Fails != failing {
				continue
			}
			label := colorize(fmt.Sprintf("%-16s", f.Finding), colorYellow, color)
			if failing {
				label = colorize(fmt.Sprintf("%-16s", f.Finding), colorRed, color)
			}
			got := "no response"
			switch {
			case f.Finding == engine.FindingNotSent && s.Error != nil:
				got = "not sent: " + strings.TrimPrefix(s.Error.Error(), "not sent: ")
			case s.Response != nil:
				got = s.StatusText()
			}
			_, _ = fmt.Fprintf(w, "%s  %s %s  %s -> %s\n", lead, label, f.Case.ID, f.Case.Describe(), got)
		}
	}
}
