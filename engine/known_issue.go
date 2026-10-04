package engine

import (
	"fmt"
	"time"

	"github.com/gburgyan/aat/plan"
)

// now returns the clock a knownIssue's expiry is judged against.
func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// knownIssueLog collects what a run's knownIssue entries did, and keeps the
// run's real outcome alongside the one it reports.
//
// The two differ on purpose. A run whose only failures are covered by an entry
// reports passed, so CI goes green and every reader of an outcome — the exit
// code, batch totals, the retry gate — needs no special case. But cleanup is
// gated on raw: a suppressed failure is still a failure as far as the
// resources are concerned, so a runOn: failure cleanup must still fire.
type knownIssueLog struct {
	raw      Outcome
	applied  []KnownIssueApplied
	resolved []KnownIssueApplied
	early    bool
}

// endedEarly notes that a covered failure stopped the run before its last
// step, because the step left nothing for the rest of the plan to build on.
// The run still reports passed; this is what lets a reader be told why it is
// shorter than the plan.
func (l *knownIssueLog) endedEarly() {
	if l != nil {
		l.early = true
	}
}

// stepFailed reports whether a completed step's result is a failure a
// knownIssue can cover. It is the same set of conditions Run decides on, read
// once and early so the progress line, the archive, and the outcome cannot
// disagree about what happened. A transport error is not among them.
func (e *Engine) stepFailed(step plan.Step, r *StepResult) bool {
	if step.ExpectFailure != nil {
		if r.ExpectFailure != nil && !r.ExpectFailure.Passed {
			return true
		}
		return r.Validation != nil && !r.Validation.Passed
	}
	switch {
	case r.StatusCode >= 400:
		return true
	case r.ResponseBodyError != nil:
		return true
	case e.oasStrictError(step, r) != nil:
		return true
	case r.Validation != nil && !r.Validation.Passed:
		return true
	}
	return false
}

// coverSetupFailure reports whether a fuzz setup copy failed only as a
// knownIssue lets its original fail and the happy path carry on: a failed
// assertion or a strict OpenAPI violation on a response that otherwise did
// what was asked. The copy is then used as the original is, and its result
// carries the entry. Nothing is logged for the run: the copy's original
// already answers for the defect.
func (e *Engine) coverSetupFailure(p *plan.Plan, step plan.Step, r *StepResult) bool {
	if r.Error != nil {
		return false
	}
	// A body error is the failure an expectFailure step may expect; on any
	// other step it means the response did not do what was asked.
	if step.ExpectFailure != nil {
		if r.ExpectFailure == nil || !r.ExpectFailure.Passed {
			return false
		}
	} else if r.StatusCode >= 400 || r.ResponseBodyError != nil {
		return false
	}
	ki, active := e.knownIssueFor(p, step)
	if ki == nil || !active {
		return false
	}
	r.KnownIssue = &KnownIssueResult{Until: ki.Until, Reason: ki.Reason, URL: ki.URL, Applied: true}
	return true
}

// withExpiry names the lapsed entry on the error of a failure it would have
// covered, so a build that has just turned red says why it stopped being
// forgiven rather than looking like a new problem.
func withExpiry(r *StepResult, err error) error {
	if r == nil || r.KnownIssue == nil || !r.KnownIssue.Expired || err == nil {
		return err
	}
	return fmt.Errorf("%w (knownIssue expired %s)", err, r.KnownIssue.Until)
}

func newKnownIssueLog() *knownIssueLog {
	return &knownIssueLog{raw: OutcomePassed}
}

// gating returns the outcome cleanup should be gated on.
func (l *knownIssueLog) gating(reported Outcome) Outcome {
	// Only a passed report can be hiding a suppressed failure; anything else
	// is already telling the truth and passes straight through.
	if l == nil || reported != OutcomePassed {
		return reported
	}
	return l.raw
}

// suppress records that an entry kept a step's failure out of the run's
// outcome, and marks the step so every surface can show why it is not red.
func (l *knownIssueLog) suppress(step plan.Step, result *StepResult, ki *plan.KnownIssue) {
	l.raw = OutcomeFailed
	r := &KnownIssueResult{
		Until:   ki.Until,
		Reason:  ki.Reason,
		URL:     ki.URL,
		Applied: true,
	}
	result.KnownIssue = r
	l.applied = append(l.applied, KnownIssueApplied{
		StepID:           step.StepID(),
		Node:             step.Node,
		KnownIssueResult: *r,
	})
}

// expired records that a step carried an entry whose date had passed. The
// failure counts, and the step says why it stopped being forgiven.
func (l *knownIssueLog) expired(result *StepResult, ki *plan.KnownIssue) {
	result.KnownIssue = &KnownIssueResult{
		Until:   ki.Until,
		Reason:  ki.Reason,
		URL:     ki.URL,
		Expired: true,
	}
}

// resolve records that a step passed although an entry was in force, which
// means the entry has outlived the defect and should be deleted.
func (l *knownIssueLog) resolve(step plan.Step, result *StepResult, ki *plan.KnownIssue) {
	r := &KnownIssueResult{
		Until:    ki.Until,
		Reason:   ki.Reason,
		URL:      ki.URL,
		Resolved: true,
	}
	result.KnownIssue = r
	l.resolved = append(l.resolved, KnownIssueApplied{
		StepID:           step.StepID(),
		Node:             step.Node,
		KnownIssueResult: *r,
	})
}

// knownIssueFor returns the entry governing step and whether it still applies.
// A step's own entry wins over the plan's.
func (e *Engine) knownIssueFor(p *plan.Plan, step plan.Step) (ki *plan.KnownIssue, active bool) {
	ki = p.KnownIssueFor(step)
	if ki == nil {
		return nil, false
	}
	return ki, ki.Active(e.now())
}
