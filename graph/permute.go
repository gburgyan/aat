package graph

import (
	"sort"
	"strings"
)

// LayerPermutation represents one combination from the cartesian product
// of layer groups. Layers contains only the group-selected layer names
// (excludes base layers). Label is a human-readable string for display/grouping.
type LayerPermutation struct {
	Layers []string // group-selected layer names (excludes base layers)
	Label  string   // human-readable: "european, amex" or "(base)"
}

// GenerateLayerPermutations computes the cartesian product of layer groups,
// where each group implicitly includes a "none" entry. Returns at least one
// permutation (the base case with no group layers).
//
// For example, groups [["european","international"],["amex"]] produces:
//
//	(base), amex, european, european+amex, international, international+amex
func GenerateLayerPermutations(groups [][]string) []LayerPermutation {
	if len(groups) == 0 {
		return []LayerPermutation{{Label: "(base)"}}
	}

	// Build extended groups: each group gets a nil entry prepended.
	// nil means "none selected from this group".
	type entry struct {
		name string
		none bool
	}
	extended := make([][]entry, len(groups))
	for i, g := range groups {
		ext := []entry{{none: true}}
		for _, name := range g {
			ext = append(ext, entry{name: name})
		}
		extended[i] = ext
	}

	// Cartesian product via iterative expansion.
	type combo []entry
	product := []combo{{}}
	for _, group := range extended {
		var next []combo
		for _, existing := range product {
			for _, e := range group {
				c := make(combo, len(existing)+1)
				copy(c, existing)
				c[len(existing)] = e
				next = append(next, c)
			}
		}
		product = next
	}

	// Convert combos to LayerPermutations.
	perms := make([]LayerPermutation, 0, len(product))
	for _, c := range product {
		var layers []string
		for _, e := range c {
			if !e.none {
				layers = append(layers, e.name)
			}
		}

		label := buildPermutationLabel(layers)
		perms = append(perms, LayerPermutation{
			Layers: layers,
			Label:  label,
		})
	}

	// Sort by label for deterministic output. "(base)" sorts first due to "(".
	sort.Slice(perms, func(i, j int) bool {
		return perms[i].Label < perms[j].Label
	})

	return perms
}

// buildPermutationLabel creates a human-readable label from the selected layers.
// Layers are sorted alphabetically and joined with ", ". Empty = "(base)".
func buildPermutationLabel(layers []string) string {
	if len(layers) == 0 {
		return "(base)"
	}
	sorted := make([]string, len(layers))
	copy(sorted, layers)
	sort.Strings(sorted)
	return strings.Join(sorted, ", ")
}
