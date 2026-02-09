package engine

import (
	"fmt"

	"github.com/gburgyan/aat/plan"
)

// RelaxationTracker tracks which soft constraints have been relaxed during
// a step's execution. It enforces circular detection (no constraint relaxed twice)
// and a maximum relaxation depth budget.
type RelaxationTracker struct {
	maxDepth int
	relaxed  map[string]bool // constraint Name → relaxed
	records  []RelaxationRecord
}

// RelaxationRecord captures a single constraint relaxation event.
type RelaxationRecord struct {
	ConstraintName string // plan.Constraint.Name
	InputRef       string // "node.input" that triggered it
	Reason         string // "resolution_exhausted" | "filter_empty" | "step_failed"
	Depth          int    // 1-indexed
}

// NewRelaxationTracker creates a tracker with the given maximum depth.
// If maxDepth <= 0, defaults to 3.
func NewRelaxationTracker(maxDepth int) *RelaxationTracker {
	if maxDepth <= 0 {
		maxDepth = 3
	}
	return &RelaxationTracker{
		maxDepth: maxDepth,
		relaxed:  make(map[string]bool),
	}
}

// CanRelax returns nil if the named constraint can be relaxed, or an error
// describing why not (already relaxed or budget exhausted).
func (rt *RelaxationTracker) CanRelax(constraintName string) error {
	if rt.relaxed[constraintName] {
		return fmt.Errorf("constraint %q already relaxed (circular)", constraintName)
	}
	if len(rt.records) >= rt.maxDepth {
		return fmt.Errorf("relaxation budget exhausted (%d/%d)", len(rt.records), rt.maxDepth)
	}
	return nil
}

// Relax records that the named constraint has been relaxed.
func (rt *RelaxationTracker) Relax(constraintName, inputRef, reason string) {
	rt.relaxed[constraintName] = true
	rt.records = append(rt.records, RelaxationRecord{
		ConstraintName: constraintName,
		InputRef:       inputRef,
		Reason:         reason,
		Depth:          len(rt.records) + 1,
	})
}

// IsRelaxed returns true if the named constraint has already been relaxed.
func (rt *RelaxationTracker) IsRelaxed(constraintName string) bool {
	return rt.relaxed[constraintName]
}

// Depth returns the number of relaxations performed so far.
func (rt *RelaxationTracker) Depth() int {
	return len(rt.records)
}

// Records returns a copy of the relaxation records.
func (rt *RelaxationTracker) Records() []RelaxationRecord {
	if len(rt.records) == 0 {
		return nil
	}
	out := make([]RelaxationRecord, len(rt.records))
	copy(out, rt.records)
	return out
}

// FindSoftConstraintForInput searches the plan's soft constraints for one that
// applies to the given step node and input. It checks both "stepNode.inputName"
// and "stepNode" in each constraint's AppliesTo list.
func FindSoftConstraintForInput(p *plan.Plan, stepNode, inputName string) (plan.Constraint, bool) {
	if p == nil || p.Intent.Constraints == nil {
		return plan.Constraint{}, false
	}

	fullRef := stepNode + "." + inputName
	for _, sc := range p.Intent.Constraints.Soft {
		for _, ref := range sc.AppliesTo {
			if ref == fullRef || ref == stepNode {
				return sc, true
			}
		}
	}
	return plan.Constraint{}, false
}
