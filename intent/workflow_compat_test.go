package intent

import (
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildCompatTestGraph creates a graph with nodes that match the compat test fixtures.
// "specialProvider" produces "specialInput" as an output, making it a producible
// name — so AUTOWIRE for "specialInput" is structural and will be validated.
func buildCompatTestGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0",
		Nodes: map[string]*graph.Node{
			"search": {
				Name: "search", Adapter: "search",
				Outputs: []graph.Output{
					{Name: "results", Type: "string[]"},
					{Name: "token", Type: "string"},
				},
			},
			"book": {
				Name: "book", Adapter: "book",
				Inputs:  []graph.Input{{Name: "itemId", Type: "string"}},
				Outputs: []graph.Output{{Name: "workbenchId", Type: "string"}, {Name: "confirmationCode", Type: "string"}},
			},
			"commit": {
				Name: "commit", Adapter: "commit",
				Inputs:  []graph.Input{{Name: "workbenchId", Type: "string"}},
				Outputs: []graph.Output{{Name: "locator", Type: "string"}},
			},
			"addonNode": {
				Name: "addonNode", Adapter: "addonNode",
				Inputs: []graph.Input{
					{Name: "workbenchId", Type: "string"},
					{Name: "specialInput", Type: "string"},
				},
				Outputs: []graph.Output{{Name: "addonResult", Type: "string"}},
			},
			"specialProvider": {
				Name: "specialProvider", Adapter: "specialProvider",
				Outputs: []graph.Output{{Name: "specialInput", Type: "string"}},
			},
		},
	}
}

