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
	Outcome        Outcome
	Steps          []StepResult
	CleanupResults []StepResult
	// CleanupSkipped lists the registered cleanups that did not run because
	// they were no longer needed, in the order cleanup reached them.
	CleanupSkipped   []CleanupSkip
	Error            error
	InstantiatedPlan *plan.Plan // fully merged plan with graph defaults, nil on early errors

	// KnownIssues lists the entries that kept a failure out of this run's
	// outcome. When it is non-empty, Outcome is passed although a step failed.
	KnownIssues []KnownIssueApplied
	// KnownIssuesResolved lists entries whose step passed anyway, so the
	// entry has outlived the defect and should be deleted.
	KnownIssuesResolved []KnownIssueApplied
	// EndedEarly is true when a covered failure stopped the run before its
	// last step. The run reports passed, but it did not test everything.
	EndedEarly bool

	// Stopped is true when execution halted at a --stop-after checkpoint.
	Stopped bool
	// StoppedAt is the StepID of the checkpoint step (set when Stopped is true).
	StoppedAt string

	// StartTime and Duration are the wall-clock span of Engine.Run, including
	// retry waits, verification, and cleanup.
	StartTime time.Time
	Duration  time.Duration

	// FuzzCapped is true when --fuzz-cases dropped fuzz cases: the seed chose
	// which ran.
	FuzzCapped bool
	// FuzzWarnings are problems with how the run fuzzed, such as the shared
	// scope on a step that changes state.
	FuzzWarnings []string
	// FuzzCopiesSkipped counts the fuzz setup copies that weren't sent: a case
	// reused the live copy, or its setup had already failed.
	FuzzCopiesSkipped int

	// Seed is the seed the run drew pool picks and random selections from;
	// Engine.WithSeed with it replays those choices. It is 0 when the run
	// ended before any step resolved.
	Seed uint64
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
	// OutputsError, on a fuzz step, says why its outputs could not be read
	// from a successful response. The case is judged on the response anyway.
	OutputsError string
	// Fuzz, on a step the fuzzer made, is how its response was judged.
	Fuzz *FuzzResult
	// FuzzSetup, on a copy of a setup step made for a fuzz case, is the ID
	// of the case's step.
	FuzzSetup string
	// KnownIssue is set when the step carried a knownIssue entry, whether or
	// not it ended up applying. Applied says it kept this step's failure out
	// of the run's outcome; Expired says the entry had lapsed.
	KnownIssue    *KnownIssueResult
	ActualBaseURL string // executor's BaseURL used for this step (set when Request is non-nil)
	OriginalPath  string // request path before rewriting (empty if no rewrite occurred)
	// CleanupFor links a cleanup step to what it cleans up after: the ID of
	// the step that created the resource, or of the cleanup step before it in
	// a chain. Empty for main steps and for plan-level cleanup steps that are
	// not a graph pairing.
	CleanupFor string
	// WhenError, on a cleanup step, says why its pairing's when condition could
	// not be evaluated. The cleanup ran anyway.
	WhenError string
	// Iterations, on a step with a repeat block, records each request the step
	// sent, in order. The step's own request, response, and status are its last
	// request's.
	Iterations []IterationResult
	// RepeatStop says why a repeated step stopped sending requests: one of the
	// RepeatStop constants.
	RepeatStop string
}

// IterationResult records one request of a repeated step.
type IterationResult struct {
	Index      int // counting from 1
	StartTime  time.Time
	Duration   time.Duration // the request and its retries
	Request    *adapter.Request
	Response   *adapter.Response
	StatusCode int
	Outputs    map[string]any
	// Inputs, on a step whose repeat block pages with next, are the inputs this
	// request sent, its cursors included.
	Inputs        map[string]any
	UntilMet      bool // the repeat condition held on this response
	RetryCount    int
	Error         error
	OASValidation *oas.ValidationResult
	ActualBaseURL string
	OriginalPath  string
}

// Why a repeated step stopped sending requests.
const (
	RepeatStopUntil     = "until"     // its condition held
	RepeatStopExhausted = "exhausted" // next's cursors came back empty: the listing's last page
	RepeatStopLoop      = "loop"      // a response gave cursors an earlier request already sent
	RepeatStopMax       = "max"       // it sent max requests first
	RepeatStopTimeout   = "timeout"   // its timeout ran out first
	RepeatStopError     = "error"     // a request failed, or the condition couldn't be evaluated
)

// CleanupSkip records a registered cleanup that did not run because it was no
// longer needed.
type CleanupSkip struct {
	Node       string // the cleanup node
	CleanupFor string // the step whose resource it would have released, or the cleanup step before it in a chain
	Reason     string // CleanupSkipReleased or CleanupSkipWhen
	ReleasedBy string // for CleanupSkipReleased: the main step that already released the resource
	When       string // for CleanupSkipWhen: the pairing's condition, which was false
}

// Reasons a registered cleanup is skipped.
const (
	// CleanupSkipReleased: a later main step already released the resource.
	CleanupSkipReleased = "released"
	// CleanupSkipWhen: the pairing's when condition was false.
	CleanupSkipWhen = "when"
)

// DisplayOutput captures an output value tagged for display to the user.
type DisplayOutput struct {
	Label string // display label (e.g. "PNR")
	Name  string // output field name
	Value any    // extracted value
}

// ExpectFailureResult captures the outcome of a negative assertion step.
type ExpectFailureResult struct {
	ExpectedStatuses plan.ExpectedStatuses // statuses that were expected, as the plan wrote them
	ActualStatus     int                   // the actual response status
	Passed           bool                  // true if ActualStatus is in ExpectedStatuses
	Description      string                // from plan's expectFailure.description
}

// KnownIssueResult records a step's knownIssue entry and what it did.
type KnownIssueResult struct {
	Until   plan.Date // the last day the entry applies
	Reason  string    // what the defect is
	URL     string    // where it is tracked, if anywhere
	Expired bool      // the date had passed, so the entry did not apply
	Applied bool      // the entry kept this step's failure out of the run outcome
	// Resolved is true when the step passed although an entry was in force,
	// which means the entry has outlived the defect.
	Resolved bool
}

// KnownIssueApplied names the step an entry governed, for the run-level lists.
type KnownIssueApplied struct {
	StepID string
	Node   string
	KnownIssueResult
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
	Field         string // for an inline select: the field taken from the chosen element
	// SortField, SortValue, and Ties describe a min or max pick: the field
	// compared, the chosen value, and how many elements share it, the chosen
	// one included.
	SortField string
	SortValue *float64
	Ties      int
	OnTie     string // the selection's onTie: "", "first", or "fail"
}

// ValueResolution records how a single input was resolved.
type ValueResolution struct {
	InputName string // input being resolved
	Source    string // "plan_default", "expression", "plan_from", "select_edge",
	// "named_selection", "from_input", "from_resolved", "fallback_pool",
	// "graph_default", "layer", "optional_skip", "override_value", "raw_value", "error"
	// Layer names the layer that set the value, when a layer did.
	Layer        string
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
	PoolRef      string // the domain pool the pool came from, when poolRef named one
	Tried        []any  // values tried and rejected before this one
	// Error, for source "error", says why the input couldn't be resolved. For a
	// named selection that failed, InputName is the selection's name.
	Error string
}
