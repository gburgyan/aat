package engine

import (
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/validate"
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
	Selections  []SelectionDecision
	Resolutions []ValueResolution
	StatusCode  int
	Error      error
	StartTime  time.Time
	Duration   time.Duration
	ErrorClass *ErrorClassification       // nil on success
	RetryCount int                        // number of retries performed (0 = no retries)
	Validation *validate.MechanicalResult // nil if no assertions configured
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

// ValueResolution records how a single input was resolved.
type ValueResolution struct {
	InputName    string         // input being resolved
	Source       string         // "edge", "select_edge", "plan_default", "expression",
	                            // "fallback_pool", "graph_default", "llm", "optional_skip"
	RawValue     any            // before expression evaluation (nil if N/A)
	FinalValue   any            // after evaluation + coercion
	FromStep     string         // source step (for edge/select_edge)
	FromOutput   string         // source output (for edge/select_edge)
	Expression   string         // template string if evaluated (e.g. "{{today + 5 days}}")
	Constraint   string         // constraint expression if checked
	ConstraintOK bool           // whether constraint passed
	PoolIndex    int            // index in fallback pool (-1 if not from pool)
	PoolSize     int            // fallback pool size (0 if no pool)
	Tried        []any          // values tried and rejected before this one
	LLMCall      *LLMCallRecord // non-nil if LLM was consulted
}

// LLMCallRecord captures details of a single LLM API call for value selection.
type LLMCallRecord struct {
	Messages     []LLMMessage
	Model        string
	Response     string
	InputTokens  int
	OutputTokens int
	DurationMs   int64
	FinishReason string
	Error        string // non-empty if LLM call failed
}

// LLMMessage is a prompt message sent to the LLM.
type LLMMessage struct {
	Role    string
	Content string
}
