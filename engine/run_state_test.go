package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRunState_InitializesBothMaps(t *testing.T) {
	state := NewRunState()
	require.NotNil(t, state.outputs)
	require.NotNil(t, state.inputs)
}

func TestStoreInputs_And_GetInput(t *testing.T) {
	state := NewRunState()
	state.StoreInputs("searchFlights", map[string]any{
		"origin":      "DEN",
		"destination": "JFK",
	})

	val, err := state.GetInput("searchFlights", "origin")
	require.NoError(t, err)
	assert.Equal(t, "DEN", val)

	val, err = state.GetInput("searchFlights", "destination")
	require.NoError(t, err)
	assert.Equal(t, "JFK", val)
}

func TestGetInput_MissingStep(t *testing.T) {
	state := NewRunState()

	_, err := state.GetInput("nonexistent", "origin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no inputs for step")
}

func TestGetInput_MissingInput(t *testing.T) {
	state := NewRunState()
	state.StoreInputs("searchFlights", map[string]any{
		"origin": "DEN",
	})

	_, err := state.GetInput("searchFlights", "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "input \"missing\" not found")
}

func TestStoreInputs_Overwrites(t *testing.T) {
	state := NewRunState()
	state.StoreInputs("step1", map[string]any{"origin": "DEN"})
	state.StoreInputs("step1", map[string]any{"origin": "LAX"})

	val, err := state.GetInput("step1", "origin")
	require.NoError(t, err)
	assert.Equal(t, "LAX", val)
}
