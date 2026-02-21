package graph

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_ValidGraphs(t *testing.T) {
	files := []string{
		"testdata/valid/minimal.yaml",
		"testdata/valid/travel_flow.yaml",
		"testdata/valid/with_conditions.yaml",
		"testdata/valid/optional_inputs.yaml",
		"testdata/valid/travelport_booking.yaml",
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			_, err := ParseFile(f)
			assert.NoError(t, err)
		})
	}
}

func TestValidate_MissingVersion(t *testing.T) {
	_, err := ParseFile("testdata/invalid/missing_version.yaml")
	require.Error(t, err)
	assertValidationContains(t, err, "version is required")
}

func TestValidate_BadVersion(t *testing.T) {
	_, err := ParseFile("testdata/invalid/bad_version.yaml")
	require.Error(t, err)
	assertValidationContains(t, err, "invalid version")
}

func TestValidate_DuplicateInput(t *testing.T) {
	_, err := ParseFile("testdata/invalid/duplicate_input.yaml")
	require.Error(t, err)
	assertValidationContains(t, err, "duplicate input name")
}

func TestValidate_BadType(t *testing.T) {
	_, err := ParseFile("testdata/invalid/bad_type.yaml")
	require.Error(t, err)
	assertValidationContains(t, err, "invalid type")
}

func TestValidate_MissingAdapter(t *testing.T) {
	_, err := ParseFile("testdata/invalid/missing_adapter.yaml")
	require.Error(t, err)
	assertValidationContains(t, err, "adapter is required")
}

func TestValidate_MultiError(t *testing.T) {
	// Build a graph with multiple errors at once
	g := &Graph{
		Version: "", // error 1: missing version
		Nodes: map[string]*Node{
			"badNode": {
				Name:    "badNode",
				Adapter: "", // error 2: missing adapter
				Inputs: []Input{
					{Name: "x", Type: "enum["}, // error 3: bad type
				},
				Outputs: []Output{
					{Name: "y", Type: "string"},
				},
			},
		},
	}

	err := Validate(g)
	require.Error(t, err)

	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	assert.GreaterOrEqual(t, len(ve.Errors), 3, "expected at least 3 errors, got: %v", ve.Errors)
}

func TestValidate_SelfReferentialCleanup(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"selfRef": {
				Name:    "selfRef",
				Adapter: "selfRef",
				Inputs:  []Input{{Name: "x", Type: "string"}},
				Outputs: []Output{{Name: "y", Type: "string"}},
				Cleanup: "selfRef",
			},
		},
	}

	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "cleanup cannot reference itself")
}

func TestValidate_CleanupUnknownNode(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"n1": {
				Name:    "n1",
				Adapter: "n1",
				Inputs:  []Input{{Name: "x", Type: "string"}},
				Outputs: []Output{{Name: "y", Type: "string"}},
				Cleanup: "doesNotExist",
			},
		},
	}

	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "cleanup references unknown node")
}

func TestValidate_ElementFieldsOnNonArray(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"n": {
				Name: "n", Adapter: "n",
				Inputs: []Input{{Name: "x", Type: "string"}},
				Outputs: []Output{
					{
						Name: "y",
						Type: "string",
						ElementFields: []Field{
							{Name: "f1", Type: "string"},
						},
					},
				},
			},
		},
	}

	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "elementFields specified but type")
}

func TestValidate_ConditionUnknownNode(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"n": {
				Name: "n", Adapter: "n",
				Inputs:  []Input{{Name: "x", Type: "string"}},
				Outputs: []Output{{Name: "y", Type: "string"}},
			},
		},
		Conditions: []Condition{
			{When: "some expr", Require: []string{"unknown"}},
		},
	}

	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "unknown node")
}

func TestValidate_ConditionEmptyWhen(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"n": {
				Name: "n", Adapter: "n",
				Inputs:  []Input{{Name: "x", Type: "string"}},
				Outputs: []Output{{Name: "y", Type: "string"}},
			},
		},
		Conditions: []Condition{
			{When: "", Require: []string{"n"}},
		},
	}

	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "'when' is required")
}

// assertValidationContains checks that the error is a ValidationError
// and that at least one of its messages contains the given substring.
func assertValidationContains(t *testing.T, err error, substr string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	for _, msg := range ve.Errors {
		if assert.ObjectsAreEqual(true, containsSubstring(msg, substr)) {
			return
		}
	}
	t.Errorf("no validation error contains %q; errors: %v", substr, ve.Errors)
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestValidateWarnings_NoWorkflows(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes:   map[string]*Node{},
	}
	warnings := ValidateWarnings(g)
	assert.Empty(t, warnings)
}

