package plan

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
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
	require.NoError(t, ExpandFuzzCases(p, "add", cases, FuzzExpandOptions{Scope: FuzzScopeShared}))

	assert.Equal(t, []string{"cart", "add", "add__fuzz_quantity_above_max", "add__fuzz_quantity_zero", "checkout"}, stepIDs(p))
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
	require.NoError(t, ExpandFuzzCases(p, "add", []FuzzCase{{ID: "quantity.zero", Mode: FuzzPositive, Input: "quantity", Value: 0}}, FuzzExpandOptions{Scope: FuzzScopeIsolated}))

	assert.Equal(t, []string{"cart", "add", "cart__add__fuzz_quantity_zero", "add__fuzz_quantity_zero", "checkout"}, stepIDs(p))
	child := p.Execution.Steps[3]
	assert.Equal(t, []string{"cart__add__fuzz_quantity_zero"}, child.DependsOn)
	assert.Equal(t, "cart__add__fuzz_quantity_zero.cartId", child.Values["cartId"].From, "the case reads its own cart")
}

func TestExpandFuzzCases_UnknownTarget(t *testing.T) {
	assert.ErrorContains(t, ExpandFuzzCases(fuzzPlan(), "pay", nil, FuzzExpandOptions{Scope: FuzzScopeShared}), `fuzz target "pay"`)
}

func TestExpandFuzzCases_TwoTargetsSameCase(t *testing.T) {
	p := fuzzPlan()
	c := []FuzzCase{{ID: "body.extra-property", Mode: FuzzEdge, Patch: []RequestPatch{{Where: "body", Path: "x", Op: "set", Value: "x"}}}}
	require.NoError(t, ExpandFuzzCases(p, "add", c, FuzzExpandOptions{Scope: FuzzScopeIsolated}))
	require.NoError(t, ExpandFuzzCases(p, "checkout", c, FuzzExpandOptions{Scope: FuzzScopeIsolated}))
	seen := map[string]bool{}
	for _, id := range stepIDs(p) {
		assert.False(t, seen[id], "duplicate step %s", id)
		seen[id] = true
	}
	assert.Contains(t, stepIDs(p), "cart__checkout__fuzz_body_extra_property")
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
	require.NoError(t, ExpandFuzzCases(p, "order", []FuzzCase{{ID: "tier.empty", Input: "tier", Value: ""}}, FuzzExpandOptions{Scope: FuzzScopeIsolated}))

	assert.Equal(t, []string{
		"products", "cart", "item", "unrelated", "order",
		"products__order__fuzz_tier_empty", "cart__order__fuzz_tier_empty", "item__order__fuzz_tier_empty",
		"order__fuzz_tier_empty",
	}, stepIDs(p), "the item's prerequisites come too, in plan order; the unrelated step does not")
	item := p.Execution.Steps[7]
	assert.Equal(t, []string{"cart__order__fuzz_tier_empty", "products__order__fuzz_tier_empty"}, item.DependsOn)
	assert.Equal(t, "order__fuzz_tier_empty", item.FuzzSetup)
	assert.Empty(t, p.Execution.Steps[8].FuzzSetup, "the case's own step is not setup")
}

