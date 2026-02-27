package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/intent"
	"github.com/gburgyan/aat/plan"
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
		noDedup, _ := cmd.Flags().GetBool("no-dedup")
		shuffle, _ := cmd.Flags().GetBool("shuffle")
		seed, _ := cmd.Flags().GetInt64("seed")
		noAutoOverrides, _ := cmd.Flags().GetBool("no-auto-overrides")

		outputDir := resolveOutputDir(cmd.Flags().Changed("output"), getString("output"), resolved.ArchiveDir)

		ba := &batchArgs{
			runArgs: runArgs{
				EnvPath:         resolved.EnvPath,
				GraphPath:       resolved.GraphPath,
				TemplatesPath:   resolved.TemplatesPath,
				OutputDir:       outputDir,
				DomainPath:      resolved.DomainPath,
				JSON:            jsonFlag,
				Quiet:           quiet,
				Overrides:       overrideFlags,
				EnvOverlay:      envOverlay,
				MaxRetries:      retries,
				Layers:          layerFlags,
				LayersDir:       resolved.LayersDir,
				LayerGroups:     layerGroups,
				NoAutoOverrides: noAutoOverrides,
			},
			PlanDirs:   resolved.PlanDirs,
			FilterPath: filterPath,
			Parallel:   parallel,
			NoDedup:    noDedup,
			Shuffle:    shuffle,
			Seed:       seed,
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
	runBatchCmd.Flags().Bool("no-dedup", false, "disable duplicate plan detection across permutations")
	runBatchCmd.Flags().Bool("shuffle", false, "randomize plan execution order")
	runBatchCmd.Flags().Int64("seed", 0, "random seed for --shuffle (0 = use current time)")

	runCmd.AddCommand(runBatchCmd)
}

// batchArgs extends runArgs with batch-specific configuration.
type batchArgs struct {
	runArgs
	PlanDirs   []string
	FilterPath string // optional subdirectory filter
	Parallel   int    // concurrency limit; <=1 means sequential
	NoDedup    bool   // disable duplicate plan detection
	Shuffle    bool   // randomize plan execution order
	Seed       int64  // random seed for shuffle (0 = use current time)
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
	Attempts    int      `json:"attempts,omitempty"`     // total attempts (omitted if 1)
	Layers      []string `json:"layers,omitempty"`       // effective layers for this run
	Permutation string   `json:"permutation,omitempty"`  // permutation label for grouping
	Skipped     bool     `json:"skipped,omitempty"`      // true if skipped as a duplicate
	DuplicateOf string   `json:"duplicate_of,omitempty"` // display name of canonical run
}

