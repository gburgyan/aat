package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/intent"
	"github.com/gburgyan/aat/plan"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerWorkflowTools adds the workflow lifecycle tools to the MCP server.
func (s *Server) registerWorkflowTools() {
	s.mcp.AddTool(
		mcp.NewTool("list_workflows",
			mcp.WithDescription("List all named workflows in the graph, including addons and composed workflows. Shows kind, template, includes, PLACEHOLDER requirements, and step lists."),
		),
		s.handleListWorkflows,
	)

	s.mcp.AddTool(
		mcp.NewTool("instantiate_workflow",
			mcp.WithDescription("Load and compose a workflow template. Returns the skeleton plan YAML with unfed inputs marked. For composed workflows, addons are spliced and PLACEHOLDERs auto-wired. No LLM call — deterministic only."),
			mcp.WithString("workflow",
				mcp.Description("Workflow name (exact or case-insensitive match)"),
				mcp.Required(),
			),
			mcp.WithString("addons",
				mcp.Description("Comma-separated addon workflow names to compose (optional)"),
			),
		),
		s.handleInstantiateWorkflow,
	)

	s.mcp.AddTool(
		mcp.NewTool("scaffold_template",
			mcp.WithDescription("Generate a new workflow template skeleton from a backward chain. Input: goal node, optional intermediate nodes. Output: skeleton plan YAML with all wiring from graph edges, PLACEHOLDERs for user-provided values. Use validate_plan and save_plan to refine and persist."),
			mcp.WithString("goal",
				mcp.Description("Goal node name to build the plan toward"),
				mcp.Required(),
			),
		),
		s.handleScaffoldTemplate,
	)
}

// handleListWorkflows lists all workflows with their metadata.
func (s *Server) handleListWorkflows(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	g := s.ctx.Graph
	if len(g.Workflows) == 0 {
		return mcp.NewToolResultText("No workflows defined in the graph."), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## %d Workflow(s)\n\n", len(g.Workflows))

	for _, wf := range g.Workflows {
		fmt.Fprintf(&b, "### %s", wf.Name)
		if wf.IsAddon() {
			b.WriteString(" [addon]")
		}
		b.WriteString("\n")

		if wf.Description != "" {
			fmt.Fprintf(&b, "%s\n", wf.Description)
		}
		if wf.Template != "" {
			fmt.Fprintf(&b, "- Template: `%s`\n", wf.Template)
		}
		if len(wf.Steps) > 0 {
			fmt.Fprintf(&b, "- Steps: %s\n", strings.Join(wf.Steps, " → "))
		}
		if len(wf.Includes) > 0 {
			b.WriteString("- Includes:\n")
			for _, inc := range wf.Includes {
				fmt.Fprintf(&b, "  - **%s** after `%s`", inc.Workflow, inc.After)
				if len(inc.Wire) > 0 {
					var wires []string
					for k, v := range inc.Wire {
						wires = append(wires, k+"="+v)
					}
					fmt.Fprintf(&b, " (wire: %s)", strings.Join(wires, ", "))
				}
				b.WriteString("\n")
			}
		}

		// Show PLACEHOLDER requirements if template exists and is an addon.
		if wf.Template != "" && wf.IsAddon() {
			placeholders := s.listPlaceholders(wf)
			if len(placeholders) > 0 {
				b.WriteString("- PLACEHOLDERs: ")
				b.WriteString(strings.Join(placeholders, ", "))
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	return mcp.NewToolResultText(b.String()), nil
}

// listPlaceholders loads a workflow template and returns the list of
// PLACEHOLDER input names.
func (s *Server) listPlaceholders(wf graph.Workflow) []string {
	if wf.Template == "" {
		return nil
	}

	p, err := intent.LoadWorkflowTemplate(wf.Template, s.ctx.GraphDir, s.ctx.Graph)
	if err != nil {
		return nil
	}

	var placeholders []string
	seen := make(map[string]bool)
	for _, step := range p.Execution.Steps {
		for inputName, sv := range step.Values {
			if s, ok := sv.Default.(string); ok && s == "PLACEHOLDER" && !seen[inputName] {
				placeholders = append(placeholders, inputName)
				seen[inputName] = true
			}
		}
	}
	return placeholders
}

// handleInstantiateWorkflow loads and composes a workflow template.
func (s *Server) handleInstantiateWorkflow(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	workflowName, err := req.RequireString("workflow")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: workflow"), nil
	}

	addonsStr, _ := req.RequireString("addons")
	var addons []string
	if addonsStr != "" {
		for _, a := range strings.Split(addonsStr, ",") {
			a = strings.TrimSpace(a)
			if a != "" {
				addons = append(addons, a)
			}
		}
	}

	g := s.ctx.Graph

	var tpl *plan.Plan

	if len(addons) > 0 {
		// Try pre-composed workflow.
		composedWF, found := intent.FindComposedWorkflow(g, workflowName, addons)
		if found {
			composed, composeErr := intent.ComposeWorkflowTemplate(composedWF, s.ctx.GraphDir, g)
			if composeErr == nil {
				tpl = composed
			}
		}

		// Dynamic composition fallback.
		if tpl == nil {
			baseWF, baseFound := findWorkflowByName(g, workflowName)
			if !baseFound {
				return mcp.NewToolResultError(fmt.Sprintf("unknown workflow %q", workflowName)), nil
			}
			if baseWF.Template == "" {
				return mcp.NewToolResultError(fmt.Sprintf("workflow %q has no template", workflowName)), nil
			}
			syntheticWF := intent.BuildSyntheticWorkflow(g, baseWF, addons)
			composed, composeErr := intent.ComposeWorkflowTemplate(syntheticWF, s.ctx.GraphDir, g)
			if composeErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("composition failed: %v", composeErr)), nil
			}
			tpl = composed
		}
	} else {
		// Plain template load.
		wf, found := findWorkflowByName(g, workflowName)
		if !found {
			return mcp.NewToolResultError(fmt.Sprintf("unknown workflow %q", workflowName)), nil
		}
		if wf.Template == "" {
			return mcp.NewToolResultError(fmt.Sprintf("workflow %q has no template", workflowName)), nil
		}

		// If the workflow has includes, compose.
		if len(wf.Includes) > 0 {
			composed, composeErr := intent.ComposeWorkflowTemplate(wf, s.ctx.GraphDir, g)
			if composeErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("composition failed: %v", composeErr)), nil
			}
			tpl = composed
		} else {
			loaded, loadErr := intent.LoadWorkflowTemplate(wf.Template, s.ctx.GraphDir, g)
			if loadErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("loading template: %v", loadErr)), nil
			}
			tpl = loaded
		}
	}

	yamlBytes, err := plan.Marshal(tpl)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshalling plan: %v", err)), nil
	}

	unfed := intent.UnfedInputsFromTemplate(tpl, g)

	var b strings.Builder
	b.WriteString("## Instantiated Workflow\n\n```yaml\n")
	b.Write(yamlBytes)
	b.WriteString("```\n")

	if len(unfed) > 0 {
		b.WriteString("\n## Inputs That Need Values\n\n")
		for _, inp := range unfed {
			fmt.Fprintf(&b, "- %s\n", inp)
		}
	}

	narrative := plan.FormatNarrative(tpl, g)
	b.WriteString("\n## Narrative\n\n")
	b.WriteString(narrative)

	return mcp.NewToolResultText(b.String()), nil
}

