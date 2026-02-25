package server

import (
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/gburgyan/aat/archive"
)

// ErrStaticMode is returned when a mutating operation is attempted in static mode.
var ErrStaticMode = errors.New("operation not available in static viewing mode")

// staticRun holds a pre-loaded run archive with its attempts.
type staticRun struct {
	archive  *archive.Archive
	attempts map[int]*archive.Archive // attempt number → archive
}

// staticBatch holds a pre-loaded batch with its member runs.
type staticBatch struct {
	batch *archive.BatchArchive
	runs  map[string]*staticRun // runID → archive + attempts
}

// staticArchiveService implements ArchiveService using in-memory archives.
type staticArchiveService struct {
	runs    map[string]*staticRun
	batches map[string]*staticBatch
}

// NewArchiveServiceFromRun creates an ArchiveService pre-loaded with a single
// run archive and its attempts. No disk access is performed.
func NewArchiveServiceFromRun(id string, a *archive.Archive, attempts map[int]*archive.Archive) ArchiveService {
	return &staticArchiveService{
		runs: map[string]*staticRun{
			id: {archive: a, attempts: attempts},
		},
		batches: make(map[string]*staticBatch),
	}
}

// NewArchiveServiceFromBatch creates an ArchiveService pre-loaded with a batch
// archive and its member runs. No disk access is performed.
func NewArchiveServiceFromBatch(id string, b *archive.BatchArchive, runs map[string]*archive.Archive, attempts map[string]map[int]*archive.Archive) ArchiveService {
	sRuns := make(map[string]*staticRun)
	for runID, a := range runs {
		sr := &staticRun{archive: a}
		if attempts != nil {
			sr.attempts = attempts[runID]
		}
		sRuns[runID] = sr
	}

	return &staticArchiveService{
		runs: sRuns,
		batches: map[string]*staticBatch{
			id: {
				batch: b,
				runs:  sRuns,
			},
		},
	}
}