func TestValidateWarnings_AddonWithValidAfter(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Workflows: []Workflow{
			{Name: "Addon", Kind: "addon", After: AfterSpec{"n1"}, Template: "workflows/addon.yaml"},
		},
		Nodes: map[string]*Node{
			"n1": {Name: "n1", Adapter: "a"},
		},
	}
	warnings := ValidateWarnings(g)
	assert.Empty(t, warnings)
}

func TestValidateWarnings_AddonAfterUnknownNode(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Workflows: []Workflow{
			{Name: "Addon", Kind: "addon", After: AfterSpec{"missing"}, Template: "workflows/addon.yaml"},
		},
		Nodes: map[string]*Node{
			"n1": {Name: "n1", Adapter: "a"},
		},
	}
	warnings := ValidateWarnings(g)
	require.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0], "unknown node")
	assert.Contains(t, warnings[0], "missing")
}

func TestValidateWarnings_AfterOnNonAddon(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Workflows: []Workflow{
			{Name: "Main", After: AfterSpec{"n1"}, Template: "workflows/main.yaml"},
		},
		Nodes: map[string]*Node{
			"n1": {Name: "n1", Adapter: "a"},
		},
	}
	warnings := ValidateWarnings(g)
	require.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0], "not \"addon\"")
}

// --- Configurable validation ---

func TestValidate_ConfigurableRequiresOptional(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"search": {
				Name:    "search",
				Adapter: "search",
				Inputs: []Input{
					{Name: "origin", Type: "string"},
					{Name: "carrier", Type: "string", Configurable: true}, // not optional!
				},
				Outputs: []Output{{Name: "out", Type: "string"}},
			},
		},
	}

	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "configurable requires optional")
}

func TestValidate_ConfigurableWithOptionalIsValid(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"search": {
				Name:    "search",
				Adapter: "search",
				Inputs: []Input{
					{Name: "origin", Type: "string"},
					{Name: "carrier", Type: "string", Optional: true, Configurable: true},
				},
				Outputs: []Output{{Name: "out", Type: "string"}},
			},
		},
	}

	err := Validate(g)
	assert.NoError(t, err)
}

// --- Constraint validation ---

func TestValidate_ConstraintBadPattern(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"n": {
				Name: "n", Adapter: "n",
				Inputs: []Input{
					{Name: "x", Type: "string", Constraints: &Constraint{Pattern: "[invalid"}},
				},
				Outputs: []Output{{Name: "y", Type: "string"}},
			},
		},
	}
	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "invalid constraint pattern")
}

func TestValidate_ConstraintMinGtMax(t *testing.T) {
	min, max := 10.0, 5.0
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"n": {
				Name: "n", Adapter: "n",
				Inputs: []Input{
					{Name: "x", Type: "integer", Constraints: &Constraint{Min: &min, Max: &max}},
				},
				Outputs: []Output{{Name: "y", Type: "string"}},
			},
		},
	}
	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "min (10) > max (5)")
}

func TestValidate_ConstraintMinLengthGtMaxLength(t *testing.T) {
	minLen, maxLen := 10, 3
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"n": {
				Name: "n", Adapter: "n",
				Inputs: []Input{
					{Name: "x", Type: "string", Constraints: &Constraint{MinLength: &minLen, MaxLength: &maxLen}},
				},
				Outputs: []Output{{Name: "y", Type: "string"}},
			},
		},
	}
	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "minLength (10) > maxLength (3)")
}

func TestValidate_ConstraintValid(t *testing.T) {
	min, max := 1.0, 100.0
	minLen, maxLen := 3, 3
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"n": {
				Name: "n", Adapter: "n",
				Inputs: []Input{
					{Name: "code", Type: "string", Constraints: &Constraint{
						MinLength: &minLen, MaxLength: &maxLen,
						Pattern:     "^[A-Z]{3}$",
						Description: "IATA airport code",
					}},
					{Name: "count", Type: "integer", Constraints: &Constraint{
						Min: &min, Max: &max,
					}},
				},
				Outputs: []Output{{Name: "y", Type: "string"}},
			},
		},
	}
	err := Validate(g)
	assert.NoError(t, err)
}

// --- elementField path validation ---

func TestValidate_ElementFieldPathTrailingDot(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"n": {
				Name: "n", Adapter: "n",
				Outputs: []Output{
					{
						Name: "items",
						Type: "item[]",
						ElementFields: []Field{
							{Name: "id", Type: "string", Path: "data.id."},
						},
					},
				},
			},
		},
	}
	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "trailing dot")
}

func TestValidate_ElementFieldPathEmptySegment(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"n": {
				Name: "n", Adapter: "n",
				Outputs: []Output{
					{
						Name: "items",
						Type: "item[]",
						ElementFields: []Field{
							{Name: "id", Type: "string", Path: "data..id"},
						},
					},
				},
			},
		},
	}
	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "empty segment")
}

