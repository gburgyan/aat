package server

import "time"

// RunListEntry is a summary of a single run for list display.
type RunListEntry struct {
	RunID       string    `json:"runId"`
	Timestamp   time.Time `json:"timestamp"`
	Outcome     string    `json:"outcome"`
	StepCount   int       `json:"stepCount"`
	PassedCount int       `json:"passedCount"`
	FailedCount int       `json:"failedCount"`
	DurationMs  int64     `json:"durationMs"`
	PlanName    string    `json:"planName,omitempty"`
}
