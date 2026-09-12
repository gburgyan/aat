package main

import (
	"github.com/spf13/cobra"
)

// runCmd is the parent Cobra command for plan execution. Its persistent flags
// locate the project and its archives, which every run subcommand uses. The
// flags that configure an execution are registered on plan and batch only
// (addExecutionFlags), so run clean and run rebuild-summaries reject them
// instead of silently ignoring them.
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute test plans",
	Long:  "Execute API test plans against a configured environment. Use 'run plan' for a single plan or 'run batch' for multiple plans.",
	RunE:  groupRunE,
}

func init() {
	runCmd.PersistentFlags().String("manifest", "", "path to aat-project.yaml or project directory")
	runCmd.PersistentFlags().String("output", "_output/runs", "directory for archive output")

	addExecutionFlags(runPlanCmd)
	addExecutionFlags(runBatchCmd)

	runCmd.AddCommand(runPlanCmd)
}

// addExecutionFlags registers the flags shared by the run subcommands that
// execute plans.
func addExecutionFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.String("env-config", "", "path to environment YAML file")
	flags.String("env", "", "environment name (for multi-environment files)")
	flags.String("graph", "", "path to graph YAML file")
	flags.String("templates", "", "path to templates directory")
	flags.String("domain", "", "path to domain knowledge YAML file")
	flags.StringSlice("override", nil, "node=url override (repeatable, e.g. searchFlights=http://localhost:8080)")
	flags.String("overlay", "", "path to environment overlay YAML file")
	flags.StringArray("var", nil, "set a var of a multi-environment file, KEY=VALUE (repeatable; wins over the file's vars, e.g. --var apiHost=localhost:9000)")
	flags.Int("retries", 0, "max plan-level retries on failure (0 = no retries)")
	flags.StringSlice("layer", nil, "data layer to apply (repeatable, e.g. --layer european --layer amex)")
	flags.Bool("no-auto-overrides", false, "disable auto-discovery of .aat-overrides.yaml")
	flags.String("oas-validate", "", "OAS validation mode: auto (default), strict, off")
	flags.Bool("verbose-auth", false, "log auth request/response details to stderr for debugging")
	flags.Bool("no-mutations", false, "skip mutation-expanded sibling steps; run only the happy path (smoke-test mode)")
}
