package plan

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
)

// TestValidate_EmptyValueOverAnyDefault checks that {} passes validation over
// any graph default: a plain one is used, and otherwise the input is left out.
func TestValidate_EmptyValueOverAnyDefault(t *testing.T) {
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"checkoutCart": {Name: "checkoutCart", Inputs: []graph.Input{
			{Name: "shippingTier", Type: "string", Default: &graph.InputDefault{Pool: []any{"standard", "express"}}},
			{Name: "postalCode", Type: "string", Default: graph.LiteralDefault("{{env.postalCode}}")},
			{Name: "notes", Type: "string", Optional: true, Default: &graph.InputDefault{Pool: []any{"a", "b"}}},
		}},
	}}
	p := &Plan{Execution: Execution{Steps: []Step{{
		Node:   "checkoutCart",
		Values: map[string]StepValue{"shippingTier": {}, "postalCode": {}, "notes": {}},
	}}}}

	require.NoError(t, Validate(p, g))
}
