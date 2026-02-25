package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/intent"
	"github.com/gburgyan/aat/plan"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerWorkflowTools adds the workflow lifecycle tools to the MCP server.
func (s *Server) registerWorkflowTools() {
	s.mcp.AddTool(
		mcp.NewTool("list_workflows",
			mcp.WithDescription("List all named workflows in the graph, including addons and composed workflows. Shows kind, template, includes, AUTOWIRE requirements, and step lists."),
		),
		s.handleListWorkflows,
	)

	s.mcp.AddTool(
		mcp.NewTool("instantiate_workflow",
			mcp.WithDescription("Load and compose a workflow template. Returns the skeleton plan YAML with unfed inputs marked. For composed workflows, slots are filled and addons are spliced with AUTOWIRE values auto-wired. No LLM call — deterministic only."),
			mcp.WithString("workflow",
				mcp.Description("Workflow name (exact or case-insensitive match)"),
				mcp.Required(),
			),
			mcp.WithString("choices",
				mcp.Description("Comma-separated slot choices as slot=option pairs (e.g., 'trip-search=Round-Trip,payment=Cash')"),
			),
			mcp.WithString("addons",
				mcp.Description("Comma-separated addon workflow names to compose (optional)"),
			),
		),
		s.handleInstantiateWorkflow,
	)

	s.mcp.AddTool(
		mcp.NewTool("get_workflow_detail",
			mcp.WithDescription("Show an enriched step-by-step recipe for a workflow: HTTP methods, data flow between steps, value sources, selections, outputs, unfed inputs, and cleanup. Optionally compose with addons."),
			mcp.WithString("workflow",
				mcp.Description("Workflow name (exact or case-insensitive match)"),
				mcp.Required(),
			),
			mcp.WithString("addons",
				mcp.Description("Comma-separated addon workflow names to compose (optional)"),
			),
			mcp.WithBoolean("summary",
				mcp.Description("If true (default), show compact data flow arrows without full value tables"),
			),
		),
		s.handleGetWorkflowDetail,
	)
}

// registerIntegrationWorkflowTools registers workflow tools for the API persona.
func (s *Server) registerIntegrationWorkflowTools() {
	s.mcp.AddTool(
		mcp.NewTool("list_integration_flows",
			mcp.WithDescription("List all integration flows with decision points (slots) and optional extensions (addons). Shows the available API workflows, their step sequences, and composable extensions."),
		),
		s.handleListWorkflows,
	)

	s.mcp.AddTool(
		mcp.NewTool("get_integration_flow",
			mcp.WithDescription("Show an enriched step-by-step recipe for an integration flow: HTTP methods, data flow between operations, value sources, selections, outputs, and required inputs. Optionally compose with addons to see the full extended flow."),
			mcp.WithString("workflow",
				mcp.Description("Integration flow name (exact or case-insensitive match)"),
				mcp.Required(),
			),
			mcp.WithString("addons",
				mcp.Description("Comma-separated addon names to compose into the flow (optional)"),
			),
			mcp.WithBoolean("summary",
				mcp.Description("If true (default), show compact data flow arrows without full value tables"),
			),
		),
		s.handleGetWorkflowDetail,
	)
}

