package server

import (
	"encoding/json"
	"errors"
	"time"
)

// Sentinel errors for service methods.
var (
	ErrRunNotFound  = errors.New("run not found")
	ErrStepNotFound = errors.New("step not found")
)

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

// RunDetail is the full overview of a single run.
type RunDetail struct {
	RunID           string        `json:"runId"`
	Timestamp       time.Time     `json:"timestamp"`
	Outcome         string        `json:"outcome"`
	Error           string        `json:"error,omitempty"`
	DurationMs      int64         `json:"durationMs"`
	DurationDisplay string        `json:"durationDisplay"`
	StepCount       int           `json:"stepCount"`
	PassedCount     int           `json:"passedCount"`
	FailedCount     int           `json:"failedCount"`
	PlanName        string        `json:"planName,omitempty"`
	Environment     string        `json:"environment,omitempty"`
	GraphVersion    string        `json:"graphVersion,omitempty"`
	ToolVersion     string        `json:"toolVersion,omitempty"`
	Steps           []StepSummary `json:"steps"`
	Cleanup         []StepSummary `json:"cleanup,omitempty"`
}

// StepSummary is a compact view of a step for timeline display.
type StepSummary struct {
	StepID              string          `json:"stepId"`
	Node                string          `json:"node"`
	Status              int             `json:"status,omitempty"`
	DurationMs          int64           `json:"durationMs"`
	DurationDisplay     string          `json:"durationDisplay"`
	Passed              bool            `json:"passed"`
	AssertionCount      int             `json:"assertionCount"`
	AssertionPassedCount int            `json:"assertionPassedCount"`
	DisplayOutputs      []DisplayOutput `json:"displayOutputs,omitempty"`
	Error               string          `json:"error,omitempty"`
	IsCleanup           bool            `json:"isCleanup,omitempty"`
	HasSelections       bool            `json:"hasSelections,omitempty"`
	HasResolutions      bool            `json:"hasResolutions,omitempty"`
	HasLLMCalls         bool            `json:"hasLLMCalls,omitempty"`
	RetryCount          int             `json:"retryCount,omitempty"`
}

// StepDetail is the full audit view of a single step.
type StepDetail struct {
	StepID               string                  `json:"stepId"`
	Node                 string                  `json:"node"`
	Status               int                     `json:"status,omitempty"`
	DurationMs           int64                   `json:"durationMs"`
	DurationDisplay      string                  `json:"durationDisplay"`
	Passed               bool                    `json:"passed"`
	AssertionCount       int                     `json:"assertionCount"`
	AssertionPassedCount int                     `json:"assertionPassedCount"`
	DisplayOutputs       []DisplayOutput         `json:"displayOutputs,omitempty"`
	Error                string                  `json:"error,omitempty"`
	IsCleanup            bool                    `json:"isCleanup,omitempty"`
	HasSelections        bool                    `json:"hasSelections,omitempty"`
	HasResolutions       bool                    `json:"hasResolutions,omitempty"`
	HasLLMCalls          bool                    `json:"hasLLMCalls,omitempty"`
	RetryCount           int                     `json:"retryCount,omitempty"`
	StartTime            time.Time               `json:"startTime,omitempty"`
	Inputs               map[string]any          `json:"inputs,omitempty"`
	Outputs              map[string]any          `json:"outputs,omitempty"`
	Request              *RequestDetail          `json:"request,omitempty"`
	Response             *ResponseDetail         `json:"response,omitempty"`
	Validation           *ValidationDetail       `json:"validation,omitempty"`
	Selections           []SelectionDetail       `json:"selections,omitempty"`
	Resolutions          []ResolutionDetail      `json:"resolutions,omitempty"`
	Relaxations          []RelaxationDetail      `json:"relaxations,omitempty"`
	ErrorClassification  *ErrorClassDetail       `json:"errorClassification,omitempty"`
	ExpectFailure        *ExpectFailureDetail    `json:"expectFailure,omitempty"`
	ResponseBodyError    *ResponseBodyErrorDetail `json:"responseBodyError,omitempty"`
	PrevStepID           string                  `json:"prevStepId,omitempty"`
	NextStepID           string                  `json:"nextStepId,omitempty"`
}

