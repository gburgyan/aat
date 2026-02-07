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

func TestValidate_BadEdgeRef(t *testing.T) {
	_, err := ParseFile("testdata/invalid/bad_edge_ref.yaml")
	require.Error(t, err)
	assertValidationContains(t, err, "unknown node")
}

func TestValidate_Cycle(t *testing.T) {
	_, err := ParseFile("testdata/invalid/cycle.yaml")
	require.Error(t, err)
	assertValidationContains(t, err, "cycle detected")
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

func TestValidate_SelectNonArray(t *testing.T) {
	_, err := ParseFile("testdata/invalid/select_non_array.yaml")
	require.Error(t, err)
	assertValidationContains(t, err, "not an array type")
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

func TestValidate_DuplicateEdge(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"a": {
				Name: "a", Adapter: "a",
				Inputs:  []Input{{Name: "x", Type: "string"}},
				Outputs: []Output{{Name: "y", Type: "string"}},
			},
			"b": {
				Name: "b", Adapter: "b",
				Inputs:  []Input{{Name: "x", Type: "string"}},
				Outputs: []Output{{Name: "y", Type: "string"}},
			},
		},
		Edges: []Edge{
			{From: "a.y", To: "b.x"},
			{From: "a.y", To: "b.x"},
		},
	}

	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "duplicate edge")
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

func TestValidate_EdgeBadFormat(t *testing.T) {
	g := &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"n": {
				Name: "n", Adapter: "n",
				Inputs:  []Input{{Name: "x", Type: "string"}},
				Outputs: []Output{{Name: "y", Type: "string"}},
			},
		},
		Edges: []Edge{
			{From: "nofield", To: "n.x"},
		},
	}

	err := Validate(g)
	require.Error(t, err)
	assertValidationContains(t, err, "invalid 'from'")
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
