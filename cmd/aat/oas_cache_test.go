package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
)

func TestSpecOperationIDs(t *testing.T) {
	g := &graph.Graph{
		OAS: "shop.yaml",
		Nodes: map[string]*graph.Node{
			"getCart":       {OAS: &graph.OASRef{OperationID: "getCart"}},
			"addItem":       {OAS: &graph.OASRef{OperationID: "addItem"}},
			"chargePayment": {OAS: &graph.OASRef{OperationID: "chargePayment", Spec: "payments.yaml"}},
			"noOperation":   {OAS: &graph.OASRef{}},
			"noSpec":        {},
		},
	}
	assert.Equal(t, []string{"addItem", "getCart"}, specOperationIDs(g, "shop.yaml"))
	assert.Equal(t, []string{"chargePayment"}, specOperationIDs(g, "payments.yaml"))
	assert.Empty(t, specOperationIDs(g, "other.yaml"))
}

func TestLoadOASCache(t *testing.T) {
	missingGraph := filepath.Join(t.TempDir(), "graph.yaml")
	tests := []struct {
		name      string
		graph     *graph.Graph
		graphPath string
		mode      string
		wantSpecs int
		wantErr   string
		wantWarn  string
	}{
		{name: "off reads nothing", graph: &graph.Graph{OAS: "missing.yaml"}, graphPath: missingGraph, mode: "off"},
		{name: "no spec referenced", graph: &graph.Graph{}, graphPath: missingGraph, mode: "strict"},
		{name: "auto warns about a spec that doesn't load", graph: &graph.Graph{OAS: "missing.yaml"}, graphPath: missingGraph, mode: "auto", wantWarn: "missing.yaml"},
		{name: "strict fails on a spec that doesn't load", graph: &graph.Graph{OAS: "missing.yaml"}, graphPath: missingGraph, mode: "strict", wantErr: "missing.yaml"},
		{name: "a spec that loads", graph: &graph.Graph{OAS: "oas/petstore.yaml"}, graphPath: "testdata/graph.yaml", mode: "strict", wantSpecs: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var warn bytes.Buffer
			cache, err := loadOASCache(tt.graph, tt.graphPath, tt.mode, &warn)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "strict OAS validation")
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, cache)
				return
			}
			require.NoError(t, err)
			if tt.wantWarn != "" {
				assert.Contains(t, warn.String(), tt.wantWarn)
			} else {
				assert.Empty(t, warn.String())
			}
			if tt.wantSpecs == 0 {
				assert.Nil(t, cache)
				return
			}
			require.NotNil(t, cache)
			assert.Equal(t, tt.wantSpecs, cache.Len())
		})
	}
}
