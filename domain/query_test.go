package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadTravelKB(t *testing.T) *KnowledgeBase {
	t.Helper()
	kb, err := ParseFile("testdata/valid/travel.yaml")
	require.NoError(t, err)
	return kb
}

func TestGetConcept_Found(t *testing.T) {
	kb := loadTravelKB(t)
	c := kb.GetConcept("domestic")
	require.NotNil(t, c)
	assert.Equal(t, "domestic", c.Name)
}

func TestGetConcept_NotFound(t *testing.T) {
	kb := loadTravelKB(t)
	c := kb.GetConcept("nonexistent")
	assert.Nil(t, c)
}

func TestGetConcept_NilKB(t *testing.T) {
	var kb *KnowledgeBase
	c := kb.GetConcept("anything")
	assert.Nil(t, c)
}

func TestGetType_Found(t *testing.T) {
	kb := loadTravelKB(t)
	td := kb.GetType("airportCode")
	require.NotNil(t, td)
	assert.Equal(t, "airportCode", td.Name)
}

func TestGetType_NotFound(t *testing.T) {
	kb := loadTravelKB(t)
	td := kb.GetType("nonexistent")
	assert.Nil(t, td)
}

func TestGetPool_Found(t *testing.T) {
	kb := loadTravelKB(t)
	p := kb.GetPool("usAirports")
	require.NotNil(t, p)
	assert.Equal(t, "usAirports", p.Name)
}

func TestGetPool_NotFound(t *testing.T) {
	kb := loadTravelKB(t)
	p := kb.GetPool("nonexistent")
	assert.Nil(t, p)
}

func TestConceptsForField_WithMatches(t *testing.T) {
	kb := loadTravelKB(t)
	concepts := kb.ConceptsForField("origin")
	require.True(t, len(concepts) >= 2, "expected at least 2 concepts for 'origin'")

	names := make([]string, len(concepts))
	for i, c := range concepts {
		names[i] = c.Name
	}
	assert.Contains(t, names, "domestic")
	assert.Contains(t, names, "international")
}

func TestConceptsForField_NoMatches(t *testing.T) {
	kb := loadTravelKB(t)
	concepts := kb.ConceptsForField("nonexistentField")
	assert.Empty(t, concepts)
}

func TestConceptsForField_NilKB(t *testing.T) {
	var kb *KnowledgeBase
	concepts := kb.ConceptsForField("origin")
	assert.Nil(t, concepts)
}

func TestConceptsForField_Sorted(t *testing.T) {
	kb := loadTravelKB(t)
	concepts := kb.ConceptsForField("origin")
	for i := 1; i < len(concepts); i++ {
		assert.True(t, concepts[i-1].Name < concepts[i].Name,
			"expected sorted: %q should come before %q", concepts[i-1].Name, concepts[i].Name)
	}
}

func TestTypeForField(t *testing.T) {
	kb := loadTravelKB(t)
	td := kb.TypeForField("airportCode")
	require.NotNil(t, td)
	assert.Equal(t, "airportCode", td.Name)
}

func TestAllValues_FlatPool(t *testing.T) {
	kb := loadTravelKB(t)
	vals := kb.AllValues("usAirports")
	assert.Len(t, vals, 20)
	assert.Contains(t, vals, "JFK")
	assert.Contains(t, vals, "LAX")
}

func TestAllValues_GroupedPool(t *testing.T) {
	kb := loadTravelKB(t)
	vals := kb.AllValues("internationalAirports")
	assert.True(t, len(vals) > 20, "expected more than 20 international airports")
	assert.Contains(t, vals, "LHR")
	assert.Contains(t, vals, "NRT")
	assert.Contains(t, vals, "SYD")
}

func TestAllValues_NotFound(t *testing.T) {
	kb := loadTravelKB(t)
	vals := kb.AllValues("nonexistent")
	assert.Nil(t, vals)
}

func TestAllValues_NilKB(t *testing.T) {
	var kb *KnowledgeBase
	vals := kb.AllValues("anything")
	assert.Nil(t, vals)
}

func TestSampleValues_Flat(t *testing.T) {
	kb := loadTravelKB(t)
	vals := kb.SampleValues("usAirports", 5)
	assert.Len(t, vals, 5)
	// All sampled values should be valid airport codes
	all := kb.AllValues("usAirports")
	allSet := make(map[string]bool)
	for _, v := range all {
		allSet[v] = true
	}
	for _, v := range vals {
		assert.True(t, allSet[v], "sampled value %q not in pool", v)
	}
}

