package engine

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/fuzz"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/graph/oas"
	"github.com/gburgyan/aat/internal/grpcstatus"
	"github.com/gburgyan/aat/plan"
)

// Fuzz findings: what a fuzz step's response says about the API. An empty
// finding means the response was what the case called for. See the plan
// package for the list.
const (
	// FindingServerError is a 5xx: the API failed on the value.
	FindingServerError = plan.FindingServerError
	// FindingNoResponse is a request that was sent and got no response, such
	// as a timeout or a dropped connection.
	FindingNoResponse = plan.FindingNoResponse
	// FindingSchemaViolation is a response that breaks the OpenAPI spec.
	FindingSchemaViolation = plan.FindingSchemaViolation
	// FindingAcceptedInvalid is a success for a value the input forbids.
	FindingAcceptedInvalid = plan.FindingAcceptedInvalid
	// FindingRejectedValid is a 4xx for a value the input allows.
	FindingRejectedValid = plan.FindingRejectedValid
	// FindingUndocumentedStatus is a status the node's OpenAPI operation does
	// not list among its responses.
	FindingUndocumentedStatus = plan.FindingUndocumentedStatus
	// FindingThrottled is a 429, or a gRPC RESOURCE_EXHAUSTED, after the
	// retries the target's retry block allows: the API turned the request away
	// before judging the value, so the case says nothing about it.
	FindingThrottled = plan.FindingThrottled
	// FindingNotSent is a case that could not be sent: its copy of a setup
	// step failed, or aat could not build the request. It says nothing about
	// the API.
	FindingNotSent = plan.FindingNotSent
)

// AllFindings lists the findings, most serious first.
var AllFindings = plan.FuzzFindings

// DefaultFuzzFail lists the findings that fail a run unless FuzzConfig.Fail
// says otherwise. The others are reported as warnings: whether an API should
// accept an unusual value is a judgement the graph does not record.
var DefaultFuzzFail = []string{FindingServerError, FindingNoResponse, FindingSchemaViolation}

// FuzzConfig asks Run to fuzz steps of the plan.
type FuzzConfig struct {
	// Targets names the steps to fuzz, by step ID or by node; a node name
	// takes every step of that node.
	Targets []string
	// AllowNoTarget lets a plan without any target run unfuzzed, as a batch
	// needs; otherwise a target that matches nothing is an error.
	AllowNoTarget bool
	Modes         []string // see fuzz.Options
	Inputs        []string // see fuzz.Options
	Max           int      // cases per target step; 0 means all
	// Scope is one of the plan.FuzzScope constants: "reuse", where a target's
	// cases share a copy of the steps it depends on until a case may have
	// changed it; "isolated", where each case gets its own copy; or "shared",
	// where every case runs on the target's own, which is cheapest but lets a
	// case change what later steps see. Empty leaves it to the step's fuzz:
	// block, or reuse.
	Scope string
	// Cases limits the run to the cases with these IDs.
	Cases []string
	// Fail lists the findings that fail the run; nil means DefaultFuzzFail.
	Fail []string
	// Matched, when set, collects what Targets, Inputs, and Cases matched in
	// every run given this configuration, so a batch can check them across
	// its plans with Unmatched.
	Matched *FuzzMatches
}

// FuzzMatches is what a FuzzConfig's names matched: the targets, inputs, and
// case IDs that named something in a run. It is safe to share between runs
// that run at once.
type FuzzMatches struct {
	mu                     sync.Mutex
	targets, inputs, cases map[string]bool
}

func newFuzzMatches() *FuzzMatches {
	return &FuzzMatches{targets: map[string]bool{}, inputs: map[string]bool{}, cases: map[string]bool{}}
}

// add records what a run matched.
func (m *FuzzMatches) add(run *FuzzMatches) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.targets == nil {
		m.targets, m.inputs, m.cases = map[string]bool{}, map[string]bool{}, map[string]bool{}
	}
	for _, pair := range []struct{ to, from map[string]bool }{{m.targets, run.targets}, {m.inputs, run.inputs}, {m.cases, run.cases}} {
		for name := range pair.from {
			pair.to[name] = true
		}
	}
}

// Unmatched returns an error naming each of the configuration's targets,
// inputs, and cases that no run given it matched, or nil when every one
// matched in some run. Without Matched it returns nil.
func (c *FuzzConfig) Unmatched() error {
	if c == nil || c.Matched == nil {
		return nil
	}
	c.Matched.mu.Lock()
	defer c.Matched.mu.Unlock()
	if c.Matched.targets == nil {
		return c.unmatched(newFuzzMatches())
	}
	return c.unmatched(c.Matched)
}

