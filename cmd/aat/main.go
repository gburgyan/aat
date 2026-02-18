package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/llm"
	"github.com/gburgyan/aat/plan"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: aat <command> [options]")
		fmt.Fprintln(os.Stderr, "commands: run, prompt, graph, plan, generate, docs, mcp")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		code := runMain(os.Args[2:])
		os.Exit(code)
	case "prompt":
		code := promptMain(os.Args[2:])
		os.Exit(code)
	case "graph":
		code := graphMain(os.Args[2:])
		os.Exit(code)
	case "plan":
		code := planMain(os.Args[2:])
		os.Exit(code)
	case "generate":
		code := generateMain(os.Args[2:])
		os.Exit(code)
	case "docs":
		code := docsMain(os.Args[2:])
		os.Exit(code)
	case "mcp":
		code := mcpMain(os.Args[2:])
		os.Exit(code)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		fmt.Fprintln(os.Stderr, "commands: run, prompt, graph, plan, generate, docs, mcp")
		os.Exit(1)
	}
}

// runArgs holds parsed CLI flags for the run command.
type runArgs struct {
	PlanPath      string
	EnvPath       string
	GraphPath     string
	TemplatesPath string
	OutputDir     string
	Mode          string
	DomainPath    string
	JSON          bool
	Quiet         bool
}

// RunSummary is the machine-readable JSON output for CI/CD pipelines.
type RunSummary struct {
	Outcome     string        `json:"outcome"`
	Error       string        `json:"error,omitempty"`
	Steps       []StepSummary `json:"steps"`
	Cleanup     []StepSummary `json:"cleanup,omitempty"`
	Summary     SummaryStats  `json:"summary"`
	ArchivePath string        `json:"archive_path,omitempty"`
}

// StepSummary is a per-step entry in the JSON summary.
type StepSummary struct {
	Name             string `json:"name"`
	Node             string `json:"node"`
	Status           int    `json:"status"`
	DurationMs       int64  `json:"duration_ms"`
	Passed           bool   `json:"passed"`
	Error            string `json:"error,omitempty"`
	Retries          int    `json:"retries"`
	AssertionsPassed int    `json:"assertions_passed"`
	AssertionsFailed int    `json:"assertions_failed"`
}

// SummaryStats is the aggregate counts in the JSON summary.
type SummaryStats struct {
	TotalSteps  int   `json:"total_steps"`
	PassedSteps int   `json:"passed_steps"`
	FailedSteps int   `json:"failed_steps"`
	DurationMs  int64 `json:"duration_ms"`
}

// runResult is the internal result from runCommand, used by runMain
// to determine exit codes and output format.
type runResult struct {
	outcome     engine.Outcome
	summary     *RunSummary
	archivePath string
	err         error
	setupErr    bool // true when error occurred before engine execution
}

// exitCodeInfra is the exit code for infrastructure/config errors.
const exitCodeInfra = 2