// HeaderEntry is a single HTTP header name-value pair.
type HeaderEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// DisplayOutput surfaces a labeled output value for quick display.
type DisplayOutput struct {
	Label string `json:"label"`
	Name  string `json:"name"`
	Value any    `json:"value,omitempty"`
}

// RequestDetail captures the outbound HTTP request.
type RequestDetail struct {
	Method  string          `json:"method"`
	URL     string          `json:"url"`
	Headers []HeaderEntry   `json:"headers,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
}

// ResponseDetail captures the HTTP response.
type ResponseDetail struct {
	Status  int             `json:"status"`
	Headers []HeaderEntry   `json:"headers,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
}

// ValidationDetail captures the outcome of mechanical assertions.
type ValidationDetail struct {
	Passed  bool              `json:"passed"`
	Results []AssertionDetail `json:"results,omitempty"`
}

// AssertionDetail captures the result of a single assertion.
type AssertionDetail struct {
	Type    string `json:"type"`
	Passed  bool   `json:"passed"`
	Skipped bool   `json:"skipped,omitempty"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Expr    string `json:"expr,omitempty"`
}

// SelectionDetail captures how an array selection was resolved.
type SelectionDetail struct {
	InputName     string         `json:"inputName"`
	SourceStep    string         `json:"sourceStep,omitempty"`
	SourceNode    string         `json:"sourceNode"`
	SourceField   string         `json:"sourceField"`
	SourceSize    int            `json:"sourceSize"`
	FilterExpr    string         `json:"filterExpr,omitempty"`
	FilteredSize  int            `json:"filteredSize"`
	Strategy      string         `json:"strategy"`
	SelectedIndex int            `json:"selectedIndex"`
	SelectionName string         `json:"selectionName,omitempty"`
	FilterRelaxed bool           `json:"filterRelaxed,omitempty"`
	LLMCall       *LLMCallDetail `json:"llmCall,omitempty"`
}

// ResolutionDetail captures how a single input was resolved.
type ResolutionDetail struct {
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
	Relaxed           bool           `json:"relaxed,omitempty"`
	RelaxedConstraint string         `json:"relaxedConstraint,omitempty"`
	LLMCall           *LLMCallDetail `json:"llmCall,omitempty"`
}

// LLMCallDetail captures details of a single LLM API call.
type LLMCallDetail struct {
	Messages     []LLMMessageDetail `json:"messages"`
	Model        string             `json:"model"`
	Response     string             `json:"response"`
	InputTokens  int                `json:"inputTokens"`
	OutputTokens int                `json:"outputTokens"`
	DurationMs   int64              `json:"durationMs"`
	FinishReason string             `json:"finishReason,omitempty"`
	Error        string             `json:"error,omitempty"`
}

// LLMMessageDetail captures a single prompt message sent to the LLM.
type LLMMessageDetail struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RelaxationDetail captures a single constraint relaxation event.
type RelaxationDetail struct {
	ConstraintName string `json:"constraintName"`
	InputRef       string `json:"inputRef"`
	Reason         string `json:"reason"`
	Depth          int    `json:"depth"`
}

// ErrorClassDetail captures the error classification for a failed step.
type ErrorClassDetail struct {
	Category     string `json:"category"`
	Detail       string `json:"detail"`
	Action       string `json:"action"`
	RetryAttempt int    `json:"retryAttempt"`
}

// ExpectFailureDetail captures the outcome of a negative assertion.
type ExpectFailureDetail struct {
	Expected []int `json:"expected"`
	Actual   int   `json:"actual"`
	Passed   bool  `json:"passed"`
}

// ResponseBodyErrorDetail captures an error detected in a 2xx response body.
type ResponseBodyErrorDetail struct {
	RulePath string `json:"rulePath"`
	Rule     string `json:"rule"`
	Message  string `json:"message,omitempty"`
	Code     string `json:"code,omitempty"`
	Category string `json:"category,omitempty"`
}
