package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/engine"
	"github.com/spf13/cobra"
)

// runPlanCmd executes a single pre-written test plan.
var runPlanCmd = &cobra.Command{
	Use:   "plan <name-or-path>",
	Short: "Execute a single test plan",
	Long:  "Execute an API test plan against a configured environment. The plan can be specified by name (resolved via plan directories) or by path.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		changed := func(name string) bool { return cmd.Flags().Changed(name) }
		getString := func(name string) string { v, _ := cmd.Flags().GetString(name); return v }

		overrides := buildProjectOverrides(changed, getString)
		resolved, err := config.ResolveProjectPaths(overrides)
		if err != nil {
			return err
		}

		planPath := resolvePlanPath(args[0], resolved.PlanDirs)

		jsonFlag, _ := cmd.Flags().GetBool("json")
		quiet, _ := cmd.Flags().GetBool("quiet")
		overrideFlags, _ := cmd.Flags().GetStringSlice("override")
		envOverlay, _ := cmd.Flags().GetString("overlay")
		envName := resolveEnvName(cmd)
		retries, _ := cmd.Flags().GetInt("retries")
		layerFlags, _ := cmd.Flags().GetStringSlice("layer")
		noAutoOverrides, _ := cmd.Flags().GetBool("no-auto-overrides")
		oasValidate, _ := cmd.Flags().GetString("oas-validate")
		verboseAuth, _ := cmd.Flags().GetBool("verbose-auth")

		outputDir := resolveOutputDir(cmd.Flags().Changed("output"), getString("output"), resolved.ArchiveDir)

		ra := &runArgs{
			PlanPath:        planPath,
			EnvPath:         resolved.EnvPath,
			EnvName:         resolveEnvNameWithDefault(envName, resolved.DefaultEnvName),
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
			NoAutoOverrides: noAutoOverrides,
			OASValidateMode: oasValidate,
			VerboseAuth:     verboseAuth,
		}

		code := executeRun(ra)
		if code != 0 {
			return &exitError{Code: code}
		}
		return nil
	},
}

func init() {
	// Local flags specific to single-plan execution
	runPlanCmd.Flags().Bool("json", false, "output machine-readable JSON summary to stdout")
	runPlanCmd.Flags().Bool("quiet", false, "suppress progress messages, show only final summary")
}

// executeRun handles output modes (JSON/quiet/normal) and returns an exit code.
// This is the entry point called by the Cobra RunE and by tests.
func executeRun(ra *runArgs) int {
	// --json implies --quiet
	if ra.JSON {
		ra.Quiet = true
	}

	ti := DetectTerminal()
	color := ti.IsTTY

	// Choose output writer: --quiet suppresses progress
	var out io.Writer = os.Stdout
	if ra.Quiet {
		out = io.Discard
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if ra.VerboseAuth {
		ctx = config.WithAuthVerbose(ctx, os.Stderr)
	}

	res := runCommand(ctx, ra, out, ti)

	if res.outcome == engine.OutcomeAborted {
		fmt.Fprintln(os.Stderr, "aat: interrupted, writing partial results...")
	}

	// JSON output
	if ra.JSON {
		if res.summary != nil {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(res.summary)
		} else {
			// Setup error before we got a RunResult — emit minimal JSON
			s := &RunSummary{
				Outcome: "error",
				Error:   errString(res.err),
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(s)
		}
		return exitCode(res)
	}

	// Quiet (non-JSON): show the final summary line
	if ra.Quiet && res.summary != nil {
		attemptSuffix := ""
		if res.summary.Attempts > 1 {
			attemptSuffix = fmt.Sprintf(" (%d attempts)", res.summary.Attempts)
		}
		switch res.summary.Outcome {
		case "passed":
			_, _ = fmt.Fprintf(os.Stdout, "%s (%d/%d steps)%s\n", colorOutcome("PASSED", color), res.summary.Summary.PassedSteps, res.summary.Summary.TotalSteps, attemptSuffix)
		case "failed":
			_, _ = fmt.Fprintf(os.Stdout, "%s: %s%s\n", colorOutcome("FAILED", color), res.summary.Error, attemptSuffix)
		case "error":
			_, _ = fmt.Fprintf(os.Stdout, "%s: %s%s\n", colorOutcome("ERROR", color), res.summary.Error, attemptSuffix)
		case "aborted":
			_, _ = fmt.Fprintf(os.Stdout, "%s (%d/%d steps)%s\n", colorOutcome("ABORTED", color), res.summary.Summary.PassedSteps, res.summary.Summary.TotalSteps, attemptSuffix)
		}
		if res.archivePath != "" {
			_, _ = fmt.Fprintf(os.Stdout, "Archive: %s\n", res.archivePath)
		}
		return exitCode(res)
	}

	// Normal (non-quiet, non-JSON): errors already printed during execution
	if res.err != nil {
		fmt.Fprintf(os.Stderr, "aat: %s\n", res.err)
	}
	return exitCode(res)
}

// runCommand executes the full run pipeline. Extracted for testability.
// The out writer receives progress messages; callers pass io.Discard for quiet mode.
func runCommand(ctx context.Context, args *runArgs, out io.Writer, ti TerminalInfo) *runResult {
	logf := func(format string, a ...any) {
		_, _ = fmt.Fprintf(out, format, a...)
	}

	// Validate required fields
	if args.PlanPath == "" {
		return &runResult{setupErr: true, err: fmt.Errorf("plan path is required")}
	}

	// Load shared infrastructure
	rctx, err := loadRunContext(ctx, args, logf)
	if err != nil {
		return &runResult{setupErr: true, err: err}
	}

	// Create observer
	var observer engine.ProgressObserver
	if out != io.Discard {
		observer = &CLIProgressObserver{out: out, term: ti}
	}

	if args.MaxRetries > 0 {
		return loadAndRunPlanWithRetries(ctx, rctx, args.PlanPath, args.OutputDir, args.MaxRetries, observer, logf)
	}
	return loadAndRunPlan(ctx, rctx, args.PlanPath, args.OutputDir, observer, logf)
}
