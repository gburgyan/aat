package oas

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
)

func newValidatorWithPetstore(t *testing.T) *Validator {
	t.Helper()
	v := NewValidator()
	err := v.LoadSpec("petstore.yaml", "testdata/petstore.yaml")
	require.NoError(t, err)
	return v
}

func TestValidate_ValidGraph(t *testing.T) {
	v := newValidatorWithPetstore(t)
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "petstore.yaml",
		Nodes: map[string]*graph.Node{
			"listPets": {
				Name:    "listPets",
				Adapter: "listPets",
				OAS:     &graph.OASRef{OperationID: "listPets"},
				Inputs: []graph.Input{
					{Name: "limit", Type: "integer"},
				},
				Outputs: []graph.Output{
					{Name: "id", Type: "integer"},
					{Name: "name", Type: "string"},
				},
			},
		},
	}

	result := v.Validate(g)
	assert.False(t, result.HasErrors())
}

func TestValidate_PathItemParameters(t *testing.T) {
	v := NewValidator()
	require.NoError(t, v.LoadSpec("carts.yaml", "testdata/path_item_params.yaml"))

	graphWithInputs := func(names ...string) *graph.Graph {
		inputs := make([]graph.Input, 0, len(names))
		for _, n := range names {
			inputs = append(inputs, graph.Input{Name: n, Type: "string"})
		}
		return &graph.Graph{
			Version: "1.0.0",
			OAS:     "carts.yaml",
			Nodes: map[string]*graph.Node{
				"getCart": {
					Name:    "getCart",
					Adapter: "getCart",
					OAS:     &graph.OASRef{OperationID: "getCart"},
					Inputs:  inputs,
					Outputs: []graph.Output{{Name: "cartId", Type: "string"}},
				},
			},
		}
	}

	t.Run("path-item parameters are known inputs", func(t *testing.T) {
		result := v.Validate(graphWithInputs("cartId", "X-Trace"))
		assert.False(t, result.HasIssues(), result.Format())
	})

	t.Run("a required path-item parameter must be a graph input", func(t *testing.T) {
		result := v.Validate(graphWithInputs("X-Trace"))
		require.True(t, result.HasIssues())
		assert.Contains(t, result.Format(), `OAS required parameter "cartId" missing`)
	})

	t.Run("an operation parameter overrides the path-item declaration", func(t *testing.T) {
		result := v.Validate(graphWithInputs("cartId"))
		require.True(t, result.HasIssues())
		assert.Contains(t, result.Format(), `OAS required parameter "X-Trace" missing`)
	})
}

func TestValidate_OutputExtractPaths(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		paths   OutputPaths // nil: no template paths known
		wantMsg string      // empty: no issue expected
	}{
		{name: "top-level output name", output: "status"},
		{name: "renamed output without paths", output: "cartStatus", wantMsg: `output "cartStatus" not found in OAS 2xx response schema`},
		{name: "renamed output at its extract path", output: "cartStatus", paths: OutputPaths{"getCart": {"cartStatus": "status"}}},
		{name: "nested object path", output: "subtotal", paths: OutputPaths{"getCart": {"subtotal": "totals.subtotal"}}},
		{name: "array items path", output: "skus", paths: OutputPaths{"getCart": {"skus": "lines.#.sku"}}},
		{name: "array index path", output: "firstSku", paths: OutputPaths{"getCart": {"firstSku": "lines.0.sku"}}},
		{
			name:    "missing nested property",
			output:  "tax",
			paths:   OutputPaths{"getCart": {"tax": "totals.tax"}},
			wantMsg: `output "tax" (extracted from "totals.tax") not found in OAS 2xx response schema`,
		},
		{name: "additionalProperties map", output: "channel", paths: OutputPaths{"getCart": {"channel": "metadata.channel"}}},
		{name: "computed by a transform", output: "itemCount", paths: OutputPaths{"getCart": {"itemCount": ""}}},
		{name: "GJSON query is not checked", output: "gearSku", paths: OutputPaths{"getCart": {"gearSku": `lines.#(category=="gear").sku`}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewValidator().WithOutputPaths(tt.paths)
			require.NoError(t, v.LoadSpec("carts.yaml", "testdata/output_paths.yaml"))
			g := &graph.Graph{
				Version: "1.0.0",
				OAS:     "carts.yaml",
				Nodes: map[string]*graph.Node{
					"getCart": {
						Name:    "getCart",
						Adapter: "getCart",
						OAS:     &graph.OASRef{OperationID: "getCart"},
						Inputs:  []graph.Input{{Name: "cartId", Type: "string"}},
						Outputs: []graph.Output{{Name: tt.output, Type: "string"}},
					},
				},
			}

			result := v.Validate(g)
			if tt.wantMsg == "" {
				assert.False(t, result.HasIssues(), result.Format())
				return
			}
			require.True(t, result.HasIssues())
			assert.Contains(t, result.Format(), tt.wantMsg)
		})
	}
}

