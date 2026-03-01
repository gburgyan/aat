package plan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRecipe_Valid(t *testing.T) {
	yaml := `
kind: recipe
metadata:
  prompt: "book a flight from Rome to New York"
selection:
  workflow: Booking
  description: "Book a round-trip flight"
  choices:
    trip-search: Round-Trip
  addons:
    - Seat Selection
overrides:
  values:
    searchFlights.origin: FCO
    searchFlights.destination: JFK
  selections:
    priceOfferReference.leg1:
      strategy: match
      filter: "carrier == 'QF'"
  assertions:
    commitReservation:
      - type: status
        expect: 200
  descriptions:
    searchFlights: "Search for flights Rome-NYC"
`
	r, err := ParseRecipe([]byte(yaml))
	require.NoError(t, err)

	assert.Equal(t, "recipe", r.Kind)
	assert.Equal(t, "book a flight from Rome to New York", r.Metadata.Prompt)
	assert.Equal(t, "Booking", r.Selection.Workflow)
	assert.Equal(t, "Book a round-trip flight", r.Selection.Description)
	assert.Equal(t, map[string]string{"trip-search": "Round-Trip"}, r.Selection.Choices)
	assert.Equal(t, []string{"Seat Selection"}, r.Selection.Addons)
	assert.Equal(t, "FCO", r.Overrides.Values["searchFlights.origin"])
	assert.Equal(t, "JFK", r.Overrides.Values["searchFlights.destination"])
	assert.Equal(t, "match", r.Overrides.Selections["priceOfferReference.leg1"].Strategy)
	assert.Equal(t, "carrier == 'QF'", r.Overrides.Selections["priceOfferReference.leg1"].Filter)
	require.Len(t, r.Overrides.Assertions["commitReservation"], 1)
	assert.Equal(t, "status", r.Overrides.Assertions["commitReservation"][0].Type)
	assert.Equal(t, "Search for flights Rome-NYC", r.Overrides.Descriptions["searchFlights"])
}

