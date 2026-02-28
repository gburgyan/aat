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
	"github.com/gburgyan/aat/graph/oas"
	"github.com/gburgyan/aat/intent"
	"github.com/gburgyan/aat/internal/version"
	"github.com/gburgyan/aat/plan"
)

// RetryNotifier is an optional interface that progress observers can implement
// to receive notifications when a plan-level retry begins.
type RetryNotifier interface {
	OnRetryStart(attempt, maxAttempts int)
}

// runArgs holds parsed CLI flags for the run command.
type runArgs struct {
	PlanPath          string
	EnvPath           string
	GraphPath         string
	TemplatesPath     string
	OutputDir         string
	DomainPath        string
	JSON              bool
	Quiet             bool
	Overrides         []string   // "nodeName=http://url" pairs
	EnvOverlay        string     // path to overlay YAML
	MaxRetries        int        // max plan-level retries (0 = no retries)
	Layers            []string   // layer names to apply
	LayersDir         string     // directory containing layer files
	LayerGroups       [][]string // layer groups for permutation (batch only)
	NoAutoOverrides   bool       // disable .aat-overrides.yaml auto-discovery
	AutoOverridesPath string     // resolved path to auto-discovered overrides file
	OASValidateMode   string     // "auto", "warn", "strict", "off"
}