// unmatched says which of the configuration's names matched nothing in m.
// The cases are only checked once every target matched, since a target that
// matched nothing has none.
func (c *FuzzConfig) unmatched(m *FuzzMatches) error {
	var errs []error
	for _, t := range c.Targets {
		if !m.targets[t] {
			errs = append(errs, fmt.Errorf("--fuzz %s: no step has that ID, and no step of that node is meant to succeed", t))
		}
	}
	for _, in := range c.Inputs {
		if !m.inputs[in] {
			errs = append(errs, fmt.Errorf("--fuzz-input %s: no step --fuzz names has that input", in))
		}
	}
	if len(errs) == 0 {
		for _, id := range c.Cases {
			if !m.cases[id] {
				errs = append(errs, fmt.Errorf("--fuzz-case %s: no step --fuzz names has a case with that ID", id))
			}
		}
	}
	return errors.Join(errs...)
}

// WithFuzz fuzzes the steps cfg names: each gets a sibling step per generated
// case, which the engine judges by FuzzResult rather than by its status.
func (e *Engine) WithFuzz(cfg *FuzzConfig) *Engine {
	e.fuzz = cfg
	return e
}

// FuzzResult is how a fuzz step's response was judged.
type FuzzResult struct {
	Case plan.FuzzCase
	// JudgedAs is the mode the response was judged by: the case's own; or
	// negative when the OpenAPI spec refuses the request the case built for a
	// reason the target's own request didn't give it, since the spec is the
	// API's own word on what it accepts; or edge for a positive value the
	// step's constraint rules out.
	JudgedAs string
	// SpecViolations are the ways the request broke the OpenAPI spec that
	// the target's own request did not. On a fuzz step they are the point,
	// not a warning, so they are kept here rather than in the step's OAS
	// validation.
	SpecViolations []string
	// Finding is one of the Finding constants, or empty when the response was
	// what the case called for.
	Finding string
	// Fails is true when the finding fails the run.
	Fails bool
	// Setup says how the steps the case ran on came to be: SetupFresh,
	// SetupReused, SetupFailed, or empty when it ran on the happy path's own.
	Setup string
}

// fuzzJudging is how a target's cases are judged: the findings that fail the
// run, and the statuses no case is faulted for.
type fuzzJudging struct {
	fail   []string
	accept plan.ExpectedStatuses
}

