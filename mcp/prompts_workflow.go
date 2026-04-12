package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/graph"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerWorkflowPrompts adds multi-step workflow prompts to the MCP server.
func (s *Server) registerWorkflowPrompts() {
	s.mcp.AddPrompt(
		mcp.NewPrompt("integration_guide",
			mcp.WithPromptDescription("Comprehensive guide for integrating with an API workflow, including chain trace, templates, OAS details, and domain context"),
			mcp.WithArgument("goal",
				mcp.RequiredArgument(),
				mcp.ArgumentDescription("Goal node name to build the integration guide for"),
			),
		),
		s.handleIntegrationGuide,
	)

	s.mcp.AddPrompt(
		mcp.NewPrompt("test_workflow",
			mcp.WithPromptDescription("Guide the full test lifecycle: understand goal, generate plan, validate, execute, and inspect results"),
			mcp.WithArgument("description",
				mcp.RequiredArgument(),
				mcp.ArgumentDescription("Description of what to test"),
			),
		),
		s.handleTestWorkflow,
	)

	s.mcp.AddPrompt(
		mcp.NewPrompt("debug_failing_test",
			mcp.WithPromptDescription("Load a failed run archive and diagnose root causes with comprehensive failure context"),
			mcp.WithArgument("run_id",
				mcp.RequiredArgument(),
				mcp.ArgumentDescription("Run ID of the failed archive to debug"),
			),
		),
		s.handleDebugFailingTest,
	)
}

// handleIntegrationGuide traces a workflow to a goal and assembles comprehensive
// context for building an integration guide.
func (s *Server) handleIntegrationGuide(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	goal := req.Params.Arguments["goal"]
	if goal == "" {
		return nil, fmt.Errorf("missing required argument: goal")
	}

	g := s.ctx.Graph
	if g.Nodes[goal] == nil {
		return nil, fmt.Errorf("unknown goal node %q", goal)
	}

	cr, err := graph.BackwardChain(g, graph.ChainOptions{Goals: []string{goal}})
	if err != nil {
		return nil, fmt.Errorf("backward chain failed: %w", err)
	}

	var ctxBuf strings.Builder
	ctxBuf.WriteString(formatChainTrace(cr, g))

	// Per-node detail with docs, template, and OAS
	for _, name := range cr.Nodes {
		node := g.Nodes[name]
		if node == nil {
			continue
		}
		ctxBuf.WriteString("---\n\n")
		ctxBuf.WriteString(formatNodeDetail(node, g))

		// Node docs
		if doc, ok := s.ctx.NodeDocs[name]; ok && doc != "" {
			ctxBuf.WriteString("\n### Documentation\n\n")
			ctxBuf.WriteString(doc)
			ctxBuf.WriteString("\n")
		}

		// Template shape
		if node.Adapter != "" {
			if tmpl, ok := s.ctx.Registry.GetTemplate(node.Adapter); ok {
				ctxBuf.WriteString("\n")
				ctxBuf.WriteString(formatTemplate(tmpl))
			}
		}

		// OAS operation
		if node.OAS != nil {
			op, method, path, doc, errMsg := s.resolveNodeOperation(name)
			if errMsg == "" && op != nil {
				ctxBuf.WriteString("\n")
				ctxBuf.WriteString(formatOperationDetail(method, path, op, doc))
			}
		}

		ctxBuf.WriteString("\n")
	}

	// Domain knowledge
	if s.ctx.KB != nil {
		domainText := s.ctx.KB.FormatForPrompt()
		if domainText != "" {
			ctxBuf.WriteString("---\n\n")
			ctxBuf.WriteString(domainText)
		}
	}

	instruction := fmt.Sprintf(
		"Create a comprehensive integration guide for achieving %s. Cover:\n"+
			"1. Prerequisites and authentication requirements\n"+
			"2. Step-by-step walkthrough of each API call in the workflow\n"+
			"3. Request construction with example payloads for each step\n"+
			"4. Data extraction and how to pass data between steps\n"+
			"5. Error handling and common failure modes\n"+
			"6. Complete working code example tying all steps together",
		goal,
	)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Integration guide for %s", goal),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: ctxBuf.String(),
				},
			},
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: instruction,
				},
			},
		},
	}, nil
}

// handleTestWorkflow assembles context for guiding the full test lifecycle.
func (s *Server) handleTestWorkflow(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	description := req.Params.Arguments["description"]
	if description == "" {
		return nil, fmt.Errorf("missing required argument: description")
	}

	g := s.ctx.Graph

	var ctxBuf strings.Builder

	// Graph overview
	ctxBuf.WriteString("# API Graph Overview\n\n")
	for _, name := range sortedNodeNames(g) {
		node := g.Nodes[name]
		if node == nil {
			continue
		}
		ctxBuf.WriteString("- ")
		ctxBuf.WriteString(formatNodeSummary(node))
		ctxBuf.WriteString("\n")
	}
	ctxBuf.WriteString("\n")

	// Domain knowledge
	if s.ctx.KB != nil {
		domainText := s.ctx.KB.FormatForPrompt()
		if domainText != "" {
			ctxBuf.WriteString("---\n\n")
			ctxBuf.WriteString(domainText)
			ctxBuf.WriteString("\n")
		}
	}

	// Available capabilities
	ctxBuf.WriteString("---\n\n")
	ctxBuf.WriteString("# Available Capabilities\n\n")

	ctxBuf.WriteString("**Plan tools:** generate_plan, validate_plan")
	if s.ctx.WorkflowsDir != "" {
		ctxBuf.WriteString(", save_plan, list_plans, load_plan")
	}
	ctxBuf.WriteString("\n")

	if s.ctx.Environment != nil && s.ctx.WorkflowsDir != "" && s.ctx.ArchiveDir != "" {
		ctxBuf.WriteString("**Execution:** execute_plan\n")
	}

	if s.ctx.ArchiveDir != "" {
		ctxBuf.WriteString("**Archive inspection:** inspect_archive, analyze_failure, diff_archives, list_archives\n")
	}
	ctxBuf.WriteString("\n")

	instruction := fmt.Sprintf(
		"Help me create and run an API test for: %s\n\n"+
			"Follow this workflow:\n"+
			"1. Understand what the user wants to test by analyzing the graph\n"+
			"2. Use `generate_plan` to create a test plan from the description\n"+
			"3. Review the generated plan with the user, offering to adjust if needed\n"+
			"4. Use `save_plan` to persist the plan\n"+
			"5. Use `execute_plan` to run the test\n"+
			"6. Use `inspect_archive` or `analyze_failure` to review results\n"+
			"7. If the test fails, diagnose the issue and suggest fixes\n\n"+
			"Start by analyzing the graph to identify which nodes and workflows are relevant.",
		description,
	)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Test workflow for: %s", description),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: ctxBuf.String(),
				},
			},
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: instruction,
				},
			},
		},
	}, nil
}

