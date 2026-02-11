package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gburgyan/aat/intent"
	"github.com/gburgyan/aat/llm"
	"github.com/gburgyan/aat/plan"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerPlanTools adds the plan lifecycle tools to the MCP server.
func (s *Server) registerPlanTools() {
	s.mcp.AddTool(
		mcp.NewTool("generate_plan",
			mcp.WithDescription("Generate an execution plan from a natural language prompt using the LLM pipeline. Returns plan YAML and narrative. Optionally saves to the plans directory."),
			mcp.WithString("prompt",
				mcp.Description("Natural language description of what to test"),
				mcp.Required(),
			),
			mcp.WithString("save_as",
				mcp.Description("Optional filename to save the plan to the plans directory (e.g. 'booking-test.yaml')"),
			),
		),
		s.handleGeneratePlan,
	)

	s.mcp.AddTool(
		mcp.NewTool("validate_plan",
			mcp.WithDescription("Parse and validate a plan YAML string against the API graph. Returns validation errors or confirms the plan is valid."),
			mcp.WithString("yaml",
				mcp.Description("Plan YAML content to validate"),
				mcp.Required(),
			),
		),
		s.handleValidatePlan,
	)

	s.mcp.AddTool(
		mcp.NewTool("list_saved_plans",
			mcp.WithDescription("List saved test plans from the plans directory, showing name, goal, and step count for each."),
		),
		s.handleListSavedPlans,
	)

	s.mcp.AddTool(
		mcp.NewTool("load_plan",
			mcp.WithDescription("Load a saved plan from the plans directory. Returns the plan YAML and a human-readable narrative."),
			mcp.WithString("name",
				mcp.Description("Plan filename (e.g. 'booking-test' or 'booking-test.yaml')"),
				mcp.Required(),
			),
		),
		s.handleLoadPlan,
	)

	s.mcp.AddTool(
		mcp.NewTool("save_plan",
			mcp.WithDescription("Validate and save a plan YAML string to the plans directory."),
			mcp.WithString("name",
				mcp.Description("Filename to save as (e.g. 'booking-test' or 'booking-test.yaml')"),
				mcp.Required(),
			),
			mcp.WithString("yaml",
				mcp.Description("Plan YAML content to save"),
				mcp.Required(),
			),
		),
		s.handleSavePlan,
	)
}

// handleGeneratePlan delegates to intent.Interpret to produce a plan from a prompt.
func (s *Server) handleGeneratePlan(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	prompt, err := req.RequireString("prompt")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: prompt"), nil
	}

	if s.ctx.Environment == nil {
		return mcp.NewToolResultError("no environment configured — set the `environment` field in aat-project.yaml to enable plan generation"), nil
	}
	if s.ctx.Environment.LLM.Endpoint == "" && s.ctx.Environment.LLM.Provider == "" {
		return mcp.NewToolResultError("no LLM configuration in the environment — add an `llm` section with endpoint and api_key"), nil
	}

	client, err := llm.NewClient(s.ctx.Environment.LLM)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("creating LLM client: %v", err)), nil
	}

	result, err := intent.Interpret(ctx, intent.InterpretRequest{
		Prompt: prompt,
		Graph:  s.ctx.Graph,
		KB:     s.ctx.KB,
		Client: client,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("plan generation failed: %v", err)), nil
	}

	yamlBytes, err := plan.Marshal(result.Plan)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshalling plan: %v", err)), nil
	}

	// Optionally save
	saveAs, _ := req.RequireString("save_as")
	if saveAs != "" && s.ctx.PlansDir != "" {
		savePath := resolveplanPath(s.ctx.PlansDir, saveAs)
		if err := os.MkdirAll(s.ctx.PlansDir, 0o755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("creating plans directory: %v", err)), nil
		}
		if err := plan.WriteFile(result.Plan, savePath); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("saving plan: %v", err)), nil
		}
	}

	narrative := plan.FormatNarrative(result.Plan, s.ctx.Graph)

	var b strings.Builder
	b.WriteString("## Generated Plan\n\n```yaml\n")
	b.Write(yamlBytes)
	b.WriteString("```\n\n")
	b.WriteString("## Narrative\n\n")
	b.WriteString(narrative)

	if saveAs != "" {
		if s.ctx.PlansDir != "" {
			fmt.Fprintf(&b, "\n\nSaved to: %s", resolveplanPath(s.ctx.PlansDir, saveAs))
		} else {
			b.WriteString("\n\n*Note: plans directory not configured — plan was not saved. Set the `plans` field in aat-project.yaml.*")
		}
	}

	return mcp.NewToolResultText(b.String()), nil
}