// runMain parses flags and delegates to runCommand.
func runMain(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	ra := &runArgs{}
	fs.StringVar(&ra.PlanPath, "plan", "", "path to plan YAML file (required)")
	fs.StringVar(&ra.EnvPath, "env", "", "path to environment YAML file (required)")
	fs.StringVar(&ra.GraphPath, "graph", "", "path to graph YAML file (required)")
	fs.StringVar(&ra.TemplatesPath, "templates", "", "path to templates directory (required)")
	fs.StringVar(&ra.OutputDir, "output", "runs", "directory for archive output")
	fs.StringVar(&ra.Mode, "mode", "", "execution mode: strict, lean, adaptive (overrides env config)")
	fs.StringVar(&ra.DomainPath, "domain", "", "path to domain knowledge YAML file")
	fs.BoolVar(&ra.JSON, "json", false, "output machine-readable JSON summary to stdout")
	fs.BoolVar(&ra.Quiet, "quiet", false, "suppress progress messages, show only final summary")

	if err := fs.Parse(args); err != nil {
		return exitCodeInfra
	}

	// --json implies --quiet
	if ra.JSON {
		ra.Quiet = true
	}

	// Choose output writer: --quiet suppresses progress
	var out io.Writer = os.Stdout
	if ra.Quiet {
		out = io.Discard
	}

	res := runCommand(context.Background(), ra, out)

	// JSON output
	if ra.JSON {
		if res.summary != nil {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(res.summary)
		} else {
			// Setup error before we got a RunResult — emit minimal JSON
			s := &RunSummary{
				Outcome: "error",
				Error:   errString(res.err),
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(s)
		}
		return exitCode(res)
	}

	// Quiet (non-JSON): show the final summary line
	if ra.Quiet && res.summary != nil {
		switch res.summary.Outcome {
		case "passed":
			fmt.Fprintf(os.Stdout, "PASSED (%d/%d steps)\n", res.summary.Summary.PassedSteps, res.summary.Summary.TotalSteps)
		case "failed":
			fmt.Fprintf(os.Stdout, "FAILED: %s\n", res.summary.Error)
		case "error":
			fmt.Fprintf(os.Stdout, "ERROR: %s\n", res.summary.Error)
		}
		if res.archivePath != "" {
			fmt.Fprintf(os.Stdout, "Archive: %s\n", res.archivePath)
		}
		return exitCode(res)
	}

	// Normal (non-quiet, non-JSON): errors already printed during execution
	if res.err != nil {
		fmt.Fprintf(os.Stderr, "aat: %s\n", res.err)
	}
	return exitCode(res)
}

// exitCode maps a runResult to the appropriate process exit code.
func exitCode(res *runResult) int {
	// Setup/config errors that occurred before engine execution
	if res.setupErr {
		return exitCodeInfra
	}
	switch res.outcome {
	case engine.OutcomePassed:
		return 0
	case engine.OutcomeFailed:
		return 1
	case engine.OutcomeError:
		return exitCodeInfra
	default:
		return 0
	}
}

// runCommand executes the full run pipeline. Extracted for testability.
// The out writer receives progress messages; callers pass io.Discard for quiet mode.
func runCommand(ctx context.Context, args *runArgs, out io.Writer) *runResult {
	logf := func(format string, a ...any) {
		fmt.Fprintf(out, format, a...)
	}

	// Validate required flags
	if args.PlanPath == "" {
		return &runResult{setupErr: true, err: fmt.Errorf("--plan is required")}
	}
	if args.EnvPath == "" {
		return &runResult{setupErr: true, err: fmt.Errorf("--env is required")}
	}
	if args.GraphPath == "" {
		return &runResult{setupErr: true, err: fmt.Errorf("--graph is required")}
	}
	if args.TemplatesPath == "" {
		return &runResult{setupErr: true, err: fmt.Errorf("--templates is required")}
	}

	// 1. Load environment
	logf("aat: loading environment...\n")
	env, err := config.LoadEnvironment(args.EnvPath)
	if err != nil {
		return &runResult{setupErr: true, err: fmt.Errorf("loading environment: %w", err)}
	}
	logf("aat: loaded environment %q\n", env.Name)

	// 2. Authenticate
	apiConfig, err := env.BuildAPIConfig(ctx)
	if err != nil {
		return &runResult{setupErr: true, err: fmt.Errorf("building API config: %w", err)}
	}
	logf("aat: authenticated via %s\n", env.Auth.Type)

	// 3. Load graph
	g, err := graph.ParseFile(args.GraphPath)
	if err != nil {
		return &runResult{setupErr: true, err: fmt.Errorf("loading graph: %w", err)}
	}
	logf("aat: loaded graph (%d nodes)\n", len(g.Nodes))

	// 4. Load plan
	p, err := plan.ParseFile(args.PlanPath)
	if err != nil {
		return &runResult{setupErr: true, err: fmt.Errorf("loading plan: %w", err)}
	}

	// 5. Validate plan against graph (early validation for clear CI error reporting)
	if err := plan.Validate(p, g); err != nil {
		return &runResult{setupErr: true, err: fmt.Errorf("plan validation: %w", err)}
	}

	// 6. Load domain knowledge (optional)
	var kb *domain.KnowledgeBase
	if args.DomainPath != "" {
		kb, err = domain.ParseFile(args.DomainPath)
		if err != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("loading domain knowledge: %w", err)}
		}
		logf("aat: loaded domain knowledge\n")
	}

	// 7. Load templates
	registry := adapter.NewRegistry()
	count, err := adapter.LoadTemplates(args.TemplatesPath, registry)
	if err != nil {
		return &runResult{setupErr: true, err: fmt.Errorf("loading templates: %w", err)}
	}
	logf("aat: loaded %d templates\n", count)

	// 8. Create executor and environment config
	executor := adapter.NewHTTPExecutor(apiConfig.BaseURL)
	envConfig := &adapter.EnvironmentConfig{
		BaseURL: apiConfig.BaseURL,
		Headers: apiConfig.Headers,
		Values:  apiConfig.Values,
	}

	// 9. Determine execution mode
	effectiveMode := config.ExecutionMode(args.Mode)
	if effectiveMode == "" {
		effectiveMode = env.LLM.Mode
	}
	if effectiveMode == "" {
		effectiveMode = config.ModeStrict
	}

	// 10. Create LLM client if mode requires it
	var llmClient llm.Client
	if effectiveMode != config.ModeStrict && env.LLM.Endpoint != "" {
		llmClient, err = llm.NewClient(env.LLM)
		if err != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("creating LLM client: %w", err)}
		}
	}

	// 11. Create engine and run
	eng := engine.NewEngine(g, registry, executor, envConfig).
		WithMode(effectiveMode).
		WithDomain(kb).
		WithLLM(llmClient).
		WithMaxRelaxationDepth(env.Settings.MaxRelaxationDepth)
	logf("aat: executing plan (%d steps, mode=%s)...\n\n", len(p.Execution.Steps), effectiveMode)

	result := eng.Run(ctx, p)

	// Print human-readable summary (suppressed in quiet mode via out=Discard)
	printRunSummary(result, out)

	// Write archive
	archivePath, archiveErr := writeRunArchive(result, p, env, g, args.OutputDir)
	if archiveErr != nil {
		logf("aat: warning: %s\n", archiveErr)
	} else {
		logf("Archive: %s\n", archivePath)
	}

	// Build machine-readable summary
	summary := buildRunSummary(result, archivePath)

	return &runResult{
		outcome:     result.Outcome,
		summary:     summary,
		archivePath: archivePath,
		err:         result.Error,
	}
}

