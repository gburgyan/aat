package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/plan"
	"github.com/spf13/cobra"
)

// planCmd is the parent Cobra command for plan subcommands.
var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Plan inspection commands",
	RunE:  groupRunE,
}

// planListCmd is the Cobra command for listing saved plans.
var planListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved plans from the plans directory",
	Long:  "List saved plans from the configured plans directory, showing name, goal, and step count.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		overrides := config.ProjectPaths{}
		if cmd.Flags().Changed("manifest") {
			overrides.ExplicitManifest, _ = cmd.Flags().GetString("manifest")
		}

		resolved, err := config.ResolveProjectPaths(overrides)
		if err != nil {
			return err
		}

		return planListCommand(resolved.PlanDirs, os.Stdout)
	},
}

func init() {
	planCmd.AddCommand(planListCmd)

	planListCmd.Flags().String("manifest", "", "path to aat-project.yaml or project directory")
}

// planListCommand lists all saved plans from the configured plan directories.
func planListCommand(planDirs []string, out io.Writer) error {
	if len(planDirs) == 0 {
		return fmt.Errorf("plans directory not configured — set the `plans` field in aat-project.yaml")
	}

	entries, err := config.ListPlans(planDirs)
	if err != nil {
		return fmt.Errorf("listing plans: %w", err)
	}

	if len(entries) == 0 {
		_, _ = fmt.Fprintln(out, "No plans found.")
		return nil
	}

	_, _ = fmt.Fprintf(out, "Found %d plan(s):\n\n", len(entries))
	for _, entry := range entries {
		parsed, err := plan.ParseAnyFile(entry.FullPath)
		if err != nil {
			_, _ = fmt.Fprintf(out, "  %-40s (parse error)\n", entry.Name)
			continue
		}
		switch p := parsed.(type) {
		case *plan.Recipe:
			_, _ = fmt.Fprintf(out, "  %-40s recipe    %s\n", entry.Name, recipeSummary(p))
		case *plan.Plan:
			goal := p.Intent.Goal
			if runes := []rune(goal); len(runes) > 60 {
				goal = string(runes[:57]) + "..."
			}
			_, _ = fmt.Fprintf(out, "  %-40s %d steps  %s\n", entry.Name, len(p.Execution.Steps), goal)
		}
	}

	return nil
}

// recipeSummary renders a recipe's selection on one line, for example
// "Checkout (customer=Registered, payment=PayPal; +Apply Coupon; layers: shipping-express)".
func recipeSummary(r *plan.Recipe) string {
	var parts []string
	if len(r.Selection.Choices) > 0 {
		slots := make([]string, 0, len(r.Selection.Choices))
		for slot := range r.Selection.Choices {
			slots = append(slots, slot)
		}
		sort.Strings(slots)
		choices := make([]string, 0, len(slots))
		for _, slot := range slots {
			choices = append(choices, slot+"="+r.Selection.Choices[slot])
		}
		parts = append(parts, strings.Join(choices, ", "))
	}
	if len(r.Selection.Addons) > 0 {
		parts = append(parts, "+"+strings.Join(r.Selection.Addons, ", +"))
	}
	if len(r.Selection.Layers) > 0 {
		parts = append(parts, "layers: "+strings.Join(r.Selection.Layers, ", "))
	}
	if len(parts) == 0 {
		return r.Selection.Workflow
	}
	return r.Selection.Workflow + " (" + strings.Join(parts, "; ") + ")"
}
