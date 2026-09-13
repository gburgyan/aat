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

func TestResolveInputs_EmptyStepValue_NonPlainDefaultFails(t *testing.T) {
	for name, def := range map[string]*graph.InputDefault{
		"pool": {Pool: []any{"standard", "express"}},
		"from": {From: "listShippingRates.tier"},
	} {
		t.Run(name, func(t *testing.T) {
			g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
				"checkoutCart": {Name: "checkoutCart", Inputs: []graph.Input{{Name: "shippingTier", Type: "string", Default: def}}},
			}}
			step := plan.Step{Node: "checkoutCart", Values: map[string]plan.StepValue{"shippingTier": {}}}

			_, _, err := ResolveInputs(step, g.Nodes["checkoutCart"], g, NewRunState())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "is {} but its graph default isn't a plain value")
		})
	}
}
