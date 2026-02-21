package main

import (
	"fmt"
	"io"
	"os"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/plan"
	"github.com/spf13/cobra"
)

// planCmd is the parent Cobra command for plan subcommands.
var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Plan inspection commands",
}

// planListCmd is the Cobra command for listing saved plans.
var planListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved plans from the plans directory",
	Long:  "List saved plans from the configured plans directory, showing name, goal, and step count.",
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
		fmt.Fprintln(out, "No plans found.")
		return nil
	}

	fmt.Fprintf(out, "Found %d plan(s):\n\n", len(entries))
	for _, entry := range entries {
		p, err := plan.ParseFile(entry.FullPath)
		if err != nil {
			fmt.Fprintf(out, "  %-40s (parse error)\n", entry.Name)
			continue
		}
		goal := p.Intent.Goal
		if len(goal) > 60 {
			goal = goal[:57] + "..."
		}
		fmt.Fprintf(out, "  %-40s %d steps  %s\n", entry.Name, len(p.Execution.Steps), goal)
	}

	return nil
}
