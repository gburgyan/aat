package oas

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/index"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
)

func TestLoadSpec_CircularReferences(t *testing.T) {
	model, err := LoadSpec("testdata/circular.yaml")
	require.NoError(t, err)

	for _, id := range []string{"createOrder", "getOrder", "getRefund", "listCategories"} {
		_, _, _, _, err := FindOperation(model, id)
		assert.NoError(t, err, id)
	}
}

// TestCircularFixture_LibopenapiReportsCycle keeps the circular fixture
// meaningful: if a libopenapi upgrade stopped reporting its cycles as errors,
// the tests that load it would no longer cover buildV3Model's tolerance.
func TestCircularFixture_LibopenapiReportsCycle(t *testing.T) {
	data, err := os.ReadFile("testdata/circular.yaml")
	require.NoError(t, err)
	doc, err := libopenapi.NewDocumentWithConfiguration(data, documentConfiguration())
	require.NoError(t, err)

	built, err := doc.BuildV3Model()
	require.Error(t, err, "libopenapi no longer reports the fixture's circular references")
	assert.NotNil(t, built)
	assert.True(t, onlyCircularReferences(err), "errors other than circular references: %v", err)
}

func TestOnlyCircularReferences(t *testing.T) {
	circular := &index.ResolvingError{
		ErrorRef:          errors.New("infinite circular reference detected: Order"),
		CircularReference: &index.CircularReferenceResult{},
	}
	unresolved := &index.ResolvingError{ErrorRef: errors.New("cannot resolve reference")}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"one circular reference", circular, true},
		{"joined circular references", errors.Join(circular, circular), true},
		{"a circular reference and another error", errors.Join(circular, errors.New("bad spec")), false},
		{"an unresolvable reference", errors.Join(unresolved), false},
		{"a plain error", errors.New("bad spec"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, onlyCircularReferences(tt.err))
		})
	}
}

func TestSpecCache_LoadCircular(t *testing.T) {
	cache := NewSpecCache()
	require.NoError(t, cache.Load("circular.yaml", "testdata/circular.yaml"))

	entry := cache.Get("circular.yaml")
	require.NotNil(t, entry)
	assert.NotNil(t, entry.Model)
	assert.NotNil(t, entry.Validator)
}

// TestValidateStep_CircularResponseSchema checks that the validator compiles
// and applies a response schema whose properties refer back to it.
func TestValidateStep_CircularResponseSchema(t *testing.T) {
	cache := NewSpecCache()
	require.NoError(t, cache.Load("circular.yaml", "testdata/circular.yaml"))
	node := &graph.Node{
		Name: "getOrder",
		OAS:  &graph.OASRef{OperationID: "getOrder", Spec: "circular.yaml"},
	}
	headers := http.Header{"Content-Type": []string{"application/json"}}

	t.Run("a valid body passes", func(t *testing.T) {
		body := []byte(`{"orderId": "ord-1", "status": "paid", "lastPayment": {"paymentId": "pay-1", "order": {"orderId": "ord-1", "status": "paid"}}}`)
		result := ValidateStep(node, "", cache, "GET", "/orders/ord-1", nil, nil, 200, headers, body)
		require.NotNil(t, result)
		assert.False(t, result.Skipped)
		require.NotNil(t, result.Response)
		assert.True(t, result.Response.Valid, "response errors: %v", result.Response.Errors)
		assert.Equal(t, 0, result.ErrorCount())
	})

	t.Run("a wrong type is an error", func(t *testing.T) {
		body := []byte(`{"orderId": 5, "status": "paid"}`)
		result := ValidateStep(node, "", cache, "GET", "/orders/ord-1", nil, nil, 200, headers, body)
		require.NotNil(t, result)
		assert.True(t, result.HasErrors())
	})
}

func TestGenerate_CircularSpec(t *testing.T) {
	model, err := LoadSpec("testdata/circular.yaml")
	require.NoError(t, err)

	result, err := Generate(model, "circular.yaml")
	require.NoError(t, err)
	assert.Contains(t, result.Graph.Nodes, "createOrder")
	assert.Contains(t, result.Graph.Nodes, "getOrder")

	categories := result.Graph.Nodes["listCategories"]
	require.NotNil(t, categories)
	var treeType string
	for _, out := range categories.Outputs {
		if out.Name == "tree" {
			treeType = out.Type
		}
	}
	assert.True(t, strings.HasSuffix(treeType, "[]"), "tree output type %q", treeType)
}

// TestValidate_CompositionCycleTerminates checks an output path through
// schemas that compose each other in a cycle, which consume no path segment.
func TestValidate_CompositionCycleTerminates(t *testing.T) {
	spec := `openapi: "3.0.3"
info:
  title: Composition Cycle
  version: "1.0.0"
paths:
  /items/{itemId}:
    get:
      operationId: getItem
      parameters:
        - name: itemId
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: The item
          content:
            application/json:
              schema:
                type: object
                properties:
                  item:
                    $ref: "#/components/schemas/Item"
components:
  schemas:
    Item:
      allOf:
        - $ref: "#/components/schemas/Listing"
    Listing:
      allOf:
        - $ref: "#/components/schemas/Item"
`
	path := filepath.Join(t.TempDir(), "cycle.yaml")
	require.NoError(t, os.WriteFile(path, []byte(spec), 0o644))

	v := NewValidator().WithOutputPaths(OutputPaths{"getItem": {"sku": "item.sku"}})
	require.NoError(t, v.LoadSpec("cycle.yaml", path))
	g := &graph.Graph{
		Version: "1.0.0",
		OAS:     "cycle.yaml",
		Nodes: map[string]*graph.Node{
			"getItem": {
				Name:    "getItem",
				Adapter: "getItem",
				OAS:     &graph.OASRef{OperationID: "getItem"},
				Inputs:  []graph.Input{{Name: "itemId", Type: "string"}},
				Outputs: []graph.Output{{Name: "sku", Type: "string"}},
			},
		},
	}

	result := v.Validate(g)
	assert.False(t, result.HasErrors())
}
