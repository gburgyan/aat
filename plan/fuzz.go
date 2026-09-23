package plan

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/gburgyan/aat/graph"
)

// Fuzz case modes: what a case's value is, and so what the API should do with
// it.
const (
	// FuzzPositive is a value the input's type and constraints allow; the API
	// should accept it.
	FuzzPositive = "positive"
	// FuzzNegative is a value the input's type or constraints forbid; the API
	// should refuse it with a 4xx.
	FuzzNegative = "negative"
	// FuzzEdge is a value neither allowed nor forbidden by what the graph
	// declares, such as a very long or unusual string; the API may accept or
	// refuse it, but must not fail.
	FuzzEdge = "edge"
)

// FuzzCase is one generated value sent to one input of a step. It is not
// written in plan files: the fuzzer puts it on the sibling step it creates,
// and the archive records it.
type FuzzCase struct {
	// ID names the case by input and strategy, such as "quantity.above-max".
	// It is stable across runs, so a case can be replayed by name.
	ID       string `yaml:"-" json:"id"`
	Mode     string `yaml:"-" json:"mode"`
	Input    string `yaml:"-" json:"input"`
	Strategy string `yaml:"-" json:"strategy"`
	Value    any    `yaml:"-" json:"value"`
	// Patch, when set, is what the case changes in the request the step
	// builds, instead of setting Value on Input: leaving a field out, or
	// setting it to null or a value of another type. Input names the input
	// the field carries, if it carries one.
	Patch []RequestPatch `yaml:"-" json:"patch,omitempty"`
	// Target is the step the case was made from.
	Target string `yaml:"-" json:"target"`
}

// RequestPatch is one change to a built request: Op "remove" or "set" of the
// body value at a GJSON Path (Where "body"), or of a query parameter or
// header by name (Where "query" or "header").
type RequestPatch struct {
	Where string `yaml:"where" json:"where"`
	Path  string `yaml:"path" json:"path"`
	Op    string `yaml:"op" json:"op"`
	Value any    `yaml:"value,omitempty" json:"value,omitempty"`
}

// fuzzSetupClosure returns the steps a case's copy of the plan needs before
// the step at idx: the steps it depends on, and every earlier step that
// builds on those, with their own prerequisites, in plan order. A step that
// adds an item to a cart the target checks out is part of the setup even when
// the target reads nothing from it. Fuzz steps and the copies made for other
// cases are never part of it.
func fuzzSetupClosure(steps []Step, idx int, byID map[string]Step) []Step {
	in := map[string]bool{}
	for _, s := range transitivePrereqClosure(steps[idx], byID) {
		in[s.StepID()] = true
	}
	for changed := true; changed; {
		changed = false
		for _, s := range steps[:idx] {
			if in[s.StepID()] || s.Fuzz != nil || s.FuzzSetup != "" || s.IsSlotMarker() {
				continue
			}
			builds := false
			for _, dep := range s.DependsOn {
				if in[dep] {
					builds = true
					break
				}
			}
			if !builds {
				continue
			}
			in[s.StepID()] = true
			for _, pre := range transitivePrereqClosure(s, byID) {
				in[pre.StepID()] = true
			}
			changed = true
		}
	}
	var out []Step
	for _, s := range steps[:idx] {
		if in[s.StepID()] {
			out = append(out, s)
		}
	}
	return out
}

// Fuzz finding names, as the engine reports them and a fuzz block's fail
// list names them.
const (
	FindingServerError        = "server-error"
	FindingNoResponse         = "no-response"
	FindingSchemaViolation    = "schema-violation"
	FindingAcceptedInvalid    = "accepted-invalid"
	FindingRejectedValid      = "rejected-valid"
	FindingUndocumentedStatus = "undocumented-status"
	FindingNotSent            = "not-sent"
)

// FuzzFindings lists the findings, most serious first.
var FuzzFindings = []string{FindingServerError, FindingNoResponse, FindingSchemaViolation, FindingAcceptedInvalid,
	FindingRejectedValid, FindingUndocumentedStatus, FindingNotSent}

// FuzzModes lists the case modes.
var FuzzModes = []string{FuzzPositive, FuzzNegative, FuzzEdge}

