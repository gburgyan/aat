package oas

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	assert.NotContains(t, result.Templates[0].Request.Headers, "Content-Type")
}

func TestGenerate_FormBodyLinearOptionalFields(t *testing.T) {
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
	bodies := map[string]string{}
	for _, tmpl := range result.Templates {
		bodies[tmpl.Adapter] = tmpl.Request.Body
	}
	assert.Equal(t, "orderId={{orderId}}{{?amount}}&amount={{amount}}{{/amount}}{{?note}}&note={{note}}{{/note}}", bodies["createRefund"])
	assert.Equal(t, "{{?limit}}&limit={{limit}}{{/limit}}{{?status}}&status={{status}}{{/status}}{{?startingAfter}}&startingAfter={{startingAfter}}{{/startingAfter}}", bodies["searchRefunds"])
}

func TestGenerate_FormObjectPropertiesWarn(t *testing.T) {
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
	var objectWarnings []string
	for _, w := range result.Warnings {
		if strings.Contains(w, "object form properties") {
			objectWarnings = append(objectWarnings, w)
		}
	}
	require.Len(t, objectWarnings, 1, "warnings: %v", result.Warnings)
	assert.Contains(t, objectWarnings[0], "metadata, shipping, items as JSON text")
	assert.Contains(t, objectWarnings[0], "metadata[key]=value")
	assert.NotContains(t, objectWarnings[0], "amount")
	assert.NotContains(t, objectWarnings[0], "tags")
}
