package plan

import (
	"fmt"
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
	// Target is the step the case was made from.
	Target string `yaml:"-" json:"target"`
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
		closure = transitivePrereqClosure(target, byID)
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
			suffix := "__fuzz-" + strings.TrimPrefix(childID, targetID+"--fuzz-")
			clones := cloneClosureWithSuffix(closure, suffix)
			idMap = make(map[string]string, len(clones))
			for i, orig := range closure {
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
		child.Values[c.Input] = StepValue{Default: c.Value, Raw: true}
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