// FuzzSettings is a step's fuzz: block: what --fuzz would do for the step,
// saved in the plan, and the judgements about the API that only fuzzing
// needs.
type FuzzSettings struct {
	// Mode, Inputs, Cases, Only, Scope, and Fail are the step's --fuzz-mode,
	// --fuzz-input, --fuzz-cases, --fuzz-case, --fuzz-scope, and --fuzz-fail.
	Mode   []string `yaml:"mode,omitempty" json:"mode,omitempty"`
	Inputs []string `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Cases  int      `yaml:"cases,omitempty" json:"cases,omitempty"`
	Only   []string `yaml:"only,omitempty" json:"only,omitempty"`
	Scope  string   `yaml:"scope,omitempty" json:"scope,omitempty"`
	Fail   []string `yaml:"fail,omitempty" json:"fail,omitempty"`
	// Skip names inputs never to fuzz.
	Skip []string `yaml:"skip,omitempty" json:"skip,omitempty"`
	// Accept lists statuses no case is faulted for: a response with one of
	// them is never accepted-invalid, rejected-valid, or undocumented-status.
	// For an API that answers 409 to anything it can't do right now, say.
	Accept ExpectedStatuses `yaml:"accept,omitempty" json:"accept,omitempty"`
	// Pinned cases are sent as written, without the generator. --fuzz-save
	// writes them.
	Pinned []PinnedFuzzCase `yaml:"pinned,omitempty" json:"pinned,omitempty"`
}

// Generates reports whether the block asks for generated cases: it has no
// pinned cases, or it sets what to generate.
func (s *FuzzSettings) Generates() bool {
	return len(s.Pinned) == 0 || len(s.Mode) > 0 || len(s.Inputs) > 0 || s.Cases > 0 || len(s.Only) > 0
}

// PinnedFuzzCase is a fuzz case written in a plan: the value for one input,
// or a patch to the request.
type PinnedFuzzCase struct {
	ID    string         `yaml:"id" json:"id"`
	Mode  string         `yaml:"mode" json:"mode"`
	Input string         `yaml:"input,omitempty" json:"input,omitempty"`
	Value any            `yaml:"value,omitempty" json:"value,omitempty"`
	Patch []RequestPatch `yaml:"patch,omitempty" json:"patch,omitempty"`
	// Found is what the case found when it was saved; it is not checked.
	Found string `yaml:"found,omitempty" json:"found,omitempty"`
}

// Case returns the pinned case as the fuzzer sends it.
func (p PinnedFuzzCase) Case() FuzzCase {
	strategy := p.ID
	if i := strings.LastIndex(p.ID, "."); i >= 0 {
		strategy = p.ID[i+1:]
	}
	return FuzzCase{ID: p.ID, Mode: p.Mode, Input: p.Input, Strategy: strategy, Value: p.Value, Patch: p.Patch}
}

// Pin returns a case as a plan writes it.
func (c FuzzCase) Pin() PinnedFuzzCase {
	return PinnedFuzzCase{ID: c.ID, Mode: c.Mode, Input: c.Input, Value: c.Value, Patch: c.Patch}
}

// StripFuzz removes every step's fuzz: block, for --no-fuzz.
func StripFuzz(p *Plan) {
	if p == nil {
		return
	}
	for i := range p.Execution.Steps {
		p.Execution.Steps[i].FuzzSettings = nil
	}
}

// FuzzStepID returns the ID of the sibling step that runs a case against the
// step targetID. It keeps to characters a step ID may hold.
func FuzzStepID(targetID, caseID string) string {
	return targetID + "--fuzz-" + strings.NewReplacer(".", "-", " ", "-").Replace(caseID)
}

// ExpandFuzzCases adds a sibling step for each case after the step targetID
// in an instantiated plan. A sibling is the target with the case's value set
// raw on its input; it has no assertions, retries, repeat, expectFailure, or
// known issue, since the engine judges it by the fuzz checks alone. With
// isolated, each sibling gets its own copy of the steps the target depends on,
// so a case cannot change the state the happy path or another case runs on.
func ExpandFuzzCases(p *Plan, targetID string, cases []FuzzCase, isolated bool) error {
	idx := -1
	byID := make(map[string]Step, len(p.Execution.Steps))
	for i, s := range p.Execution.Steps {
		byID[s.StepID()] = s
		if s.StepID() == targetID {
			idx = i
		}
	}
	if idx < 0 {
		return fmt.Errorf("fuzz target %q is not a step of the plan", targetID)
	}
	target := p.Execution.Steps[idx]

	var closure []Step
	if isolated {
		closure = fuzzSetupClosure(p.Execution.Steps, idx, byID)
	}

	var added []Step
	for _, c := range cases {
		c.Target = targetID
		childID := FuzzStepID(targetID, c.ID)
		if _, taken := byID[childID]; taken {
			return fmt.Errorf("fuzz case %q: step %q already exists", c.ID, childID)
		}

		var idMap map[string]string
		if isolated && len(closure) > 0 {
			// ExpandFuzzCases marks each copy with the case it sets up, so the
			// engine can tell a failed setup from a failed plan.
			// The suffix names the target too: two targets can have a case with
			// the same ID, and their copies of a shared step must not collide.
			suffix := "__" + childID
			clones := cloneClosureWithSuffix(closure, suffix)
			for i := range clones {
				clones[i].FuzzSetup = childID
				clones[i].FuzzSettings = nil // a copy is setup, not a target
			}
			idMap = make(map[string]string, len(clones))
			for i, orig := range closure {
				if _, taken := byID[clones[i].StepID()]; taken {
					return fmt.Errorf("fuzz case %q: step %q already exists", c.ID, clones[i].StepID())
				}
				byID[clones[i].StepID()] = clones[i]
				idMap[orig.StepID()] = clones[i].StepID()
			}
			added = append(added, clones...)
		}

		child := deepCopyStep(target)
		child.ID = childID
		child.IsGoal = false
		child.Assertions = nil
		child.Repeat = nil
		child.Retry = nil
		child.ExpectFailure = nil
		child.KnownIssue = nil
		child.Mutations = nil
		child.MutationScope = ""
		child.FuzzSettings = nil
		if child.Values == nil {
			child.Values = map[string]StepValue{}
		}
		if len(c.Patch) == 0 {
			child.Values[c.Input] = StepValue{Default: c.Value, Raw: true}
		}
		cc := c
		child.Fuzz = &cc
		if len(idMap) > 0 {
			rewriteStepRefs(&child, idMap)
		}
		added = append(added, child)
		byID[childID] = child
	}

	steps := make([]Step, 0, len(p.Execution.Steps)+len(added))
	steps = append(steps, p.Execution.Steps[:idx+1]...)
	steps = append(steps, added...)
	steps = append(steps, p.Execution.Steps[idx+1:]...)
	p.Execution.Steps = steps
	return nil
}

// Describe says what the case sends, as in `quantity=-1`, `remove quantity`,
// or `null body shipping.country`.
func (c FuzzCase) Describe() string {
	if len(c.Patch) == 0 {
		return fmt.Sprintf("%s=%s", c.Input, compactValue(c.Value))
	}
	parts := make([]string, 0, len(c.Patch))
	for _, p := range c.Patch {
		target := p.Where + " " + p.Path
		switch {
		case p.Op == "remove":
			parts = append(parts, "remove "+target)
		case p.Value == nil:
			parts = append(parts, "null "+target)
		default:
			parts = append(parts, fmt.Sprintf("set %s=%s", target, compactValue(p.Value)))
		}
	}
	return strings.Join(parts, ", ")
}

// compactValue renders a value for one line, cutting a long one short.
func compactValue(v any) string {
	var s string
	switch t := v.(type) {
	case string:
		s = strconv.Quote(t)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			s = fmt.Sprint(v)
		} else {
			s = string(b)
		}
	}
	if len(s) > 40 {
		s = s[:37] + "..."
	}
	return s
}

// validateFuzzSettings checks a step's fuzz: block against its node.
func validateFuzzSettings(prefix string, s *FuzzSettings, node *graph.Node) []string {
	if s == nil {
		return nil
	}
	var errs []string
	add := func(format string, args ...any) {
		errs = append(errs, prefix+": fuzz: "+fmt.Sprintf(format, args...))
	}
	inputs := map[string]bool{}
	for _, in := range node.Inputs {
		inputs[in.Name] = true
	}
	for _, m := range s.Mode {
		if !slices.Contains(FuzzModes, m) {
			add("unknown mode %q (use %s)", m, strings.Join(FuzzModes, ", "))
		}
	}
	for _, list := range [][]string{s.Inputs, s.Skip} {
		for _, in := range list {
			if !inputs[in] {
				add("node %s has no input %q", node.Name, in)
			}
		}
	}
	if s.Cases < 0 {
		add("cases must not be negative")
	}
	if s.Scope != "" && s.Scope != "isolated" && s.Scope != "shared" {
		add("unknown scope %q (use isolated or shared)", s.Scope)
	}
	for _, f := range s.Fail {
		if !slices.Contains(FuzzFindings, f) {
			add("unknown finding %q in fail (use %s)", f, strings.Join(FuzzFindings, ", "))
		}
	}
	for _, st := range s.Accept {
		if st.Code >= 500 || st.Class >= 5 {
			add("accept %s: a server error is never an acceptable answer", st)
		}
	}
	ids := map[string]bool{}
	for j, p := range s.Pinned {
		where := fmt.Sprintf("pinned case %d", j)
		if p.ID == "" {
			add("%s has no id", where)
		} else if ids[p.ID] {
			add("%s: id %q is used twice", where, p.ID)
		}
		ids[p.ID] = true
		if !slices.Contains(FuzzModes, p.Mode) {
			add("%s (%s): mode %q is not one of %s", where, p.ID, p.Mode, strings.Join(FuzzModes, ", "))
		}
		switch {
		case len(p.Patch) > 0 && p.Value != nil:
			add("%s (%s): set a value or a patch, not both", where, p.ID)
		case len(p.Patch) == 0 && p.Input == "":
			add("%s (%s): name the input it sets, or give a patch", where, p.ID)
		}
		if p.Input != "" && !inputs[p.Input] {
			add("%s (%s): node %s has no input %q", where, p.ID, node.Name, p.Input)
		}
		for _, rp := range p.Patch {
			if rp.Where != "body" && rp.Where != "query" && rp.Where != "header" {
				add("%s (%s): patch where %q is not body, query, or header", where, p.ID, rp.Where)
			}
			if rp.Op != "remove" && rp.Op != "set" {
				add("%s (%s): patch op %q is not remove or set", where, p.ID, rp.Op)
			}
			if rp.Path == "" {
				add("%s (%s): patch has no path", where, p.ID)
			}
		}
	}
	return errs
}
