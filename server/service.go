package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	a, err := s.loadArchive(id)
	if err != nil {
		return nil, err
	}
	return toRunDetail(a), nil
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
// archive directory or file doesn't exist.
func (s *ArchiveService) loadArchive(id string) (*archive.Archive, error) {
	archivePath := filepath.Join(s.archiveDir, id, "archive.json")
	a, err := archive.Read(archivePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("run %q: %w", id, ErrRunNotFound)
		}
		return nil, fmt.Errorf("reading archive %q: %w", id, err)
	}
	return a, nil
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

// hasLLMCalls checks whether any selection or resolution in the step has an LLM call.
func hasLLMCalls(s archive.StepRecord) bool {
	for _, sel := range s.Selections {
		if sel.LLMCall != nil {
			return true
		}
	}
	for _, res := range s.Resolutions {
		if res.LLMCall != nil {
			return true
		}
	}
	return false
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

func toRunDetail(a *archive.Archive) *RunDetail {
	passed := 0
	failed := 0
	steps := make([]StepSummary, len(a.Steps))
	for i, s := range a.Steps {
		steps[i] = toStepSummary(s, false)
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
			cleanup[i] = toStepSummary(s, true)
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
	}
}

func toStepSummary(s archive.StepRecord, isCleanup bool) StepSummary {
	status := 0
	if s.Response != nil {
		status = s.Response.Status
	}

	assertionCount, assertionPassed := countAssertions(s)

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
		HasLLMCalls:          hasLLMCalls(s),
		HasTransform:         s.TransformScript != "",
		RetryCount:           s.RetryCount,
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
		HasLLMCalls:          hasLLMCalls(s),
		RetryCount:           s.RetryCount,
		StartTime:            s.StartTime,
		Inputs:               s.Inputs,
		Outputs:              s.Outputs,
		Request:              toRequestDetail(s.Request),
		Response:             toResponseDetail(s.Response),
		Validation:           toValidationDetail(s.Validation),
		Selections:           toSelectionDetails(s.Selections, nodeSteps),
		Resolutions:          toResolutionDetails(s.Resolutions),
		Relaxations:          toRelaxationDetails(s.Relaxations),
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
			FilterRelaxed: r.FilterRelaxed,
			LLMCall:       toLLMCallDetail(r.LLMCall),
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
			InputName:         r.InputName,
			Source:            r.Source,
			RawValue:          r.RawValue,
			FinalValue:        r.FinalValue,
			FromStep:          r.FromStep,
			FromOutput:        r.FromOutput,
			Expression:        r.Expression,
			Constraint:        r.Constraint,
			ConstraintOK:      r.ConstraintOK,
			PoolIndex:         r.PoolIndex,
			PoolSize:          r.PoolSize,
			Tried:             r.Tried,
			Relaxed:           r.Relaxed,
			RelaxedConstraint: r.RelaxedConstraint,
			LLMCall:           toLLMCallDetail(r.LLMCall),
		}
	}
	return out
}

func toLLMCallDetail(c *archive.LLMCallRecord) *LLMCallDetail {
	if c == nil {
		return nil
	}
	var msgs []LLMMessageDetail
	if len(c.Messages) > 0 {
		msgs = make([]LLMMessageDetail, len(c.Messages))
		for i, m := range c.Messages {
			msgs[i] = LLMMessageDetail{
				Role:    m.Role,
				Content: m.Content,
			}
		}
	}
	return &LLMCallDetail{
		Messages:     msgs,
		Model:        c.Model,
		Response:     c.Response,
		InputTokens:  c.InputTokens,
		OutputTokens: c.OutputTokens,
		DurationMs:   c.DurationMs,
		FinishReason: c.FinishReason,
		Error:        c.Error,
	}
}

func toRelaxationDetails(recs []archive.RelaxationArchiveRecord) []RelaxationDetail {
	if len(recs) == 0 {
		return nil
	}
	out := make([]RelaxationDetail, len(recs))
	for i, r := range recs {
		out[i] = RelaxationDetail{
			ConstraintName: r.ConstraintName,
			InputRef:       r.InputRef,
			Reason:         r.Reason,
			Depth:          r.Depth,
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
