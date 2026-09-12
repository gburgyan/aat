package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/gburgyan/aat/validate"
	"github.com/spf13/cobra"
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
	EnvName           string // environment name for multi-env files
	GraphPath         string
	TemplatesPath     string
	OutputDir         string
	DomainPath        string
	JSON              bool
	Quiet             bool
	Overrides         []string          // "nodeName=http://url" pairs
	EnvOverlay        string            // path to overlay YAML
	MaxRetries        int               // max plan-level retries (0 = no retries)
	Layers            []string          // layer names to apply
	LayersDir         string            // directory containing layer files
	LayerGroups       [][]string        // layer groups for permutation (batch only)
	NoAutoOverrides   bool              // disable .aat-overrides.yaml auto-discovery
	AutoOverridesPath string            // resolved path to auto-discovered overrides file
	OASValidateMode   string            // "auto", "strict", "off"
	VerboseAuth       bool              // log auth request/response details to stderr
	SkipMutations     bool              // strip mutations from plans before running (smoke-test mode)
	StopAfterStep     string            // stop after this step ID; skip cleanup (checkpoint handoff)
	DumpStatePath     string            // write accumulated run state to this file (mode 0600)
	Vars              map[string]string // --var KEY=VALUE for multi-environment files
}

// RunSummary is the machine-readable JSON output for CI/CD pipelines.
type RunSummary struct {
	Outcome     string        `json:"outcome"`
	Error       string        `json:"error,omitempty"`
	Steps       []StepSummary `json:"steps"`
	Cleanup     []StepSummary `json:"cleanup,omitempty"`
	Summary     SummaryStats  `json:"summary"`
	ArchivePath string        `json:"archive_path,omitempty"`
	Attempts    int           `json:"attempts,omitempty"`   // total attempts (omitted if 1)
	Retried     bool          `json:"retried,omitempty"`    // true if any retries occurred
	StoppedAt   string        `json:"stopped_at,omitempty"` // checkpoint step ID when the outcome is "stopped"
	// State is the accumulated run state (base URL, live auth headers, step
	// outputs), populated only when --dump-state=- requests stdout output.
	// Contains UNREDACTED auth — emitted only on explicit opt-in.
	State *engine.StateExport `json:"state,omitempty"`
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
	RetriedOn        []string             `json:"retried_on,omitempty"` // error category of each retried attempt
	AssertionsPassed int                  `json:"assertions_passed"`
	AssertionsFailed int                  `json:"assertions_failed"`
	FailedAssertions []string             `json:"failed_assertions,omitempty"` // "type: message" per failed assertion
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
	TotalSteps  int            `json:"total_steps"`
	PassedSteps int            `json:"passed_steps"`
	FailedSteps int            `json:"failed_steps"`
	DurationMs  int64          `json:"duration_ms"`
	Issues      map[string]int `json:"issues,omitempty"`
}

// runResult is the internal result from runCommand, used by executeRun
// to determine exit codes and output format.
type runResult struct {
	outcome     engine.Outcome
	summary     *RunSummary
	archivePath string
	err         error
	setupErr    bool            // true when error occurred before engine execution
	attempts    int             // total attempts (1 = no retries)
	layers      []string        // effective layers applied to this run
	secrets     map[string]bool // the run's known secrets, for redacting what a batch archives about it
}

// exitCodeInfra is the exit code for infrastructure/config errors.
const exitCodeInfra = 2

// exitCode maps a runResult to the appropriate process exit code.
func exitCode(res *runResult) int {
	// Setup/config errors that occurred before engine execution
	if res.setupErr {
		return exitCodeInfra
	}
	return outcomeExitCode(res.outcome)
}

// outcomeExitCode maps a run outcome to its exit code: 0 passed or stopped at a
// checkpoint, 1 failed, 2 error, 130 aborted.
func outcomeExitCode(outcome engine.Outcome) int {
	switch outcome {
	case engine.OutcomeFailed:
		return 1
	case engine.OutcomeError:
		return exitCodeInfra
	case engine.OutcomeAborted:
		return 130
	default:
		return 0
	}
}

