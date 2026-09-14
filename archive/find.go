package archive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrRunNotFound reports that a run reference matches no run in an archive
// directory.
var ErrRunNotFound = errors.New("run not found")

// RunRef locates one run's archive in an archive directory.
type RunRef struct {
	// ID is the run's directory name, such as run-20260910-225958-d819f460.
	ID string
	// BatchID is the directory name of the batch the run belongs to, or empty
	// for a run of its own.
	BatchID string
	// Dir is the run's directory, and ArchivePath its archive.json.
	Dir         string
	ArchivePath string
	// Timestamp orders runs newest first; see ListRuns.
	Timestamp time.Time
}

// Ref returns the reference FindRun resolves to r: the run ID, preceded by its
// batch ID and a slash for a run inside a batch.
func (r RunRef) Ref() string {
	if r.BatchID == "" {
		return r.ID
	}
	return r.BatchID + "/" + r.ID
}

// ListRuns returns every run in archiveDir, newest first. A directory holding
// archive.json is a run. Any other directory is read as a batch whose
// subdirectories holding archive.json are its runs, so the runs of a batch that
// is still going, or that stopped before writing batch.json, are included.
// Saved names count like generated ones. A run's time is the timestamp in its
// summary.json, else the one in a generated directory name, else the
// modification time of its archive.json. ListRuns only reads, and a missing
// archiveDir holds no runs.
func ListRuns(archiveDir string) ([]RunRef, error) {
	entries, err := os.ReadDir(archiveDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading archive directory: %w", err)
	}
	var runs []RunRef
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(archiveDir, e.Name())
		if run, ok := runInDir(dir, e.Name(), ""); ok {
			runs = append(runs, run)
			continue
		}
		members, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, m := range members {
			if !m.IsDir() {
				continue
			}
			if run, ok := runInDir(filepath.Join(dir, m.Name()), m.Name(), e.Name()); ok {
				runs = append(runs, run)
			}
		}
	}
	sort.SliceStable(runs, func(i, j int) bool {
		if !runs[i].Timestamp.Equal(runs[j].Timestamp) {
			return runs[i].Timestamp.After(runs[j].Timestamp)
		}
		return runs[i].Ref() > runs[j].Ref()
	})
	return runs, nil
}

// FindRun resolves ref to a run in archiveDir. ref is one of:
//   - latest: the newest run ListRuns returns, runs inside batches included
//   - a batch ID and a run ID joined by a slash
//   - a run ID, looked up at the top level first and then inside every batch
//
// Every name must be a single directory name (see CheckDirName), so no
// reference reaches outside archiveDir. A reference that matches no run
// returns an error wrapping ErrRunNotFound.
func FindRun(archiveDir, ref string) (RunRef, error) {
	if ref == "latest" {
		runs, err := ListRuns(archiveDir)
		if err != nil {
			return RunRef{}, err
		}
		if len(runs) == 0 {
			return RunRef{}, fmt.Errorf("%w: no runs in %s", ErrRunNotFound, archiveDir)
		}
		return runs[0], nil
	}

	notFound := fmt.Errorf("%w: no run %q in %s", ErrRunNotFound, ref, archiveDir)
	if batchID, runID, nested := strings.Cut(ref, "/"); nested {
		if CheckDirName(batchID) != nil || CheckDirName(runID) != nil {
			return RunRef{}, notFound
		}
		if run, ok := runInDir(filepath.Join(archiveDir, batchID, runID), runID, batchID); ok {
			return run, nil
		}
		return RunRef{}, notFound
	}
	if CheckDirName(ref) != nil {
		return RunRef{}, notFound
	}
	if run, ok := runInDir(filepath.Join(archiveDir, ref), ref, ""); ok {
		return run, nil
	}
	runs, err := ListRuns(archiveDir)
	if err != nil {
		return RunRef{}, err
	}
	for _, run := range runs {
		if run.ID == ref {
			return run, nil
		}
	}
	return RunRef{}, notFound
}

// FindBatch resolves ref, a batch ID, to the batch's directory in archiveDir:
// a directory named ref that holds batch.json. ref must be a single directory
// name (see CheckDirName). A reference that matches no batch returns an error
// wrapping ErrRunNotFound.
func FindBatch(archiveDir, ref string) (string, error) {
	notFound := fmt.Errorf("%w: no batch %q in %s", ErrRunNotFound, ref, archiveDir)
	if CheckDirName(ref) != nil {
		return "", notFound
	}
	dir := filepath.Join(archiveDir, ref)
	if info, err := os.Stat(filepath.Join(dir, "batch.json")); err != nil || !info.Mode().IsRegular() {
		return "", notFound
	}
	return dir, nil
}

// runInDir returns the run in dir when dir holds an archive.json file.
func runInDir(dir, id, batchID string) (RunRef, bool) {
	path := filepath.Join(dir, "archive.json")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return RunRef{}, false
	}
	return RunRef{
		ID:          id,
		BatchID:     batchID,
		Dir:         dir,
		ArchivePath: path,
		Timestamp:   runTime(dir, id, info),
	}, true
}

// runTime returns the time ListRuns orders a run by.
func runTime(dir, id string, archiveInfo os.FileInfo) time.Time {
	if s, err := ReadSummary(filepath.Join(dir, "summary.json")); err == nil && !s.Timestamp.IsZero() {
		return s.Timestamp
	}
	if ts, err := ParseRunTimestamp(strings.TrimPrefix(id, "!")); err == nil {
		return ts
	}
	return archiveInfo.ModTime()
}