func TestFuzzSettings_ParseAndValidate(t *testing.T) {
	p, err := Parse([]byte(`
execution:
  steps:
    - id: add
      node: addItem
      fuzz:
        mode: [negative]
        skip: [note]
        accept: [409, 4xx]
        fail: [server-error, accepted-invalid]
        pinned:
          - id: quantity.below-min
            mode: negative
            input: quantity
            value: 0
            found: server-error
          - id: body.channel.remove
            mode: edge
            patch: [{where: body, path: channel, op: remove}]
`))
	require.NoError(t, err)
	s := p.Execution.Steps[0].FuzzSettings
	require.NotNil(t, s)
	assert.Equal(t, []string{"negative"}, s.Mode)
	assert.Equal(t, ExpectedStatuses{{Code: 409}, {Class: 4}}, s.Accept)
	require.Len(t, s.Pinned, 2)
	assert.Equal(t, FuzzCase{ID: "quantity.below-min", Mode: "negative", Input: "quantity", Strategy: "below-min", Value: 0}, s.Pinned[0].Case())
	assert.Equal(t, []RequestPatch{{Where: "body", Path: "channel", Op: "remove"}}, s.Pinned[1].Patch)
	assert.True(t, s.Generates(), "mode asks for generated cases too")
	assert.False(t, (&FuzzSettings{Pinned: s.Pinned}).Generates(), "pinned cases alone")

	_, err = Parse([]byte("execution:\n  steps:\n    - node: addItem\n      fuzz:\n        modes: [negative]\n"))
	assert.ErrorContains(t, err, `unknown key "modes"`)

	node := &graph.Node{Name: "addItem", Inputs: []graph.Input{{Name: "quantity", Type: "integer"}}}
	errs := validateFuzzSettings("step 0 (add)", &FuzzSettings{
		Mode:   []string{"hostile"},
		Skip:   []string{"qty"},
		Scope:  "global",
		Fail:   []string{"crash"},
		Accept: ExpectedStatuses{{Code: 500}},
		Pinned: []PinnedFuzzCase{
			{ID: "a", Mode: "negative"},
			{ID: "a", Mode: "negative", Input: "quantity", Value: 1, Patch: []RequestPatch{{Where: "cookie", Path: "x", Op: "drop"}}},
		},
	}, node)
	joined := strings.Join(errs, "\n")
	for _, want := range []string{
		`unknown mode "hostile"`, `no input "qty"`, `unknown scope "global"`, `unknown finding "crash"`,
		"accept 500", "name the input it sets", `id "a" is used twice`, "set a value or a patch, not both",
		`where "cookie"`, `op "drop"`,
	} {
		assert.Contains(t, joined, want)
	}
}

func TestExpandFuzzCases_ReuseOrderAndReadOnly(t *testing.T) {
	p := &Plan{Execution: Execution{Steps: []Step{
		{ID: "products", Node: "listProducts"},
		{ID: "cart", Node: "createCart"},
		{ID: "view", Node: "getCart", DependsOn: []string{"cart"}},
		{ID: "add", Node: "addItem", DependsOn: []string{"cart", "products", "view"}},
	}}}
	readOnly := func(s Step) bool { return s.Node == "listProducts" || s.Node == "getCart" }
	cases := []FuzzCase{
		{ID: "quantity.zero", Mode: FuzzNegative, Input: "quantity", Value: 0},
		{ID: "quantity.large", Mode: FuzzEdge, Input: "quantity", Value: 99},
	}
	require.NoError(t, ExpandFuzzCases(p, "add", cases, FuzzExpandOptions{Scope: FuzzScopeReuse, ReadOnly: readOnly}))

	assert.Equal(t, []string{
		"products", "cart", "view", "add",
		"cart__add__fuzz_quantity_zero", "view__add__fuzz_quantity_zero", "add__fuzz_quantity_zero",
		"cart__add__fuzz_quantity_large", "view__add__fuzz_quantity_large", "add__fuzz_quantity_large",
	}, stepIDs(p), "listProducts is read-only and depends on nothing copied, so it is used as it is; getCart reads the copied cart, so it is copied")

	byID := map[string]Step{}
	for _, s := range p.Execution.Steps {
		byID[s.StepID()] = s
	}
	assert.Equal(t, "cart", byID["cart__add__fuzz_quantity_zero"].FuzzSetupOf)
	assert.Equal(t, "add__fuzz_quantity_zero", byID["view__add__fuzz_quantity_zero"].FuzzSetup)
	assert.Contains(t, byID["add__fuzz_quantity_zero"].DependsOn, "products", "the uncopied step keeps its ID")

	// The second case waits for the first: its first copy, and its own step.
	assert.Contains(t, byID["cart__add__fuzz_quantity_large"].DependsOn, "add__fuzz_quantity_zero")
	assert.NotContains(t, byID["view__add__fuzz_quantity_large"].DependsOn, "add__fuzz_quantity_zero", "it waits through the cart copy")
	assert.Contains(t, byID["add__fuzz_quantity_large"].DependsOn, "add__fuzz_quantity_zero")
	assert.NotContains(t, byID["cart__add__fuzz_quantity_zero"].DependsOn, "add", "the first case doesn't wait for the target")
}

func TestFuzzStepID_IsAnExpressionIdentifier(t *testing.T) {
	assert.Equal(t, "book__fuzz_email_whitespace", FuzzStepID("book", "email.whitespace"))
	assert.Equal(t, "pay_declined__fuzz_body_shipping_country_remove", FuzzStepID("pay--declined", "body.shipping.country.remove"))
	// A copy is named after its case's step; an expression can read it.
	_, err := EvalExpr("{{search__book__fuzz_email_whitespace.origin}}", ExprContext{Outputs: func(step, output string) (any, error) { return "DEN", nil }})
	assert.NoError(t, err)
}
