package engine

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"

	"github.com/gburgyan/aat/plan"
)

// selectionResult holds the output of applySelection.
type selectionResult struct {
	element      any
	index        int
	filteredSize int
}

// applySelection selects a single element from arr according to the strategy in sel.
// Returns the selected element, its index in the (possibly filtered) array, and the
// size of the array after filtering.
func applySelection(arr []any, sel *plan.SelectionConfig) (*selectionResult, error) {
	if len(arr) == 0 {
		return nil, fmt.Errorf("array is empty")
	}

	// Default to first when no config or empty strategy and no filter
	if sel == nil || (sel.Strategy == "" && sel.Filter == "") {
		return &selectionResult{element: arr[0], index: 0, filteredSize: len(arr)}, nil
	}

	// match uses filter directly without pre-filtering
	if sel.Strategy == "match" {
		elem, idx, err := selectMatch(arr, sel.Filter)
		if err != nil {
			return nil, err
		}
		return &selectionResult{element: elem, index: idx, filteredSize: len(arr)}, nil
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
		compField := sel.SortField
		if compField == "" {
			compField = sel.Field
		}
		return selectByFieldExtreme(working, compField, false)
	case "max":
		compField := sel.SortField
		if compField == "" {
			compField = sel.Field
		}
		return selectByFieldExtreme(working, compField, true)
	default:
		return nil, fmt.Errorf("unknown selection strategy %q", sel.Strategy)
	}
}

// applyFilter evaluates a predicate expression against each element, keeping
// those where the predicate returns true.
func applyFilter(arr []any, expr string) ([]any, error) {
	var result []any
	for _, elem := range arr {
		m, err := elementToMap(elem)
		if err != nil {
			return nil, fmt.Errorf("converting element for filter: %w", err)
		}
		match, err := plan.EvalPredicate(expr, m)
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

// selectMatch iterates elements and returns the first one matching the predicate.
func selectMatch(arr []any, expr string) (any, int, error) {
	for i, elem := range arr {
		m, err := elementToMap(elem)
		if err != nil {
			return nil, 0, fmt.Errorf("converting element for match: %w", err)
		}
		match, err := plan.EvalPredicate(expr, m)
		if err != nil {
			return nil, 0, fmt.Errorf("evaluating match predicate: %w", err)
		}
		if match {
			return elem, i, nil
		}
	}
	return nil, 0, fmt.Errorf("no element matches predicate %q", expr)
}

// selectByFieldExtreme finds the element with the minimum or maximum value of a field.
func selectByFieldExtreme(arr []any, field string, max bool) (*selectionResult, error) {
	bestIdx := -1
	var bestVal float64

	for i, elem := range arr {
		val, err := extractNumericField(elem, field)
		if err != nil {
			return nil, fmt.Errorf("extracting field %q from element %d: %w", field, i, err)
		}
		if bestIdx == -1 || (max && val > bestVal) || (!max && val < bestVal) {
			bestIdx = i
			bestVal = val
		}
	}

	return &selectionResult{
		element:      arr[bestIdx],
		index:        bestIdx,
		filteredSize: len(arr),
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

// toFloat64 coerces a value to float64.
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
	case json.Number:
		return n.Float64()
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}