// validateCompatInMemory mirrors ValidateWorkflowCompat logic using pre-built
// plans instead of loading from disk. Includes the producible-names filter.
func validateCompatInMemory(g *graph.Graph, plans map[string]*plan.Plan) *WorkflowCompatResult {
	result := &WorkflowCompatResult{}

	var bases, addons []graph.Workflow
	for _, wf := range g.Workflows {
		if wf.IsAddon() {
			addons = append(addons, wf)
		} else {
			bases = append(bases, wf)
		}
	}

	if len(addons) == 0 || len(bases) == 0 {
		return result
	}

	producible := buildProducibleNames(g)

	for _, addon := range addons {
		addonPlan := plans[addon.Template]
		if addonPlan == nil {
			continue
		}

		autowireInputs := collectAutowireInputs(addonPlan)
		for inputName := range addon.Wire {
			delete(autowireInputs, inputName)
		}
		var nonProducibleInputs []string
		for inputName := range autowireInputs {
			if !producible[inputName] {
				nonProducibleInputs = append(nonProducibleInputs, inputName)
				delete(autowireInputs, inputName)
			}
		}
		if len(nonProducibleInputs) > 0 {
			result.NonProducible = append(result.NonProducible, WorkflowNonProducible{
				Addon:  addon.Name,
				Inputs: nonProducibleInputs,
			})
		}
		if len(autowireInputs) == 0 {
			continue
		}

		for _, base := range bases {
			basePlan := plans[base.Template]
			if basePlan == nil {
				continue
			}

			if addon.After.IsSet() {
				found := false
				for _, afterNode := range addon.After {
					if findStepByNode(basePlan, afterNode) != "" {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			outputMap := buildOutputMap(basePlan, g)

			var unfed []string
			for inputName := range autowireInputs {
				if _, found := outputMap[inputName]; !found {
					unfed = append(unfed, inputName)
				}
			}

			if len(unfed) > 0 {
				result.Warnings = append(result.Warnings, WorkflowCompatWarning{
					Addon:        addon.Name,
					BaseWorkflow: base.Name,
					UnfedInputs:  unfed,
				})
			}
		}
	}

	return result
}

func TestValidateWorkflowCompat_AllSatisfied(t *testing.T) {
	g := buildCompatTestGraph()
	g.Workflows = []graph.Workflow{
		{Name: "Booking", Template: "testdata/compat/base_full.yaml"},
		{
			Name:     "AddonWired",
			Kind:     "addon",
			Template: "testdata/compat/addon_wired.yaml",
			After:    graph.AfterSpec{"book"},
		},
	}

	result := ValidateWorkflowCompat(g, ".")
	assert.False(t, result.HasWarnings(), "expected no warnings, got: %v", result.Warnings)
	assert.False(t, result.HasErrors())
}

func TestValidateWorkflowCompat_UnfedStructuralInput(t *testing.T) {
	g := buildCompatTestGraph()
	g.Workflows = []graph.Workflow{
		{Name: "Booking", Template: "testdata/compat/base_full.yaml"},
		{
			Name:     "AddonUnfed",
			Kind:     "addon",
			Template: "testdata/compat/addon_unfed.yaml",
			After:    graph.AfterSpec{"book"},
		},
	}

	result := ValidateWorkflowCompat(g, ".")
	require.True(t, result.HasWarnings())
	require.Len(t, result.Warnings, 1)
	w := result.Warnings[0]
	assert.Equal(t, "AddonUnfed", w.Addon)
	assert.Equal(t, "Booking", w.BaseWorkflow)
	// specialInput is producible (specialProvider outputs it) but base_full
	// doesn't include specialProvider → unfed warning.
	// workbenchId is produced by book → satisfied.
	assert.Equal(t, []string{"specialInput"}, w.UnfedInputs)
}

func TestValidateWorkflowCompat_ValueInputNotFlagged(t *testing.T) {
	// An AUTOWIRE input whose name doesn't match any graph output or
	// elementField is a value input (LLM-filled) and should not generate
	// a compatibility warning. It IS recorded as non-producible.
	g := buildCompatTestGraph()
	g.Workflows = []graph.Workflow{
		{Name: "Booking", Template: "testdata/compat/base_full.yaml"},
		{
			Name:     "AddonValue",
			Kind:     "addon",
			Template: "testdata/compat/addon_value_input.yaml",
			After:    graph.AfterSpec{"book"},
		},
	}

	result := ValidateWorkflowCompat(g, ".")
	assert.False(t, result.HasWarnings(), "value inputs should not be compatibility warnings")
	require.True(t, result.HasNonProducible())
	np := result.NonProducible[0]
	assert.Equal(t, "AddonValue", np.Addon)
	assert.Contains(t, np.Inputs, "email")
	assert.Contains(t, np.Inputs, "phoneNumber")
}

func TestValidateWorkflowCompat_ElementFieldProducible(t *testing.T) {
	// AUTOWIRE input matching an elementField name (not just output name)
	// is considered structural and should be flagged when unfed.
	g := buildCompatTestGraph()
	// Add an elementField "itemCode" to search.results
	g.Nodes["search"].Outputs[0].ElementFields = []graph.Field{
		{Name: "itemCode", Type: "string"},
	}
	g.Workflows = []graph.Workflow{
		{Name: "Booking", Template: "testdata/compat/base_full.yaml"},
		{
			Name:     "AddonEF",
			Kind:     "addon",
			Template: "testdata/compat/addon_elementfield.yaml",
			After:    graph.AfterSpec{"book"},
		},
	}

	result := ValidateWorkflowCompat(g, ".")
	require.True(t, result.HasWarnings())
	w := result.Warnings[0]
	// itemCode is producible (elementField) but not in base output map → unfed
	assert.Equal(t, []string{"itemCode"}, w.UnfedInputs)
}

func TestValidateWorkflowCompat_WireOverrideSatisfied(t *testing.T) {
	g := buildCompatTestGraph()
	g.Workflows = []graph.Workflow{
		{Name: "Booking", Template: "testdata/compat/base_full.yaml"},
		{
			Name:     "AddonWired",
			Kind:     "addon",
			Template: "testdata/compat/addon_unfed.yaml",
			After:    graph.AfterSpec{"book"},
			Wire: map[string]string{
				"specialInput": "search.token",
			},
		},
	}

	result := ValidateWorkflowCompat(g, ".")
	assert.False(t, result.HasWarnings(), "wire override should satisfy specialInput")
}

func TestValidateWorkflowCompat_ManualWireNotFlagged(t *testing.T) {
	g := buildCompatTestGraph()
	g.Workflows = []graph.Workflow{
		{Name: "Booking", Template: "testdata/compat/base_full.yaml"},
		{
			Name:     "AddonManual",
			Kind:     "addon",
			Template: "testdata/compat/addon_unfed.yaml",
			After:    graph.AfterSpec{"book"},
			Wire: map[string]string{
				"specialInput": "MANUAL",
			},
		},
	}

	result := ValidateWorkflowCompat(g, ".")
	assert.False(t, result.HasWarnings(), "MANUAL wire should not be flagged")
}

func TestValidateWorkflowCompat_AddonNotInBase(t *testing.T) {
	g := buildCompatTestGraph()
	g.Workflows = []graph.Workflow{
		{Name: "SearchOnly", Template: "testdata/compat/base_partial.yaml"},
		{
			Name:     "AddonWired",
			Kind:     "addon",
			Template: "testdata/compat/addon_wired.yaml",
			After:    graph.AfterSpec{"book"}, // book is NOT in base_partial
		},
	}

	result := ValidateWorkflowCompat(g, ".")
	assert.False(t, result.HasWarnings(), "addon After node not in base → no compatibility check")
}

func TestValidateWorkflowCompat_MultipleBasesPartialCompat(t *testing.T) {
	g := buildCompatTestGraph()
	g.Workflows = []graph.Workflow{
		{Name: "FullBase", Template: "testdata/compat/base_full.yaml"},
		{Name: "PartialBase", Template: "testdata/compat/base_partial.yaml"},
		{
			Name:     "AddonWired",
			Kind:     "addon",
			Template: "testdata/compat/addon_wired.yaml",
			After:    graph.AfterSpec{"book"},
		},
	}

	result := ValidateWorkflowCompat(g, ".")
	// FullBase has book (After node present) and produces workbenchId → OK
	// PartialBase doesn't have book → addon is not compatible → no check
	assert.False(t, result.HasWarnings())
}

func TestValidateWorkflowCompat_TemplateLoadError(t *testing.T) {
	g := buildCompatTestGraph()
	g.Workflows = []graph.Workflow{
		{Name: "BadBase", Template: "testdata/compat/nonexistent.yaml"},
		{
			Name:     "AddonWired",
			Kind:     "addon",
			Template: "testdata/compat/addon_wired.yaml",
			After:    graph.AfterSpec{"book"},
		},
	}

	result := ValidateWorkflowCompat(g, ".")
	require.True(t, result.HasErrors())
	assert.Equal(t, "BadBase", result.Errors[0].Workflow)
	assert.False(t, result.HasWarnings(), "should not warn when base template fails to load")
}

func TestValidateWorkflowCompat_NoWorkflows(t *testing.T) {
	g := buildCompatTestGraph()
	g.Workflows = nil

	result := ValidateWorkflowCompat(g, ".")
	assert.False(t, result.HasIssues())
}

func TestValidateWorkflowCompat_NoAddons(t *testing.T) {
	g := buildCompatTestGraph()
	g.Workflows = []graph.Workflow{
		{Name: "Base", Template: "testdata/compat/base_full.yaml"},
	}

	result := ValidateWorkflowCompat(g, ".")
	assert.False(t, result.HasIssues())
}

func TestValidateWorkflowCompat_NoBases(t *testing.T) {
	g := buildCompatTestGraph()
	g.Workflows = []graph.Workflow{
		{Name: "Addon", Kind: "addon", Template: "testdata/compat/addon_wired.yaml", After: graph.AfterSpec{"book"}},
	}

	result := ValidateWorkflowCompat(g, ".")
	assert.False(t, result.HasIssues())
}

func TestWorkflowCompatResult_Format(t *testing.T) {
	result := &WorkflowCompatResult{
		Warnings: []WorkflowCompatWarning{
			{
				Addon:        "SeatAddon",
				BaseWorkflow: "SimpleBooking",
				UnfedInputs:  []string{"seatMapId", "segmentRef"},
			},
		},
	}

	formatted := result.Format()
	assert.Contains(t, formatted, "Workflow compatibility warnings:")
	assert.Contains(t, formatted, `addon "SeatAddon"`)
	assert.Contains(t, formatted, `base "SimpleBooking"`)
	assert.Contains(t, formatted, "seatMapId, segmentRef")
}

func TestWorkflowCompatResult_FormatNonProducible(t *testing.T) {
	result := &WorkflowCompatResult{
		NonProducible: []WorkflowNonProducible{
			{Addon: "ContactAddon", Inputs: []string{"email", "phoneNumber"}},
		},
	}

	formatted := result.Format()
	assert.Contains(t, formatted, "Non-producible AUTOWIRE inputs")
	assert.Contains(t, formatted, `addon "ContactAddon"`)
	assert.Contains(t, formatted, "email, phoneNumber")
}

func TestWorkflowCompatResult_FormatBoth(t *testing.T) {
	result := &WorkflowCompatResult{
		Warnings: []WorkflowCompatWarning{
			{Addon: "SeatAddon", BaseWorkflow: "Booking", UnfedInputs: []string{"seatMapId"}},
		},
		NonProducible: []WorkflowNonProducible{
			{Addon: "ContactAddon", Inputs: []string{"email"}},
		},
	}

	formatted := result.Format()
	assert.Contains(t, formatted, "Workflow compatibility warnings:")
	assert.Contains(t, formatted, "Non-producible AUTOWIRE inputs")
}

func TestWorkflowCompatResult_FormatEmpty(t *testing.T) {
	result := &WorkflowCompatResult{}
	assert.Equal(t, "", result.Format())
}

// --- buildProducibleNames ---

func TestBuildProducibleNames(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"a": {
				Name: "a",
				Outputs: []graph.Output{
					{Name: "alpha", Type: "string"},
					{
						Name: "items", Type: "item[]",
						ElementFields: []graph.Field{
							{Name: "itemId", Type: "string"},
							{Name: "itemName", Type: "string"},
						},
					},
				},
			},
			"b": {
				Name: "b",
				Outputs: []graph.Output{
					{Name: "beta", Type: "string"},
				},
			},
		},
	}

	names := buildProducibleNames(g)
	assert.True(t, names["alpha"])
	assert.True(t, names["items"])
	assert.True(t, names["itemId"])
	assert.True(t, names["itemName"])
	assert.True(t, names["beta"])
	assert.False(t, names["nonexistent"])
	assert.False(t, names["email"]) // no node produces email
}

// --- collectAutowireInputs ---

func TestCollectAutowireInputs(t *testing.T) {
	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Values: map[string]plan.StepValue{
						"a": {Default: "AUTOWIRE"},
						"b": {Default: "literal"},
						"c": {From: "other.output"},
					},
				},
				{
					Node: "step2",
					Values: map[string]plan.StepValue{
						"d": {Default: "AUTOWIRE"},
						"a": {Default: "AUTOWIRE"}, // duplicate name from different step
					},
				},
			},
		},
	}

	inputs := collectAutowireInputs(p)
	assert.True(t, inputs["a"])
	assert.True(t, inputs["d"])
	assert.False(t, inputs["b"])
	assert.False(t, inputs["c"])
}