// buildRunSummary converts an engine.RunResult to a RunSummary.
func buildRunSummary(result *engine.RunResult, archivePath string) *RunSummary {
	s := &RunSummary{
		Outcome:     result.Outcome.String(),
		Error:       errString(result.Error),
		ArchivePath: archivePath,
	}

	var totalDur time.Duration
	var passed, failed int

	for _, step := range result.Steps {
		ss := toStepSummary(step)
		s.Steps = append(s.Steps, ss)
		totalDur += step.Duration
		if ss.Passed {
			passed++
		} else {
			failed++
		}
	}

	for _, step := range result.CleanupResults {
		ss := toStepSummary(step)
		s.Cleanup = append(s.Cleanup, ss)
		totalDur += step.Duration
	}

	s.Summary = SummaryStats{
		TotalSteps:  len(result.Steps),
		PassedSteps: passed,
		FailedSteps: failed,
		DurationMs:  totalDur.Milliseconds(),
	}

	return s
}

// toStepSummary converts a single engine.StepResult to a StepSummary.
func toStepSummary(step engine.StepResult) StepSummary {
	ss := StepSummary{
		Name:       step.Node,
		Node:       step.Node,
		Status:     step.StatusCode,
		DurationMs: step.Duration.Milliseconds(),
		Retries:    step.RetryCount,
	}

	// Determine passed/failed
	if step.Error != nil {
		ss.Error = step.Error.Error()
		ss.Passed = false
	} else if step.ExpectFailure != nil {
		ss.Passed = step.ExpectFailure.Passed
		if !ss.Passed {
			ss.Error = fmt.Sprintf("expected status %v, got %d", step.ExpectFailure.ExpectedStatuses, step.ExpectFailure.ActualStatus)
		}
	} else if step.StatusCode >= 400 {
		ss.Passed = false
		ss.Error = fmt.Sprintf("status %d", step.StatusCode)
	} else {
		ss.Passed = true
	}

	// Assertion counts
	if step.Validation != nil {
		for _, ar := range step.Validation.Results {
			if ar.Passed {
				ss.AssertionsPassed++
			} else {
				ss.AssertionsFailed++
				ss.Passed = false
			}
		}
	}

	return ss
}