// handleDebugFailingTest loads a failed archive and assembles failure context
// for diagnosis.
func (s *Server) handleDebugFailingTest(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	runID := req.Params.Arguments["run_id"]
	if runID == "" {
		return nil, fmt.Errorf("missing required argument: run_id")
	}

	if s.ctx.ArchiveDir == "" {
		return nil, fmt.Errorf("archive directory not configured")
	}

	a, err := loadArchive(s.ctx.ArchiveDir, runID)
	if err != nil {
		return nil, err
	}

	// If the run passed, return a helpful message
	if a.Result.Outcome == "passed" {
		return &mcp.GetPromptResult{
			Description: fmt.Sprintf("Debug run %s", runID),
			Messages: []mcp.PromptMessage{
				{
					Role: mcp.RoleUser,
					Content: mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Run %s passed. Use `inspect_archive` to review the details instead.", runID),
					},
				},
			},
		}, nil
	}

	g := s.ctx.Graph
	var ctxBuf strings.Builder

	// Full failure analysis
	ctxBuf.WriteString(formatFailureAnalysis(a))
	ctxBuf.WriteString("\n")

	// Context from successful steps preceding the first failure
	failedSteps := findFailedSteps(a.Steps)
	if len(failedSteps) > 0 {
		firstFailedNode := failedSteps[0].Node
		ctxBuf.WriteString("## Successful Steps Before Failure\n\n")
		hasSuccessful := false
		for _, step := range a.Steps {
			if step.Node == firstFailedNode {
				break
			}
			status := 0
			if step.Response != nil {
				status = step.Response.Status
			}
			fmt.Fprintf(&ctxBuf, "- **%s**: status %d", step.Node, status)
			if len(step.Outputs) > 0 {
				keys := make([]string, 0, len(step.Outputs))
				for k := range step.Outputs {
					keys = append(keys, k)
				}
				fmt.Fprintf(&ctxBuf, " (outputs: %s)", strings.Join(keys, ", "))
			}
			ctxBuf.WriteString("\n")
			hasSuccessful = true
		}
		if !hasSuccessful {
			ctxBuf.WriteString("(none — the first step failed)\n")
		}
		ctxBuf.WriteString("\n")
	}

	// Full detail of failed steps
	if len(failedSteps) > 0 {
		ctxBuf.WriteString("## Failed Step Details\n\n")
		for i, step := range failedSteps {
			ctxBuf.WriteString(formatStepRecord(&step, i+1, len(failedSteps)))
			ctxBuf.WriteString("\n")
		}
	}

	// Graph context for failed nodes
	failedNodeNames := uniqueFailedNodeNames(failedSteps)
	if len(failedNodeNames) > 0 {
		ctxBuf.WriteString("## Graph Context for Failed Nodes\n\n")
		for _, name := range failedNodeNames {
			node := g.Nodes[name]
			if node == nil {
				continue
			}
			ctxBuf.WriteString(formatNodeDetail(node, g))
			ctxBuf.WriteString("\n")
		}
	}

	// Plan goal for intent context
	if a.Metadata.Plan != nil && a.Metadata.Plan.Intent.Goal != "" {
		ctxBuf.WriteString("## Test Goal\n\n")
		ctxBuf.WriteString(a.Metadata.Plan.Intent.Goal)
		ctxBuf.WriteString("\n\n")
	}

	instruction := "Diagnose why this test run failed. For each failure:\n" +
		"1. Identify the root cause (bad input data, auth issue, API change, constraint mismatch, etc.)\n" +
		"2. Trace the data flow to find where the wrong value originated\n" +
		"3. Suggest specific fixes to the test plan or configuration\n" +
		"4. If the issue is environmental (auth, API health), suggest how to verify and resolve\n\n" +
		"If multiple steps failed, determine if they share a common root cause (e.g., an early step failure cascading)."

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Debug failing test: %s", runID),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: ctxBuf.String(),
				},
			},
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: instruction,
				},
			},
		},
	}, nil
}

// uniqueFailedNodeNames returns deduplicated, order-preserving node names from failed steps.
func uniqueFailedNodeNames(steps []archive.StepRecord) []string {
	seen := make(map[string]bool)
	var names []string
	for _, s := range steps {
		if !seen[s.Node] {
			seen[s.Node] = true
			names = append(names, s.Node)
		}
	}
	return names
}
