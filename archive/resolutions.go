package archive

import (
	"fmt"
	"strconv"
)

// InputResolution is how one input of a step got its value: its resolution
// record, and the array selection that picked the value, if one did.
type InputResolution struct {
	ValueResolutionRecord
	Selection *SelectionRecord `json:"selection,omitempty"`
}

// StepResolutions joins a step's resolution records to the selections that
// picked their values, in the order the inputs were resolved. An input that
// couldn't be resolved has source "error".
func StepResolutions(step *StepRecord) []InputResolution {
	if len(step.Resolutions) == 0 {
		return nil
	}
	// A named selection is recorded for itself and then for each input that
	// reads it, so the last record under an input's name is the input's.
	selections := make(map[string]*SelectionRecord, len(step.Selections))
	for i := range step.Selections {
		selections[step.Selections[i].InputName] = &step.Selections[i]
	}
	joined := make([]InputResolution, len(step.Resolutions))
	for i, r := range step.Resolutions {
		joined[i] = InputResolution{ValueResolutionRecord: r, Selection: selections[r.InputName]}
	}
	return joined
}

// SelectionTieWarnings returns a warning for each min or max selection whose
// candidates tied, so the first of them was chosen with nothing to tell them
// apart, as in
//
//	selection "cheapest": 3 of 8 elements from listProducts.products tie for min price at 19.99; picked index 0 (…)
//
// A named selection's decision is recorded once for the selection and once for
// each input that reads it; it gets one warning. A selection with onTie: first
// takes the first on purpose and gets none.
func SelectionTieWarnings(selections []SelectionRecord) []string {
	var warnings []string
	seen := make(map[string]bool)
	for _, s := range selections {
		if s.Ties < 2 || s.OnTie == "first" {
			continue
		}
		label := fmt.Sprintf("input %q", s.InputName)
		if s.SelectionName != "" {
			label = fmt.Sprintf("selection %q", s.SelectionName)
		}
		if seen[label] {
			continue
		}
		seen[label] = true
		at := ""
		if s.SortValue != nil {
			at = " at " + strconv.FormatFloat(*s.SortValue, 'f', -1, 64)
		}
		warnings = append(warnings, fmt.Sprintf("%s: %d of %d elements from %s.%s tie for %s %s%s; picked index %d (add a filter to choose, or onTie: first to accept)",
			label, s.Ties, s.FilteredSize, s.SourceNode, s.SourceField, s.Strategy, s.SortField, at, s.SelectedIndex))
	}
	return warnings
}
