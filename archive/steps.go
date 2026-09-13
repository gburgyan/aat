package archive

import "fmt"

// StepID returns a step's ID: its StepID, or its node when the record has
// none, as in cleanup records written before cleanup steps had IDs.
func StepID(s StepRecord) string {
	if s.StepID != "" {
		return s.StepID
	}
	return s.Node
}

// CleanupStepIDs returns the IDs of cleanup steps, in order. A repeated ID gets
// a suffix, _2, _3, and so on, so each one is unique, as the web UI names them.
func CleanupStepIDs(cleanup []StepRecord) []string {
	seen := make(map[string]int, len(cleanup))
	ids := make([]string, len(cleanup))
	for i, s := range cleanup {
		id := StepID(s)
		seen[id]++
		if seen[id] > 1 {
			id = fmt.Sprintf("%s_%d", id, seen[id])
		}
		ids[i] = id
	}
	return ids
}

// FindStep returns the step of a whose ID is id: a main step, matched by
// StepID, or else a cleanup step, matched by the ID CleanupStepIDs gives it.
// cleanup reports that the step is a cleanup step. The step is nil when no ID
// matches.
func FindStep(a *Archive, id string) (step *StepRecord, cleanup bool) {
	for i := range a.Steps {
		if StepID(a.Steps[i]) == id {
			return &a.Steps[i], false
		}
	}
	for i, cleanupID := range CleanupStepIDs(a.Cleanup) {
		if cleanupID == id {
			return &a.Cleanup[i], true
		}
	}
	return nil, false
}

// StepPassed reports whether a step recorded no error, no failed assertion,
// no unmet expected failure, and no error in its response body. It does not
// look at the HTTP status on its own.
func StepPassed(s StepRecord) bool {
	if s.Error != "" {
		return false
	}
	if s.Validation != nil && !s.Validation.Passed {
		return false
	}
	if s.ExpectFailure != nil && !s.ExpectFailure.Passed {
		return false
	}
	if s.ResponseBodyError != nil {
		return false
	}
	return true
}