// expandFuzz adds the fuzz cases to an instantiated plan: for each step the
// CLI's FuzzConfig names or that has a fuzz: block, the generated cases and
// the pinned ones, with the CLI's settings over the block's. The modes,
// inputs, skip list, case IDs, and cap apply to pinned cases as they do to
// generated ones.
func (e *Engine) expandFuzz(p *plan.Plan, seed uint64) error {
	cli := e.fuzz
	e.fuzzJudge = map[string]fuzzJudging{}
	e.fuzzRun = newFuzzRunState()

	type target struct {
		step    plan.Step
		fromCLI bool
	}
	var targets []target
	matched := newFuzzMatches()
	for _, s := range p.Execution.Steps {
		if s.Fuzz != nil || s.FuzzSetup != "" {
			continue
		}
		fromCLI := false
		if cli != nil {
			for _, t := range cli.Targets {
				switch {
				case s.StepID() == t:
					if why := s.Unfuzzable(); why != "" {
						return fmt.Errorf("--fuzz %s: the step %s", t, why)
					}
				case s.Node == t && s.VariantOf == "" && s.Unfuzzable() == "":
					// A node name takes the plan's own steps of that node that
					// are meant to succeed: not the mutation siblings and
					// isolated clones instantiation made, nor a negative step
					// written for one bad request.
				default:
					continue
				}
				fromCLI = true
				matched.targets[t] = true
			}
		}
		if fromCLI || s.FuzzSettings != nil {
			targets = append(targets, target{step: s, fromCLI: fromCLI})
		}
	}

	for _, tg := range targets {
		step := tg.step
		node := e.graph.Nodes[step.Node]
		if node == nil {
			return fmt.Errorf("fuzz target %s: node %q not found in graph", step.StepID(), step.Node)
		}
		block := step.FuzzSettings
		if block == nil {
			block = &plan.FuzzSettings{}
		}
		opts := fuzz.Options{Modes: block.Mode, Inputs: block.Inputs, Skip: block.Skip, Only: block.Only, Seed: seed, Now: time.Now()}
		limit, scope, fail := block.Cases, block.Scope, block.Fail
		generate := block.Generates()
		if tg.fromCLI {
			generate = true
			if len(cli.Modes) > 0 {
				opts.Modes = cli.Modes
			}
			if len(cli.Inputs) > 0 {
				// --fuzz-input names inputs of any target: each target takes
				// those its node has.
				opts.Inputs = nil
				for _, in := range cli.Inputs {
					if slices.ContainsFunc(node.Inputs, func(ni graph.Input) bool { return ni.Name == in }) {
						opts.Inputs = append(opts.Inputs, in)
						matched.inputs[in] = true
					}
				}
				generate = len(opts.Inputs) > 0
			}
			if cli.Max > 0 {
				limit = cli.Max
			}
			if len(cli.Cases) > 0 {
				opts.Only = cli.Cases
			}
			if cli.Scope != "" {
				scope = cli.Scope
			}
			if cli.Fail != nil {
				fail = cli.Fail
			}
		}
		if fail == nil {
			fail = DefaultFuzzFail
		}

		var cases []plan.FuzzCase
		if generate {
			tmpl, _ := e.registry.GetTemplate(node.Adapter)
			generated, err := fuzz.Generate(fuzz.Target{Step: step, Node: node, KB: e.KB, Template: tmpl}, opts)
			if err != nil {
				return fmt.Errorf("fuzz target %s: %w", step.StepID(), err)
			}
			cases = generated
		}
		// A pinned case replaces a generated one with its ID.
		var pinned []plan.FuzzCase
		for _, pc := range block.Pinned {
			c, err := e.pinnedCase(step, node, pc)
			if err != nil {
				return err
			}
			pinned = append(pinned, c)
		}
		if tg.fromCLI && len(cli.Inputs) > 0 && len(opts.Inputs) == 0 {
			pinned = nil // none of the named inputs is this target's
		}
		for _, c := range fuzz.Select(pinned, opts) {
			cases = slices.DeleteFunc(cases, func(g plan.FuzzCase) bool { return g.ID == c.ID })
			cases = append(cases, c)
		}
		for _, c := range cases {
			if tg.fromCLI {
				matched.cases[c.ID] = true // --fuzz-case names cases of the steps --fuzz names
			}
		}
		// A block's only: list is checked against the cases the step has,
		// before the cap picks among them; --fuzz-case is checked across
		// every target, below.
		if !tg.fromCLI || len(cli.Cases) == 0 {
			for _, id := range block.Only {
				if !slices.ContainsFunc(cases, func(c plan.FuzzCase) bool { return c.ID == id }) {
					return fmt.Errorf("fuzz target %s: only: the step has no case %q", step.StepID(), id)
				}
			}
		}
		var capped bool
		cases, capped = fuzz.Cap(cases, limit, seed)
		e.fuzzCapped = e.fuzzCapped || capped

		if scope == "" {
			scope = plan.FuzzScopeReuse
		}
		if scope == plan.FuzzScopeShared && len(cases) > 0 && !e.readOnlyStep(step) {
			e.fuzzRun.warnings = append(e.fuzzRun.warnings, fmt.Sprintf(
				"fuzz scope shared on %s: each case the API accepts changes the state the rest of the plan sees; reuse or isolated keeps it apart",
				step.StepID()))
		}
		if err := plan.ExpandFuzzCases(p, step.StepID(), cases, plan.FuzzExpandOptions{Scope: scope, ReadOnly: e.readOnlyStep}); err != nil {
			return err
		}
		e.fuzzRun.groups[step.StepID()] = &fuzzGroup{scope: scope, readOnly: e.readOnlyStep(step), live: map[string]string{}}
		e.fuzzJudge[step.StepID()] = fuzzJudging{fail: fail, accept: block.Accept}
		e.fuzzRun.settings[step.StepID()] = plan.FuzzSettings{Scope: scope, Fail: fail, Accept: block.Accept}
		for name, sv := range step.Values {
			if sv.Constraint != "" {
				if e.fuzzRun.constraints[step.StepID()] == nil {
					e.fuzzRun.constraints[step.StepID()] = map[string]string{}
				}
				e.fuzzRun.constraints[step.StepID()][name] = sv.Constraint
			}
		}
	}

	if cli != nil {
		e.fuzzRun.matched = matched
		if cli.Matched != nil {
			cli.Matched.add(matched)
		}
		if !cli.AllowNoTarget {
			if err := cli.unmatched(matched); err != nil {
				return err
			}
		}
	}

	// Each case's steps are its own, and it retries a rate limit as its target
	// would.
	retries := map[string]*plan.RetryConfig{}
	for _, tg := range targets {
		retries[tg.step.StepID()] = throttleRetry(tg.step.Retry)
	}
	for i := range p.Execution.Steps {
		s := &p.Execution.Steps[i]
		switch {
		case s.Fuzz != nil:
			e.fuzzRun.caseTarget[s.StepID()] = s.Fuzz.Target
			e.fuzzRun.owner[s.StepID()] = s.StepID()
			s.Retry = retries[s.Fuzz.Target]
		case s.FuzzSetup != "":
			e.fuzzRun.owner[s.StepID()] = s.FuzzSetup
		}
	}
	return nil
}

