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
	// CleanupSkipped lists the registered cleanups that did not run because
	// they were no longer needed.
	CleanupSkipped []CleanupSkipRecord `json:"cleanupSkipped,omitempty"`
	// KnownIssues lists the entries that kept a failure out of this run's
	// outcome. When it is non-empty the run passed although a step failed.
	KnownIssues []KnownIssueRecord `json:"knownIssues,omitempty"`
	// KnownIssuesResolved lists entries whose step passed anyway, so the entry
	// has outlived the defect it was written for and should be deleted.
	KnownIssuesResolved []KnownIssueRecord `json:"knownIssuesResolved,omitempty"`
	Result              ArchiveResult      `json:"result"`
}

// ArchiveMetadata captures provenance and context for a run.
type ArchiveMetadata struct {
	Version          string     `json:"version" redact:"-"`
	RunID            string     `json:"runId" redact:"-"`
	Timestamp        time.Time  `json:"timestamp"`
	Plan             *plan.Plan `json:"plan"`
	InstantiatedPlan *plan.Plan `json:"instantiatedPlan,omitempty"`
	Environment      string     `json:"environment" redact:"-"`
	GraphVersion     string     `json:"graphVersion" redact:"-"`
	ToolVersion      string     `json:"toolVersion" redact:"-"`
	Attempt          int        `json:"attempt,omitempty"`           // 1-indexed attempt number
	TotalAttempts    int        `json:"totalAttempts,omitempty"`     // total attempts (omitted if 1)
	Layers           []string   `json:"layers,omitempty" redact:"-"` // effective layers applied to this run
	// OASValidation is the OpenAPI validation mode the run used (auto, strict,
	// or off) when its graph references a spec. It is empty for a graph that
	// references none, for a run the MCP server executed, and in archives
	// written before the mode was recorded.
	OASValidation string `json:"oasValidation,omitempty" redact:"-"`
	// Seed is the seed the run drew its pool picks and random selections
	// from; `aat run plan --seed` with it replays them. It is 0 for a run that
	// ended before any step, and in archives written before seeds were kept.
	Seed uint64 `json:"seed,omitempty" redact:"-"`
}

// StepRecord captures the execution trace for a single step.
type StepRecord struct {
	StepID          string                  `json:"stepId,omitempty" redact:"-"`
	Node            string                  `json:"node" redact:"-"`
	StartTime       time.Time               `json:"startTime,omitempty"`
	DurationMs      int64                   `json:"durationMs"`
	Inputs          map[string]any          `json:"inputs"`
	Request         *RequestRecord          `json:"request,omitempty"`
	Response        *ResponseRecord         `json:"response,omitempty"`
	Outputs         map[string]any          `json:"outputs,omitempty"`
	TransformScript string                  `json:"transformScript,omitempty"`
	Validation      *ValidationRecord       `json:"validation,omitempty"`
	Selections      []SelectionRecord       `json:"selections,omitempty"`
	Resolutions     []ValueResolutionRecord `json:"resolutions,omitempty"`
	DisplayOutputs  []DisplayOutputRecord   `json:"displayOutputs,omitempty"`
	ErrorClass      *ErrorClassRecord       `json:"errorClassification,omitempty"`
	ExpectFailure   *ExpectFailureRecord    `json:"expectFailure,omitempty"`
	// KnownIssue is set when the step carried an entry, whether or not it
	// applied. The step still reads as failed; see Archive.KnownIssues.
	KnownIssue        *KnownIssueRecord        `json:"knownIssue,omitempty"`
	ResponseBodyError *ResponseBodyErrorRecord `json:"responseBodyError,omitempty"`
	// Fuzz, on a step the fuzzer made, is the case it sent and how its
	// response was judged.
	Fuzz          *FuzzRecord          `json:"fuzz,omitempty"`
	OASValidation *OASValidationRecord `json:"oasValidation,omitempty"`
	Error         string               `json:"error,omitempty"`
	RetryCount    int                  `json:"retryCount,omitempty"`
	RetriedOn     []string             `json:"retriedOn,omitempty" redact:"-"` // error category of each retried attempt, in order
	// CleanupFor, on a cleanup step, is the ID of the step whose resource it
	// releases, or of the cleanup step before it in a chain.
	CleanupFor string `json:"cleanupFor,omitempty" redact:"-"`
	// WhenError, on a cleanup step, says why its pairing's when condition could
	// not be evaluated. The cleanup ran anyway.
	WhenError string `json:"whenError,omitempty"`
	// Iterations, on a step with a repeat block, records each request it sent,
	// in order. The step's own request and response are its last request's, and
	// its outputs are that request's with the collected outputs gathered across
	// all of them.
	Iterations []IterationRecord `json:"iterations,omitempty"`
	// RepeatStop says why a repeated step stopped sending requests: "until" when
	// its condition held, "exhausted" when its next cursors came back empty on
	// a listing's last page, "loop" when a response gave cursors an earlier
	// request already sent, "max" or "timeout" when a limit ended it first, and
	// "error" when a request failed or the condition couldn't be evaluated.
	RepeatStop string `json:"repeatStop,omitempty" redact:"-"`
}

