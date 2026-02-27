package oas

import (
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSpec_Valid(t *testing.T) {
	model, err := LoadSpec("testdata/petstore.yaml")
	require.NoError(t, err)
	require.NotNil(t, model)
	assert.Equal(t, "Petstore", model.Info.Title)
}

func TestLoadSpec_FileNotFound(t *testing.T) {
	_, err := LoadSpec("testdata/nonexistent.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading OAS spec")
}

func TestLoadSpec_InvalidSpec(t *testing.T) {
	// Use a graph YAML file which is not a valid OAS spec
	_, err := LoadSpec("../../graph/testdata/valid/minimal.yaml")
	require.Error(t, err)
}

func TestFindOperation_Found(t *testing.T) {
	model, err := LoadSpec("testdata/petstore.yaml")
	require.NoError(t, err)

	tests := []struct {
		operationID string
		wantMethod  string
		wantPath    string
	}{
		{"listPets", "GET", "/pets"},
		{"createPet", "POST", "/pets"},
		{"getPet", "GET", "/pets/{petId}"},
		{"deletePet", "DELETE", "/pets/{petId}"},
	}

	for _, tt := range tests {
		t.Run(tt.operationID, func(t *testing.T) {
			method, path, pathItem, op, err := FindOperation(model, tt.operationID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantMethod, method)
			assert.Equal(t, tt.wantPath, path)
			assert.NotNil(t, pathItem)
			assert.NotNil(t, op)
			assert.Equal(t, tt.operationID, op.OperationId)
		})
	}
}

func TestFindOperation_NotFound(t *testing.T) {
	model, err := LoadSpec("testdata/petstore.yaml")
	require.NoError(t, err)

	_, _, _, _, err = FindOperation(model, "nonExistentOp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonExistentOp")
	assert.Contains(t, err.Error(), "not found")
}

func TestResolveNodeSpec(t *testing.T) {
	tests := []struct {
		name     string
		node     *graph.Node
		graphOAS string
		want     string
	}{
		{
			name:     "node override",
			node:     &graph.Node{OAS: &graph.OASRef{OperationID: "op", Spec: "node-spec.yaml"}},
			graphOAS: "graph-spec.yaml",
			want:     "node-spec.yaml",
		},
		{
			name:     "graph default",
			node:     &graph.Node{OAS: &graph.OASRef{OperationID: "op"}},
			graphOAS: "graph-spec.yaml",
			want:     "graph-spec.yaml",
		},
		{
			name:     "neither set",
			node:     &graph.Node{OAS: &graph.OASRef{OperationID: "op"}},
			graphOAS: "",
			want:     "",
		},
		{
			name:     "no OAS ref",
			node:     &graph.Node{},
			graphOAS: "graph-spec.yaml",
			want:     "graph-spec.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveNodeSpec(tt.node, tt.graphOAS)
			assert.Equal(t, tt.want, got)
		})
	}
}
