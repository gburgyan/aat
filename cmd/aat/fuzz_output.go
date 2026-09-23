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
	mode := colorize("fuzz "+f.Case.ID+" ("+judged+")", colorDim, color)
	switch {
	case f.Fails:
		return mode + " " + colorize(strings.ToUpper(f.Finding), colorRed, color)
	case f.Finding != "":
		return mode + " " + colorize(f.Finding, colorYellow, color)
	default:
		return mode
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
	var parts []string
	if n := fs.Findings[""]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d as expected", n))
	}
	for _, finding := range engine.AllFindings {
		if n := fs.Findings[finding]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, finding))
		}
	}
	_, _ = fmt.Fprintf(w, "%sFuzz: %s: %s\n", lead, pluralize(fs.Cases, "case"), strings.Join(parts, ", "))

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
			if s.Response != nil {
				got = engine.ActualStatusText(s.Response, s.StatusCode)
			}
			_, _ = fmt.Fprintf(w, "%s  %s %s  %s -> %s\n", lead, label, f.Case.ID, f.Case.Describe(), got)
		}
	}
}
