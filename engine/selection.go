package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"

	"github.com/gburgyan/aat/internal/predicate"
	"github.com/gburgyan/aat/plan"
)

// selectionResult holds the output of applySelection.
type selectionResult struct {
	element      any
	index        int
	filteredSize int
	// ties counts the elements that share the chosen min or max value, the
	// chosen one included, so 1 means no tie. It is 0 for other strategies.
	ties int
	// sortValue is the chosen min or max value; nil for other strategies.
	sortValue *float64
}

// applySelection selects a single element from arr according to the strategy in sel.
// Returns the selected element, its index in the (possibly filtered) array, and the
// size of the array after filtering (for match, the number of matching elements).
func applySelection(arr []any, sel *plan.SelectionConfig) (*selectionResult, error) {
	if len(arr) == 0 {
		return nil, fmt.Errorf("array is empty")
	}

	// Default to first when no config or empty strategy and no filter
	if sel == nil || (sel.Strategy == "" && sel.Filter == "") {
		return &selectionResult{element: arr[0], index: 0, filteredSize: len(arr)}, nil
	}

	// match uses filter directly without pre-filtering; a filter with no
	// strategy means match, as plan.SelectionStrategies documents
	if sel.Strategy == "match" || sel.Strategy == "" {
		elem, idx, matches, err := selectMatch(arr, sel.Filter)
		if err != nil {
			return nil, err
		}
		return &selectionResult{element: elem, index: idx, filteredSize: matches}, nil
	}

	// Apply filter for all other strategies if present
	working := arr
	if sel.Filter != "" {
		var err error
		working, err = applyFilter(arr, sel.Filter)
		if err != nil {
			return nil, err
		}
	}

	switch sel.Strategy {
	case "first":
		return &selectionResult{element: working[0], index: 0, filteredSize: len(working)}, nil
	case "last":
		idx := len(working) - 1
		return &selectionResult{element: working[idx], index: idx, filteredSize: len(working)}, nil
	case "index":
		if sel.Index < 0 || sel.Index >= len(working) {
			return &selectionResult{}, fmt.Errorf("index %d out of bounds for array of length %d", sel.Index, len(working))
		}
		return &selectionResult{element: working[sel.Index], index: sel.Index, filteredSize: len(working)}, nil
	case "random":
		idx := rand.IntN(len(working))
		return &selectionResult{element: working[idx], index: idx, filteredSize: len(working)}, nil
	case "min":
		return selectByFieldExtreme(working, compareField(sel), false)
	case "max":
		return selectByFieldExtreme(working, compareField(sel), true)
	default:
		return nil, fmt.Errorf("unknown selection strategy %q", sel.Strategy)
	}
}

// compareField returns the field the min and max strategies compare: sortField,
// or field when there is no sortField.
func compareField(sel *plan.SelectionConfig) string {
	if sel.SortField != "" {
		return sel.SortField
	}
	return sel.Field
}

// tieError is the resolution error for a min or max selection whose candidates
// tie, when its onTie is fail. It returns nil otherwise.
func tieError(result *selectionResult, onTie, strategy, field string) error {
	if onTie != "fail" || result.ties < 2 {
		return nil
	}
	at := ""
	if result.sortValue != nil {
		at = " at " + strconv.FormatFloat(*result.sortValue, 'f', -1, 64)
	}
	return fmt.Errorf("%d of %d elements tie for %s %s%s, and onTie is fail; add a filter that picks one",
		result.ties, result.filteredSize, strategy, field, at)
}

// applyFilter evaluates a predicate expression against each element, keeping
// those where the predicate returns true.
func applyFilter(arr []any, expr string) ([]any, error) {
	pred, err := predicate.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("evaluating filter: %w", err)
	}
	var result []any
	for _, elem := range arr {
		m, err := elementToMap(elem)
		if err != nil {
			return nil, fmt.Errorf("converting element for filter: %w", err)
		}
		match, err := pred.Eval(m)
		if err != nil {
			return nil, fmt.Errorf("evaluating filter: %w", err)
		}
		if match {
			result = append(result, elem)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("filter %q matched no elements", expr)
	}
	return result, nil
}

// selectMatch returns the first element matching the predicate, its index, and
// how many elements match. An element after the first match that the predicate
// can't be evaluated against is not counted, so the choice fails only as it did
// before counting.
func selectMatch(arr []any, expr string) (any, int, int, error) {
	pred, err := predicate.Parse(expr)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("evaluating match predicate: %w", err)
	}
	first, matches := -1, 0
	for i, elem := range arr {
		m, err := elementToMap(elem)
		if err == nil {
			var match bool
			match, err = pred.Eval(m)
			if err == nil && match {
				if first < 0 {
					first = i
				}
				matches++
				continue
			}
		}
		if err != nil && first < 0 {
			return nil, 0, 0, fmt.Errorf("evaluating match predicate: %w", err)
		}
	}
	if first < 0 {
		return nil, 0, 0, fmt.Errorf("no element matches predicate %q", expr)
	}
	return arr[first], first, matches, nil
}

// selectByFieldExtreme finds the element with the minimum or maximum value of a
// field, the first of them when several share it, and counts how many do.
func selectByFieldExtreme(arr []any, field string, max bool) (*selectionResult, error) {
	bestIdx, ties := -1, 0
	var bestVal float64

	for i, elem := range arr {
		val, err := extractNumericField(elem, field)
		if err != nil {
			return nil, fmt.Errorf("extracting field %q from element %d: %w", field, i, err)
		}
		switch {
		case bestIdx == -1 || (max && val > bestVal) || (!max && val < bestVal):
			bestIdx, bestVal, ties = i, val, 1
		case val == bestVal:
			ties++
		}
	}

	return &selectionResult{
		element:      arr[bestIdx],
		index:        bestIdx,
		filteredSize: len(arr),
		ties:         ties,
		sortValue:    &bestVal,
	}, nil
}

// elementToMap converts an element to map[string]any for predicate evaluation.
func elementToMap(element any) (map[string]any, error) {
	if m, ok := element.(map[string]any); ok {
		return m, nil
	}
	// JSON round-trip for struct types
	data, err := json.Marshal(element)
	if err != nil {
		return nil, fmt.Errorf("marshaling element: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshaling element to map: %w", err)
	}
	return m, nil
}

// extractNumericField extracts a field from an element and coerces it to float64.
func extractNumericField(element any, field string) (float64, error) {
	val, err := extractField(element, field)
	if err != nil {
		return 0, err
	}
	return toFloat64(val)
}

// toFloat64 coerces a value to float64. A string holding a decimal number, the
// way many APIs send money amounts, is parsed; NaN and infinities are not numbers
// to sort by.
func toFloat64(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int16:
		return float64(n), nil
	case int8:
		return float64(n), nil
	case uint:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	case uint32:
		return float64(n), nil
	case uint16:
		return float64(n), nil
	case uint8:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, fmt.Errorf("cannot convert string %q to a number", n)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}