// IterationRecord is one request of a repeated step.
type IterationRecord struct {
	Index      int             `json:"index"`
	StartTime  time.Time       `json:"startTime,omitempty"`
	DurationMs int64           `json:"durationMs"`
	Request    *RequestRecord  `json:"request,omitempty"`
	Response   *ResponseRecord `json:"response,omitempty"`
	Outputs    map[string]any  `json:"outputs,omitempty"`
	// Inputs, on a step whose repeat block pages with next, are the inputs this
	// request sent, its cursors included.
	Inputs        map[string]any       `json:"inputs,omitempty"`
	UntilMet      bool                 `json:"untilMet"`
	RetryCount    int                  `json:"retryCount,omitempty"`
	Error         string               `json:"error,omitempty"`
	OASValidation *OASValidationRecord `json:"oasValidation,omitempty"`
}

// CleanupSkipRecord is a registered cleanup that did not run because it was no
// longer needed.
type CleanupSkipRecord struct {
	Node       string `json:"node" redact:"-"`
	CleanupFor string `json:"cleanupFor,omitempty" redact:"-"` // the step it would have cleaned up after
	// Reason is "released" when a later step already released the resource, or
	// "when" when the pairing's when condition was false.
	Reason     string `json:"reason" redact:"-"`
	ReleasedBy string `json:"releasedBy,omitempty" redact:"-"` // the step that released it
	When       string `json:"when,omitempty" redact:"-"`       // the condition that was false
}

// Description says why the cleanup was skipped, as in "released by
// capturePayment" or `when status == "authorized" is false`.
func (r CleanupSkipRecord) Description() string {
	switch r.Reason {
	case "released":
		return "released by " + r.ReleasedBy
	case "when":
		return "when " + r.When + " is false"
	default:
		return r.Reason
	}
}

// DisplayOutputRecord captures an output value tagged for display.
type DisplayOutputRecord struct {
	Label string `json:"label" redact:"-"`
	Name  string `json:"name" redact:"-"`
	Value any    `json:"value,omitempty"`
}

// FuzzRecord is a fuzz case and its finding.
type FuzzRecord struct {
	ID       string `json:"id" redact:"-"`
	Target   string `json:"target" redact:"-"`
	Mode     string `json:"mode" redact:"-"`
	Input    string `json:"input" redact:"-"`
	Strategy string `json:"strategy" redact:"-"`
	Value    any    `json:"value"`
	// JudgedAs is the mode the response was judged by, when it differs from
	// Mode: negative when the request broke the OpenAPI spec.
	JudgedAs string `json:"judgedAs,omitempty" redact:"-"`
	// SpecViolations are the ways the request broke the OpenAPI spec.
	SpecViolations []string `json:"specViolations,omitempty"`
	// Finding is empty when the response was what the case called for.
	Finding string `json:"finding,omitempty" redact:"-"`
	// Fails is true when the finding failed the run.
	Fails bool `json:"fails,omitempty"`
}

// ExpectFailureRecord captures the outcome of a negative assertion. Expected
// holds the statuses as the plan wrote them, so an HTTP plan archives numbers
// and a gRPC one archives names.
type ExpectFailureRecord struct {
	Expected []plan.ExpectedStatus `json:"expected"`
	Actual   int                   `json:"actual"`
	// ActualName is the gRPC status the step came back with, and is empty for
	// an HTTP step.
	ActualName string `json:"actualName,omitempty"`
	Passed     bool   `json:"passed"`
}