// planFuzzes reports whether any step of the plan has a fuzz: block.
func planFuzzes(p *plan.Plan) bool {
	for _, s := range p.Execution.Steps {
		if s.FuzzSettings != nil {
			return true
		}
	}
	return false
}

// pinnedCase returns a pinned case as the fuzzer sends it. A pinned null for
// an input is sent as the generator sends one: the body fields the input
// fills are set to null, since an input resolved to nothing is left out of
// the request instead.
func (e *Engine) pinnedCase(step plan.Step, node *graph.Node, pc plan.PinnedFuzzCase) (plan.FuzzCase, error) {
	c := pc.Case()
	if len(c.Patch) > 0 {
		c.Patch = slices.Clone(c.Patch)
		for i := range c.Patch {
			c.Patch[i].Value = plan.AsWritten(c.Patch[i].Value)
		}
	}
	if c.Value != nil || len(c.Patch) > 0 {
		return c, nil
	}
	if tmpl, ok := e.registry.GetTemplate(node.Adapter); ok {
		fields, _ := tmpl.RequestFields()
		for _, f := range fields {
			if f.Input == c.Input && f.Where == adapter.FieldBody {
				c.Patch = append(c.Patch, plan.RequestPatch{Where: f.Where, Path: f.Path, Op: adapter.PatchSet})
			}
		}
	}
	if len(c.Patch) == 0 {
		return c, fmt.Errorf("fuzz target %s: pinned case %s sets %s to null, but the template sends it in no body field; give a patch instead",
			step.StepID(), c.ID, c.Input)
	}
	return c, nil
}

// refused reports whether the API refused a request it answered: a 4xx, or
// a success whose body the graph's error detection reads as an error. A
// throttled request was turned away before its value was looked at, so it is
// not a refusal.
func refused(r *StepResult) bool {
	return r.Response != nil && !throttled(r) && (r.StatusCode >= 400 && r.StatusCode < 500 || r.StatusCode < 400 && r.ResponseBodyError != nil)
}

// throttled reports whether the API answered with a rate limit: an HTTP 429,
// or a gRPC RESOURCE_EXHAUSTED, which maps to it.
func throttled(r *StepResult) bool {
	return r.Response != nil && r.StatusCode == http.StatusTooManyRequests
}

// throttleRetry is the retry block a target's cases get: the target's own,
// cut down to retrying a rate limit, so a case the API throttles is sent again
// as the happy path would be, and nothing else a case finds is retried away.
// It is nil when the target's block would not retry a 429.
func throttleRetry(target *plan.RetryConfig) *plan.RetryConfig {
	if !shouldRetry(CategoryTransient, http.StatusTooManyRequests, "", target, 1) &&
		!shouldRetry(CategoryTransient, http.StatusTooManyRequests, grpcstatus.Name(grpcstatus.ResourceExhausted), target, 1) {
		return nil
	}
	return &plan.RetryConfig{Max: target.Max, On: []string{strconv.Itoa(http.StatusTooManyRequests)}}
}

// notSent reports whether a request got no response because it was never
// sent: aat could not build it, or its setup failed.
func notSent(r *StepResult) bool {
	return r.Response == nil && (r.Request == nil || errors.As(r.Error, new(*adapter.NotSentError)))
}

