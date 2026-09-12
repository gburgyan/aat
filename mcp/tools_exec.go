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
	"github.com/gburgyan/aat/internal/version"
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
	if err := checkPlanName(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Guard: required configuration
	if len(s.ctx.PlanDirs) == 0 && s.ctx.WorkflowsDir == "" {
		return mcp.NewToolResultError("plans directory not configured — set the `plans` field in aat-project.yaml"), nil
	}
	if s.ctx.Environment == nil {
		return mcp.NewToolResultError("no environment configured — set the `environment` field in aat-project.yaml to enable execution"), nil
	}
	if s.ctx.ArchiveDir == "" {
		return mcp.NewToolResultError("archive directory not configured — set the `archives` field in aat-project.yaml to store results"), nil
	}

	// Search PlanDirs first, then fall back to WorkflowsDir
	var planPath string
	if len(s.ctx.PlanDirs) > 0 {
		if found, err := config.FindPlan(s.ctx.PlanDirs, name); err == nil {
			planPath = found
		}
	}
	if planPath == "" && s.ctx.WorkflowsDir != "" {
		planPath = resolveWorkflowPath(s.ctx.WorkflowsDir, name)
	}
	if planPath == "" {
		return mcp.NewToolResultError(fmt.Sprintf("plan %q not found", name)), nil
	}
	parsed, err := plan.ParseAnyFile(planPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading plan: %v", err)), nil
	}

	var p *plan.Plan
	var layers []string
	switch v := parsed.(type) {
	case *plan.Plan:
		p = v
	case *plan.Recipe:
		reconstituted, reconErr := s.ctx.reconstitute(v)
		if reconErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("reconstituting recipe: %v", reconErr)), nil
		}
		p = reconstituted
		layers = v.Selection.Layers
	}

	layeredDefaults, err := s.ctx.layeredDefaults(layers)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if _, err := plan.InstantiateAndValidateWithLayers(p, s.ctx.Graph, layeredDefaults); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("plan validation failed:\n%v", err)), nil
	}

	// Determine effective auth: plan auth overrides env auth
	effectiveAuth := s.ctx.Environment.Auth
	planOverridesAuth := p.Auth != nil
	if planOverridesAuth {
		effectiveAuth = *p.Auth
	}

	// Authenticate — use cached provider for default auth, direct call for plan auth
	var token *config.OAuthToken
	if planOverridesAuth {
		token, err = config.Authenticate(ctx, effectiveAuth)
	} else if s.ctx.AuthProvider != nil {
		token, err = s.ctx.AuthProvider.Authenticate(ctx)
	} else {
		token, err = config.Authenticate(ctx, effectiveAuth)
	}
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("authentication failed: %v", err)), nil
	}
	apiConfig := s.ctx.Environment.BuildAPIConfigFromToken(token, effectiveAuth, p.Headers)

	// Create executor and environment config
	executor := adapter.NewHTTPExecutor(apiConfig.BaseURL)
	envConfig := &adapter.EnvironmentConfig{
		BaseURL:   apiConfig.BaseURL,
		Headers:   apiConfig.Headers,
		Protected: apiConfig.Protected,
	}
	router := engine.NewExecutorRouter(executor, envConfig)

	// Apply env-file overrides (inherit plan auth if present)
	if len(s.ctx.Environment.Overrides) > 0 {
		var resolvedOverrides []config.ResolvedOverride
		if planOverridesAuth || s.ctx.AuthProvider == nil {
			resolvedOverrides, err = s.ctx.Environment.BuildOverrideConfigsWithAuth(ctx, apiConfig, effectiveAuth)
		} else {
			resolvedOverrides, err = s.ctx.Environment.BuildOverrideConfigsWithProvider(ctx, apiConfig, s.ctx.AuthProvider)
		}
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("building overrides: %v", err)), nil
		}
		for _, ov := range resolvedOverrides {
			router.AddResolvedOverride(ov)
		}
	}

	// Build and run engine
	eng := engine.NewEngine(s.ctx.Graph, s.ctx.Registry, router).
		WithDomain(s.ctx.KB).
		WithEnvValues(s.ctx.Environment.Values).
		WithLayers(layeredDefaults)

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
		ToolVersion:  version.Effective(),
	}
	secrets := config.RunSecrets(s.ctx.Environment, p.Auth)
	arc, err := engine.ToArchive(result, meta, s.ctx.Environment.APIBaseURL, secrets)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("writing archive: %v", err)), nil
	}
	archivePath := filepath.Join(s.ctx.ArchiveDir, runID, "archive.json")
	if err := archive.Write(arc, archivePath); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("writing archive: %v", err)), nil
	}

	// Format summary
	summary := formatExecutionSummary(result, runID)
	return mcp.NewToolResultText(summary), nil
}

// formatExecutionSummary produces a Markdown summary of a plan execution.
func formatExecutionSummary(result *engine.RunResult, runID string) string {
	var b strings.Builder

	// Outcome header
	fmt.Fprintf(&b, "## Execution: %s\n\n", result.Outcome.String())
	fmt.Fprintf(&b, "- **Run ID:** %s\n", runID)
	if result.Error != nil {
		fmt.Fprintf(&b, "- **Error:** %s\n", result.Error)
	}
	b.WriteString("\n")

	// Step summary table
	if len(result.Steps) > 0 {
		b.WriteString("| # | Step | Node | Status | Duration |\n")
		b.WriteString("|---|------|------|--------|----------|\n")

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

			stepID := step.StepID
			if stepID == "" {
				stepID = step.Node
			}
			fmt.Fprintf(&b, "| %d | %s | %s | %s | %s |\n",
				i+1, stepID, step.Node, status, formatDurationMs(step.Duration.Milliseconds()))
		}

		b.WriteString("\n")
		fmt.Fprintf(&b, "**Total duration:** %s\n", formatDurationMs(result.Elapsed().Milliseconds()))
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