func TestParseRecipe_MissingWorkflow(t *testing.T) {
	yaml := `
kind: recipe
selection:
  description: "no workflow"
`
	_, err := ParseRecipe([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "selection.workflow")
}

func TestParseRecipe_WrongKind(t *testing.T) {
	yaml := `
kind: plan
execution:
  steps:
    - node: foo
`
	_, err := ParseRecipe([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected kind: recipe")
}

func TestParseRecipe_MalformedYAML(t *testing.T) {
	_, err := ParseRecipe([]byte(`{{{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "YAML parse error")
}

func TestIsRecipeFile(t *testing.T) {
	tests := []struct {
		name   string
		yaml   string
		expect bool
	}{
		{"recipe", "kind: recipe\nselection:\n  workflow: Booking\n", true},
		{"plan", "execution:\n  steps:\n    - node: foo\n", false},
		{"plan with kind", "kind: plan\nexecution:\n  steps:\n    - node: foo\n", false},
		{"empty", "", false},
		{"invalid yaml", "{{{", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, IsRecipeFile([]byte(tt.yaml)))
		})
	}
}

func TestParseAny_Recipe(t *testing.T) {
	yaml := `
kind: recipe
selection:
  workflow: Booking
`
	result, err := ParseAny([]byte(yaml))
	require.NoError(t, err)
	_, ok := result.(*Recipe)
	assert.True(t, ok, "expected *Recipe")
}

func TestParseAny_Plan(t *testing.T) {
	yaml := `
execution:
  steps:
    - node: searchFlights
`
	result, err := ParseAny([]byte(yaml))
	require.NoError(t, err)
	_, ok := result.(*Plan)
	assert.True(t, ok, "expected *Plan")
}

func TestMarshalRecipe_RoundTrip(t *testing.T) {
	original := &Recipe{
		Kind: "recipe",
		Selection: RecipeSelection{
			Workflow:    "Booking",
			Description: "Book a flight",
			Choices:     map[string]string{"trip-search": "One-Way"},
			Addons:      []string{"Seat Selection"},
		},
		Overrides: RecipeOverrides{
			Values: map[string]any{
				"searchFlights.origin":      "LAX",
				"searchFlights.destination": "JFK",
			},
			Selections: map[string]RecipeSelectionOverride{
				"priceOfferReference.offering": {
					Strategy: "match",
					Filter:   "carrier == 'AA'",
				},
			},
			Assertions: map[string][]RecipeAssertion{
				"commitReservation": {
					{Type: "status", Expect: 200},
				},
			},
			Descriptions: map[string]string{
				"searchFlights": "Search for flights",
			},
		},
	}

	data, err := MarshalRecipe(original)
	require.NoError(t, err)

	parsed, err := ParseRecipe(data)
	require.NoError(t, err)

	assert.Equal(t, original.Kind, parsed.Kind)
	assert.Equal(t, original.Selection.Workflow, parsed.Selection.Workflow)
	assert.Equal(t, original.Selection.Description, parsed.Selection.Description)
	assert.Equal(t, original.Selection.Choices, parsed.Selection.Choices)
	assert.Equal(t, original.Selection.Addons, parsed.Selection.Addons)
	assert.Equal(t, original.Overrides.Values["searchFlights.origin"], parsed.Overrides.Values["searchFlights.origin"])
	assert.Equal(t, original.Overrides.Selections["priceOfferReference.offering"].Strategy, parsed.Overrides.Selections["priceOfferReference.offering"].Strategy)
	assert.Equal(t, original.Overrides.Descriptions["searchFlights"], parsed.Overrides.Descriptions["searchFlights"])
}

func TestWriteRecipe_AndParseRecipeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-recipe.yaml")

	r := &Recipe{
		Kind: "recipe",
		Selection: RecipeSelection{
			Workflow: "Booking",
		},
		Overrides: RecipeOverrides{
			Values: map[string]any{
				"searchFlights.origin": "DEN",
			},
		},
	}

	err := WriteRecipe(r, path)
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(path)
	require.NoError(t, err)

	// Parse it back
	parsed, err := ParseRecipeFile(path)
	require.NoError(t, err)
	assert.Equal(t, "Booking", parsed.Selection.Workflow)
	assert.Equal(t, "DEN", parsed.Overrides.Values["searchFlights.origin"])
}

func TestParseAnyFile_Recipe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recipe.yaml")

	err := os.WriteFile(path, []byte("kind: recipe\nselection:\n  workflow: Booking\n"), 0o644)
	require.NoError(t, err)

	result, err := ParseAnyFile(path)
	require.NoError(t, err)
	_, ok := result.(*Recipe)
	assert.True(t, ok, "expected *Recipe")
}

func TestParseAnyFile_Plan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.yaml")

	err := os.WriteFile(path, []byte("execution:\n  steps:\n    - node: searchFlights\n"), 0o644)
	require.NoError(t, err)

	result, err := ParseAnyFile(path)
	require.NoError(t, err)
	_, ok := result.(*Plan)
	assert.True(t, ok, "expected *Plan")
}

func TestParseAnyFile_NotFound(t *testing.T) {
	_, err := ParseAnyFile("/nonexistent/path.yaml")
	require.Error(t, err)
}

func TestParseRecipe_MinimalValid(t *testing.T) {
	yaml := `
kind: recipe
selection:
  workflow: Booking
`
	r, err := ParseRecipe([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "Booking", r.Selection.Workflow)
	assert.Empty(t, r.Selection.Choices)
	assert.Empty(t, r.Selection.Addons)
	assert.Empty(t, r.Overrides.Values)
}

func TestParseRecipe_EmptyOverrides(t *testing.T) {
	yaml := `
kind: recipe
selection:
  workflow: Booking
overrides: {}
`
	r, err := ParseRecipe([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "Booking", r.Selection.Workflow)
}
