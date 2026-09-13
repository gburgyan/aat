package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gburgyan/aat/graph"
)

func TestRequiredFromOptionalValues(t *testing.T) {
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"getCart": {Name: "getCart", Outputs: []graph.Output{
			{Name: "cartId", Type: "string"},
			{Name: "couponCode", Type: "string", Optional: true},
			{Name: "promotions", Type: "promotion[]", Optional: true},
		}},
		"checkoutCart": {Name: "checkoutCart", Inputs: []graph.Input{
			{Name: "cartId", Type: "string"},
			{Name: "couponCode", Type: "string"},
			{Name: "promo", Type: "string", Optional: true},
			{Name: "promoCode", Type: "string"},
			{Name: "promoName", Type: "string", Optional: true},
		}},
	}}
	p := &Plan{Execution: Execution{Steps: []Step{
		{ID: "cart", Node: "getCart"},
		{
			Node: "checkoutCart", DependsOn: []string{"cart"},
			Selections: map[string]StepSelection{"best": {From: "cart.promotions"}},
			Values: map[string]StepValue{
				"cartId":     {From: "cart.cartId"},
				"couponCode": {From: "cart.couponCode"},
				"promo":      {From: "cart.couponCode"},
				"promoCode":  {FromSelection: "best.code"},
				"promoName":  {FromSelection: "best.name"},
			},
		},
	}}}

	assert.Equal(t, []string{
		`step 1 (checkoutCart): required input "couponCode" takes cart.couponCode, an optional output; when it's missing the step fails (mark the input optional to leave it out, or give it a value)`,
		`step 1 (checkoutCart): required input "promoCode" reads selection "best" from cart.promotions, an optional output; when it's missing the step fails (mark the input optional to leave it out, or give it a value)`,
	}, RequiredFromOptionalValues(p, g))
}
