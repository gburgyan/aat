package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/graph"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

// runBatchCmd executes all plans in a directory as a correlated batch.
var runBatchCmd = &cobra.Command{
	Use:   "batch [directory]",
	Short: "Execute all plans in a directory as a batch",
	Long: `Execute all plans in a directory (or all configured plan directories) as a correlated batch.
Results are saved under a single batch directory for correlation.

Without arguments, runs all plans from configured plan directories.
With a relative path, filters to plans under that subdirectory.
With an absolute path, treats it as a standalone plan directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		changed := func(name string) bool { return cmd.Flags().Changed(name) }
		getString := func(name string) string { v, _ := cmd.Flags().GetString(name); return v }

		overrides := buildProjectOverrides(changed, getString)
		resolved, err := config.ResolveProjectPaths(overrides)
		if err != nil {
			return err
		}

		var filterPath string
		if len(args) > 0 {
			filterPath = args[0]
		}

		jsonFlag, _ := cmd.Flags().GetBool("json")
		quiet, _ := cmd.Flags().GetBool("quiet")
		overrideFlags, _ := cmd.Flags().GetStringSlice("override")
		envOverlay, _ := cmd.Flags().GetString("env-overlay")
		parallel, _ := cmd.Flags().GetInt("parallel")
		retries, _ := cmd.Flags().GetInt("retries")
		layerFlags, _ := cmd.Flags().GetStringSlice("layer")
		layerGroupFlags, _ := cmd.Flags().GetStringArray("layer-group")
		layerGroups := parseLayerGroups(layerGroupFlags)

		outputDir := resolveOutputDir(cmd.Flags().Changed("output"), getString("output"), resolved.ArchiveDir)

		ba := &batchArgs{
			runArgs: runArgs{
				EnvPath:       resolved.EnvPath,
				GraphPath:     resolved.GraphPath,
				TemplatesPath: resolved.TemplatesPath,
				OutputDir:     outputDir,
				DomainPath:    resolved.DomainPath,
				JSON:          jsonFlag,
				Quiet:         quiet,
				Overrides:     overrideFlags,
				EnvOverlay:    envOverlay,
				MaxRetries:    retries,
				Layers:        layerFlags,
				LayersDir:     resolved.LayersDir,
				LayerGroups:   layerGroups,
			},
			PlanDirs:   resolved.PlanDirs,
			FilterPath: filterPath,
			Parallel:   parallel,
		}

		code := executeBatch(ba)
		if code != 0 {
			return &exitError{Code: code}
		}
		return nil
	},
}

func init() {
	runBatchCmd.Flags().Bool("json", false, "output machine-readable JSON batch summary to stdout")
	runBatchCmd.Flags().Bool("quiet", false, "suppress progress messages, show only per-plan summary lines")
	runBatchCmd.Flags().Int("parallel", 1, "number of plans to execute concurrently (default 1 = sequential)")
	runBatchCmd.Flags().StringArray("layer-group", nil,
		"layer group for permutation (comma-separated names, repeatable)")

	runCmd.AddCommand(runBatchCmd)
}

// batchArgs extends runArgs with batch-specific configuration.
type batchArgs struct {
	runArgs
	PlanDirs   []string
	FilterPath string // optional subdirectory filter
	Parallel   int    // concurrency limit; <=1 means sequential
}

// BatchSummary is the machine-readable JSON output for batch CI/CD pipelines.
type BatchSummary struct {
	Outcome     string           `json:"outcome"`
	BatchID     string           `json:"batchId"`
	Runs        []BatchRunResult `json:"runs"`
	Summary     BatchStats       `json:"summary"`
	ArchivePath string           `json:"archive_path,omitempty"`
}

// BatchRunResult is a per-plan entry in the batch JSON summary.
type BatchRunResult struct {
	PlanName    string   `json:"plan_name"`
	Outcome     string   `json:"outcome"`
	StepCount   int      `json:"step_count"`
	PassedSteps int      `json:"passed_steps"`
	FailedSteps int      `json:"failed_steps"`
	DurationMs  int64    `json:"duration_ms"`
	Error       string   `json:"error,omitempty"`
	ArchivePath string   `json:"archive_path,omitempty"`
	Attempts    int      `json:"attempts,omitempty"`    // total attempts (omitted if 1)
	Layers      []string `json:"layers,omitempty"`      // effective layers for this run
	Permutation string   `json:"permutation,omitempty"` // permutation label for grouping
}

// BatchStats is the aggregate counts in the batch JSON summary.
type BatchStats struct {
	TotalPlans  int   `json:"total_plans"`
	PassedPlans int   `json:"passed_plans"`
	FailedPlans int   `json:"failed_plans"`
	ErrorPlans  int   `json:"error_plans"`
	DurationMs  int64 `json:"duration_ms"`
}

// batchResult is the internal result from batchCommand.
type batchResult struct {
	summary  *BatchSummary
	batchDir string
	err      error
	setupErr bool
}

// executeBatch handles output modes and returns an exit code.
func executeBatch(ba *batchArgs) int {
	if ba.JSON {
		ba.Quiet = true
	}

	var out io.Writer = os.Stdout
	if ba.Quiet {
		out = io.Discard
	}

	res := batchCommand(context.Background(), ba, out)

	if ba.JSON {
		if res.summary != nil {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(res.summary)
		} else {
			s := &BatchSummary{
				Outcome: "error",
			}
			if res.err != nil {
				s.Runs = []BatchRunResult{{PlanName: "", Outcome: "error", Error: res.err.Error()}}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(s)
		}
		return batchExitCode(res)
	}

	if ba.Quiet && res.summary != nil {
		for _, r := range res.summary.Runs {
			name := specDisplayName(r.PlanName, r.Permutation)
			attemptSuffix := ""
			if r.Attempts > 1 {
				attemptSuffix = fmt.Sprintf(" (%d attempts)", r.Attempts)
			}
			switch r.Outcome {
			case "passed":
				_, _ = fmt.Fprintf(os.Stdout, "%s: PASSED%s\n", name, attemptSuffix)
			case "failed":
				_, _ = fmt.Fprintf(os.Stdout, "%s: FAILED: %s%s\n", name, r.Error, attemptSuffix)
			case "error":
				_, _ = fmt.Fprintf(os.Stdout, "%s: ERROR: %s%s\n", name, r.Error, attemptSuffix)
			}
		}
		_, _ = fmt.Fprintf(os.Stdout, "Batch: %d/%d PASSED",
			res.summary.Summary.PassedPlans, res.summary.Summary.TotalPlans)
		if res.summary.Summary.FailedPlans > 0 {
			_, _ = fmt.Fprintf(os.Stdout, ", %d FAILED", res.summary.Summary.FailedPlans)
		}
		if res.summary.Summary.ErrorPlans > 0 {
			_, _ = fmt.Fprintf(os.Stdout, ", %d ERROR", res.summary.Summary.ErrorPlans)
		}
		_, _ = fmt.Fprintln(os.Stdout)
		if res.batchDir != "" {
			_, _ = fmt.Fprintf(os.Stdout, "Archive: %s\n", res.batchDir)
		}
		return batchExitCode(res)
	}

	if res.err != nil {
		fmt.Fprintf(os.Stderr, "aat: %s\n", res.err)
	}
	return batchExitCode(res)
}

// batchExitCode maps a batchResult to a process exit code.
// 0 = all pass, 1 = any fail, 2 = any error or setup error.
func batchExitCode(res *batchResult) int {
	if res.setupErr {
		return exitCodeInfra
	}
	if res.summary == nil {
		return exitCodeInfra
	}
	if res.summary.Summary.ErrorPlans > 0 {
		return exitCodeInfra
	}
	if res.summary.Summary.FailedPlans > 0 {
		return 1
	}
	return 0
}

// batchRunSpec describes a single (plan, permutation) pair in the run matrix.
type batchRunSpec struct {
	entry       config.PlanEntry
	layers      []string // effective layers for this run (base + permutation)
	permutation string   // permutation label for display/grouping ("" when no layer groups)
}

// buildRunMatrix generates the list of batchRunSpec entries. When layer groups
// are present, every plan is crossed with every permutation (cartesian product).
// Otherwise, each plan gets a single entry with the base layers.
func buildRunMatrix(plans []config.PlanEntry, baseLayers []string, layerGroups [][]string) []batchRunSpec {
	if len(layerGroups) == 0 {
		// No permutations — one spec per plan, no permutation label.
		specs := make([]batchRunSpec, len(plans))
		for i, entry := range plans {
			specs[i] = batchRunSpec{
				entry:  entry,
				layers: baseLayers,
			}
		}
		return specs
	}

	perms := graph.GenerateLayerPermutations(layerGroups)
	specs := make([]batchRunSpec, 0, len(plans)*len(perms))
	for _, entry := range plans {
		for _, perm := range perms {
			// Combine base layers + permutation layers, deduplicating.
			effective := mergeLayerNames(baseLayers, perm.Layers)
			specs = append(specs, batchRunSpec{
				entry:       entry,
				layers:      effective,
				permutation: perm.Label,
			})
		}
	}
	return specs
}

// mergeLayerNames combines base and extra layer names, deduplicating while
// preserving order (base first, then extras).
func mergeLayerNames(base, extra []string) []string {
	if len(extra) == 0 {
		if len(base) == 0 {
			return nil
		}
		result := make([]string, len(base))
		copy(result, base)
		return result
	}
	seen := make(map[string]bool, len(base)+len(extra))
	var result []string
	for _, name := range base {
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	for _, name := range extra {
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	return result
}

// specDisplayName returns the display name for a batchRunSpec. When a permutation
// label is present, it appends " [label]" to the plan name.
func specDisplayName(planName string, permutation string) string {
	if permutation == "" {
		return planName
	}
	return fmt.Sprintf("%s [%s]", planName, permutation)
}

// batchCommand executes the batch pipeline.
func batchCommand(ctx context.Context, args *batchArgs, out io.Writer) *batchResult {
	logf := func(format string, a ...any) {
		_, _ = fmt.Fprintf(out, format, a...)
	}

	// 1. Discover plans
	plans, source, err := discoverBatchPlans(args.PlanDirs, args.FilterPath)
	if err != nil {
		return &batchResult{setupErr: true, err: err}
	}

	if len(plans) == 0 {
		logf("aat: no plans found\n")
		return &batchResult{
			summary: &BatchSummary{
				Outcome: "passed",
				Summary: BatchStats{},
			},
		}
	}

	// 2. Build the run matrix (plans × permutations)
	specs := buildRunMatrix(plans, args.Layers, args.LayerGroups)

	if len(args.LayerGroups) > 0 {
		permCount := len(specs) / len(plans)
		logf("aat: batch run — %d plans x %d permutations = %d total runs", len(plans), permCount, len(specs))
	} else {
		logf("aat: batch run — %d plans", len(plans))
	}
	if args.Parallel > 1 {
		logf(" (parallel=%d)", args.Parallel)
	}
	logf("\n\n")

	// 3. Load shared infrastructure once
	rctx, err := loadRunContext(ctx, &args.runArgs, logf)
	if err != nil {
		return &batchResult{setupErr: true, err: err}
	}

	// 4. Generate batch ID and create batch directory
	batchID := archive.GenerateBatchID()
	batchDir := filepath.Join(args.OutputDir, batchID)

	logf("\n")

	// 5. Execute plans
	batchStart := time.Now()
	var runs []BatchRunResult
	var batchEntries []archive.BatchRunEntry

	if args.Parallel > 1 {
		runs, batchEntries = batchParallel(ctx, rctx, specs, batchDir, args, out)
	} else {
		runs, batchEntries = batchSequential(ctx, rctx, specs, batchDir, args, out)
	}

	totalDur := time.Since(batchStart)

	// 6. Compute aggregate stats
	stats := BatchStats{
		TotalPlans: len(runs),
		DurationMs: totalDur.Milliseconds(),
	}
	for _, r := range runs {
		switch r.Outcome {
		case "passed":
			stats.PassedPlans++
		case "failed":
			stats.FailedPlans++
		default:
			stats.ErrorPlans++
		}
	}

	aggregateOutcome := "passed"
	if stats.ErrorPlans > 0 {
		aggregateOutcome = "error"
	} else if stats.FailedPlans > 0 {
		aggregateOutcome = "failed"
	}

	// 7. Write batch.json
	batchArchive := &archive.BatchArchive{
		Metadata: archive.BatchMetadata{
			Version:     "1.0.0",
			BatchID:     batchID,
			Timestamp:   batchStart,
			Source:      source,
			ToolVersion: "0.1.0",
			Layers:      args.Layers,
			LayerGroups: args.LayerGroups,
		},
		Runs: batchEntries,
		Result: archive.BatchResult{
			Outcome:         aggregateOutcome,
			TotalRuns:       stats.TotalPlans,
			PassedRuns:      stats.PassedPlans,
			FailedRuns:      stats.FailedPlans,
			ErrorRuns:       stats.ErrorPlans,
			TotalDurationMs: stats.DurationMs,
		},
	}

	batchJSONPath := filepath.Join(batchDir, "batch.json")
	if err := archive.WriteBatch(batchArchive, batchJSONPath); err != nil {
		logf("aat: warning: failed to write batch.json: %s\n", err)
	}

	// 8. Print aggregate summary
	logf("\nBatch: %d/%d PASSED", stats.PassedPlans, stats.TotalPlans)
	if stats.FailedPlans > 0 {
		logf(", %d FAILED", stats.FailedPlans)
	}
	if stats.ErrorPlans > 0 {
		logf(", %d ERROR", stats.ErrorPlans)
	}
	logf(" (%s)\n", formatDuration(totalDur))
	logf("Archive: %s\n", batchDir)

	summary := &BatchSummary{
		Outcome:     aggregateOutcome,
		BatchID:     batchID,
		Runs:        runs,
		Summary:     stats,
		ArchivePath: batchDir,
	}

	return &batchResult{
		summary:  summary,
		batchDir: batchDir,
	}
}

// batchSequential executes specs one at a time with streaming step output.
func batchSequential(ctx context.Context, rctx *runContext, specs []batchRunSpec, batchDir string, args *batchArgs, out io.Writer) ([]BatchRunResult, []archive.BatchRunEntry) {
	var runs []BatchRunResult
	var batchEntries []archive.BatchRunEntry
	noopLogf := func(string, ...any) {}

	for _, spec := range specs {
		planName := planBaseName(spec.entry.Name)
		displayName := specDisplayName(planName, spec.permutation)

		// Create a shallow copy of rctx with per-spec layers
		specCtx := specRunContext(rctx, spec.layers)

		// Create streaming observer for sequential mode (unless output suppressed)
		var observer engine.ProgressObserver
		if out != io.Discard {
			observer = NewBatchStreamObserver(out, displayName)
		}

		var res *runResult
		if args.MaxRetries > 0 {
			res = loadAndRunPlanWithRetries(ctx, specCtx, spec.entry.FullPath, batchDir, args.MaxRetries, observer, noopLogf)
		} else {
			res = loadAndRunPlan(ctx, specCtx, spec.entry.FullPath, batchDir, observer, noopLogf)
		}

		br, be := buildPlanResult(planName, spec.permutation, res)
		runs = append(runs, br)
		batchEntries = append(batchEntries, be)

		// When no observer, print a one-line summary (quiet-ish fallback)
		if observer == nil {
			dur := formatDuration(time.Duration(br.DurationMs) * time.Millisecond)
			switch br.Outcome {
			case "passed":
				_, _ = fmt.Fprintf(out, "  %s  PASSED (%d steps, %s)\n", displayName, br.StepCount, dur)
			case "failed":
				_, _ = fmt.Fprintf(out, "  %s  FAILED (%d steps, %s)\n", displayName, br.StepCount, dur)
			case "error":
				_, _ = fmt.Fprintf(out, "  %s  ERROR: %s\n", displayName, br.Error)
			}
		}

		_, _ = fmt.Fprintln(out)
	}

	return runs, batchEntries
}

// batchParallel executes specs concurrently using errgroup with a concurrency limit.
func batchParallel(ctx context.Context, rctx *runContext, specs []batchRunSpec, batchDir string, args *batchArgs, out io.Writer) ([]BatchRunResult, []archive.BatchRunEntry) {
	n := len(specs)
	results := make([]runResult, n)
	displayNames := make([]string, n)
	planNames := make([]string, n)

	for i, spec := range specs {
		planNames[i] = planBaseName(spec.entry.Name)
		displayNames[i] = specDisplayName(planNames[i], spec.permutation)
	}

	// Detect terminal for progress rendering
	ti := DetectTerminal()
	var renderer *ProgressRenderer
	if out != io.Discard && ti.IsTTY {
		renderer = NewProgressRenderer(out, ti.Width)
	}

	// Shared mutex for non-TTY output
	var outputMu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(args.Parallel)

	for i, spec := range specs {
		i, spec := i, spec // capture loop vars

		g.Go(func() error {
			noopLogf := func(string, ...any) {}

			// Create a shallow copy of rctx with per-spec layers
			specCtx := specRunContext(rctx, spec.layers)

			var observer engine.ProgressObserver
			if renderer != nil {
				state := &PlanProgressState{
					PlanName:  displayNames[i],
					PlanIndex: i,
					StartTime: time.Now(),
				}
				renderer.AddPlan(state)
				observer = NewParallelProgressObserver(state, renderer)
			}

			var res *runResult
			if args.MaxRetries > 0 {
				res = loadAndRunPlanWithRetries(gctx, specCtx, spec.entry.FullPath, batchDir, args.MaxRetries, observer, noopLogf)
			} else {
				res = loadAndRunPlan(gctx, specCtx, spec.entry.FullPath, batchDir, observer, noopLogf)
			}

			if res != nil {
				results[i] = *res
			}

			// Build result line for completion
			br, _ := buildPlanResult(planNames[i], spec.permutation, res)
			resultLine := formatBatchResultLine(displayNames[i], br)

			if renderer != nil {
				// Find the state from the observer and complete it
				if ppo, ok := observer.(*ParallelProgressObserver); ok {
					renderer.CompletePlan(ppo.state, resultLine)
				}
			} else if out != io.Discard {
				// Non-TTY fallback: print simple result line under mutex
				outputMu.Lock()
				_, _ = fmt.Fprintln(out, resultLine)
				outputMu.Unlock()
			}

			return nil
		})
	}

	_ = g.Wait()

	if renderer != nil {
		renderer.Finish()
	}

	// Build final results in original spec order
	var runs []BatchRunResult
	var batchEntries []archive.BatchRunEntry
	for i, spec := range specs {
		br, be := buildPlanResult(planNames[i], spec.permutation, &results[i])
		runs = append(runs, br)
		batchEntries = append(batchEntries, be)
	}

	return runs, batchEntries
}

// specRunContext creates a shallow copy of runContext with layers overridden
// for a specific batchRunSpec. This ensures each spec gets its own layer set
// while sharing all other infrastructure.
func specRunContext(rctx *runContext, layers []string) *runContext {
	cp := *rctx
	cp.Layers = layers
	return &cp
}

// buildPlanResult constructs BatchRunResult and BatchRunEntry from a runResult.
func buildPlanResult(planName, permutation string, res *runResult) (BatchRunResult, archive.BatchRunEntry) {
	br := BatchRunResult{
		PlanName:    planName,
		ArchivePath: res.archivePath,
		Permutation: permutation,
	}
	be := archive.BatchRunEntry{
		PlanName:    planName,
		Permutation: permutation,
	}

	if res.summary != nil {
		br.Outcome = res.summary.Outcome
		br.StepCount = res.summary.Summary.TotalSteps
		br.PassedSteps = res.summary.Summary.PassedSteps
		br.FailedSteps = res.summary.Summary.FailedSteps
		br.DurationMs = res.summary.Summary.DurationMs
		br.Error = res.summary.Error

		be.Outcome = res.summary.Outcome
		be.StepCount = res.summary.Summary.TotalSteps
		be.PassedCount = res.summary.Summary.PassedSteps
		be.FailedCount = res.summary.Summary.FailedSteps
		be.DurationMs = res.summary.Summary.DurationMs
		be.Error = res.summary.Error
	} else {
		br.Outcome = "error"
		if res.err != nil {
			br.Error = res.err.Error()
		}
		be.Outcome = "error"
		if res.err != nil {
			be.Error = res.err.Error()
		}
	}

	// Extract run ID from archive path for batch entry
	if res.archivePath != "" {
		be.RunID = filepath.Base(filepath.Dir(res.archivePath))
	}

	// Annotate with attempt count if retries occurred
	if res.attempts > 1 {
		br.Attempts = res.attempts
		be.Attempts = res.attempts
	}

	// Propagate per-run effective layers
	be.Layers = res.layers
	br.Layers = res.layers

	return br, be
}

// formatBatchResultLine formats a single plan result as a display line.
func formatBatchResultLine(displayName string, br BatchRunResult) string {
	dur := formatDuration(time.Duration(br.DurationMs) * time.Millisecond)
	switch br.Outcome {
	case "passed":
		return fmt.Sprintf("  %s  PASSED (%d steps, %s)", displayName, br.StepCount, dur)
	case "failed":
		return fmt.Sprintf("  %s  FAILED (%d steps, %s)", displayName, br.StepCount, dur)
	case "error":
		return fmt.Sprintf("  %s  ERROR: %s", displayName, br.Error)
	default:
		return fmt.Sprintf("  %s  %s", displayName, br.Outcome)
	}
}

// parseLayerGroups splits comma-separated layer group flags into groups.
// Each flag value becomes a group of layer names.
func parseLayerGroups(flags []string) [][]string {
	if len(flags) == 0 {
		return nil
	}
	groups := make([][]string, 0, len(flags))
	for _, flag := range flags {
		var names []string
		for _, name := range strings.Split(flag, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			groups = append(groups, names)
		}
	}
	return groups
}

// discoverBatchPlans finds plans based on the filter path.
// Returns the plan entries, a source description, and any error.
func discoverBatchPlans(planDirs []string, filterPath string) ([]config.PlanEntry, string, error) {
	if filterPath != "" && filepath.IsAbs(filterPath) {
		// Absolute path: treat as a standalone plan directory
		entries, err := config.ListPlans([]string{filterPath})
		return entries, filterPath, err
	}

	if len(planDirs) == 0 {
		return nil, "", fmt.Errorf("no plan directories configured")
	}

	entries, err := config.ListPlans(planDirs)
	if err != nil {
		return nil, "", err
	}

	if filterPath == "" {
		return entries, "all", nil
	}

	// Filter by relative prefix
	var filtered []config.PlanEntry
	for _, e := range entries {
		if strings.HasPrefix(e.Name, filterPath) {
			filtered = append(filtered, e)
		}
	}
	return filtered, filterPath, nil
}

// planBaseName returns the plan name without extension for display.
func planBaseName(name string) string {
	ext := filepath.Ext(name)
	if ext == ".yaml" || ext == ".yml" {
		return strings.TrimSuffix(name, ext)
	}
	return name
}

// formatDuration produces a human-friendly duration string.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