// registerTestWorkflowTools registers workflow tools for the test persona.
func (s *Server) registerTestWorkflowTools() {
	s.mcp.AddTool(
		mcp.NewTool("list_workflows",
			mcp.WithDescription("List all named workflows in the graph, including addons and composed workflows. Shows kind, template, includes, AUTOWIRE requirements, and step lists."),
		),
		s.handleListWorkflows,
	)

	s.mcp.AddTool(
		mcp.NewTool("instantiate_workflow",
			mcp.WithDescription("Load and compose a workflow template. Returns the skeleton plan YAML with unfed inputs marked. For composed workflows, slots are filled and addons are spliced with AUTOWIRE values auto-wired. No LLM call — deterministic only."),
			mcp.WithString("workflow",
				mcp.Description("Workflow name (exact or case-insensitive match)"),
				mcp.Required(),
			),
			mcp.WithString("choices",
				mcp.Description("Comma-separated slot choices as slot=option pairs (e.g., 'trip-search=Round-Trip,payment=Cash')"),
			),
			mcp.WithString("addons",
				mcp.Description("Comma-separated addon workflow names to compose (optional)"),
			),
		),
		s.handleInstantiateWorkflow,
	)

	s.mcp.AddTool(
		mcp.NewTool("get_workflow_detail",
			mcp.WithDescription("Show an enriched step-by-step recipe for a workflow: HTTP methods, data flow between steps, value sources, selections, outputs, unfed inputs, and cleanup. Optionally compose with addons."),
			mcp.WithString("workflow",
				mcp.Description("Workflow name (exact or case-insensitive match)"),
				mcp.Required(),
			),
			mcp.WithString("addons",
				mcp.Description("Comma-separated addon workflow names to compose (optional)"),
			),
			mcp.WithBoolean("summary",
				mcp.Description("If true (default), show compact data flow arrows without full value tables"),
			),
		),
		s.handleGetWorkflowDetail,
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
		} else if wf.IsSlot() {
			b.WriteString(" [slot]")
		}
		b.WriteString("\n")

		if wf.Description != "" {
			fmt.Fprintf(&b, "%s\n", wf.Description)
		}
		if wf.Template != "" {
			fmt.Fprintf(&b, "- Template: `%s`\n", wf.Template)
		}
		if wf.After.IsSet() {
			fmt.Fprintf(&b, "- After: `%s`\n", wf.After.String())
		}
		if wf.Priority != 0 {
			fmt.Fprintf(&b, "- Priority: %d\n", wf.Priority)
		}
		if len(wf.Wire) > 0 {
			b.WriteString("- Wire:")
			for k, v := range wf.Wire {
				fmt.Fprintf(&b, " %s=%s", k, v)
			}
			b.WriteString("\n")
		}

		// Show slot definitions if present.
		if len(wf.Slots) > 0 {
			b.WriteString("- Slots:\n")
			for _, sd := range wf.Slots {
				defaultStr := ""
				if sd.Default != "" {
					defaultStr = fmt.Sprintf(" (default: %s)", sd.Default)
				}
				fmt.Fprintf(&b, "  - **%s**: %s%s\n", sd.Name, strings.Join(sd.Options, ", "), defaultStr)
				if sd.Description != "" {
					fmt.Fprintf(&b, "    %s\n", sd.Description)
				}
			}
		}

		// Show AUTOWIRE requirements if template exists and is an addon.
		if wf.Template != "" && wf.IsAddon() {
			placeholders := s.listPlaceholders(wf)
			if len(placeholders) > 0 {
				b.WriteString("- AUTOWIRE inputs: ")
				b.WriteString(strings.Join(placeholders, ", "))
				b.WriteString("\n")
			}
		}

		// Show step summary if template exists.
		if wf.Template != "" {
			if stepSummary := s.stepSummary(wf); stepSummary != "" {
				b.WriteString(stepSummary)
			}
		}
		b.WriteString("\n")
	}

	return mcp.NewToolResultText(b.String()), nil
}

// stepSummary loads a workflow template and returns a formatted step summary line.
func (s *Server) stepSummary(wf graph.Workflow) string {
	if wf.Template == "" {
		return ""
	}

	p, err := intent.LoadWorkflowTemplate(wf.Template, s.ctx.GraphDir, s.ctx.Graph)
	if err != nil {
		return ""
	}

	if len(p.Execution.Steps) == 0 {
		return ""
	}

	nodes := make([]string, len(p.Execution.Steps))
	for i, step := range p.Execution.Steps {
		if step.IsSlotMarker() {
			nodes[i] = "[" + step.Slot + "]"
		} else {
			nodes[i] = step.Node
		}
	}
	return fmt.Sprintf("- Steps: %d — %s\n", len(nodes), strings.Join(nodes, " → "))
}