// requestViolations lists the ways a request broke the OpenAPI spec, as
// "path: message", or nil when it was not checked.
func requestViolations(v *oas.ValidationResult) []string {
	if v == nil || v.Request == nil || v.Request.Skipped {
		return nil
	}
	var violations []string
	for _, se := range v.Request.Errors {
		if se.Path != "" {
			violations = append(violations, se.Path+": "+se.Message)
		} else {
			violations = append(violations, se.Message)
		}
	}
	return violations
}

// judgeFuzz decides a fuzz step's finding from its result.
func (e *Engine) judgeFuzz(step plan.Step, node *graph.Node, r *StepResult) *FuzzResult {
	c := step.Fuzz
	mode := c.Mode
	var violations []string
	if v := r.OASValidation; v != nil && v.Request != nil {
		// A violation the target's own request had too is the spec's quarrel
		// with the happy path, not something the case did, so it neither makes
		// the case negative nor is reported as the case's.
		inherited := e.fuzzRun.inherited(c.Target)
		for _, violation := range requestViolations(v) {
			if !slices.Contains(inherited, violation) {
				violations = append(violations, violation)
			}
		}
		if len(violations) > 0 {
			mode = plan.FuzzNegative
		}
		cp := *v
		cp.Request = nil
		r.OASValidation = &cp
	}
	// A positive value the step's constraint rules out, such as a destination
	// equal to the origin, is not known to be allowed: the plan would never
	// send it.
	if mode == plan.FuzzPositive && e.fuzzRun.breaksConstraint(c, r.Inputs) {
		mode = plan.FuzzEdge
	}

	// A status the operation doesn't list has no schema to break, so the
	// response validator's complaint about it is that finding, not a schema
	// violation.
	undocumented := false
	if e.oasCache != nil && r.Response != nil && grpcStatusName(r.Response) == "" {
		documented, known := oas.StatusDocumented(node, e.graphOAS, e.oasCache, r.StatusCode)
		undocumented = known && !documented
	}

	finding := ""
	// Anything with a response is judged on it: an error after one, such as
	// outputs that couldn't be read, says nothing about whether it came. The
	// findings are tried most serious first, so a status the spec doesn't list
	// never hides a value the API should not have taken.
	switch {
	case notSent(r):
		finding = FindingNotSent
	case r.Response == nil:
		finding = FindingNoResponse
	case r.StatusCode >= 500:
		finding = FindingServerError
	case throttled(r):
		finding = FindingThrottled
	case !undocumented && r.OASValidation != nil && r.OASValidation.Response != nil && !r.OASValidation.Response.Valid && !r.OASValidation.Response.Skipped:
		finding = FindingSchemaViolation
	case mode == plan.FuzzNegative && !refused(r):
		finding = FindingAcceptedInvalid
	case mode == plan.FuzzPositive && refused(r):
		finding = FindingRejectedValid
	case undocumented:
		finding = FindingUndocumentedStatus
	}
	judging, ok := e.fuzzJudge[c.Target]
	if !ok {
		judging.fail = DefaultFuzzFail
	}
	fail := judging.fail
	// A status the block accepts is never a judgement call against the API.
	if r.Response != nil && judging.accept.Matches(r.StatusCode, grpcStatusName(r.Response)) {
		switch finding {
		case FindingAcceptedInvalid, FindingRejectedValid, FindingUndocumentedStatus:
			finding = ""
		}
	}
	return &FuzzResult{Case: *c, JudgedAs: mode, SpecViolations: violations, Finding: finding, Fails: finding != "" && slices.Contains(fail, finding)}
}

// ParseFindings reads a comma-separated list of findings, as --fuzz-fail
// takes it.
func ParseFindings(list []string) ([]string, error) {
	out := []string{}
	for _, f := range list {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !slices.Contains(AllFindings, f) {
			return nil, fmt.Errorf("unknown finding %q: use %s", f, strings.Join(AllFindings, ", "))
		}
		out = append(out, f)
	}
	return out, nil
}

// fuzzFailureError says which findings failed the run, or returns nil when
// none did.
func fuzzFailureError(steps []StepResult) error {
	counts := map[string]int{}
	for _, s := range steps {
		if s.Fuzz != nil && s.Fuzz.Fails {
			counts[s.Fuzz.Finding]++
		}
	}
	if len(counts) == 0 {
		return nil
	}
	var parts []string
	for _, f := range AllFindings {
		if n := counts[f]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, f))
		}
	}
	return fmt.Errorf("fuzzing found %s", strings.Join(parts, ", "))
}

