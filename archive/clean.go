package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CleanOptions configures the cleanup behavior.
type CleanOptions struct {
	MaxAge time.Duration // delete runs older than this
	DryRun bool          // if true, report but don't delete
}

// CleanResult captures the outcome of a clean operation.
type CleanResult struct {
	Deleted     int      // number of directories removed (or would be in dry-run)
	DeletedDirs []string // names of removed directories
	Skipped     int      // named/saved directories skipped
	Errors      int      // directories with errors
}

// CleanOldRuns removes unnamed run and batch directories older than opts.MaxAge
// from archiveDir. Named/saved directories are never deleted.
func CleanOldRuns(archiveDir string, opts CleanOptions, logf func(string, ...any)) (*CleanResult, error) {
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return nil, fmt.Errorf("reading archive directory: %w", err)
	}

	now := time.Now()
	cutoff := now.Add(-opts.MaxAge)
	result := &CleanResult{}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()

		// Skip named/saved runs.
		if IsNamed(name) {
			result.Skipped++
			continue
		}

		// Parse timestamp from directory name.
		ts, err := ParseRunTimestamp(name)
		if err != nil {
			logf("  warning: %s: cannot parse timestamp: %s\n", name, err)
			result.Errors++
			continue
		}

		if ts.After(cutoff) {
			continue // too recent, keep it
		}

		dirPath := filepath.Join(archiveDir, name)
		if opts.DryRun {
			logf("  would delete: %s (age: %s)\n", name, formatAge(now, ts))
			result.Deleted++
			result.DeletedDirs = append(result.DeletedDirs, name)
			continue
		}

		if err := os.RemoveAll(dirPath); err != nil {
			logf("  error: %s: %s\n", name, err)
			result.Errors++
			continue
		}
		logf("  deleted: %s (age: %s)\n", name, formatAge(now, ts))
		result.Deleted++
		result.DeletedDirs = append(result.DeletedDirs, name)
	}

	return result, nil
}

// ParseRunTimestamp extracts the creation time from an auto-generated directory name.
// Expected format: (run|batch)-YYYYMMDD-HHMMSS-XXXXXXXX
func ParseRunTimestamp(name string) (time.Time, error) {
	var dateStr string
	switch {
	case len(name) >= 19 && name[:4] == "run-":
		dateStr = name[4:19] // "YYYYMMDD-HHMMSS"
	case len(name) >= 21 && name[:6] == "batch-":
		dateStr = name[6:21] // "YYYYMMDD-HHMMSS"
	default:
		return time.Time{}, fmt.Errorf("unrecognized format: %s", name)
	}
	return time.ParseInLocation("20060102-150405", dateStr, time.Local)
}

func formatAge(now, ts time.Time) string {
	d := now.Sub(ts)
	days := int(d.Hours() / 24)
	if days == 0 {
		return "< 1 day"
	}
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}