// KnownIssueRecord captures a knownIssue entry and what it did on this run.
type KnownIssueRecord struct {
	// StepID and Node are set on the run-level lists and empty on a step's
	// own record, where the step already says which one it is.
	StepID string    `json:"stepId,omitempty"`
	Node   string    `json:"node,omitempty"`
	Until  plan.Date `json:"until"`
	Reason string    `json:"reason"`
	URL    string    `json:"url,omitempty"`
	// Applied: the entry kept this step's failure out of the run's outcome.
	Applied bool `json:"applied,omitempty"`
	// Expired: the date had passed, so the failure counted as usual.
	Expired bool `json:"expired,omitempty"`
	// Resolved: the step passed, so the entry is no longer earning its place.
	Resolved bool `json:"resolved,omitempty"`
}

// Description renders why the entry mattered, in the one form every surface
// shows, as CleanupSkipRecord.Description does for a skipped cleanup.
func (r KnownIssueRecord) Description() string {
	switch {
	case r.Resolved:
		return "passing again; remove the entry (expires " + r.Until.String() + ")"
	case r.Expired:
		return "expired " + r.Until.String() + ": " + r.Reason
	default:
		return "until " + r.Until.String() + ": " + r.Reason
	}
}

// ResponseBodyErrorRecord captures an error detected in a 2xx response body.
type ResponseBodyErrorRecord struct {
	RulePath string `json:"rulePath" redact:"-"`
	Rule     string `json:"rule" redact:"-"`
	Message  string `json:"message,omitempty"`
	Code     string `json:"code,omitempty"`
	Category string `json:"category,omitempty" redact:"-"`
}

// OASValidationRecord captures runtime OAS schema validation for a step.
type OASValidationRecord struct {
	OperationID string            `json:"operationId,omitempty" redact:"-"`
	Request     *OASPayloadRecord `json:"request,omitempty"`
	Response    *OASPayloadRecord `json:"response,omitempty"`
	Skipped     bool              `json:"skipped,omitempty"`
	SkipReason  string            `json:"skipReason,omitempty"`
}

// OASPayloadRecord captures validation for one direction (request or response).
type OASPayloadRecord struct {
	Valid               bool             `json:"valid"`
	Errors              []OASSchemaError `json:"errors,omitempty"`
	CompilationWarnings []string         `json:"compilationWarnings,omitempty"`
	// Skipped marks a payload that was not validated, with SkipReason saying
	// why: a request body type the validator doesn't read, or a schema it
	// couldn't compile.
	Skipped    bool   `json:"skipped,omitempty"`
	SkipReason string `json:"skipReason,omitempty"`
}

// OASSchemaError is a single OAS validation error in the archive.
type OASSchemaError struct {
	Path    string `json:"path" redact:"-"`
	Message string `json:"message"`
}

// RequestRecord captures the outbound HTTP request.
type RequestRecord struct {
	Method      string            `json:"method" redact:"-"`
	URL         string            `json:"url"`
	OriginalURL string            `json:"originalUrl,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        json.RawMessage   `json:"body,omitempty"`
	// Protocol names how the request was sent. It is empty for HTTP, which
	// every archive written before there was a second protocol is.
	Protocol string `json:"protocol,omitempty" redact:"-"`
}

// ResponseRecord captures the HTTP response.
type ResponseRecord struct {
	// Status is the HTTP status, or for a gRPC response the one its code maps
	// to, so that a reader comparing statuses needs no special case.
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`

	// Trailers are a gRPC call's trailing metadata, kept apart from its header
	// metadata because a server chooses which to send a value in. It is empty
	// for an HTTP response.
	Trailers map[string]string `json:"trailers,omitempty"`

	// GRPCCode is the gRPC status name, such as "NOT_FOUND". It is empty for
	// an HTTP response, and is what a reader should show when it is not.
	GRPCCode string `json:"grpcCode,omitempty" redact:"-"`
	// GRPCMessage is the message the server sent with the status.
	GRPCMessage string `json:"grpcMessage,omitempty"`
	// GRPCDetails are the status details, each already encoded as JSON.
	GRPCDetails []json.RawMessage `json:"grpcDetails,omitempty"`
}

// ValidationRecord captures the outcome of mechanical assertions.
type ValidationRecord struct {
	Passed  bool              `json:"passed"`
	Results []AssertionRecord `json:"results,omitempty"`
}

