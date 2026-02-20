package server

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gburgyan/aat/archive"
)

// ArchiveService provides read access to run archives for the web API.
type ArchiveService struct {
	archiveDir string
}

// NewArchiveService creates an ArchiveService that reads from the given directory.
func NewArchiveService(archiveDir string) *ArchiveService {
	return &ArchiveService{archiveDir: archiveDir}
}

// ListRuns scans the archive directory for run directories, reads each archive,
// and returns summaries sorted newest-first. limit=0 means no limit.
// Unreadable archives are skipped silently.
func (s *ArchiveService) ListRuns(limit int) ([]RunListEntry, error) {
	if s.archiveDir == "" {
		return nil, fmt.Errorf("archive directory not configured")
	}

	entries, err := os.ReadDir(s.archiveDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading archive directory: %w", err)
	}

	// Filter directories with "run-" prefix
	var runDirs []os.DirEntry
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "run-") {
			runDirs = append(runDirs, e)
		}
	}

	// Sort descending by name (timestamp-based = newest first)
	sort.Slice(runDirs, func(i, j int) bool {
		return runDirs[i].Name() > runDirs[j].Name()
	})

	// Apply limit
	if limit > 0 && len(runDirs) > limit {
		runDirs = runDirs[:limit]
	}

	var results []RunListEntry
	for _, dir := range runDirs {
		archivePath := filepath.Join(s.archiveDir, dir.Name(), "archive.json")
		a, err := archive.Read(archivePath)
		if err != nil {
			continue
		}
		results = append(results, toRunListEntry(a))
	}

	return results, nil
}

// LatestRunID returns the RunID of the most recent archive, or "" if none exist.
func (s *ArchiveService) LatestRunID() (string, error) {
	runs, err := s.ListRuns(1)
	if err != nil {
		return "", err
	}
	if len(runs) == 0 {
		return "", nil
	}
	return runs[0].RunID, nil
}

// toRunListEntry converts an archive to a RunListEntry view model.
func toRunListEntry(a *archive.Archive) RunListEntry {
	passed := 0
	failed := 0
	for _, s := range a.Steps {
		if stepPassed(s) {
			passed++
		} else {
			failed++
		}
	}

	return RunListEntry{
		RunID:       a.Metadata.RunID,
		Timestamp:   a.Metadata.Timestamp,
		Outcome:     a.Result.Outcome,
		StepCount:   len(a.Steps),
		PassedCount: passed,
		FailedCount: failed,
		DurationMs:  totalDuration(a),
		PlanName:    extractPlanName(a),
	}
}

// totalDuration sums step durations across all steps and cleanup.
func totalDuration(a *archive.Archive) int64 {
	var total int64
	for _, s := range a.Steps {
		total += s.DurationMs
	}
	for _, s := range a.Cleanup {
		total += s.DurationMs
	}
	return total
}

// extractPlanName returns a human-readable name for the plan, preferring
// Prompt > Description > Goal.
func extractPlanName(a *archive.Archive) string {
	if a.Metadata.Plan == nil {
		return ""
	}
	if p := a.Metadata.Plan.Metadata.Prompt; p != "" {
		return p
	}
	if d := a.Metadata.Plan.Intent.Description; d != "" {
		return d
	}
	return a.Metadata.Plan.Intent.Goal
}

// stepPassed returns true when a step has no errors or failures.
func stepPassed(s archive.StepRecord) bool {
	if s.Error != "" {
		return false
	}
	if s.Validation != nil && !s.Validation.Passed {
		return false
	}
	if s.ExpectFailure != nil && !s.ExpectFailure.Passed {
		return false
	}
	if s.ResponseBodyError != nil {
		return false
	}
	return true
}
