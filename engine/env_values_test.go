package engine

import (
	"os"
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
)

func TestBuildResolveContext_EnvValuesUsedWhenOSUnset(t *testing.T) {
	eng := &Engine{
		envValues: map[string]string{"pcc": "XB7"},
	}
	node := &graph.Node{Name: "test"}
	rctx := eng.buildResolveContext(node)

	assert.Equal(t, "XB7", rctx.EnvLookup("pcc"))
}

func TestBuildResolveContext_OSEnvTakesPriority(t *testing.T) {
	t.Setenv("pcc", "OVERRIDE")

	eng := &Engine{
		envValues: map[string]string{"pcc": "XB7"},
	}
	node := &graph.Node{Name: "test"}
	rctx := eng.buildResolveContext(node)

	assert.Equal(t, "OVERRIDE", rctx.EnvLookup("pcc"))
}

func TestBuildResolveContext_MissingFromBothReturnsEmpty(t *testing.T) {
	eng := &Engine{
		envValues: map[string]string{"pcc": "XB7"},
	}
	node := &graph.Node{Name: "test"}
	rctx := eng.buildResolveContext(node)

	assert.Equal(t, "", rctx.EnvLookup("NONEXISTENT_KEY_12345"))
}

func TestBuildResolveContext_NilEnvValuesUsesOsGetenv(t *testing.T) {
	t.Setenv("AAT_TEST_KEY_99", "from-os")

	eng := &Engine{}
	node := &graph.Node{Name: "test"}
	rctx := eng.buildResolveContext(node)

	// Should use plain os.Getenv (same function pointer)
	assert.Equal(t, "from-os", rctx.EnvLookup("AAT_TEST_KEY_99"))
	assert.Equal(t, os.Getenv("PATH"), rctx.EnvLookup("PATH"))
}

func TestWithEnvValues_Chaining(t *testing.T) {
	eng := NewEngine(nil, nil, nil)
	result := eng.WithEnvValues(map[string]string{"k": "v"})
	assert.Same(t, eng, result)
	assert.Equal(t, "v", eng.envValues["k"])
}
