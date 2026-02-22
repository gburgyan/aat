package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/intent"
	"github.com/gburgyan/aat/llm"
	"github.com/gburgyan/aat/plan"
)

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
	Overrides     []string // "nodeName=http://url" pairs
	EnvOverlay    string   // path to overlay YAML
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
	Name             string               `json:"name"`
	Node             string               `json:"node"`
	Status           int                  `json:"status"`
	DurationMs       int64                `json:"duration_ms"`
	Passed           bool                 `json:"passed"`
	Error            string               `json:"error,omitempty"`
	Retries          int                  `json:"retries"`
	AssertionsPassed int                  `json:"assertions_passed"`
	AssertionsFailed int                  `json:"assertions_failed"`
	DisplayOutputs   []DisplayOutputEntry `json:"display_outputs,omitempty"`
}

// DisplayOutputEntry is a display-tagged output in the JSON summary.
type DisplayOutputEntry struct {
	Label string `json:"label"`
	Name  string `json:"name"`
	Value any    `json:"value,omitempty"`
}

// SummaryStats is the aggregate counts in the JSON summary.
type SummaryStats struct {
	TotalSteps  int   `json:"total_steps"`
	PassedSteps int   `json:"passed_steps"`
	FailedSteps int   `json:"failed_steps"`
	DurationMs  int64 `json:"duration_ms"`
}

