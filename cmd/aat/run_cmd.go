package main

import (
	"github.com/spf13/cobra"
)

// runCmd is the parent Cobra command for plan execution.
// It hosts shared PersistentFlags inherited by subcommands (plan, batch).
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute test plans",
	Long:  "Execute API test plans against a configured environment. Use 'run plan' for a single plan or 'run batch' for multiple plans.",
}

func init() {
	// Shared flags inherited by all run subcommands
	runCmd.PersistentFlags().String("manifest", "", "path to aat-project.yaml or project directory")
	runCmd.PersistentFlags().String("env", "", "path to environment YAML file")
	runCmd.PersistentFlags().String("graph", "", "path to graph YAML file")
	runCmd.PersistentFlags().String("templates", "", "path to templates directory")
	runCmd.PersistentFlags().String("output", "_output/runs", "directory for archive output")
	runCmd.PersistentFlags().String("domain", "", "path to domain knowledge YAML file")
	runCmd.PersistentFlags().StringSlice("override", nil, "node=url override (repeatable, e.g. searchFlights=http://localhost:8080)")
	runCmd.PersistentFlags().String("env-overlay", "", "path to environment overlay YAML file")
	runCmd.PersistentFlags().Int("retries", 0, "max plan-level retries on failure (0 = no retries)")
	runCmd.PersistentFlags().StringSlice("layer", nil, "data layer to apply (repeatable, e.g. --layer european --layer amex)")

	runCmd.AddCommand(runPlanCmd)
}
