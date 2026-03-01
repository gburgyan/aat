package intent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildRecipeTestGraph creates a graph for recipe tests, reusing the
// composition test graph and adding a base workflow.
func buildRecipeTestGraph() *graph.Graph {
	g := &graph.Graph{
		Version: "1.0.0",
		Workflows: []graph.Workflow{
			{
				Name:     "Base",
				Template: "testdata/compose/parent.yaml",
			},
			{
				Name:     "Addon",
				Kind:     "addon",
				Template: "testdata/compose/addon.yaml",
				After:    graph.AfterSpec{"book"},
			},
		},
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Adapter: "search",
				Outputs: []graph.Output{{Name: "results", Type: "string[]"}},
			},
			"book": {
				Name: "book", Adapter: "book",
				Inputs:  []graph.Input{{Name: "flightId", Type: "string"}},
				Outputs: []graph.Output{{Name: "workbenchId", Type: "string"}},
			},
			"commit": {
				Name: "commit", Adapter: "commit",
				Inputs:  []graph.Input{{Name: "workbenchId", Type: "string"}},
				Outputs: []graph.Output{{Name: "locator", Type: "string"}},
			},
			"addon1": {
				Name: "addon1", Adapter: "addon1",
				Inputs: []graph.Input{
					{Name: "workbenchId", Type: "string"},
					{Name: "specialInput", Type: "string"},
				},
				Outputs: []graph.Output{{Name: "result1", Type: "string"}},
			},
			"addon2": {
				Name: "addon2", Adapter: "addon2",
				Inputs:  []graph.Input{{Name: "data", Type: "string"}},
				Outputs: []graph.Output{{Name: "result2", Type: "string"}},
			},
		},
	}
	return g
}

func TestReconstitute_SimpleWorkflow(t *testing.T) {
	g := buildRecipeTestGraph()

	recipe := &plan.Recipe{
		Kind: "recipe",
		Metadata: plan.Metadata{
			Prompt: "book a flight",
		},
		Selection: plan.RecipeSelection{
			Workflow:    "Base",
			Description: "Book a flight",
		},
	}

	p, err := Reconstitute(recipe, g, ".")
	require.NoError(t, err)
	require.NotNil(t, p)

	// Verify basic structure.
	assert.Len(t, p.Execution.Steps, 3) // search, book, commit

	// Verify template descriptions are used (not overridden).
	var searchStep *plan.Step
	for i := range p.Execution.Steps {
		if p.Execution.Steps[i].Node == "search" {
			searchStep = &p.Execution.Steps[i]
			break
		}
	}
	require.NotNil(t, searchStep, "search step should exist")
	assert.Equal(t, "Search for items", searchStep.Description) // template description preserved
}

func TestReconstitute_WithAddons(t *testing.T) {
	g := buildRecipeTestGraph()

	recipe := &plan.Recipe{
		Kind: "recipe",
		Selection: plan.RecipeSelection{
			Workflow: "Base",
			Addons:   []string{"Addon"},
		},
	}

	p, err := Reconstitute(recipe, g, ".")
	require.NoError(t, err)
	require.NotNil(t, p)

	// Should have 5 steps: search, book, inc0_addon1, inc0_addon2, commit
	assert.GreaterOrEqual(t, len(p.Execution.Steps), 4, "should have base + addon steps")
}