// listPlaceholders loads a workflow template and returns the list of
// AUTOWIRE input names.
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
			if s, ok := sv.Default.(string); ok && (s == "AUTOWIRE" || s == "PLACEHOLDER") && !seen[inputName] {
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

	choicesStr, _ := req.RequireString("choices")
	choices := parseChoicesParam(choicesStr)

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

	wf, found := findWorkflowByName(g, workflowName)
	if !found {
		return mcp.NewToolResultError(fmt.Sprintf("unknown workflow %q", workflowName)), nil
	}
	if wf.Template == "" {
		return mcp.NewToolResultError(fmt.Sprintf("workflow %q has no template", workflowName)), nil
	}

	var tpl *plan.Plan

	if len(wf.Slots) > 0 {
		composed, composeErr := intent.ComposeWithSlotsAndAddons(wf, choices, addons, g, s.ctx.GraphDir)
		if composeErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("composition failed: %v", composeErr)), nil
		}
		tpl = composed
	} else if len(addons) > 0 {
		composed, composeErr := intent.ComposeWithAddons(wf, addons, g, s.ctx.GraphDir)
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

// handleGetWorkflowDetail returns an enriched step-by-step recipe for a workflow.
func (s *Server) handleGetWorkflowDetail(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	wf, found := findWorkflowByName(g, workflowName)
	if !found {
		return mcp.NewToolResultError(fmt.Sprintf("unknown workflow %q", workflowName)), nil
	}
	if wf.Template == "" {
		return mcp.NewToolResultError(fmt.Sprintf("workflow %q has no template", workflowName)), nil
	}

	var p *plan.Plan
	if len(wf.Slots) > 0 {
		// For workflows with slots, use defaults for get_workflow_detail.
		composed, composeErr := intent.ComposeWithSlots(wf, nil, g, s.ctx.GraphDir)
		if composeErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("slot composition failed: %v", composeErr)), nil
		}
		if len(addons) > 0 {
			composed, composeErr = intent.ComposeWithSlotsAndAddons(wf, nil, addons, g, s.ctx.GraphDir)
			if composeErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("composition failed: %v", composeErr)), nil
			}
		}
		p = composed
	} else if len(addons) > 0 {
		composed, composeErr := intent.ComposeWithAddons(wf, addons, g, s.ctx.GraphDir)
		if composeErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("composition failed: %v", composeErr)), nil
		}
		p = composed
	} else {
		loaded, loadErr := intent.LoadWorkflowTemplate(wf.Template, s.ctx.GraphDir, g)
		if loadErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("loading template: %v", loadErr)), nil
		}
		p = loaded
	}

	summaryMode := req.GetBool("summary", true)
	detail := formatWorkflowDetail(wf, p, g, s.ctx.Registry, summaryMode)
	return mcp.NewToolResultText(detail), nil
}

// formatWorkflowDetail renders an enriched step-by-step recipe for a workflow plan.
// When summary is true, value tables are replaced with compact data flow arrows.
func formatWorkflowDetail(wf graph.Workflow, p *plan.Plan, g *graph.Graph, reg *adapter.Registry, summary bool) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Workflow: %s\n\n", wf.Name)
	if wf.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", wf.Description)
	}

	fmt.Fprintf(&b, "## Steps (%d)\n", len(p.Execution.Steps))

	for i, step := range p.Execution.Steps {
		fmt.Fprintf(&b, "\n### %d. %s\n", i+1, step.StepID())

		node := g.Nodes[step.Node]

		if step.Description != "" {
			fmt.Fprintf(&b, "**Description:** %s\n", step.Description)
		} else if node != nil && node.Description != "" {
			fmt.Fprintf(&b, "**Description:** %s\n", node.Description)
		}

		// HTTP info from template
		if node != nil {
			if tmpl, ok := reg.GetTemplate(node.Adapter); ok {
				fmt.Fprintf(&b, "**HTTP:** %s %s\n", tmpl.Request.Method, tmpl.Request.Path)
			}
		}

		// Dependencies
		if len(step.DependsOn) > 0 {
			fmt.Fprintf(&b, "**Depends on:** %s\n", strings.Join(step.DependsOn, ", "))
		} else {
			b.WriteString("**Depends on:** (none)\n")
		}

		if !summary {
			// Values table (full detail mode)
			if len(step.Values) > 0 {
				b.WriteString("\n**Values:**\n")
				b.WriteString("| Input | Source | Value/Ref |\n")
				b.WriteString("|-------|--------|-----------|\n")

				// Sort value keys for deterministic output.
				valueKeys := make([]string, 0, len(step.Values))
				for k := range step.Values {
					valueKeys = append(valueKeys, k)
				}
				sort.Strings(valueKeys)

				for _, inputName := range valueKeys {
					sv := step.Values[inputName]
					source, ref := classifyStepValue(sv)
					fmt.Fprintf(&b, "| %s | %s | %s |\n", inputName, source, ref)
				}
			}

			// Selections
			if len(step.Selections) > 0 {
				b.WriteString("\n**Selections:**\n")
				selKeys := make([]string, 0, len(step.Selections))
				for k := range step.Selections {
					selKeys = append(selKeys, k)
				}
				sort.Strings(selKeys)
				for _, selName := range selKeys {
					sel := step.Selections[selName]
					desc := fmt.Sprintf("- **%s** from %s", selName, sel.From)
					if sel.Strategy != "" {
						desc += fmt.Sprintf(" — strategy: %s", sel.Strategy)
					}
					if sel.Filter != "" {
						desc += fmt.Sprintf(", filter: `%s`", sel.Filter)
					}
					b.WriteString(desc + "\n")
				}
			}

			// Outputs from graph node
			if node != nil && len(node.Outputs) > 0 {
				b.WriteString("\n**Outputs:**\n")
				b.WriteString("| Output | Type | Element Fields |\n")
				b.WriteString("|--------|------|----------------|\n")
				for _, out := range node.Outputs {
					efNames := ""
					if len(out.ElementFields) > 0 {
						names := make([]string, len(out.ElementFields))
						for j, ef := range out.ElementFields {
							names[j] = ef.Name
						}
						efNames = strings.Join(names, ", ")
					}
					fmt.Fprintf(&b, "| %s | %s | %s |\n", out.Name, out.Type, efNames)
				}
			}
		}

		b.WriteString("\n---\n")
	}

	// Data flow summary
	flows := buildDataFlowSummary(p, g)
	if len(flows) > 0 {
		b.WriteString("\n## Data Flow\n\n")
		for _, flow := range flows {
			b.WriteString(flow + "\n")
		}
	}

	// Unfed inputs
	unfed := intent.UnfedInputsFromTemplate(p, g)
	if len(unfed) > 0 {
		b.WriteString("\n## Unfed Inputs\n\n")
		for _, inp := range unfed {
			fmt.Fprintf(&b, "- %s\n", inp)
		}
	}

	// Cleanup
	if len(p.Execution.Cleanup) > 0 {
		b.WriteString("\n## Cleanup\n\n")
		for _, cs := range p.Execution.Cleanup {
			runOn := cs.RunOn
			if runOn == "" {
				runOn = "always"
			}
			fmt.Fprintf(&b, "- %s (runOn: %s)\n", cs.Node, runOn)
		}
	}

	return b.String()
}

