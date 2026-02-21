package server

import (
	"errors"
	"time"
)

// ErrTraceNotFound indicates the requested trace does not exist.
var ErrTraceNotFound = errors.New("trace not found")

// TraceListEntry is a summary of a single plan trace for list display.
type TraceListEntry struct {
	TraceID         string    `json:"traceId"`
	Timestamp       time.Time `json:"timestamp"`
	Prompt          string    `json:"prompt"`
	WorkflowName    string    `json:"workflowName,omitempty"`
	TotalDurationMs int64     `json:"totalDurationMs"`
	HasError        bool      `json:"hasError"`
	LLMCallCount    int       `json:"llmCallCount"`
}

// TraceDetail is the full view of a plan trace for the trace detail page.
type TraceDetail struct {
	TraceID            string          `json:"traceId"`
	Timestamp          time.Time       `json:"timestamp"`
	Prompt             string          `json:"prompt"`
	SelectionCall      *LLMCallDetail  `json:"selectionCall"`
	SelectionRetryCall *LLMCallDetail  `json:"selectionRetryCall,omitempty"`
	WorkflowSelection  any             `json:"workflowSelection,omitempty"`
	Skeleton           *SkeletonDetail `json:"skeleton,omitempty"`
	PlanCall           *LLMCallDetail  `json:"planCall"`
	TargetedResponse   any             `json:"targetedResponse,omitempty"`
	MergedPlanYAML     string          `json:"mergedPlanYaml,omitempty"`
	FinalPlanYAML      string          `json:"finalPlanYaml,omitempty"`
	ValidationErr      string          `json:"validationErr,omitempty"`
	RetryCall          *LLMCallDetail  `json:"retryCall,omitempty"`
	RetryValidationErr string          `json:"retryValidationErr,omitempty"`
	TotalDurationMs    int64           `json:"totalDurationMs"`
	Error              string          `json:"error,omitempty"`

	// Wrong-plan escape fields.
	WrongPlanSignal any            `json:"wrongPlanSignal,omitempty"`
	WrongPlanCall   *LLMCallDetail `json:"wrongPlanCall,omitempty"`
	ReselectionCall *LLMCallDetail `json:"reselectionCall,omitempty"`

	// Workflow metadata.
	WorkflowName         string `json:"workflowName,omitempty"`
	TemplatePath         string `json:"templatePath,omitempty"`
	Repetitions          any    `json:"repetitions,omitempty"`
	TemplateExpandedYAML string `json:"templateExpandedYaml,omitempty"`
}

// SkeletonDetail captures the skeleton plan construction step.
type SkeletonDetail struct {
	PlanYAML    string   `json:"planYaml"`
	UnfedInputs []string `json:"unfedInputs,omitempty"`
	DurationMs  int64    `json:"durationMs"`
}
