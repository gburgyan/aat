package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/llm"
	"github.com/gburgyan/aat/plan"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerExecTools adds plan execution tools to the MCP server.
func (s *Server) registerExecTools() {
	s.mcp.AddTool(
		mcp.NewTool("execute_plan",
			mcp.WithDescription("Execute a saved test plan against the API. Authenticates, runs the engine, writes an archive, and returns a summary. Use inspect_archive to see full details."),
			mcp.WithString("name",
				mcp.Description("Plan filename (e.g. 'booking-test' or 'booking-test.yaml')"),
				mcp.Required(),
			),
			mcp.WithString("mode",
				mcp.Description("Execution mode: strict (no LLM), lean (LLM after pool exhausted), or adaptive (lean + relaxation). Defaults to environment setting or strict."),
			),
		),
		s.handleExecutePlan,
	)
}

// handleExecutePlan loads a plan, authenticates, runs the engine, and writes an archive.
func (s *Server) handleExecutePlan(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: name"), nil
	}

	// Guard: required configuration
	if s.ctx.PlansDir == "" {
		return mcp.NewToolResultError("plans directory not configured — set the `plans` field in aat-project.yaml"), nil
	}
	if s.ctx.Environment == nil {
		return mcp.NewToolResultError("no environment configured — set the `environment` field in aat-project.yaml to enable execution"), nil
	}
	if s.ctx.ArchiveDir == "" {
		return mcp.NewToolResultError("archive directory not configured — set the `archives` field in aat-project.yaml to store results"), nil
	}

	// Load and validate plan
	planPath := resolveplanPath(s.ctx.PlansDir, name)
	p, err := plan.ParseFile(planPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading plan: %v", err)), nil
	}

	if _, err := plan.InstantiateAndValidate(p, s.ctx.Graph); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("plan validation failed:\n%v", err)), nil
	}

	// Determine effective auth: plan auth overrides env auth
	effectiveAuth := s.ctx.Environment.Auth
	if p.Auth != nil {
		effectiveAuth = *p.Auth
	}

	// Authenticate (fresh each call — tokens expire)
	apiConfig, err := s.ctx.Environment.BuildAPIConfigFromAuth(ctx, effectiveAuth, p.Headers)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("authentication failed: %v", err)), nil
	}

	// Create executor and environment config
	executor := adapter.NewHTTPExecutor(apiConfig.BaseURL)
	envConfig := &adapter.EnvironmentConfig{
		BaseURL: apiConfig.BaseURL,
		Headers: apiConfig.Headers,
		Values:  apiConfig.Values,
	}
	router := engine.NewExecutorRouter(executor, envConfig)

	// Apply env-file overrides (inherit plan auth if present)
	if len(s.ctx.Environment.Overrides) > 0 {
		resolvedOverrides, err := s.ctx.Environment.BuildOverrideConfigsWithAuth(ctx, apiConfig.Headers, effectiveAuth)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("building overrides: %v", err)), nil
		}
		for _, ov := range resolvedOverrides {
			ovExec := adapter.NewHTTPExecutor(ov.APIConfig.BaseURL)
			ovCfg := &adapter.EnvironmentConfig{
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
			router.AddOverride(ov.Pattern, ovExec, ovCfg, rewrite)
		}
	}

	// Determine execution mode
	modeStr, _ := req.RequireString("mode")
	effectiveMode := config.ExecutionMode(modeStr)
	if effectiveMode == "" {
		effectiveMode = s.ctx.Environment.LLM.Mode
	}
	if effectiveMode == "" {
		effectiveMode = config.ModeStrict
	}

	// Validate mode
	switch effectiveMode {
	case config.ModeStrict, config.ModeLean, config.ModeAdaptive:
		// valid
	default:
		return mcp.NewToolResultError(fmt.Sprintf("invalid mode %q — use strict, lean, or adaptive", effectiveMode)), nil
	}

	// Create LLM client if mode requires it
	var llmClient llm.Client
	if effectiveMode != config.ModeStrict && s.ctx.Environment.LLM.Endpoint != "" {
		llmClient, err = llm.NewClient(s.ctx.Environment.LLM)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("creating LLM client: %v", err)), nil
		}
	}

	// Build and run engine
	eng := engine.NewEngine(s.ctx.Graph, s.ctx.Registry, router).
		WithMode(effectiveMode).
		WithDomain(s.ctx.KB).
		WithLLM(llmClient).
		WithMaxRelaxationDepth(s.ctx.Environment.Settings.MaxRelaxationDepth)

	result := eng.Run(ctx, p)

	// Write archive
	runID := archive.GenerateRunID()
	meta := archive.ArchiveMetadata{
		Version:      "1.0.0",
		RunID:        runID,
		Timestamp:    time.Now(),
		Plan:         p,
		Environment:  s.ctx.Environment.Name,
		GraphVersion: s.ctx.Graph.Version,
		ToolVersion:  "0.1.0",
	}
	secrets := s.ctx.Environment.CollectSecrets()
	if p.Auth != nil {
		for k, v := range config.CollectAuthSecrets(p.Auth) {
			secrets[k] = v
		}
	}
	arc := engine.ToArchive(result, meta, s.ctx.Environment.APIBaseURL, secrets)
	archivePath := filepath.Join(s.ctx.ArchiveDir, runID, "archive.json")
	if err := archive.Write(arc, archivePath); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("writing archive: %v", err)), nil
	}

	// Format summary
	summary := formatExecutionSummary(result, runID, effectiveMode)
	return mcp.NewToolResultText(summary), nil
}

