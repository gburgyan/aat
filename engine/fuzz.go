package engine

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gburgyan/aat/adapter"
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
	// Scope is "isolated", where each case gets its own copy of the steps the
	// target depends on, or "shared", where every case runs on the target's
	// own: faster, but a case that changes state can change what later steps
	// see. Empty leaves it to the step's fuzz: block, or isolated.
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
				if s.StepID() == t || s.Node == t {
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
		opts := fuzz.Options{Modes: block.Mode, Inputs: block.Inputs, Max: block.Cases, Seed: seed, Now: time.Now()}
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
			tmpl, _ := e.registry.GetTemplate(node.Adapter)
			generated, capped, err := fuzz.GenerateCapped(fuzz.Target{Step: step, Node: node, KB: e.KB, Template: tmpl}, opts)
			if err != nil {
				return fmt.Errorf("fuzz target %s: %w", step.StepID(), err)
			}
			e.fuzzCapped = e.fuzzCapped || capped
			cases = slices.DeleteFunc(generated, func(c plan.FuzzCase) bool { return c.Input != "" && slices.Contains(block.Skip, c.Input) })
		}
		// A pinned case replaces a generated one with its ID.
		for _, pc := range block.Pinned {
			cases = slices.DeleteFunc(cases, func(c plan.FuzzCase) bool { return c.ID == pc.ID })
			cases = append(cases, pc.Case())
		}
		if len(only) > 0 {
			cases = slices.DeleteFunc(cases, func(c plan.FuzzCase) bool { return !slices.Contains(only, c.ID) })
		}
		for _, c := range cases {
			found[c.ID] = true
		}
		if err := plan.ExpandFuzzCases(p, step.StepID(), cases, scope != "shared"); err != nil {
			return err
		}
		e.fuzzJudge[step.StepID()] = fuzzJudging{fail: fail, accept: block.Accept}
	}
	if cli != nil && len(targets) > 0 {
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
	if e.oasCache != nil && r.Error == nil && r.Response != nil && grpcStatusName(r.Response) == "" {
		documented, known := oas.StatusDocumented(node, e.graphOAS, e.oasCache, r.StatusCode)
		undocumented = known && !documented
	}

	finding := ""
	switch {
	case r.Error != nil && (r.Request == nil || errors.As(r.Error, new(*adapter.NotSentError))):
		finding = FindingNotSent
	case r.Error != nil:
		finding = FindingNoResponse
	case r.StatusCode >= 500:
		finding = FindingServerError
	case undocumented:
		finding = FindingUndocumentedStatus
	case r.OASValidation != nil && r.OASValidation.Response != nil && !r.OASValidation.Response.Valid && !r.OASValidation.Response.Skipped:
		finding = FindingSchemaViolation
	case mode == plan.FuzzNegative && r.StatusCode < 400:
		finding = FindingAcceptedInvalid
	case mode == plan.FuzzPositive && r.StatusCode >= 400:
		finding = FindingRejectedValid
	}
	judging, ok := e.fuzzJudge[c.Target]
	if !ok {
		judging.fail = DefaultFuzzFail
	}
	fail := judging.fail
	// A status the block accepts is never a judgement call against the API.
	if judging.accept.Matches(r.StatusCode, grpcStatusName(r.Response)) && r.Error == nil {
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

// FuzzSummary counts a run's fuzz cases by finding.
type FuzzSummary struct {
	Cases int
	// Findings counts the cases by finding; a case whose response was what
	// it called for is counted under "".
	Findings map[string]int
	// Failing counts the cases whose finding failed the run.
	Failing int
}

// SummarizeFuzz counts the fuzz cases among steps, or returns nil when there
// are none.
func SummarizeFuzz(steps []StepResult) *FuzzSummary {
	var s *FuzzSummary
	for _, r := range steps {
		if r.Fuzz == nil {
			continue
		}
		if s == nil {
			s = &FuzzSummary{Findings: map[string]int{}}
		}
		s.Cases++
		s.Findings[r.Fuzz.Finding]++
		if r.Fuzz.Fails {
			s.Failing++
		}
	}
	return s
}
