package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"gopkg.in/yaml.v3"

	"github.com/gburgyan/aat/fuzz"
	"github.com/gburgyan/aat/plan"
)

// registerFuzzTools adds the fuzz case generator.
func (s *Server) registerFuzzTools() {
	s.mcp.AddTool(
		mcp.NewTool("generate_fuzz_cases",
			mcp.WithDescription("Generate the fuzz cases `aat run plan --fuzz` would send to one step of a saved plan, without running anything. "+
				"Returns them as a `fuzz:` block of pinned cases: paste the ones worth keeping into the step, and every run sends them, "+
				"judged as --fuzz judges them (positive cases should be accepted, negative ones refused with a 4xx, none should fail with a 5xx)."),
			mcp.WithString("plan", mcp.Description("Plan filename (e.g. 'smoke' or 'smoke.yaml')"), mcp.Required()),
			mcp.WithString("step", mcp.Description("Step ID, or a node name for the first step of that node"), mcp.Required()),
			mcp.WithString("mode", mcp.Description("Comma-separated modes: positive, negative, edge (default all)")),
			mcp.WithString("inputs", mcp.Description("Comma-separated inputs to fuzz (default: every input not wired from another step)")),
			mcp.WithNumber("count", mcp.Description("At most this many cases, picked by seed (default all)")),
			mcp.WithNumber("seed", mcp.Description("Seed that picks the cases when count caps them")),
		),
		s.handleGenerateFuzzCases,
	)
}

// handleGenerateFuzzCases generates a step's fuzz cases and returns them as a
// fuzz: block of pinned cases.
func (s *Server) handleGenerateFuzzCases(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("plan")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: plan"), nil
	}
	if err := checkPlanName(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	stepRef, err := req.RequireString("step")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: step"), nil
	}

	p, layeredDefaults, err := s.loadNamedPlan(name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	inst := plan.InstantiateWithLayers(p, s.ctx.Graph, layeredDefaults)
	var step *plan.Step
	for i, st := range inst.Execution.Steps {
		if st.StepID() == stepRef || (step == nil && st.Node == stepRef) {
			step = &inst.Execution.Steps[i]
			if st.StepID() == stepRef {
				break
			}
		}
	}
	if step == nil {
		return mcp.NewToolResultError(fmt.Sprintf("plan %s has no step %q (by step ID or node)", name, stepRef)), nil
	}
	node := s.ctx.Graph.Nodes[step.Node]
	if node == nil {
		return mcp.NewToolResultError(fmt.Sprintf("node %q not found in graph", step.Node)), nil
	}
	tmpl, _ := s.ctx.Registry.GetTemplate(node.Adapter)

	opts := fuzz.Options{
		Modes:  splitList(req.GetString("mode", "")),
		Inputs: splitList(req.GetString("inputs", "")),
		Max:    req.GetInt("count", 0),
		Seed:   uint64(max(req.GetInt("seed", 0), 0)),
		Now:    time.Now(),
	}
	cases, err := fuzz.Generate(fuzz.Target{Step: *step, Node: node, KB: s.ctx.KB, Template: tmpl}, opts)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(cases) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No fuzz cases for step %s: every input is wired from another step, and the template writes no fields of its own. Name wired inputs with `inputs` to fuzz them.", step.StepID())), nil
	}

	pinned := make([]plan.PinnedFuzzCase, len(cases))
	for i, c := range cases {
		pinned[i] = c.Pin()
	}
	block, err := yaml.Marshal(map[string]any{"fuzz": plan.FuzzSettings{Pinned: pinned}})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("formatting the cases: %v", err)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## %d fuzz cases for step %s (%s)\n\n", len(cases), step.StepID(), step.Node)
	b.WriteString("| Case | Mode | Sends |\n|------|------|-------|\n")
	for _, c := range cases {
		fmt.Fprintf(&b, "| `%s` | %s | `%s` |\n", c.ID, c.Mode, strings.ReplaceAll(c.Describe(), "|", "\\|"))
	}
	b.WriteString("\nAs a fuzz: block for the step (keep the cases worth keeping):\n\n```yaml\n")
	b.Write(block)
	b.WriteString("```\n")
	return mcp.NewToolResultText(b.String()), nil
}

// splitList reads a comma-separated parameter.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