func TestValidate_Rule1_EmptyOperationID(t *testing.T) {
	v := NewValidator()
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "petstore.yaml",
		Nodes: map[string]*graph.Node{
			"badNode": {
				Name:    "badNode",
				Adapter: "test",
				OAS:     &graph.OASRef{OperationID: ""},
			},
		},
	}

	result := v.Validate(g)
	assert.True(t, result.HasErrors())
	assert.Len(t, result.Issues, 1)
	assert.Equal(t, graph.SpecError, result.Issues[0].Severity)
	assert.Contains(t, result.Issues[0].Message, "operationId is empty")
}

func TestValidate_Rule2_NoSpecPath(t *testing.T) {
	v := NewValidator()
	g := &graph.Graph{
		Version: "1.0.0",
		// No graph-level OAS
		Nodes: map[string]*graph.Node{
			"orphan": {
				Name:    "orphan",
				Adapter: "test",
				OAS:     &graph.OASRef{OperationID: "listPets"},
				// No node-level spec
			},
		},
	}

	result := v.Validate(g)
	assert.True(t, result.HasErrors())
	assert.Len(t, result.Issues, 1)
	assert.Contains(t, result.Issues[0].Message, "no OAS spec path")
}

func TestValidate_Rule3_SpecNotLoaded(t *testing.T) {
	v := NewValidator()
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "missing.yaml",
		Nodes: map[string]*graph.Node{
			"node": {
				Name:    "node",
				Adapter: "test",
				OAS:     &graph.OASRef{OperationID: "listPets"},
			},
		},
	}

	result := v.Validate(g)
	assert.True(t, result.HasErrors())
	assert.Contains(t, result.Issues[0].Message, "not loaded")
}

func TestValidate_Rule4_OperationNotFound(t *testing.T) {
	v := newValidatorWithPetstore(t)
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "petstore.yaml",
		Nodes: map[string]*graph.Node{
			"node": {
				Name:    "node",
				Adapter: "test",
				OAS:     &graph.OASRef{OperationID: "nonExistent"},
			},
		},
	}

	result := v.Validate(g)
	assert.True(t, result.HasErrors())
	assert.Contains(t, result.Issues[0].Message, "nonExistent")
	assert.Contains(t, result.Issues[0].Message, "not found")
}

func TestValidate_Rule5_InputNotInOAS(t *testing.T) {
	v := newValidatorWithPetstore(t)
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "petstore.yaml",
		Nodes: map[string]*graph.Node{
			"node": {
				Name:    "node",
				Adapter: "test",
				OAS:     &graph.OASRef{OperationID: "listPets"},
				Inputs: []graph.Input{
					{Name: "limit", Type: "integer"},     // exists in OAS
					{Name: "extraParam", Type: "string"}, // does NOT exist in OAS
				},
			},
		},
	}

	result := v.Validate(g)
	assert.False(t, result.HasErrors())
	assert.True(t, result.HasIssues())

	// Should have exactly one warning for extraParam
	var warnings []graph.SpecValidationIssue
	for _, issue := range result.Issues {
		if issue.Severity == graph.SpecWarning {
			warnings = append(warnings, issue)
		}
	}
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Message, "extraParam")
}

func TestValidate_Rule6_RequiredParamMissing(t *testing.T) {
	v := newValidatorWithPetstore(t)
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "petstore.yaml",
		Nodes: map[string]*graph.Node{
			"node": {
				Name:    "node",
				Adapter: "test",
				OAS:     &graph.OASRef{OperationID: "getPet"},
				Inputs:  []graph.Input{
					// Missing petId, which is required in OAS
				},
			},
		},
	}

	result := v.Validate(g)
	assert.False(t, result.HasErrors())
	assert.True(t, result.HasIssues())

	found := false
	for _, issue := range result.Issues {
		if issue.Severity == graph.SpecWarning {
			if contains(issue.Message, "petId") && contains(issue.Message, "required") {
				found = true
			}
		}
	}
	assert.True(t, found, "expected warning about missing required parameter petId")
}

// TestValidate_RequiredFieldSuppliedByTemplate: a required body property the
// template sends itself (with no graph input behind it) is not reported
// missing; one the template does not send still is.
func TestValidate_RequiredFieldSuppliedByTemplate(t *testing.T) {
	v := newValidatorWithPetstore(t)
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "petstore.yaml",
		Nodes: map[string]*graph.Node{
			"createPet": {
				Name:    "createPet",
				Adapter: "createPet",
				OAS:     &graph.OASRef{OperationID: "createPet"},
				Inputs:  []graph.Input{{Name: "tag", Type: "string", Optional: true}},
			},
		},
	}

	missing := func(result *graph.SpecValidationResult) bool {
		for _, issue := range result.Issues {
			if contains(issue.Message, `required parameter "name"`) {
				return true
			}
		}
		return false
	}

	assert.True(t, missing(v.Validate(g)), "without template knowledge the required name is missing")

	v.WithSuppliedFields(SuppliedFields{"createPet": {"name": true}})
	assert.False(t, missing(v.Validate(g)), "the template supplies name itself")
}

