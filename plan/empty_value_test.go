package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
)

func TestValidate_EmptyValueNeedsPlainDefault(t *testing.T) {
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

	err := Validate(p, g)
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, `step 0 (checkoutCart): required input "shippingTier" is {} but its graph default isn't a plain value`)
	assert.NotContains(t, msg, `"postalCode" is {}`, "a plain default is fine")
	assert.NotContains(t, msg, `"notes" is {}`, "an optional input marked {} is left out")
}