func TestSampleValues_LargerThanPool(t *testing.T) {
	kb := loadTravelKB(t)
	vals := kb.SampleValues("cabinClasses", 100)
	assert.Len(t, vals, 4) // pool only has 4 values
}

func TestSampleValues_NotFound(t *testing.T) {
	kb := loadTravelKB(t)
	vals := kb.SampleValues("nonexistent", 5)
	assert.Nil(t, vals)
}

func TestSampleValues_GroupedPool(t *testing.T) {
	kb := loadTravelKB(t)
	vals := kb.SampleValues("internationalAirports", 3)
	assert.Len(t, vals, 3)
}

func TestFormatForPrompt_NonEmpty(t *testing.T) {
	kb := loadTravelKB(t)
	output := kb.FormatForPrompt()
	assert.NotEmpty(t, output)
	assert.Contains(t, output, "# Domain Concepts")
	assert.Contains(t, output, "# Domain Types")
	assert.Contains(t, output, "# Value Pools")
}

func TestFormatForPrompt_ContainsConcepts(t *testing.T) {
	kb := loadTravelKB(t)
	output := kb.FormatForPrompt()
	assert.Contains(t, output, "## domestic")
	assert.Contains(t, output, "## international")
	assert.Contains(t, output, "Applies to:")
}

func TestFormatForPrompt_ContainsTypes(t *testing.T) {
	kb := loadTravelKB(t)
	output := kb.FormatForPrompt()
	assert.Contains(t, output, "## airportCode")
	assert.Contains(t, output, "Format:")
}

func TestFormatForPrompt_ContainsPools(t *testing.T) {
	kb := loadTravelKB(t)
	output := kb.FormatForPrompt()
	assert.Contains(t, output, "## usAirports")
	assert.Contains(t, output, "Values:")
}

func TestFormatForPrompt_TruncatesLargePools(t *testing.T) {
	kb := loadTravelKB(t)
	output := kb.FormatForPrompt()
	// usAirports has 20 values, should be truncated to 10 + "..."
	assert.Contains(t, output, "...")
}

func TestFormatForPrompt_NilKB(t *testing.T) {
	var kb *KnowledgeBase
	assert.Equal(t, "", kb.FormatForPrompt())
}

func TestFormatConceptsForPrompt_Subset(t *testing.T) {
	kb := loadTravelKB(t)
	output := kb.FormatConceptsForPrompt("domestic", "international")
	assert.Contains(t, output, "## domestic")
	assert.Contains(t, output, "## international")
	assert.NotContains(t, output, "## roundTrip")
}

func TestFormatConceptsForPrompt_NoArgs(t *testing.T) {
	kb := loadTravelKB(t)
	output := kb.FormatConceptsForPrompt()
	assert.Contains(t, output, "## domestic")
	assert.Contains(t, output, "## roundTrip")
}

func TestFormatConceptsForPrompt_NotFound(t *testing.T) {
	kb := loadTravelKB(t)
	output := kb.FormatConceptsForPrompt("nonexistent")
	assert.Empty(t, output)
}

func TestFormatTypesForPrompt_Subset(t *testing.T) {
	kb := loadTravelKB(t)
	output := kb.FormatTypesForPrompt("airportCode", "currencyCode")
	assert.Contains(t, output, "## airportCode")
	assert.Contains(t, output, "## currencyCode")
	assert.NotContains(t, output, "## traveler")
}

func TestFormatTypesForPrompt_NoArgs(t *testing.T) {
	kb := loadTravelKB(t)
	output := kb.FormatTypesForPrompt()
	assert.Contains(t, output, "## airportCode")
	assert.Contains(t, output, "## traveler")
}

func TestFormatForPrompt_Deterministic(t *testing.T) {
	kb := loadTravelKB(t)
	output1 := kb.FormatForPrompt()
	output2 := kb.FormatForPrompt()
	assert.Equal(t, output1, output2, "FormatForPrompt should produce deterministic output")
}

func TestFormatForPrompt_PoolField(t *testing.T) {
	kb := loadTravelKB(t)
	output := kb.FormatForPrompt()
	assert.Contains(t, output, "Pool: usAirports")
}

