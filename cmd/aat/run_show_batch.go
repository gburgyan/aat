package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"github.com/gburgyan/aat/archive"
)

// shownBatch is a batch as aat run show prints it, and its --json document.
type shownBatch struct {
	Batch           string                `json:"batch"`
	Path            string                `json:"path"`
	Outcome         string                `json:"outcome"`
	DurationMs      int64                 `json:"duration_ms"`
	TotalRuns       int                   `json:"total_runs"`
	PassedRuns      int                   `json:"passed_runs"`
	FailedRuns      int                   `json:"failed_runs"`
	ErrorRuns       int                   `json:"error_runs"`
	AbortedRuns     int                   `json:"aborted_runs,omitempty"`
	SkippedRuns     int                   `json:"skipped_runs,omitempty"`
	Runs            []shownBatchRun       `json:"runs"`
	Cleanup         []shownCleanupNode    `json:"cleanup,omitempty"`
	CleanupFailures []shownCleanupFailure `json:"cleanup_failures,omitempty"`
}

// shownBatchRun is one run of a batch, or a permutation skipped as a duplicate.
type shownBatchRun struct {
	Run         string   `json:"run,omitempty"`
	Plan        string   `json:"plan"`
	Layers      []string `json:"layers,omitempty"`
	Outcome     string   `json:"outcome"`
	Steps       int      `json:"steps"`
	Passed      int      `json:"passed"`
	Failed      int      `json:"failed"`
	DurationMs  int64    `json:"duration_ms"`
	Skipped     bool     `json:"skipped,omitempty"`
	DuplicateOf string   `json:"duplicate_of,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// shownCleanupNode counts what one cleanup node did across a batch's runs: the
// steps that ran and failed, and the pairings skipped because a step released
// the resource or because their when condition was false.
type shownCleanupNode struct {
	Node     string `json:"node"`
	Ran      int    `json:"ran"`
	Failed   int    `json:"failed"`
	Released int    `json:"skipped_released"`
	When     int    `json:"skipped_when"`
}

// shownCleanupFailure is a cleanup step that failed in one of a batch's runs.
type shownCleanupFailure struct {
	Run        string `json:"run"`
	Step       string `json:"step"`
	Node       string `json:"node"`
	CleanupFor string `json:"cleanup_for,omitempty"`
	Status     int    `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
}

