package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunTransform_CannotLoadCode checks that a transform cannot load code from
// files or strings, change function environments, or act on the host process
// through the base library or the package library.
func TestRunTransform_CannotLoadCode(t *testing.T) {
	for _, name := range []string{
		"dofile", "loadfile", "load", "loadstring", "require", "module",
		"getfenv", "setfenv", "collectgarbage", "newproxy", "_printregs", "package",
	} {
		result, err := runTransform(`return { present = `+name+` ~= nil }`, map[string]any{}, "{}")
		require.NoError(t, err, name)
		assert.Equal(t, false, result["present"], name)
	}
}

func TestRunTransform_DofileFails(t *testing.T) {
	_, err := runTransform(`
		dofile("/etc/hosts")
		return outputs
	`, map[string]any{}, "{}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lua script error")
}

func TestRunTransform_BaseLibraryStillWorks(t *testing.T) {
	result, err := runTransform(`
		local ok, message = pcall(function() error("boom") end)
		outputs.ok = ok
		outputs.count = tonumber("3") + select("#", "a", "b")
		outputs.kind = type(outputs)
		return outputs
	`, map[string]any{}, "{}")
	require.NoError(t, err)
	assert.Equal(t, false, result["ok"])
	assert.Equal(t, float64(5), result["count"])
	assert.Equal(t, "table", result["kind"])
}
