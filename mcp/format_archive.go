package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gburgyan/aat/archive"
)

// formatArchiveListEntry returns a one-line Markdown summary for an archive.
func formatArchiveListEntry(a *archive.Archive) string {
	stepCount := len(a.Steps)
	dur := totalDurationMs(a)
	return fmt.Sprintf("**%s** — %s — %s — %s (%d steps)",
		a.Metadata.RunID,
		a.Metadata.Timestamp.Format("2006-01-02 15:04:05"),
		a.Result.Outcome,
		formatDurationMs(dur),
		stepCount,
	)
}

// formatArchiveDetail returns a full Markdown view of an archive.
func formatArchiveDetail(a *archive.Archive) string {
	var b strings.Builder

	// Header
	fmt.Fprintf(&b, "# Run: %s\n\n", a.Metadata.RunID)
	fmt.Fprintf(&b, "- **Timestamp:** %s\n", a.Metadata.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "- **Outcome:** %s\n", a.Result.Outcome)
	fmt.Fprintf(&b, "- **Duration:** %s\n", formatDurationMs(totalDurationMs(a)))
	fmt.Fprintf(&b, "- **Environment:** %s\n", a.Metadata.Environment)
	if a.Result.Error != "" {
		fmt.Fprintf(&b, "- **Error:** %s\n", a.Result.Error)
	}

	// Plan goal
	if a.Metadata.Plan != nil && a.Metadata.Plan.Intent.Goal != "" {
		fmt.Fprintf(&b, "\n## Goal\n\n%s\n", a.Metadata.Plan.Intent.Goal)
	}

	// Steps
	total := len(a.Steps)
	if total > 0 {
		b.WriteString("\n## Steps\n\n")
		for i, step := range a.Steps {
			b.WriteString(formatStepRecord(&step, i+1, total))
			b.WriteString("\n")
		}
	}

	// Cleanup
	if len(a.Cleanup) > 0 {
		b.WriteString("## Cleanup\n\n")
		for i, step := range a.Cleanup {
			b.WriteString(formatStepRecord(&step, i+1, len(a.Cleanup)))
			b.WriteString("\n")
		}
	}

	return b.String()
}