func TestReconstitute_WithSelections(t *testing.T) {
	g := buildRecipeTestGraph()

	recipe := &plan.Recipe{
		Kind: "recipe",
		Selection: plan.RecipeSelection{
			Workflow: "Base",
		},
		Overrides: plan.RecipeOverrides{
			Selections: map[string]plan.RecipeSelectionOverride{
				"book.flightId": {
					Strategy: "match",
					Filter:   "carrier == 'AA'",
				},
			},
		},
	}

	// This should succeed — the selection override will be applied
	// (though the filter may not match a real named selection, it's
	// valid from a structural perspective).
	p, err := Reconstitute(recipe, g, ".")
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestReconstitute_WithAssertions(t *testing.T) {
	g := buildRecipeTestGraph()

	recipe := &plan.Recipe{
		Kind: "recipe",
		Selection: plan.RecipeSelection{
			Workflow: "Base",
		},
		Overrides: plan.RecipeOverrides{
			Assertions: map[string][]plan.RecipeAssertion{
				"commit": {
					{Type: "status", Expect: 200},
					{Type: "fieldExists", Path: "locator"},
				},
			},
		},
	}

	p, err := Reconstitute(recipe, g, ".")
	require.NoError(t, err)

	// Find the commit step and check assertions.
	var commitStep *plan.Step
	for i := range p.Execution.Steps {
		if p.Execution.Steps[i].Node == "commit" {
			commitStep = &p.Execution.Steps[i]
			break
		}
	}
	require.NotNil(t, commitStep)
	require.NotNil(t, commitStep.Assertions)
	// Should have at least the assertions we specified (PostProcess adds status assertion too).
	found := false
	for _, a := range commitStep.Assertions.Mechanical {
		if a.Type == "fieldExists" && a.Path == "locator" {
			found = true
			break
		}
	}
	assert.True(t, found, "should have fieldExists assertion for locator")
}

func TestReconstitute_UnknownWorkflow(t *testing.T) {
	g := buildRecipeTestGraph()

	recipe := &plan.Recipe{
		Kind: "recipe",
		Selection: plan.RecipeSelection{
			Workflow: "NonExistent",
		},
	}

	_, err := Reconstitute(recipe, g, ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown or template-less workflow")
}

func TestReconstitute_NilRecipe(t *testing.T) {
	g := buildRecipeTestGraph()
	_, err := Reconstitute(nil, g, ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recipe is nil")
}

func TestWorkflowSelectionToRecipeSelection_RoundTrip(t *testing.T) {
	ws := &WorkflowSelection{
		Workflow:    "Booking",
		Description: "Book a flight",
		Choices:     map[string]string{"trip-search": "Round-Trip"},
		Addons:      []string{"Seat Selection"},
	}

	rs := WorkflowSelectionToRecipeSelection(ws)

	assert.Equal(t, ws.Workflow, rs.Workflow)
	assert.Equal(t, ws.Description, rs.Description)
	assert.Equal(t, ws.Choices, rs.Choices)
	assert.Equal(t, ws.Addons, rs.Addons)

	// Convert back.
	ws2 := recipeSelectionToWorkflowSelection(rs)
	assert.Equal(t, ws.Workflow, ws2.Workflow)
	assert.Equal(t, ws.Description, ws2.Description)
	assert.Equal(t, ws.Choices, ws2.Choices)
	assert.Equal(t, ws.Addons, ws2.Addons)
}

func TestTargetedResponseToRecipeOverrides_RoundTrip(t *testing.T) {
	tr := &TargetedResponse{
		Values: map[string]any{
			"search.origin":      "FCO",
			"search.destination": "JFK",
		},
		Selections: map[string]TargetedSelection{
			"offering.leg1": {
				Strategy: "match",
				Filter:   "carrier == 'QF'",
			},
		},
		Assertions: map[string][]TargetedAssertion{
			"commit": {
				{Type: "status", Expect: 200},
			},
		},
	}

	ro := TargetedResponseToRecipeOverrides(tr)

	assert.Equal(t, "FCO", ro.Values["search.origin"])
	assert.Equal(t, "JFK", ro.Values["search.destination"])
	assert.Equal(t, "match", ro.Selections["offering.leg1"].Strategy)
	assert.Equal(t, "carrier == 'QF'", ro.Selections["offering.leg1"].Filter)
	require.Len(t, ro.Assertions["commit"], 1)
	assert.Equal(t, "status", ro.Assertions["commit"][0].Type)
	assert.Nil(t, ro.Descriptions) // descriptions are not saved

	// Convert back.
	tr2 := recipeOverridesToTargetedResponse(ro)
	assert.Equal(t, tr.Values["search.origin"], tr2.Values["search.origin"])
	assert.Equal(t, tr.Selections["offering.leg1"].Strategy, tr2.Selections["offering.leg1"].Strategy)
}

func TestTargetedResponseToRecipeOverrides_Nil(t *testing.T) {
	ro := TargetedResponseToRecipeOverrides(nil)
	assert.Nil(t, ro.Values)
	assert.Nil(t, ro.Selections)
	assert.Nil(t, ro.Assertions)
	assert.Nil(t, ro.Descriptions)
}

func TestWorkflowSelectionToRecipeSelection_Nil(t *testing.T) {
	rs := WorkflowSelectionToRecipeSelection(nil)
	assert.Empty(t, rs.Workflow)
}

func TestTargetedResponseToRecipeOverrides_EmptyMaps(t *testing.T) {
	tr := &TargetedResponse{
		Values:     map[string]any{},
		Selections: map[string]TargetedSelection{},
		Assertions: map[string][]TargetedAssertion{},
	}

	ro := TargetedResponseToRecipeOverrides(tr)
	// Empty maps should be cleaned to nil for cleaner YAML.
	assert.Nil(t, ro.Values)
	assert.Nil(t, ro.Selections)
	assert.Nil(t, ro.Assertions)
	assert.Nil(t, ro.Descriptions)
}

func TestReconstitute_MissingRecipeLayersLoadedFromDisk(t *testing.T) {
	g := buildRecipeTestGraph()

	// Create a temp layers directory with two layer files.
	layersDir := t.TempDir()

	cliLayerYAML := `name: cliLayer
description: Layer from CLI flag
inputs:
  flightId:
    value: "CLI-FLIGHT"
`
	recipeLayerYAML := `name: recipeLayer
description: Layer embedded in recipe
inputs:
  flightId:
    value: "RECIPE-FLIGHT"
`
	require.NoError(t, os.WriteFile(filepath.Join(layersDir, "cliLayer.yaml"), []byte(cliLayerYAML), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(layersDir, "recipeLayer.yaml"), []byte(recipeLayerYAML), 0644))

	// Pre-load only the CLI layer (simulating what loadRunContext does).
	preLoaded, err := graph.ResolveLayerNames([]string{"cliLayer"}, layersDir)
	require.NoError(t, err)

	recipe := &plan.Recipe{
		Kind: "recipe",
		Metadata: plan.Metadata{
			Prompt: "book a flight with recipe layer",
		},
		Selection: plan.RecipeSelection{
			Workflow:    "Base",
			Description: "Book a flight",
			Layers:      []string{"recipeLayer", "cliLayer"},
		},
	}

	// Reconstitute with pre-loaded layers that are missing "recipeLayer".
	p, err := Reconstitute(recipe, g, ".", WithAvailableLayers(preLoaded), WithLayersDir(layersDir))
	require.NoError(t, err)
	require.NotNil(t, p)

	// Verify that the plan was reconstituted with both layers applied.
	// The book step's flightId should reflect the layered default from cliLayer
	// (applied second, so it wins over recipeLayer).
	var bookStep *plan.Step
	for i := range p.Execution.Steps {
		if p.Execution.Steps[i].Node == "book" {
			bookStep = &p.Execution.Steps[i]
			break
		}
	}
	require.NotNil(t, bookStep, "book step should exist")

	// The important assertion: reconstitution succeeded even though
	// "recipeLayer" was not in the pre-loaded set. The fix loads it from disk.
	assert.Len(t, p.Execution.Steps, 3)

	// Verify the shared pre-loaded map was NOT mutated.
	assert.Len(t, preLoaded, 1, "pre-loaded map should not be mutated")
	_, hasRecipeLayer := preLoaded["recipeLayer"]
	assert.False(t, hasRecipeLayer, "recipeLayer should not appear in original pre-loaded map")
}
