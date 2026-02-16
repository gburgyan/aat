package plan

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarshal_BareScalars(t *testing.T) {
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{
					Node: "searchFlights",
					Values: map[string]StepValue{
						"origin":      {Default: "DEN"},
						"destination": {Default: "SFO"},
					},
				},
			},
		},
	}

	data, err := Marshal(p)
	require.NoError(t, err)

	yaml := string(data)
	// Bare scalars should appear as "origin: DEN" not "origin:\n  default: DEN"
	assert.Contains(t, yaml, "origin: DEN")
	assert.Contains(t, yaml, "destination: SFO")
	assert.NotContains(t, yaml, "default: DEN")
}

func TestMarshal_ComplexStepValue(t *testing.T) {
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{
					Node: "priceOffer",
					Values: map[string]StepValue{
						"offeringId": {
							From: "searchFlights.catalogOfferings",
							Select: &SelectionConfig{
								Strategy: "first",
								Field:    "Identifier.value",
							},
						},
					},
				},
			},
		},
	}

	data, err := Marshal(p)
	require.NoError(t, err)

	yaml := string(data)
	assert.Contains(t, yaml, "from: searchFlights.catalogOfferings")
	assert.Contains(t, yaml, "strategy: first")
	assert.Contains(t, yaml, "field: Identifier.value")
}

func TestMarshalRoundTrip_Minimal(t *testing.T) {
	original, err := ParseFile("testdata/valid/minimal.yaml")
	require.NoError(t, err)

	data, err := Marshal(original)
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)

	assert.Equal(t, len(original.Execution.Steps), len(parsed.Execution.Steps))
	assert.Equal(t, original.Execution.Steps[0].Node, parsed.Execution.Steps[0].Node)
	assert.Equal(t, original.Execution.Steps[0].Values["origin"].Default, parsed.Execution.Steps[0].Values["origin"].Default)
}

func TestMarshalRoundTrip_FullFeatured(t *testing.T) {
	original, err := ParseFile("testdata/valid/full_featured.yaml")
	require.NoError(t, err)

	data, err := Marshal(original)
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)

	// Verify key structural elements survive the round trip
	assert.Equal(t, original.Metadata.Prompt, parsed.Metadata.Prompt)
	assert.Equal(t, original.Intent.Goal, parsed.Intent.Goal)
	assert.Equal(t, original.Intent.Description, parsed.Intent.Description)
	assert.Equal(t, len(original.Execution.Steps), len(parsed.Execution.Steps))

	// Check constraints
	require.NotNil(t, parsed.Intent.Constraints)
	assert.Equal(t, len(original.Intent.Constraints.Hard), len(parsed.Intent.Constraints.Hard))
	assert.Equal(t, len(original.Intent.Constraints.Soft), len(parsed.Intent.Constraints.Soft))
	assert.Equal(t, original.Intent.Constraints.Free, parsed.Intent.Constraints.Free)

	// Check from/select values round-trip
	assert.Equal(t, original.Execution.Steps[1].Values["offeringId"].From, parsed.Execution.Steps[1].Values["offeringId"].From)
	assert.Equal(t, original.Execution.Steps[1].Values["offeringId"].Select.Strategy, parsed.Execution.Steps[1].Values["offeringId"].Select.Strategy)

	// Check cleanup
	assert.Equal(t, len(original.Execution.Cleanup), len(parsed.Execution.Cleanup))
	assert.Equal(t, original.Execution.Cleanup[0].Node, parsed.Execution.Cleanup[0].Node)
}

func TestWriteFile_CreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "plan.yaml")

	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{Node: "test", Values: map[string]StepValue{"key": {Default: "val"}}},
			},
		},
	}

	err := WriteFile(p, path)
	require.NoError(t, err)

	// File should exist and be readable
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "node: test")
}

func TestWriteFile_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.yaml")

	p1 := &Plan{
		Execution: Execution{
			Steps: []Step{{Node: "first"}},
		},
	}
	p2 := &Plan{
		Execution: Execution{
			Steps: []Step{{Node: "second"}},
		},
	}

	require.NoError(t, WriteFile(p1, path))
	require.NoError(t, WriteFile(p2, path))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "node: second")
	assert.NotContains(t, string(data), "node: first")
}

func TestMarshal_WithMetadata(t *testing.T) {
	p := &Plan{
		Metadata: Metadata{
			Created:      time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC),
			Prompt:       "Test prompt",
			GraphVersion: "1.0.0",
		},
		Execution: Execution{
			Steps: []Step{{Node: "test"}},
		},
	}

	data, err := Marshal(p)
	require.NoError(t, err)

	yaml := string(data)
	assert.Contains(t, yaml, "prompt: Test prompt")
	assert.Contains(t, yaml, "graphVersion: 1.0.0")
}

