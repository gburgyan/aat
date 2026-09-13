package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gburgyan/aat/config"
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
				mcp.Description("Optional filename to save the plan to the plans directory (e.g. 'booking-test.yaml'). Requires `plans` field in aat-project.yaml."),
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
			mcp.WithDescription("List saved test plans from the plans directory, showing name, goal, and step count for each. Requires `plans` field in aat-project.yaml."),
		),
		s.handleListSavedPlans,
	)

	s.mcp.AddTool(
		mcp.NewTool("load_plan",
			mcp.WithDescription("Load a saved plan from the plans directory. Returns the plan YAML and a human-readable narrative. Requires `plans` field in aat-project.yaml."),
			mcp.WithString("name",
				mcp.Description("Plan filename (e.g. 'booking-test' or 'booking-test.yaml')"),
				mcp.Required(),
			),
		),
		s.handleLoadPlan,
	)

	s.mcp.AddTool(
		mcp.NewTool("save_plan",
			mcp.WithDescription("Validate and save a plan YAML string to the plans directory. Requires `plans` field in aat-project.yaml."),
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
	if saveAs, _ := req.RequireString("save_as"); saveAs != "" {
		if err := checkPlanName(saveAs); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
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
		Prompt:   prompt,
		Graph:    s.ctx.Graph,
		KB:       s.ctx.KB,
		Client:   client,
		GraphDir: s.ctx.GraphDir,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("plan generation failed: %v", err)), nil
	}

	yamlBytes, err := plan.Marshal(result.Plan)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshalling plan: %v", err)), nil
	}

	// Optionally save (as recipe by default, compact and workflow-evolution-resilient)
	saveAs, _ := req.RequireString("save_as")
	if saveAs != "" && len(s.ctx.PlanDirs) > 0 {
		savePath, err := config.ResolvePlanWritePath(s.ctx.PlanDirs, saveAs)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("resolving save path: %v", err)), nil
		}
		if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("creating plans directory: %v", err)), nil
		}
		// Save as recipe if we have the workflow selection and targeted response.
		if result.WorkflowSelection != nil {
			r := &plan.Recipe{
				Kind:      "recipe",
				Metadata:  result.Plan.Metadata,
				Selection: intent.WorkflowSelectionToRecipeSelection(result.WorkflowSelection),
			}
			if result.TargetedResponse != nil {
				r.Overrides = intent.TargetedResponseToRecipeOverrides(result.TargetedResponse)
			}
			if err := plan.WriteRecipe(r, savePath); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("saving recipe: %v", err)), nil
			}
		} else {
			if err := plan.WriteFile(result.Plan, savePath); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("saving plan: %v", err)), nil
			}
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
		if len(s.ctx.PlanDirs) > 0 {
			savePath, _ := config.ResolvePlanWritePath(s.ctx.PlanDirs, saveAs)
			fmt.Fprintf(&b, "\n\nSaved to: %s", savePath)
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

	parsed, err := plan.ParseAny([]byte(yamlStr))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Parse failed:\n%v", err)), nil
	}

	var p *plan.Plan
	var warnings string
	switch v := parsed.(type) {
	case *plan.Plan:
		if _, err := plan.InstantiateAndValidate(v, s.ctx.Graph); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Validation failed:\n%v", err)), nil
		}
		p = v
		warnings = s.planWarnings(v)
	case *plan.Recipe:
		// Reconstitution validates the composed plan with the recipe's layers.
		reconstituted, reconErr := s.ctx.reconstitute(v)
		if reconErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("reconstituting recipe: %v", reconErr)), nil
		}
		p = reconstituted
	}

	summary := fmt.Sprintf("Plan is valid: %d steps, goal: %s", len(p.Execution.Steps), p.Intent.Goal)
	return mcp.NewToolResultText(summary + warnings), nil
}

// planWarnings lists, after a blank line, the problems in a plan file that
// don't fail validation, such as a required input that takes from: an optional
// output. It returns "" when there are none.
func (s *Server) planWarnings(p *plan.Plan) string {
	warnings := plan.RequiredFromOptionalValues(p, s.ctx.Graph)
	if len(warnings) == 0 {
		return ""
	}
	return "\n\nWarnings:\n- " + strings.Join(warnings, "\n- ")
}