// SummarizeFuzz counts the fuzz cases among steps, as the archive's summary
// counts them, or returns nil when there are none.
func SummarizeFuzz(steps []StepResult) *archive.FuzzSummary {
	var s *archive.FuzzSummary
	for _, r := range steps {
		if r.Fuzz == nil {
			continue
		}
		if s == nil {
			s = &archive.FuzzSummary{}
		}
		s.Add(r.Fuzz.Finding, r.Fuzz.Setup, r.Fuzz.Fails)
	}
	return s
}

// DescribeFuzz says how a run's fuzz cases came out, as every report of a run
// says it: "50 cases: 47 as expected, 1 server-error, 2 accepted-invalid (1
// failing) · setup: 4 fresh, 46 reused".
func DescribeFuzz(s *archive.FuzzSummary) string {
	var parts []string
	if n := s.Findings[archive.FindingOK]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d as expected", n))
	}
	for _, f := range AllFindings {
		if n := s.Findings[f]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, f))
		}
	}
	text := fmt.Sprintf("%d case", s.Cases)
	if s.Cases != 1 {
		text += "s"
	}
	text += ": " + strings.Join(parts, ", ")
	if s.Failing > 0 {
		text += fmt.Sprintf(" (%d failing)", s.Failing)
	}
	var setup []string
	for _, k := range []string{SetupFresh, SetupReused, SetupFailed} {
		if n := s.Setup[k]; n > 0 {
			setup = append(setup, fmt.Sprintf("%d %s", n, k))
		}
	}
	if len(setup) > 0 {
		text += " · setup: " + strings.Join(setup, ", ")
	}
	return text
}

// Setup of a fuzz case: how the steps it runs on came to be.
const (
	// SetupFresh is a case whose copy of the setup was sent for it.
	SetupFresh = "fresh"
	// SetupReused is a case that ran on the copy an earlier case left clean.
	SetupReused = "reused"
	// SetupFailed is a case whose copy of the setup failed, or that wasn't
	// tried because the target's setup kept failing.
	SetupFailed = "failed"
)

// maxSetupFailures is how many setups in a row may fail before a target's
// remaining cases are not sent: an API that is refusing the setup, often
// for a rate limit, is not asked again and again.
const maxSetupFailures = 3

// fuzzGroup is a fuzz target's setup during Run.
type fuzzGroup struct {
	scope string
	// readOnly is true when the target only reads, so no case can change
	// its setup.
	readOnly bool
	// live maps a setup step's original ID to the copy the next case may
	// reuse, while clean.
	live map[string]string
	// clean is true when no case since the live copy was made may have
	// changed what it set up.
	clean bool
	// building is the case whose copy is being sent.
	building string
	// failures counts the setups in a row that failed.
	failures int
	// stopped, once set, says why the target's remaining cases are not sent.
	stopped string
}

// fuzzRunState is what the engine tracks about fuzz cases during Run.
type fuzzRunState struct {
	groups     map[string]*fuzzGroup // by target step ID
	caseTarget map[string]string     // case step ID → target step ID
	caseSetup  map[string]string     // case step ID → a Setup constant
	// owner maps the ID of each fuzz case, and of each copy of a setup step
	// made for one, to the case's step ID.
	owner map[string]string
	// baseline holds each target's own request violations, by its step ID.
	baseline map[string][]string
	// settings holds the scope, fail list, and accepted statuses each
	// target's cases ran with, by its step ID.
	settings map[string]plan.FuzzSettings
	// constraints holds each target's value constraints, by its step ID and
	// then input.
	constraints map[string]map[string]string
	// matched is what the CLI's names matched, or nil without them.
	matched  *FuzzMatches
	warnings []string
}

func newFuzzRunState() *fuzzRunState {
	return &fuzzRunState{groups: map[string]*fuzzGroup{}, caseTarget: map[string]string{}, caseSetup: map[string]string{},
		owner: map[string]string{}, baseline: map[string][]string{}, settings: map[string]plan.FuzzSettings{},
		constraints: map[string]map[string]string{}}
}

