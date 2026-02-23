package intent

import (
	"fmt"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// ReconstituteOption is a functional option for Reconstitute.
type ReconstituteOption func(*reconstituteConfig)

type reconstituteConfig struct {
	layersDir       string
	availableLayers map[string]*graph.Layer
}

// WithLayersDir specifies where to find layer files during reconstitution.
func WithLayersDir(dir string) ReconstituteOption {
	return func(c *reconstituteConfig) {
		c.layersDir = dir
	}
}

// WithAvailableLayers provides pre-loaded layers for reconstitution.
func WithAvailableLayers(layers map[string]*graph.Layer) ReconstituteOption {
	return func(c *reconstituteConfig) {
		c.availableLayers = layers
	}
}

// Reconstitute replays the composition pipeline from a compact Recipe to
// produce a full Plan. The steps mirror what Interpret does after Call 1/2:
//  1. Convert RecipeSelection → WorkflowSelection
//  2. Load + compose workflow template (slots + addons)
//  3. ExpandMultiplicity
//  4. Convert RecipeOverrides → TargetedResponse
//  5. applyTargetedResponse (with unfedInputSet)
//  6. PostProcess
//  7. Apply layers (if any) and plan.InstantiateAndValidate
func Reconstitute(recipe *plan.Recipe, g *graph.Graph, graphDir string, opts ...ReconstituteOption) (*plan.Plan, error) {
	if recipe == nil {
		return nil, fmt.Errorf("reconstitute: recipe is nil")
	}

	var cfg reconstituteConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	// 1. Convert to internal types.
	ws := recipeSelectionToWorkflowSelection(recipe.Selection)

	// 2. Load/compose workflow template.
	wf, found := findWorkflowByName(g, ws.Workflow)
	if !found || wf.Template == "" {
		return nil, fmt.Errorf("reconstitute: unknown or template-less workflow %q; available: %s", ws.Workflow, listWorkflowNames(g))
	}

	var tpl *plan.Plan

	hasSlots := len(wf.Slots) > 0
	hasAddons := len(ws.Addons) > 0

	if hasSlots {
		composed, err := ComposeWithSlotsAndAddons(wf, ws.Choices, ws.Addons, g, graphDir)
		if err != nil {
			return nil, fmt.Errorf("reconstitute: composing slots for %q: %w", ws.Workflow, err)
		}
		tpl = composed
	} else if hasAddons {
		composed, err := ComposeWithAddons(wf, ws.Addons, g, graphDir)
		if err != nil {
			return nil, fmt.Errorf("reconstitute: composing addons for %q: %w", ws.Workflow, err)
		}
		tpl = composed
	} else {
		loaded, err := LoadWorkflowTemplate(wf.Template, graphDir, g)
		if err != nil {
			return nil, fmt.Errorf("reconstitute: loading template for %q: %w", ws.Workflow, err)
		}
		tpl = loaded
	}

	// 3. Expand multiplicity.
	ExpandMultiplicity(tpl, ws.Repetitions)

	skeleton := tpl

	// 4. Convert overrides to TargetedResponse.
	targeted := recipeOverridesToTargetedResponse(recipe.Overrides)
	unfedSet := unfedInputSet(skeleton, g)

	// 5. Apply targeted response.
	applyTargetedResponse(skeleton, targeted, unfedSet)

	// 6. PostProcess.
	PostProcess(skeleton, g, &ws, recipe.Metadata.Prompt)

	// 7. Resolve layers and validate.
	var layeredDefaults map[string]*graph.InputDefault
	if len(ws.Layers) > 0 {
		available := cfg.availableLayers
		if available == nil && cfg.layersDir != "" {
			var err error
			available, err = graph.ResolveLayerNames(ws.Layers, cfg.layersDir)
			if err != nil {
				return nil, fmt.Errorf("reconstitute: loading layers: %w", err)
			}
		}
		if available != nil {
			var err error
			layeredDefaults, err = graph.ApplyLayers(g, ws.Layers, available)
			if err != nil {
				return nil, fmt.Errorf("reconstitute: applying layers: %w", err)
			}
		}
	}

	if _, err := plan.InstantiateAndValidateWithLayers(skeleton, g, layeredDefaults); err != nil {
		return nil, fmt.Errorf("reconstitute: validating plan: %w", err)
	}

	return skeleton, nil
}

// recipeSelectionToWorkflowSelection converts the plan-package recipe selection
// to the intent-package WorkflowSelection.
func recipeSelectionToWorkflowSelection(rs plan.RecipeSelection) WorkflowSelection {
	return WorkflowSelection{
		Workflow:    rs.Workflow,
		Description: rs.Description,
		Layers:      rs.Layers,
		Choices:     rs.Choices,
		Addons:      rs.Addons,
		Repetitions: rs.Repetitions,
	}
}

// recipeOverridesToTargetedResponse converts the plan-package recipe overrides
// to the intent-package TargetedResponse for application to the skeleton.
func recipeOverridesToTargetedResponse(ro plan.RecipeOverrides) *TargetedResponse {
	tr := &TargetedResponse{
		Values:     make(map[string]any),
		Selections: make(map[string]TargetedSelection),
		Assertions: make(map[string][]TargetedAssertion),
	}

	for k, v := range ro.Values {
		tr.Values[k] = v
	}

	for k, sel := range ro.Selections {
		tr.Selections[k] = TargetedSelection{
			Strategy:  sel.Strategy,
			Filter:    sel.Filter,
			SortField: sel.SortField,
			Index:     sel.Index,
			Prompt:    sel.Prompt,
		}
	}

	for k, assertions := range ro.Assertions {
		var ta []TargetedAssertion
		for _, a := range assertions {
			ta = append(ta, TargetedAssertion{
				Type:   a.Type,
				Expect: a.Expect,
				Path:   a.Path,
				Value:  a.Value,
				Expr:   a.Expr,
			})
		}
		tr.Assertions[k] = ta
	}

	return tr
}

// WorkflowSelectionToRecipeSelection converts intent.WorkflowSelection to
// plan.RecipeSelection for saving as a recipe.
func WorkflowSelectionToRecipeSelection(ws *WorkflowSelection) plan.RecipeSelection {
	if ws == nil {
		return plan.RecipeSelection{}
	}
	return plan.RecipeSelection{
		Workflow:    ws.Workflow,
		Description: ws.Description,
		Layers:      ws.Layers,
		Choices:     ws.Choices,
		Addons:      ws.Addons,
		Repetitions: ws.Repetitions,
	}
}

// TargetedResponseToRecipeOverrides converts intent.TargetedResponse to
// plan.RecipeOverrides for saving as a recipe.
func TargetedResponseToRecipeOverrides(tr *TargetedResponse) plan.RecipeOverrides {
	if tr == nil {
		return plan.RecipeOverrides{}
	}

	ro := plan.RecipeOverrides{
		Values:     make(map[string]any),
		Selections: make(map[string]plan.RecipeSelectionOverride),
		Assertions: make(map[string][]plan.RecipeAssertion),
	}

	for k, v := range tr.Values {
		ro.Values[k] = v
	}

	for k, sel := range tr.Selections {
		ro.Selections[k] = plan.RecipeSelectionOverride{
			Strategy:  sel.Strategy,
			Filter:    sel.Filter,
			SortField: sel.SortField,
			Index:     sel.Index,
			Prompt:    sel.Prompt,
		}
	}

	for k, assertions := range tr.Assertions {
		var ra []plan.RecipeAssertion
		for _, a := range assertions {
			ra = append(ra, plan.RecipeAssertion{
				Type:   a.Type,
				Expect: a.Expect,
				Path:   a.Path,
				Value:  a.Value,
				Expr:   a.Expr,
			})
		}
		ro.Assertions[k] = ra
	}

	// Clean up empty maps to produce cleaner YAML.
	if len(ro.Values) == 0 {
		ro.Values = nil
	}
	if len(ro.Selections) == 0 {
		ro.Selections = nil
	}
	if len(ro.Assertions) == 0 {
		ro.Assertions = nil
	}

	return ro
}
