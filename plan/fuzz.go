package plan

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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
