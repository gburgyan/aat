package engine

import (
	"time"

	"github.com/gburgyan/aat/adapter"
)

// Outcome describes the overall result of a plan execution.
type Outcome int

const (
	// OutcomePassed means all steps completed successfully.
	OutcomePassed Outcome = iota
	// OutcomeFailed means a step returned a non-2xx status.
	OutcomeFailed
	// OutcomeError means execution was aborted due to an infrastructure error.
	OutcomeError
)

func (o Outcome) String() string {
	switch o {
	case OutcomePassed:
		return "passed"
	case OutcomeFailed:
		return "failed"
	case OutcomeError:
		return "error"
	default:
		return "unknown"
	}
}

// RunResult captures the complete outcome of a plan execution.
type RunResult struct {
	Outcome        Outcome
	Steps          []StepResult
	CleanupResults []StepResult
	Error          error
}

// StepResult captures the outcome of a single step execution.
type StepResult struct {
	Node       string
	Inputs     map[string]any
	Request    *adapter.Request
	Response   *adapter.Response
	Outputs    map[string]any
	Selections []SelectionDecision
	StatusCode int
	Error      error
	Duration   time.Duration
	ErrorClass *ErrorClassification // nil on success
	RetryCount int                  // number of retries performed (0 = no retries)
}

// SelectionDecision records how a particular array selection was resolved.
type SelectionDecision struct {
	InputName     string
	SourceNode    string
	SourceField   string
	SourceSize    int
	FilterExpr    string
	FilteredSize  int
	Strategy      string
	SelectedIndex int
}
