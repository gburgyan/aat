package intent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

func TestApplyInjectValues_DecodedForms(t *testing.T) {
	g := &graph.Graph{Nodes: map[string]*graph.Node{
		"createCart": {Name: "createCart", Outputs: []graph.Output{{Name: "cartId", Type: "string"}}},
		"addItem": {Name: "addItem", Inputs: []graph.Input{
			{Name: "cartId", Type: "string"},
			{Name: "skus", Type: "string[]"},
			{Name: "region", Type: "string"},
			{Name: "quantity", Type: "integer"},
			{Name: "coupon", Type: "string"},
			{Name: "note", Type: "string"},
			{Name: "channel", Type: "string"},
		}},
	}}
	p := &plan.Plan{Execution: plan.Execution{Steps: []plan.Step{
		{ID: "cart_1", Node: "createCart"},
		{Node: "addItem", Values: map[string]plan.StepValue{
			"quantity": {Pool: []any{1, 2}},            // a pool is a value the step sets
			"coupon":   {FromInput: "cart_1.coupon"},   // so is fromInput
			"note":     {},                             // an empty {} is unset
			"channel":  {Locked: true, Default: "web"}, // a locked value stays
		}},
	}}}

	applyInjectValues(p, map[string]graph.InjectValue{
		"cartId":   {InputDefault: graph.InputDefault{From: "createCart.cartId"}},
		"skus":     graph.InjectLiteral([]any{"SKU-1", "SKU-2"}),
		"region":   {InputDefault: graph.InputDefault{Pool: []any{"us", "eu"}}},
		"quantity": graph.InjectLiteral(5),
		"coupon":   graph.InjectLiteral("SAVE10"),
		"note":     graph.InjectLiteral("gift"),
		"channel":  graph.InjectLiteral("phone"),
	}, g)

	values := p.Execution.Steps[1].Values
	assert.Equal(t, "cart_1.cartId", values["cartId"].From, "the node name becomes the composed step's ID")
	assert.Equal(t, []any{"SKU-1", "SKU-2"}, values["skus"].Default, "a list is injected as the list")
	assert.Equal(t, []any{"us", "eu"}, values["region"].Pool)
	assert.Equal(t, []any{1, 2}, values["quantity"].Pool, "the step's pool is kept")
	assert.Nil(t, values["quantity"].Default)
	assert.Equal(t, "cart_1.coupon", values["coupon"].FromInput)
	assert.Equal(t, "gift", values["note"].Default, "{} is filled")
	assert.Equal(t, "web", values["channel"].Default)
	_, hasOnCart := p.Execution.Steps[0].Values["skus"]
	require.False(t, hasOnCart, "createCart has no skus input")
}
