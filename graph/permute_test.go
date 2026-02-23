package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateLayerPermutations(t *testing.T) {
	tests := []struct {
		name     string
		groups   [][]string
		expected []LayerPermutation
	}{
		{
			name:   "nil groups returns base",
			groups: nil,
			expected: []LayerPermutation{
				{Label: "(base)"},
			},
		},
		{
			name:   "empty groups slice returns base",
			groups: [][]string{},
			expected: []LayerPermutation{
				{Label: "(base)"},
			},
		},
		{
			name:   "single group with one entry",
			groups: [][]string{{"amex"}},
			expected: []LayerPermutation{
				{Label: "(base)"},
				{Layers: []string{"amex"}, Label: "amex"},
			},
		},
		{
			name:   "single group with two entries",
			groups: [][]string{{"european", "international"}},
			expected: []LayerPermutation{
				{Label: "(base)"},
				{Layers: []string{"european"}, Label: "european"},
				{Layers: []string{"international"}, Label: "international"},
			},
		},
		{
			name:   "two groups — full example from plan",
			groups: [][]string{{"european", "international"}, {"amex"}},
			expected: []LayerPermutation{
				{Label: "(base)"},
				{Layers: []string{"amex"}, Label: "amex"},
				{Layers: []string{"european", "amex"}, Label: "amex, european"},
				{Layers: []string{"international", "amex"}, Label: "amex, international"},
				{Layers: []string{"european"}, Label: "european"},
				{Layers: []string{"international"}, Label: "international"},
			},
		},
		{
			name:   "three groups",
			groups: [][]string{{"a"}, {"b"}, {"c"}},
			expected: []LayerPermutation{
				{Label: "(base)"},
				{Layers: []string{"a"}, Label: "a"},
				{Layers: []string{"a", "b"}, Label: "a, b"},
				{Layers: []string{"a", "b", "c"}, Label: "a, b, c"},
				{Layers: []string{"a", "c"}, Label: "a, c"},
				{Layers: []string{"b"}, Label: "b"},
				{Layers: []string{"b", "c"}, Label: "b, c"},
				{Layers: []string{"c"}, Label: "c"},
			},
		},
		{
			name:   "label sorting is alphabetical",
			groups: [][]string{{"zebra", "alpha"}},
			expected: []LayerPermutation{
				{Label: "(base)"},
				{Layers: []string{"alpha"}, Label: "alpha"},
				{Layers: []string{"zebra"}, Label: "zebra"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateLayerPermutations(tt.groups)
			assert.Equal(t, len(tt.expected), len(result), "permutation count")

			for i, exp := range tt.expected {
				if i >= len(result) {
					break
				}
				got := result[i]
				assert.Equal(t, exp.Label, got.Label, "label at index %d", i)
				if exp.Layers == nil {
					assert.Empty(t, got.Layers, "layers at index %d should be empty", i)
				} else {
					assert.Equal(t, exp.Label, got.Label, "label at index %d", i)
				}
			}
		})
	}
}

func TestBuildPermutationLabel(t *testing.T) {
	assert.Equal(t, "(base)", buildPermutationLabel(nil))
	assert.Equal(t, "(base)", buildPermutationLabel([]string{}))
	assert.Equal(t, "amex", buildPermutationLabel([]string{"amex"}))
	assert.Equal(t, "amex, european", buildPermutationLabel([]string{"european", "amex"}))
	assert.Equal(t, "a, b, c", buildPermutationLabel([]string{"c", "a", "b"}))
}

func TestGenerateLayerPermutations_Counts(t *testing.T) {
	// With groups of sizes 3 and 2, we get (3+1)*(2+1) = 12 permutations.
	groups := [][]string{{"a", "b", "c"}, {"x", "y"}}
	result := GenerateLayerPermutations(groups)
	assert.Equal(t, 12, len(result))

	// Verify (base) is always first (sorts first due to parenthesis).
	assert.Equal(t, "(base)", result[0].Label)
}