// handleValidatePlan parses and validates a YAML plan against the graph.
func (s *Server) handleValidatePlan(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlStr, err := req.RequireString("yaml")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: yaml"), nil
	}

	p, err := plan.Parse([]byte(yamlStr))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	if err := plan.Validate(p, s.ctx.Graph); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Validation failed:\n%v", err)), nil
	}

	summary := fmt.Sprintf("Plan is valid: %d steps, goal: %s", len(p.Execution.Steps), p.Intent.Goal)
	return mcp.NewToolResultText(summary), nil
}

// handleListSavedPlans scans the plans directory for YAML files.
func (s *Server) handleListSavedPlans(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.ctx.PlansDir == "" {
		return mcp.NewToolResultText(
			"Plans directory not configured. Set the `plans` field in aat-project.yaml to enable plan management.",
		), nil
	}

	entries, err := os.ReadDir(s.ctx.PlansDir)
	if err != nil {
		if os.IsNotExist(err) {
			return mcp.NewToolResultText("Plans directory is empty (no plans saved yet)."), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("reading plans directory: %v", err)), nil
	}

	type planInfo struct {
		name      string
		goal      string
		stepCount int
	}

	var plans []planInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(s.ctx.PlansDir, entry.Name())
		p, err := plan.ParseFile(path)
		if err != nil {
			// Include the file but note it's unparseable
			plans = append(plans, planInfo{name: entry.Name(), goal: "(parse error)", stepCount: 0})
			continue
		}
		plans = append(plans, planInfo{
			name:      entry.Name(),
			goal:      p.Intent.Goal,
			stepCount: len(p.Execution.Steps),
		})
	}

	if len(plans) == 0 {
		return mcp.NewToolResultText("No plans found in the plans directory."), nil
	}

	sort.Slice(plans, func(i, j int) bool { return plans[i].name < plans[j].name })

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d plan(s):\n\n", len(plans))
	for _, p := range plans {
		fmt.Fprintf(&b, "- **%s** — goal: %s, %d steps\n", p.name, p.goal, p.stepCount)
	}
	return mcp.NewToolResultText(b.String()), nil
}

// handleLoadPlan loads a plan from the plans directory and returns YAML + narrative.
func (s *Server) handleLoadPlan(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: name"), nil
	}

	if s.ctx.PlansDir == "" {
		return mcp.NewToolResultError("plans directory not configured — set the `plans` field in aat-project.yaml"), nil
	}

	path := resolveplanPath(s.ctx.PlansDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return mcp.NewToolResultError(fmt.Sprintf("plan %q not found", name)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("reading plan: %v", err)), nil
	}

	p, err := plan.Parse(data)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("parsing plan: %v", err)), nil
	}

	narrative := plan.FormatNarrative(p, s.ctx.Graph)

	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n```yaml\n", filepath.Base(path))
	b.Write(data)
	b.WriteString("```\n\n")
	b.WriteString("## Narrative\n\n")
	b.WriteString(narrative)

	return mcp.NewToolResultText(b.String()), nil
}

// handleSavePlan validates and saves a plan YAML to the plans directory.
func (s *Server) handleSavePlan(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: name"), nil
	}
	yamlStr, err := req.RequireString("yaml")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: yaml"), nil
	}

	if s.ctx.PlansDir == "" {
		return mcp.NewToolResultError("plans directory not configured — set the `plans` field in aat-project.yaml"), nil
	}

	p, err := plan.Parse([]byte(yamlStr))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	if err := plan.Validate(p, s.ctx.Graph); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Validation failed:\n%v", err)), nil
	}

	savePath := resolveplanPath(s.ctx.PlansDir, name)
	if err := plan.WriteFile(p, savePath); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("saving plan: %v", err)), nil
	}

	summary := fmt.Sprintf("Plan saved to %s (%d steps, goal: %s)", filepath.Base(savePath), len(p.Execution.Steps), p.Intent.Goal)
	return mcp.NewToolResultText(summary), nil
}

// resolveplanPath resolves a plan name to a full file path, appending .yaml if needed.
func resolveplanPath(plansDir, name string) string {
	ext := filepath.Ext(name)
	if ext != ".yaml" && ext != ".yml" {
		name = name + ".yaml"
	}
	return filepath.Join(plansDir, name)
}