// classifyStepValue determines the source kind and display reference for a plan step value.
func classifyStepValue(sv plan.StepValue) (source, ref string) {
	switch {
	case sv.From != "" && sv.Select != nil:
		return "from+select", sv.From + " (" + sv.Select.Strategy + ")"
	case sv.From != "":
		return "from", sv.From
	case sv.FromSelection != "":
		return "fromSelection", sv.FromSelection
	case sv.Default != nil:
		s := fmt.Sprintf("%v", sv.Default)
		if s == "AUTOWIRE" || s == "PLACEHOLDER" {
			return "AUTOWIRE", "(needs wiring)"
		}
		return "literal", s
	default:
		return "empty", "(unset)"
	}
}

// buildDataFlowSummary walks all from/fromSelection references in the plan
// and produces a list of "sourceStep → targetStep (outputNames)" lines.
func buildDataFlowSummary(p *plan.Plan, g *graph.Graph) []string {
	// Collect edges: sourceStep → targetStep → set of output/field names.
	type edgeKey struct {
		from, to string
	}
	edges := make(map[edgeKey]map[string]bool)

	addEdge := func(fromStep, toStep, field string) {
		key := edgeKey{fromStep, toStep}
		if edges[key] == nil {
			edges[key] = make(map[string]bool)
		}
		edges[key][field] = true
	}

	for _, step := range p.Execution.Steps {
		stepID := step.StepID()

		for inputName, sv := range step.Values {
			if sv.From != "" {
				parts := strings.SplitN(sv.From, ".", 2)
				if len(parts) == 2 {
					addEdge(parts[0], stepID, parts[1])
				}
			}
			if sv.FromSelection != "" {
				selName, _ := plan.ParseFromSelection(sv.FromSelection)
				if sel, ok := step.Selections[selName]; ok && sel.From != "" {
					parts := strings.SplitN(sel.From, ".", 2)
					if len(parts) == 2 {
						addEdge(parts[0], stepID, inputName)
					}
				}
			}
		}

		for _, sel := range step.Selections {
			if sel.From != "" {
				parts := strings.SplitN(sel.From, ".", 2)
				if len(parts) == 2 {
					addEdge(parts[0], stepID, parts[1])
				}
			}
		}
	}

	// Sort edges deterministically.
	type sortableEdge struct {
		from, to string
		fields   []string
	}
	var sorted []sortableEdge
	for key, fields := range edges {
		fieldList := make([]string, 0, len(fields))
		for f := range fields {
			fieldList = append(fieldList, f)
		}
		sort.Strings(fieldList)
		sorted = append(sorted, sortableEdge{key.from, key.to, fieldList})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].from != sorted[j].from {
			return sorted[i].from < sorted[j].from
		}
		return sorted[i].to < sorted[j].to
	})

	lines := make([]string, len(sorted))
	for i, e := range sorted {
		lines[i] = fmt.Sprintf("%s → %s (%s)", e.from, e.to, strings.Join(e.fields, ", "))
	}
	return lines
}

// parseChoicesParam parses a "slot=option,slot2=option2" string into a map.
func parseChoicesParam(s string) map[string]string {
	choices := make(map[string]string)
	if s == "" {
		return choices
	}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			choices[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return choices
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