func TestValidate_ElementFieldPathValid(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"n": {
				Name: "n", Adapter: "n",
				Outputs: []Output{
					{
						Name: "items",
						Type: "item[]",
						ElementFields: []Field{
							{Name: "ref", Type: "string", Path: "ProductBrandOptions.0.ProductBrandOffering.0.Product.0.productRef"},
						},
					},
				},
			},
		},
	}
	err := Validate(g)
	assert.NoError(t, err)
}

// --- Requires/Satisfies Validation ---

func TestValidate_UnsatisfiedRequirement(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"n": {
				Name: "n", Adapter: "n",
				Requires: []string{"noSuchToken"},
				Inputs:   []Input{{Name: "x", Type: "string"}},
				Outputs:  []Output{{Name: "y", Type: "string"}},
			},
		},
	}

	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "requires token")
	assertValidationContains(t, err, "noSuchToken")
}

func TestValidate_RequiresSatisfiesCycle(t *testing.T) {
	// A requires B, B requires A via tokens.
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a",
				Requires: []string{"tokenB"}, Satisfies: []string{"tokenA"},
				Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b",
				Requires: []string{"tokenA"}, Satisfies: []string{"tokenB"},
				Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
	}

	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "requires/satisfies cycle")
}

func TestValidate_RequiresSatisfiesCycle_CycleBreakerSuppresses(t *testing.T) {
	// Same cycle as above, but one node is a CycleBreaker.
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a",
				Requires: []string{"tokenB"}, Satisfies: []string{"tokenA"},
				CycleBreaker: true,
				Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b",
				Requires: []string{"tokenA"}, Satisfies: []string{"tokenB"},
				Inputs: []Input{{Name: "x", Type: "string"}}, Outputs: []Output{{Name: "y", Type: "string"}}},
		},
	}

	err := Validate(g)
	assert.NoError(t, err) // CycleBreaker suppresses the cycle
}

func TestValidate_RequiresSatisfies_ValidGraph(t *testing.T) {
	// A valid graph with requires/satisfies, no cycles.
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"source": {Name: "source", Adapter: "s",
				Satisfies: []string{"dataReady"},
				Outputs:   []Output{{Name: "out", Type: "string"}}},
			"consumer": {Name: "consumer", Adapter: "c",
				Requires: []string{"dataReady"},
				Inputs:   []Input{{Name: "x", Type: "string"}},
				Outputs:  []Output{{Name: "y", Type: "string"}}},
		},
	}

	err := Validate(g)
	assert.NoError(t, err)
}

func TestValidateWarnings_MultipleSatisfiersNoPreferred(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a", Satisfies: []string{"token1"}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Satisfies: []string{"token1"}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"c": {Name: "c", Adapter: "c", Requires: []string{"token1"}, Inputs: []Input{{Name: "x", Type: "string"}}},
		},
	}

	warnings := ValidateWarnings(g)
	require.NotEmpty(t, warnings)
	found := false
	for _, w := range warnings {
		if containsSubstring(w, "token1") && containsSubstring(w, "preferred") {
			found = true
		}
	}
	assert.True(t, found, "expected warning about multiple satisfiers without preferred, got: %v", warnings)
}

func TestValidateWarnings_MultipleSatisfiersWithPreferred(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a", Satisfies: []string{"token1"}, Preferred: true, Outputs: []Output{{Name: "y", Type: "string"}}},
			"b": {Name: "b", Adapter: "b", Satisfies: []string{"token1"}, Outputs: []Output{{Name: "y", Type: "string"}}},
			"c": {Name: "c", Adapter: "c", Requires: []string{"token1"}, Inputs: []Input{{Name: "x", Type: "string"}}},
		},
	}

	warnings := ValidateWarnings(g)
	// No warning about multiple satisfiers since one is preferred.
	for _, w := range warnings {
		assert.False(t, containsSubstring(w, "preferred"), "unexpected warning: %s", w)
	}
}

// --- Workflow After/Wire Validation ---

func TestValidateWarnings_WorkflowUnknownKind(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Workflows: []Workflow{
			{Name: "Main", Kind: "bogus", Template: "workflows/main.yaml"},
		},
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a"},
		},
	}
	warnings := ValidateWarnings(g)
	require.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0], "unknown kind")
}

func TestValidateWarnings_NegativePriority(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Workflows: []Workflow{
			{Name: "Bad", Kind: "addon", Priority: -1, After: AfterSpec{"a"}, Template: "workflows/bad.yaml"},
		},
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a"},
		},
	}
	warnings := ValidateWarnings(g)
	require.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0], "negative priority")
}

func TestValidateWarnings_ZeroPriorityNoWarning(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Workflows: []Workflow{
			{Name: "OK", Kind: "addon", Priority: 0, After: AfterSpec{"a"}, Template: "workflows/ok.yaml"},
		},
		Nodes: map[string]*Node{
			"a": {Name: "a", Adapter: "a"},
		},
	}
	warnings := ValidateWarnings(g)
	for _, w := range warnings {
		assert.NotContains(t, w, "negative priority")
	}
}