// handleScaffoldTemplate generates a skeleton plan from a backward chain.
func (s *Server) handleScaffoldTemplate(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	goalNode, err := req.RequireString("goal")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: goal"), nil
	}

	g := s.ctx.Graph
	if g.Nodes[goalNode] == nil {
		return mcp.NewToolResultError(fmt.Sprintf("unknown goal node %q", goalNode)), nil
	}

	cr, err := graph.BackwardChain(g, graph.ChainOptions{
		Goals: []string{goalNode},
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("backward chaining failed: %v", err)), nil
	}

	skeleton := intent.BuildSkeleton(g, cr, &intent.GoalAnalysis{
		Goal:        goalNode,
		Description: fmt.Sprintf("Scaffold for %s", goalNode),
	}, "", time.Now())

	yamlBytes, err := plan.Marshal(skeleton)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshalling skeleton: %v", err)), nil
	}

	unfed := intent.UnfedInputs(g, cr)

	var b strings.Builder
	b.WriteString("## Scaffolded Template\n\n```yaml\n")
	b.Write(yamlBytes)
	b.WriteString("```\n")

	if len(unfed) > 0 {
		b.WriteString("\n## Inputs That Need Values\n\n")
		for _, inp := range unfed {
			fmt.Fprintf(&b, "- %s\n", inp)
		}
	}

	b.WriteString("\n## Chain\n\n")
	fmt.Fprintf(&b, "Nodes: %s\n", strings.Join(cr.Nodes, " → "))
	fmt.Fprintf(&b, "Entry nodes: %s\n", strings.Join(cr.EntryNodes, ", "))

	b.WriteString("\n*Next steps: fill in literal values, refine selection strategies, add assertions, then use `validate_plan` and `save_plan`.*\n")

	return mcp.NewToolResultText(b.String()), nil
}

// findWorkflowByName looks up a workflow by name (case-insensitive).
func findWorkflowByName(g *graph.Graph, name string) (graph.Workflow, bool) {
	for _, wf := range g.Workflows {
		if strings.EqualFold(wf.Name, name) {
			return wf, true
		}
	}
	return graph.Workflow{}, false
}
