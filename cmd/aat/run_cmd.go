package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/fuzz"
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
	flags.StringSlice("fuzz", nil, "fuzz these steps, by step ID or node (comma-separated or repeatable): each generated case runs as a sibling step")
	flags.StringSlice("fuzz-mode", nil, "fuzz case modes to run: positive, negative, edge (default all)")
	flags.StringSlice("fuzz-input", nil, "fuzz only these inputs; a wired input is fuzzed only when named here")
	flags.Int("fuzz-cases", 0, "at most this many cases per fuzzed step, picked by the run's seed (0 = all)")
	flags.StringSlice("fuzz-case", nil, "run only the fuzz cases with these IDs, such as quantity.above-max")
	flags.String("fuzz-scope", "isolated", "isolated: each case gets its own copy of the steps the target depends on; shared: cases reuse the target's, which is faster but lets a case change what later steps see")
	flags.StringSlice("fuzz-fail", nil, "findings that fail the run (default server-error,no-response,schema-violation; also accepted-invalid, rejected-valid, not-sent)")
}

// fuzzConfigFromFlags reads the --fuzz flags, or returns nil when --fuzz is
// not set. The other --fuzz-* flags need --fuzz.
func fuzzConfigFromFlags(cmd *cobra.Command) (*engine.FuzzConfig, error) {
	flags := cmd.Flags()
	targets, _ := flags.GetStringSlice("fuzz")
	if len(targets) == 0 {
		for _, name := range []string{"fuzz-mode", "fuzz-input", "fuzz-cases", "fuzz-case", "fuzz-scope", "fuzz-fail"} {
			if flags.Changed(name) {
				return nil, fmt.Errorf("--%s needs --fuzz", name)
			}
		}
		return nil, nil
	}
	cfg := &engine.FuzzConfig{Targets: targets}
	cfg.Modes, _ = flags.GetStringSlice("fuzz-mode")
	for _, m := range cfg.Modes {
		if !slices.Contains(fuzz.AllModes, m) {
			return nil, fmt.Errorf("--fuzz-mode %s: use %s", m, strings.Join(fuzz.AllModes, ", "))
		}
	}
	cfg.Inputs, _ = flags.GetStringSlice("fuzz-input")
	cfg.Max, _ = flags.GetInt("fuzz-cases")
	if cfg.Max < 0 {
		return nil, fmt.Errorf("--fuzz-cases must not be negative")
	}
	cfg.Cases, _ = flags.GetStringSlice("fuzz-case")
	switch scope, _ := flags.GetString("fuzz-scope"); scope {
	case "isolated":
	case "shared":
		cfg.Shared = true
	default:
		return nil, fmt.Errorf("--fuzz-scope %s: use isolated or shared", scope)
	}
	if flags.Changed("fuzz-fail") {
		fail, _ := flags.GetStringSlice("fuzz-fail")
		parsed, err := engine.ParseFindings(fail)
		if err != nil {
			return nil, fmt.Errorf("--fuzz-fail: %w", err)
		}
		cfg.Fail = parsed
	}
	return cfg, nil
}
