package main

import (
	"context"
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
		jsonFlag, _ := cmd.Flags().GetBool("json")
		dumpState, _ := cmd.Flags().GetString("dump-state")
		dumpStateSecrets, _ := cmd.Flags().GetBool("dump-state-secrets")
		if dumpStateSecrets && dumpState == "" {
			return runSetupFailure(jsonFlag, fmt.Errorf("--dump-state-secrets requires --dump-state"))
		}

		overrides := buildProjectOverrides(changed, getString)
		resolved, err := config.ResolveProjectPaths(overrides)
		if err != nil {
			return runSetupFailure(jsonFlag, err)
		}

		planPath := resolvePlanPath(args[0], resolved.PlanDirs)

		quiet, _ := cmd.Flags().GetBool("quiet")
		overrideFlags, _ := cmd.Flags().GetStringSlice("override")
		envOverlay, _ := cmd.Flags().GetString("overlay")
		retries, _ := cmd.Flags().GetInt("retries")
		layerFlags, _ := cmd.Flags().GetStringSlice("layer")
		noAutoOverrides, _ := cmd.Flags().GetBool("no-auto-overrides")
		oasValidate, _ := cmd.Flags().GetString("oas-validate")
		verboseAuth, _ := cmd.Flags().GetBool("verbose-auth")
		noMutations, _ := cmd.Flags().GetBool("no-mutations")
		varFlags, _ := cmd.Flags().GetStringArray("var")
		vars, err := config.ParseVars(varFlags)
		if err != nil {
			return runSetupFailure(jsonFlag, err)
		}
		stopAfter, _ := cmd.Flags().GetString("stop-after")

		envName, err := selectEnvName(cmd, resolved, envOverlay, noAutoOverrides)
		if err != nil {
			return runSetupFailure(jsonFlag, err)
		}

		outputDir := resolveOutputDir(cmd.Flags().Changed("output"), getString("output"), resolved.ArchiveDir)

		ra := &runArgs{
			PlanPath:         planPath,
			EnvPath:          resolved.EnvPath,
			EnvName:          envName,
			GraphPath:        resolved.GraphPath,
			TemplatesPath:    resolved.TemplatesPath,
			OutputDir:        outputDir,
			DomainPath:       resolved.DomainPath,
			JSON:             jsonFlag,
			Quiet:            quiet,
			Overrides:        overrideFlags,
			EnvOverlay:       envOverlay,
			MaxRetries:       retries,
			Layers:           layerFlags,
			LayersDir:        resolved.LayersDir,
			NoAutoOverrides:  noAutoOverrides,
			OASValidateMode:  oasValidate,
			VerboseAuth:      verboseAuth,
			SkipMutations:    noMutations,
			StopAfterStep:    stopAfter,
			DumpStatePath:    dumpState,
			DumpStateSecrets: dumpStateSecrets,
			Vars:             vars,
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
	runPlanCmd.Flags().String("stop-after", "", "stop execution after the named step (by step ID); cleanup is skipped so resources stay alive for handoff")
	runPlanCmd.Flags().String("dump-state", "", "write accumulated run state (base URLs, request headers, step inputs and outputs, with credentials redacted) to FILE (mode 0600); use \"-\" for stdout, which then carries only the state (progress goes to stderr; with --json it is nested under \"state\")")
	runPlanCmd.Flags().Bool("dump-state-secrets", false, "keep live credentials in the --dump-state output, for a harness that sends requests as the run's session")
}

// executeRun handles output modes (JSON/quiet/normal) and returns an exit code.
// This is the entry point called by the Cobra RunE and by tests.
func executeRun(ra *runArgs) int {
	// --json implies --quiet
	if ra.JSON {
		ra.Quiet = true
	}

	// Live credentials on stdout reach terminals, CI logs, and transcripts.
	if ra.DumpStateSecrets && ra.DumpStatePath == "-" {
		fmt.Fprintln(os.Stderr, "aat: warning: --dump-state-secrets writes live credentials to stdout")
	}

	// console receives progress and the summary line. With --dump-state - (and
	// no --json) stdout carries only the state export, so they go to stderr and
	// the export pipes cleanly into another tool. Colour follows the console.
	console := os.Stdout
	if ra.DumpStatePath == "-" && !ra.JSON {
		console = os.Stderr
	}
	ti := DetectTerminal(console)
	color := ti.IsTTY

	// Choose output writer: --quiet suppresses progress
	var out io.Writer = console
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
			writeJSON(res.summary)
		} else {
			// Setup error before we got a RunResult — emit minimal JSON
			writeJSON(&RunSummary{Outcome: "error", Error: errString(res.err)})
			if res.err != nil {
				fmt.Fprintf(os.Stderr, "aat: %s\n", res.err)
			}
		}
		return exitCode(res)
	}

	// Non-JSON stdout dump (--dump-state -): emit the state export as its own
	// JSON object. In --json mode it is already nested under summary.state.
	if ra.DumpStatePath == "-" && res.summary != nil && res.summary.State != nil {
		writeJSON(res.summary.State)
	}

	// Quiet (non-JSON): show the final summary line
	if ra.Quiet && res.summary != nil {
		attemptSuffix := ""
		if res.summary.Attempts > 1 {
			attemptSuffix = fmt.Sprintf(" (%d attempts)", res.summary.Attempts)
		}
		switch res.summary.Outcome {
		case "passed":
			_, _ = fmt.Fprintf(console, "%s (%d/%d steps)%s\n", colorOutcome("PASSED", color), res.summary.Summary.PassedSteps, res.summary.Summary.TotalSteps, attemptSuffix)
		case "failed":
			_, _ = fmt.Fprintf(console, "%s: %s%s\n", colorOutcome("FAILED", color), res.summary.Error, attemptSuffix)
		case "error":
			_, _ = fmt.Fprintf(console, "%s: %s%s\n", colorOutcome("ERROR", color), res.summary.Error, attemptSuffix)
		case "aborted":
			_, _ = fmt.Fprintf(console, "%s (%d/%d steps)%s\n", colorOutcome("ABORTED", color), res.summary.Summary.PassedSteps, res.summary.Summary.TotalSteps, attemptSuffix)
		case "stopped":
			_, _ = fmt.Fprintf(console, "%s (%d/%d steps)%s\n", colorOutcome("STOPPED", color), res.summary.Summary.PassedSteps, res.summary.Summary.TotalSteps, attemptSuffix)
		}
		if res.archivePath != "" {
			_, _ = fmt.Fprintf(console, "Archive: %s\n", res.archivePath)
		}
		return exitCode(res)
	}

	// Normal (non-quiet, non-JSON): once the engine ran, the progress output has
	// already printed the outcome and its error; print only errors from before.
	if res.err != nil && res.summary == nil {
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