// handleListSavedPlans scans the plans directories for YAML files.
func (s *Server) handleListSavedPlans(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if len(s.ctx.PlanDirs) == 0 {
		return mcp.NewToolResultText(
			"Plans directory not configured. Set the `plans` field in aat-project.yaml to enable plan management.",
		), nil
	}

	planEntries, err := config.ListPlans(s.ctx.PlanDirs)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("listing plans: %v", err)), nil
	}

	type planInfo struct {
		name      string
		goal      string
		stepCount int
	}

	var plans []planInfo
	for _, entry := range planEntries {
		parsed, err := plan.ParseAnyFile(entry.FullPath)
		if err != nil {
			plans = append(plans, planInfo{name: entry.Name, goal: "(parse error)", stepCount: 0})
			continue
		}
		switch v := parsed.(type) {
		case *plan.Plan:
			plans = append(plans, planInfo{
				name:      entry.Name,
				goal:      v.Intent.Goal,
				stepCount: len(v.Execution.Steps),
			})
		case *plan.Recipe:
			plans = append(plans, planInfo{
				name:      entry.Name,
				goal:      "[recipe] " + v.Selection.Description,
				stepCount: 0,
			})
		}
	}

	if len(plans) == 0 {
		return mcp.NewToolResultText("No plans found in the plans directory."), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d plan(s):\n\n", len(plans))
	for _, p := range plans {
		if p.stepCount > 0 {
			fmt.Fprintf(&b, "- **%s** — goal: %s, %d steps\n", p.name, p.goal, p.stepCount)
		} else {
			fmt.Fprintf(&b, "- **%s** — %s\n", p.name, p.goal)
		}
	}
	return mcp.NewToolResultText(b.String()), nil
}

// handleLoadPlan loads a plan from the plans directory and returns YAML + narrative.
// Recipes are reconstituted to full plans before generating the narrative.
func (s *Server) handleLoadPlan(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: name"), nil
	}
	if err := checkPlanName(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if len(s.ctx.PlanDirs) == 0 {
		return mcp.NewToolResultError("plans directory not configured — set the `plans` field in aat-project.yaml"), nil
	}

	path, err := config.FindPlan(s.ctx.PlanDirs, name)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("plan %q not found", name)), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reading plan: %v", err)), nil
	}

	parsed, err := plan.ParseAny(data)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("parsing plan: %s: %v", path, err)), nil
	}

	var p *plan.Plan
	switch v := parsed.(type) {
	case *plan.Plan:
		p = v
	case *plan.Recipe:
		reconstituted, reconErr := s.ctx.reconstitute(v)
		if reconErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("reconstituting recipe: %v", reconErr)), nil
		}
		p = reconstituted
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
// Accepts both full plan YAML and recipe YAML.
func (s *Server) handleSavePlan(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: name"), nil
	}
	if err := checkPlanName(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	yamlStr, err := req.RequireString("yaml")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: yaml"), nil
	}

	if len(s.ctx.PlanDirs) == 0 {
		return mcp.NewToolResultError("plans directory not configured — set the `plans` field in aat-project.yaml"), nil
	}

	parsed, err := plan.ParseAny([]byte(yamlStr))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Parse failed:\n%v", err)), nil
	}

	savePath, saveErr := config.ResolvePlanWritePath(s.ctx.PlanDirs, name)
	if saveErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving save path: %v", saveErr)), nil
	}
	if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("creating plans directory: %v", err)), nil
	}

	var summary string
	switch v := parsed.(type) {
	case *plan.Plan:
		if _, err := plan.InstantiateAndValidate(v, s.ctx.Graph); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Validation failed:\n%v", err)), nil
		}
		if err := plan.WriteFile(v, savePath); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("saving plan: %v", err)), nil
		}
		summary = fmt.Sprintf("Plan saved to %s (%d steps, goal: %s)", filepath.Base(savePath), len(v.Execution.Steps), v.Intent.Goal) + s.planWarnings(v)
	case *plan.Recipe:
		// Validate by reconstituting.
		if _, reconErr := s.ctx.reconstitute(v); reconErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Recipe validation failed:\n%v", reconErr)), nil
		}
		if err := plan.WriteRecipe(v, savePath); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("saving recipe: %v", err)), nil
		}
		summary = fmt.Sprintf("Recipe saved to %s (workflow: %s)", filepath.Base(savePath), v.Selection.Workflow)
	}

	return mcp.NewToolResultText(summary), nil
}

// resolveWorkflowPath resolves a plan name to a full file path, appending .yaml if needed.
func resolveWorkflowPath(workflowsDir, name string) string {
	ext := filepath.Ext(name)
	if ext != ".yaml" && ext != ".yml" {
		name = name + ".yaml"
	}
	return filepath.Join(workflowsDir, name)
}
