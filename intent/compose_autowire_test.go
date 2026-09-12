package intent

import (
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// autowireTestGraph models a cart checkout whose optional gift message only
// the Gift Wrap addon produces.
func autowireTestGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"createCart": {
				Name: "createCart", Adapter: "createCart",
				Outputs: []graph.Output{{Name: "cartId", Type: "string"}},
			},
			"copyCart": {
				Name: "copyCart", Adapter: "copyCart",
				Outputs: []graph.Output{{Name: "cartId", Type: "string"}},
			},
			"addGiftWrap": {
				Name: "addGiftWrap", Adapter: "addGiftWrap",
				Inputs:  []graph.Input{{Name: "cartId", Type: "string"}},
				Outputs: []graph.Output{{Name: "giftMessage", Type: "string"}},
			},
			"checkout": {
				Name: "checkout", Adapter: "checkout",
				Inputs: []graph.Input{
					{Name: "cartId", Type: "string"},
					{Name: "giftMessage", Type: "string", Optional: true},
				},
				Outputs: []graph.Output{{Name: "orderId", Type: "string"}},
			},
			"confirmOrder": {
				Name: "confirmOrder", Adapter: "confirmOrder",
				Inputs: []graph.Input{{Name: "orderId", Type: "string"}},
			},
		},
		Workflows: []graph.Workflow{
			{Name: "Checkout", Template: "testdata/compose/autowire_base.yaml"},
			{Name: "Checkout Plain", Template: "testdata/compose/autowire_base_plain.yaml"},
			{Name: "Nearest", Template: "testdata/compose/autowire_nearest.yaml"},
			{Name: "Cycle", Template: "testdata/compose/autowire_cycle.yaml"},
			{
				Name: "Gift Wrap", Kind: "addon",
				Template: "testdata/compose/autowire_gift_wrap.yaml",
				After:    graph.AfterSpec{"createCart"},
			},
		},
	}
}

func composeAutowire(t *testing.T, base string, addons ...string) *plan.Plan {
	t.Helper()
	g := autowireTestGraph()
	wf, ok := findWorkflowByName(g, base)
	require.True(t, ok, "workflow %q", base)
	p, err := Compose(ComposeRequest{Base: wf, Addons: addons, Graph: g, GraphDir: "."})
	require.NoError(t, err)
	return p
}

func autowireStep(t *testing.T, p *plan.Plan, id string) plan.Step {
	t.Helper()
	for _, s := range p.Execution.Steps {
		if s.StepID() == id {
			return s
		}
	}
	require.Failf(t, "step not found", "no step %q in the composed plan", id)
	return plan.Step{}
}

func TestCompose_OptionalAutowire(t *testing.T) {
	t.Run("with the addon that produces it", func(t *testing.T) {
		checkout := autowireStep(t, composeAutowire(t, "Checkout", "Gift Wrap"), "checkout")
		assert.Equal(t, plan.StepValue{From: "inc0_addGiftWrap.giftMessage"}, checkout.Values["giftMessage"])
		assert.Contains(t, checkout.DependsOn, "inc0_addGiftWrap")
	})

	t.Run("without it", func(t *testing.T) {
		checkout := autowireStep(t, composeAutowire(t, "Checkout"), "checkout")
		v, listed := checkout.Values["giftMessage"]
		assert.True(t, listed, "the input stays in the plan")
		assert.True(t, v.IsEmpty(), "and is left unset: %+v", v)
	})
}

func TestCompose_BaseMarkerFromAddon(t *testing.T) {
	withAddon := autowireStep(t, composeAutowire(t, "Checkout Plain", "Gift Wrap"), "checkout")
	assert.Equal(t, "inc0_addGiftWrap.giftMessage", withAddon.Values["giftMessage"].From)

	without := autowireStep(t, composeAutowire(t, "Checkout Plain"), "checkout")
	assert.Equal(t, "AUTOWIRE", without.Values["giftMessage"].Default,
		"a plain marker that nothing feeds stays for an override, or for validation to reject")
}

func TestCompose_AutowireWithoutSlotsOrAddons(t *testing.T) {
	p := composeAutowire(t, "Checkout")
	assert.Equal(t, "createCart.cartId", autowireStep(t, p, "checkout").Values["cartId"].From)
	assert.Equal(t, "checkout.orderId", autowireStep(t, p, "confirmOrder").Values["orderId"].From)
}

func TestCompose_RemainingAutowireTakesNearestEarlierProducer(t *testing.T) {
	p := composeAutowire(t, "Nearest")
	assert.Equal(t, "copyCart.cartId", autowireStep(t, p, "checkout").Values["cartId"].From,
		"the nearer of two earlier producers, never the one after")
}

func TestCompose_RemainingAutowireSkipsDependentProducer(t *testing.T) {
	checkout := autowireStep(t, composeAutowire(t, "Cycle"), "checkout")
	assert.Equal(t, "AUTOWIRE", checkout.Values["cartId"].Default, "wiring to a step that depends on checkout would loop")
	assert.NotContains(t, checkout.DependsOn, "late")
}

func TestCompose_AddonMarkerKeepsItsWiring(t *testing.T) {
	p := composeAutowire(t, "Checkout", "Gift Wrap")
	assert.Equal(t, "createCart.cartId", autowireStep(t, p, "inc0_addGiftWrap").Values["cartId"].From)
}