func TestMarshal_StepValueWithFallback(t *testing.T) {
	strategy := "random"
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{
					Node: "test",
					Values: map[string]StepValue{
						"city": {
							Default:          "DEN",
							FallbackPool:     []any{"SFO", "LAX"},
							FallbackStrategy: &strategy,
						},
					},
				},
			},
		},
	}

	data, err := Marshal(p)
	require.NoError(t, err)

	yaml := string(data)
	// Should marshal as mapping, not bare scalar
	assert.Contains(t, yaml, "default: DEN")
	assert.Contains(t, yaml, "fallbackPool:")
	assert.Contains(t, yaml, "fallbackStrategy: random")
}

func TestMarshal_FromSelection(t *testing.T) {
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{
					Node: "priceOffer",
					Selections: map[string]StepSelection{
						"offering": {
							From:     "searchFlights.catalogOfferings",
							Strategy: "first",
						},
					},
					Values: map[string]StepValue{
						"offeringId": {FromSelection: "offering.offeringId"},
						"productRef": {FromSelection: "offering.productRef"},
					},
				},
			},
		},
	}

	data, err := Marshal(p)
	require.NoError(t, err)

	yaml := string(data)
	// Selections should appear
	assert.Contains(t, yaml, "selections:")
	assert.Contains(t, yaml, "offering:")
	assert.Contains(t, yaml, "from: searchFlights.catalogOfferings")
	assert.Contains(t, yaml, "strategy: first")

	// FromSelection should appear as mapping, not bare scalar
	assert.Contains(t, yaml, "fromSelection: offering.offeringId")
	assert.Contains(t, yaml, "fromSelection: offering.productRef")
	assert.NotContains(t, yaml, "default:")
}

func TestMarshalRoundTrip_NamedSelections(t *testing.T) {
	original, err := ParseFile("testdata/valid/named_selections.yaml")
	require.NoError(t, err)

	data, err := Marshal(original)
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)

	// Verify selections survive round trip
	priceStep := parsed.Execution.Steps[1]
	require.Len(t, priceStep.Selections, 1)
	assert.Equal(t, "searchFlights.catalogOfferings", priceStep.Selections["offering"].From)
	assert.Equal(t, "first", priceStep.Selections["offering"].Strategy)

	// Verify fromSelection values survive
	assert.Equal(t, "offering.offeringId", priceStep.Values["offeringId"].FromSelection)
	assert.Equal(t, "offering.productRef", priceStep.Values["productRef"].FromSelection)
}

func TestMarshalRoundTrip_StepAliasing(t *testing.T) {
	original := &Plan{
		Execution: Execution{
			Steps: []Step{
				{
					ID:   "search_leg1",
					Node: "searchFlights",
					Values: map[string]StepValue{
						"origin":        {Default: "MEL"},
						"destination":   {Default: "SYD"},
						"departureDate": {Default: "2026-03-01"},
					},
				},
				{
					ID:        "search_leg2",
					Node:      "searchFlights",
					DependsOn: []string{"search_leg1"},
					Values: map[string]StepValue{
						"origin":        {Default: "SYD"},
						"destination":   {Default: "BNE"},
						"departureDate": {Default: "2026-03-05"},
					},
				},
			},
		},
	}

	data, err := Marshal(original)
	require.NoError(t, err)

	yaml := string(data)
	// ID field should appear in marshalled YAML
	assert.Contains(t, yaml, "id: search_leg1")
	assert.Contains(t, yaml, "id: search_leg2")

	// Round-trip parse
	parsed, err := Parse(data)
	require.NoError(t, err)

	require.Len(t, parsed.Execution.Steps, 2)
	assert.Equal(t, "search_leg1", parsed.Execution.Steps[0].ID)
	assert.Equal(t, "searchFlights", parsed.Execution.Steps[0].Node)
	assert.Equal(t, "search_leg1", parsed.Execution.Steps[0].StepID())

	assert.Equal(t, "search_leg2", parsed.Execution.Steps[1].ID)
	assert.Equal(t, "searchFlights", parsed.Execution.Steps[1].Node)
	assert.Equal(t, "search_leg2", parsed.Execution.Steps[1].StepID())
	assert.Equal(t, []string{"search_leg1"}, parsed.Execution.Steps[1].DependsOn)
}

func TestMarshal_StepWithoutID(t *testing.T) {
	// Verify that steps without ID don't emit the id field
	original := &Plan{
		Execution: Execution{
			Steps: []Step{
				{
					Node: "searchFlights",
					Values: map[string]StepValue{
						"origin": {Default: "DEN"},
					},
				},
			},
		},
	}

	data, err := Marshal(original)
	require.NoError(t, err)

	yaml := string(data)
	assert.NotContains(t, yaml, "id:")
	assert.Contains(t, yaml, "node: searchFlights")
}
