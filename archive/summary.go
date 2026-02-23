package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// BuildRunSummary computes a lightweight summary from a full archive.
func BuildRunSummary(a *Archive) *RunSummary {
	passed := 0
	failed := 0
	for _, s := range a.Steps {
		if stepRecordPassed(s) {
			passed++
		} else {
			failed++
		}
	}

	var totalDur int64
	for _, s := range a.Steps {
		totalDur += s.DurationMs
	}
	for _, s := range a.Cleanup {
		totalDur += s.DurationMs
	}

	return &RunSummary{
		RunID:         a.Metadata.RunID,
		Timestamp:     a.Metadata.Timestamp,
		Outcome:       a.Result.Outcome,
		StepCount:     len(a.Steps),
		PassedCount:   passed,
		FailedCount:   failed,
		DurationMs:    totalDur,
		PlanName:      extractPlanName(a),
		Attempt:       a.Metadata.Attempt,
		TotalAttempts: a.Metadata.TotalAttempts,
		Layers:        a.Metadata.Layers,
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

// stepRecordPassed returns true when a step has no errors or failures.
func stepRecordPassed(s StepRecord) bool {
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
