package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

func TestRunState_GetOutputMissing(t *testing.T) {
	state := NewRunState()
	state.StoreOutputs("getCart", map[string]any{"cartId": "cart-1"})

	_, err := state.GetOutput("getCart", "couponCode")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrOutputMissing))
	assert.Equal(t, `output "couponCode" not found for node "getCart"`, err.Error(), "the message is unchanged")

	_, err = state.GetOutput("applyCoupon", "discount")
	assert.True(t, errors.Is(err, ErrOutputMissing))
}

func optionalOutputGraph(couponOptional bool) *graph.Graph {
	return &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"checkoutCart": {Name: "checkoutCart", Inputs: []graph.Input{
			{Name: "cartId", Type: "string"},
			{Name: "couponCode", Type: "string", Optional: couponOptional},
			{Name: "giftSku", Type: "string", Optional: true},
		}},
	}}
}

func TestResolveInputs_OptionalFromMissingOutput(t *testing.T) {
	g := optionalOutputGraph(true)
	state := NewRunState()
	state.StoreOutputs("getCart", map[string]any{"cartId": "cart-1"})
	step := plan.Step{Node: "checkoutCart", Values: map[string]plan.StepValue{
		"cartId":     {From: "getCart.cartId"},
		"couponCode": {From: "getCart.couponCode"},
		"giftSku":    {From: "getCart.giftOptions", Select: &plan.SelectionConfig{Strategy: "first", Field: "sku"}},
	}}

	inputs, _, resolutions, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["checkoutCart"], g, state, nil)

	require.NoError(t, err)
	assert.Equal(t, map[string]any{"cartId": "cart-1"}, inputs, "optional inputs from missing outputs are left out")
	require.Len(t, resolutions, 3)
	assert.Equal(t, ValueResolution{InputName: "couponCode", Source: "optional_skip", FromStep: "getCart", FromOutput: "couponCode", PoolIndex: -1}, resolutions[1])
	assert.Equal(t, "optional_skip", resolutions[2].Source, "a selection from a missing array too")
}

func TestResolveInputs_RequiredFromMissingOutputFails(t *testing.T) {
	g := optionalOutputGraph(false)
	state := NewRunState()
	state.StoreOutputs("getCart", map[string]any{"cartId": "cart-1"})
	step := plan.Step{Node: "checkoutCart", Values: map[string]plan.StepValue{
		"cartId":     {From: "getCart.cartId"},
		"couponCode": {From: "getCart.couponCode"},
	}}

	_, _, err := ResolveInputs(step, g.Nodes["checkoutCart"], g, state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `output "couponCode" not found for node "getCart"`)
}

func TestResolveInputs_SelectionFromMissingOutput(t *testing.T) {
	state := NewRunState()
	state.StoreOutputs("getCart", map[string]any{"cartId": "cart-1"})
	step := plan.Step{
		Node:       "checkoutCart",
		Selections: map[string]plan.StepSelection{"promo": {From: "getCart.promotions"}},
		Values: map[string]plan.StepValue{
			"cartId":     {From: "getCart.cartId"},
			"couponCode": {FromSelection: "promo.code"},
		},
	}

	t.Run("optional readers are left out", func(t *testing.T) {
		g := optionalOutputGraph(true)
		inputs, decisions, resolutions, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["checkoutCart"], g, state, nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"cartId": "cart-1"}, inputs)
		assert.Empty(t, decisions)
		assert.Contains(t, resolutions, ValueResolution{InputName: "couponCode", Source: "optional_skip", FromStep: "getCart", FromOutput: "promotions", PoolIndex: -1})
	})

	t.Run("a required reader fails", func(t *testing.T) {
		g := optionalOutputGraph(false)
		_, _, err := ResolveInputs(step, g.Nodes["checkoutCart"], g, state)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `resolving selection "promo"`)
	})
}

func TestResolveInputs_CancelledKeepsWhatResolved(t *testing.T) {
	g := optionalOutputGraph(true)
	state := NewRunState()
	state.StoreOutputs("getCart", map[string]any{"cartId": "cart-1", "promotions": []any{map[string]any{"code": "SAVE10"}}})
	step := plan.Step{
		Node:       "checkoutCart",
		Selections: map[string]plan.StepSelection{"promo": {From: "getCart.promotions"}},
		Values:     map[string]plan.StepValue{"couponCode": {FromSelection: "promo.code"}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, decisions, _, err := ResolveInputsWithContext(ctx, step, g.Nodes["checkoutCart"], g, state, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, decisions, 1, "the named selection resolved before the cancellation was seen")
	assert.Equal(t, "promo", decisions[0].SelectionName)
}
