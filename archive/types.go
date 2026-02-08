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
	Version      string     `json:"version"`
	RunID        string     `json:"runId"`
	Timestamp    time.Time  `json:"timestamp"`
	Plan         *plan.Plan `json:"plan"`
	Environment  string     `json:"environment"`
	GraphVersion string     `json:"graphVersion"`
	ToolVersion  string     `json:"toolVersion"`
}

// StepRecord captures the execution trace for a single step.
type StepRecord struct {
	Node       string            `json:"node"`
	StartTime  time.Time         `json:"startTime,omitempty"`
	DurationMs int64             `json:"duration_ms"`
	Inputs     map[string]any    `json:"inputs"`
	Request    *RequestRecord    `json:"request,omitempty"`
	Response   *ResponseRecord   `json:"response,omitempty"`
	Outputs    map[string]any    `json:"outputs,omitempty"`
	Validation *ValidationRecord `json:"validation,omitempty"`
	Selections []SelectionRecord `json:"selections,omitempty"`
	ErrorClass *ErrorClassRecord `json:"errorClassification,omitempty"`
	Error      string            `json:"error,omitempty"`
	RetryCount int               `json:"retryCount,omitempty"`
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
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Expr    string `json:"expr,omitempty"`
}

// SelectionRecord captures how an array selection was resolved.
type SelectionRecord struct {
	InputName     string `json:"inputName"`
	SourceNode    string `json:"sourceNode"`
	SourceField   string `json:"sourceField"`
	SourceSize    int    `json:"sourceSize"`
	FilterExpr    string `json:"filterExpr,omitempty"`
	FilteredSize  int    `json:"filteredSize"`
	Strategy      string `json:"strategy"`
	SelectedIndex int    `json:"selectedIndex"`
}

// ErrorClassRecord captures the error classification for a failed step.
type ErrorClassRecord struct {
	Category     string `json:"category"`
	Detail       string `json:"detail"`
	Action       string `json:"action"`
	RetryAttempt int    `json:"retryAttempt"`
}

// ArchiveResult captures the overall outcome of a run.
type ArchiveResult struct {
	Outcome string `json:"outcome"`
	Error   string `json:"error,omitempty"`
}
