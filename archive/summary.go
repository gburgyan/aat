package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// RunDurationMs returns how long a run took: the wall-clock duration the
// archive records, or, for an archive written before that was recorded, the
// sum of its step and cleanup durations.
func RunDurationMs(a *Archive) int64 {
	if a.Result.DurationMs > 0 {
		return a.Result.DurationMs
	}
	var total int64
	for _, s := range a.Steps {
		total += s.DurationMs
	}
	for _, s := range a.Cleanup {
		total += s.DurationMs
	}
	return total
}

// BuildRunSummary computes a lightweight summary from a full archive.
func BuildRunSummary(a *Archive) *RunSummary {
	passed := 0
	failed := 0
	for _, s := range a.Steps {
		if StepPassed(s) {
			passed++
		} else {
			failed++
		}
	}

	// Count categorized issues across all steps (main + cleanup).
	var allSteps []StepRecord
	allSteps = append(allSteps, a.Steps...)
	allSteps = append(allSteps, a.Cleanup...)
	issues := countIssues(allSteps)

	return &RunSummary{
		RunID:         a.Metadata.RunID,
		Timestamp:     a.Metadata.Timestamp,
		Outcome:       a.Result.Outcome,
		StepCount:     len(a.Steps),
		PassedCount:   passed,
		FailedCount:   failed,
		DurationMs:    RunDurationMs(a),
		PlanName:      extractPlanName(a),
		Attempt:       a.Metadata.Attempt,
		TotalAttempts: a.Metadata.TotalAttempts,
		Layers:        a.Metadata.Layers,
		Issues:        issues,
	}
}

// WriteSummary writes a RunSummary as JSON to the given path.
func WriteSummary(s *RunSummary, path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling summary: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating summary directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing summary file: %w", err)
	}

	return nil
}

// ReadSummary loads a RunSummary from a JSON file at the given path.
func ReadSummary(path string) (*RunSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading summary file: %w", err)
	}

	var s RunSummary
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshalling summary: %w", err)
	}

	return &s, nil
}

// countIssues counts categorized issues across all steps. Returns nil when
// no issues exist so omitempty works correctly.
func countIssues(steps []StepRecord) map[string]int {
	var issues map[string]int
	for _, s := range steps {
		if s.OASValidation == nil || s.OASValidation.Skipped {
			continue
		}
		n := 0
		if s.OASValidation.Request != nil {
			n += len(s.OASValidation.Request.Errors)
		}
		if s.OASValidation.Response != nil {
			n += len(s.OASValidation.Response.Errors)
		}
		if n > 0 {
			if issues == nil {
				issues = make(map[string]int)
			}
			issues["oas"] += n
		}
	}
	return issues
}

// extractPlanName returns a human-readable name for the plan, preferring
// Prompt > Description > Goal.
func extractPlanName(a *Archive) string {
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