// printRunSummary writes a human-readable step-by-step summary to the given writer.
func printRunSummary(result *engine.RunResult, out io.Writer) {
	total := len(result.Steps)
	for i, step := range result.Steps {
		prefix := fmt.Sprintf("  [%d/%d] %-20s", i+1, total, step.Node)
		if step.Error != nil {
			if step.RetryCount > 0 {
				fmt.Fprintf(out, "%s ERROR [%s] (after %d retries)\n", prefix, errorCategory(step), step.RetryCount)
			} else {
				fmt.Fprintf(out, "%s ERROR: %s\n", prefix, step.Error)
			}
		} else if step.Response != nil {
			status := fmt.Sprintf("%d", step.StatusCode)
			validMark := ""
			if step.Validation != nil && !step.Validation.Passed {
				validMark = "  ASSERTIONS FAILED"
			}
			fmt.Fprintf(out, "%s %s  %dms%s\n", prefix, status, step.Duration.Milliseconds(), validMark)
		} else {
			fmt.Fprintf(out, "%s (no response)\n", prefix)
		}
	}

	if len(result.CleanupResults) > 0 {
		fmt.Fprintln(out, "\n  cleanup:")
		for _, step := range result.CleanupResults {
			prefix := fmt.Sprintf("    %-22s", step.Node)
			if step.Error != nil {
				fmt.Fprintf(out, "%s ERROR: %s\n", prefix, step.Error)
			} else if step.Response != nil {
				fmt.Fprintf(out, "%s %d  %dms\n", prefix, step.StatusCode, step.Duration.Milliseconds())
			} else {
				fmt.Fprintf(out, "%s (no response)\n", prefix)
			}
		}
	}

	fmt.Fprintln(out)
	switch result.Outcome {
	case engine.OutcomePassed:
		fmt.Fprintf(out, "PASSED (%d/%d steps, %s)\n", total, total, totalDuration(result))
	case engine.OutcomeFailed:
		fmt.Fprintf(out, "FAILED: %s\n", outcomeMessage(result))
	case engine.OutcomeError:
		fmt.Fprintf(out, "ERROR: %s\n", outcomeMessage(result))
	}
}

// errorCategory returns the error classification category string for a step.
func errorCategory(step engine.StepResult) string {
	if step.ErrorClass != nil {
		return step.ErrorClass.Category.String()
	}
	return "unknown"
}

// totalDuration sums the duration of all steps.
func totalDuration(result *engine.RunResult) string {
	var total time.Duration
	for _, s := range result.Steps {
		total += s.Duration
	}
	for _, s := range result.CleanupResults {
		total += s.Duration
	}
	if total < time.Second {
		return fmt.Sprintf("%dms", total.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", total.Seconds())
}

// outcomeMessage produces a summary error string.
func outcomeMessage(result *engine.RunResult) string {
	if result.Error != nil {
		return result.Error.Error()
	}
	return result.Outcome.String()
}

// errString returns the error string or empty.
func errString(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