// AssertionRecord captures the result of a single assertion.
type AssertionRecord struct {
	Type    string `json:"type" redact:"-"`
	Passed  bool   `json:"passed"`
	Skipped bool   `json:"skipped,omitempty"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty" redact:"-"`
	Expr    string `json:"expr,omitempty"`
	Raw     bool   `json:"raw,omitempty"`
}

// SelectionRecord captures how an array selection was resolved.
type SelectionRecord struct {
	InputName     string `json:"inputName" redact:"-"`
	SourceNode    string `json:"sourceNode" redact:"-"`
	SourceField   string `json:"sourceField" redact:"-"`
	SourceSize    int    `json:"sourceSize"`
	FilterExpr    string `json:"filterExpr,omitempty"`
	FilteredSize  int    `json:"filteredSize"`
	Strategy      string `json:"strategy" redact:"-"`
	SelectedIndex int    `json:"selectedIndex"`
	SelectionName string `json:"selectionName,omitempty" redact:"-"`
	Field         string `json:"field,omitempty" redact:"-"`
	// SortField, SortValue, and Ties describe a min or max pick: the field
	// compared, the chosen value, and how many elements share it, the chosen
	// one included.
	SortField string   `json:"sortField,omitempty" redact:"-"`
	SortValue *float64 `json:"sortValue,omitempty"`
	Ties      int      `json:"ties,omitempty"`
	OnTie     string   `json:"onTie,omitempty" redact:"-"`
}

// ErrorClassRecord captures the error classification for a failed step.
type ErrorClassRecord struct {
	Category     string `json:"category" redact:"-"`
	Detail       string `json:"detail"`
	Action       string `json:"action" redact:"-"`
	RetryAttempt int    `json:"retryAttempt"`
}

// ValueResolutionRecord captures how a single input was resolved.
type ValueResolutionRecord struct {
	InputName    string `json:"inputName" redact:"-"`
	Source       string `json:"source" redact:"-"`
	RawValue     any    `json:"rawValue,omitempty"`
	FinalValue   any    `json:"finalValue,omitempty"`
	FromStep     string `json:"fromStep,omitempty" redact:"-"`
	FromOutput   string `json:"fromOutput,omitempty" redact:"-"`
	FromInput    string `json:"fromInput,omitempty" redact:"-"`
	Expression   string `json:"expression,omitempty"`
	Constraint   string `json:"constraint,omitempty"`
	ConstraintOK *bool  `json:"constraintOk,omitempty"`
	PoolIndex    int    `json:"poolIndex,omitempty"`
	PoolSize     int    `json:"poolSize,omitempty"`
	// PoolRef names the domain pool the pool came from, when poolRef named one.
	PoolRef string `json:"poolRef,omitempty" redact:"-"`
	Tried   []any  `json:"tried,omitempty"`
	// Error, for source "error", says why the input couldn't be resolved. For a
	// named selection that failed, InputName is the selection's name.
	Error string `json:"error,omitempty"`
	// Layer names the layer that set the value, when a layer did.
	Layer string `json:"layer,omitempty" redact:"-"`
}

// RunSummary is a lightweight summary of a run, written alongside the full
// archive as summary.json to avoid reading multi-megabyte archives for listing.
type RunSummary struct {
	RunID         string         `json:"runId"`
	Timestamp     time.Time      `json:"timestamp"`
	Outcome       string         `json:"outcome"`
	StepCount     int            `json:"stepCount"`
	PassedCount   int            `json:"passedCount"`
	FailedCount   int            `json:"failedCount"`
	DurationMs    int64          `json:"durationMs"`
	PlanName      string         `json:"planName,omitempty"`
	Attempt       int            `json:"attempt,omitempty"`
	TotalAttempts int            `json:"totalAttempts,omitempty"`
	Layers        []string       `json:"layers,omitempty"`
	Issues        map[string]int `json:"issues,omitempty"`
	// OAS is the run's OpenAPI validation, recorded whenever the archive
	// records a validation mode, a clean run included.
	OAS *OASSummary `json:"oas,omitempty"`
	// Seed replays the run's pool picks and random selections with
	// aat run plan --seed.
	Seed uint64 `json:"seed,omitempty"`
	// Fuzz counts the run's fuzz cases by finding, when it had any.
	Fuzz *FuzzSummary `json:"fuzz,omitempty"`
}

