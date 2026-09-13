package oas

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
)

func TestOperationsModel(t *testing.T) {
	model, err := LoadSpec("testdata/petstore.yaml")
	require.NoError(t, err)
	fullPaths := model.Paths.PathItems.Len()

	pruned := operationsModel(model, []string{"getPet"})
	var paths []string
	for path := range pruned.Paths.PathItems.KeysFromOldest() {
		paths = append(paths, path)
	}
	assert.Equal(t, []string{"/pets/{petId}"}, paths)
	assert.Same(t, model.Paths.PathItems.GetOrZero("/pets/{petId}"), pruned.Paths.PathItems.GetOrZero("/pets/{petId}"),
		"the copy shares its path items with the model")
	assert.Equal(t, fullPaths, model.Paths.PathItems.Len(), "the model keeps every path")

	assert.Equal(t, 0, operationsModel(model, nil).Paths.PathItems.Len())
}

// TestSpecCache_LoadOperations checks validation with a validator built for
// some of a spec's operations, including a step on an operation left out.
func TestSpecCache_LoadOperations(t *testing.T) {
	_, specPath := writeTestSpec(t)
	cache := NewSpecCache()
	require.NoError(t, cache.LoadOperations("spec.yaml", specPath, []string{"getPet"}))

	entry := cache.Get("spec.yaml")
	require.NotNil(t, entry)
	_, _, _, _, err := FindOperation(entry.Model, "createPet")
	assert.NoError(t, err, "the stored model keeps every operation")

	headers := http.Header{"Content-Type": []string{"application/json"}}
	tests := []struct {
		name        string
		operationID string
		method      string
		path        string
		status      int
		body        string
		wantErrors  bool
	}{
		{"a listed operation with a valid response", "getPet", "GET", "/pets/1", 200, `{"id": 1, "name": "Fido"}`, false},
		{"a listed operation with an invalid response", "getPet", "GET", "/pets/1", 200, `{"name": "Fido"}`, true},
		{"an operation left out with a valid response", "createPet", "POST", "/pets", 201, `{"id": 1, "name": "Fido"}`, false},
		{"an operation left out with an invalid response", "createPet", "POST", "/pets", 201, `{"name": "Fido"}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &graph.Node{Name: tt.operationID, OAS: &graph.OASRef{OperationID: tt.operationID, Spec: "spec.yaml"}}
			result := ValidateStep(node, "", cache, tt.method, tt.path, nil, nil, tt.status, headers, []byte(tt.body))
			require.NotNil(t, result)
			assert.False(t, result.Skipped)
			assert.Equal(t, tt.wantErrors, result.HasErrors(), "response: %+v", result.Response)
		})
	}
}
