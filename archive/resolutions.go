package archive

import (
	"fmt"
	"strconv"
)

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
