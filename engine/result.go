package engine

import (
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph/oas"
	"github.com/gburgyan/aat/plan"
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
	// OutcomeAborted means execution was interrupted by the user (e.g. Ctrl+C).
	OutcomeAborted
	// OutcomeStopped means execution halted at a requested checkpoint
	// (--stop-after). Steps up to the checkpoint passed; cleanup was skipped so
	// created resources remain alive for an external harness.
	OutcomeStopped
)

func (o Outcome) String() string {
	switch o {
	case OutcomePassed:
		return "passed"
	case OutcomeFailed:
		return "failed"
	case OutcomeError:
		return "error"
	case OutcomeAborted:
		return "aborted"
	case OutcomeStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// RunResult captures the complete outcome of a plan execution.
type RunResult struct {
	Outcome          Outcome
	Steps            []StepResult
	CleanupResults   []StepResult
	Error            error
	InstantiatedPlan *plan.Plan // fully merged plan with graph defaults, nil on early errors

	// Stopped is true when execution halted at a --stop-after checkpoint.
	Stopped bool
	// StoppedAt is the StepID of the checkpoint step (set when Stopped is true).
	StoppedAt string

	// StartTime and Duration are the wall-clock span of Engine.Run, including
	// retry waits, verification, and cleanup.
	StartTime time.Time
	Duration  time.Duration
}

// Elapsed returns how long the run took: its recorded wall-clock Duration, or,
// for a result the engine did not time, the sum of its step and cleanup
// durations.
func (r *RunResult) Elapsed() time.Duration {
	if r.Duration > 0 {
		return r.Duration
	}
	var total time.Duration
	for _, s := range r.Steps {
		total += s.Duration
	}
	for _, s := range r.CleanupResults {
		total += s.Duration
	}
	return total
}

// StepResult captures the outcome of a single step execution.
type StepResult struct {
	StepID            string // effective step identifier (ID if set, else Node)
	Node              string
	Inputs            map[string]any
	Request           *adapter.Request
	Response          *adapter.Response
	Outputs           map[string]any
	TransformScript   string
	Selections        []SelectionDecision
	Resolutions       []ValueResolution
	StatusCode        int
	Error             error
	StartTime         time.Time                  // start of the first attempt
	Duration          time.Duration              // from StartTime to the end of the last attempt, retry waits included
	ErrorClass        *ErrorClassification       // nil on success
	RetryCount        int                        // number of retries performed (0 = no retries)
	RetriedOn         []ErrorCategory            // category of each failed attempt that was retried, in order
	Validation        *validate.MechanicalResult // nil if no assertions configured
	OASValidation     *oas.ValidationResult      // nil when OAS validation not configured or node has no OAS ref
	DisplayOutputs    []DisplayOutput            // outputs tagged with display labels
	ExpectFailure     *ExpectFailureResult       // non-nil for negative assertion steps
	ResponseBodyError *ResponseBodyError         // non-nil when error detected in 2xx response body
	ActualBaseURL     string                     // executor's BaseURL used for this step (set when Request is non-nil)
	OriginalPath      string                     // request path before rewriting (empty if no rewrite occurred)
}

// DisplayOutput captures an output value tagged for display to the user.
type DisplayOutput struct {
	Label string // display label (e.g. "PNR")
	Name  string // output field name
	Value any    // extracted value
}

// ExpectFailureResult captures the outcome of a negative assertion step.
type ExpectFailureResult struct {
	ExpectedStatuses []int  // status codes that were expected
	ActualStatus     int    // the actual response status
	Passed           bool   // true if ActualStatus is in ExpectedStatuses
	Description      string // from plan's expectFailure.description
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
	SelectionName string // non-empty for named selections
}

// ValueResolution records how a single input was resolved.
type ValueResolution struct {
	InputName string // input being resolved
	Source    string // "plan_default", "expression", "plan_from", "select_edge",
	// "named_selection", "from_input", "from_resolved", "fallback_pool",
	// "graph_default", "optional_skip", "override_value"
	RawValue     any    // before expression evaluation (nil if N/A)
	FinalValue   any    // after evaluation + coercion
	FromStep     string // source step (for edge/select_edge/from_input)
	FromOutput   string // source output (for edge/select_edge)
	FromInput    string // source input name (for from_input resolution)
	Expression   string // template string if evaluated (e.g. "{{today + 5 days}}")
	Constraint   string // constraint expression if checked
	ConstraintOK bool   // whether constraint passed
	PoolIndex    int    // index in fallback pool (-1 if not from pool)
	PoolSize     int    // fallback pool size (0 if no pool)
	Tried        []any  // values tried and rejected before this one
}
