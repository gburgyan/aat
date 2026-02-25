package server

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gburgyan/aat/archive"
	"gopkg.in/yaml.v3"
)

// autoGenPrefixRe matches auto-generated run/batch directory prefixes.
var autoGenPrefixRe = regexp.MustCompile(`^(run|batch)-`)

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

// staticData holds in-memory archives for file-based viewing.
type staticData struct {
	runs    map[string]*staticRun
	batches map[string]*staticBatch
}

// ArchiveService provides read access to run archives for the web API.
type ArchiveService struct {
	archiveDir string
	// static holds in-memory archives for file-based viewing.
	// When non-nil, disk is not used.
	static *staticData
}

// NewArchiveService creates an ArchiveService that reads from the given directory.
func NewArchiveService(archiveDir string) *ArchiveService {
	return &ArchiveService{archiveDir: archiveDir}
}

// NewArchiveServiceFromRun creates an ArchiveService pre-loaded with a single
// run archive and its attempts. No disk access is performed.
func NewArchiveServiceFromRun(id string, a *archive.Archive, attempts map[int]*archive.Archive) *ArchiveService {
	return &ArchiveService{
		static: &staticData{
			runs: map[string]*staticRun{
				id: {archive: a, attempts: attempts},
			},
			batches: make(map[string]*staticBatch),
		},
	}
}

// NewArchiveServiceFromBatch creates an ArchiveService pre-loaded with a batch
// archive and its member runs. No disk access is performed.
func NewArchiveServiceFromBatch(id string, b *archive.BatchArchive, runs map[string]*archive.Archive, attempts map[string]map[int]*archive.Archive) *ArchiveService {
	staticRuns := make(map[string]*staticRun)
	for runID, a := range runs {
		sr := &staticRun{archive: a}
		if attempts != nil {
			sr.attempts = attempts[runID]
		}
		staticRuns[runID] = sr
	}

	return &ArchiveService{
		static: &staticData{
			runs: staticRuns,
			batches: map[string]*staticBatch{
				id: {
					batch: b,
					runs:  staticRuns,
				},
			},
		},
	}
}

// IsStatic returns true if the service is operating in static (in-memory) mode.
func (s *ArchiveService) IsStatic() bool {
	return s.static != nil
}

// ListRuns scans the archive directory for run directories, reads each archive,
// and returns summaries sorted newest-first. limit=0 means no limit.
// When savedOnly is true, only named/saved runs are returned.
// Unreadable archives are skipped silently.
func (s *ArchiveService) ListRuns(limit int, savedOnly bool) ([]RunListEntry, error) {
	if s.static != nil {
		return s.listRunsStatic(limit, savedOnly)
	}

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

	// Content-based scanning: check for archive.json (skip batch-only dirs).
	type runEntry struct {
		entry RunListEntry
	}
	var all []runEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !isRunDir(filepath.Join(s.archiveDir, e.Name())) {
			continue
		}
		if savedOnly && !isNamed(e.Name()) {
			continue
		}
		summary, err := s.readRunSummary(e.Name())
		if err != nil {
			continue
		}
		entry := summaryToRunListEntry(summary)
		entry.RunID = e.Name()
		entry.Name = displayName(e.Name())
		all = append(all, runEntry{entry: entry})
	}

	// Sort by metadata timestamp, newest first.
	sort.Slice(all, func(i, j int) bool {
		return all[i].entry.Timestamp.After(all[j].entry.Timestamp)
	})

	// Apply limit.
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}

	if len(all) == 0 {
		return nil, nil
	}

	results := make([]RunListEntry, len(all))
	for i, re := range all {
		results[i] = re.entry
	}
	return results, nil
}

