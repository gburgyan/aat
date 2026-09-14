package oas

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generatedNodeNames returns the sorted node names of a generated graph.
func generatedNodeNames(result *GenerateResult) []string {
	names := make([]string, 0, len(result.Graph.Nodes))
	for name := range result.Graph.Nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestGenerateOperations_Filters(t *testing.T) {
	model, err := LoadSpec("testdata/petstore.yaml")
	require.NoError(t, err)

	tests := []struct {
		name      string
		opts      GenerateOptions
		wantNodes []string
		wantErr   string
	}{
		{name: "by operationId", opts: GenerateOptions{OperationIDs: []string{"getPet", "createPet"}}, wantNodes: []string{"createPet", "getPet"}},
		{name: "by path prefix, including paths under it", opts: GenerateOptions{PathPrefixes: []string{"/pets"}}, wantNodes: []string{"createPet", "deletePet", "getPet", "listPets"}},
		{name: "by a deeper path prefix", opts: GenerateOptions{PathPrefixes: []string{"/pets/{petId}"}}, wantNodes: []string{"deletePet", "getPet"}},
		{name: "a trailing slash on a prefix", opts: GenerateOptions{PathPrefixes: []string{"/pets/"}}, wantNodes: []string{"createPet", "deletePet", "getPet", "listPets"}},
		{name: "filters combine as a union", opts: GenerateOptions{OperationIDs: []string{"listPets"}, PathPrefixes: []string{"/pets/{petId}"}}, wantNodes: []string{"deletePet", "getPet", "listPets"}},
		{name: "an unknown operationId suggests similar ones", opts: GenerateOptions{OperationIDs: []string{"getpet"}}, wantErr: `operationId "getpet" is not in the spec (did you mean getPet?)`},
		{name: "a prefix matches whole segments only", opts: GenerateOptions{PathPrefixes: []string{"/pe"}}, wantErr: `path "/pe" matches no path in the spec`},
		{name: "a prefix must start with /", opts: GenerateOptions{PathPrefixes: []string{"pets"}}, wantErr: `path "pets" must start with /`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GenerateOperations(model, "petstore.yaml", tt.opts)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantNodes, generatedNodeNames(result))
			assert.Len(t, result.Templates, len(tt.wantNodes))
		})
	}
}

// writeGenerateSpec writes an OpenAPI 3.0 spec with the given paths section and
// loads it.
func writeGenerateSpec(t *testing.T, paths string) *GenerateResult {
	t.Helper()
	spec := "openapi: \"3.0.3\"\ninfo:\n  title: Shop\n  version: \"1.0.0\"\npaths:\n" + paths
	path := filepath.Join(t.TempDir(), "shop.yaml")
	require.NoError(t, os.WriteFile(path, []byte(spec), 0o644))
	model, err := LoadSpec(path)
	require.NoError(t, err)
	result, err := Generate(model, "shop.yaml")
	require.NoError(t, err)
	return result
}

func TestGenerate_EmptyFormBodyNoWarning(t *testing.T) {
	result := writeGenerateSpec(t, `  /orders:
    get:
      operationId: listOrders
      requestBody:
        content:
          application/x-www-form-urlencoded:
            schema:
              type: object
              properties: {}
              additionalProperties: false
      responses:
        "200":
          description: The orders
`)
	assert.Empty(t, result.Warnings)
	require.Len(t, result.Templates, 1)
	assert.Empty(t, result.Templates[0].Request.Body)
	assert.Nil(t, result.Templates[0].Request.Form)
	assert.NotContains(t, result.Templates[0].Request.Headers, "Content-Type")
}

func TestGenerate_FormBodyFields(t *testing.T) {
	result := writeGenerateSpec(t, `  /refunds:
    post:
      operationId: createRefund
      requestBody:
        content:
          application/x-www-form-urlencoded:
            schema:
              type: object
              required:
                - orderId
              properties:
                orderId:
                  type: string
                amount:
                  type: integer
                note:
                  type: string
      responses:
        "200":
          description: The refund
  /refunds/search:
    post:
      operationId: searchRefunds
      requestBody:
        content:
          application/x-www-form-urlencoded:
            schema:
              type: object
              properties:
                limit:
                  type: integer
                status:
                  type: string
                startingAfter:
                  type: string
      responses:
        "200":
          description: The refunds
`)
	forms := map[string]ScaffoldForm{}
	for _, tmpl := range result.Templates {
		assert.Empty(t, tmpl.Request.Body)
		assert.NotContains(t, tmpl.Request.Headers, "Content-Type", "request.form sets it")
		forms[tmpl.Adapter] = tmpl.Request.Form
	}
	assert.Equal(t, ScaffoldForm{{"orderId", "{{orderId}}"}, {"amount", "{{amount}}"}, {"note", "{{note}}"}}, forms["createRefund"])
	assert.Equal(t, ScaffoldForm{{"limit", "{{limit}}"}, {"status", "{{status}}"}, {"startingAfter", "{{startingAfter}}"}}, forms["searchRefunds"])
}

func TestGenerate_FormObjectProperties(t *testing.T) {
	result := writeGenerateSpec(t, `  /refunds:
    post:
      operationId: createRefund
      requestBody:
        content:
          application/x-www-form-urlencoded:
            schema:
              type: object
              properties:
                amount:
                  type: integer
                metadata:
                  type: object
                  additionalProperties:
                    type: string
                shipping:
                  anyOf:
                    - type: object
                      properties:
                        city:
                          type: string
                    - type: string
                      enum:
                        - ""
                items:
                  type: array
                  items:
                    type: object
                    properties:
                      sku:
                        type: string
                tags:
                  type: array
                  items:
                    type: string
      responses:
        "200":
          description: The refund
`)
	assert.Empty(t, result.Warnings, "request.form sends an object value as bracketed keys")
	require.Len(t, result.Templates, 1)
	assert.Equal(t, ScaffoldForm{
		{"amount", "{{amount}}"},
		{"metadata", "{{metadata}}"},
		{"shipping", "{{shipping}}"},
		{"items", "{{items}}"},
		{"tags", "{{tags}}"},
	}, result.Templates[0].Request.Form)
}

// TestGenerate_FormDeepObjectArrays checks that an array property the spec
// encodes as a deepObject, as Stripe's spec does, is written with [] and an
// object property keeps its plain name.
func TestGenerate_FormDeepObjectArrays(t *testing.T) {
	result := writeGenerateSpec(t, `  /charges:
    post:
      operationId: createCharge
      requestBody:
        content:
          application/x-www-form-urlencoded:
            schema:
              type: object
              properties:
                expand:
                  type: array
                  items:
                    type: string
                metadata:
                  type: object
            encoding:
              expand:
                style: deepObject
                explode: true
              metadata:
                style: deepObject
                explode: true
      responses:
        "200":
          description: The charge
`)
	require.Len(t, result.Templates, 1)
	assert.Equal(t, ScaffoldForm{{"expand[]", "{{expand}}"}, {"metadata", "{{metadata}}"}}, result.Templates[0].Request.Form)
}