// TestValidate_HeaderOnlyInputNotReported: an input the template sends only in
// a request header is not reported as missing from the operation's parameters
// and request body.
func TestValidate_HeaderOnlyInputNotReported(t *testing.T) {
	v := newValidatorWithPetstore(t)
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "petstore.yaml",
		Nodes: map[string]*graph.Node{
			"createPet": {
				Name:    "createPet",
				Adapter: "createPet",
				OAS:     &graph.OASRef{OperationID: "createPet"},
				Inputs: []graph.Input{
					{Name: "name", Type: "string"},
					{Name: "requestKey", Type: "string", Optional: true},
				},
			},
		},
	}

	reported := func(result *graph.SpecValidationResult) bool {
		for _, issue := range result.Issues {
			if contains(issue.Message, `input "requestKey" not found`) {
				return true
			}
		}
		return false
	}

	assert.True(t, reported(v.Validate(g)), "without template knowledge requestKey is not a parameter")

	v.WithHeaderInputs(HeaderInputs{"createPet": {"requestKey": true}})
	assert.False(t, reported(v.Validate(g)), "the template sends requestKey only in a header")
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestValidate_Rule7_OutputNotInOAS(t *testing.T) {
	v := newValidatorWithPetstore(t)
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "petstore.yaml",
		Nodes: map[string]*graph.Node{
			"node": {
				Name:    "node",
				Adapter: "test",
				OAS:     &graph.OASRef{OperationID: "getPet"},
				Inputs: []graph.Input{
					{Name: "petId", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "id", Type: "integer"},         // exists in Pet schema
					{Name: "name", Type: "string"},        // exists in Pet schema
					{Name: "customField", Type: "string"}, // NOT in Pet schema
				},
			},
		},
	}

	result := v.Validate(g)
	assert.False(t, result.HasErrors())

	found := false
	for _, issue := range result.Issues {
		if issue.Severity == graph.SpecWarning && contains(issue.Message, "customField") {
			found = true
		}
	}
	assert.True(t, found, "expected warning about output customField not in OAS response")
}

func TestValidate_NodeWithoutOAS_Skipped(t *testing.T) {
	v := newValidatorWithPetstore(t)
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "petstore.yaml",
		Nodes: map[string]*graph.Node{
			"withOAS": {
				Name:    "withOAS",
				Adapter: "test",
				OAS:     &graph.OASRef{OperationID: "listPets"},
			},
			"withoutOAS": {
				Name:    "withoutOAS",
				Adapter: "test",
				// No OAS
			},
		},
	}

	result := v.Validate(g)
	// Should not produce any issues for the node without OAS
	for _, issue := range result.Issues {
		assert.NotEqual(t, "withoutOAS", issue.Node)
	}
}

func TestValidate_MultipleNodes_MultipleIssues(t *testing.T) {
	v := newValidatorWithPetstore(t)
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "petstore.yaml",
		Nodes: map[string]*graph.Node{
			"badOp": {
				Name:    "badOp",
				Adapter: "test",
				OAS:     &graph.OASRef{OperationID: "nonExistent"},
			},
			"emptyOpId": {
				Name:    "emptyOpId",
				Adapter: "test",
				OAS:     &graph.OASRef{OperationID: ""},
			},
		},
	}

	result := v.Validate(g)
	assert.True(t, result.HasErrors())
	assert.Len(t, result.Issues, 2) // one per node
}

func TestValidate_NodeSpecOverride(t *testing.T) {
	v := newValidatorWithPetstore(t)
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "default.yaml",
		Nodes: map[string]*graph.Node{
			"node": {
				Name:    "node",
				Adapter: "test",
				OAS:     &graph.OASRef{OperationID: "listPets", Spec: "petstore.yaml"},
			},
		},
	}

	// Only petstore.yaml loaded, not default.yaml
	result := v.Validate(g)
	assert.False(t, result.HasErrors())
}

