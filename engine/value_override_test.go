package engine

import (
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveValueOverride_Empty(t *testing.T) {
	router := NewExecutorRouter(adapter.NewHTTPExecutor("http://x"), &adapter.EnvironmentConfig{})
	values, ef := router.ResolveValueOverride("anyNode")
	assert.Nil(t, values)
	assert.Nil(t, ef)
}

func TestAddValueOverride_EmptyEntrySkipped(t *testing.T) {
	router := NewExecutorRouter(adapter.NewHTTPExecutor("http://x"), &adapter.EnvironmentConfig{})
	router.AddValueOverride("createBooking", nil, nil)
	values, ef := router.ResolveValueOverride("createBooking")
	assert.Nil(t, values)
	assert.Nil(t, ef)
}

func TestResolveValueOverride_ExactMatchValues(t *testing.T) {
	router := NewExecutorRouter(adapter.NewHTTPExecutor("http://x"), &adapter.EnvironmentConfig{})
	router.AddValueOverride("createBooking", map[string]any{
		"lastName": "",
		"age":      -1,
	}, nil)

	values, ef := router.ResolveValueOverride("createBooking")
	require.NotNil(t, values)
	assert.Equal(t, "", values["lastName"])
	assert.Equal(t, -1, values["age"])
	assert.Nil(t, ef)

	// Non-matching node gets nothing
	values, ef = router.ResolveValueOverride("searchFlights")
	assert.Nil(t, values)
	assert.Nil(t, ef)
}

func TestResolveValueOverride_ExpectFailure(t *testing.T) {
	router := NewExecutorRouter(adapter.NewHTTPExecutor("http://x"), &adapter.EnvironmentConfig{})
	ef := &plan.ExpectFailure{Status: []int{400, 422}, Description: "invalid input"}
	router.AddValueOverride("createBooking", nil, ef)

	values, gotEF := router.ResolveValueOverride("createBooking")
	assert.Nil(t, values)
	assert.Equal(t, ef, gotEF)
}

func TestResolveValueOverride_ExactWinsOverGlob(t *testing.T) {
	router := NewExecutorRouter(adapter.NewHTTPExecutor("http://x"), &adapter.EnvironmentConfig{})
	router.AddValueOverride("create*", map[string]any{"a": 1, "b": 2}, nil)
	router.AddValueOverride("createBooking", map[string]any{"b": 99}, nil)

	values, _ := router.ResolveValueOverride("createBooking")
	require.NotNil(t, values)
	assert.Equal(t, 1, values["a"], "glob contribution retained for non-conflicting keys")
	assert.Equal(t, 99, values["b"], "exact match wins on conflict")
}

func TestResolveValueOverride_ExactBeforeGlob_ExpectFailure(t *testing.T) {
	// First matching expectFailure wins (exact match applied first).
	router := NewExecutorRouter(adapter.NewHTTPExecutor("http://x"), &adapter.EnvironmentConfig{})
	globEF := &plan.ExpectFailure{Status: []int{500}}
	exactEF := &plan.ExpectFailure{Status: []int{400}}
	router.AddValueOverride("create*", nil, globEF)
	router.AddValueOverride("createBooking", nil, exactEF)

	_, ef := router.ResolveValueOverride("createBooking")
	require.NotNil(t, ef)
	assert.Equal(t, []int{400}, ef.Status, "exact match applied before glob, first non-nil wins")
}

func TestHasOverrides_IncludesValueOverrides(t *testing.T) {
	router := NewExecutorRouter(adapter.NewHTTPExecutor("http://x"), &adapter.EnvironmentConfig{})
	assert.False(t, router.HasOverrides())
	router.AddValueOverride("createBooking", map[string]any{"x": 1}, nil)
	assert.True(t, router.HasOverrides())
}
