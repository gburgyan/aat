package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestComputeAutoWireEdges_Basic(t *testing.T) {
	g := &Graph{
		AutoWire: AutoWireSources,
		Nodes: map[string]*Node{
			"producer": {
				Name: "producer", Adapter: "p",
				Outputs: []Output{{Name: "token", Type: "string"}},
			},
			"consumer": {
				Name: "consumer", Adapter: "c",
				Inputs:  []Input{{Name: "token", Type: "string"}},
				Outputs: []Output{{Name: "result", Type: "string"}},
			},
		},
	}

	ComputeAutoWireEdges(g)

	require.Len(t, g.Edges, 1)
	assert.Equal(t, "producer.token", g.Edges[0].From)
	assert.Equal(t, "consumer.token", g.Edges[0].To)
	assert.True(t, g.Edges[0].AutoWired)
}

func TestComputeAutoWireEdges_ExplicitOverride(t *testing.T) {
	g := &Graph{
		AutoWire: AutoWireSources,
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a", Outputs: []Output{{Name: "id", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Outputs: []Output{{Name: "id", Type: "string"}}},
			"c": {
				Name: "c", Adapter: "c",
				Inputs:  []Input{{Name: "id", Type: "string"}},
				Outputs: []Output{{Name: "out", Type: "string"}},
			},
		},
		Edges: []Edge{
			{From: "a.id", To: "c.id"}, // explicit edge
		},
	}

	ComputeAutoWireEdges(g)

	// Should only have the original explicit edge — no auto-wire since explicit covers c.id
	assert.Len(t, g.Edges, 1)
	assert.False(t, g.Edges[0].AutoWired)
}

func TestComputeAutoWireEdges_NoAutoWireInput(t *testing.T) {
	g := &Graph{
		AutoWire: AutoWireSources,
		Nodes: map[string]*Node{
			"producer": {
				Name: "producer", Adapter: "p",
				Outputs: []Output{{Name: "token", Type: "string"}},
			},
			"consumer": {
				Name: "consumer", Adapter: "c",
				Inputs:  []Input{{Name: "token", Type: "string", NoAutoWire: true}},
				Outputs: []Output{{Name: "out", Type: "string"}},
			},
		},
	}

	ComputeAutoWireEdges(g)
	assert.Empty(t, g.Edges)
}

func TestComputeAutoWireEdges_SkipsArrayOutputs(t *testing.T) {
	g := &Graph{
		AutoWire: AutoWireSources,
		Nodes: map[string]*Node{
			"search": {
				Name: "search", Adapter: "s",
				Outputs: []Output{{Name: "items", Type: "item[]"}},
			},
			"detail": {
				Name: "detail", Adapter: "d",
				Inputs:  []Input{{Name: "items", Type: "string"}},
				Outputs: []Output{{Name: "out", Type: "string"}},
			},
		},
	}

	ComputeAutoWireEdges(g)
	assert.Empty(t, g.Edges, "array outputs should not be auto-wired")
}

func TestComputeAutoWireEdges_TypeMismatchWarning(t *testing.T) {
	g := &Graph{
		AutoWire: AutoWireSources,
		Version:  "1.0.0",
		Nodes: map[string]*Node{
			"producer": {
				Name: "producer", Adapter: "p",
				Outputs: []Output{{Name: "count", Type: "integer"}},
			},
			"consumer": {
				Name: "consumer", Adapter: "c",
				Inputs:  []Input{{Name: "count", Type: "string"}},
				Outputs: []Output{{Name: "out", Type: "string"}},
			},
		},
	}

	ComputeAutoWireEdges(g)

	// Edge should still be created despite type mismatch
	require.Len(t, g.Edges, 1)
	assert.True(t, g.Edges[0].AutoWired)

	// But warning should be generated
	warnings := ValidateWarnings(g)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "type mismatch")
	assert.Contains(t, warnings[0], "integer")
	assert.Contains(t, warnings[0], "string")
}

