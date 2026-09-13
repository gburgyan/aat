package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

func TestResolveInputs_EmptyStepValue_EvaluatesLiteralDefault(t *testing.T) {
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"checkoutCart": {Name: "checkoutCart", Inputs: []graph.Input{
			{Name: "postalCode", Type: "string", Default: graph.LiteralDefault("{{env.postalCode}}")},
		}},
	}}
	step := plan.Step{Node: "checkoutCart", Values: map[string]plan.StepValue{"postalCode": {}}}
	rctx := &ResolveContext{Now: fixedNow(), EnvLookup: func(key string) string {
		if key == "postalCode" {
			return "78701"
		}
		return ""
	}}

	inputs, _, resolutions, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["checkoutCart"], g, NewRunState(), rctx)

	require.NoError(t, err)
	assert.Equal(t, "78701", inputs["postalCode"], "not the text {{env.postalCode}}")
	require.Len(t, resolutions, 1)
	assert.Equal(t, "graph_default", resolutions[0].Source)
	assert.Equal(t, "{{env.postalCode}}", resolutions[0].Expression)
}

// TestResolveInputs_EmptyStepValue_NonPlainDefaultLeavesInputOut pins {} over
// a pool or from default: the input is left out, for a template that sends it
// in a conditional block, such as a payment sent only for orders paid now.
func TestResolveInputs_EmptyStepValue_NonPlainDefaultLeavesInputOut(t *testing.T) {
	for name, def := range map[string]*graph.InputDefault{
		"pool": {Pool: []any{"standard", "express"}},
		"from": {From: "listShippingRates.tier"},
	} {
		t.Run(name, func(t *testing.T) {
			g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
				"checkoutCart": {Name: "checkoutCart", Inputs: []graph.Input{{Name: "shippingTier", Type: "string", Default: def}}},
			}}
			step := plan.Step{Node: "checkoutCart", Values: map[string]plan.StepValue{"shippingTier": {}}}

			inputs, _, resolutions, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["checkoutCart"], g, NewRunState(), nil)
			require.NoError(t, err)
			assert.Nil(t, inputs["shippingTier"])
			require.Len(t, resolutions, 1)
			assert.Equal(t, ValueResolution{InputName: "shippingTier", Source: "graph_default", PoolIndex: -1}, resolutions[0])
		})
	}
}

// TestResolveInputs_EmptyStepValue_FollowsLayers pins that {} on a required
// input falls back to its default after layers, which decide both the value and
// whether it is plain.
func TestResolveInputs_EmptyStepValue_FollowsLayers(t *testing.T) {
	tests := []struct {
		name  string
		base  *graph.InputDefault
		layer *graph.InputDefault
		want  any
	}{
		{name: "a plain layer over a plain default", base: graph.LiteralDefault("standard"), layer: graph.LiteralDefault("express"), want: "express"},
		{name: "a plain layer over a pool", base: &graph.InputDefault{Pool: []any{"standard", "economy"}}, layer: graph.LiteralDefault("express"), want: "express"},
		{name: "a pool layer over a plain default", base: graph.LiteralDefault("standard"), layer: &graph.InputDefault{Pool: []any{"standard", "express"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
				"checkoutCart": {Name: "checkoutCart", Inputs: []graph.Input{{Name: "shippingMethod", Type: "string", Default: tt.base}}},
			}}
			layered, err := graph.ApplyLayers(g, []string{"shipping"}, map[string]*graph.Layer{
				"shipping": {Name: "shipping", Inputs: map[string]*graph.InputDefault{"checkoutCart.shippingMethod": tt.layer}},
			})
			require.NoError(t, err)
			step := plan.Step{Node: "checkoutCart", Values: map[string]plan.StepValue{"shippingMethod": {}}}
			rctx := &ResolveContext{Now: fixedNow(), LayeredDefaults: layered}

			inputs, _, _, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["checkoutCart"], g, NewRunState(), rctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want, inputs["shippingMethod"])
		})
	}
}
