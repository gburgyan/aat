package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RebuildResult captures the outcome of a summary rebuild operation.
type RebuildResult struct {
	Rebuilt int // number of summary files rebuilt
	Skipped int // directories without archive.json
	Errors  int // directories with read/write errors
}

// RebuildSummaries walks the archive directory and rebuilds summary.json for
// every run directory that contains an archive.json. This includes both
// top-level run directories and run directories nested within batch directories.
func RebuildSummaries(archiveDir string, logf func(string, ...any)) (*RebuildResult, error) {
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return nil, fmt.Errorf("reading archive directory: %w", err)
	}

	result := &RebuildResult{}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirPath := filepath.Join(archiveDir, e.Name())

		if strings.HasPrefix(e.Name(), "batch-") || isNamedBatch(dirPath) {
			// Batch directory — rebuild summaries for nested run directories
			rebuildBatchRunSummaries(dirPath, result, logf)
			continue
		}

		// Top-level run directory
		rebuildOneSummary(dirPath, result, logf)
	}

	return result, nil
}

// isNamedBatch checks if a directory is a batch directory by looking for batch.json.
func isNamedBatch(dirPath string) bool {
	_, err := os.Stat(filepath.Join(dirPath, "batch.json"))
	return err == nil
}

// rebuildBatchRunSummaries walks subdirectories of a batch directory.
func rebuildBatchRunSummaries(batchDir string, result *RebuildResult, logf func(string, ...any)) {
	entries, err := os.ReadDir(batchDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rebuildOneSummary(filepath.Join(batchDir, e.Name()), result, logf)
	}
}

// rebuildOneSummary rebuilds summary.json for a single run directory.
func rebuildOneSummary(runDir string, result *RebuildResult, logf func(string, ...any)) {
	archivePath := filepath.Join(runDir, "archive.json")
	if _, err := os.Stat(archivePath); err != nil {
		result.Skipped++
		return
	}

	a, err := Read(archivePath)
	if err != nil {
		logf("  warning: %s: %s\n", runDir, err)
		result.Errors++
		return
	}

	summary := BuildRunSummary(a)
	summaryPath := filepath.Join(runDir, "summary.json")
	if err := WriteSummary(summary, summaryPath); err != nil {
		logf("  warning: %s: %s\n", summaryPath, err)
		result.Errors++
		return
	}

	result.Rebuilt++
}
