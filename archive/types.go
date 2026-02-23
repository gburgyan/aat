package archive

import (
	"encoding/json"
	"time"

	"github.com/gburgyan/aat/plan"
)

// Archive is the top-level JSON structure for a serialized run.
type Archive struct {
	Metadata ArchiveMetadata `json:"metadata"`
	Steps    []StepRecord    `json:"steps"`
	Cleanup  []StepRecord    `json:"cleanup,omitempty"`
	Result   ArchiveResult   `json:"result"`
}

// ArchiveMetadata captures provenance and context for a run.
type ArchiveMetadata struct {
	Version          string     `json:"version"`
	RunID            string     `json:"runId"`
	Timestamp        time.Time  `json:"timestamp"`
	Plan             *plan.Plan `json:"plan"`
	InstantiatedPlan *plan.Plan `json:"instantiatedPlan,omitempty"`
	Environment      string     `json:"environment"`
	GraphVersion     string     `json:"graphVersion"`
	ToolVersion      string     `json:"toolVersion"`
	Attempt          int        `json:"attempt,omitempty"`       // 1-indexed attempt number
	TotalAttempts    int        `json:"totalAttempts,omitempty"` // total attempts (omitted if 1)
}

// StepRecord captures the execution trace for a single step.
type StepRecord struct {
	StepID      string                    `json:"stepId,omitempty"`
	Node        string                    `json:"node"`
	StartTime   time.Time                 `json:"startTime,omitempty"`
	DurationMs  int64                     `json:"duration_ms"`
	Inputs      map[string]any            `json:"inputs"`
	Request     *RequestRecord            `json:"request,omitempty"`
	Response    *ResponseRecord           `json:"response,omitempty"`
	Outputs        map[string]any            `json:"outputs,omitempty"`
	TransformScript string                   `json:"transformScript,omitempty"`
	Validation  *ValidationRecord         `json:"validation,omitempty"`
	Selections  []SelectionRecord         `json:"selections,omitempty"`
	Resolutions []ValueResolutionRecord   `json:"resolutions,omitempty"`
	DisplayOutputs    []DisplayOutputRecord    `json:"displayOutputs,omitempty"`
	ErrorClass        *ErrorClassRecord         `json:"errorClassification,omitempty"`
	ExpectFailure     *ExpectFailureRecord      `json:"expectFailure,omitempty"`
	ResponseBodyError *ResponseBodyErrorRecord  `json:"responseBodyError,omitempty"`
	Error             string                    `json:"error,omitempty"`
	RetryCount        int                       `json:"retryCount,omitempty"`
}

// DisplayOutputRecord captures an output value tagged for display.
type DisplayOutputRecord struct {
	Label string `json:"label"`
	Name  string `json:"name"`
	Value any    `json:"value,omitempty"`
}

// ExpectFailureRecord captures the outcome of a negative assertion.
type ExpectFailureRecord struct {
	Expected []int `json:"expected"`
	Actual   int   `json:"actual"`
	Passed   bool  `json:"passed"`
}

// ResponseBodyErrorRecord captures an error detected in a 2xx response body.
type ResponseBodyErrorRecord struct {
	RulePath string `json:"rulePath"`
	Rule     string `json:"rule"`
	Message  string `json:"message,omitempty"`
	Code     string `json:"code,omitempty"`
	Category string `json:"category,omitempty"`
}

// RequestRecord captures the outbound HTTP request.
type RequestRecord struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// ResponseRecord captures the HTTP response.
type ResponseRecord struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// ValidationRecord captures the outcome of mechanical assertions.
type ValidationRecord struct {
	Passed  bool              `json:"passed"`
	Results []AssertionRecord `json:"results,omitempty"`
}