// runResult is the internal result from runCommand, used by executeRun
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

	// Display outputs
	for _, do := range step.DisplayOutputs {
		ss.DisplayOutputs = append(ss.DisplayOutputs, DisplayOutputEntry{
			Label: do.Label,
			Name:  do.Name,
			Value: do.Value,
		})
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
			for _, do := range step.DisplayOutputs {
				fmt.Fprintf(out, "        %s: %v\n", do.Label, do.Value)
			}
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

// parseOverrideFlag parses "nodeName=http://url" into (name, url, error).
func parseOverrideFlag(flag string) (string, string, error) {
	idx := strings.Index(flag, "=")
	if idx < 1 {
		return "", "", fmt.Errorf("invalid override %q: expected nodeName=url", flag)
	}
	name := flag[:idx]
	url := flag[idx+1:]
	if url == "" {
		return "", "", fmt.Errorf("invalid override %q: URL is empty", flag)
	}
	return name, url, nil
}

// addResolvedOverride adds a config.ResolvedOverride to the executor router.
func addResolvedOverride(router *engine.ExecutorRouter, ov config.ResolvedOverride) {
	overrideExec := adapter.NewHTTPExecutor(ov.APIConfig.BaseURL)
	overrideCfg := &adapter.EnvironmentConfig{
		BaseURL: ov.APIConfig.BaseURL,
		Headers: ov.APIConfig.Headers,
		Values:  ov.APIConfig.Values,
	}
	var rewrite *adapter.PathRewrite
	if ov.PathRewrite != nil {
		rewrite = &adapter.PathRewrite{
			Strip:  ov.PathRewrite.Strip,
			Prefix: ov.PathRewrite.Prefix,
		}
	}
	router.AddOverride(ov.Pattern, overrideExec, overrideCfg, rewrite)
}

// writeRunArchive creates a run archive in the output directory and returns
// the archive path. It collects secrets from the environment for redaction.
func writeRunArchive(result *engine.RunResult, p *plan.Plan, env *config.Environment, g *graph.Graph, outputDir string) (string, error) {
	secrets := env.CollectSecrets()
	if p.Auth != nil {
		for k, v := range config.CollectAuthSecrets(p.Auth) {
			secrets[k] = v
		}
	}
	return writeRunArchiveWithSecrets(result, p, env, g, outputDir, secrets)
}

// writeRunArchiveWithSecrets creates a run archive using a pre-built secrets set.
func writeRunArchiveWithSecrets(result *engine.RunResult, p *plan.Plan, env *config.Environment, g *graph.Graph, outputDir string, secrets map[string]bool) (string, error) {
	runID := archive.GenerateRunID()
	meta := archive.ArchiveMetadata{
		Version:      "1.0.0",
		RunID:        runID,
		Timestamp:    time.Now(),
		Plan:         p,
		Environment:  env.Name,
		GraphVersion: g.Version,
		ToolVersion:  "0.1.0",
	}
	arc := engine.ToArchive(result, meta, env.APIBaseURL, secrets)
	archivePath := filepath.Join(outputDir, runID, "archive.json")
	if err := archive.Write(arc, archivePath); err != nil {
		return "", fmt.Errorf("writing archive: %w", err)
	}
	return archivePath, nil
}

// resolveOutputDir determines the output directory from flag, manifest, or default.
func resolveOutputDir(flagChanged bool, flagValue, manifestDir string) string {
	if flagChanged {
		return flagValue
	}
	if manifestDir != "" {
		return manifestDir
	}
	return "_output/runs"
}

// buildProjectOverrides constructs config.ProjectPaths from explicitly-set cobra flags.
func buildProjectOverrides(changed func(string) bool, getString func(string) string) config.ProjectPaths {
	overrides := config.ProjectPaths{}
	if changed("manifest") {
		overrides.ExplicitManifest = getString("manifest")
	}
	if changed("graph") {
		overrides.GraphPath = getString("graph")
	}
	if changed("env") {
		overrides.EnvPath = getString("env")
	}
	if changed("templates") {
		overrides.TemplatesPath = getString("templates")
	}
	if changed("domain") {
		overrides.DomainPath = getString("domain")
	}
	return overrides
}

// resolvePlanPath searches plan directories for a plan by name when needed.
func resolvePlanPath(planPath string, planDirs []string) string {
	if planPath == "" || filepath.IsAbs(planPath) {
		return planPath
	}
	if _, err := os.Stat(planPath); err == nil {
		return planPath
	}
	if len(planDirs) > 0 {
		if found, findErr := config.FindPlan(planDirs, planPath); findErr == nil {
			return found
		}
	}
	return planPath
}

// runContext holds pre-loaded shared infrastructure for plan execution.
// This allows batch execution to load environment, graph, templates, and domain
// once and reuse them across multiple plan runs.
type runContext struct {
	Env                *config.Environment
	Graph              *graph.Graph
	Registry           *adapter.Registry
	KB                 *domain.KnowledgeBase
	LLMClient          llm.Client
	Mode               config.ExecutionMode
	MaxRelaxationDepth int
	GraphDir           string // for recipe reconstitution
	Secrets            map[string]bool

	// Override configuration (from env-file, overlay, CLI flags)
	Overrides     []string // CLI --override flags
	EnvOverlay    string   // path to overlay YAML
}

// loadRunContext loads all shared infrastructure from the given args.
// It loads environment, graph, templates, domain, determines execution mode,
// and creates an LLM client. This is the expensive setup that should happen once.
func loadRunContext(ctx context.Context, args *runArgs, logf func(string, ...any)) (*runContext, error) {
	if args.EnvPath == "" {
		return nil, fmt.Errorf("--env is required")
	}
	if args.GraphPath == "" {
		return nil, fmt.Errorf("--graph is required")
	}
	if args.TemplatesPath == "" {
		return nil, fmt.Errorf("--templates is required")
	}

	// 1. Load environment
	logf("aat: loading environment...\n")
	env, err := config.LoadEnvironment(args.EnvPath)
	if err != nil {
		return nil, fmt.Errorf("loading environment: %w", err)
	}
	logf("aat: loaded environment %q\n", env.Name)

	// 2. Load graph
	g, err := graph.ParseFile(args.GraphPath)
	if err != nil {
		return nil, fmt.Errorf("loading graph: %w", err)
	}
	logf("aat: loaded graph (%d nodes)\n", len(g.Nodes))

	// 3. Load domain knowledge (optional)
	var kb *domain.KnowledgeBase
	if args.DomainPath != "" {
		kb, err = domain.ParseFile(args.DomainPath)
		if err != nil {
			return nil, fmt.Errorf("loading domain knowledge: %w", err)
		}
		logf("aat: loaded domain knowledge\n")
	}

	// 4. Load templates
	registry := adapter.NewRegistry()
	count, err := adapter.LoadTemplates(args.TemplatesPath, registry)
	if err != nil {
		return nil, fmt.Errorf("loading templates: %w", err)
	}
	logf("aat: loaded %d templates\n", count)

	// 5. Determine execution mode
	effectiveMode := config.ExecutionMode(args.Mode)
	if effectiveMode == "" {
		effectiveMode = env.LLM.Mode
	}
	if effectiveMode == "" {
		effectiveMode = config.ModeStrict
	}

	// 6. Create LLM client if mode requires it
	var llmClient llm.Client
	if effectiveMode != config.ModeStrict && env.LLM.Endpoint != "" {
		llmClient, err = llm.NewClient(env.LLM)
		if err != nil {
			return nil, fmt.Errorf("creating LLM client: %w", err)
		}
	}

	return &runContext{
		Env:                env,
		Graph:              g,
		Registry:           registry,
		KB:                 kb,
		LLMClient:          llmClient,
		Mode:               effectiveMode,
		MaxRelaxationDepth: env.Settings.MaxRelaxationDepth,
		GraphDir:           filepath.Dir(args.GraphPath),
		Secrets:            env.CollectSecrets(),
		Overrides:          args.Overrides,
		EnvOverlay:         args.EnvOverlay,
	}, nil
}

// loadAndRunPlan parses a plan file, then executes it using the shared runContext.
// Returns a runResult. The outputDir controls where the archive is written.
func loadAndRunPlan(ctx context.Context, rctx *runContext, planPath, outputDir string, observer engine.ProgressObserver, logf func(string, ...any)) *runResult {
	// 1. Parse plan (or recipe)
	parsed, err := plan.ParseAnyFile(planPath)
	if err != nil {
		return &runResult{setupErr: true, err: fmt.Errorf("loading plan: %w", err)}
	}

	var p *plan.Plan
	switch v := parsed.(type) {
	case *plan.Plan:
		p = v
	case *plan.Recipe:
		logf("aat: reconstituting recipe %q...\n", v.Selection.Workflow)
		reconstituted, reconErr := intent.Reconstitute(v, rctx.Graph, rctx.GraphDir)
		if reconErr != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("reconstituting recipe: %w", reconErr)}
		}
		p = reconstituted
	default:
		return &runResult{setupErr: true, err: fmt.Errorf("unexpected parse result type %T", parsed)}
	}

	// 2. Validate plan against graph
	if _, err := plan.InstantiateAndValidate(p, rctx.Graph); err != nil {
		return &runResult{setupErr: true, err: fmt.Errorf("plan validation: %w", err)}
	}

	// 3. Determine effective auth: plan auth overrides env auth
	effectiveAuth := rctx.Env.Auth
	if p.Auth != nil {
		effectiveAuth = *p.Auth
		logf("aat: using plan-level auth (%s)\n", effectiveAuth.Type)
	}

	// 4. Authenticate with effective auth, merging plan headers
	apiConfig, err := rctx.Env.BuildAPIConfigFromAuth(ctx, effectiveAuth, p.Headers)
	if err != nil {
		return &runResult{setupErr: true, err: fmt.Errorf("building API config: %w", err)}
	}
	logf("aat: authenticated via %s\n", effectiveAuth.Type)

	// 5. Create executor, environment config, and router
	executor := adapter.NewHTTPExecutor(apiConfig.BaseURL)
	envConfig := &adapter.EnvironmentConfig{
		BaseURL: apiConfig.BaseURL,
		Headers: apiConfig.Headers,
		Values:  apiConfig.Values,
	}
	router := engine.NewExecutorRouter(executor, envConfig)

	// 5a. Apply env-file overrides
	if len(rctx.Env.Overrides) > 0 {
		resolvedOverrides, err := rctx.Env.BuildOverrideConfigsWithAuth(ctx, apiConfig.Headers, effectiveAuth)
		if err != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("building overrides: %w", err)}
		}
		for _, ov := range resolvedOverrides {
			addResolvedOverride(router, ov)
		}
	}

	// 5b. Apply overlay file overrides
	if rctx.EnvOverlay != "" {
		overlayOverrides, err := config.LoadOverlayFile(rctx.EnvOverlay)
		if err != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("loading overlay: %w", err)}
		}
		resolvedOverrides, err := (&config.Environment{
			APIBaseURL: rctx.Env.APIBaseURL,
			Auth:       effectiveAuth,
			Overrides:  overlayOverrides,
		}).BuildOverrideConfigsWithAuth(ctx, apiConfig.Headers, effectiveAuth)
		if err != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("building overlay overrides: %w", err)}
		}
		for _, ov := range resolvedOverrides {
			addResolvedOverride(router, ov)
		}
	}

	// 5c. Apply CLI --override flags
	for _, flag := range rctx.Overrides {
		name, url, err := parseOverrideFlag(flag)
		if err != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("parsing --override: %w", err)}
		}
		overrideExec := adapter.NewHTTPExecutor(url)
		overrideCfg := &adapter.EnvironmentConfig{
			BaseURL: url,
			Headers: make(map[string]string),
			Values:  make(map[string]string),
		}
		router.AddOverride(name, overrideExec, overrideCfg, nil)
	}

	// Log active overrides
	if router.HasOverrides() {
		for _, pat := range router.OverridePatterns() {
			logf("aat: override: %s\n", pat)
		}
	}

	// 6. Create engine and run
	eng := engine.NewEngine(rctx.Graph, rctx.Registry, router).
		WithMode(rctx.Mode).
		WithDomain(rctx.KB).
		WithLLM(rctx.LLMClient).
		WithMaxRelaxationDepth(rctx.MaxRelaxationDepth).
		WithProgress(observer)
	logf("aat: executing plan (%d steps, mode=%s)...\n\n", len(p.Execution.Steps), rctx.Mode)

	result := eng.Run(ctx, p)

	// Print human-readable summary only when no observer is active
	if observer == nil {
		printRunSummary(result, io.Discard)
	}

	// 7. Write archive
	secrets := make(map[string]bool)
	for k, v := range rctx.Secrets {
		secrets[k] = v
	}
	if p.Auth != nil {
		for k, v := range config.CollectAuthSecrets(p.Auth) {
			secrets[k] = v
		}
	}
	archivePath, archiveErr := writeRunArchiveWithSecrets(result, p, rctx.Env, rctx.Graph, outputDir, secrets)
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