// --- In-memory validation tests ---

func TestValidateWorkflowCompat_InMemory_AllSatisfied(t *testing.T) {
	g := buildCompatTestGraph()
	g.Workflows = []graph.Workflow{
		{Name: "Base", Template: "base"},
		{Name: "Addon", Kind: "addon", Template: "addon", After: graph.AfterSpec{"book"}},
	}

	plans := map[string]*plan.Plan{
		"base": {
			Execution: plan.Execution{
				Steps: []plan.Step{
					{Node: "search"},
					{Node: "book"},
				},
			},
		},
		"addon": {
			Execution: plan.Execution{
				Steps: []plan.Step{
					{Node: "addonNode", Values: map[string]plan.StepValue{
						"workbenchId": {Default: "AUTOWIRE"},
					}},
				},
			},
		},
	}

	result := validateCompatInMemory(g, plans)
	assert.False(t, result.HasWarnings())
}

func TestValidateWorkflowCompat_InMemory_UnfedStructural(t *testing.T) {
	g := buildCompatTestGraph()
	g.Workflows = []graph.Workflow{
		{Name: "Base", Template: "base"},
		{Name: "Addon", Kind: "addon", Template: "addon", After: graph.AfterSpec{"search"}},
	}

	plans := map[string]*plan.Plan{
		"base": {
			Execution: plan.Execution{
				Steps: []plan.Step{
					{Node: "search"},
				},
			},
		},
		"addon": {
			Execution: plan.Execution{
				Steps: []plan.Step{
					{Node: "addonNode", Values: map[string]plan.StepValue{
						"workbenchId":  {Default: "AUTOWIRE"},
						"specialInput": {Default: "AUTOWIRE"},
					}},
				},
			},
		},
	}

	result := validateCompatInMemory(g, plans)
	require.True(t, result.HasWarnings())
	w := result.Warnings[0]
	// search only produces "results" and "token".
	// workbenchId is producible (book outputs it) but not in search-only base → unfed.
	// specialInput is producible (specialProvider outputs it) but not in base → unfed.
	assert.Len(t, w.UnfedInputs, 2)
	assert.Contains(t, w.UnfedInputs, "workbenchId")
	assert.Contains(t, w.UnfedInputs, "specialInput")
}

func TestValidateWorkflowCompat_InMemory_ValueInputFiltered(t *testing.T) {
	g := buildCompatTestGraph()
	g.Workflows = []graph.Workflow{
		{Name: "Base", Template: "base"},
		{Name: "Addon", Kind: "addon", Template: "addon", After: graph.AfterSpec{"book"}},
	}

	plans := map[string]*plan.Plan{
		"base": {
			Execution: plan.Execution{
				Steps: []plan.Step{
					{Node: "search"},
					{Node: "book"},
				},
			},
		},
		"addon": {
			Execution: plan.Execution{
				Steps: []plan.Step{
					{Node: "addonNode", Values: map[string]plan.StepValue{
						"workbenchId": {Default: "AUTOWIRE"}, // producible, satisfied
						"email":       {Default: "AUTOWIRE"}, // NOT producible → non-producible entry
					}},
				},
			},
		},
	}

	result := validateCompatInMemory(g, plans)
	// email is not produced by any node → no compatibility warning
	assert.False(t, result.HasWarnings())
	// email shows up as non-producible
	require.True(t, result.HasNonProducible())
	assert.Contains(t, result.NonProducible[0].Inputs, "email")
}