// FuzzSummary counts a run's fuzz cases in summary.json.
type FuzzSummary struct {
	Cases int `json:"cases"`
	// Findings counts the cases by finding; "ok" counts those whose response
	// was what they called for.
	Findings map[string]int `json:"findings"`
	// Failing counts the cases whose finding failed the run.
	Failing int `json:"failing"`
}

// OASSummary is a run's OpenAPI validation in summary.json: the mode, and
// counts over the run's main, verification, and cleanup steps.
type OASSummary struct {
	// Mode is auto, strict, or off, as the archive's metadata records it.
	Mode string `json:"mode"`
	// ValidatedRequests and ValidatedResponses count the request and response
	// bodies checked against the spec. A body the validator skipped, such as
	// one of a type it doesn't read, isn't counted.
	ValidatedRequests  int `json:"validatedRequests"`
	ValidatedResponses int `json:"validatedResponses"`
	// Violations counts the validation errors found, as issues.oas does.
	Violations int `json:"violations"`
}

// ArchiveResult captures the overall outcome of a run.
type ArchiveResult struct {
	Outcome string `json:"outcome" redact:"-"`
	Error   string `json:"error,omitempty"`
	// KnownIssues counts the failures an entry kept out of Outcome. When it is
	// non-zero, a passed run still contains a failed step.
	KnownIssues int `json:"knownIssues,omitempty"`
	// EndedEarly is true when a covered failure stopped the run before its
	// last step: it passed, but it did not test everything.
	EndedEarly bool `json:"endedEarly,omitempty"`
	// DurationMs is the run's wall-clock time, retry waits and cleanup
	// included. Archives written before it was recorded omit it; see
	// RunDurationMs.
	DurationMs int64 `json:"durationMs,omitempty"`
}

// BatchArchive is the top-level JSON structure for a batch run (batch.json).
type BatchArchive struct {
	Metadata BatchMetadata   `json:"metadata"`
	Runs     []BatchRunEntry `json:"runs"`
	Result   BatchResult     `json:"result"`
}

// BatchMetadata captures provenance and context for a batch run.
type BatchMetadata struct {
	Version     string     `json:"version" redact:"-"`
	BatchID     string     `json:"batchId" redact:"-"`
	Timestamp   time.Time  `json:"timestamp"`
	Source      string     `json:"source,omitempty" redact:"-"` // directory filter or "all"
	ToolVersion string     `json:"toolVersion,omitempty" redact:"-"`
	Layers      []string   `json:"layers,omitempty" redact:"-"`      // CLI layers applied at the batch level
	LayerGroups [][]string `json:"layerGroups,omitempty" redact:"-"` // layer groups for permutation
}

// BatchRunEntry is a summary of a single run within a batch.
type BatchRunEntry struct {
	PlanName    string         `json:"planName" redact:"-"`
	RunID       string         `json:"runId" redact:"-"`
	Outcome     string         `json:"outcome" redact:"-"`
	StepCount   int            `json:"stepCount"`
	PassedCount int            `json:"passedCount"`
	FailedCount int            `json:"failedCount"`
	DurationMs  int64          `json:"durationMs"`
	Error       string         `json:"error,omitempty"`
	Attempts    int            `json:"attempts,omitempty"`               // total attempts (omitted if 1)
	Layers      []string       `json:"layers,omitempty" redact:"-"`      // effective layers for this run
	Permutation string         `json:"permutation,omitempty" redact:"-"` // permutation label for grouping
	Skipped     bool           `json:"skipped,omitempty"`                // true if this run was skipped as a duplicate
	DuplicateOf string         `json:"duplicateOf,omitempty" redact:"-"` // display name of canonical run (when skipped)
	Issues      map[string]int `json:"issues,omitempty"`
}

// BatchResult captures the aggregate outcome of a batch.
type BatchResult struct {
	Outcome         string `json:"outcome" redact:"-"` // passed, failed, error, aborted
	TotalRuns       int    `json:"totalRuns"`
	PassedRuns      int    `json:"passedRuns"`
	FailedRuns      int    `json:"failedRuns"`
	ErrorRuns       int    `json:"errorRuns"`
	AbortedRuns     int    `json:"abortedRuns,omitempty"`
	SkippedRuns     int    `json:"skippedRuns,omitempty"`
	TotalDurationMs int64  `json:"totalDurationMs"`
}