// BatchStats is the aggregate counts in the batch JSON summary.
type BatchStats struct {
	TotalPlans   int   `json:"total_plans"`
	PassedPlans  int   `json:"passed_plans"`
	FailedPlans  int   `json:"failed_plans"`
	ErrorPlans   int   `json:"error_plans"`
	SkippedPlans int   `json:"skipped_plans,omitempty"`
	DurationMs   int64 `json:"duration_ms"`
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

	ti := DetectTerminal()
	color := ti.IsTTY

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
				_, _ = fmt.Fprintf(os.Stdout, "%s: %s%s\n", name, colorOutcome("PASSED", color), attemptSuffix)
			case "failed":
				_, _ = fmt.Fprintf(os.Stdout, "%s: %s: %s%s\n", name, colorOutcome("FAILED", color), r.Error, attemptSuffix)
			case "error":
				_, _ = fmt.Fprintf(os.Stdout, "%s: %s: %s%s\n", name, colorOutcome("ERROR", color), r.Error, attemptSuffix)
			case "skipped":
				_, _ = fmt.Fprintf(os.Stdout, "%s: %s (duplicate of %s)\n", name, colorOutcome("SKIPPED", color), r.DuplicateOf)
			}
		}
		_, _ = fmt.Fprintf(os.Stdout, "Batch: %d/%d %s",
			res.summary.Summary.PassedPlans, res.summary.Summary.TotalPlans, colorOutcome("PASSED", color))
		if res.summary.Summary.FailedPlans > 0 {
			_, _ = fmt.Fprintf(os.Stdout, ", %d %s", res.summary.Summary.FailedPlans, colorOutcome("FAILED", color))
		}
		if res.summary.Summary.ErrorPlans > 0 {
			_, _ = fmt.Fprintf(os.Stdout, ", %d %s", res.summary.Summary.ErrorPlans, colorOutcome("ERROR", color))
		}
		if res.summary.Summary.SkippedPlans > 0 {
			_, _ = fmt.Fprintf(os.Stdout, ", %d %s", res.summary.Summary.SkippedPlans, colorOutcome("SKIPPED", color))
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

	// 3a. Deduplicate specs when layer groups are present
	var skippedSpecs []skippedSpec
	if len(args.LayerGroups) > 0 && !args.NoDedup {
		dedup, dedupErr := deduplicateSpecs(rctx, specs)
		if dedupErr != nil {
			return &batchResult{setupErr: true, err: fmt.Errorf("deduplication: %w", dedupErr)}
		}
		if len(dedup.skipped) > 0 {
			logf("aat: dedup — %d duplicate permutations detected:\n", len(dedup.skipped))
			for _, s := range dedup.skipped {
				skippedName := specDisplayName(planBaseName(s.spec.entry.Name), s.spec.permutation)
				logf("  %s → duplicate of %s\n", skippedName, s.duplicateOf)
			}
			logf("\n")
			skippedSpecs = dedup.skipped
			specs = dedup.specs
		}
	}

	// 3b. Shuffle specs if requested
	if args.Shuffle {
		seed := args.Seed
		if seed == 0 {
			seed = time.Now().UnixNano()
		}
		rng := rand.New(rand.NewSource(seed))
		rng.Shuffle(len(specs), func(i, j int) { specs[i], specs[j] = specs[j], specs[i] })
		logf("aat: shuffled %d specs (seed=%d)\n", len(specs), seed)
	}

	// 4. Generate batch ID and create batch directory
	batchID := archive.GenerateBatchID()
	batchDir := filepath.Join(args.OutputDir, batchID)

	// Detect terminal for progress rendering and color
	ti := DetectTerminal()

	logf("\n")

	// 5. Execute plans
	batchStart := time.Now()
	var runs []BatchRunResult
	var batchEntries []archive.BatchRunEntry

	if args.Parallel > 1 {
		runs, batchEntries = batchParallel(ctx, rctx, specs, batchDir, args, out, ti)
	} else {
		runs, batchEntries = batchSequential(ctx, rctx, specs, batchDir, args, out, ti)
	}

	totalDur := time.Since(batchStart)

	// 6. Inject skipped entries
	for _, s := range skippedSpecs {
		skippedPlanName := planBaseName(s.spec.entry.Name)
		br := BatchRunResult{
			PlanName:    skippedPlanName,
			Outcome:     "skipped",
			Permutation: s.spec.permutation,
			Layers:      s.spec.layers,
			Skipped:     true,
			DuplicateOf: s.duplicateOf,
		}
		be := archive.BatchRunEntry{
			PlanName:    skippedPlanName,
			Outcome:     "skipped",
			Permutation: s.spec.permutation,
			Layers:      s.spec.layers,
			Skipped:     true,
			DuplicateOf: s.duplicateOf,
		}
		runs = append(runs, br)
		batchEntries = append(batchEntries, be)
	}

	// 7. Compute aggregate stats
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
		case "skipped":
			stats.SkippedPlans++
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

	// 8. Write batch.json
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
			SkippedRuns:     stats.SkippedPlans,
			TotalDurationMs: stats.DurationMs,
		},
	}

	batchJSONPath := filepath.Join(batchDir, "batch.json")
	if err := archive.WriteBatch(batchArchive, batchJSONPath); err != nil {
		logf("aat: warning: failed to write batch.json: %s\n", err)
	}

	// 9. Print aggregate summary
	logf("\nBatch: %d/%d PASSED", stats.PassedPlans, stats.TotalPlans)
	if stats.FailedPlans > 0 {
		logf(", %d FAILED", stats.FailedPlans)
	}
	if stats.ErrorPlans > 0 {
		logf(", %d ERROR", stats.ErrorPlans)
	}
	if stats.SkippedPlans > 0 {
		logf(", %d SKIPPED", stats.SkippedPlans)
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
func batchSequential(ctx context.Context, rctx *runContext, specs []batchRunSpec, batchDir string, args *batchArgs, out io.Writer, ti TerminalInfo) ([]BatchRunResult, []archive.BatchRunEntry) {
	var runs []BatchRunResult
	var batchEntries []archive.BatchRunEntry
	noopLogf := func(string, ...any) {}

	for i, spec := range specs {
		planName := planBaseName(spec.entry.Name)
		displayName := specDisplayName(planName, spec.permutation)

		// Create a shallow copy of rctx with per-spec layers
		specCtx := specRunContext(rctx, spec.layers)

		// Create streaming observer for sequential mode (unless output suppressed)
		var observer engine.ProgressObserver
		if out != io.Discard {
			observer = NewBatchStreamObserver(out, displayName, ti, i, len(specs))
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
func batchParallel(ctx context.Context, rctx *runContext, specs []batchRunSpec, batchDir string, args *batchArgs, out io.Writer, ti TerminalInfo) ([]BatchRunResult, []archive.BatchRunEntry) {
	n := len(specs)
	results := make([]runResult, n)
	displayNames := make([]string, n)
	planNames := make([]string, n)

	for i, spec := range specs {
		planNames[i] = planBaseName(spec.entry.Name)
		displayNames[i] = specDisplayName(planNames[i], spec.permutation)
	}

	var renderer *ProgressRenderer
	if out != io.Discard && ti.IsTTY {
		renderer = NewProgressRenderer(out, ti.Width, ti.IsTTY, n)
	}

	// Shared mutex for non-TTY output
	var outputMu sync.Mutex

	// Atomic counter for completed plan progress (parallel mode)
	var completedCount atomic.Int64

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

			// Build result line for completion with plan counter
			br, _ := buildPlanResult(planNames[i], spec.permutation, res)
			completed := completedCount.Add(1)
			counterPrefix := fmt.Sprintf("[%d/%d]", completed, n)
			resultLine := counterPrefix + formatBatchResultLine(displayNames[i], br, ti.IsTTY)

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
func formatBatchResultLine(displayName string, br BatchRunResult, color bool) string {
	dur := formatDuration(time.Duration(br.DurationMs) * time.Millisecond)
	switch br.Outcome {
	case "passed":
		return fmt.Sprintf("  %s  %s (%d steps, %s)", displayName, colorOutcome("PASSED", color), br.StepCount, dur)
	case "failed":
		return fmt.Sprintf("  %s  %s (%d steps, %s)", displayName, colorOutcome("FAILED", color), br.StepCount, dur)
	case "error":
		return fmt.Sprintf("  %s  %s: %s", displayName, colorOutcome("ERROR", color), br.Error)
	case "skipped":
		return fmt.Sprintf("  %s  %s (duplicate of %s)", displayName, colorOutcome("SKIPPED", color), br.DuplicateOf)
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

// dedupResult holds the outcome of duplicate detection across specs.
type dedupResult struct {
	specs   []batchRunSpec
	skipped []skippedSpec
}

// skippedSpec describes a spec that was identified as a duplicate of a canonical spec.
type skippedSpec struct {
	spec        batchRunSpec
	duplicateOf string // display name of the canonical spec
}

// instantiateForFingerprint mirrors the parse+reconstitute+instantiate logic from
// loadAndRunPlanToDir but stops before auth/execution. Returns the instantiated
// plan and the effective layers (needed for canonical scoring).
func instantiateForFingerprint(rctx *runContext, spec batchRunSpec) (*plan.Plan, []string, error) {
	parsed, err := plan.ParseAnyFile(spec.entry.FullPath)
	if err != nil {
		return nil, nil, err
	}

	var p *plan.Plan
	var effectiveLayers []string
	switch v := parsed.(type) {
	case *plan.Plan:
		p = v
		effectiveLayers = spec.layers
	case *plan.Recipe:
		// Merge CLI layers after recipe layers (same logic as loadAndRunPlanToDir)
		recipeLayers := make([]string, len(v.Selection.Layers))
		copy(recipeLayers, v.Selection.Layers)
		if len(spec.layers) > 0 {
			seen := make(map[string]bool, len(recipeLayers))
			for _, l := range recipeLayers {
				seen[l] = true
			}
			for _, l := range spec.layers {
				if !seen[l] {
					recipeLayers = append(recipeLayers, l)
				}
			}
			v.Selection.Layers = recipeLayers
		}
		effectiveLayers = v.Selection.Layers
		var reconOpts []intent.ReconstituteOption
		if rctx.LayersDir != "" {
			reconOpts = append(reconOpts, intent.WithLayersDir(rctx.LayersDir))
		}
		if rctx.AvailableLayers != nil {
			reconOpts = append(reconOpts, intent.WithAvailableLayers(rctx.AvailableLayers))
		}
		reconstituted, reconErr := intent.Reconstitute(v, rctx.Graph, rctx.GraphDir, reconOpts...)
		if reconErr != nil {
			return nil, nil, reconErr
		}
		p = reconstituted
	default:
		return nil, nil, fmt.Errorf("unexpected parse result type %T", parsed)
	}

	// Apply layers to get layered defaults, then instantiate
	var layeredDefaults map[string]*graph.InputDefault
	if len(effectiveLayers) > 0 && rctx.LayersDir != "" {
		available, loadErr := graph.ResolveLayerNames(effectiveLayers, rctx.LayersDir)
		if loadErr != nil {
			return nil, nil, loadErr
		}
		layeredDefaults, err = graph.ApplyLayers(rctx.Graph, effectiveLayers, available)
		if err != nil {
			return nil, nil, err
		}
	}

	inst := plan.InstantiateWithLayers(p, rctx.Graph, layeredDefaults)
	return inst, effectiveLayers, nil
}

// dedupScore computes a canonical selection score for a spec. Lower is better.
// Primary: symmetric difference between spec.layers and effectiveLayers.
// Tiebreaker: fewer effective layers = simpler = better.
func dedupScore(specLayers, effectiveLayers []string) int {
	specSet := make(map[string]bool, len(specLayers))
	for _, l := range specLayers {
		specSet[l] = true
	}
	effectiveSet := make(map[string]bool, len(effectiveLayers))
	for _, l := range effectiveLayers {
		effectiveSet[l] = true
	}

	// Symmetric difference: items in specSet but not effectiveSet, plus vice versa
	symDiff := 0
	for l := range specSet {
		if !effectiveSet[l] {
			symDiff++
		}
	}
	for l := range effectiveSet {
		if !specSet[l] {
			symDiff++
		}
	}

	return symDiff*100 + len(effectiveLayers)
}

// deduplicateSpecs identifies duplicate instantiated plans across specs and
// selects the most accurately-labeled permutation as canonical for each group.
// Specs from different plan files are never deduplicated against each other.
func deduplicateSpecs(rctx *runContext, specs []batchRunSpec) (*dedupResult, error) {
	type fingerprintInfo struct {
		fingerprint     string
		effectiveLayers []string
		index           int
		err             error
	}

	infos := make([]fingerprintInfo, len(specs))
	for i, spec := range specs {
		inst, effectiveLayers, err := instantiateForFingerprint(rctx, spec)
		if err != nil {
			infos[i] = fingerprintInfo{index: i, err: err}
			continue
		}
		fp := plan.Fingerprint(inst)
		infos[i] = fingerprintInfo{
			fingerprint:     fp,
			effectiveLayers: effectiveLayers,
			index:           i,
		}
	}

	// Group by (planFilePath, fingerprint). Specs with errors are always unique.
	type groupKey struct {
		planFile    string
		fingerprint string
	}
	groups := make(map[groupKey][]int) // key → indices into specs
	for i, info := range infos {
		if info.err != nil {
			continue // errors are included as unique
		}
		key := groupKey{
			planFile:    specs[i].entry.FullPath,
			fingerprint: info.fingerprint,
		}
		groups[key] = append(groups[key], i)
	}

	// For each group with >1 member, pick the canonical spec (lowest score).
	isSkipped := make(map[int]string) // index → canonical display name
	for _, indices := range groups {
		if len(indices) <= 1 {
			continue
		}

		// Find canonical: lowest score
		bestIdx := indices[0]
		bestScore := dedupScore(specs[bestIdx].layers, infos[bestIdx].effectiveLayers)
		for _, idx := range indices[1:] {
			score := dedupScore(specs[idx].layers, infos[idx].effectiveLayers)
			if score < bestScore {
				bestScore = score
				bestIdx = idx
			}
		}

		canonicalName := specDisplayName(
			planBaseName(specs[bestIdx].entry.Name),
			specs[bestIdx].permutation,
		)

		for _, idx := range indices {
			if idx != bestIdx {
				isSkipped[idx] = canonicalName
			}
		}
	}

	result := &dedupResult{}
	for i, spec := range specs {
		if canonName, skipped := isSkipped[i]; skipped {
			result.skipped = append(result.skipped, skippedSpec{
				spec:        spec,
				duplicateOf: canonName,
			})
		} else {
			result.specs = append(result.specs, spec)
		}
	}

	return result, nil
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
