package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/intent"
	"github.com/gburgyan/aat/plan"
	"github.com/spf13/cobra"
)

// planCmd is the parent Cobra command for plan subcommands.
var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Plan inspection and validation commands",
}

// planValidateCmd is the Cobra command for plan validation.
var planValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a plan against a graph",
	Long:  "Validate a single plan file or all workflow templates against a graph definition.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		// Build overrides from explicitly-set flags only
		overrides := config.ProjectPaths{}
		if cmd.Flags().Changed("graph") {
			overrides.GraphPath, _ = cmd.Flags().GetString("graph")
		}

		resolved, err := config.ResolveProjectPaths(overrides)
		if err != nil {
			return err
		}

		planPath, _ := cmd.Flags().GetString("plan")
		unfed, _ := cmd.Flags().GetBool("unfed")

		pa := &planValidateArgs{
			GraphPath: resolved.GraphPath,
			PlanPath:  planPath,
			Unfed:     unfed,
		}

		code := planValidateCommand(pa)
		if code != 0 {
			return &exitError{Code: code}
		}
		return nil
	},
}

func init() {
	planCmd.AddCommand(planValidateCmd)

	planValidateCmd.Flags().String("graph", "", "path to graph YAML file")
	planValidateCmd.Flags().String("plan", "", "path to plan YAML file (single plan mode)")
	planValidateCmd.Flags().Bool("unfed", false, "show unfed inputs for each plan/template")
}

// planValidateArgs holds parsed CLI flags for plan validate.
type planValidateArgs struct {
	GraphPath string
	PlanPath  string
	Unfed     bool
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

	if _, err := plan.InstantiateAndValidate(p, g); err != nil {
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

		// Addon templates are fragments with intentionally unfed required
		// inputs (filled by LLM at composition time). Only validate
		// standalone workflows structurally.
		if !wf.IsAddon() {
			if _, err := plan.InstantiateAndValidate(p, g); err != nil {
				fmt.Printf("  %-40s FAIL\n", wf.Name)
				fmt.Printf("    %s\n", err)
				hasError = true
				continue
			}
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