// RunSummary is the machine-readable JSON output for CI/CD pipelines.
type RunSummary struct {
	Outcome     string        `json:"outcome"`
	Error       string        `json:"error,omitempty"`
	Steps       []StepSummary `json:"steps"`
	Cleanup     []StepSummary `json:"cleanup,omitempty"`
	Summary     SummaryStats  `json:"summary"`
	ArchivePath string        `json:"archive_path,omitempty"`
	Attempts    int           `json:"attempts,omitempty"` // total attempts (omitted if 1)
	Retried     bool          `json:"retried,omitempty"`  // true if any retries occurred
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
	setupErr    bool     // true when error occurred before engine execution
	attempts    int      // total attempts (1 = no retries)
	layers      []string // effective layers applied to this run
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
func printRunSummary(result *engine.RunResult, out io.Writer, ti TerminalInfo) {
	total := len(result.Steps)
	color := ti.IsTTY
	ncw := nodeColWidth(ti.Width, 60)

	var oasWarnings int
	for i, step := range result.Steps {
		nodeStr := formatNodeCol(step.Node, ncw, color)
		prefix := fmt.Sprintf("  [%d/%d] %s", i+1, total, nodeStr)
		if step.Error != nil {
			errLabel := colorize("ERROR", colorRed, color)
			if step.RetryCount > 0 {
				_, _ = fmt.Fprintf(out, "%s %s [%s] (after %d retries)\n", prefix, errLabel, errorCategory(step), step.RetryCount)
			} else {
				_, _ = fmt.Fprintf(out, "%s %s: %s\n", prefix, errLabel, step.Error)
			}
		} else if step.Response != nil {
			status := colorStatus(step.StatusCode, color)
			validMark := ""
			if step.Validation != nil && !step.Validation.Passed {
				validMark = "  " + colorize("ASSERTIONS FAILED", colorYellow, color)
			}
			oasMark := ""
			if step.OASValidation != nil && step.OASValidation.HasErrors() {
				ec := step.OASValidation.ErrorCount()
				oasWarnings += ec
				oasMark = "  " + colorize(fmt.Sprintf("OAS: %d warning(s)", ec), colorYellow, color)
			}
			_, _ = fmt.Fprintf(out, "%s %s  %dms%s%s\n", prefix, status, step.Duration.Milliseconds(), validMark, oasMark)
			for _, do := range step.DisplayOutputs {
				_, _ = fmt.Fprintf(out, "        %s: %v\n", do.Label, do.Value)
			}
		} else {
			_, _ = fmt.Fprintf(out, "%s (no response)\n", prefix)
		}
	}

	if len(result.CleanupResults) > 0 {
		_, _ = fmt.Fprintln(out, "\n  cleanup:")
		cleanupNCW := nodeColWidth(ti.Width, 58)
		for _, step := range result.CleanupResults {
			node := fmt.Sprintf("%-*s", cleanupNCW, truncateNode(step.Node, cleanupNCW))
			prefix := fmt.Sprintf("    %s", node)
			if step.Error != nil {
				errLabel := colorize("ERROR", colorRed, color)
				_, _ = fmt.Fprintf(out, "%s %s: %s\n", prefix, errLabel, step.Error)
			} else if step.Response != nil {
				status := colorStatus(step.StatusCode, color)
				_, _ = fmt.Fprintf(out, "%s %s  %dms\n", prefix, status, step.Duration.Milliseconds())
			} else {
				_, _ = fmt.Fprintf(out, "%s (no response)\n", prefix)
			}
		}
	}

	_, _ = fmt.Fprintln(out)
	switch result.Outcome {
	case engine.OutcomePassed:
		_, _ = fmt.Fprintf(out, "%s (%d/%d steps, %s)\n", colorOutcome("PASSED", color), total, total, totalDuration(result))
	case engine.OutcomeFailed:
		_, _ = fmt.Fprintf(out, "%s: %s\n", colorOutcome("FAILED", color), outcomeMessage(result))
	case engine.OutcomeError:
		_, _ = fmt.Fprintf(out, "%s: %s\n", colorOutcome("ERROR", color), outcomeMessage(result))
	}
	if oasWarnings > 0 {
		_, _ = fmt.Fprintf(out, "OAS: %s\n", colorize(fmt.Sprintf("%d warning(s)", oasWarnings), colorYellow, color))
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
func writeRunArchive(result *engine.RunResult, p *plan.Plan, env *config.Environment, g *graph.Graph, outputDir string, layers []string) (string, error) {
	secrets := env.CollectSecrets()
	if p.Auth != nil {
		for k, v := range config.CollectAuthSecrets(p.Auth) {
			secrets[k] = v
		}
	}
	return writeRunArchiveWithSecrets(result, p, env, g, outputDir, secrets, layers)
}

// writeRunArchiveWithSecrets creates a run archive using a pre-built secrets set.
func writeRunArchiveWithSecrets(result *engine.RunResult, p *plan.Plan, env *config.Environment, g *graph.Graph, outputDir string, secrets map[string]bool, layers []string) (string, error) {
	runID := archive.GenerateRunID()
	meta := archive.ArchiveMetadata{
		Version:      "1.0.0",
		RunID:        runID,
		Timestamp:    time.Now(),
		Plan:         p,
		Environment:  env.Name,
		GraphVersion: g.Version,
		ToolVersion:  version.Version,
		Layers:       layers,
	}
	arc := engine.ToArchive(result, meta, env.APIBaseURL, secrets)
	archivePath := filepath.Join(outputDir, runID, "archive.json")
	if err := archive.Write(arc, archivePath); err != nil {
		return "", fmt.Errorf("writing archive: %w", err)
	}
	return archivePath, nil
}

// retryDelay is the fixed delay between plan-level retry attempts.
const retryDelay = 2 * time.Second

// isRetryable returns true if the run result should trigger a plan-level retry.
// Setup errors (bad plan, missing template) are not retried.
func isRetryable(res *runResult) bool {
	if res.setupErr {
		return false
	}
	switch res.outcome {
	case engine.OutcomePassed:
		return false
	case engine.OutcomeFailed, engine.OutcomeError:
		return true
	default:
		return false
	}
}

// loadAndRunPlanWithRetries wraps loadAndRunPlan with plan-level retry logic.
// maxRetries is the number of additional attempts after the first failure (0 = no retries).
// Each attempt gets fresh engine state. Failed attempts are saved as attempt-NN.json.
func loadAndRunPlanWithRetries(ctx context.Context, rctx *runContext, planPath, outputDir string, maxRetries int, observer engine.ProgressObserver, logf func(string, ...any)) *runResult {
	if maxRetries <= 0 {
		// No retries — original behavior
		return loadAndRunPlan(ctx, rctx, planPath, outputDir, observer, logf)
	}

	// Generate a stable run ID for the entire logical run
	runID := archive.GenerateRunID()
	runDir := filepath.Join(outputDir, runID)
	totalPossible := maxRetries + 1

	// Collect secrets once for all archive writes
	secrets := make(map[string]bool)
	for k, v := range rctx.Secrets {
		secrets[k] = v
	}

	// Parse plan once to collect plan-level auth secrets
	parsed, err := plan.ParseAnyFile(planPath)
	if err == nil {
		if p, ok := parsed.(*plan.Plan); ok && p.Auth != nil {
			for k, v := range config.CollectAuthSecrets(p.Auth) {
				secrets[k] = v
			}
		}
	}

	var lastRes *runResult
	for attempt := 1; attempt <= totalPossible; attempt++ {
		select {
		case <-ctx.Done():
			return &runResult{
				outcome:  engine.OutcomeError,
				err:      fmt.Errorf("execution cancelled: %w", ctx.Err()),
				attempts: attempt - 1,
			}
		default:
		}

		if attempt > 1 {
			logf("[attempt %d/%d] retrying...\n", attempt, totalPossible)
			if rn, ok := observer.(RetryNotifier); ok {
				rn.OnRetryStart(attempt, totalPossible)
			}
			// Brief delay between retries
			select {
			case <-ctx.Done():
				return &runResult{
					outcome:  engine.OutcomeError,
					err:      fmt.Errorf("execution cancelled during retry delay: %w", ctx.Err()),
					attempts: attempt - 1,
				}
			case <-time.After(retryDelay):
			}
		}

		// Execute the plan (writes its own archive to runDir)
		res := loadAndRunPlanToDir(ctx, rctx, planPath, runDir, attempt, totalPossible, observer, logf)
		lastRes = res
		lastRes.attempts = attempt

		if !isRetryable(res) {
			break
		}

		// If this was the last allowed attempt, keep archive.json as the final result
		if attempt >= totalPossible {
			break
		}

		// Save intermediate failed attempt archive before retrying
		if res.summary != nil {
			// Rename the archive.json that was written to attempt-NN.json
			attemptFile := fmt.Sprintf("attempt-%02d.json", attempt)
			mainArchive := filepath.Join(runDir, "archive.json")
			attemptPath := filepath.Join(runDir, attemptFile)
			if renameErr := os.Rename(mainArchive, attemptPath); renameErr != nil {
				logf("aat: warning: could not save attempt archive: %s\n", renameErr)
			}
		}

		if attempt < totalPossible {
			reason := "unknown"
			if res.err != nil {
				reason = res.err.Error()
			}
			logf("[attempt %d/%d] FAILED: %s\n", attempt, totalPossible, reason)
		}
	}

	// Annotate summary with attempt info
	if lastRes != nil && lastRes.summary != nil && lastRes.attempts > 1 {
		lastRes.summary.Attempts = lastRes.attempts
		lastRes.summary.Retried = true
	}

	return lastRes
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
	Env          *config.Environment
	Graph        *graph.Graph
	Registry     *adapter.Registry
	KB           *domain.KnowledgeBase
	GraphDir     string // for recipe reconstitution
	Secrets      map[string]bool
	AuthProvider *config.AuthProvider // cached default auth

	// Override configuration (from env-file, auto-overrides, overlay, CLI flags)
	Overrides         []string // CLI --override flags
	EnvOverlay        string   // path to overlay YAML
	AutoOverridesPath string   // path to auto-discovered .aat-overrides.yaml

	// Layer configuration
	Layers          []string                // layer names from CLI flags
	LayersDir       string                  // directory containing layer files
	AvailableLayers map[string]*graph.Layer // pre-loaded layers (nil until needed)

	// OAS validation
	OASCache        *oas.SpecCache // loaded specs for runtime validation (nil if none)
	OASValidateMode string         // effective mode: "auto", "warn", "strict", "off"
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

	// Discover auto-overrides if not disabled
	autoOverridesPath := args.AutoOverridesPath
	if autoOverridesPath == "" && !args.NoAutoOverrides {
		autoOverridesPath = config.FindAutoOverrides()
		if autoOverridesPath != "" {
			logf("aat: auto-discovered overrides: %s\n", autoOverridesPath)
		}
	}

	rctx := &runContext{
		Env:               env,
		Graph:             g,
		Registry:          registry,
		KB:                kb,
		GraphDir:          filepath.Dir(args.GraphPath),
		Secrets:           env.CollectSecrets(),
		AuthProvider:      config.NewAuthProvider(env.Auth),
		Overrides:         args.Overrides,
		EnvOverlay:        args.EnvOverlay,
		AutoOverridesPath: autoOverridesPath,
		Layers:            args.Layers,
		LayersDir:         args.LayersDir,
	}

	// Pre-load layers if directory is configured and any layers are referenced
	// (from --layer flags and/or --layer-group flags).
	if rctx.LayersDir != "" {
		allNames := collectAllLayerNames(args.Layers, args.LayerGroups)
		if len(allNames) > 0 {
			layers, err := graph.ResolveLayerNames(allNames, rctx.LayersDir)
			if err != nil {
				return nil, fmt.Errorf("loading layers: %w", err)
			}
			rctx.AvailableLayers = layers
			logf("aat: loaded %d layers\n", len(layers))
		}
	}

	// Resolve OAS validation mode: CLI flag > env setting > "auto"
	oasMode := resolveOASMode(args.OASValidateMode, env.Settings.OASValidation)
	rctx.OASValidateMode = oasMode

	// Load OAS specs for runtime validation (unless disabled)
	if oasMode != "off" {
		specPaths := collectOASSpecPaths(g)
		if len(specPaths) > 0 {
			graphDir := filepath.Dir(args.GraphPath)
			oasCache := oas.NewSpecCache()
			for _, sp := range specPaths {
				fsPath := sp
				if !filepath.IsAbs(sp) {
					fsPath = filepath.Join(graphDir, sp)
				}
				if loadErr := oasCache.Load(sp, fsPath); loadErr != nil {
					logf("aat: warning: could not load OAS spec %q: %s\n", sp, loadErr)
				}
			}
			if oasCache.Len() > 0 {
				rctx.OASCache = oasCache
				logf("aat: loaded %d OAS spec(s) for runtime validation\n", oasCache.Len())
			}
		}
	}

	return rctx, nil
}

// loadAndRunPlan parses a plan file, then executes it using the shared runContext.
// Returns a runResult. The outputDir is the parent; a run-ID subdirectory is created.
func loadAndRunPlan(ctx context.Context, rctx *runContext, planPath, outputDir string, observer engine.ProgressObserver, logf func(string, ...any)) *runResult {
	runID := archive.GenerateRunID()
	runDir := filepath.Join(outputDir, runID)
	return loadAndRunPlanToDir(ctx, rctx, planPath, runDir, 0, 0, observer, logf)
}

// loadAndRunPlanToDir parses a plan file, executes it, and writes the archive
// to the specified run directory (archive.json within runDir).
func loadAndRunPlanToDir(ctx context.Context, rctx *runContext, planPath, runDir string, attempt, totalAttempts int, observer engine.ProgressObserver, logf func(string, ...any)) *runResult {
	// 1. Parse plan (or recipe)
	parsed, err := plan.ParseAnyFile(planPath)
	if err != nil {
		return &runResult{setupErr: true, err: fmt.Errorf("loading plan: %w", err)}
	}

	var p *plan.Plan
	var effectiveLayers []string
	switch v := parsed.(type) {
	case *plan.Plan:
		p = v
		effectiveLayers = rctx.Layers
	case *plan.Recipe:
		logf("aat: reconstituting recipe %q...\n", v.Selection.Workflow)
		// Merge CLI layers after recipe layers
		recipeLayers := v.Selection.Layers
		if len(rctx.Layers) > 0 {
			seen := make(map[string]bool, len(recipeLayers))
			for _, l := range recipeLayers {
				seen[l] = true
			}
			for _, l := range rctx.Layers {
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
			return &runResult{setupErr: true, err: fmt.Errorf("reconstituting recipe: %w", reconErr)}
		}
		p = reconstituted
	default:
		return &runResult{setupErr: true, err: fmt.Errorf("unexpected parse result type %T", parsed)}
	}

	// Compute layered defaults from the effective set of layers (CLI + recipe).
	// This must happen after the switch so recipe-embedded layers are included.
	var layeredDefaults map[string]*graph.InputDefault
	if len(effectiveLayers) > 0 && rctx.LayersDir != "" {
		available, loadErr := graph.ResolveLayerNames(effectiveLayers, rctx.LayersDir)
		if loadErr != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("loading layers: %w", loadErr)}
		}
		var applyErr error
		layeredDefaults, applyErr = graph.ApplyLayers(rctx.Graph, effectiveLayers, available)
		if applyErr != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("applying layers: %w", applyErr)}
		}
	}

	// 2. Validate plan against graph (with layers)
	if _, err := plan.InstantiateAndValidateWithLayers(p, rctx.Graph, layeredDefaults); err != nil {
		return &runResult{setupErr: true, err: fmt.Errorf("plan validation: %w", err)}
	}

	// 3. Determine effective auth: plan auth overrides env auth
	effectiveAuth := rctx.Env.Auth
	planOverridesAuth := p.Auth != nil
	if planOverridesAuth {
		effectiveAuth = *p.Auth
		logf("aat: using plan-level auth (%s)\n", effectiveAuth.Type)
	}

	// 4. Authenticate — use cached provider for default auth, direct call for plan auth
	var token *config.OAuthToken
	if planOverridesAuth {
		token, err = config.Authenticate(ctx, effectiveAuth)
	} else {
		token, err = rctx.AuthProvider.Authenticate(ctx)
	}
	if err != nil {
		return &runResult{setupErr: true, err: fmt.Errorf("authenticating: %w", err)}
	}
	apiConfig := rctx.Env.BuildAPIConfigFromToken(token, effectiveAuth, p.Headers)
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
		var resolvedOverrides []config.ResolvedOverride
		if planOverridesAuth {
			resolvedOverrides, err = rctx.Env.BuildOverrideConfigsWithAuth(ctx, apiConfig.Headers, effectiveAuth)
		} else {
			resolvedOverrides, err = rctx.Env.BuildOverrideConfigsWithProvider(ctx, apiConfig.Headers, rctx.AuthProvider)
		}
		if err != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("building overrides: %w", err)}
		}
		for _, ov := range resolvedOverrides {
			addResolvedOverride(router, ov)
		}
	}

	// 5a.5. Apply auto-discovered .aat-overrides.yaml
	if rctx.AutoOverridesPath != "" {
		autoOverrides, err := config.LoadOverlayFile(rctx.AutoOverridesPath)
		if err != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("loading auto-overrides %s: %w", rctx.AutoOverridesPath, err)}
		}
		autoOverrideEnv := &config.Environment{
			APIBaseURL: rctx.Env.APIBaseURL,
			Auth:       effectiveAuth,
			Overrides:  autoOverrides,
		}
		var resolvedOverrides []config.ResolvedOverride
		if planOverridesAuth {
			resolvedOverrides, err = autoOverrideEnv.BuildOverrideConfigsWithAuth(ctx, apiConfig.Headers, effectiveAuth)
		} else {
			resolvedOverrides, err = autoOverrideEnv.BuildOverrideConfigsWithProvider(ctx, apiConfig.Headers, rctx.AuthProvider)
		}
		if err != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("building auto-overrides: %w", err)}
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
		overlayEnv := &config.Environment{
			APIBaseURL: rctx.Env.APIBaseURL,
			Auth:       effectiveAuth,
			Overrides:  overlayOverrides,
		}
		var resolvedOverrides []config.ResolvedOverride
		if planOverridesAuth {
			resolvedOverrides, err = overlayEnv.BuildOverrideConfigsWithAuth(ctx, apiConfig.Headers, effectiveAuth)
		} else {
			resolvedOverrides, err = overlayEnv.BuildOverrideConfigsWithProvider(ctx, apiConfig.Headers, rctx.AuthProvider)
		}
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
		WithDomain(rctx.KB).
		WithProgress(observer).
		WithLayers(layeredDefaults)

	if rctx.OASCache != nil {
		eng.WithOASSpecs(rctx.OASCache, rctx.Graph.OAS, rctx.OASValidateMode == "strict")
	}

	logf("aat: executing plan (%d steps)...\n\n", len(p.Execution.Steps))

	result := eng.Run(ctx, p)

	// Print human-readable summary only when no observer is active
	if observer == nil {
		printRunSummary(result, io.Discard, TerminalInfo{})
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

	meta := archive.ArchiveMetadata{
		Version:       "1.0.0",
		RunID:         filepath.Base(runDir),
		Timestamp:     time.Now(),
		Plan:          p,
		Environment:   rctx.Env.Name,
		GraphVersion:  rctx.Graph.Version,
		ToolVersion:   version.Version,
		Attempt:       attempt,
		TotalAttempts: totalAttempts,
		Layers:        effectiveLayers,
	}
	arc := engine.ToArchive(result, meta, rctx.Env.APIBaseURL, secrets)
	archivePath := filepath.Join(runDir, "archive.json")
	if archiveErr := archive.Write(arc, archivePath); archiveErr != nil {
		logf("aat: warning: %s\n", archiveErr)
		archivePath = ""
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
		layers:      effectiveLayers,
	}
}

// resolveOASMode returns the effective OAS validation mode.
// CLI flag takes precedence over env setting; defaults to "auto".
func resolveOASMode(cliFlag, envSetting string) string {
	if cliFlag != "" {
		return cliFlag
	}
	if envSetting != "" {
		return envSetting
	}
	return "auto"
}

// collectOASSpecPaths returns the unique set of OAS spec paths from the graph.
func collectOASSpecPaths(g *graph.Graph) []string {
	seen := make(map[string]bool)
	var paths []string
	if g.OAS != "" && !seen[g.OAS] {
		seen[g.OAS] = true
		paths = append(paths, g.OAS)
	}
	for _, node := range g.Nodes {
		if node.OAS != nil && node.OAS.Spec != "" && !seen[node.OAS.Spec] {
			seen[node.OAS.Spec] = true
			paths = append(paths, node.OAS.Spec)
		}
	}
	return paths
}

// collectAllLayerNames returns the union of layer names from base layers and
// all layer groups. Used to pre-load all layers that any permutation might need.
func collectAllLayerNames(baseLayers []string, layerGroups [][]string) []string {
	seen := make(map[string]bool)
	var names []string
	for _, name := range baseLayers {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for _, group := range layerGroups {
		for _, name := range group {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}
