package plan

// AutowireMarker reports whether sv is a workflow-template marker asking
// composition to wire the input from a step's output of the same name:
// AUTOWIRE, the legacy PLACEHOLDER, or AUTOWIRE?, which marks an input that may
// stay unset when no step produces it. optional is true for AUTOWIRE?.
func AutowireMarker(sv StepValue) (marker, optional bool) {
	s, ok := sv.Default.(string)
	if !ok {
		return false, false
	}
	switch s {
	case "AUTOWIRE", "PLACEHOLDER":
		return true, false
	case "AUTOWIRE?":
		return true, true
	}
	return false, false
}