func (s *staticArchiveService) ListRuns(limit int, _ bool) ([]RunListEntry, error) {
	var results []RunListEntry
	for id, sr := range s.runs {
		a := sr.archive
		summary := archive.BuildRunSummary(a)
		entry := summaryToRunListEntry(summary)
		entry.RunID = id
		entry.Name = displayName(id)
		results = append(results, entry)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.After(results[j].Timestamp)
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	if len(results) == 0 {
		return nil, nil
	}
	return results, nil
}

func (s *staticArchiveService) LatestRunID() (string, error) {
	runs, err := s.ListRuns(1, false)
	if err != nil {
		return "", err
	}
	if len(runs) == 0 {
		return "", nil
	}
	return runs[0].RunID, nil
}

func (s *staticArchiveService) GetRun(id string) (*RunDetail, error) {
	a, batchID, err := s.loadArchiveWithContext(id)
	if err != nil {
		return nil, err
	}
	detail := toRunDetail(a)
	detail.RunID = id
	detail.BatchID = batchID
	detail.Name = displayName(id)

	if a.Metadata.Attempt > 1 {
		detail.Attempts = s.loadAttemptSummaries(id)
		detail.TotalAttempts = len(detail.Attempts) + 1
	}

	return detail, nil
}

func (s *staticArchiveService) GetAttempt(runID string, attemptNum int) (*RunDetail, error) {
	// Check standalone runs.
	if sr, ok := s.runs[runID]; ok {
		if sr.attempts != nil {
			if a, ok := sr.attempts[attemptNum]; ok {
				detail := toRunDetail(a)
				return detail, nil
			}
		}
		return nil, fmt.Errorf("attempt %d of run %q: %w", attemptNum, runID, ErrRunNotFound)
	}

	// Check batch member runs.
	for batchID, sb := range s.batches {
		if sr, ok := sb.runs[runID]; ok {
			if sr.attempts != nil {
				if a, ok := sr.attempts[attemptNum]; ok {
					detail := toRunDetail(a)
					detail.BatchID = batchID
					return detail, nil
				}
			}
			return nil, fmt.Errorf("attempt %d of run %q: %w", attemptNum, runID, ErrRunNotFound)
		}
	}

	return nil, fmt.Errorf("run %q: %w", runID, ErrRunNotFound)
}

func (s *staticArchiveService) GetStep(runID, stepID string) (*StepDetail, error) {
	a, err := s.loadArchive(runID)
	if err != nil {
		return nil, err
	}
	return getStepFromArchive(a, runID, stepID)
}

func (s *staticArchiveService) GetAttemptStep(runID string, attemptNum int, stepID string) (*StepDetail, error) {
	findAttempt := func(sr *staticRun) *archive.Archive {
		if sr.attempts == nil {
			return nil
		}
		return sr.attempts[attemptNum]
	}

	if sr, ok := s.runs[runID]; ok {
		a := findAttempt(sr)
		if a == nil {
			return nil, fmt.Errorf("attempt %d of run %q: %w", attemptNum, runID, ErrRunNotFound)
		}
		return getStepFromArchive(a, runID, stepID)
	}

	for _, sb := range s.batches {
		if sr, ok := sb.runs[runID]; ok {
			a := findAttempt(sr)
			if a == nil {
				return nil, fmt.Errorf("attempt %d of run %q: %w", attemptNum, runID, ErrRunNotFound)
			}
			return getStepFromArchive(a, runID, stepID)
		}
	}

	return nil, fmt.Errorf("run %q: %w", runID, ErrRunNotFound)
}

func (s *staticArchiveService) ListBatches(limit int, _ bool) ([]BatchListEntry, error) {
	var results []BatchListEntry
	for id, sb := range s.batches {
		entry := toBatchListEntry(sb.batch)
		entry.BatchID = id
		entry.Name = displayName(id)
		results = append(results, entry)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.After(results[j].Timestamp)
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func (s *staticArchiveService) GetBatch(id string) (*BatchDetail, error) {
	sb, ok := s.batches[id]
	if !ok {
		return nil, fmt.Errorf("batch %q: %w", id, ErrBatchNotFound)
	}
	detail := toBatchDetail(sb.batch)
	detail.BatchID = id
	detail.Name = displayName(id)
	return detail, nil
}

func (s *staticArchiveService) ExportRun(_ string, _ io.Writer) (string, error) {
	return "", ErrStaticMode
}

func (s *staticArchiveService) ExportBatch(_ string, _ io.Writer) (string, error) {
	return "", ErrStaticMode
}

func (s *staticArchiveService) ImportArchive(_ io.ReaderAt, _ int64, _ string) (string, string, error) {
	return "", "", ErrStaticMode
}

func (s *staticArchiveService) RenameRun(_, _ string) (string, error) {
	return "", ErrStaticMode
}

func (s *staticArchiveService) UnnameRun(_ string) (string, error) {
	return "", ErrStaticMode
}

func (s *staticArchiveService) RenameBatch(_, _ string) (string, error) {
	return "", ErrStaticMode
}

func (s *staticArchiveService) UnnameBatch(_ string) (string, error) {
	return "", ErrStaticMode
}

// --- static internal helpers ---

func (s *staticArchiveService) loadArchive(id string) (*archive.Archive, error) {
	a, _, err := s.loadArchiveWithContext(id)
	return a, err
}

func (s *staticArchiveService) loadArchiveWithContext(id string) (*archive.Archive, string, error) {
	// Check batch member runs first to preserve batch context.
	for batchID, sb := range s.batches {
		if sr, ok := sb.runs[id]; ok {
			return sr.archive, batchID, nil
		}
	}
	// Then check standalone runs.
	if sr, ok := s.runs[id]; ok {
		return sr.archive, "", nil
	}
	return nil, "", fmt.Errorf("run %q: %w", id, ErrRunNotFound)
}

func (s *staticArchiveService) loadAttemptSummaries(runID string) []AttemptSummary {
	// Find the static run (standalone or batch member).
	var sr *staticRun
	if r, ok := s.runs[runID]; ok {
		sr = r
	} else {
		for _, sb := range s.batches {
			if r, ok := sb.runs[runID]; ok {
				sr = r
				break
			}
		}
	}
	if sr == nil || len(sr.attempts) == 0 {
		return nil
	}

	var attempts []AttemptSummary
	for num, a := range sr.attempts {
		attempts = append(attempts, AttemptSummary{
			Attempt:  a.Metadata.Attempt,
			Outcome:  a.Result.Outcome,
			Error:    a.Result.Error,
			FileName: fmt.Sprintf("attempt-%02d.json", num),
		})
	}

	sort.Slice(attempts, func(i, j int) bool {
		return attempts[i].Attempt < attempts[j].Attempt
	})

	return attempts
}