// formatExecutionSummary produces a Markdown summary of a plan execution.
func formatExecutionSummary(result *engine.RunResult, runID string, mode config.ExecutionMode) string {
	var b strings.Builder

	// Outcome header
	fmt.Fprintf(&b, "## Execution: %s\n\n", result.Outcome.String())
	fmt.Fprintf(&b, "- **Run ID:** %s\n", runID)
	fmt.Fprintf(&b, "- **Mode:** %s\n", mode)
	if result.Error != nil {
		fmt.Fprintf(&b, "- **Error:** %s\n", result.Error)
	}
	b.WriteString("\n")

	// Step summary table
	if len(result.Steps) > 0 {
		b.WriteString("| # | Node | Status | Duration |\n")
		b.WriteString("|---|------|--------|----------|\n")

		var totalDur time.Duration
		for i, step := range result.Steps {
			status := "OK"
			if step.Error != nil {
				status = "ERROR"
			} else if step.ExpectFailure != nil {
				if step.ExpectFailure.Passed {
					status = fmt.Sprintf("EXPECTED %d", step.StatusCode)
				} else {
					status = fmt.Sprintf("UNEXPECTED %d", step.StatusCode)
				}
			} else if step.StatusCode >= 400 {
				status = fmt.Sprintf("%d", step.StatusCode)
			} else if step.Response != nil {
				status = fmt.Sprintf("%d", step.StatusCode)
			}

			if step.Validation != nil && !step.Validation.Passed {
				status += " (assertions failed)"
			}

			fmt.Fprintf(&b, "| %d | %s | %s | %s |\n",
				i+1, step.Node, status, formatDurationMs(step.Duration.Milliseconds()))
			totalDur += step.Duration
		}

		b.WriteString("\n")
		fmt.Fprintf(&b, "**Total duration:** %s\n", formatDurationMs(totalDur.Milliseconds()))
	}

	// Cleanup
	if len(result.CleanupResults) > 0 {
		b.WriteString("\n### Cleanup\n\n")
		for _, step := range result.CleanupResults {
			status := "OK"
			if step.Error != nil {
				status = "ERROR"
			} else if step.Response != nil {
				status = fmt.Sprintf("%d", step.StatusCode)
			}
			fmt.Fprintf(&b, "- %s: %s (%s)\n",
				step.Node, status, formatDurationMs(step.Duration.Milliseconds()))
		}
	}

	fmt.Fprintf(&b, "\n**Archive:** %s\n", runID)
	b.WriteString("\nUse `inspect_archive` to see full request/response details.")

	return b.String()
}
