package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/intent"
	"github.com/gburgyan/aat/plan"
)

// planMain dispatches to plan subcommands.
func planMain(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aat plan <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands: validate")
		return 1
	}
	switch args[0] {
	case "validate":
		return planValidateMain(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown plan subcommand: %s\n", args[0])
		return 1
	}
}

// planValidateArgs holds parsed CLI flags for plan validate.
type planValidateArgs struct {
	GraphPath string
	PlanPath  string
	Unfed     bool
}

// planValidateMain parses flags and runs plan validation.
func planValidateMain(args []string) int {
	fs := flag.NewFlagSet("plan validate", flag.ContinueOnError)
	pa := &planValidateArgs{}
	fs.StringVar(&pa.GraphPath, "graph", "", "path to graph YAML file (required)")
	fs.StringVar(&pa.PlanPath, "plan", "", "path to plan YAML file (single plan mode)")
	fs.BoolVar(&pa.Unfed, "unfed", false, "show unfed inputs for each plan/template")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	return planValidateCommand(pa)
}

// planValidateCommand runs plan validation against a graph.
// In single-plan mode (--plan provided), validates one plan file.
// In all-templates mode (--plan omitted), validates all workflow templates.
// Extracted for testability.
func planValidateCommand(args *planValidateArgs) int {
	if args.GraphPath == "" {
		fmt.Fprintln(os.Stderr, "aat plan validate: --graph is required")
		return 1
	}

	// Load graph.
	g, err := graph.ParseFile(args.GraphPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aat plan validate: loading graph: %s\n", err)
		return 1
	}

	// Single plan mode.
	if args.PlanPath != "" {
		return validateSinglePlan(args.PlanPath, g, args.Unfed)
	}

	// All workflow templates mode.
	return validateAllTemplates(args.GraphPath, g, args.Unfed)
}

// validateSinglePlan validates one plan file against the graph.
func validateSinglePlan(planPath string, g *graph.Graph, showUnfed bool) int {
	p, err := plan.ParseFile(planPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aat plan validate: %s\n", err)
		return 1
	}

	if err := plan.Validate(p, g); err != nil {
		fmt.Fprintf(os.Stderr, "aat plan validate: %s\n", err)
		return 1
	}

	fmt.Printf("Plan validation: OK (%d steps)\n", len(p.Execution.Steps))

	if showUnfed {
		printUnfed(p, g)
	}

	return 0
}

// validateAllTemplates validates all workflow templates referenced in the graph.
func validateAllTemplates(graphPath string, g *graph.Graph, showUnfed bool) int {
	graphDir := filepath.Dir(graphPath)

	// Collect workflows that have templates.
	var templated []graph.Workflow
	for _, wf := range g.Workflows {
		if wf.Template != "" {
			templated = append(templated, wf)
		}
	}

	if len(templated) == 0 {
		fmt.Println("No workflow templates found.")
		return 0
	}

	fmt.Printf("Validating %d workflow templates...\n\n", len(templated))

	hasError := false
	for _, wf := range templated {
		p, err := intent.LoadWorkflowTemplate(wf.Template, graphDir, g)
		if err != nil {
			fmt.Printf("  %-40s FAIL\n", wf.Name)
			fmt.Printf("    %s\n", err)
			hasError = true
			continue
		}

		if err := plan.Validate(p, g); err != nil {
			fmt.Printf("  %-40s FAIL\n", wf.Name)
			fmt.Printf("    %s\n", err)
			hasError = true
			continue
		}

		fmt.Printf("  %-40s OK (%d steps)\n", wf.Name, len(p.Execution.Steps))

		if showUnfed {
			printUnfed(p, g)
		}
	}

	fmt.Println()
	if hasError {
		fmt.Println("Workflow template validation: FAILED")
		return 1
	}

	fmt.Println("Workflow template validation: OK")
	return 0
}

// printUnfed prints unfed inputs for a plan if any exist.
func printUnfed(p *plan.Plan, g *graph.Graph) {
	unfed := intent.UnfedInputsFromTemplate(p, g)
	if len(unfed) == 0 {
		return
	}
	fmt.Println("    Unfed inputs:")
	for _, u := range unfed {
		fmt.Printf("      - %s\n", u)
	}
}