// breaksConstraint reports whether a case's value fails the constraint its
// target's step value puts on the input, given the inputs the case resolved.
func (f *fuzzRunState) breaksConstraint(c *plan.FuzzCase, inputs map[string]any) bool {
	if f == nil || len(c.Patch) > 0 {
		return false
	}
	constraint := f.constraints[c.Target][c.Input]
	value, ok := inputs[c.Input]
	if constraint == "" || !ok {
		return false
	}
	others := maps.Clone(inputs)
	delete(others, c.Input)
	holds, err := checkConstraint(constraint, value, others)
	return err == nil && !holds
}

// inherited returns the violations of a target's own request, which its
// cases inherit rather than cause.
func (f *fuzzRunState) inherited(target string) []string {
	if f == nil {
		return nil
	}
	return f.baseline[target]
}

// targetRan records a fuzz target's own result, which its cases are judged
// against.
func (f *fuzzRunState) targetRan(step plan.Step, r *StepResult) {
	if f.groups[step.StepID()] != nil {
		f.baseline[step.StepID()] = requestViolations(r.OASValidation)
	}
}

// readOnlyStep reports whether a step only reads: an HTTP GET, HEAD, or
// OPTIONS on a node with no cleanup pairing. A gRPC call is never assumed to
// be one.
func (e *Engine) readOnlyStep(s plan.Step) bool {
	node := e.graph.Nodes[s.Node]
	if node == nil || node.Cleanup.Node != "" {
		return false
	}
	tmpl, ok := e.registry.GetTemplate(node.Adapter)
	if !ok || tmpl.Protocol == adapter.ProtocolGRPC {
		return false
	}
	switch strings.ToUpper(tmpl.Request.Method) {
	case "GET", "HEAD", "OPTIONS":
		return true
	}
	return false
}

// beforeSetupCopy decides what to do with a fuzz setup copy about to run. It
// returns reused when the copy's live counterpart stands in for it, having
// pointed this copy's ID at that counterpart's outputs and inputs, and a
// reason when the case is not to be sent at all.
func (f *fuzzRunState) beforeSetupCopy(step plan.Step, state *RunState) (reused bool, reason string) {
	caseID := step.FuzzSetup
	g := f.groups[f.caseTarget[caseID]]
	if g == nil {
		return false, ""
	}
	if g.stopped != "" {
		f.caseSetup[caseID] = SetupFailed
		return false, g.stopped
	}
	if g.scope == plan.FuzzScopeReuse && g.clean && g.building != caseID {
		if live := g.live[step.FuzzSetupOf]; live != "" {
			if outputs, ok := state.GetAllOutputs(live); ok {
				state.StoreOutputs(step.StepID(), outputs)
			}
			if inputs, ok := state.AllInputs(live); ok {
				state.StoreInputs(step.StepID(), inputs)
			}
			f.caseSetup[caseID] = SetupReused
			return true, ""
		}
	}
	if g.building != caseID {
		// A fresh copy: whatever was live is no longer.
		g.live, g.clean, g.building = map[string]string{}, false, caseID
		f.caseSetup[caseID] = SetupFresh
	}
	return false, ""
}

// setupCopySent records a copy that was sent and passed.
func (f *fuzzRunState) setupCopySent(step plan.Step) {
	if g := f.groups[f.caseTarget[step.FuzzSetup]]; g != nil {
		g.live[step.FuzzSetupOf] = step.StepID()
	}
}

// setupCopyFailed records a copy that failed, and stops the target's cases
// once its setup has failed too often in a row.
func (f *fuzzRunState) setupCopyFailed(step plan.Step) {
	caseID := step.FuzzSetup
	f.caseSetup[caseID] = SetupFailed
	g := f.groups[f.caseTarget[caseID]]
	if g == nil {
		return
	}
	g.clean, g.building = false, ""
	g.failures++
	if g.failures >= maxSetupFailures && g.stopped == "" {
		g.stopped = fmt.Sprintf("the setup for %s failed %d times in a row", f.caseTarget[caseID], g.failures)
	}
}

// caseJudged records how a case's response leaves its setup: clean when the
// API refused or throttled the request or it was never sent, since none of
// those changes anything, or when the target only reads, and changed
// otherwise, so the next case sets up afresh.
func (f *fuzzRunState) caseJudged(step plan.Step, r *StepResult) {
	caseID := step.StepID()
	g := f.groups[f.caseTarget[caseID]]
	if g == nil || f.caseSetup[caseID] == SetupFailed {
		return
	}
	g.failures = 0
	g.building = ""
	g.clean = g.readOnly || refused(r) || throttled(r) || notSent(r)
}