func TestFormatForPrompt_CompositeTypeFields(t *testing.T) {
	kb := loadTravelKB(t)
	output := kb.FormatForPrompt()
	assert.Contains(t, output, "Fields:")
	assert.Contains(t, output, "given (string)")
}

func TestFormatForPrompt_AnnotatedValues(t *testing.T) {
	kb, err := ParseFile("testdata/valid/annotated.yaml")
	require.NoError(t, err)

	output := kb.FormatForPrompt()

	// Flat values with annotations
	assert.Contains(t, output, "ADT (Adult)")
	assert.Contains(t, output, "CNN (Child (2-11))")
	assert.Contains(t, output, "INF (Infant under 2 (lap))")

	// Grouped values with annotations
	assert.Contains(t, output, "WCHR (Wheelchair (ramp))")
	assert.Contains(t, output, "MEDA (Medical case)")

	// Plain values without annotations (no parenthetical)
	assert.Contains(t, output, "alpha")
	assert.NotContains(t, output, "alpha (")
}

func TestFormatForPrompt_SectionLabels(t *testing.T) {
	kb := &KnowledgeBase{
		ValuePools: map[string]*ValuePool{
			"airports": {
				Name:          "airports",
				Description:   "Airport codes",
				Type:          "string",
				Values:        []string{"JFK", "LAX", "ORD", "LHR", "CDG", "NRT"},
				SectionLabels: map[string]string{"JFK": "Major US hubs", "LHR": "Major international"},
			},
		},
	}
	output := kb.FormatForPrompt()
	assert.Contains(t, output, "[Major US hubs] JFK, LAX, ORD; [Major international] LHR, CDG, NRT")
}

func TestFormatForPrompt_SectionLabels_Truncation(t *testing.T) {
	// Verify truncation still works with section labels
	values := []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L"}
	kb := &KnowledgeBase{
		ValuePools: map[string]*ValuePool{
			"test": {
				Name:          "test",
				Description:   "test pool",
				Type:          "string",
				Values:        values,
				SectionLabels: map[string]string{"A": "First", "G": "Second"},
			},
		},
	}
	output := kb.FormatForPrompt()
	// Only first 10 shown, then "..."
	assert.Contains(t, output, "[First] A")
	assert.Contains(t, output, "[Second] G")
	assert.Contains(t, output, ", ...")
}

func TestFormatForPrompt_MixedAnnotationsAndSections(t *testing.T) {
	kb := &KnowledgeBase{
		ValuePools: map[string]*ValuePool{
			"mixed": {
				Name:        "mixed",
				Description: "Mixed pool",
				Type:        "string",
				Values:      []string{"JFK", "LAX", "ORD", "LHR", "CDG"},
				Annotations:   map[string]string{"LAX": "Los Angeles", "LHR": "London Heathrow"},
				SectionLabels: map[string]string{"JFK": "US hubs", "LHR": "International"},
			},
		},
	}
	output := kb.FormatForPrompt()
	assert.Contains(t, output, "[US hubs] JFK, LAX (Los Angeles), ORD; [International] LHR (London Heathrow), CDG")
}

func TestFormatForPrompt_SectionLabelsFromFixture(t *testing.T) {
	kb, err := ParseFile("testdata/valid/annotated.yaml")
	require.NoError(t, err)

	output := kb.FormatForPrompt()

	// airportCodes pool: head comments only
	assert.Contains(t, output, "[Major US hubs] JFK, LAX, ORD; [Major international] LHR, CDG, NRT")

	// mixedPool: head comments + line comments
	assert.Contains(t, output, "[Domestic] JFK (New York), LAX (Los Angeles), ORD; [International] LHR (London Heathrow), CDG, NRT (Tokyo Narita)")
}

func TestFormatForPrompt_AnnotationsNil(t *testing.T) {
	// Ensure no panic or change when annotations are nil
	kb := &KnowledgeBase{
		ValuePools: map[string]*ValuePool{
			"test": {
				Name:        "test",
				Description: "test pool",
				Type:        "string",
				Values:      []string{"X", "Y"},
			},
		},
	}
	output := kb.FormatForPrompt()
	assert.Contains(t, output, "Values: X, Y")
	assert.NotContains(t, output, "X (")
}