// AssertionRecord captures the result of a single assertion.
type AssertionRecord struct {
	Type    string `json:"type"`
	Passed  bool   `json:"passed"`
	Skipped bool   `json:"skipped,omitempty"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Expr    string `json:"expr,omitempty"`
}

// SelectionRecord captures how an array selection was resolved.
type SelectionRecord struct {
	InputName     string         `json:"inputName"`
	SourceNode    string         `json:"sourceNode"`
	SourceField   string         `json:"sourceField"`
	SourceSize    int            `json:"sourceSize"`
	FilterExpr    string         `json:"filterExpr,omitempty"`
	FilteredSize  int            `json:"filteredSize"`
	Strategy      string `json:"strategy"`
	SelectedIndex int    `json:"selectedIndex"`
	SelectionName string `json:"selectionName,omitempty"`
}

// ErrorClassRecord captures the error classification for a failed step.
type ErrorClassRecord struct {
	Category     string `json:"category"`
	Detail       string `json:"detail"`
	Action       string `json:"action"`
	RetryAttempt int    `json:"retryAttempt"`
}

// ValueResolutionRecord captures how a single input was resolved.
type ValueResolutionRecord struct {
	InputName         string         `json:"inputName"`
	Source            string         `json:"source"`
	RawValue          any            `json:"rawValue,omitempty"`
	FinalValue        any            `json:"finalValue,omitempty"`
	FromStep          string         `json:"fromStep,omitempty"`
	FromOutput        string         `json:"fromOutput,omitempty"`
	Expression        string         `json:"expression,omitempty"`
	Constraint   string `json:"constraint,omitempty"`
	ConstraintOK *bool  `json:"constraintOk,omitempty"`
	PoolIndex    int    `json:"poolIndex,omitempty"`
	PoolSize     int    `json:"poolSize,omitempty"`
	Tried        []any  `json:"tried,omitempty"`
}

// RunSummary is a lightweight summary of a run, written alongside the full
// archive as summary.json to avoid reading multi-megabyte archives for listing.
type RunSummary struct {
	RunID         string    `json:"runId"`
	Timestamp     time.Time `json:"timestamp"`
	Outcome       string    `json:"outcome"`
	StepCount     int       `json:"stepCount"`
	PassedCount   int       `json:"passedCount"`
	FailedCount   int       `json:"failedCount"`
	DurationMs    int64     `json:"durationMs"`
	PlanName      string    `json:"planName,omitempty"`
	Attempt       int       `json:"attempt,omitempty"`
	TotalAttempts int       `json:"totalAttempts,omitempty"`
}

// ArchiveResult captures the overall outcome of a run.
type ArchiveResult struct {
	Outcome string `json:"outcome"`
	Error   string `json:"error,omitempty"`
}

// BatchArchive is the top-level JSON structure for a batch run (batch.json).
type BatchArchive struct {
	Metadata BatchMetadata   `json:"metadata"`
	Runs     []BatchRunEntry `json:"runs"`
	Result   BatchResult     `json:"result"`
}

// BatchMetadata captures provenance and context for a batch run.
type BatchMetadata struct {
	Version     string    `json:"version"`
	BatchID     string    `json:"batchId"`
	Timestamp   time.Time `json:"timestamp"`
	Source      string    `json:"source,omitempty"` // directory filter or "all"
	ToolVersion string    `json:"toolVersion,omitempty"`
}

// BatchRunEntry is a summary of a single run within a batch.
type BatchRunEntry struct {
	PlanName    string `json:"planName"`
	RunID       string `json:"runId"`
	Outcome     string `json:"outcome"`
	StepCount   int    `json:"stepCount"`
	PassedCount int    `json:"passedCount"`
	FailedCount int    `json:"failedCount"`
	DurationMs  int64  `json:"durationMs"`
	Error       string `json:"error,omitempty"`
	Attempts    int    `json:"attempts,omitempty"` // total attempts (omitted if 1)
}

// BatchResult captures the aggregate outcome of a batch.
type BatchResult struct {
	Outcome         string `json:"outcome"` // passed, failed, error
	TotalRuns       int    `json:"totalRuns"`
	PassedRuns      int    `json:"passedRuns"`
	FailedRuns      int    `json:"failedRuns"`
	ErrorRuns       int    `json:"errorRuns"`
	TotalDurationMs int64  `json:"totalDurationMs"`
}