// writeJSON writes v to stdout as indented JSON, the format of every --json
// document.
func writeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// runSetupFailure reports an error that stopped aat run plan before it could
// run the plan. The message goes to stderr, and under --json the error document
// goes to stdout as well, so a pipeline parsing the output sees why it exited 2.
func runSetupFailure(jsonOut bool, err error) error {
	if jsonOut {
		writeJSON(&RunSummary{Outcome: "error", Error: err.Error()})
	}
	return &exitError{Code: exitCodeInfra, Err: err}
}

// buildRunSummary converts an engine.RunResult to a RunSummary.
func buildRunSummary(result *engine.RunResult, archivePath string) *RunSummary {
	s := &RunSummary{
		Outcome:     result.Outcome.String(),
		Error:       errString(result.Error),
		ArchivePath: archivePath,
		StoppedAt:   result.StoppedAt,
	}

	var passed, failed int

	for _, step := range result.Steps {
		ss := toStepSummary(step)
		s.Steps = append(s.Steps, ss)
		if ss.Passed {
			passed++
		} else {
			failed++
		}
	}

	for _, step := range result.CleanupResults {
		s.Cleanup = append(s.Cleanup, toStepSummary(step))
	}

	s.Summary = SummaryStats{
		TotalSteps:  len(result.Steps),
		PassedSteps: passed,
		FailedSteps: failed,
		DurationMs:  result.Elapsed().Milliseconds(),
		Issues:      countEngineIssues(result),
	}

	return s
}

// countEngineIssues counts categorized issues across engine step results.
// Returns nil when no issues exist so omitempty works correctly.
func countEngineIssues(result *engine.RunResult) map[string]int {
	var issues map[string]int
	allSteps := make([]engine.StepResult, 0, len(result.Steps)+len(result.CleanupResults))
	allSteps = append(allSteps, result.Steps...)
	allSteps = append(allSteps, result.CleanupResults...)
	for _, step := range allSteps {
		if step.OASValidation == nil {
			continue
		}
		n := step.OASValidation.ErrorCount()
		if n > 0 {
			if issues == nil {
				issues = make(map[string]int)
			}
			issues["oas"] += n
		}
	}
	return issues
}

