package engine

import (
	"testing"

	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplySelection_NilConfig_DefaultsToFirst(t *testing.T) {
	arr := []any{"a", "b", "c"}
	result, err := applySelection(arr, nil)
	require.NoError(t, err)
	assert.Equal(t, "a", result.element)
	assert.Equal(t, 0, result.index)
	assert.Equal(t, 3, result.filteredSize)
}

func TestApplySelection_EmptyStrategy_DefaultsToFirst(t *testing.T) {
	arr := []any{"a", "b", "c"}
	result, err := applySelection(arr, &plan.SelectionConfig{Strategy: ""})
	require.NoError(t, err)
	assert.Equal(t, "a", result.element)
	assert.Equal(t, 0, result.index)
}

func TestApplySelection_First(t *testing.T) {
	arr := []any{
		map[string]any{"id": "first", "val": 1},
		map[string]any{"id": "second", "val": 2},
	}
	result, err := applySelection(arr, &plan.SelectionConfig{Strategy: "first"})
	require.NoError(t, err)
	assert.Equal(t, arr[0], result.element)
	assert.Equal(t, 0, result.index)
}

func TestApplySelection_Last(t *testing.T) {
	arr := []any{
		map[string]any{"id": "first"},
		map[string]any{"id": "second"},
		map[string]any{"id": "third"},
	}
	result, err := applySelection(arr, &plan.SelectionConfig{Strategy: "last"})
	require.NoError(t, err)
	assert.Equal(t, arr[2], result.element)
	assert.Equal(t, 2, result.index)
}

func TestApplySelection_Index_Valid(t *testing.T) {
	arr := []any{"a", "b", "c", "d"}
	result, err := applySelection(arr, &plan.SelectionConfig{Strategy: "index", Index: 2})
	require.NoError(t, err)
	assert.Equal(t, "c", result.element)
	assert.Equal(t, 2, result.index)
}

func TestApplySelection_Index_OutOfBounds(t *testing.T) {
	arr := []any{"a", "b"}
	_, err := applySelection(arr, &plan.SelectionConfig{Strategy: "index", Index: 5})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of bounds")
}

func TestApplySelection_Random_ElementInArray(t *testing.T) {
	arr := []any{"a", "b", "c", "d", "e"}
	result, err := applySelection(arr, &plan.SelectionConfig{Strategy: "random"})
	require.NoError(t, err)
	assert.Contains(t, arr, result.element)
	assert.GreaterOrEqual(t, result.index, 0)
	assert.Less(t, result.index, len(arr))
}

func TestApplySelection_Min_SimpleField(t *testing.T) {
	arr := []any{
		map[string]any{"name": "expensive", "price": 300.0},
		map[string]any{"name": "cheapest", "price": 50.0},
		map[string]any{"name": "mid", "price": 150.0},
	}
	result, err := applySelection(arr, &plan.SelectionConfig{
		Strategy: "min",
		Field:    "price",
	})
	require.NoError(t, err)
	m := result.element.(map[string]any)
	assert.Equal(t, "cheapest", m["name"])
	assert.Equal(t, 1, result.index)
}

func TestApplySelection_Min_WithSortField(t *testing.T) {
	arr := []any{
		map[string]any{"id": "A", "score": 80.0},
		map[string]any{"id": "B", "score": 20.0},
		map[string]any{"id": "C", "score": 50.0},
	}
	result, err := applySelection(arr, &plan.SelectionConfig{
		Strategy:  "min",
		Field:     "id",
		SortField: "score",
	})
	require.NoError(t, err)
	m := result.element.(map[string]any)
	assert.Equal(t, "B", m["id"])
	assert.Equal(t, 1, result.index)
}

func TestApplySelection_Min_NestedField(t *testing.T) {
	arr := []any{
		map[string]any{"id": "A", "details": map[string]any{"weight": 30.0}},
		map[string]any{"id": "B", "details": map[string]any{"weight": 10.0}},
		map[string]any{"id": "C", "details": map[string]any{"weight": 20.0}},
	}
	result, err := applySelection(arr, &plan.SelectionConfig{
		Strategy:  "min",
		Field:     "id",
		SortField: "details.weight",
	})
	require.NoError(t, err)
	m := result.element.(map[string]any)
	assert.Equal(t, "B", m["id"])
}

func TestApplySelection_Max(t *testing.T) {
	arr := []any{
		map[string]any{"name": "low", "rating": 2.5},
		map[string]any{"name": "high", "rating": 9.1},
		map[string]any{"name": "mid", "rating": 5.0},
	}
	result, err := applySelection(arr, &plan.SelectionConfig{
		Strategy: "max",
		Field:    "rating",
	})
	require.NoError(t, err)
	m := result.element.(map[string]any)
	assert.Equal(t, "high", m["name"])
	assert.Equal(t, 1, result.index)
}

func TestApplySelection_Match_Found(t *testing.T) {
	arr := []any{
		map[string]any{"id": "x1", "carrier": "AA"},
		map[string]any{"id": "x2", "carrier": "UA"},
		map[string]any{"id": "x3", "carrier": "DL"},
	}
	result, err := applySelection(arr, &plan.SelectionConfig{
		Strategy: "match",
		Filter:   "carrier == 'UA'",
	})
	require.NoError(t, err)
	m := result.element.(map[string]any)
	assert.Equal(t, "x2", m["id"])
	assert.Equal(t, 1, result.index)
}

func TestApplySelection_Match_NotFound(t *testing.T) {
	arr := []any{
		map[string]any{"id": "x1", "carrier": "AA"},
		map[string]any{"id": "x2", "carrier": "UA"},
	}
	_, err := applySelection(arr, &plan.SelectionConfig{
		Strategy: "match",
		Filter:   "carrier == 'DL'",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no element matches")
}

func TestApplySelection_Filter_PlusFirst(t *testing.T) {
	arr := []any{
		map[string]any{"id": "a1", "stops": 1.0},
		map[string]any{"id": "a2", "stops": 0.0},
		map[string]any{"id": "a3", "stops": 0.0},
	}
	result, err := applySelection(arr, &plan.SelectionConfig{
		Strategy: "first",
		Filter:   "stops == 0",
	})
	require.NoError(t, err)
	m := result.element.(map[string]any)
	assert.Equal(t, "a2", m["id"])
	assert.Equal(t, 0, result.index)
	assert.Equal(t, 2, result.filteredSize) // 2 elements passed filter
}

func TestApplySelection_Filter_PlusMin(t *testing.T) {
	arr := []any{
		map[string]any{"id": "a1", "carrier": "AA", "price": 500.0},
		map[string]any{"id": "a2", "carrier": "UA", "price": 200.0},
		map[string]any{"id": "a3", "carrier": "AA", "price": 300.0},
		map[string]any{"id": "a4", "carrier": "UA", "price": 100.0},
	}
	result, err := applySelection(arr, &plan.SelectionConfig{
		Strategy: "min",
		Field:    "price",
		Filter:   "carrier == 'AA'",
	})
	require.NoError(t, err)
	m := result.element.(map[string]any)
	assert.Equal(t, "a3", m["id"]) // cheapest AA flight
	assert.Equal(t, 2, result.filteredSize)
}

func TestApplySelection_Filter_ProducesEmpty(t *testing.T) {
	arr := []any{
		map[string]any{"id": "a1", "carrier": "AA"},
		map[string]any{"id": "a2", "carrier": "UA"},
	}
	_, err := applySelection(arr, &plan.SelectionConfig{
		Strategy: "first",
		Filter:   "carrier == 'DL'",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "matched no elements")
}

func TestApplySelection_EmptyArray(t *testing.T) {
	_, err := applySelection([]any{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "array is empty")
}

func TestApplySelection_UnknownStrategy(t *testing.T) {
	arr := []any{"a"}
	_, err := applySelection(arr, &plan.SelectionConfig{Strategy: "bogus"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown selection strategy")
}

func TestApplySelection_Min_IntegerValues(t *testing.T) {
	// Test that integer values work with min/max (coerced to float64)
	arr := []any{
		map[string]any{"id": "A", "count": 10},
		map[string]any{"id": "B", "count": 5},
		map[string]any{"id": "C", "count": 15},
	}
	result, err := applySelection(arr, &plan.SelectionConfig{
		Strategy: "min",
		Field:    "count",
	})
	require.NoError(t, err)
	m := result.element.(map[string]any)
	assert.Equal(t, "B", m["id"])
}

func TestElementToMap_AlreadyMap(t *testing.T) {
	m := map[string]any{"key": "value"}
	result, err := elementToMap(m)
	require.NoError(t, err)
	assert.Equal(t, m, result)
}

func TestElementToMap_NonMap(t *testing.T) {
	// Strings can't be converted to maps
	_, err := elementToMap("just a string")
	require.Error(t, err)
}

func TestToFloat64_Types(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected float64
		wantErr  bool
	}{
		{"float64", 42.5, 42.5, false},
		{"float32", float32(3.14), 3.140000104904175, false},
		{"int", 42, 42.0, false},
		{"int64", int64(100), 100.0, false},
		{"int32", int32(50), 50.0, false},
		{"string", "not a number", 0, true},
		{"bool", true, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := toFloat64(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.InDelta(t, tt.expected, result, 0.001)
			}
		})
	}
}