// shownBatchDir returns the batch directory ref names, when it names one: a
// batch ID in the archive directory, or a path to a batch directory or its
// batch.json.
func shownBatchDir(ref string, archiveDir func() (string, error)) (string, bool, error) {
	if isShowPath(ref) {
		info, err := os.Stat(ref)
		switch {
		case err != nil:
			return "", false, nil
		case info.IsDir() && showIsFile(filepath.Join(ref, "batch.json")):
			return ref, true, nil
		case !info.IsDir() && filepath.Base(ref) == "batch.json":
			return filepath.Dir(ref), true, nil
		}
		return "", false, nil
	}
	if ref == "latest" || strings.Contains(ref, "/") {
		return "", false, nil
	}
	dir, err := archiveDir()
	if err != nil {
		return "", false, err
	}
	batchDir, err := archive.FindBatch(dir, ref)
	if errors.Is(err, archive.ErrRunNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return batchDir, true, nil
}

// showBatch prints a batch: its totals, one row per run, and what cleanup did
// across its runs.
func showBatch(out io.Writer, dir string, format showFormat) error {
	view, err := buildShownBatch(dir)
	if err != nil {
		return err
	}
	if format != showText {
		return writeShowJSON(out, view, format == showCompactJSON)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s  %s\n", view.Batch, strings.ToUpper(view.Outcome), formatDuration(time.Duration(view.DurationMs)*time.Millisecond))
	fmt.Fprintf(&b, "batch: %s\n", filepath.Join(view.Path, "batch.json"))
	fmt.Fprintf(&b, "runs: %d, %d passed, %d failed, %d errors", view.TotalRuns, view.PassedRuns, view.FailedRuns, view.ErrorRuns)
	if view.AbortedRuns > 0 {
		fmt.Fprintf(&b, ", %d aborted", view.AbortedRuns)
	}
	if view.SkippedRuns > 0 {
		fmt.Fprintf(&b, ", %d skipped as duplicates", view.SkippedRuns)
	}
	b.WriteString("\n\n")
	writeShownBatchRuns(&b, view.Runs)
	if len(view.Cleanup) > 0 {
		b.WriteString("\ncleanup:\n")
		writeShownCleanupNodes(&b, view.Cleanup)
	}
	if len(view.CleanupFailures) > 0 {
		b.WriteString("\ncleanup failures:\n")
		for _, f := range view.CleanupFailures {
			fmt.Fprintf(&b, "  %s  %s", f.Run, f.Step)
			if f.CleanupFor != "" {
				fmt.Fprintf(&b, " (for %s)", f.CleanupFor)
			}
			switch {
			case f.Error != "":
				fmt.Fprintf(&b, ": %s", f.Error)
			case f.Status != 0:
				fmt.Fprintf(&b, ": status %d", f.Status)
			}
			b.WriteByte('\n')
		}
	}
	_, err = io.WriteString(out, b.String())
	return err
}

// buildShownBatch reads a batch's batch.json and its runs' archives.
func buildShownBatch(dir string) (shownBatch, error) {
	b, err := archive.ReadBatch(filepath.Join(dir, "batch.json"))
	if err != nil {
		return shownBatch{}, err
	}
	view := shownBatch{
		Batch:       b.Metadata.BatchID,
		Path:        dir,
		Outcome:     b.Result.Outcome,
		DurationMs:  b.Result.TotalDurationMs,
		TotalRuns:   b.Result.TotalRuns,
		PassedRuns:  b.Result.PassedRuns,
		FailedRuns:  b.Result.FailedRuns,
		ErrorRuns:   b.Result.ErrorRuns,
		AbortedRuns: b.Result.AbortedRuns,
		SkippedRuns: b.Result.SkippedRuns,
		Runs:        []shownBatchRun{},
	}
	if view.Batch == "" {
		view.Batch = filepath.Base(dir)
	}

	cleanup := map[string]*shownCleanupNode{}
	node := func(name string) *shownCleanupNode {
		if cleanup[name] == nil {
			cleanup[name] = &shownCleanupNode{Node: name}
		}
		return cleanup[name]
	}
	for _, entry := range b.Runs {
		view.Runs = append(view.Runs, shownBatchRun{
			Run:         entry.RunID,
			Plan:        entry.PlanName,
			Layers:      entry.Layers,
			Outcome:     entry.Outcome,
			Steps:       entry.StepCount,
			Passed:      entry.PassedCount,
			Failed:      entry.FailedCount,
			DurationMs:  entry.DurationMs,
			Skipped:     entry.Skipped,
			DuplicateOf: entry.DuplicateOf,
			Error:       entry.Error,
		})
		if entry.Skipped || entry.RunID == "" {
			continue
		}
		cleanupSteps, skipped, err := readRunCleanup(filepath.Join(dir, entry.RunID, "archive.json"))
		if err != nil {
			continue // a run whose archive is missing or unreadable counts no cleanup
		}
		for i, id := range archive.CleanupStepIDs(cleanupSteps) {
			step := cleanupSteps[i]
			n := node(step.Node)
			n.Ran++
			if shownStepPassed(step) {
				continue
			}
			n.Failed++
			failure := shownCleanupFailure{Run: entry.RunID, Step: id, Node: step.Node, CleanupFor: step.CleanupFor, Error: step.Error}
			if step.Response != nil {
				failure.Status = step.Response.Status
			}
			view.CleanupFailures = append(view.CleanupFailures, failure)
		}
		for _, skip := range skipped {
			switch skip.Reason {
			case "released":
				node(skip.Node).Released++
			case "when":
				node(skip.Node).When++
			}
		}
	}

	names := make([]string, 0, len(cleanup))
	for name := range cleanup {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		view.Cleanup = append(view.Cleanup, *cleanup[name])
	}
	return view, nil
}

// readRunCleanup reads only the cleanup records of a run's archive. A batch's
// archives can hold hundreds of megabytes of response bodies, which the batch
// view doesn't need to decode.
func readRunCleanup(path string) ([]archive.StepRecord, []archive.CleanupSkipRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var steps []archive.StepRecord
	if raw := gjson.GetBytes(data, "cleanup"); raw.Exists() {
		if err := json.Unmarshal([]byte(raw.Raw), &steps); err != nil {
			return nil, nil, fmt.Errorf("%s: reading cleanup: %w", path, err)
		}
	}
	var skipped []archive.CleanupSkipRecord
	if raw := gjson.GetBytes(data, "cleanupSkipped"); raw.Exists() {
		if err := json.Unmarshal([]byte(raw.Raw), &skipped); err != nil {
			return nil, nil, fmt.Errorf("%s: reading cleanupSkipped: %w", path, err)
		}
	}
	return steps, skipped, nil
}

// writeShownBatchRuns writes a batch's runs as a table.
func writeShownBatchRuns(b *strings.Builder, runs []shownBatchRun) {
	runWidth, planWidth, layerWidth := len("RUN"), len("PLAN"), len("LAYERS")
	layers := make([]string, len(runs))
	for i, r := range runs {
		layers[i] = showOrDash(strings.Join(r.Layers, ","))
		runWidth = max(runWidth, len(showOrDash(r.Run)))
		planWidth = max(planWidth, len(r.Plan))
		layerWidth = max(layerWidth, len(layers[i]))
	}
	line := func(index, run, plan, layer, result, steps, took, rest string) {
		row := fmt.Sprintf("%3s  %-*s  %-*s  %-*s  %-6s  %5s  %7s  %s", index, runWidth, run, planWidth, plan, layerWidth, layer, result, steps, took, rest)
		b.WriteString(strings.TrimRight(row, " "))
		b.WriteByte('\n')
	}
	line("#", "RUN", "PLAN", "LAYERS", "RESULT", "STEPS", "TIME", "")
	for i, r := range runs {
		steps, took, rest := "-", "-", ""
		switch {
		case r.Skipped && r.DuplicateOf != "":
			rest = "duplicate of " + r.DuplicateOf
		case r.Skipped:
			rest = "skipped"
		default:
			steps = fmt.Sprintf("%d/%d", r.Passed, r.Steps)
			took = formatDuration(time.Duration(r.DurationMs) * time.Millisecond)
		}
		if r.Error != "" {
			rest = showShort(r.Error, 80)
		}
		line(strconv.Itoa(i+1), showOrDash(r.Run), r.Plan, layers[i], shownRunResult(r), steps, took, rest)
	}
}

// writeShownCleanupNodes writes what each cleanup node did across a batch.
func writeShownCleanupNodes(b *strings.Builder, nodes []shownCleanupNode) {
	nodeWidth := len("NODE")
	for _, n := range nodes {
		nodeWidth = max(nodeWidth, len(n.Node))
	}
	fmt.Fprintf(b, "  %-*s  %4s  %6s  %8s  %4s\n", nodeWidth, "NODE", "RAN", "FAILED", "RELEASED", "WHEN")
	for _, n := range nodes {
		fmt.Fprintf(b, "  %-*s  %4d  %6d  %8d  %4d\n", nodeWidth, n.Node, n.Ran, n.Failed, n.Released, n.When)
	}
}

// shownRunResult is a batch run's RESULT column.
func shownRunResult(r shownBatchRun) string {
	switch {
	case r.Skipped:
		return "skip"
	case r.Outcome == "passed":
		return "pass"
	case r.Outcome == "failed":
		return "FAIL"
	default:
		return strings.ToUpper(r.Outcome)
	}
}

// showOrDash returns s, or "-" when it is empty.
func showOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
