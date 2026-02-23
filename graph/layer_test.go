package graph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLayer_ScalarValue(t *testing.T) {
	yaml := `
name: amex
description: American Express card
inputs:
  cardType: Amex
  cardVendorCode: AX
`
	layer, err := ParseLayer([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "amex", layer.Name)
	assert.Equal(t, "American Express card", layer.Description)
	require.Len(t, layer.Inputs, 2)
	assert.Equal(t, "Amex", layer.Inputs["cardType"].Value)
	assert.Equal(t, "AX", layer.Inputs["cardVendorCode"].Value)
}

func TestParseLayer_PoolShorthand(t *testing.T) {
	yaml := `
name: european
inputs:
  origin: [CDG, LHR, FRA]
  destination: [AMS, FCO, MAD]
`
	layer, err := ParseLayer([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "european", layer.Name)
	assert.Equal(t, []any{"CDG", "LHR", "FRA"}, layer.Inputs["origin"].Pool)
	assert.Equal(t, []any{"AMS", "FCO", "MAD"}, layer.Inputs["destination"].Pool)
}

func TestParseLayer_MapSyntax(t *testing.T) {
	yaml := `
name: near-term
inputs:
  departureDate:
    pool: ["2026-03-01", "2026-03-02"]
    constraint: "date > today"
`
	layer, err := ParseLayer([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "near-term", layer.Name)
	d := layer.Inputs["departureDate"]
	require.NotNil(t, d)
	assert.Equal(t, []any{"2026-03-01", "2026-03-02"}, d.Pool)
	assert.Equal(t, "date > today", d.Constraint)
}

func TestParseLayer_QualifiedAddress(t *testing.T) {
	yaml := `
name: specific
inputs:
  origin: [CDG, LHR]
  searchMultiCity.leg2Destination: [FCO, MAD]
`
	layer, err := ParseLayer([]byte(yaml))
	require.NoError(t, err)
	require.Contains(t, layer.Inputs, "origin")
	require.Contains(t, layer.Inputs, "searchMultiCity.leg2Destination")
	assert.Equal(t, []any{"FCO", "MAD"}, layer.Inputs["searchMultiCity.leg2Destination"].Pool)
}

func TestParseLayer_MissingName(t *testing.T) {
	yaml := `
inputs:
  origin: CDG
`
	_, err := ParseLayer([]byte(yaml))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name")
}

func TestParseLayer_InvalidYAML(t *testing.T) {
	_, err := ParseLayer([]byte(":::invalid"))
	assert.Error(t, err)
}

func TestParseLayerFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	content := `
name: test-layer
inputs:
  origin: CDG
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	layer, err := ParseLayerFile(path)
	require.NoError(t, err)
	assert.Equal(t, "test-layer", layer.Name)
}

func TestParseLayerFile_NotFound(t *testing.T) {
	_, err := ParseLayerFile("/nonexistent/layer.yaml")
	assert.Error(t, err)
}

func TestLoadLayersFromDir(t *testing.T) {
	dir := t.TempDir()
	writeLayer(t, dir, "european.yaml", "european", "origin: [CDG, LHR]")
	writeLayer(t, dir, "amex.yaml", "amex", "cardType: Amex")

	layers, err := LoadLayersFromDir(dir)
	require.NoError(t, err)
	assert.Len(t, layers, 2)
	assert.Contains(t, layers, "european")
	assert.Contains(t, layers, "amex")
}

func TestLoadLayersFromDir_DuplicateName(t *testing.T) {
	dir := t.TempDir()
	writeLayer(t, dir, "a.yaml", "same-name", "origin: CDG")
	writeLayer(t, dir, "b.yaml", "same-name", "origin: LHR")

	_, err := LoadLayersFromDir(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestLoadLayersFromDir_SkipsNonYAML(t *testing.T) {
	dir := t.TempDir()
	writeLayer(t, dir, "layer.yaml", "good", "origin: CDG")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# hi"), 0o644))

	layers, err := LoadLayersFromDir(dir)
	require.NoError(t, err)
	assert.Len(t, layers, 1)
}

func TestLoadLayersFromDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	layers, err := LoadLayersFromDir(dir)
	require.NoError(t, err)
	assert.Len(t, layers, 0)
}

func TestLoadLayersFromDir_YmlExtension(t *testing.T) {
	dir := t.TempDir()
	writeLayer(t, dir, "test.yml", "yml-layer", "origin: CDG")

	layers, err := LoadLayersFromDir(dir)
	require.NoError(t, err)
	assert.Contains(t, layers, "yml-layer")
}

func TestResolveLayerNames(t *testing.T) {
	dir := t.TempDir()
	writeLayer(t, dir, "european.yaml", "european", "origin: [CDG, LHR]")
	writeLayer(t, dir, "amex.yaml", "amex", "cardType: Amex")
	writeLayer(t, dir, "unused.yaml", "unused", "foo: bar")

	layers, err := ResolveLayerNames([]string{"european", "amex"}, dir)
	require.NoError(t, err)
	assert.Len(t, layers, 2)
	assert.Contains(t, layers, "european")
	assert.Contains(t, layers, "amex")
}

func TestResolveLayerNames_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeLayer(t, dir, "european.yaml", "european", "origin: CDG")

	_, err := ResolveLayerNames([]string{"european", "missing"}, dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

func TestMergeInputDefault_NilBase(t *testing.T) {
	overlay := &InputDefault{Value: "CDG"}
	result := MergeInputDefault(nil, overlay)
	assert.Equal(t, "CDG", result.Value)
}

func TestMergeInputDefault_NilOverlay(t *testing.T) {
	base := &InputDefault{Value: "JFK"}
	result := MergeInputDefault(base, nil)
	assert.Equal(t, "JFK", result.Value)
	// Verify it's a copy, not the same pointer
	base.Value = "LAX"
	assert.Equal(t, "JFK", result.Value)
}

func TestMergeInputDefault_BothNil(t *testing.T) {
	result := MergeInputDefault(nil, nil)
	assert.Nil(t, result)
}

func TestMergeInputDefault_OverlayPool(t *testing.T) {
	base := &InputDefault{Value: "JFK"}
	overlay := &InputDefault{Pool: []any{"CDG", "LHR"}}
	result := MergeInputDefault(base, overlay)
	assert.Nil(t, result.Value)
	assert.Equal(t, []any{"CDG", "LHR"}, result.Pool)
}

func TestMergeInputDefault_OverlayValue(t *testing.T) {
	base := &InputDefault{Pool: []any{"JFK", "LAX"}}
	overlay := &InputDefault{Value: "CDG"}
	result := MergeInputDefault(base, overlay)
	assert.Equal(t, "CDG", result.Value)
	assert.Nil(t, result.Pool)
}

func TestMergeInputDefault_OverlayPoolStrategy(t *testing.T) {
	random := "random"
	sequential := "sequential"
	base := &InputDefault{Pool: []any{"A", "B"}, PoolStrategy: &random}
	overlay := &InputDefault{PoolStrategy: &sequential}
	result := MergeInputDefault(base, overlay)
	assert.Equal(t, "sequential", *result.PoolStrategy)
	assert.Equal(t, []any{"A", "B"}, result.Pool)
}

func TestMergeInputDefault_OverlayConstraint(t *testing.T) {
	base := &InputDefault{Pool: []any{"A", "B"}, Constraint: "old > 1"}
	overlay := &InputDefault{Constraint: "new > 2"}
	result := MergeInputDefault(base, overlay)
	assert.Equal(t, "new > 2", result.Constraint)
}

func TestMergeInputDefault_OverlayFrom(t *testing.T) {
	base := &InputDefault{From: "nodeA.fieldA"}
	overlay := &InputDefault{From: "nodeB.fieldB"}
	result := MergeInputDefault(base, overlay)
	assert.Equal(t, "nodeB.fieldB", result.From)
}

func TestMergeInputDefault_OverlaySelect(t *testing.T) {
	base := &InputDefault{Select: &InputDefaultSelect{Strategy: "first"}}
	overlay := &InputDefault{Select: &InputDefaultSelect{Strategy: "random", Filter: "x > 1"}}
	result := MergeInputDefault(base, overlay)
	assert.Equal(t, "random", result.Select.Strategy)
	assert.Equal(t, "x > 1", result.Select.Filter)
}

func TestMergeInputDefault_PartialOverlay(t *testing.T) {
	base := &InputDefault{
		Pool:       []any{"A", "B"},
		Constraint: "x > 0",
	}
	overlay := &InputDefault{Constraint: "x > 5"} // only constraint changes
	result := MergeInputDefault(base, overlay)
	assert.Equal(t, []any{"A", "B"}, result.Pool) // preserved
	assert.Equal(t, "x > 5", result.Constraint)   // overridden
}

func TestApplyLayers_SingleLayer_BareEntry(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"searchFlights": {
				Name: "searchFlights",
				Inputs: []Input{
					{Name: "origin", Default: &InputDefault{Pool: []any{"JFK", "LAX"}}},
					{Name: "destination", Default: &InputDefault{Value: "LHR"}},
				},
			},
			"searchHotels": {
				Name: "searchHotels",
				Inputs: []Input{
					{Name: "origin", Default: &InputDefault{Value: "NYC"}},
				},
			},
		},
	}

	layers := map[string]*Layer{
		"european": {
			Name:   "european",
			Inputs: map[string]*InputDefault{"origin": {Pool: []any{"CDG", "FRA"}}},
		},
	}

	result, err := ApplyLayers(g, []string{"european"}, layers)
	require.NoError(t, err)

	// Both nodes' origin should now have the European pool
	sf := result["searchFlights.origin"]
	require.NotNil(t, sf)
	assert.Equal(t, []any{"CDG", "FRA"}, sf.Pool)
	assert.Nil(t, sf.Value)

	sh := result["searchHotels.origin"]
	require.NotNil(t, sh)
	assert.Equal(t, []any{"CDG", "FRA"}, sh.Pool)
	assert.Nil(t, sh.Value)

	// Destination unchanged
	dest := result["searchFlights.destination"]
	require.NotNil(t, dest)
	assert.Equal(t, "LHR", dest.Value)
}

func TestApplyLayers_QualifiedOverridesBare(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"nodeA": {
				Name: "nodeA",
				Inputs: []Input{
					{Name: "origin", Default: &InputDefault{Value: "JFK"}},
				},
			},
			"nodeB": {
				Name: "nodeB",
				Inputs: []Input{
					{Name: "origin", Default: &InputDefault{Value: "LAX"}},
				},
			},
		},
	}

	layers := map[string]*Layer{
		"mixed": {
			Name: "mixed",
			Inputs: map[string]*InputDefault{
				"origin":        {Pool: []any{"CDG", "FRA"}},           // bare: all nodes
				"nodeB.origin":  {Value: "AMS"},                        // qualified: nodeB only
			},
		},
	}

	result, err := ApplyLayers(g, []string{"mixed"}, layers)
	require.NoError(t, err)

	// nodeA gets bare override
	a := result["nodeA.origin"]
	require.NotNil(t, a)
	assert.Equal(t, []any{"CDG", "FRA"}, a.Pool)

	// nodeB gets qualified override (trumps bare within same layer)
	b := result["nodeB.origin"]
	require.NotNil(t, b)
	assert.Equal(t, "AMS", b.Value)
	assert.Nil(t, b.Pool) // qualified set value, which clears pool
}

func TestApplyLayers_StackedLayers(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"search": {
				Name: "search",
				Inputs: []Input{
					{Name: "origin", Default: &InputDefault{Pool: []any{"JFK", "LAX"}}},
					{Name: "cardType"},
				},
			},
		},
	}

	layers := map[string]*Layer{
		"european": {
			Name:   "european",
			Inputs: map[string]*InputDefault{"origin": {Pool: []any{"CDG", "FRA"}}},
		},
		"amex": {
			Name:   "amex",
			Inputs: map[string]*InputDefault{"cardType": {Value: "Amex"}},
		},
	}

	result, err := ApplyLayers(g, []string{"european", "amex"}, layers)
	require.NoError(t, err)

	origin := result["search.origin"]
	require.NotNil(t, origin)
	assert.Equal(t, []any{"CDG", "FRA"}, origin.Pool)

	card := result["search.cardType"]
	require.NotNil(t, card)
	assert.Equal(t, "Amex", card.Value)
}

func TestApplyLayers_LaterLayerOverridesEarlier(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"search": {
				Name: "search",
				Inputs: []Input{
					{Name: "origin", Default: &InputDefault{Value: "JFK"}},
				},
			},
		},
	}

	layers := map[string]*Layer{
		"layer1": {
			Name:   "layer1",
			Inputs: map[string]*InputDefault{"origin": {Value: "CDG"}},
		},
		"layer2": {
			Name:   "layer2",
			Inputs: map[string]*InputDefault{"origin": {Value: "AMS"}},
		},
	}

	result, err := ApplyLayers(g, []string{"layer1", "layer2"}, layers)
	require.NoError(t, err)

	assert.Equal(t, "AMS", result["search.origin"].Value)
}

func TestApplyLayers_MissingLayerName(t *testing.T) {
	g := &Graph{Nodes: map[string]*Node{}}
	layers := map[string]*Layer{}

	_, err := ApplyLayers(g, []string{"nonexistent"}, layers)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestApplyLayers_EmptyLayers(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"search": {
				Name: "search",
				Inputs: []Input{
					{Name: "origin", Default: &InputDefault{Value: "JFK"}},
				},
			},
		},
	}

	result, err := ApplyLayers(g, []string{}, map[string]*Layer{})
	require.NoError(t, err)
	// Should contain the graph default
	assert.Equal(t, "JFK", result["search.origin"].Value)
}

func TestApplyLayers_NoGraphDefault(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"search": {
				Name: "search",
				Inputs: []Input{
					{Name: "origin"}, // no default
				},
			},
		},
	}

	layers := map[string]*Layer{
		"european": {
			Name:   "european",
			Inputs: map[string]*InputDefault{"origin": {Pool: []any{"CDG"}}},
		},
	}

	result, err := ApplyLayers(g, []string{"european"}, layers)
	require.NoError(t, err)

	// Layer should add a default even when none existed in the graph
	origin := result["search.origin"]
	require.NotNil(t, origin)
	assert.Equal(t, []any{"CDG"}, origin.Pool)
}

func TestApplyLayers_QualifiedIgnoresUnknownNode(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"search": {
				Name:   "search",
				Inputs: []Input{{Name: "origin", Default: &InputDefault{Value: "JFK"}}},
			},
		},
	}

	layers := map[string]*Layer{
		"test": {
			Name: "test",
			Inputs: map[string]*InputDefault{
				"unknownNode.origin": {Value: "CDG"},
			},
		},
	}

	result, err := ApplyLayers(g, []string{"test"}, layers)
	require.NoError(t, err)
	// search.origin should be unchanged
	assert.Equal(t, "JFK", result["search.origin"].Value)
}

// writeLayer is a test helper that writes a layer YAML file.
func writeLayer(t *testing.T, dir, filename, name, inputsYAML string) {
	t.Helper()
	content := "name: " + name + "\ninputs:\n"
	for _, line := range splitLines(inputsYAML) {
		content += "  " + line + "\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644))
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range []string{s} {
		lines = append(lines, line)
	}
	return lines
}
