package graph

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadLayersFromDir_MissingDirHoldsNoLayers(t *testing.T) {
	layers, err := LoadLayersFromDir(filepath.Join(t.TempDir(), "layers"))
	require.NoError(t, err)
	assert.Empty(t, layers)
}

func TestResolveLayerNames_MissingDirSaysSo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "layers")
	_, err := ResolveLayerNames([]string{"card-visa"}, dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `layer "card-visa" not found: the layers directory `+dir+` doesn't exist`)
}
