package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fuzzPlan() *Plan {
	return &Plan{Execution: Execution{Steps: []Step{
		{ID: "cart", Node: "createCart"},
		{ID: "add", Node: "addItem", DependsOn: []string{"cart"}, Values: map[string]StepValue{
			"cartId":   {From: "cart.cartId"},
			"quantity": {Default: 2},
		}, Retry: &RetryConfig{Max: 2}, Assertions: &Assertions{}},
		{ID: "checkout", Node: "checkout", DependsOn: []string{"add"}},
	}}}
}

func stepIDs(p *Plan) []string {
	var ids []string
	for _, s := range p.Execution.Steps {
		ids = append(ids, s.StepID())
	}
	return ids
}

func TestExpandFuzzCases_Shared(t *testing.T) {
	p := fuzzPlan()
	cases := []FuzzCase{
		{ID: "quantity.above-max", Mode: FuzzNegative, Input: "quantity", Value: 100},
		{ID: "quantity.zero", Mode: FuzzPositive, Input: "quantity", Value: 0},
	}
	require.NoError(t, ExpandFuzzCases(p, "add", cases, false))

	assert.Equal(t, []string{"cart", "add", "add--fuzz-quantity-above-max", "add--fuzz-quantity-zero", "checkout"}, stepIDs(p))
	child := p.Execution.Steps[2]
	assert.Equal(t, StepValue{Default: 100, Raw: true}, child.Values["quantity"])
	assert.Equal(t, "cart.cartId", child.Values["cartId"].From, "other values stay")
	assert.Equal(t, []string{"cart"}, child.DependsOn)
	assert.Nil(t, child.Retry)
	assert.Nil(t, child.Assertions)
	require.NotNil(t, child.Fuzz)
	assert.Equal(t, "add", child.Fuzz.Target)
	assert.Equal(t, 2, p.Execution.Steps[1].Values["quantity"].Default, "the target is untouched")
}

func TestExpandFuzzCases_Isolated(t *testing.T) {
	p := fuzzPlan()
	require.NoError(t, ExpandFuzzCases(p, "add", []FuzzCase{{ID: "quantity.zero", Mode: FuzzPositive, Input: "quantity", Value: 0}}, true))

	assert.Equal(t, []string{"cart", "add", "cart__add--fuzz-quantity-zero", "add--fuzz-quantity-zero", "checkout"}, stepIDs(p))
	child := p.Execution.Steps[3]
	assert.Equal(t, []string{"cart__add--fuzz-quantity-zero"}, child.DependsOn)
	assert.Equal(t, "cart__add--fuzz-quantity-zero.cartId", child.Values["cartId"].From, "the case reads its own cart")
}

func TestExpandFuzzCases_UnknownTarget(t *testing.T) {
	assert.ErrorContains(t, ExpandFuzzCases(fuzzPlan(), "pay", nil, false), `fuzz target "pay"`)
}

func TestExpandFuzzCases_TwoTargetsSameCase(t *testing.T) {
	p := fuzzPlan()
	c := []FuzzCase{{ID: "body.extra-property", Mode: FuzzEdge, Patch: []RequestPatch{{Where: "body", Path: "x", Op: "set", Value: "x"}}}}
	require.NoError(t, ExpandFuzzCases(p, "add", c, true))
	require.NoError(t, ExpandFuzzCases(p, "checkout", c, true))
	seen := map[string]bool{}
	for _, id := range stepIDs(p) {
		assert.False(t, seen[id], "duplicate step %s", id)
		seen[id] = true
	}
	assert.Contains(t, stepIDs(p), "cart__checkout--fuzz-body-extra-property")
}

// TestExpandFuzzCases_SetupIncludesStepsThatBuildOnIt checks that a case's
// copy of the setup includes an earlier step the target reads nothing from
// but that changes what it works on: adding the item the checkout needs.
func TestExpandFuzzCases_SetupIncludesStepsThatBuildOnIt(t *testing.T) {
	p := &Plan{Execution: Execution{Steps: []Step{
		{ID: "products", Node: "listProducts"},
		{ID: "cart", Node: "createCart"},
		{ID: "item", Node: "addItem", DependsOn: []string{"cart", "products"}},
		{ID: "unrelated", Node: "listOrders"},
		{ID: "order", Node: "checkout", DependsOn: []string{"cart"}},
	}}}
	require.NoError(t, ExpandFuzzCases(p, "order", []FuzzCase{{ID: "tier.empty", Input: "tier", Value: ""}}, true))

	assert.Equal(t, []string{
		"products", "cart", "item", "unrelated", "order",
		"products__order--fuzz-tier-empty", "cart__order--fuzz-tier-empty", "item__order--fuzz-tier-empty",
		"order--fuzz-tier-empty",
	}, stepIDs(p), "the item's prerequisites come too, in plan order; the unrelated step does not")
	item := p.Execution.Steps[7]
	assert.Equal(t, []string{"cart__order--fuzz-tier-empty", "products__order--fuzz-tier-empty"}, item.DependsOn)
	assert.Equal(t, "order--fuzz-tier-empty", item.FuzzSetup)
	assert.Empty(t, p.Execution.Steps[8].FuzzSetup, "the case's own step is not setup")
}
