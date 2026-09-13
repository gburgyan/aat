package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequiredFromOptionalDefaults(t *testing.T) {
	g := &Graph{Nodes: map[string]*Node{
		"getCart": {Name: "getCart", Outputs: []Output{
			{Name: "cartId", Type: "string"},
			{Name: "couponCode", Type: "string", Optional: true},
		}},
		"checkoutCart": {Name: "checkoutCart", Inputs: []Input{
			{Name: "cartId", Type: "string", Default: &InputDefault{From: "getCart.cartId"}},
			{Name: "couponCode", Type: "string", Default: &InputDefault{From: "getCart.couponCode"}},
			{Name: "promo", Type: "string", Optional: true, Default: &InputDefault{From: "getCart.couponCode"}},
		}},
	}}

	assert.Equal(t, []string{
		`node "checkoutCart": required input "couponCode" defaults from getCart.couponCode, an optional output; when it's missing the step fails (mark the input optional to leave it out, or give it a value)`,
	}, RequiredFromOptionalDefaults(g))
	assert.True(t, OptionalOutput(g, "getCart.couponCode.code"), "a field of the output")
	assert.False(t, OptionalOutput(g, "getCart.cartId"))
	assert.False(t, OptionalOutput(g, "nope.cartId"))
}
