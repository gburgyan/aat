package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFile_TravelFixture(t *testing.T) {
	kb, err := ParseFile("testdata/valid/travel.yaml")
	require.NoError(t, err)
	require.NotNil(t, kb)

	assert.Len(t, kb.Concepts, 13)
	assert.Len(t, kb.Types, 6)
	assert.Len(t, kb.ValuePools, 6)
}

func TestParseFile_MinimalFixture(t *testing.T) {
	kb, err := ParseFile("testdata/valid/minimal.yaml")
	require.NoError(t, err)
	require.NotNil(t, kb)

	assert.Len(t, kb.Concepts, 1)
	assert.Nil(t, kb.Types)
	assert.Nil(t, kb.ValuePools)
}

func TestParse_PopulatesNamesFromMapKeys(t *testing.T) {
	kb, err := ParseFile("testdata/valid/travel.yaml")
	require.NoError(t, err)

	// Concepts
	c := kb.Concepts["domestic"]
	require.NotNil(t, c)
	assert.Equal(t, "domestic", c.Name)

	// Types
	td := kb.Types["airportCode"]
	require.NotNil(t, td)
	assert.Equal(t, "airportCode", td.Name)

	// Value pools
	p := kb.ValuePools["usAirports"]
	require.NotNil(t, p)
	assert.Equal(t, "usAirports", p.Name)
}

func TestParse_ConceptExamples(t *testing.T) {
	kb, err := ParseFile("testdata/valid/travel.yaml")
	require.NoError(t, err)

	c := kb.Concepts["domestic"]
	require.NotNil(t, c)
	require.NotNil(t, c.Examples)
	assert.Contains(t, c.Examples, "US")
	assert.Contains(t, c.Examples["US"], "JFK-LAX")
}

func TestParse_CompositeType(t *testing.T) {
	kb, err := ParseFile("testdata/valid/travel.yaml")
	require.NoError(t, err)

	td := kb.Types["traveler"]
	require.NotNil(t, td)
	assert.Equal(t, "composite", td.Format)
	require.NotNil(t, td.Fields)
	assert.Len(t, td.Fields, 4)

	given := td.Fields["given"]
	require.NotNil(t, given)
	assert.Equal(t, "string", given.Type)
	assert.Equal(t, "generate realistic name", given.Strategy)
}

func TestParse_GroupedPool(t *testing.T) {
	kb, err := ParseFile("testdata/valid/travel.yaml")
	require.NoError(t, err)

	p := kb.ValuePools["internationalAirports"]
	require.NotNil(t, p)
	assert.Empty(t, p.Values)
	require.NotNil(t, p.Groups)
	assert.Contains(t, p.Groups, "europe")
	assert.Contains(t, p.Groups["europe"], "LHR")
}

func TestParse_MalformedYAML(t *testing.T) {
	_, err := Parse([]byte("not: [yaml: broken"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "YAML parse error")
}

func TestParse_EmptyInput(t *testing.T) {
	_, err := Parse([]byte(""))
	assert.Error(t, err)
	// Empty YAML produces an empty KB which fails validation
}

func TestParseFile_NotFound(t *testing.T) {
	_, err := ParseFile("testdata/nonexistent.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading domain file")
}

func TestParse_PoolReference(t *testing.T) {
	kb, err := ParseFile("testdata/valid/travel.yaml")
	require.NoError(t, err)

	td := kb.Types["airportCode"]
	require.NotNil(t, td)
	assert.Equal(t, "usAirports", td.Pool)
}

func TestMerge_Override(t *testing.T) {
	kb1 := &KnowledgeBase{
		Concepts: map[string]*Concept{
			"c1": {Name: "c1", Description: "original", AppliesTo: []string{"f1"}},
		},
		Types:      map[string]*TypeDef{},
		ValuePools: map[string]*ValuePool{},
	}
	kb2 := &KnowledgeBase{
		Concepts: map[string]*Concept{
			"c1": {Name: "c1", Description: "overridden", AppliesTo: []string{"f2"}},
		},
		Types:      map[string]*TypeDef{},
		ValuePools: map[string]*ValuePool{},
	}

	merged := Merge(kb1, kb2)
	assert.Equal(t, "overridden", merged.Concepts["c1"].Description)
	assert.Equal(t, []string{"f2"}, merged.Concepts["c1"].AppliesTo)
}

func TestMerge_CombinePoolValues(t *testing.T) {
	kb1 := &KnowledgeBase{
		Concepts: map[string]*Concept{},
		Types:    map[string]*TypeDef{},
		ValuePools: map[string]*ValuePool{
			"pool1": {Name: "pool1", Description: "p1", Type: "string", Values: []string{"a", "b"}},
		},
	}
	kb2 := &KnowledgeBase{
		Concepts: map[string]*Concept{},
		Types:    map[string]*TypeDef{},
		ValuePools: map[string]*ValuePool{
			"pool1": {Name: "pool1", Description: "p1", Type: "string", Values: []string{"c", "d"}},
		},
	}

	merged := Merge(kb1, kb2)
	p := merged.ValuePools["pool1"]
	require.NotNil(t, p)
	assert.Equal(t, []string{"a", "b", "c", "d"}, p.Values)
}

func TestMerge_CombinePoolGroups(t *testing.T) {
	kb1 := &KnowledgeBase{
		Concepts: map[string]*Concept{},
		Types:    map[string]*TypeDef{},
		ValuePools: map[string]*ValuePool{
			"pool1": {
				Name: "pool1", Description: "p1", Type: "string",
				Groups: map[string][]string{"g1": {"a", "b"}},
			},
		},
	}
	kb2 := &KnowledgeBase{
		Concepts: map[string]*Concept{},
		Types:    map[string]*TypeDef{},
		ValuePools: map[string]*ValuePool{
			"pool1": {
				Name: "pool1", Description: "p1", Type: "string",
				Groups: map[string][]string{"g1": {"c"}, "g2": {"d"}},
			},
		},
	}

	merged := Merge(kb1, kb2)
	p := merged.ValuePools["pool1"]
	require.NotNil(t, p)
	assert.Equal(t, []string{"a", "b", "c"}, p.Groups["g1"])
	assert.Equal(t, []string{"d"}, p.Groups["g2"])
}

func TestMerge_NilInputs(t *testing.T) {
	kb := &KnowledgeBase{
		Concepts: map[string]*Concept{
			"c1": {Name: "c1", Description: "test", AppliesTo: []string{"f"}},
		},
		Types:      map[string]*TypeDef{},
		ValuePools: map[string]*ValuePool{},
	}

	merged := Merge(nil, kb, nil)
	assert.Len(t, merged.Concepts, 1)
}

func TestMerge_DoesNotMutateOriginal(t *testing.T) {
	kb1 := &KnowledgeBase{
		Concepts: map[string]*Concept{},
		Types:    map[string]*TypeDef{},
		ValuePools: map[string]*ValuePool{
			"pool1": {Name: "pool1", Description: "p1", Type: "string", Values: []string{"a"}},
		},
	}
	kb2 := &KnowledgeBase{
		Concepts: map[string]*Concept{},
		Types:    map[string]*TypeDef{},
		ValuePools: map[string]*ValuePool{
			"pool1": {Name: "pool1", Description: "p1", Type: "string", Values: []string{"b"}},
		},
	}

	Merge(kb1, kb2)
	// Original kb1 pool should be unchanged
	assert.Equal(t, []string{"a"}, kb1.ValuePools["pool1"].Values)
}
