package main

import (
	"bytes"
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
	"unicode/utf8"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/validate"
	"github.com/spf13/cobra"
	"github.com/tidwall/gjson"
)

// defaultShowMaxBytes is how much of a step part aat run show prints before it
// cuts the output.
const defaultShowMaxBytes = 64 * 1024

// runShowCmd prints what a run archive recorded, without a browser.
var runShowCmd = &cobra.Command{
	Use:   "show <run-id|batch-id/run-id|latest|path>",
	Short: "Show a run's steps, or one step's request, response, inputs, or outputs",
	Long: `Show what a run archive recorded.

Without --step, list the run's steps with their node, HTTP status, result,
duration, and output names, then its verification and cleanup steps. --step
shows one step, named by its step ID or by a node that ran once, with where
each input's value came from. --request, --response, --inputs, --outputs, and
--resolutions print that part of the step as JSON; --resolutions says how each
input got its value. --path narrows the part with a gjson path, such as
items.0.sku, and --shape prints its structure instead of its values: each path
with its type, array sizes, and a sample value, which is the way to learn a
large response.

The run is latest (the newest run, runs inside batches included), a run ID, a
batch ID and a run ID joined by a slash, or a path to a run directory, an
archive.json, or an exported .aar file. IDs are looked up in the archive
directory: --output, else the manifest's archives, else _output/runs. Archives
are redacted when they are written, and show prints only what the archive
holds.`,
	Example: `  aat run show latest
  aat run show latest --step checkout
  aat run show latest --step checkout --response --shape
  aat run show latest --step checkout --response --path orderId
  aat run show latest --step checkout --resolutions
  aat run show _output/runs/run-20260910-230852-8b2139bc/archive.json --json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		opts, err := showOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		archiveDir := func() (string, error) {
			changed := func(name string) bool { return cmd.Flags().Changed(name) }
			getString := func(name string) string { v, _ := cmd.Flags().GetString(name); return v }
			resolved, err := config.ResolveProjectPaths(buildProjectOverrides(changed, getString))
			if err != nil {
				return "", err
			}
			return resolveOutputDir(changed("output"), getString("output"), resolved.ArchiveDir), nil
		}
		return runShowCommand(args[0], archiveDir, opts, os.Stdout, os.Stderr)
	},
}

func init() {
	flags := runShowCmd.Flags()
	flags.String("step", "", "show one step, by step ID or by the node it ran")
	flags.Bool("request", false, "print the step's request body")
	flags.Bool("response", false, "print the step's response body")
	flags.Bool("inputs", false, "print the step's resolved inputs")
	flags.Bool("outputs", false, "print the step's outputs")
	flags.Bool("resolutions", false, "print how each input got its value: source, upstream step, expression, pool pick, or selection, and the error for one that failed")
	flags.String("path", "", "print what a gjson path selects in the part (the response body unless another part is chosen)")
	flags.Bool("shape", false, "print the part's structure instead of its values: each path with its type, array sizes, and a sample")
	flags.Int("max-bytes", defaultShowMaxBytes, "cut a printed part after this many bytes (0 for no limit)")
	flags.Bool("json", false, "print the step list or the step as JSON, or the shape as a JSON array")
	runCmd.AddCommand(runShowCmd)
}

// showOptions holds the flags of aat run show.
type showOptions struct {
	Step     string
	Part     string // request, response, inputs, outputs, or resolutions; empty for the step overview
	Path     string
	Shape    bool
	JSON     bool
	MaxBytes int
}

// showOptionsFromFlags reads and checks the flags of aat run show.
func showOptionsFromFlags(cmd *cobra.Command) (showOptions, error) {
	flags := cmd.Flags()
	var opts showOptions
	opts.Step, _ = flags.GetString("step")
	opts.Path, _ = flags.GetString("path")
	opts.Shape, _ = flags.GetBool("shape")
	opts.JSON, _ = flags.GetBool("json")
	opts.MaxBytes, _ = flags.GetInt("max-bytes")
	for _, part := range []string{"request", "response", "inputs", "outputs", "resolutions"} {
		if set, _ := flags.GetBool(part); set {
			if opts.Part != "" {
				return opts, fmt.Errorf("--%s and --%s: choose one part of the step", opts.Part, part)
			}
			opts.Part = part
		}
	}
	if opts.Step == "" && (opts.Part != "" || opts.Path != "" || opts.Shape) {
		return opts, errors.New("--request, --response, --inputs, --outputs, --resolutions, --path, and --shape need --step")
	}
	if opts.Part == "" && (opts.Path != "" || opts.Shape) {
		opts.Part = "response"
	}
	if opts.MaxBytes < 0 {
		return opts, errors.New("--max-bytes must be 0 or more")
	}
	return opts, nil
}

// runShowCommand prints the run that ref names, as aat run show does.
// archiveDir resolves the archive directory; it is called only when ref is not
// a path.
func runShowCommand(ref string, archiveDir func() (string, error), opts showOptions, out, errOut io.Writer) error {
	a, src, err := loadShownArchive(ref, archiveDir)
	if err != nil {
		return err
	}
	if opts.Step == "" {
		return showRun(out, a, src, opts.JSON)
	}
	step, id, cleanup, err := findShownStep(a, opts.Step)
	if err != nil {
		return err
	}
	if opts.Part == "" {
		return showStep(out, step, id, cleanup, opts.JSON)
	}
	return showStepPart(out, errOut, step, id, opts)
}

// shownRun says where the archive aat run show prints came from.
type shownRun struct {
	Ref      string   // run ID, or batch ID and run ID; empty for a file
	Path     string   // the archive file
	Attempts []string // the other attempt files next to it
}

// loadShownArchive reads the archive that ref names.
func loadShownArchive(ref string, archiveDir func() (string, error)) (*archive.Archive, shownRun, error) {
	if isShowPath(ref) {
		return loadShownFile(ref)
	}
	dir, err := archiveDir()
	if err != nil {
		return nil, shownRun{}, err
	}
	run, err := archive.FindRun(dir, ref)
	if errors.Is(err, archive.ErrRunNotFound) {
		return nil, shownRun{}, fmt.Errorf("%w (a run is latest, a run ID, batch-ID/run-ID, or a path to a run directory, archive.json, or .aar file)", err)
	}
	if err != nil {
		return nil, shownRun{}, err
	}
	a, err := archive.Read(run.ArchivePath)
	if err != nil {
		return nil, shownRun{}, err
	}
	return a, shownRun{Ref: run.Ref(), Path: run.ArchivePath, Attempts: showAttemptFiles(run.Dir, "archive.json")}, nil
}

// isShowPath reports whether ref names a file or directory rather than a run in
// the archive directory: a name ending in .json, .aar, or .aab, or an existing
// path with a separator in it.
func isShowPath(ref string) bool {
	switch strings.ToLower(filepath.Ext(ref)) {
	case ".json", ".aar", ".aab":
		return true
	}
	if !strings.ContainsAny(ref, `/\`) {
		return false
	}
	_, err := os.Stat(ref)
	return err == nil
}

// loadShownFile reads a run from a run directory, an archive or attempt file,
// or an exported .aar file.
func loadShownFile(path string) (*archive.Archive, shownRun, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, shownRun{}, err
	}
	if info.IsDir() {
		if showIsFile(filepath.Join(path, "batch.json")) {
			return nil, shownRun{}, showBatchError(path)
		}
		path = filepath.Join(path, "archive.json")
	}
	switch {
	case strings.EqualFold(filepath.Ext(path), ".aab"):
		return nil, shownRun{}, fmt.Errorf("%s is an exported batch; import it with aat import, then show one of its runs", path)
	case strings.EqualFold(filepath.Ext(path), ".aar"):
		a, _, err := archive.LoadRunFromZip(path)
		if err != nil {
			return nil, shownRun{}, err
		}
		return a, shownRun{Path: path}, nil
	case filepath.Base(path) == "batch.json":
		return nil, shownRun{}, showBatchError(filepath.Dir(path))
	}
	a, err := archive.Read(path)
	if err != nil {
		return nil, shownRun{}, err
	}
	return a, shownRun{Path: path, Attempts: showAttemptFiles(filepath.Dir(path), filepath.Base(path))}, nil
}

// showBatchError explains that dir holds a batch, naming the runs to show.
func showBatchError(dir string) error {
	entries, _ := os.ReadDir(dir)
	var runs []string
	for _, e := range entries {
		if e.IsDir() && showIsFile(filepath.Join(dir, e.Name(), "archive.json")) {
			runs = append(runs, filepath.Join(dir, e.Name()))
		}
	}
	if len(runs) == 0 {
		return fmt.Errorf("%s is a batch with no run archives", dir)
	}
	return fmt.Errorf("%s is a batch; show one of its runs:\n  %s", dir, strings.Join(runs, "\n  "))
}

// showAttemptFiles lists the attempt-NN.json files in dir other than current:
// the archives of a plan's other attempts.
func showAttemptFiles(dir, current string) []string {
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && name != current && strings.HasPrefix(name, "attempt-") && strings.HasSuffix(name, ".json") {
			names = append(names, name)
		}
	}
	return names
}

func showIsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// findShownStep returns the step that name identifies, with its ID: a step ID,
// or a node that ran exactly once.
func findShownStep(a *archive.Archive, name string) (step *archive.StepRecord, id string, cleanup bool, err error) {
	if step, cleanup := archive.FindStep(a, name); step != nil {
		return step, name, cleanup, nil
	}
	cleanupIDs := archive.CleanupStepIDs(a.Cleanup)
	var matches []string
	for i := range a.Steps {
		if a.Steps[i].Node == name {
			matches = append(matches, archive.StepID(a.Steps[i]))
			step, cleanup = &a.Steps[i], false
		}
	}
	for i := range a.Cleanup {
		if a.Cleanup[i].Node == name {
			matches = append(matches, cleanupIDs[i])
			step, cleanup = &a.Cleanup[i], true
		}
	}
	switch len(matches) {
	case 1:
		return step, matches[0], cleanup, nil
	case 0:
		ids := make([]string, 0, len(a.Steps)+len(cleanupIDs))
		for _, s := range a.Steps {
			ids = append(ids, archive.StepID(s))
		}
		ids = append(ids, cleanupIDs...)
		return nil, "", false, fmt.Errorf("no step %q in the run; its steps are %s", name, strings.Join(ids, ", "))
	default:
		return nil, "", false, fmt.Errorf("node %s ran as %d steps, so name one with --step: %s", name, len(matches), strings.Join(matches, ", "))
	}
}

// shownStepRow is one row of aat run show's step list.
type shownStepRow struct {
	Index        int      `json:"index"`
	StepID       string   `json:"step_id"`
	Node         string   `json:"node"`
	Status       int      `json:"status,omitempty"`
	Passed       bool     `json:"passed"`
	DurationMs   int64    `json:"duration_ms"`
	Outputs      []string `json:"outputs,omitempty"`
	Verification bool     `json:"verification,omitempty"`
	CleanupFor   string   `json:"cleanup_for,omitempty"`
}

// shownRunList is the step list of aat run show, and its --json document.
type shownRunList struct {
	Run            string               `json:"run,omitempty"`
	ArchivePath    string               `json:"archive_path"`
	Plan           string               `json:"plan,omitempty"`
	Outcome        string               `json:"outcome"`
	Error          string               `json:"error,omitempty"`
	DurationMs     int64                `json:"duration_ms"`
	Attempt        int                  `json:"attempt,omitempty"`
	TotalAttempts  int                  `json:"total_attempts,omitempty"`
	OtherAttempts  []string             `json:"other_attempts,omitempty"`
	Steps          []shownStepRow       `json:"steps"`
	Cleanup        []shownStepRow       `json:"cleanup,omitempty"`
	CleanupSkipped []CleanupSkipSummary `json:"cleanup_skipped,omitempty"`
}

// showRun prints a run's step list.
func showRun(out io.Writer, a *archive.Archive, src shownRun, asJSON bool) error {
	list := buildShownRunList(a, src)
	if asJSON {
		return writeShowJSON(out, list)
	}

	var b strings.Builder
	name := list.Run
	if name == "" {
		name = list.ArchivePath
	}
	fmt.Fprintf(&b, "%s  %s  %s", name, strings.ToUpper(list.Outcome), formatDuration(time.Duration(list.DurationMs)*time.Millisecond))
	if list.TotalAttempts > 1 {
		fmt.Fprintf(&b, "  attempt %d of %d", list.Attempt, list.TotalAttempts)
	}
	b.WriteByte('\n')
	if list.Plan != "" {
		fmt.Fprintf(&b, "plan: %s\n", list.Plan)
	}
	if list.Run != "" {
		fmt.Fprintf(&b, "archive: %s\n", list.ArchivePath)
	}
	if len(list.OtherAttempts) > 0 {
		fmt.Fprintf(&b, "other attempts: %s\n", strings.Join(list.OtherAttempts, ", "))
	}
	if list.Error != "" {
		fmt.Fprintf(&b, "error: %s\n", list.Error)
	}

	var main, verification []shownStepRow
	for _, row := range list.Steps {
		if row.Verification {
			verification = append(verification, row)
		} else {
			main = append(main, row)
		}
	}
	b.WriteByte('\n')
	writeShownSteps(&b, main, "OUTPUTS")
	if len(verification) > 0 {
		b.WriteString("\nverification:\n")
		writeShownSteps(&b, verification, "OUTPUTS")
	}
	if len(list.Cleanup) > 0 {
		b.WriteString("\ncleanup:\n")
		writeShownSteps(&b, list.Cleanup, "FOR")
	}
	if len(list.CleanupSkipped) > 0 {
		b.WriteString("\ncleanup skipped:\n")
		writeCleanupSkips(&b, list.CleanupSkipped)
	}
	_, err := io.WriteString(out, b.String())
	return err
}

// buildShownRunList collects a run's step list.
func buildShownRunList(a *archive.Archive, src shownRun) shownRunList {
	summary := archive.BuildRunSummary(a)
	list := shownRunList{
		Run:           src.Ref,
		ArchivePath:   src.Path,
		Plan:          summary.PlanName,
		Outcome:       a.Result.Outcome,
		Error:         a.Result.Error,
		DurationMs:    summary.DurationMs,
		Attempt:       a.Metadata.Attempt,
		TotalAttempts: a.Metadata.TotalAttempts,
		OtherAttempts: src.Attempts,
		Steps:         []shownStepRow{},
	}
	verificationNodes := map[string]bool{}
	if a.Metadata.Plan != nil {
		for _, v := range a.Metadata.Plan.Execution.Verification {
			verificationNodes[v.Node] = true
		}
	}
	for i, s := range a.Steps {
		row := newShownStepRow(i+1, archive.StepID(s), s)
		// Verification steps run after the main flow as verify_<node>.
		row.Verification = strings.HasPrefix(row.StepID, "verify_") && (a.Metadata.Plan == nil || verificationNodes[s.Node])
		list.Steps = append(list.Steps, row)
	}
	for i, id := range archive.CleanupStepIDs(a.Cleanup) {
		row := newShownStepRow(i+1, id, a.Cleanup[i])
		row.CleanupFor = a.Cleanup[i].CleanupFor
		list.Cleanup = append(list.Cleanup, row)
	}
	for _, skip := range a.CleanupSkipped {
		list.CleanupSkipped = append(list.CleanupSkipped, CleanupSkipSummary(skip))
	}
	return list
}

func newShownStepRow(index int, id string, s archive.StepRecord) shownStepRow {
	row := shownStepRow{
		Index:      index,
		StepID:     id,
		Node:       s.Node,
		Passed:     shownStepPassed(s),
		DurationMs: s.DurationMs,
	}
	if s.Response != nil {
		row.Status = s.Response.Status
	}
	for name := range s.Outputs {
		row.Outputs = append(row.Outputs, name)
	}
	sort.Strings(row.Outputs)
	return row
}

// shownStepPassed applies the run summary's rule: a step fails on an error, a
// failed assertion, an unmet expected failure, an error in its response body,
// or a status of 400 or more that it did not expect.
func shownStepPassed(s archive.StepRecord) bool {
	if !archive.StepPassed(s) {
		return false
	}
	return s.ExpectFailure != nil || s.Response == nil || s.Response.Status < 400
}

// writeShownSteps writes rows as a table. last names the final column:
// OUTPUTS for main steps, FOR for the step a cleanup step releases.
func writeShownSteps(b *strings.Builder, rows []shownStepRow, last string) {
	idWidth, nodeWidth := len("STEP"), len("NODE")
	for _, row := range rows {
		idWidth = max(idWidth, len(row.StepID))
		nodeWidth = max(nodeWidth, len(row.Node))
	}
	line := func(index, id, node, status, result, took, rest string) {
		row := fmt.Sprintf("%3s  %-*s  %-*s  %6s  %-6s  %7s  %s", index, idWidth, id, nodeWidth, node, status, result, took, rest)
		b.WriteString(strings.TrimRight(row, " "))
		b.WriteByte('\n')
	}
	line("#", "STEP", "NODE", "STATUS", "RESULT", "TIME", last)
	for _, row := range rows {
		status := "-"
		if row.Status != 0 {
			status = strconv.Itoa(row.Status)
		}
		result := "pass"
		if !row.Passed {
			result = "FAIL"
		}
		rest := showShort(strings.Join(row.Outputs, ", "), 80)
		if last == "FOR" {
			rest = row.CleanupFor
		}
		line(strconv.Itoa(row.Index), row.StepID, row.Node, status, result, formatDuration(time.Duration(row.DurationMs)*time.Millisecond), rest)
	}
}

// writeCleanupSkips writes the table of registered cleanups that did not run
// because they were no longer needed.
func writeCleanupSkips(b *strings.Builder, skips []CleanupSkipSummary) {
	nodeWidth, forWidth := len("NODE"), len("FOR")
	for _, s := range skips {
		nodeWidth = max(nodeWidth, len(s.Node))
		forWidth = max(forWidth, len(s.CleanupFor))
	}
	line := func(node, cleanupFor, reason string) {
		b.WriteString(strings.TrimRight(fmt.Sprintf("     %-*s  %-*s  %s", nodeWidth, node, forWidth, cleanupFor, reason), " "))
		b.WriteByte('\n')
	}
	line("NODE", "FOR", "REASON")
	for _, s := range skips {
		line(s.Node, s.CleanupFor, archive.CleanupSkipRecord(s).Description())
	}
}

// shownStep is one step as aat run show prints it, and its --json document.
type shownStep struct {
	StepID            string                    `json:"step_id"`
	Node              string                    `json:"node"`
	Cleanup           bool                      `json:"cleanup,omitempty"`
	CleanupFor        string                    `json:"cleanup_for,omitempty"`
	Method            string                    `json:"method,omitempty"`
	URL               string                    `json:"url,omitempty"`
	Status            int                       `json:"status,omitempty"`
	Passed            bool                      `json:"passed"`
	DurationMs        int64                     `json:"duration_ms"`
	Retries           int                       `json:"retries,omitempty"`
	RetriedOn         []string                  `json:"retried_on,omitempty"`
	Error             string                    `json:"error,omitempty"`
	Inputs            map[string]any            `json:"inputs,omitempty"`
	Outputs           map[string]any            `json:"outputs,omitempty"`
	Assertions        []shownAssertion          `json:"assertions,omitempty"`
	RequestBodyBytes  int                       `json:"request_body_bytes,omitempty"`
	ResponseBodyBytes int                       `json:"response_body_bytes,omitempty"`
	Resolutions       []archive.InputResolution `json:"resolutions,omitempty"`
	Warnings          []string                  `json:"warnings,omitempty"`
}

// shownAssertion is one assertion result of a shown step.
type shownAssertion struct {
	Type    string `json:"type"`
	Passed  bool   `json:"passed"`
	Skipped bool   `json:"skipped,omitempty"`
	Message string `json:"message"`
}

// showStep prints one step: where its request went, what came back, its
// inputs and outputs, and the sizes of its bodies.
func showStep(out io.Writer, step *archive.StepRecord, id string, cleanup, asJSON bool) error {
	view := buildShownStep(step, id, cleanup)
	if asJSON {
		return writeShowJSON(out, view)
	}

	var b strings.Builder
	kind := "step"
	if cleanup {
		kind = "cleanup step"
	}
	fmt.Fprintf(&b, "%s %s", kind, view.StepID)
	if view.Node != view.StepID {
		fmt.Fprintf(&b, " (node %s)", view.Node)
	}
	if view.CleanupFor != "" {
		fmt.Fprintf(&b, ", for %s", view.CleanupFor)
	}
	b.WriteByte('\n')
	if view.Method != "" {
		fmt.Fprintf(&b, "%s %s\n", view.Method, view.URL)
	}
	status := "no response"
	if view.Status != 0 {
		status = fmt.Sprintf("status %d", view.Status)
	}
	result := "pass"
	if !view.Passed {
		result = "FAIL"
	}
	fmt.Fprintf(&b, "%s  %s  %s", status, result, formatDuration(time.Duration(view.DurationMs)*time.Millisecond))
	if view.Retries > 0 {
		fmt.Fprintf(&b, "  retried %d times (%s)", view.Retries, strings.Join(view.RetriedOn, ", "))
	}
	b.WriteByte('\n')
	if view.Error != "" {
		fmt.Fprintf(&b, "error: %s\n", view.Error)
	}
	writeShownInputs(&b, view.Inputs, view.Resolutions)
	writeShownValues(&b, "outputs", view.Outputs)
	if len(view.Assertions) > 0 {
		var passed, failed, skipped int
		for _, as := range view.Assertions {
			switch {
			case as.Skipped:
				skipped++
			case as.Passed:
				passed++
			default:
				failed++
			}
		}
		fmt.Fprintf(&b, "assertions: %d passed, %d failed", passed, failed)
		if skipped > 0 {
			fmt.Fprintf(&b, ", %d skipped", skipped)
		}
		b.WriteByte('\n')
		for _, as := range view.Assertions {
			if !as.Passed && !as.Skipped {
				fmt.Fprintf(&b, "  FAIL %s: %s\n", as.Type, as.Message)
			}
		}
	}
	if len(view.Warnings) > 0 {
		b.WriteString("warnings:\n")
		for _, warning := range view.Warnings {
			fmt.Fprintf(&b, "  %s\n", warning)
		}
	}
	fmt.Fprintf(&b, "request body: %s\n", showSize(view.RequestBodyBytes))
	fmt.Fprintf(&b, "response body: %s\n", showSize(view.ResponseBodyBytes))
	if view.ResponseBodyBytes > 0 {
		fmt.Fprintf(&b, "\nNext: --response --shape for the response's structure, --response --path PATH for one part of it\n")
	}
	_, err := io.WriteString(out, b.String())
	return err
}

func buildShownStep(step *archive.StepRecord, id string, cleanup bool) shownStep {
	view := shownStep{
		StepID:     id,
		Node:       step.Node,
		Cleanup:    cleanup,
		CleanupFor: step.CleanupFor,
		Passed:     shownStepPassed(*step),
		DurationMs: step.DurationMs,
		Retries:    step.RetryCount,
		RetriedOn:  step.RetriedOn,
		Error:      step.Error,
		Inputs:     step.Inputs,
		Outputs:    step.Outputs,
	}
	if step.Request != nil {
		view.Method, view.URL = step.Request.Method, step.Request.URL
		view.RequestBodyBytes = compactSize(step.Request.Body)
	}
	if step.Response != nil {
		view.Status = step.Response.Status
		view.ResponseBodyBytes = compactSize(step.Response.Body)
	}
	if step.Validation != nil {
		for _, r := range step.Validation.Results {
			view.Assertions = append(view.Assertions, shownAssertion{Type: r.Type, Passed: r.Passed, Skipped: r.Skipped, Message: r.Message})
		}
	}
	view.Resolutions = archive.StepResolutions(step)
	view.Warnings = archive.SelectionTieWarnings(step.Selections)
	return view
}

// writeShownInputs writes a step's inputs, one per line sorted by name, each
// with where its value came from. An input that couldn't be resolved, or that
// was left unset, shows its source with no value.
func writeShownInputs(b *strings.Builder, inputs map[string]any, resolutions []archive.InputResolution) {
	sources := make(map[string]archive.InputResolution, len(resolutions))
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	for _, r := range resolutions {
		sources[r.InputName] = r
		if _, ok := inputs[r.InputName]; !ok && (r.Source == "error" || r.Source == "optional_skip") {
			names = append(names, r.InputName)
		}
	}
	if len(names) == 0 {
		return
	}
	sort.Strings(names)
	values := make([]string, len(names))
	nameWidth, valueWidth := 0, 0
	for i, name := range names {
		values[i] = "-"
		if v, ok := inputs[name]; ok {
			values[i] = showValue(v)
		}
		nameWidth = max(nameWidth, len(name))
		valueWidth = min(max(valueWidth, len(values[i])), 40)
	}
	b.WriteString("inputs:\n")
	for i, name := range names {
		line := fmt.Sprintf("  %-*s  %-*s  %s", nameWidth, name, valueWidth, values[i], describeResolution(sources[name]))
		b.WriteString(strings.TrimRight(line, " "))
		b.WriteByte('\n')
	}
}

// describeResolution says briefly where an input's value came from, as in
// "plan_from createCart.cartId" or "select_edge min price of listProducts.products[2]".
func describeResolution(r archive.InputResolution) string {
	if r.Source == "" {
		return ""
	}
	parts := []string{r.Source}
	switch {
	case r.Selection != nil:
		sel := r.Selection
		pick := sel.Strategy
		if sel.SortField != "" {
			pick += " " + sel.SortField
		}
		parts = append(parts, fmt.Sprintf("%s of %s.%s[%d]", pick, sel.SourceNode, sel.SourceField, sel.SelectedIndex))
	case r.FromStep != "" && r.FromOutput != "":
		parts = append(parts, r.FromStep+"."+r.FromOutput)
	case r.FromStep != "" && r.FromInput != "":
		parts = append(parts, r.FromStep+"."+r.FromInput)
	}
	if r.Expression != "" {
		parts = append(parts, r.Expression)
	}
	if r.Error != "" {
		parts = append(parts, r.Error)
	}
	return strings.Join(parts, " ")
}

// writeShownValues writes a step's inputs or outputs, one per line, sorted by
// name.
func writeShownValues(b *strings.Builder, title string, values map[string]any) {
	if len(values) == 0 {
		return
	}
	names := make([]string, 0, len(values))
	width := 0
	for name := range values {
		names = append(names, name)
		width = max(width, len(name))
	}
	sort.Strings(names)
	fmt.Fprintf(b, "%s:\n", title)
	for _, name := range names {
		fmt.Fprintf(b, "  %-*s  %s\n", width, name, showValue(values[name]))
	}
}

// showValue renders an input or output on one line: a scalar as JSON, cut to
// 80 characters, and an array or object by its size.
func showValue(v any) string {
	switch t := v.(type) {
	case []any:
		if len(t) == 1 {
			return "[1 item]"
		}
		return fmt.Sprintf("[%d items]", len(t))
	case map[string]any:
		if len(t) == 1 {
			return "{1 key}"
		}
		return fmt.Sprintf("{%d keys}", len(t))
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return showShort(string(data), 80)
}

// showStepPart prints one part of a step as JSON, narrowed by --path, or its
// shape with --shape.
func showStepPart(out, errOut io.Writer, step *archive.StepRecord, id string, opts showOptions) error {
	doc, err := stepPartJSON(step, opts.Part)
	if err != nil {
		return err
	}
	if len(doc) == 0 {
		return fmt.Errorf("step %s has no %s", id, stepPartName(opts.Part))
	}
	if opts.Path != "" {
		result := gjson.GetBytes(doc, validate.NormalizeJSONPath(opts.Path))
		if !result.Exists() {
			return fmt.Errorf("--path %s matches nothing in the %s of step %s%s", opts.Path, stepPartName(opts.Part), id, topLevelHint(doc))
		}
		doc = []byte(result.Raw)
	}

	var text []byte
	switch {
	case opts.Shape && opts.JSON:
		lines := archive.Shape(doc)
		if lines == nil {
			lines = []archive.ShapeLine{}
		}
		if text, err = json.MarshalIndent(lines, "", "  "); err != nil {
			return err
		}
		text = append(text, '\n')
	case opts.Shape:
		text = []byte(archive.RenderShape(archive.Shape(doc)))
		if len(text) == 0 {
			text = []byte("(no paths: the value is an empty object)\n")
		}
	default:
		var buf bytes.Buffer
		if json.Indent(&buf, doc, "", "  ") != nil {
			buf.Reset()
			buf.Write(doc)
		}
		buf.WriteByte('\n')
		text = buf.Bytes()
	}
	return writeCapped(out, errOut, text, opts.MaxBytes)
}

// stepPartJSON returns a part of a step as JSON, or nothing when the step has
// none.
func stepPartJSON(step *archive.StepRecord, part string) ([]byte, error) {
	switch part {
	case "request":
		if step.Request == nil {
			return nil, nil
		}
		return step.Request.Body, nil
	case "response":
		if step.Response == nil {
			return nil, nil
		}
		return step.Response.Body, nil
	case "resolutions":
		resolutions := archive.StepResolutions(step)
		if len(resolutions) == 0 {
			return nil, nil
		}
		return json.Marshal(resolutions)
	case "inputs":
		if len(step.Inputs) == 0 {
			return nil, nil
		}
		return json.Marshal(step.Inputs)
	default:
		if len(step.Outputs) == 0 {
			return nil, nil
		}
		return json.Marshal(step.Outputs)
	}
}

func stepPartName(part string) string {
	switch part {
	case "request", "response":
		return part + " body"
	}
	return part
}

// topLevelHint describes the top level of a JSON document, for an error about
// a path that matched nothing.
func topLevelHint(doc []byte) string {
	root := gjson.ParseBytes(doc)
	switch {
	case root.IsObject():
		var keys []string
		root.ForEach(func(key, _ gjson.Result) bool {
			keys = append(keys, key.String())
			return len(keys) < 20
		})
		if len(keys) == 0 {
			return "; it is an empty object"
		}
		return "; its top-level keys are " + strings.Join(keys, ", ")
	case root.IsArray():
		return fmt.Sprintf("; it is an array of %d items, so a path starts with an index or # (0.id, #.id)", root.Get("#").Int())
	}
	return ""
}

// writeCapped writes text to out. Past maxBytes (0 for no limit) it stops at a
// character boundary and says so on errOut.
func writeCapped(out, errOut io.Writer, text []byte, maxBytes int) error {
	if maxBytes == 0 || len(text) <= maxBytes {
		_, err := out.Write(text)
		return err
	}
	n := maxBytes
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}
	if _, err := out.Write(append(text[:n:n], '\n')); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(errOut, "aat: output cut at %d of %d bytes; narrow it with --path or --shape, or raise --max-bytes (0 for no limit)\n", n, len(text))
	return nil
}

// writeShowJSON writes v as indented JSON, leaving characters such as & in URLs
// unescaped.
func writeShowJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// compactSize returns the size of a JSON body without insignificant space.
func compactSize(body json.RawMessage) int {
	if len(body) == 0 {
		return 0
	}
	var buf bytes.Buffer
	if json.Compact(&buf, body) != nil {
		return len(body)
	}
	return buf.Len()
}

// showSize renders a byte count.
func showSize(n int) string {
	switch {
	case n == 0:
		return "none"
	case n < 1024:
		return fmt.Sprintf("%d bytes", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

// showShort cuts s to limit characters, marking the cut with an ellipsis.
func showShort(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	count := 0
	for i := range s {
		if count == limit-1 {
			return s[:i] + "…"
		}
		count++
	}
	return s
}