func TestComputeAutoWireEdges_Disabled(t *testing.T) {
	g := &Graph{
		AutoWire: AutoWireOff,
		Nodes: map[string]*Node{
			"producer": {
				Name: "producer", Adapter: "p",
				Outputs: []Output{{Name: "token", Type: "string"}},
			},
			"consumer": {
				Name: "consumer", Adapter: "c",
				Inputs:  []Input{{Name: "token", Type: "string"}},
				Outputs: []Output{{Name: "out", Type: "string"}},
			},
		},
	}

	ComputeAutoWireEdges(g)
	assert.Empty(t, g.Edges)
}

func TestComputeAutoWireEdges_NoDuplicates(t *testing.T) {
	g := &Graph{
		AutoWire: AutoWireSources,
		Nodes: map[string]*Node{
			"producer": {
				Name: "producer", Adapter: "p",
				Outputs: []Output{{Name: "token", Type: "string"}},
			},
			"consumer": {
				Name: "consumer", Adapter: "c",
				Inputs:  []Input{{Name: "token", Type: "string"}},
				Outputs: []Output{{Name: "out", Type: "string"}},
			},
		},
	}

	ComputeAutoWireEdges(g)
	edgeCount := len(g.Edges)
	// Running again should not create duplicates because the first run's edges
	// become "explicit" targets for the second run
	ComputeAutoWireEdges(g)
	assert.Equal(t, edgeCount, len(g.Edges), "running twice should not duplicate edges")
}

func TestComputeAutoWireEdges_NoSelfWire(t *testing.T) {
	g := &Graph{
		AutoWire: AutoWireSources,
		Nodes: map[string]*Node{
			"node": {
				Name: "node", Adapter: "n",
				Inputs:  []Input{{Name: "data", Type: "string"}},
				Outputs: []Output{{Name: "data", Type: "string"}},
			},
		},
	}

	ComputeAutoWireEdges(g)
	assert.Empty(t, g.Edges, "should not wire a node to itself")
}

func TestComputeAutoWireEdges_MultipleProducers(t *testing.T) {
	g := &Graph{
		AutoWire: AutoWireSources,
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a", Outputs: []Output{{Name: "id", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Outputs: []Output{{Name: "id", Type: "string"}}},
			"c": {
				Name: "c", Adapter: "c",
				Inputs:  []Input{{Name: "id", Type: "string"}},
				Outputs: []Output{{Name: "out", Type: "string"}},
			},
		},
	}

	ComputeAutoWireEdges(g)

	// Both a.id→c.id and b.id→c.id should be created
	assert.Len(t, g.Edges, 2)
	for _, e := range g.Edges {
		assert.True(t, e.AutoWired)
		assert.Equal(t, "c.id", e.To)
	}
}

func TestComputeAutoWireEdges_SkipsOptionalInputs(t *testing.T) {
	g := &Graph{
		AutoWire: AutoWireSources,
		Nodes: map[string]*Node{
			"producer": {
				Name: "producer", Adapter: "p",
				Outputs: []Output{{Name: "hint", Type: "string"}},
			},
			"consumer": {
				Name: "consumer", Adapter: "c",
				Inputs:  []Input{{Name: "hint", Type: "string", Optional: true}},
				Outputs: []Output{{Name: "out", Type: "string"}},
			},
		},
	}

	ComputeAutoWireEdges(g)
	assert.Empty(t, g.Edges, "optional inputs should not be auto-wired")
}