func TestWorkflow_IsAddon(t *testing.T) {
	assert.True(t, Workflow{Kind: "addon"}.IsAddon())
	assert.False(t, Workflow{}.IsAddon())
	assert.False(t, Workflow{Kind: "other"}.IsAddon())
}

func TestParse_WorkflowWithAfterWire(t *testing.T) {
	g, err := ParseFile("testdata/valid/with_workflow_includes.yaml")
	require.NoError(t, err)

	// Workflows exist
	require.Len(t, g.Workflows, 2)

	// Addon workflow has After and Wire
	addon := g.Workflows[1]
	assert.Equal(t, "addon", addon.Kind)
	assert.True(t, addon.IsAddon())
	assert.Equal(t, AfterSpec{"book"}, addon.After)
	assert.Equal(t, "book.workbenchId", addon.Wire["workbenchId"])
}

// --- Error Detection Validation ---

func TestValidate_ErrorDetection_ValidGraphLevel(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		ErrorDetection: []ErrorDetectionRule{
			{Path: "ErrorResponse.Result.Error", Rule: "non-empty", Details: &ErrorDetailMapping{
				Message: "ErrorResponse.Result.Error.0.Message",
				Code:    "ErrorResponse.Result.Error.0.SourceCode",
			}},
		},
		Nodes: map[string]*Node{
			"n": {Name: "n", Adapter: "n", Outputs: []Output{{Name: "y", Type: "string"}}},
		},
	}
	err := Validate(g)
	assert.NoError(t, err)
}

func TestValidate_ErrorDetection_ValidNodeLevel(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"gql": {
				Name: "gql", Adapter: "gql",
				ErrorDetection: []ErrorDetectionRule{
					{Path: "errors", Rule: "non-empty"},
				},
				Outputs: []Output{{Name: "y", Type: "string"}},
			},
		},
	}
	err := Validate(g)
	assert.NoError(t, err)
}

func TestValidate_ErrorDetection_MissingPath(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		ErrorDetection: []ErrorDetectionRule{
			{Rule: "exists"},
		},
		Nodes: map[string]*Node{
			"n": {Name: "n", Adapter: "n", Outputs: []Output{{Name: "y", Type: "string"}}},
		},
	}
	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "path is required")
}

func TestValidate_ErrorDetection_MissingRule(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		ErrorDetection: []ErrorDetectionRule{
			{Path: "error"},
		},
		Nodes: map[string]*Node{
			"n": {Name: "n", Adapter: "n", Outputs: []Output{{Name: "y", Type: "string"}}},
		},
	}
	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "rule is required")
}

func TestValidate_ErrorDetection_UnknownRule(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		ErrorDetection: []ErrorDetectionRule{
			{Path: "error", Rule: "banana"},
		},
		Nodes: map[string]*Node{
			"n": {Name: "n", Adapter: "n", Outputs: []Output{{Name: "y", Type: "string"}}},
		},
	}
	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "unknown rule")
}

func TestValidate_ErrorDetection_EqualsRequiresValue(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		ErrorDetection: []ErrorDetectionRule{
			{Path: "success", Rule: "equals"},
		},
		Nodes: map[string]*Node{
			"n": {Name: "n", Adapter: "n", Outputs: []Output{{Name: "y", Type: "string"}}},
		},
	}
	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "equals rule requires a value")
}

func TestValidate_ErrorDetection_EqualsWithValue(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		ErrorDetection: []ErrorDetectionRule{
			{Path: "success", Rule: "equals", Value: false},
		},
		Nodes: map[string]*Node{
			"n": {Name: "n", Adapter: "n", Outputs: []Output{{Name: "y", Type: "string"}}},
		},
	}
	err := Validate(g)
	assert.NoError(t, err)
}

func TestValidate_ErrorDetection_InvalidDetailPath(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		ErrorDetection: []ErrorDetectionRule{
			{Path: "error", Rule: "exists", Details: &ErrorDetailMapping{
				Message: "error..message",
			}},
		},
		Nodes: map[string]*Node{
			"n": {Name: "n", Adapter: "n", Outputs: []Output{{Name: "y", Type: "string"}}},
		},
	}
	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "invalid details.message path")
}

func TestValidate_ErrorDetection_NodeLevelInvalid(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"n": {
				Name: "n", Adapter: "n",
				ErrorDetection: []ErrorDetectionRule{
					{Path: "errors", Rule: "bogus"},
				},
				Outputs: []Output{{Name: "y", Type: "string"}},
			},
		},
	}
	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "node \"n\": errorDetection[0]: unknown rule")
}
