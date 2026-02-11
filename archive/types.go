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
	Node        string                    `json:"node"`
	StartTime   time.Time                 `json:"startTime,omitempty"`
	DurationMs  int64                     `json:"duration_ms"`
	Inputs      map[string]any            `json:"inputs"`
	Request     *RequestRecord            `json:"request,omitempty"`
	Response    *ResponseRecord           `json:"response,omitempty"`
	Outputs     map[string]any            `json:"outputs,omitempty"`
	Validation  *ValidationRecord         `json:"validation,omitempty"`
	Selections  []SelectionRecord         `json:"selections,omitempty"`
	Resolutions []ValueResolutionRecord   `json:"resolutions,omitempty"`
	Relaxations []RelaxationArchiveRecord `json:"relaxations,omitempty"`
	ErrorClass    *ErrorClassRecord         `json:"errorClassification,omitempty"`
	ExpectFailure *ExpectFailureRecord      `json:"expectFailure,omitempty"`
	Error         string                    `json:"error,omitempty"`
	RetryCount    int                       `json:"retryCount,omitempty"`
}

// ExpectFailureRecord captures the outcome of a negative assertion.
type ExpectFailureRecord struct {
	Expected []int `json:"expected"`
	Actual   int   `json:"actual"`
	Passed   bool  `json:"passed"`
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
	Strategy      string         `json:"strategy"`
	SelectedIndex int            `json:"selectedIndex"`
	LLMCall       *LLMCallRecord `json:"llmCall,omitempty"`
	SelectionName string         `json:"selectionName,omitempty"`
	FilterRelaxed bool           `json:"filterRelaxed,omitempty"`
}

// RelaxationArchiveRecord captures a single constraint relaxation event.
type RelaxationArchiveRecord struct {
	ConstraintName string `json:"constraintName"`
	InputRef       string `json:"inputRef"`
	Reason         string `json:"reason"`
	Depth          int    `json:"depth"`
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
	Constraint        string         `json:"constraint,omitempty"`
	ConstraintOK      *bool          `json:"constraintOk,omitempty"`
	PoolIndex         int            `json:"poolIndex,omitempty"`
	PoolSize          int            `json:"poolSize,omitempty"`
	Tried             []any          `json:"tried,omitempty"`
	LLMCall           *LLMCallRecord `json:"llmCall,omitempty"`
	Relaxed           bool           `json:"relaxed,omitempty"`
	RelaxedConstraint string         `json:"relaxedConstraint,omitempty"`
}

// LLMCallRecord captures details of a single LLM API call.
type LLMCallRecord struct {
	Messages     []LLMMessageRecord `json:"messages"`
	Model        string             `json:"model"`
	Response     string             `json:"response"`
	InputTokens  int                `json:"inputTokens"`
	OutputTokens int                `json:"outputTokens"`
	DurationMs   int64              `json:"durationMs"`
	FinishReason string             `json:"finishReason,omitempty"`
	Error        string             `json:"error,omitempty"`
}

// LLMMessageRecord captures a single prompt message sent to the LLM.
type LLMMessageRecord struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ArchiveResult captures the overall outcome of a run.
type ArchiveResult struct {
	Outcome string `json:"outcome"`
	Error   string `json:"error,omitempty"`
}
