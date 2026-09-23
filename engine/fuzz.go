package engine

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gburgyan/aat/fuzz"
	"github.com/gburgyan/aat/plan"
)

// Fuzz findings: what a fuzz step's response says about the API. An empty
// finding means the response was what the case called for.
const (
	// FindingServerError is a 5xx: the API failed on the value.
	FindingServerError = "server-error"
	// FindingNoResponse is a request that was sent and got no response, such
	// as a timeout or a dropped connection.
	FindingNoResponse = "no-response"
	// FindingSchemaViolation is a response that breaks the OpenAPI spec.
	FindingSchemaViolation = "schema-violation"
	// FindingAcceptedInvalid is a success for a value the input forbids.
	FindingAcceptedInvalid = "accepted-invalid"
	// FindingRejectedValid is a 4xx for a value the input allows.
	FindingRejectedValid = "rejected-valid"
	// FindingNotSent is a case aat could not build a request for, such as a
	// value a gRPC message cannot hold. It says nothing about the API.
	FindingNotSent = "not-sent"
)

// AllFindings lists the findings, most serious first.
var AllFindings = []string{FindingServerError, FindingNoResponse, FindingSchemaViolation, FindingAcceptedInvalid, FindingRejectedValid, FindingNotSent}

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
	// Shared runs every case on the target's own prerequisites instead of a
	// copy of them for each case. It is faster, but a case that changes state
	// can change what later steps see.
	Shared bool
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

// expandFuzz adds the fuzz cases to an instantiated plan.
func (e *Engine) expandFuzz(p *plan.Plan, seed uint64) error {
	cfg := e.fuzz
	var targets []plan.Step
	matched := map[string]bool{}
	for _, s := range p.Execution.Steps {
		for _, t := range cfg.Targets {
			if s.StepID() == t || s.Node == t {
				targets = append(targets, s)
				matched[t] = true
				break
			}
		}
	}
	if !cfg.AllowNoTarget {
		for _, t := range cfg.Targets {
			if !matched[t] {
				return fmt.Errorf("--fuzz %s: the plan has no step with that ID and no step of that node", t)
			}
		}
	}

	found := map[string]bool{}
	for _, target := range targets {
		node := e.graph.Nodes[target.Node]
		if node == nil {
			return fmt.Errorf("fuzz target %s: node %q not found in graph", target.StepID(), target.Node)
		}
		cases, capped, err := fuzz.GenerateCapped(fuzz.Target{Step: target, Node: node, KB: e.KB}, fuzz.Options{
			Modes:  cfg.Modes,
			Inputs: cfg.Inputs,
			Max:    cfg.Max,
			Seed:   seed,
			Now:    time.Now(),
		})
		if err != nil {
			return fmt.Errorf("fuzz target %s: %w", target.StepID(), err)
		}
		e.fuzzCapped = e.fuzzCapped || capped
		if len(cfg.Cases) > 0 {
			cases = slices.DeleteFunc(cases, func(c plan.FuzzCase) bool { return !slices.Contains(cfg.Cases, c.ID) })
		}
		for _, c := range cases {
			found[c.ID] = true
		}
		if err := plan.ExpandFuzzCases(p, target.StepID(), cases, !cfg.Shared); err != nil {
			return err
		}
	}
	for _, id := range cfg.Cases {
		if !found[id] && len(targets) > 0 {
			return fmt.Errorf("--fuzz-case %s: no target step has a case with that ID", id)
		}
	}
	return nil
}

// judgeFuzz decides a fuzz step's finding from its result.
func (e *Engine) judgeFuzz(step plan.Step, r *StepResult) *FuzzResult {
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

	finding := ""
	switch {
	case r.Error != nil && r.Request == nil:
		finding = FindingNotSent
	case r.Error != nil:
		finding = FindingNoResponse
	case r.StatusCode >= 500:
		finding = FindingServerError
	case r.OASValidation != nil && r.OASValidation.Response != nil && !r.OASValidation.Response.Valid && !r.OASValidation.Response.Skipped:
		finding = FindingSchemaViolation
	case mode == plan.FuzzNegative && r.StatusCode < 400:
		finding = FindingAcceptedInvalid
	case mode == plan.FuzzPositive && r.StatusCode >= 400:
		finding = FindingRejectedValid
	}
	fail := e.fuzz.Fail
	if fail == nil {
		fail = DefaultFuzzFail
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
