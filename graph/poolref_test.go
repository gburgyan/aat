package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/internal/yamlx"
)

func TestInputDefault_PoolRefYAML(t *testing.T) {
	var d InputDefault
	require.NoError(t, yamlx.Decode([]byte("poolRef: airportCodes.us\nconstraint: value != origin\n"), &d))
	assert.Equal(t, "airportCodes.us", d.PoolRef)
	assert.True(t, d.HasValue())
	assert.False(t, d.IsLiteralOnly())
}

func TestMergeInputDefault_PoolRef(t *testing.T) {
	base := &InputDefault{Pool: []any{"JFK", "ORD"}, Constraint: "value != origin"}

	got := MergeInputDefault(base, &InputDefault{PoolRef: "airportCodes.eu"})
	assert.Equal(t, "airportCodes.eu", got.PoolRef)
	assert.Nil(t, got.Pool, "a layer's poolRef replaces the base's pool")
	assert.Equal(t, "value != origin", got.Constraint, "the base's constraint stays")

	got = MergeInputDefault(&InputDefault{PoolRef: "airportCodes"}, &InputDefault{Value: "JFK"})
	assert.Empty(t, got.PoolRef, "a layer's value replaces the base's poolRef")
	assert.Equal(t, "JFK", got.Value)
}

func TestDefaultShapeError_PoolAndPoolRef(t *testing.T) {
	assert.Equal(t, "set pool or poolRef, not both",
		DefaultShapeError(&InputDefault{Pool: []any{"A"}, PoolRef: "codes"}, "string"))
	assert.Empty(t, DefaultShapeError(&InputDefault{PoolRef: "codes"}, "string"))
}

func TestPoolRefs(t *testing.T) {
	g := &Graph{Nodes: map[string]*Node{
		"search": {Name: "search", Inputs: []Input{
			{Name: "origin", Default: &InputDefault{PoolRef: "airportCodes.us"}},
			{Name: "cabin", Default: &InputDefault{Value: "economy"}},
		}},
	}}
	assert.Equal(t, []PoolRefUse{{Where: "node search input origin", Ref: "airportCodes.us"}}, g.PoolRefs())

	l := &Layer{Name: "eu", Inputs: map[string]*InputDefault{"origin": {PoolRef: "airportCodes.eu"}, "cabin": {Value: "business"}}}
	assert.Equal(t, []PoolRefUse{{Where: "layer eu: origin", Ref: "airportCodes.eu"}}, l.PoolRefs())
}
