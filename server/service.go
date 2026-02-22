package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gburgyan/aat/archive"
	"gopkg.in/yaml.v3"
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
		a, err := s.loadArchive(dir.Name())
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

// GetRun loads a full run overview by run ID.
func (s *ArchiveService) GetRun(id string) (*RunDetail, error) {
	a, batchID, err := s.loadArchiveWithContext(id)
	if err != nil {
		return nil, err
	}
	detail := toRunDetail(a)
	detail.BatchID = batchID

	// Load attempt summaries if this was a retried run.
	if a.Metadata.TotalAttempts > 1 {
		detail.Attempts = s.loadAttemptSummaries(id, batchID)
	}

	return detail, nil
}

// GetStep loads the full detail of a single step within a run.
func (s *ArchiveService) GetStep(runID, stepID string) (*StepDetail, error) {
	a, err := s.loadArchive(runID)
	if err != nil {
		return nil, err
	}
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
	// First, try standalone run directory.
	archivePath := filepath.Join(s.archiveDir, id, "archive.json")
	a, err := archive.Read(archivePath)
	if err == nil {
		return a, "", nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, "", fmt.Errorf("reading archive %q: %w", id, err)
	}

	// Not found as standalone — scan batch directories.
	entries, dirErr := os.ReadDir(s.archiveDir)
	if dirErr != nil {
		return nil, "", fmt.Errorf("run %q: %w", id, ErrRunNotFound)
	}

	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "batch-") {
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
// Unreadable batch archives are skipped silently.
func (s *ArchiveService) ListBatches(limit int) ([]BatchListEntry, error) {
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

	var batchDirs []os.DirEntry
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "batch-") {
			batchDirs = append(batchDirs, e)
		}
	}

	sort.Slice(batchDirs, func(i, j int) bool {
		return batchDirs[i].Name() > batchDirs[j].Name()
	})

	if limit > 0 && len(batchDirs) > limit {
		batchDirs = batchDirs[:limit]
	}

	var results []BatchListEntry
	for _, dir := range batchDirs {
		b, err := s.loadBatchArchive(dir.Name())
		if err != nil {
			continue
		}
		results = append(results, toBatchListEntry(b))
	}

	return results, nil
}

// GetBatch loads a full batch overview by batch ID.
func (s *ArchiveService) GetBatch(id string) (*BatchDetail, error) {
	b, err := s.loadBatchArchive(id)
	if err != nil {
		return nil, err
	}
	return toBatchDetail(b), nil
}

// loadBatchArchive reads a batch archive by batch ID. Returns ErrBatchNotFound
// if the batch directory or batch.json doesn't exist.
func (s *ArchiveService) loadBatchArchive(id string) (*archive.BatchArchive, error) {
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

// --- conversion: archive → view model ---

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
		RunID:         a.Metadata.RunID,
		Timestamp:     a.Metadata.Timestamp,
		Outcome:       a.Result.Outcome,
		StepCount:     len(a.Steps),
		PassedCount:   passed,
		FailedCount:   failed,
		DurationMs:    totalDuration(a),
		PlanName:      extractPlanName(a),
		Attempt:       a.Metadata.Attempt,
		TotalAttempts: a.Metadata.TotalAttempts,
	}
}

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
