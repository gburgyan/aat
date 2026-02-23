package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/config"
	"github.com/spf13/cobra"
)

var importCmd = &cobra.Command{
	Use:   "import <file.aar|file.aab>",
	Short: "Import an archive file",
	Long:  "Import a previously exported run (.aar) or batch (.aab) archive into the archive directory.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		filePath := args[0]

		// Read the file.
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", filePath, err)
		}

		// Determine name.
		nameFlag, _ := cmd.Flags().GetString("name")
		var name string
		if nameFlag != "" {
			name = nameFlag
		} else {
			name, err = archive.SanitizeArchiveName(filepath.Base(filePath))
			if err != nil {
				return fmt.Errorf("deriving name from filename: %w", err)
			}
		}

		// Determine output directory.
		outputDir := "_output/runs"
		if cmd.Flags().Changed("output") {
			outputDir, _ = cmd.Flags().GetString("output")
		} else {
			overrides := config.ProjectPaths{}
			if cmd.Flags().Changed("manifest") {
				overrides.ExplicitManifest, _ = cmd.Flags().GetString("manifest")
			}
			resolved, resolveErr := config.ResolveProjectPaths(overrides)
			if resolveErr == nil && resolved.ArchiveDir != "" {
				outputDir = resolved.ArchiveDir
			}
		}

		// Import.
		reader := bytes.NewReader(data)
		dirName, archiveType, err := archive.ImportArchive(reader, int64(len(data)), name, outputDir)
		if err != nil {
			return err
		}

		destPath := filepath.Join(outputDir, dirName)
		fmt.Printf("Imported %s %q → %s\n", archiveType, dirName, destPath)
		return nil
	},
}

func init() {
	importCmd.Flags().String("output", "_output/runs", "directory for archive output")
	importCmd.Flags().String("manifest", "", "path to aat-project.yaml or project directory")
	importCmd.Flags().String("name", "", "name for the imported archive (default: derived from filename)")
}
