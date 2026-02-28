package main

import (
	"fmt"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/config"
	"github.com/spf13/cobra"
)

// runRebuildSummariesCmd rebuilds all summary.json files from full archives.
var runRebuildSummariesCmd = &cobra.Command{
	Use:   "rebuild-summaries",
	Short: "Rebuild all summary.json files from full archives",
	Long: `Rebuild summary.json files for all run archives in the output directory.

This forces re-computation of cached summary data (step counts, issues, etc.)
from the full archive files. Use this after upgrading AAT to pick up new
summary fields without re-running plans.

The next web UI listing will use the rebuilt summaries immediately.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		changed := func(name string) bool { return cmd.Flags().Changed(name) }
		getString := func(name string) string { v, _ := cmd.Flags().GetString(name); return v }

		overrides := buildProjectOverrides(changed, getString)
		resolved, err := config.ResolveProjectPaths(overrides)
		if err != nil {
			return err
		}

		outputDir := resolveOutputDir(cmd.Flags().Changed("output"), getString("output"), resolved.ArchiveDir)

		fmt.Printf("aat: rebuilding summaries in %s...\n", outputDir)

		result, err := archive.RebuildSummaries(outputDir, func(format string, a ...any) {
			fmt.Printf(format, a...)
		})
		if err != nil {
			return fmt.Errorf("rebuild summaries: %w", err)
		}

		fmt.Printf("aat: rebuilt %d summaries", result.Rebuilt)
		if result.Errors > 0 {
			fmt.Printf(", %d errors", result.Errors)
		}
		fmt.Println()

		return nil
	},
}

func init() {
	runCmd.AddCommand(runRebuildSummariesCmd)
}
