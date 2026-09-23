package engine

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/fuzz"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/graph/oas"
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
	// JudgedAs is the mode the response was judged by: the case's own, or
	// negative when the OpenAPI spec refuses the request the case built,
	// since the spec is the API's own word on what it accepts.
	JudgedAs string
	// SpecViolations are the ways the request broke the OpenAPI spec. On a
	// fuzz step they are the point, not a warning, so they are kept here
	// rather than in the step's OAS validation.
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
// the pinned ones, with the CLI's settings over the block's.
func (e *Engine) expandFuzz(p *plan.Plan, seed uint64) error {
	cli := e.fuzz
	e.fuzzJudge = map[string]fuzzJudging{}
	e.fuzzRun = newFuzzRunState()

	type target struct {
		step    plan.Step
		fromCLI bool
	}
	var targets []target
	matched := map[string]bool{}
	for _, s := range p.Execution.Steps {
		if s.Fuzz != nil || s.FuzzSetup != "" {
			continue
		}
		fromCLI := false
		if cli != nil {
			for _, t := range cli.Targets {
				// A node name takes the plan's own steps of that node, not the
				// mutation siblings and isolated clones instantiation made.
				if s.StepID() == t || s.Node == t && s.VariantOf == "" {
					fromCLI = true
					matched[t] = true
				}
			}
		}
		if fromCLI || s.FuzzSettings != nil {
			targets = append(targets, target{step: s, fromCLI: fromCLI})
		}
	}
	if cli != nil && !cli.AllowNoTarget {
		for _, t := range cli.Targets {
			if !matched[t] {
				return fmt.Errorf("--fuzz %s: the plan has no step with that ID and no step of that node", t)
			}
		}
	}

	found := map[string]bool{}
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
		opts := fuzz.Options{Modes: block.Mode, Inputs: block.Inputs, Skip: block.Skip, Max: block.Cases, Seed: seed, Now: time.Now()}
		only, scope, fail := block.Only, block.Scope, block.Fail
		if tg.fromCLI {
			if len(cli.Modes) > 0 {
				opts.Modes = cli.Modes
			}
			if len(cli.Inputs) > 0 {
				opts.Inputs = cli.Inputs
			}
			if cli.Max > 0 {
				opts.Max = cli.Max
			}
			if len(cli.Cases) > 0 {
				only = cli.Cases
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
		if tg.fromCLI || block.Generates() {
			opts.Only = only
			tmpl, _ := e.registry.GetTemplate(node.Adapter)
			generated, capped, err := fuzz.GenerateCapped(fuzz.Target{Step: step, Node: node, KB: e.KB, Template: tmpl}, opts)
			if err != nil {
				return fmt.Errorf("fuzz target %s: %w", step.StepID(), err)
			}
			e.fuzzCapped = e.fuzzCapped || capped
			cases = generated
		}
		// A pinned case replaces a generated one with its ID.
		for _, pc := range block.Pinned {
			c, err := e.pinnedCase(step, node, pc)
			if err != nil {
				return err
			}
			cases = slices.DeleteFunc(cases, func(g plan.FuzzCase) bool { return g.ID == pc.ID })
			if len(only) == 0 || slices.Contains(only, pc.ID) {
				cases = append(cases, c)
			}
		}
		// --fuzz-case names cases of the steps --fuzz names.
		if tg.fromCLI {
			for _, c := range cases {
				found[c.ID] = true
			}
		}
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
		for _, c := range cases {
			e.fuzzRun.caseTarget[plan.FuzzStepID(step.StepID(), c.ID)] = step.StepID()
		}
		e.fuzzJudge[step.StepID()] = fuzzJudging{fail: fail, accept: block.Accept}
	}
	if cli != nil && len(matched) > 0 {
		for _, id := range cli.Cases {
			if !found[id] {
				return fmt.Errorf("--fuzz-case %s: no target step has a case with that ID", id)
			}
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
// a success whose body the graph's error detection reads as an error.
func refused(r *StepResult) bool {
	return r.Response != nil && (r.StatusCode >= 400 && r.StatusCode < 500 || r.StatusCode < 400 && r.ResponseBodyError != nil)
}

// notSent reports whether a request got no response because it was never
// sent: aat could not build it, or its setup failed.
func notSent(r *StepResult) bool {
	return r.Response == nil && (r.Request == nil || errors.As(r.Error, new(*adapter.NotSentError)))
}

// judgeFuzz decides a fuzz step's finding from its result.
func (e *Engine) judgeFuzz(step plan.Step, node *graph.Node, r *StepResult) *FuzzResult {
	c := step.Fuzz
	mode := c.Mode
	var violations []string
	if v := r.OASValidation; v != nil && v.Request != nil && !v.Request.Skipped {
		for _, se := range v.Request.Errors {
			if se.Path != "" {
				violations = append(violations, se.Path+": "+se.Message)
			} else {
				violations = append(violations, se.Message)
			}
		}
		if !v.Request.Valid {
			mode = plan.FuzzNegative
		}
		cp := *v
		cp.Request = nil
		r.OASValidation = &cp
	}

	// A status the operation doesn't list has no schema to break, so the
	// response validator's complaint about it is this finding, not a
	// schema violation.
	undocumented := false
	if e.oasCache != nil && r.Response != nil && grpcStatusName(r.Response) == "" {
		documented, known := oas.StatusDocumented(node, e.graphOAS, e.oasCache, r.StatusCode)
		undocumented = known && !documented
	}

	finding := ""
	// Anything with a response is judged on it: an error after one, such as
	// outputs that couldn't be read, says nothing about whether it came.
	switch {
	case notSent(r):
		finding = FindingNotSent
	case r.Response == nil:
		finding = FindingNoResponse
	case r.StatusCode >= 500:
		finding = FindingServerError
	case undocumented:
		finding = FindingUndocumentedStatus
	case r.OASValidation != nil && r.OASValidation.Response != nil && !r.OASValidation.Response.Valid && !r.OASValidation.Response.Skipped:
		finding = FindingSchemaViolation
	case mode == plan.FuzzNegative && !refused(r):
		finding = FindingAcceptedInvalid
	case mode == plan.FuzzPositive && refused(r):
		finding = FindingRejectedValid
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
	warnings   []string
}

func newFuzzRunState() *fuzzRunState {
	return &fuzzRunState{groups: map[string]*fuzzGroup{}, caseTarget: map[string]string{}, caseSetup: map[string]string{}}
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
// API refused the request or it was never sent, since a refusal changes
// nothing, or when the target only reads, and changed otherwise, so the next
// case sets up afresh.
func (f *fuzzRunState) caseJudged(step plan.Step, r *StepResult) {
	caseID := step.StepID()
	g := f.groups[f.caseTarget[caseID]]
	if g == nil || f.caseSetup[caseID] == SetupFailed {
		return
	}
	g.failures = 0
	g.building = ""
	g.clean = g.readOnly || refused(r) || notSent(r)
}