// formatStepRecord formats a single step as a Markdown block.
func formatStepRecord(s *archive.StepRecord, idx, total int) string {
	var b strings.Builder

	// Header
	fmt.Fprintf(&b, "### [%d/%d] %s\n\n", idx, total, s.Node)

	// Status line
	dur := formatDurationMs(s.DurationMs)
	if s.Request != nil && s.Response != nil {
		fmt.Fprintf(&b, "**%s %s** → %d (%s)\n\n",
			s.Request.Method, s.Request.URL, s.Response.Status, dur)
	} else if s.Error != "" {
		fmt.Fprintf(&b, "**ERROR** (%s): %s\n\n", dur, s.Error)
	} else {
		fmt.Fprintf(&b, "**Status:** %s\n\n", dur)
	}

	// Error classification
	if s.ErrorClass != nil {
		fmt.Fprintf(&b, "**Error Class:** %s — %s (action: %s)\n\n",
			s.ErrorClass.Category, s.ErrorClass.Detail, s.ErrorClass.Action)
	}

	// Response body error
	if s.ResponseBodyError != nil {
		fmt.Fprintf(&b, "**Response Body Error:** detected at `%s` (rule: %s)\n",
			s.ResponseBodyError.RulePath, s.ResponseBodyError.Rule)
		if s.ResponseBodyError.Message != "" {
			fmt.Fprintf(&b, "  Message: %s\n", s.ResponseBodyError.Message)
		}
		if s.ResponseBodyError.Code != "" {
			fmt.Fprintf(&b, "  Code: %s\n", s.ResponseBodyError.Code)
		}
		if s.ResponseBodyError.Category != "" {
			fmt.Fprintf(&b, "  Category: %s\n", s.ResponseBodyError.Category)
		}
		b.WriteString("\n")
	}

	// ExpectFailure
	if s.ExpectFailure != nil {
		result := "PASSED"
		if !s.ExpectFailure.Passed {
			result = "FAILED"
		}
		fmt.Fprintf(&b, "**Expect Failure:** %s (expected %v, got %d)\n\n",
			result, s.ExpectFailure.Expected, s.ExpectFailure.Actual)
	}

	// Request body (truncated)
	if s.Request != nil && len(s.Request.Body) > 0 {
		b.WriteString("**Request Body:**\n```json\n")
		b.WriteString(truncateBody(s.Request.Body, 2000))
		b.WriteString("\n```\n\n")
	}

	// Response body (truncated)
	if s.Response != nil && len(s.Response.Body) > 0 {
		b.WriteString("**Response Body:**\n```json\n")
		b.WriteString(truncateBody(s.Response.Body, 2000))
		b.WriteString("\n```\n\n")
	}

	// Validation assertions
	if s.Validation != nil {
		b.WriteString("**Assertions:**\n\n")
		b.WriteString("| Type | Passed | Message |\n|------|--------|---------|\n")
		for _, ar := range s.Validation.Results {
			passed := "yes"
			if !ar.Passed {
				passed = "**NO**"
			}
			fmt.Fprintf(&b, "| %s | %s | %s |\n", ar.Type, passed, ar.Message)
		}
		b.WriteString("\n")
	}

	// Selections
	if len(s.Selections) > 0 {
		b.WriteString("**Selections:**\n\n")
		for _, sel := range s.Selections {
			fmt.Fprintf(&b, "- %s: %s.%s[%d] (strategy: %s, source: %d items",
				sel.InputName, sel.SourceNode, sel.SourceField, sel.SelectedIndex, sel.Strategy, sel.SourceSize)
			if sel.FilterExpr != "" {
				fmt.Fprintf(&b, ", filter: `%s` → %d", sel.FilterExpr, sel.FilteredSize)
			}
			b.WriteString(")\n")
		}
		b.WriteString("\n")
	}

	// Value resolutions
	if len(s.Resolutions) > 0 {
		b.WriteString("**Value Resolutions:**\n\n")
		for _, r := range s.Resolutions {
			fmt.Fprintf(&b, "- **%s**: source=%s", r.InputName, r.Source)
			if r.FromStep != "" {
				fmt.Fprintf(&b, " (from %s.%s)", r.FromStep, r.FromOutput)
			}
			if r.Expression != "" {
				fmt.Fprintf(&b, " expr=`%s`", r.Expression)
			}
			if r.Constraint != "" {
				ok := "passed"
				if r.ConstraintOK != nil && !*r.ConstraintOK {
					ok = "failed"
				}
				fmt.Fprintf(&b, " constraint=`%s` (%s)", r.Constraint, ok)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Retry count
	if s.RetryCount > 0 {
		fmt.Fprintf(&b, "**Retries:** %d\n\n", s.RetryCount)
	}

	return b.String()
}

// formatFailureAnalysis returns a failure-focused Markdown analysis of an archive.
func formatFailureAnalysis(a *archive.Archive) string {
	if a.Result.Outcome == "passed" {
		return "Run passed — use `inspect_archive` for details."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Failure Analysis: %s\n\n", a.Metadata.RunID)
	fmt.Fprintf(&b, "**Outcome:** %s\n", a.Result.Outcome)
	if a.Result.Error != "" {
		fmt.Fprintf(&b, "**Error:** %s\n", a.Result.Error)
	}
	b.WriteString("\n")

	// Identify failed steps
	failedSteps := findFailedSteps(a.Steps)
	if len(failedSteps) == 0 {
		b.WriteString("No individual step failures found. The error may be at the plan/infrastructure level.\n")
		return b.String()
	}

	b.WriteString("## Failed Steps\n\n")
	for _, fs := range failedSteps {
		fmt.Fprintf(&b, "### %s\n\n", fs.Node)

		if fs.ErrorClass != nil {
			fmt.Fprintf(&b, "**Category:** %s\n", fs.ErrorClass.Category)
			fmt.Fprintf(&b, "**Detail:** %s\n", fs.ErrorClass.Detail)
			fmt.Fprintf(&b, "**Action:** %s\n\n", fs.ErrorClass.Action)
		}

		if fs.Response != nil {
			fmt.Fprintf(&b, "**HTTP Status:** %d\n", fs.Response.Status)
			if len(fs.Response.Body) > 0 {
				b.WriteString("**Response Excerpt:**\n```json\n")
				b.WriteString(truncateBody(fs.Response.Body, 500))
				b.WriteString("\n```\n\n")
			}
		}

		if fs.Error != "" {
			fmt.Fprintf(&b, "**Error:** %s\n\n", fs.Error)
		}

		// Validation failures
		if fs.Validation != nil && !fs.Validation.Passed {
			b.WriteString("**Failed Assertions:**\n")
			for _, ar := range fs.Validation.Results {
				if !ar.Passed {
					fmt.Fprintf(&b, "- %s: %s\n", ar.Type, ar.Message)
				}
			}
			b.WriteString("\n")
		}

		// Retries
		if fs.RetryCount > 0 {
			fmt.Fprintf(&b, "**Retries:** %d\n\n", fs.RetryCount)
		}
	}

	// Suggestions
	b.WriteString("## Suggested Next Steps\n\n")
	categories := collectFailureCategories(failedSteps)
	seen := make(map[string]bool)
	for _, cat := range categories {
		suggestion := suggestNextSteps(cat)
		if suggestion != "" && !seen[cat] {
			seen[cat] = true
			fmt.Fprintf(&b, "- **%s:** %s\n", cat, suggestion)
		}
	}
	if len(seen) == 0 {
		b.WriteString("- Review the failed step details above for clues\n")
		b.WriteString("- Use `inspect_archive` to see the full request/response details\n")
	}

	return b.String()
}

// formatArchiveDiff returns a side-by-side comparison of two archives.
func formatArchiveDiff(a1, a2 *archive.Archive) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Archive Diff\n\n")

	// Overview
	b.WriteString("## Overview\n\n")
	b.WriteString("| | Run 1 | Run 2 |\n|---|---|---|\n")
	fmt.Fprintf(&b, "| **Run ID** | %s | %s |\n", a1.Metadata.RunID, a2.Metadata.RunID)
	fmt.Fprintf(&b, "| **Timestamp** | %s | %s |\n",
		a1.Metadata.Timestamp.Format("2006-01-02 15:04:05"),
		a2.Metadata.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "| **Outcome** | %s | %s |\n", a1.Result.Outcome, a2.Result.Outcome)
	fmt.Fprintf(&b, "| **Duration** | %s | %s |\n",
		formatDurationMs(totalDurationMs(a1)),
		formatDurationMs(totalDurationMs(a2)))
	fmt.Fprintf(&b, "| **Steps** | %d | %d |\n", len(a1.Steps), len(a2.Steps))
	b.WriteString("\n")

	// Match steps by node name (position-based for duplicates)
	pairs := matchSteps(a1.Steps, a2.Steps)

	if len(pairs) > 0 {
		b.WriteString("## Step Comparison\n\n")
		b.WriteString("| Step | Run 1 Status | Run 2 Status | Run 1 Duration | Run 2 Duration | Delta |\n")
		b.WriteString("|------|-------------|-------------|---------------|---------------|-------|\n")

		for _, p := range pairs {
			node := p.node
			s1Status := "-"
			s2Status := "-"
			s1Dur := "-"
			s2Dur := "-"
			delta := "-"

			if p.s1 != nil {
				if p.s1.Response != nil {
					s1Status = fmt.Sprintf("%d", p.s1.Response.Status)
				} else if p.s1.Error != "" {
					s1Status = "ERROR"
				}
				s1Dur = formatDurationMs(p.s1.DurationMs)
			}
			if p.s2 != nil {
				if p.s2.Response != nil {
					s2Status = fmt.Sprintf("%d", p.s2.Response.Status)
				} else if p.s2.Error != "" {
					s2Status = "ERROR"
				}
				s2Dur = formatDurationMs(p.s2.DurationMs)
			}
			if p.s1 != nil && p.s2 != nil {
				diff := p.s2.DurationMs - p.s1.DurationMs
				sign := "+"
				if diff < 0 {
					sign = ""
				}
				delta = fmt.Sprintf("%s%s", sign, formatDurationMs(diff))
			}

			label := node
			if p.s1 == nil {
				label += " *(added)*"
			} else if p.s2 == nil {
				label += " *(removed)*"
			}

			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
				label, s1Status, s2Status, s1Dur, s2Dur, delta)
		}
		b.WriteString("\n")
	}

	// Output key differences for matched steps
	b.WriteString("## Differences\n\n")
	hasDiffs := false
	for _, p := range pairs {
		if p.s1 == nil || p.s2 == nil {
			continue
		}
		diffs := compareStepOutputs(p.s1, p.s2)
		if len(diffs) > 0 {
			hasDiffs = true
			fmt.Fprintf(&b, "### %s\n\n", p.node)
			for _, d := range diffs {
				fmt.Fprintf(&b, "- %s\n", d)
			}
			b.WriteString("\n")
		}
	}
	if !hasDiffs {
		b.WriteString("No output differences found between matched steps.\n")
	}

	return b.String()
}

// truncateBody pretty-prints JSON and truncates to maxLen characters.
func truncateBody(body json.RawMessage, maxLen int) string {
	if len(body) == 0 {
		return ""
	}

	// Try to pretty-print
	var parsed any
	if err := json.Unmarshal(body, &parsed); err == nil {
		pretty, err := json.MarshalIndent(parsed, "", "  ")
		if err == nil {
			s := string(pretty)
			if len(s) > maxLen {
				return s[:maxLen] + "\n... (truncated)"
			}
			return s
		}
	}

	// Fallback: raw string
	s := string(body)
	if len(s) > maxLen {
		return s[:maxLen] + "\n... (truncated)"
	}
	return s
}

// formatDurationMs formats a duration in milliseconds to a human-readable string.
func formatDurationMs(ms int64) string {
	if ms < 0 {
		return fmt.Sprintf("-%s", formatDurationMs(-ms))
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000.0)
}

// suggestNextSteps returns actionable suggestions based on an error category.
func suggestNextSteps(category string) string {
	switch category {
	case "auth":
		return "Check credentials and authentication configuration. Tokens may have expired."
	case "client":
		return "Review request inputs and plan values. The API rejected the request."
	case "server":
		return "The API returned a server error. Retry, or check if the API is healthy."
	case "timeout":
		return "Request timed out. Consider increasing the timeout or checking API latency."
	case "validation":
		return "Response assertions failed. Review the expected values in the plan."
	case "network":
		return "Network error. Check connectivity and the API base URL."
	case "response_error":
		return "The API returned 200 OK but the response body contains an error. Check request inputs and API documentation."
	default:
		return ""
	}
}

// --- internal helpers ---

// totalDurationMs sums step durations across all steps and cleanup.
func totalDurationMs(a *archive.Archive) int64 {
	var total int64
	for _, s := range a.Steps {
		total += s.DurationMs
	}
	for _, s := range a.Cleanup {
		total += s.DurationMs
	}
	return total
}

// findFailedSteps returns steps that have errors, status >= 400, or failed validation.
func findFailedSteps(steps []archive.StepRecord) []archive.StepRecord {
	var failed []archive.StepRecord
	for _, s := range steps {
		isFailed := false
		if s.Error != "" {
			isFailed = true
		}
		if s.Response != nil && s.Response.Status >= 400 {
			// Skip expectFailure steps that passed
			if s.ExpectFailure != nil && s.ExpectFailure.Passed {
				continue
			}
			isFailed = true
		}
		if s.Validation != nil && !s.Validation.Passed {
			isFailed = true
		}
		if s.ResponseBodyError != nil {
			isFailed = true
		}
		if isFailed {
			failed = append(failed, s)
		}
	}
	return failed
}

// collectFailureCategories extracts unique error categories from failed steps.
func collectFailureCategories(steps []archive.StepRecord) []string {
	var cats []string
	for _, s := range steps {
		if s.ErrorClass != nil {
			cats = append(cats, s.ErrorClass.Category)
		} else if s.Response != nil {
			switch {
			case s.Response.Status == 401 || s.Response.Status == 403:
				cats = append(cats, "auth")
			case s.Response.Status >= 400 && s.Response.Status < 500:
				cats = append(cats, "client")
			case s.Response.Status >= 500:
				cats = append(cats, "server")
			}
		}
		if s.Validation != nil && !s.Validation.Passed {
			cats = append(cats, "validation")
		}
		if s.ResponseBodyError != nil {
			cats = append(cats, "response_error")
		}
		if s.Error != "" && s.ErrorClass == nil && s.Response == nil {
			cats = append(cats, "network")
		}
	}
	return cats
}

// stepPair holds matched steps from two archives for comparison.
type stepPair struct {
	node string
	s1   *archive.StepRecord
	s2   *archive.StepRecord
}

// matchSteps matches steps from two archives by node name, preserving order.
// Duplicate node names are matched positionally.
func matchSteps(steps1, steps2 []archive.StepRecord) []stepPair {
	// Build index for steps2: node name → list of unmatched indices
	type entry struct {
		indices []int
		next    int
	}
	idx2 := make(map[string]*entry)
	for i, s := range steps2 {
		e, ok := idx2[s.Node]
		if !ok {
			e = &entry{}
			idx2[s.Node] = e
		}
		e.indices = append(e.indices, i)
	}

	matched2 := make(map[int]bool)
	var pairs []stepPair

	// Match each step from steps1
	for i := range steps1 {
		s1 := &steps1[i]
		if e, ok := idx2[s1.Node]; ok && e.next < len(e.indices) {
			j := e.indices[e.next]
			e.next++
			matched2[j] = true
			pairs = append(pairs, stepPair{node: s1.Node, s1: s1, s2: &steps2[j]})
		} else {
			pairs = append(pairs, stepPair{node: s1.Node, s1: s1, s2: nil})
		}
	}

	// Add unmatched steps from steps2
	for j := range steps2 {
		if !matched2[j] {
			pairs = append(pairs, stepPair{node: steps2[j].Node, s1: nil, s2: &steps2[j]})
		}
	}

	return pairs
}

// compareStepOutputs compares two matched steps and returns human-readable diffs.
func compareStepOutputs(s1, s2 *archive.StepRecord) []string {
	var diffs []string

	// Status diff
	if s1.Response != nil && s2.Response != nil && s1.Response.Status != s2.Response.Status {
		diffs = append(diffs, fmt.Sprintf("Status changed: %d → %d", s1.Response.Status, s2.Response.Status))
	}

	// Error diff
	if s1.Error != s2.Error {
		if s1.Error == "" {
			diffs = append(diffs, fmt.Sprintf("New error: %s", s2.Error))
		} else if s2.Error == "" {
			diffs = append(diffs, "Error resolved")
		} else {
			diffs = append(diffs, fmt.Sprintf("Error changed: %q → %q", s1.Error, s2.Error))
		}
	}

	// Validation diff
	v1Passed := s1.Validation == nil || s1.Validation.Passed
	v2Passed := s2.Validation == nil || s2.Validation.Passed
	if v1Passed != v2Passed {
		if v2Passed {
			diffs = append(diffs, "Assertions: now passing")
		} else {
			diffs = append(diffs, "Assertions: now failing")
		}
	}

	// Retry count diff
	if s1.RetryCount != s2.RetryCount {
		diffs = append(diffs, fmt.Sprintf("Retries: %d → %d", s1.RetryCount, s2.RetryCount))
	}

	// Output key diff
	if s1.Outputs != nil || s2.Outputs != nil {
		keys1 := outputKeys(s1.Outputs)
		keys2 := outputKeys(s2.Outputs)
		added, removed := diffStringSets(keys1, keys2)
		for _, k := range added {
			diffs = append(diffs, fmt.Sprintf("Output added: %s", k))
		}
		for _, k := range removed {
			diffs = append(diffs, fmt.Sprintf("Output removed: %s", k))
		}
	}

	return diffs
}

// outputKeys returns the sorted keys from an output map.
func outputKeys(m map[string]any) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// diffStringSets returns elements in b but not a (added) and in a but not b (removed).
func diffStringSets(a, b []string) (added, removed []string) {
	setA := make(map[string]bool, len(a))
	for _, s := range a {
		setA[s] = true
	}
	setB := make(map[string]bool, len(b))
	for _, s := range b {
		setB[s] = true
	}
	for _, s := range b {
		if !setA[s] {
			added = append(added, s)
		}
	}
	for _, s := range a {
		if !setB[s] {
			removed = append(removed, s)
		}
	}
	return
}