func TestValidate_RequestBodyInputs(t *testing.T) {
	v := newValidatorWithPetstore(t)
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "petstore.yaml",
		Nodes: map[string]*graph.Node{
			"create": {
				Name:    "create",
				Adapter: "test",
				OAS:     &graph.OASRef{OperationID: "createPet"},
				Inputs: []graph.Input{
					{Name: "name", Type: "string"}, // in request body
					{Name: "tag", Type: "string"},  // in request body
				},
			},
		},
	}

	result := v.Validate(g)
	assert.False(t, result.HasErrors())

	// name and tag are both in the request body schema — should match
	inputWarnings := 0
	for _, issue := range result.Issues {
		if issue.Severity == graph.SpecWarning && contains(issue.Message, "not found in OAS parameters") {
			inputWarnings++
		}
	}
	assert.Equal(t, 0, inputWarnings)
}

func TestValidate_RequiredRequestBodyProp(t *testing.T) {
	v := newValidatorWithPetstore(t)
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "petstore.yaml",
		Nodes: map[string]*graph.Node{
			"create": {
				Name:    "create",
				Adapter: "test",
				OAS:     &graph.OASRef{OperationID: "createPet"},
				Inputs: []graph.Input{
					// Missing "name" which is required in createPet request body
					{Name: "tag", Type: "string"},
				},
			},
		},
	}

	result := v.Validate(g)
	assert.False(t, result.HasErrors())

	found := false
	for _, issue := range result.Issues {
		if issue.Severity == graph.SpecWarning && contains(issue.Message, "name") && contains(issue.Message, "required") {
			found = true
		}
	}
	assert.True(t, found, "expected warning about missing required body property 'name'")
}

func TestValidate_NoResponseSchema_SkipsRule7(t *testing.T) {
	v := newValidatorWithPetstore(t)
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "petstore.yaml",
		Nodes: map[string]*graph.Node{
			"delete": {
				Name:    "delete",
				Adapter: "test",
				OAS:     &graph.OASRef{OperationID: "deletePet"},
				Inputs: []graph.Input{
					{Name: "petId", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "anything", Type: "string"}, // deletePet has 204 with no content
				},
			},
		},
	}

	result := v.Validate(g)
	assert.False(t, result.HasErrors())

	// Since deletePet has no 2xx response body, rule 7 should be skipped
	for _, issue := range result.Issues {
		assert.NotContains(t, issue.Message, "not found in OAS 2xx response")
	}
}

func TestSpecValidationResult_Format(t *testing.T) {
	result := &graph.SpecValidationResult{
		Issues: []graph.SpecValidationIssue{
			{Severity: graph.SpecError, Node: "node1", Message: "operationId not found"},
			{Severity: graph.SpecWarning, Node: "node2", Message: "extra input"},
		},
	}

	formatted := result.Format()
	assert.Contains(t, formatted, "Errors:")
	assert.Contains(t, formatted, "Warnings:")
	assert.Contains(t, formatted, "operationId not found")
	assert.Contains(t, formatted, "extra input")
}

func TestSpecValidationResult_Error(t *testing.T) {
	result := &graph.SpecValidationResult{
		Issues: []graph.SpecValidationIssue{
			{Severity: graph.SpecError, Node: "node1", Message: "bad op"},
			{Severity: graph.SpecWarning, Node: "node2", Message: "extra input"},
		},
	}

	errStr := result.Error()
	assert.Contains(t, errStr, "bad op")
	assert.NotContains(t, errStr, "extra input") // warnings excluded from Error()
}

func TestSpecValidationResult_NoIssues(t *testing.T) {
	result := &graph.SpecValidationResult{}
	assert.False(t, result.HasErrors())
	assert.False(t, result.HasIssues())
	assert.Empty(t, result.Error())
	assert.Empty(t, result.Format())
}

func TestCollectSpecPaths(t *testing.T) {
	v := NewValidator()

	g := &graph.Graph{
		OAS: "default.yaml",
		Nodes: map[string]*graph.Node{
			"a": {OAS: &graph.OASRef{OperationID: "op1", Spec: "a-spec.yaml"}},
			"b": {OAS: &graph.OASRef{OperationID: "op2"}},
			"c": {}, // no OAS
		},
	}

	paths := v.CollectSpecPaths(g)
	assert.Contains(t, paths, "default.yaml")
	assert.Contains(t, paths, "a-spec.yaml")
	assert.Len(t, paths, 2) // deduped, no empty
}

func TestCollectSpecPaths_NoOAS(t *testing.T) {
	v := NewValidator()
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"a": {},
		},
	}

	paths := v.CollectSpecPaths(g)
	assert.Empty(t, paths)
}

func TestGetSpec(t *testing.T) {
	v := newValidatorWithPetstore(t)

	model := v.GetSpec("petstore.yaml")
	require.NotNil(t, model)
	assert.Equal(t, "Petstore", model.Info.Title)

	assert.Nil(t, v.GetSpec("nonexistent.yaml"))
}

func TestLoadSpec_Error(t *testing.T) {
	v := NewValidator()
	err := v.LoadSpec("bad.yaml", "testdata/nonexistent.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading OAS spec")
}