func (s *ArchiveService) listRunsStatic(limit int, _ bool) ([]RunListEntry, error) {
	var results []RunListEntry
	for id, sr := range s.static.runs {
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

// LatestRunID returns the RunID of the most recent archive, or "" if none exist.
func (s *ArchiveService) LatestRunID() (string, error) {
	runs, err := s.ListRuns(1, false)
	if err != nil {
		return "", err
	}
	if len(runs) == 0 {
		return "", nil
	}
	return runs[0].RunID, nil
}

// GetRun loads a full run overview by run ID.
func (s *ArchiveService) GetRun(id string) (*RunDetail, error) {
	a, batchID, err := s.loadArchiveWithContext(id)
	if err != nil {
		return nil, err
	}
	detail := toRunDetail(a)
	detail.RunID = id
	detail.BatchID = batchID
	detail.Name = displayName(id)

	// Load attempt summaries if this was a retried run.
	// Use Attempt (actual attempt number) rather than TotalAttempts (max possible),
	// since TotalAttempts reflects configured retries, not actual retries taken.
	if a.Metadata.Attempt > 1 {
		detail.Attempts = s.loadAttemptSummaries(id, batchID)
		detail.TotalAttempts = len(detail.Attempts) + 1 // actual total from files found
	}

	return detail, nil
}

// GetAttempt loads a prior attempt archive for a run and returns it as a RunDetail.
func (s *ArchiveService) GetAttempt(runID string, attemptNum int) (*RunDetail, error) {
	// Check static data first.
	if s.static != nil {
		return s.getAttemptStatic(runID, attemptNum)
	}

	// Find the run directory (standalone or batch member).
	_, batchID, err := s.loadArchiveWithContext(runID)
	if err != nil {
		return nil, err
	}

	var runDir string
	if batchID != "" {
		runDir = filepath.Join(s.archiveDir, batchID, runID)
	} else {
		runDir = filepath.Join(s.archiveDir, runID)
	}

	attemptPath := filepath.Join(runDir, fmt.Sprintf("attempt-%02d.json", attemptNum))
	a, err := archive.Read(attemptPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("attempt %d of run %q: %w", attemptNum, runID, ErrRunNotFound)
		}
		return nil, fmt.Errorf("reading attempt %d of run %q: %w", attemptNum, runID, err)
	}

	detail := toRunDetail(a)
	detail.BatchID = batchID
	return detail, nil
}

func (s *ArchiveService) getAttemptStatic(runID string, attemptNum int) (*RunDetail, error) {
	// Check standalone runs.
	if sr, ok := s.static.runs[runID]; ok {
		if sr.attempts != nil {
			if a, ok := sr.attempts[attemptNum]; ok {
				detail := toRunDetail(a)
				return detail, nil
			}
		}
		return nil, fmt.Errorf("attempt %d of run %q: %w", attemptNum, runID, ErrRunNotFound)
	}

	// Check batch member runs.
	for batchID, sb := range s.static.batches {
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

// GetStep loads the full detail of a single step within a run.
func (s *ArchiveService) GetStep(runID, stepID string) (*StepDetail, error) {
	a, err := s.loadArchive(runID)
	if err != nil {
		return nil, err
	}
	return getStepFromArchive(a, runID, stepID)
}

// GetAttemptStep loads the full detail of a single step within a prior attempt archive.
func (s *ArchiveService) GetAttemptStep(runID string, attemptNum int, stepID string) (*StepDetail, error) {
	// In static mode, get the attempt detail and extract step from its archive.
	if s.static != nil {
		return s.getAttemptStepStatic(runID, attemptNum, stepID)
	}

	// Find the run directory (standalone or batch member).
	_, batchID, err := s.loadArchiveWithContext(runID)
	if err != nil {
		return nil, err
	}

	var runDir string
	if batchID != "" {
		runDir = filepath.Join(s.archiveDir, batchID, runID)
	} else {
		runDir = filepath.Join(s.archiveDir, runID)
	}

	attemptPath := filepath.Join(runDir, fmt.Sprintf("attempt-%02d.json", attemptNum))
	a, err := archive.Read(attemptPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("attempt %d of run %q: %w", attemptNum, runID, ErrRunNotFound)
		}
		return nil, fmt.Errorf("reading attempt %d of run %q: %w", attemptNum, runID, err)
	}

	return getStepFromArchive(a, runID, stepID)
}

func (s *ArchiveService) getAttemptStepStatic(runID string, attemptNum int, stepID string) (*StepDetail, error) {
	// Find the attempt archive in static data.
	findAttempt := func(sr *staticRun) *archive.Archive {
		if sr.attempts == nil {
			return nil
		}
		return sr.attempts[attemptNum]
	}

	if sr, ok := s.static.runs[runID]; ok {
		a := findAttempt(sr)
		if a == nil {
			return nil, fmt.Errorf("attempt %d of run %q: %w", attemptNum, runID, ErrRunNotFound)
		}
		return getStepFromArchive(a, runID, stepID)
	}

	for _, sb := range s.static.batches {
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

// getStepFromArchive extracts a step detail from a loaded archive.
func getStepFromArchive(a *archive.Archive, runID, stepID string) (*StepDetail, error) {
	rec, isCleanup, found := findStep(a, stepID)
	if !found {
		return nil, fmt.Errorf("step %q in run %q: %w", stepID, runID, ErrStepNotFound)
	}
	nodeSteps := nodeToStepMap(a)
	detail := toStepDetail(rec, isCleanup, nodeSteps)
	detail.Extractions = buildExtractions(stepID, rec.Outputs, a, nodeSteps)
	detail.PlanStepYAML = findPlanStepYAML(a, stepID, rec.Node, isCleanup)
	detail.InstantiatedStepYAML = findInstantiatedStepYAML(a, stepID, rec.Node, isCleanup)

	// Compute prev/next across all steps (steps then cleanup in order).
	allSteps := make([]string, 0, len(a.Steps)+len(a.Cleanup))
	for _, s := range a.Steps {
		allSteps = append(allSteps, effectiveStepID(s))
	}
	for _, s := range a.Cleanup {
		allSteps = append(allSteps, effectiveStepID(s))
	}
	for i, id := range allSteps {
		if id == stepID {
			if i > 0 {
				detail.PrevStepID = allSteps[i-1]
			}
			if i < len(allSteps)-1 {
				detail.NextStepID = allSteps[i+1]
			}
			break
		}
	}

	return detail, nil
}

// loadArchive reads an archive by run ID. Returns ErrRunNotFound if the
// archive directory or file doesn't exist. Falls through to batch directories
// if the run is not found as a standalone archive.
func (s *ArchiveService) loadArchive(id string) (*archive.Archive, error) {
	a, _, err := s.loadArchiveWithContext(id)
	return a, err
}

// loadArchiveWithContext reads an archive by run ID and returns the batch ID
// if the run is found inside a batch directory. Returns ("", ErrRunNotFound)
// if not found anywhere.
func (s *ArchiveService) loadArchiveWithContext(id string) (*archive.Archive, string, error) {
	// Check static data first.
	if s.static != nil {
		// Check batch member runs first to preserve batch context.
		for batchID, sb := range s.static.batches {
			if sr, ok := sb.runs[id]; ok {
				return sr.archive, batchID, nil
			}
		}
		// Then check standalone runs.
		if sr, ok := s.static.runs[id]; ok {
			return sr.archive, "", nil
		}
		return nil, "", fmt.Errorf("run %q: %w", id, ErrRunNotFound)
	}

	// First, try standalone run directory.
	archivePath := filepath.Join(s.archiveDir, id, "archive.json")
	a, err := archive.Read(archivePath)
	if err == nil {
		return a, "", nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, "", fmt.Errorf("reading archive %q: %w", id, err)
	}

	// Not found as standalone — scan batch directories (any dir with batch.json).
	entries, dirErr := os.ReadDir(s.archiveDir)
	if dirErr != nil {
		return nil, "", fmt.Errorf("run %q: %w", id, ErrRunNotFound)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !isBatchDir(filepath.Join(s.archiveDir, e.Name())) {
			continue
		}
		batchRunPath := filepath.Join(s.archiveDir, e.Name(), id, "archive.json")
		a, err := archive.Read(batchRunPath)
		if err == nil {
			return a, e.Name(), nil
		}
	}

	return nil, "", fmt.Errorf("run %q: %w", id, ErrRunNotFound)
}

// ListBatches scans the archive directory for batch directories, reads each batch.json,
// and returns summaries sorted newest-first. limit=0 means no limit.
// When savedOnly is true, only named/saved batches are returned.
// Unreadable batch archives are skipped silently.
func (s *ArchiveService) ListBatches(limit int, savedOnly bool) ([]BatchListEntry, error) {
	if s.static != nil {
		return s.listBatchesStatic(limit)
	}

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

	type batchDirInfo struct {
		dirName string
		batch   *archive.BatchArchive
	}
	var batchDirs []batchDirInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !isBatchDir(filepath.Join(s.archiveDir, e.Name())) {
			continue
		}
		if savedOnly && !isNamed(e.Name()) {
			continue
		}
		b, err := s.loadBatchArchive(e.Name())
		if err != nil {
			continue
		}
		batchDirs = append(batchDirs, batchDirInfo{dirName: e.Name(), batch: b})
	}

	// Sort by metadata timestamp, newest first.
	sort.Slice(batchDirs, func(i, j int) bool {
		return batchDirs[i].batch.Metadata.Timestamp.After(batchDirs[j].batch.Metadata.Timestamp)
	})

	if limit > 0 && len(batchDirs) > limit {
		batchDirs = batchDirs[:limit]
	}

	var results []BatchListEntry
	for _, bd := range batchDirs {
		entry := toBatchListEntry(bd.batch)
		entry.BatchID = bd.dirName
		entry.Name = displayName(bd.dirName)
		results = append(results, entry)
	}

	return results, nil
}

func (s *ArchiveService) listBatchesStatic(limit int) ([]BatchListEntry, error) {
	var results []BatchListEntry
	for id, sb := range s.static.batches {
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

// GetBatch loads a full batch overview by batch ID.
func (s *ArchiveService) GetBatch(id string) (*BatchDetail, error) {
	b, err := s.loadBatchArchive(id)
	if err != nil {
		return nil, err
	}
	detail := toBatchDetail(b)
	detail.BatchID = id
	detail.Name = displayName(id)
	return detail, nil
}

// loadBatchArchive reads a batch archive by batch ID. Returns ErrBatchNotFound
// if the batch directory or batch.json doesn't exist.
func (s *ArchiveService) loadBatchArchive(id string) (*archive.BatchArchive, error) {
	if s.static != nil {
		if sb, ok := s.static.batches[id]; ok {
			return sb.batch, nil
		}
		return nil, fmt.Errorf("batch %q: %w", id, ErrBatchNotFound)
	}

	batchPath := filepath.Join(s.archiveDir, id, "batch.json")
	b, err := archive.ReadBatch(batchPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("batch %q: %w", id, ErrBatchNotFound)
		}
		return nil, fmt.Errorf("reading batch archive %q: %w", id, err)
	}
	return b, nil
}

// loadAttemptSummaries scans a run directory for attempt-NN.json files
// and returns a summary of each prior failed attempt.
func (s *ArchiveService) loadAttemptSummaries(runID, batchID string) []AttemptSummary {
	if s.static != nil {
		return s.loadAttemptSummariesStatic(runID)
	}

	// Determine the run directory path.
	var runDir string
	if batchID != "" {
		runDir = filepath.Join(s.archiveDir, batchID, runID)
	} else {
		runDir = filepath.Join(s.archiveDir, runID)
	}

	entries, err := os.ReadDir(runDir)
	if err != nil {
		return nil
	}

	var attempts []AttemptSummary
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "attempt-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		attemptPath := filepath.Join(runDir, name)
		a, err := archive.Read(attemptPath)
		if err != nil {
			continue
		}

		attempts = append(attempts, AttemptSummary{
			Attempt:  a.Metadata.Attempt,
			Outcome:  a.Result.Outcome,
			Error:    a.Result.Error,
			FileName: name,
		})
	}

	// Sort by attempt number.
	sort.Slice(attempts, func(i, j int) bool {
		return attempts[i].Attempt < attempts[j].Attempt
	})

	return attempts
}

func (s *ArchiveService) loadAttemptSummariesStatic(runID string) []AttemptSummary {
	// Find the static run (standalone or batch member).
	var sr *staticRun
	if r, ok := s.static.runs[runID]; ok {
		sr = r
	} else {
		for _, sb := range s.static.batches {
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

// effectiveStepID returns the step's explicit StepID if set, else falls back to Node.
func effectiveStepID(s archive.StepRecord) string {
	if s.StepID != "" {
		return s.StepID
	}
	return s.Node
}

// findStep scans Steps then Cleanup for a step matching the given ID.
func findStep(a *archive.Archive, stepID string) (archive.StepRecord, bool, bool) {
	for _, s := range a.Steps {
		if effectiveStepID(s) == stepID {
			return s, false, true
		}
	}
	for _, s := range a.Cleanup {
		if effectiveStepID(s) == stepID {
			return s, true, true
		}
	}
	return archive.StepRecord{}, false, false
}

// formatDuration renders a millisecond duration as a human-readable string.
func formatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	if ms < 60000 {
		secs := float64(ms) / 1000.0
		if ms%1000 == 0 {
			return fmt.Sprintf("%ds", ms/1000)
		}
		return fmt.Sprintf("%.1fs", secs)
	}
	mins := ms / 60000
	remainMs := ms % 60000
	if remainMs == 0 {
		return fmt.Sprintf("%dm", mins)
	}
	secs := remainMs / 1000
	return fmt.Sprintf("%dm %ds", mins, secs)
}

// --- export/import ---

// ErrStaticMode is returned when a mutating operation is attempted in static mode.
var ErrStaticMode = errors.New("operation not available in static viewing mode")

// ExportRun streams the run directory as a zip to w and returns the suggested filename.
func (s *ArchiveService) ExportRun(id string, w io.Writer) (string, error) {
	if s.static != nil {
		return "", ErrStaticMode
	}
	dir, err := s.resolveRunDir(id)
	if err != nil {
		return "", err
	}
	if err := archive.ExportDir(dir, w); err != nil {
		return "", fmt.Errorf("exporting run %q: %w", id, err)
	}
	name := displayName(id)
	if name == "" {
		name = id
	}
	return name + ".aar", nil
}

// ExportBatch streams the batch directory as a zip to w and returns the suggested filename.
func (s *ArchiveService) ExportBatch(id string, w io.Writer) (string, error) {
	if s.static != nil {
		return "", ErrStaticMode
	}
	dir := filepath.Join(s.archiveDir, id)
	if !isBatchDir(dir) {
		return "", fmt.Errorf("batch %q: %w", id, ErrBatchNotFound)
	}
	if err := archive.ExportDir(dir, w); err != nil {
		return "", fmt.Errorf("exporting batch %q: %w", id, err)
	}
	name := displayName(id)
	if name == "" {
		name = id
	}
	return name + ".aab", nil
}

// ImportArchive imports a zip archive into the archive directory.
func (s *ArchiveService) ImportArchive(data io.ReaderAt, size int64, filename string) (ref string, archiveType string, err error) {
	if s.static != nil {
		return "", "", ErrStaticMode
	}
	name, err := archive.SanitizeArchiveName(filename)
	if err != nil {
		return "", "", err
	}
	dirName, archiveType, err := archive.ImportArchive(data, size, name, s.archiveDir)
	if err != nil {
		return "", "", err
	}
	return dirName, archiveType, nil
}

// resolveRunDir returns the filesystem path to a run directory (standalone or batch member).
func (s *ArchiveService) resolveRunDir(id string) (string, error) {
	// Try standalone.
	dir := filepath.Join(s.archiveDir, id)
	if isRunDir(dir) {
		return dir, nil
	}

	// Scan batch directories.
	entries, err := os.ReadDir(s.archiveDir)
	if err != nil {
		return "", fmt.Errorf("run %q: %w", id, ErrRunNotFound)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		batchPath := filepath.Join(s.archiveDir, e.Name())
		if !isBatchDir(batchPath) {
			continue
		}
		runPath := filepath.Join(batchPath, id)
		if isRunDir(runPath) {
			return runPath, nil
		}
	}
	return "", fmt.Errorf("run %q: %w", id, ErrRunNotFound)
}

// --- naming helpers ---

// isNamed returns true if the directory name represents a named/saved run or batch.
// A directory is named if it doesn't match the auto-generated run-/batch- prefix pattern,
// or if it starts with "!".
func isNamed(dirName string) bool {
	if strings.HasPrefix(dirName, "!") {
		return true
	}
	return !autoGenPrefixRe.MatchString(dirName)
}

// displayName returns the display name for a directory.
// For named dirs: strips the "!" prefix if present, returns the dir name.
// For unnamed dirs: returns empty string.
func displayName(dirName string) string {
	if strings.HasPrefix(dirName, "!") {
		return dirName[1:]
	}
	if !autoGenPrefixRe.MatchString(dirName) {
		return dirName
	}
	return ""
}

// isRunDir checks if a directory contains an archive.json file (standalone run).
func isRunDir(path string) bool {
	_, err := os.Stat(filepath.Join(path, "archive.json"))
	return err == nil
}

// isBatchDir checks if a directory contains a batch.json file.
func isBatchDir(path string) bool {
	_, err := os.Stat(filepath.Join(path, "batch.json"))
	return err == nil
}

// RenameRun renames a run directory to give it a user-friendly name.
// Returns the new directory name (routing reference).
func (s *ArchiveService) RenameRun(currentRef, newName string) (string, error) {
	if s.static != nil {
		return "", ErrStaticMode
	}
	return s.renameDir(currentRef, newName, "archive.json")
}

// UnnameRun restores a run directory to its original auto-generated name.
// Returns the original directory name.
func (s *ArchiveService) UnnameRun(currentRef string) (string, error) {
	if s.static != nil {
		return "", ErrStaticMode
	}
	return s.unnameDir(currentRef, "archive.json")
}

// RenameBatch renames a batch directory to give it a user-friendly name.
// Returns the new directory name (routing reference).
func (s *ArchiveService) RenameBatch(currentRef, newName string) (string, error) {
	if s.static != nil {
		return "", ErrStaticMode
	}
	return s.renameDir(currentRef, newName, "batch.json")
}

// UnnameBatch restores a batch directory to its original auto-generated name.
// Returns the original directory name.
func (s *ArchiveService) UnnameBatch(currentRef string) (string, error) {
	if s.static != nil {
		return "", ErrStaticMode
	}
	return s.unnameDir(currentRef, "batch.json")
}

// renameDir handles the common rename logic for runs and batches.
func (s *ArchiveService) renameDir(currentRef, newName, metadataFile string) (string, error) {
	oldPath := filepath.Join(s.archiveDir, currentRef)
	if _, err := os.Stat(oldPath); err != nil {
		return "", fmt.Errorf("directory %q: %w", currentRef, ErrRunNotFound)
	}

	if err := validateDirName(newName); err != nil {
		return "", err
	}

	// Compute new directory name.
	var newDirName string
	if newName == "" {
		// Save with original ID: read metadata to get original ID.
		origID, err := s.getOriginalID(currentRef, metadataFile)
		if err != nil {
			return "", err
		}
		newDirName = "!" + origID
	} else if autoGenPrefixRe.MatchString(newName) {
		// Name starts with run- or batch-: prefix with !
		newDirName = "!" + newName
	} else {
		newDirName = newName
	}

	// Check for collision.
	if newDirName == currentRef {
		return currentRef, nil
	}
	newPath := filepath.Join(s.archiveDir, newDirName)
	if _, err := os.Stat(newPath); err == nil {
		return "", fmt.Errorf("directory %q already exists", newDirName)
	}

	if err := os.Rename(oldPath, newPath); err != nil {
		return "", fmt.Errorf("renaming %q to %q: %w", currentRef, newDirName, err)
	}

	return newDirName, nil
}

// unnameDir restores a directory to its original auto-generated name.
func (s *ArchiveService) unnameDir(currentRef, metadataFile string) (string, error) {
	oldPath := filepath.Join(s.archiveDir, currentRef)
	if _, err := os.Stat(oldPath); err != nil {
		return "", fmt.Errorf("directory %q: %w", currentRef, ErrRunNotFound)
	}

	origID, err := s.getOriginalID(currentRef, metadataFile)
	if err != nil {
		return "", err
	}

	if origID == currentRef {
		return currentRef, nil
	}

	newPath := filepath.Join(s.archiveDir, origID)
	if _, err := os.Stat(newPath); err == nil {
		return "", fmt.Errorf("directory %q already exists", origID)
	}

	if err := os.Rename(oldPath, newPath); err != nil {
		return "", fmt.Errorf("restoring %q to %q: %w", currentRef, origID, err)
	}

	return origID, nil
}

// getOriginalID reads the metadata file to extract the original auto-generated ID.
func (s *ArchiveService) getOriginalID(dirName, metadataFile string) (string, error) {
	if metadataFile == "batch.json" {
		b, err := s.loadBatchArchive(dirName)
		if err != nil {
			return "", err
		}
		return b.Metadata.BatchID, nil
	}
	a, err := s.loadArchive(dirName)
	if err != nil {
		return "", err
	}
	return a.Metadata.RunID, nil
}

// validateDirName checks that a name is safe for use as a directory name.
func validateDirName(name string) error {
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("name must not contain path separators")
	}
	if strings.Contains(name, "\x00") {
		return fmt.Errorf("name must not contain null bytes")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("name must not be %q", name)
	}
	return nil
}

// --- summary helpers ---

// readRunSummary tries to read summary.json for a run directory. If the summary
// file is missing or unreadable, it falls back to reading the full archive and
// computing the summary on-the-fly.
func (s *ArchiveService) readRunSummary(dirName string) (*archive.RunSummary, error) {
	summaryPath := filepath.Join(s.archiveDir, dirName, "summary.json")
	summary, err := archive.ReadSummary(summaryPath)
	if err == nil {
		return summary, nil
	}

	// Fallback: read full archive and compute summary.
	a, err := s.loadArchive(dirName)
	if err != nil {
		return nil, err
	}
	summary = archive.BuildRunSummary(a)

	// Lazy backfill: write the summary so subsequent loads are fast.
	_ = archive.WriteSummary(summary, summaryPath)

	return summary, nil
}

// summaryToRunListEntry maps a RunSummary to a RunListEntry.
func summaryToRunListEntry(s *archive.RunSummary) RunListEntry {
	return RunListEntry{
		RunID:         s.RunID,
		Timestamp:     s.Timestamp,
		Outcome:       s.Outcome,
		StepCount:     s.StepCount,
		PassedCount:   s.PassedCount,
		FailedCount:   s.FailedCount,
		DurationMs:    s.DurationMs,
		PlanName:      s.PlanName,
		Attempt:       s.Attempt,
		TotalAttempts: s.TotalAttempts,
		Layers:        s.Layers,
	}
}

// --- conversion: archive → view model ---

func toBatchListEntry(b *archive.BatchArchive) BatchListEntry {
	return BatchListEntry{
		BatchID:         b.Metadata.BatchID,
		Timestamp:       b.Metadata.Timestamp,
		Outcome:         b.Result.Outcome,
		TotalRuns:       b.Result.TotalRuns,
		PassedRuns:      b.Result.PassedRuns,
		FailedRuns:      b.Result.FailedRuns,
		ErrorRuns:       b.Result.ErrorRuns,
		TotalDurationMs: b.Result.TotalDurationMs,
		Source:          b.Metadata.Source,
		ToolVersion:     b.Metadata.ToolVersion,
		Layers:          b.Metadata.Layers,
	}
}

func toBatchDetail(b *archive.BatchArchive) *BatchDetail {
	runs := make([]BatchRunSummary, len(b.Runs))
	for i, r := range b.Runs {
		runs[i] = BatchRunSummary{
			PlanName:    r.PlanName,
			RunID:       r.RunID,
			Outcome:     r.Outcome,
			StepCount:   r.StepCount,
			PassedCount: r.PassedCount,
			FailedCount: r.FailedCount,
			DurationMs:  r.DurationMs,
			Error:       r.Error,
			Attempts:    r.Attempts,
			Layers:      r.Layers,
			Permutation: r.Permutation,
		}
	}

	return &BatchDetail{
		BatchID:         b.Metadata.BatchID,
		Timestamp:       b.Metadata.Timestamp,
		Outcome:         b.Result.Outcome,
		TotalRuns:       b.Result.TotalRuns,
		PassedRuns:      b.Result.PassedRuns,
		FailedRuns:      b.Result.FailedRuns,
		ErrorRuns:       b.Result.ErrorRuns,
		TotalDurationMs: b.Result.TotalDurationMs,
		DurationDisplay: formatDuration(b.Result.TotalDurationMs),
		Source:          b.Metadata.Source,
		ToolVersion:     b.Metadata.ToolVersion,
		Runs:            runs,
		Layers:          b.Metadata.Layers,
		LayerGroups:     b.Metadata.LayerGroups,
	}
}

func toRunDetail(a *archive.Archive) *RunDetail {
	// Use the earliest step start time as the baseline for offset computation.
	// The metadata timestamp may be set after execution completes.
	var runStart time.Time
	for _, s := range a.Steps {
		if !s.StartTime.IsZero() && (runStart.IsZero() || s.StartTime.Before(runStart)) {
			runStart = s.StartTime
		}
	}
	for _, s := range a.Cleanup {
		if !s.StartTime.IsZero() && (runStart.IsZero() || s.StartTime.Before(runStart)) {
			runStart = s.StartTime
		}
	}

	passed := 0
	failed := 0
	steps := make([]StepSummary, len(a.Steps))
	for i, s := range a.Steps {
		steps[i] = toStepSummary(s, false, runStart)
		if stepPassed(s) {
			passed++
		} else {
			failed++
		}
	}

	var cleanup []StepSummary
	if len(a.Cleanup) > 0 {
		cleanup = make([]StepSummary, len(a.Cleanup))
		for i, s := range a.Cleanup {
			cleanup[i] = toStepSummary(s, true, runStart)
		}
	}

	dur := totalDuration(a)
	return &RunDetail{
		RunID:           a.Metadata.RunID,
		Timestamp:       a.Metadata.Timestamp,
		Outcome:         a.Result.Outcome,
		Error:           a.Result.Error,
		DurationMs:      dur,
		DurationDisplay: formatDuration(dur),
		StepCount:       len(a.Steps),
		PassedCount:     passed,
		FailedCount:     failed,
		PlanName:        extractPlanName(a),
		Environment:     a.Metadata.Environment,
		GraphVersion:    a.Metadata.GraphVersion,
		ToolVersion:     a.Metadata.ToolVersion,
		Steps:           steps,
		Cleanup:         cleanup,
		Attempt:         a.Metadata.Attempt,
		TotalAttempts:   a.Metadata.TotalAttempts,
		Layers:          a.Metadata.Layers,
	}
}

func toStepSummary(s archive.StepRecord, isCleanup bool, runStart time.Time) StepSummary {
	status := 0
	if s.Response != nil {
		status = s.Response.Status
	}

	assertionCount, assertionPassed := countAssertions(s)

	var offsetMs int64
	if !s.StartTime.IsZero() && !runStart.IsZero() {
		offsetMs = s.StartTime.Sub(runStart).Milliseconds()
		if offsetMs < 0 {
			offsetMs = 0
		}
	}

	return StepSummary{
		StepID:               effectiveStepID(s),
		Node:                 s.Node,
		Status:               status,
		DurationMs:           s.DurationMs,
		DurationDisplay:      formatDuration(s.DurationMs),
		Passed:               stepPassed(s),
		AssertionCount:       assertionCount,
		AssertionPassedCount: assertionPassed,
		DisplayOutputs:       toDisplayOutputs(s.DisplayOutputs),
		Error:                s.Error,
		IsCleanup:            isCleanup,
		HasSelections:        len(s.Selections) > 0,
		HasResolutions:       len(s.Resolutions) > 0,
		HasTransform:         s.TransformScript != "",
		RetryCount:           s.RetryCount,
		OffsetMs:             offsetMs,
	}
}

// nodeToStepMap builds a mapping from node name to effective step ID across all steps.
func nodeToStepMap(a *archive.Archive) map[string]string {
	m := make(map[string]string)
	for _, s := range a.Steps {
		m[s.Node] = effectiveStepID(s)
	}
	for _, s := range a.Cleanup {
		m[s.Node] = effectiveStepID(s)
	}
	return m
}

func toStepDetail(s archive.StepRecord, isCleanup bool, nodeSteps map[string]string) *StepDetail {
	status := 0
	if s.Response != nil {
		status = s.Response.Status
	}

	assertionCount, assertionPassed := countAssertions(s)

	return &StepDetail{
		StepID:               effectiveStepID(s),
		Node:                 s.Node,
		Status:               status,
		DurationMs:           s.DurationMs,
		DurationDisplay:      formatDuration(s.DurationMs),
		Passed:               stepPassed(s),
		AssertionCount:       assertionCount,
		AssertionPassedCount: assertionPassed,
		DisplayOutputs:       toDisplayOutputs(s.DisplayOutputs),
		Error:                s.Error,
		IsCleanup:            isCleanup,
		HasSelections:        len(s.Selections) > 0,
		HasResolutions:       len(s.Resolutions) > 0,
		RetryCount:           s.RetryCount,
		StartTime:            s.StartTime,
		Inputs:               s.Inputs,
		Outputs:              s.Outputs,
		Request:              toRequestDetail(s.Request),
		Response:             toResponseDetail(s.Response),
		Validation:           toValidationDetail(s.Validation),
		Selections:           toSelectionDetails(s.Selections, nodeSteps),
		Resolutions:          toResolutionDetails(s.Resolutions),
		ErrorClassification:  toErrorClassDetail(s.ErrorClass),
		ExpectFailure:        toExpectFailureDetail(s.ExpectFailure),
		ResponseBodyError:    toResponseBodyErrorDetail(s.ResponseBodyError),
		TransformScript:      s.TransformScript,
	}
}

func countAssertions(s archive.StepRecord) (total, passed int) {
	if s.Validation == nil {
		return 0, 0
	}
	for _, r := range s.Validation.Results {
		total++
		if r.Passed {
			passed++
		}
	}
	return total, passed
}

func toHeaderEntries(headers map[string]string) []HeaderEntry {
	if len(headers) == 0 {
		return nil
	}
	entries := make([]HeaderEntry, 0, len(headers))
	for k, v := range headers {
		entries = append(entries, HeaderEntry{Name: k, Value: v})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries
}

func toDisplayOutputs(recs []archive.DisplayOutputRecord) []DisplayOutput {
	if len(recs) == 0 {
		return nil
	}
	out := make([]DisplayOutput, len(recs))
	for i, r := range recs {
		out[i] = DisplayOutput{
			Label: r.Label,
			Name:  r.Name,
			Value: r.Value,
		}
	}
	return out
}

func toRequestDetail(r *archive.RequestRecord) *RequestDetail {
	if r == nil {
		return nil
	}
	return &RequestDetail{
		Method:  r.Method,
		URL:     r.URL,
		Headers: toHeaderEntries(r.Headers),
		Body:    r.Body,
	}
}

func toResponseDetail(r *archive.ResponseRecord) *ResponseDetail {
	if r == nil {
		return nil
	}
	return &ResponseDetail{
		Status:  r.Status,
		Headers: toHeaderEntries(r.Headers),
		Body:    r.Body,
	}
}

func toValidationDetail(v *archive.ValidationRecord) *ValidationDetail {
	if v == nil {
		return nil
	}
	var results []AssertionDetail
	if len(v.Results) > 0 {
		results = make([]AssertionDetail, len(v.Results))
		for i, r := range v.Results {
			results[i] = AssertionDetail{
				Type:    r.Type,
				Passed:  r.Passed,
				Skipped: r.Skipped,
				Message: r.Message,
				Path:    r.Path,
				Expr:    r.Expr,
			}
		}
	}
	return &ValidationDetail{
		Passed:  v.Passed,
		Results: results,
	}
}

func toSelectionDetails(recs []archive.SelectionRecord, nodeSteps map[string]string) []SelectionDetail {
	if len(recs) == 0 {
		return nil
	}
	out := make([]SelectionDetail, len(recs))
	for i, r := range recs {
		out[i] = SelectionDetail{
			InputName:     r.InputName,
			SourceStep:    nodeSteps[r.SourceNode],
			SourceNode:    r.SourceNode,
			SourceField:   r.SourceField,
			SourceSize:    r.SourceSize,
			FilterExpr:    r.FilterExpr,
			FilteredSize:  r.FilteredSize,
			Strategy:      r.Strategy,
			SelectedIndex: r.SelectedIndex,
			SelectionName: r.SelectionName,
		}
	}
	return out
}

func toResolutionDetails(recs []archive.ValueResolutionRecord) []ResolutionDetail {
	if len(recs) == 0 {
		return nil
	}
	out := make([]ResolutionDetail, len(recs))
	for i, r := range recs {
		out[i] = ResolutionDetail{
			InputName:    r.InputName,
			Source:       r.Source,
			RawValue:     r.RawValue,
			FinalValue:   r.FinalValue,
			FromStep:     r.FromStep,
			FromOutput:   r.FromOutput,
			Expression:   r.Expression,
			Constraint:   r.Constraint,
			ConstraintOK: r.ConstraintOK,
			PoolIndex:    r.PoolIndex,
			PoolSize:     r.PoolSize,
			Tried:        r.Tried,
		}
	}
	return out
}

func toErrorClassDetail(r *archive.ErrorClassRecord) *ErrorClassDetail {
	if r == nil {
		return nil
	}
	return &ErrorClassDetail{
		Category:     r.Category,
		Detail:       r.Detail,
		Action:       r.Action,
		RetryAttempt: r.RetryAttempt,
	}
}

func toExpectFailureDetail(r *archive.ExpectFailureRecord) *ExpectFailureDetail {
	if r == nil {
		return nil
	}
	return &ExpectFailureDetail{
		Expected: r.Expected,
		Actual:   r.Actual,
		Passed:   r.Passed,
	}
}

func toResponseBodyErrorDetail(r *archive.ResponseBodyErrorRecord) *ResponseBodyErrorDetail {
	if r == nil {
		return nil
	}
	return &ResponseBodyErrorDetail{
		RulePath: r.RulePath,
		Rule:     r.Rule,
		Message:  r.Message,
		Code:     r.Code,
		Category: r.Category,
	}
}

// --- extraction + plan step helpers ---

// buildExtractions builds a reverse index from a step's outputs to downstream consumers.
// For each output, it scans all steps' resolutions (FromStep match) and selections
// (SourceNode match via nodeSteps) to find consumers.
func buildExtractions(stepID string, outputs map[string]any, a *archive.Archive, nodeSteps map[string]string) []ExtractionDetail {
	if len(outputs) == 0 {
		return nil
	}

	// Collect all steps (main + cleanup) for scanning.
	allSteps := make([]archive.StepRecord, 0, len(a.Steps)+len(a.Cleanup))
	allSteps = append(allSteps, a.Steps...)
	allSteps = append(allSteps, a.Cleanup...)

	// Build output names sorted for deterministic order.
	outputNames := make([]string, 0, len(outputs))
	for name := range outputs {
		outputNames = append(outputNames, name)
	}
	sort.Strings(outputNames)

	var extractions []ExtractionDetail
	for _, name := range outputNames {
		val := outputs[name]
		var consumers []OutputConsumer

		for _, other := range allSteps {
			otherID := effectiveStepID(other)

			// Check resolutions: FromStep matches this step's ID and FromOutput matches output name.
			for _, res := range other.Resolutions {
				if res.FromStep == stepID && res.FromOutput == name {
					consumers = append(consumers, OutputConsumer{
						StepID:    otherID,
						InputName: res.InputName,
						Via:       "resolution",
					})
				}
			}

			// Check selections: SourceNode matches this step's node (via nodeSteps mapping).
			for _, sel := range other.Selections {
				if nodeSteps[sel.SourceNode] == stepID && sel.SourceField == name {
					consumers = append(consumers, OutputConsumer{
						StepID:    otherID,
						InputName: sel.InputName,
						Via:       "selection",
					})
				}
			}
		}

		extractions = append(extractions, ExtractionDetail{
			Name:      name,
			Value:     val,
			Consumers: consumers,
		})
	}

	return extractions
}

// findPlanStepYAML marshals the plan step matching the given step ID to YAML.
// Returns empty string if the archive has no plan or no matching step is found.
func findPlanStepYAML(a *archive.Archive, stepID, nodeName string, isCleanup bool) string {
	if a.Metadata.Plan == nil {
		return ""
	}

	// Search main execution steps by StepID().
	for _, s := range a.Metadata.Plan.Execution.Steps {
		if s.StepID() == stepID {
			return marshalStepYAML(s)
		}
	}

	// Search cleanup steps by node name.
	if isCleanup {
		for _, c := range a.Metadata.Plan.Execution.Cleanup {
			if c.Node == nodeName {
				return marshalStepYAML(c)
			}
		}
	}

	// Search verification steps by node name.
	for _, v := range a.Metadata.Plan.Execution.Verification {
		if v.Node == nodeName {
			return marshalStepYAML(v)
		}
	}

	return ""
}

// findInstantiatedStepYAML marshals the instantiated plan step matching the given step ID to YAML.
// Returns empty string if the archive has no instantiated plan or no matching step is found.
func findInstantiatedStepYAML(a *archive.Archive, stepID, nodeName string, isCleanup bool) string {
	if a.Metadata.InstantiatedPlan == nil {
		return ""
	}

	for _, s := range a.Metadata.InstantiatedPlan.Execution.Steps {
		if s.StepID() == stepID {
			return marshalStepYAML(s)
		}
	}

	if isCleanup {
		for _, c := range a.Metadata.InstantiatedPlan.Execution.Cleanup {
			if c.Node == nodeName {
				return marshalStepYAML(c)
			}
		}
	}

	for _, v := range a.Metadata.InstantiatedPlan.Execution.Verification {
		if v.Node == nodeName {
			return marshalStepYAML(v)
		}
	}

	return ""
}

// marshalStepYAML marshals any step-like value to YAML, returning empty string on error.
func marshalStepYAML(v any) string {
	data, err := yaml.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

// --- existing helpers ---

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