// toStepSummary converts a single engine.StepResult to a StepSummary.
func toStepSummary(step engine.StepResult) StepSummary {
	ss := StepSummary{
		Name:       resultStepID(step),
		Node:       step.Node,
		Status:     step.StatusCode,
		DurationMs: step.Duration.Milliseconds(),
		Retries:    step.RetryCount,
	}
	for _, c := range step.RetriedOn {
		ss.RetriedOn = append(ss.RetriedOn, c.String())
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
	ss.FailedAssertions = failedAssertions(step.Validation)

	return ss
}

// failedAssertions describes each failed assertion as "type: message", such as
// "status: expected status 200, got 201", so run output says why a step failed.
func failedAssertions(v *validate.MechanicalResult) []string {
	if v == nil {
		return nil
	}
	var msgs []string
	for _, ar := range v.Results {
		if !ar.Passed {
			msgs = append(msgs, fmt.Sprintf("%s: %s", ar.Type, ar.Message))
		}
	}
	return msgs
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

// plannedStepCount returns how many steps a run of p numbers in its progress
// output: the instantiated steps (mutation siblings included) plus the
// verification steps that follow them.
func plannedStepCount(p *plan.Plan, g *graph.Graph, layeredDefaults map[string]*graph.InputDefault) int {
	inst := plan.InstantiateWithLayers(p, g, layeredDefaults)
	if inst == nil {
		return len(p.Execution.Steps)
	}
	return len(inst.Execution.Steps) + len(plan.VerificationSteps(inst, g, layeredDefaults))
}

// overrideFlagsToHostOverrides turns --override NODE=URL flags into override
// entries equivalent to `- match: NODE` with `baseUrl: URL`, so they inherit
// headers and auth exactly like an env.yaml entry.
func overrideFlagsToHostOverrides(flags []string) ([]config.HostOverride, error) {
	overrides := make([]config.HostOverride, 0, len(flags))
	for _, flag := range flags {
		name, url, err := parseOverrideFlag(flag)
		if err != nil {
			return nil, fmt.Errorf("parsing --override: %w", err)
		}
		overrides = append(overrides, config.HostOverride{Match: name, BaseURL: url})
	}
	return overrides, nil
}

// overlayOverrides returns an overlay file's override entries, or nil when no
// overlay was loaded.
func overlayOverrides(overlay *config.OverlayFile) []config.HostOverride {
	if overlay == nil {
		return nil
	}
	return overlay.Overrides
}

// addHostOverrides resolves override entries against the default route and the
// effective auth provider and registers them on the router. An entry without a
// baseUrl inherits apiBaseURL; an entry without auth inherits the provider's
// credential; every entry keeps the default route's overlay headers.
func addHostOverrides(ctx context.Context, router *engine.ExecutorRouter, apiBaseURL string, overrides []config.HostOverride, base *config.APIConfig, provider *config.AuthProvider) error {
	if len(overrides) == 0 {
		return nil
	}
	env := &config.Environment{APIBaseURL: apiBaseURL, Overrides: overrides}
	resolved, err := env.BuildOverrideConfigsWithProvider(ctx, base, provider)
	if err != nil {
		return err
	}
	for _, ov := range resolved {
		router.AddResolvedOverride(ov)
	}
	return nil
}

// writeRunArchive creates a run archive in the output directory and returns
// the archive path. The secrets of the environment, the plan, and any overlays
// the run used (nil entries are skipped) are redacted.
func writeRunArchive(result *engine.RunResult, p *plan.Plan, env *config.Environment, g *graph.Graph, outputDir string, layers []string, overlays ...*config.OverlayFile) (string, error) {
	secrets := config.RunSecrets(env, p.Auth, overlays...)
	runID := archive.GenerateRunID()
	meta := archive.ArchiveMetadata{
		Version:      "1.0.0",
		RunID:        runID,
		Timestamp:    time.Now(),
		Plan:         p,
		Environment:  env.Name,
		GraphVersion: g.Version,
		ToolVersion:  version.Effective(),
		Layers:       layers,
	}
	arc, err := engine.ToArchive(result, meta, env.APIBaseURL, secrets)
	if err != nil {
		return "", fmt.Errorf("writing archive: %w", err)
	}
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

	var lastRes *runResult
	for attempt := 1; attempt <= totalPossible; attempt++ {
		select {
		case <-ctx.Done():
			return &runResult{
				outcome:  engine.OutcomeAborted,
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
					outcome:  engine.OutcomeAborted,
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

// resolveEnvName extracts the --env flag value, falling back to the AAT_ENV_NAME
// environment variable if the flag was not explicitly set.
func resolveEnvName(cmd *cobra.Command) string {
	if cmd.Flags().Changed("env") {
		v, _ := cmd.Flags().GetString("env")
		return v
	}
	return os.Getenv("AAT_ENV_NAME")
}

// selectEnvName chooses the environment of a run, batch, or prompt: --env, then
// AAT_ENV_NAME, then the environment: of the --overlay file or of
// .aat-overrides.yaml, then the manifest's defaultEnvironment. The manifest's
// default applies only to the environment file the manifest names, not to one
// given with --env-config. A single-environment file has no environments to
// choose from, so only an explicit --env reaches it (and is rejected when the
// file loads); the other sources are defaults, and it ignores them.
func selectEnvName(cmd *cobra.Command, resolved *config.ProjectPaths, overlayPath string, noAutoOverrides bool) (string, error) {
	if cmd.Flags().Changed("env") {
		return resolveEnvName(cmd), nil
	}
	if resolved.EnvPath != "" {
		if multi, err := config.IsMultiEnvFile(resolved.EnvPath); err == nil && !multi {
			return "", nil
		}
	}
	if envName := os.Getenv("AAT_ENV_NAME"); envName != "" {
		return envName, nil
	}
	overlayEnv, overlaySrc, err := resolveOverlayEnvName(overlayPath, noAutoOverrides)
	if err != nil {
		return "", fmt.Errorf("resolving overlay environment: %w", err)
	}
	if overlayEnv != "" {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "aat: using environment %q from overlay %s\n", overlayEnv, overlaySrc)
		return overlayEnv, nil
	}
	if cmd.Flags().Changed("env-config") {
		return "", nil
	}
	return resolved.DefaultEnvName, nil
}

// resolveOverlayEnvName extracts an environment name from overlay files. Explicit
// --overlay wins over auto-discovered .aat-overrides.yaml. Returns (envName,
// sourcePath) for logging, or ("", "") when neither overlay sets the field or
// auto-discovery is disabled and no explicit overlay is supplied.
func resolveOverlayEnvName(explicitOverlayPath string, noAutoOverrides bool) (string, string, error) {
	if explicitOverlayPath != "" {
		env, err := config.PeekOverlayEnvironment(explicitOverlayPath)
		if err != nil {
			return "", "", err
		}
		if env != "" {
			return env, explicitOverlayPath, nil
		}
	}
	if !noAutoOverrides {
		if p := config.FindAutoOverrides(); p != "" {
			env, err := config.PeekOverlayEnvironment(p)
			if err != nil {
				return "", "", err
			}
			if env != "" {
				return env, p, nil
			}
		}
	}
	return "", "", nil
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
	if changed("env-config") {
		overrides.EnvPath = getString("env-config")
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
	GraphDir     string               // for recipe reconstitution
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
	OASValidateMode string         // effective mode: "auto", "strict", "off"

	// Execution options
	SkipMutations bool   // strip mutations from each plan before instantiation
	StopAfterStep string // stop after this step ID; skip cleanup (checkpoint handoff)
	DumpStatePath string // write accumulated run state to this file (mode 0600)
}

// loadRunContext loads all shared infrastructure from the given args.
// It loads environment, graph, templates, domain, determines execution mode,
// and creates an LLM client. This is the expensive setup that should happen once.
func loadRunContext(ctx context.Context, args *runArgs, logf func(string, ...any)) (*runContext, error) {
	if args.EnvPath == "" {
		return nil, fmt.Errorf("--env-config is required")
	}
	if args.GraphPath == "" {
		return nil, fmt.Errorf("--graph is required")
	}
	if args.TemplatesPath == "" {
		return nil, fmt.Errorf("--templates is required")
	}

	// 1. Load environment
	logf("aat: loading environment...\n")
	env, err := config.LoadNamedEnvironmentWithVars(args.EnvPath, args.EnvName, args.Vars)
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
		AuthProvider:      config.NewAuthProvider(env.Auth),
		Overrides:         args.Overrides,
		EnvOverlay:        args.EnvOverlay,
		AutoOverridesPath: autoOverridesPath,
		Layers:            args.Layers,
		LayersDir:         args.LayersDir,
		SkipMutations:     args.SkipMutations,
		StopAfterStep:     args.StopAfterStep,
		DumpStatePath:     args.DumpStatePath,
	}

	// Pre-load layers referenced by --layer and/or --layer-group flags.
	if allNames := collectAllLayerNames(args.Layers, args.LayerGroups); len(allNames) > 0 {
		layers, err := graph.ResolveLayerNames(allNames, rctx.LayersDir)
		if err != nil {
			return nil, fmt.Errorf("loading layers: %w", err)
		}
		rctx.AvailableLayers = layers
		logf("aat: loaded %d layers\n", len(layers))
	}

	// Resolve OAS validation mode: CLI flag > env setting > "auto"
	oasMode, err := resolveOASMode(args.OASValidateMode, env.Settings.OASValidation)
	if err != nil {
		return nil, err
	}
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
		reconstituted, reconErr := intent.Reconstitute(v, rctx.Graph, rctx.GraphDir,
			intent.WithLayersDir(rctx.LayersDir), intent.WithAvailableLayers(rctx.AvailableLayers))
		if reconErr != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("reconstituting recipe: %w", reconErr)}
		}
		p = reconstituted
	default:
		return &runResult{setupErr: true, err: fmt.Errorf("unexpected parse result type %T", parsed)}
	}

	// Optionally strip mutations before instantiation/validation so the run
	// exercises only the happy-path steps. Useful as a smoke-test mode.
	if rctx.SkipMutations {
		plan.StripMutations(p)
	}

	// Compute layered defaults from the effective set of layers (CLI + recipe).
	// This must happen after the switch so recipe-embedded layers are included.
	layeredDefaults, err := graph.LayeredDefaults(rctx.Graph, effectiveLayers, rctx.LayersDir)
	if err != nil {
		return &runResult{setupErr: true, err: err}
	}

	// 2. Validate plan against graph (with layers)
	if _, err := plan.InstantiateAndValidateWithLayers(p, rctx.Graph, layeredDefaults); err != nil {
		return &runResult{setupErr: true, err: fmt.Errorf("plan validation: %w", err)}
	}

	// 3. Pre-load overlay files to discover transaction-level auth before authenticating.
	// Priority: env auth < auto-overrides auth < env-overlay auth < plan auth.
	var autoOverlay *config.OverlayFile
	if rctx.AutoOverridesPath != "" {
		autoOverlay, err = config.LoadOverlayFile(rctx.AutoOverridesPath)
		if err != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("loading auto-overrides %s: %w", rctx.AutoOverridesPath, err)}
		}
	}
	var envOverlayFile *config.OverlayFile
	if rctx.EnvOverlay != "" {
		envOverlayFile, err = config.LoadOverlayFile(rctx.EnvOverlay)
		if err != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("loading overlay: %w", err)}
		}
	}

	// 4. Determine effective auth: overlay auth promotes to transaction level.
	effectiveAuth := rctx.Env.Auth
	overlayOverridesAuth := false
	if autoOverlay != nil && autoOverlay.Auth != nil {
		effectiveAuth = *autoOverlay.Auth
		overlayOverridesAuth = true
		logf("aat: using overlay auth from %s (%s)\n", rctx.AutoOverridesPath, effectiveAuth.Type)
	}
	if envOverlayFile != nil && envOverlayFile.Auth != nil {
		effectiveAuth = *envOverlayFile.Auth
		overlayOverridesAuth = true
		logf("aat: using overlay auth from %s (%s)\n", rctx.EnvOverlay, effectiveAuth.Type)
	}
	planOverridesAuth := p.Auth != nil
	if planOverridesAuth {
		effectiveAuth = *p.Auth
		logf("aat: using plan-level auth (%s)\n", effectiveAuth.Type)
	}

	// 5. Authenticate — use cached provider only when using unmodified env auth.
	// When overlay/plan overrides auth, create a fresh AuthProvider so the token
	// is cached for reuse by per-node override resolution (avoids duplicate auth calls).
	effectiveProvider := rctx.AuthProvider
	if planOverridesAuth || overlayOverridesAuth {
		effectiveProvider = config.NewAuthProvider(effectiveAuth)
	}
	token, err := effectiveProvider.Authenticate(ctx)
	if err != nil {
		return &runResult{setupErr: true, err: fmt.Errorf("authenticating: %w", err)}
	}
	apiConfig := rctx.Env.BuildAPIConfigFromToken(token, effectiveAuth, p.Headers)
	logf("aat: authenticated via %s\n", effectiveAuth.Type)

	// 5b. Overlay headers apply to every request on every route: after the
	// environment, plan, and credential headers, and out of reach of template
	// headers. .aat-overrides.yaml first, then the --overlay file.
	if autoOverlay != nil {
		apiConfig.AddOverlayHeaders(autoOverlay.Headers)
	}
	if envOverlayFile != nil {
		apiConfig.AddOverlayHeaders(envOverlayFile.Headers)
	}

	// 6. Create executor, environment config, and router
	executor := adapter.NewHTTPExecutor(apiConfig.BaseURL)
	envConfig := &adapter.EnvironmentConfig{
		BaseURL:   apiConfig.BaseURL,
		Headers:   apiConfig.Headers,
		Protected: apiConfig.Protected,
	}
	router := engine.NewExecutorRouter(executor, envConfig)

	// 6a–6d. Register per-node overrides from every source, lowest precedence
	// first: env.yaml, .aat-overrides.yaml, the --overlay file, then --override
	// flags. The router lets the last registered match of each kind win, and
	// every source resolves the same way, inheriting headers and the effective
	// credential unless an entry declares its own auth.
	flagOverrides, err := overrideFlagsToHostOverrides(rctx.Overrides)
	if err != nil {
		return &runResult{setupErr: true, err: err}
	}
	sources := []struct {
		label     string
		overrides []config.HostOverride
	}{
		{"overrides", rctx.Env.Overrides},
		{"auto-overrides", overlayOverrides(autoOverlay)},
		{"overlay overrides", overlayOverrides(envOverlayFile)},
		{"--override flags", flagOverrides},
	}
	for _, src := range sources {
		if err := addHostOverrides(ctx, router, rctx.Env.APIBaseURL, src.overrides, apiConfig, effectiveProvider); err != nil {
			return &runResult{setupErr: true, err: fmt.Errorf("building %s: %w", src.label, err)}
		}
	}

	// Log active overrides
	if router.HasOverrides() {
		for _, pat := range router.OverridePatterns() {
			logf("aat: override: %s\n", pat)
		}
	}

	// 7. Create engine and run
	eng := engine.NewEngine(rctx.Graph, rctx.Registry, router).
		WithDomain(rctx.KB).
		WithProgress(observer).
		WithLayers(layeredDefaults).
		WithEnvValues(rctx.Env.Values).
		WithStopAfter(rctx.StopAfterStep)

	if rctx.OASCache != nil {
		eng.WithOASSpecs(rctx.OASCache, rctx.Graph.OAS, rctx.OASValidateMode == "strict")
	}

	logf("aat: executing plan (%d steps)...\n\n", plannedStepCount(p, rctx.Graph, layeredDefaults))

	result := eng.Run(ctx, p)

	// 8. Write archive
	secrets := config.RunSecrets(rctx.Env, p.Auth, autoOverlay, envOverlayFile)

	meta := archive.ArchiveMetadata{
		Version:       "1.0.0",
		RunID:         filepath.Base(runDir),
		Timestamp:     time.Now(),
		Plan:          p,
		Environment:   rctx.Env.Name,
		GraphVersion:  rctx.Graph.Version,
		ToolVersion:   version.Effective(),
		Attempt:       attempt,
		TotalAttempts: totalAttempts,
		Layers:        effectiveLayers,
	}
	archivePath := filepath.Join(runDir, "archive.json")
	arc, archiveErr := engine.ToArchive(result, meta, rctx.Env.APIBaseURL, secrets)
	if archiveErr == nil {
		archiveErr = archive.Write(arc, archivePath)
	}
	if archiveErr != nil {
		logf("aat: warning: archive not written: %s\n", archiveErr)
		archivePath = ""
	} else {
		logf("Archive: %s\n", archivePath)
	}

	// Build machine-readable summary
	summary := buildRunSummary(result, archivePath)

	// 8b. Dump accumulated run state for external harness consumption.
	// A path of "-" means stdout: attach the export to the summary so it is
	// surfaced inline by --json (or printed standalone in non-JSON mode),
	// obviating the file. Any other value writes a 0600 file.
	// Non-fatal: a dump failure must not change the run's exit code.
	if rctx.DumpStatePath != "" {
		exp := engine.BuildStateExport(result, apiConfig.BaseURL)
		if rctx.DumpStatePath == "-" {
			summary.State = exp
		} else if dumpErr := engine.WriteStateExport(exp, rctx.DumpStatePath); dumpErr != nil {
			// Always visible: under --quiet or --json logf is silent, and a
			// harness waiting for this file needs to know it is missing.
			fmt.Fprintf(os.Stderr, "aat: warning: failed to write state dump: %s\n", dumpErr)
		} else {
			logf("aat: state dumped to %s\n", rctx.DumpStatePath)
		}
	}

	return &runResult{
		outcome:     result.Outcome,
		summary:     summary,
		archivePath: archivePath,
		err:         result.Error,
		layers:      effectiveLayers,
		secrets:     secrets,
	}
}

// resolveOASMode returns the effective OAS validation mode: the CLI flag, else
// the environment setting, else "auto". An unknown flag value is an error; the
// environment setting is validated when the environment loads.
func resolveOASMode(cliFlag, envSetting string) (string, error) {
	if cliFlag != "" {
		if err := config.CheckOASValidationMode(cliFlag); err != nil {
			return "", fmt.Errorf("--oas-validate: %w", err)
		}
		return cliFlag, nil
	}
	if envSetting != "" {
		return envSetting, nil
	}
	return "auto", nil
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
