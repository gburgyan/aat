package main

import (
	"fmt"
	"time"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/config"
	"github.com/spf13/cobra"
)

// runCleanCmd deletes old, unsaved run archives.
var runCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Delete old, unsaved run archives",
	Long: `Delete auto-generated run and batch archives older than a specified age.

Named/saved runs (those renamed in the web UI or prefixed with !) are never
deleted. Use --dry-run to preview what would be removed.`,
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
		days, _ := cmd.Flags().GetInt("days")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		opts := archive.CleanOptions{
			MaxAge: time.Duration(days) * 24 * time.Hour,
			DryRun: dryRun,
		}

		if dryRun {
			fmt.Printf("aat: dry run — previewing cleanup in %s (older than %d days)...\n", outputDir, days)
		} else {
			fmt.Printf("aat: cleaning runs in %s older than %d days...\n", outputDir, days)
		}

		result, err := archive.CleanOldRuns(outputDir, opts, func(format string, a ...any) {
			fmt.Printf(format, a...)
		})
		if err != nil {
			return fmt.Errorf("clean: %w", err)
		}

		verb := "deleted"
		if dryRun {
			verb = "would delete"
		}
		fmt.Printf("aat: %s %d run(s), skipped %d saved", verb, result.Deleted, result.Skipped)
		if result.Errors > 0 {
			fmt.Printf(", %d errors", result.Errors)
		}
		fmt.Println()

		return nil
	},
}

func init() {
	runCleanCmd.Flags().Int("days", 7, "delete runs older than this many days")
	runCleanCmd.Flags().Bool("dry-run", false, "preview what would be deleted without removing anything")
	runCmd.AddCommand(runCleanCmd)
}