func TestComputeAutoWireEdges_SourcesSkipsPassThrough(t *testing.T) {
	// Node "middle" both inputs and outputs "token" — it's a pass-through.
	// In sources mode, it should NOT be used as a producer for "token".
	g := &Graph{
		AutoWire: AutoWireSources,
		Nodes: map[string]*Node{
			"origin": {
				Name: "origin", Adapter: "o",
				Outputs: []Output{{Name: "token", Type: "string"}},
			},
			"middle": {
				Name: "middle", Adapter: "m",
				Inputs:  []Input{{Name: "token", Type: "string"}},
				Outputs: []Output{{Name: "token", Type: "string"}, {Name: "extra", Type: "string"}},
			},
			"consumer": {
				Name: "consumer", Adapter: "c",
				Inputs:  []Input{{Name: "token", Type: "string"}},
				Outputs: []Output{{Name: "out", Type: "string"}},
			},
		},
	}

	ComputeAutoWireEdges(g)

	// Only origin.token should wire to consumer.token and middle.token.
	// middle.token should NOT wire to consumer.token (pass-through skipped).
	var fromRefs []string
	for _, e := range g.Edges {
		fromRefs = append(fromRefs, e.From)
	}
	assert.Contains(t, fromRefs, "origin.token")
	assert.NotContains(t, fromRefs, "middle.token", "pass-through output should be skipped in sources mode")

	// middle.extra is NOT a pass-through (no input named "extra"), so it should not be affected.
	// But no consumer has an "extra" input, so no edge for it.
}

func TestComputeAutoWireEdges_AllIncludesPassThrough(t *testing.T) {
	// Same graph as above, but with AutoWireAll — pass-throughs should be included.
	g := &Graph{
		AutoWire: AutoWireAll,
		Nodes: map[string]*Node{
			"origin": {
				Name: "origin", Adapter: "o",
				Outputs: []Output{{Name: "token", Type: "string"}},
			},
			"middle": {
				Name: "middle", Adapter: "m",
				Inputs:  []Input{{Name: "token", Type: "string"}},
				Outputs: []Output{{Name: "token", Type: "string"}},
			},
			"consumer": {
				Name: "consumer", Adapter: "c",
				Inputs:  []Input{{Name: "token", Type: "string"}},
				Outputs: []Output{{Name: "out", Type: "string"}},
			},
		},
	}

	ComputeAutoWireEdges(g)

	// Both origin.token and middle.token should wire to consumer.token
	var toConsumer []string
	for _, e := range g.Edges {
		if e.To == "consumer.token" {
			toConsumer = append(toConsumer, e.From)
		}
	}
	assert.Contains(t, toConsumer, "origin.token")
	assert.Contains(t, toConsumer, "middle.token", "pass-through should be included in all mode")
}

func TestAutoWireMode_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		yaml string
		want AutoWireMode
	}{
		{`autoWire: true`, AutoWireSources},
		{`autoWire: false`, AutoWireOff},
		{`autoWire: sources`, AutoWireSources},
		{`autoWire: "sources"`, AutoWireSources},
		{`autoWire: all`, AutoWireAll},
		{`autoWire: "all"`, AutoWireAll},
	}
	for _, tt := range tests {
		t.Run(tt.yaml, func(t *testing.T) {
			var out struct {
				AutoWire AutoWireMode `yaml:"autoWire"`
			}
			err := yaml.Unmarshal([]byte(tt.yaml), &out)
			require.NoError(t, err)
			assert.Equal(t, tt.want, out.AutoWire)
		})
	}
}

func TestAutoWireMode_UnmarshalYAML_Invalid(t *testing.T) {
	var out struct {
		AutoWire AutoWireMode `yaml:"autoWire"`
	}
	err := yaml.Unmarshal([]byte(`autoWire: bogus`), &out)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}

func TestAutoWireMode_IsEnabled(t *testing.T) {
	assert.False(t, AutoWireOff.IsEnabled())
	assert.True(t, AutoWireSources.IsEnabled())
	assert.True(t, AutoWireAll.IsEnabled())
}

func TestAutoWireMode_MarshalYAML(t *testing.T) {
	val, err := AutoWireSources.MarshalYAML()
	require.NoError(t, err)
	assert.Equal(t, true, val)

	val, err = AutoWireAll.MarshalYAML()
	require.NoError(t, err)
	assert.Equal(t, "all", val)

	val, err = AutoWireOff.MarshalYAML()
	require.NoError(t, err)
	assert.Equal(t, false, val)
}
