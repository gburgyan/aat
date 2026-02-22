package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/engine"
	"github.com/spf13/cobra"
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
		mode, _ := cmd.Flags().GetString("mode")

		outputDir := resolveOutputDir(cmd.Flags().Changed("output"), getString("output"), resolved.ArchiveDir)

		ba := &batchArgs{
			runArgs: runArgs{
				EnvPath:       resolved.EnvPath,
				GraphPath:     resolved.GraphPath,
				TemplatesPath: resolved.TemplatesPath,
				OutputDir:     outputDir,
				Mode:          mode,
				DomainPath:    resolved.DomainPath,
				JSON:          jsonFlag,
				Quiet:         quiet,
				Overrides:     overrideFlags,
				EnvOverlay:    envOverlay,
			},
			PlanDirs:   resolved.PlanDirs,
			FilterPath: filterPath,
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

	runCmd.AddCommand(runBatchCmd)
}

// batchArgs extends runArgs with batch-specific configuration.
type batchArgs struct {
	runArgs
	PlanDirs   []string
	FilterPath string // optional subdirectory filter
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
	PlanName    string `json:"plan_name"`
	Outcome     string `json:"outcome"`
	StepCount   int    `json:"step_count"`
	PassedSteps int    `json:"passed_steps"`
	FailedSteps int    `json:"failed_steps"`
	DurationMs  int64  `json:"duration_ms"`
	Error       string `json:"error,omitempty"`
	ArchivePath string `json:"archive_path,omitempty"`
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
	summary     *BatchSummary
	batchDir    string
	err         error
	setupErr    bool
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
			enc.Encode(res.summary)
		} else {
			s := &BatchSummary{
				Outcome: "error",
			}
			if res.err != nil {
				s.Runs = []BatchRunResult{{PlanName: "", Outcome: "error", Error: res.err.Error()}}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(s)
		}
		return batchExitCode(res)
	}

	if ba.Quiet && res.summary != nil {
		for _, r := range res.summary.Runs {
			switch r.Outcome {
			case "passed":
				fmt.Fprintf(os.Stdout, "%s: PASSED\n", r.PlanName)
			case "failed":
				fmt.Fprintf(os.Stdout, "%s: FAILED: %s\n", r.PlanName, r.Error)
			case "error":
				fmt.Fprintf(os.Stdout, "%s: ERROR: %s\n", r.PlanName, r.Error)
			}
		}
		fmt.Fprintf(os.Stdout, "Batch: %d/%d PASSED",
			res.summary.Summary.PassedPlans, res.summary.Summary.TotalPlans)
		if res.summary.Summary.FailedPlans > 0 {
			fmt.Fprintf(os.Stdout, ", %d FAILED", res.summary.Summary.FailedPlans)
		}
		if res.summary.Summary.ErrorPlans > 0 {
			fmt.Fprintf(os.Stdout, ", %d ERROR", res.summary.Summary.ErrorPlans)
		}
		fmt.Fprintln(os.Stdout)
		if res.batchDir != "" {
			fmt.Fprintf(os.Stdout, "Archive: %s\n", res.batchDir)
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

// batchCommand executes the batch pipeline.
func batchCommand(ctx context.Context, args *batchArgs, out io.Writer) *batchResult {
	logf := func(format string, a ...any) {
		fmt.Fprintf(out, format, a...)
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

	logf("aat: batch run — %d plans\n\n", len(plans))

	// 2. Load shared infrastructure once
	rctx, err := loadRunContext(ctx, &args.runArgs, logf)
	if err != nil {
		return &batchResult{setupErr: true, err: err}
	}

	// 3. Generate batch ID and create batch directory
	batchID := archive.GenerateBatchID()
	batchDir := filepath.Join(args.OutputDir, batchID)

	logf("\n")

	// 4. Execute each plan
	batchStart := time.Now()
	var runs []BatchRunResult
	var batchEntries []archive.BatchRunEntry

	for i, entry := range plans {
		planName := planBaseName(entry.Name)
		logf("  [%d/%d] %s", i+1, len(plans), planName)

		// Create per-plan output directory inside the batch directory
		planOutputDir := batchDir

		// Create a quiet observer for batch (suppress per-step output)
		var observer engine.ProgressObserver
		noopLogf := func(string, ...any) {}

		res := loadAndRunPlan(ctx, rctx, entry.FullPath, planOutputDir, observer, noopLogf)

		// Build per-plan results
		br := BatchRunResult{
			PlanName:    planName,
			ArchivePath: res.archivePath,
		}
		be := archive.BatchRunEntry{
			PlanName: planName,
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

		runs = append(runs, br)
		batchEntries = append(batchEntries, be)

		// Print per-plan summary line
		dur := formatDuration(time.Duration(br.DurationMs) * time.Millisecond)
		switch br.Outcome {
		case "passed":
			logf("  PASSED (%d steps, %s)\n", br.StepCount, dur)
		case "failed":
			logf("  FAILED (%d steps, %s)\n", br.StepCount, dur)
		case "error":
			logf("  ERROR: %s\n", br.Error)
		default:
			logf("  %s\n", br.Outcome)
		}
	}

	totalDur := time.Since(batchStart)

	// 5. Compute aggregate stats
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

	// 6. Write batch.json
	batchArchive := &archive.BatchArchive{
		Metadata: archive.BatchMetadata{
			Version:     "1.0.0",
			BatchID:     batchID,
			Timestamp:   batchStart,
			Source:      source,
			ToolVersion: "0.1.0",
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

	// 7. Print aggregate summary
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
