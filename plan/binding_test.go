package plan

import (
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refundGraph is shaped like the Stripe project: a PaymentIntent is created and
// refunded, and getCharge's charge defaults from createRefund.
func refundGraph() *graph.Graph {
	return &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"createPaymentIntent": {
			Name:    "createPaymentIntent",
			Outputs: []graph.Output{{Name: "intent", Type: "string"}},
		},
		"createRefund": {
			Name:    "createRefund",
			Inputs:  []graph.Input{{Name: "intent", Type: "string", Default: &graph.InputDefault{From: "createPaymentIntent.intent"}}},
			Outputs: []graph.Output{{Name: "charge", Type: "string"}},
		},
		"getCharge": {
			Name: "getCharge",
			Inputs: []graph.Input{
				{Name: "charge", Type: "string", Default: &graph.InputDefault{From: "createRefund.charge"}},
				{Name: "expand", Type: "string", Optional: true},
			},
			Outputs: []graph.Output{{Name: "refunded", Type: "boolean"}},
		},
	}}
}

// TestInstantiate_VerificationReadsTheLastStepNotExpectedToFail is the Stripe
// run's failure: a verification of getCharge read its charge from the first
// createRefund step, a refund expected to fail, which returned no charge.
func TestInstantiate_VerificationReadsTheLastStepNotExpectedToFail(t *testing.T) {
	failing := &ExpectFailure{Status: []int{400}}
	p := &Plan{Execution: Execution{
		Steps: []Step{
			{ID: "authorize", Node: "createPaymentIntent"},
			{ID: "refundBeforeConfirm", Node: "createRefund", ExpectFailure: failing},
			{ID: "refund", Node: "createRefund"},
			{ID: "refundTwice", Node: "createRefund", ExpectFailure: failing},
		},
		Verification: []VerificationStep{{Node: "getCharge"}},
	}}

	inst, err := InstantiateAndValidate(p, refundGraph())
	require.NoError(t, err)

	steps := VerificationSteps(inst, refundGraph(), nil)
	require.Len(t, steps, 1)
	assert.Equal(t, "refund.charge", steps[0].Values["charge"].From)
	assert.Nil(t, p.Execution.Verification[0].BoundDefaults, "the caller's plan is unchanged")

	for _, step := range inst.Execution.Steps[1:] {
		assert.Equal(t, "authorize.intent", step.Values["intent"].From)
		assert.Contains(t, step.DependsOn, "authorize")
	}
}

// TestInstantiate_MainStepReadsTheNearestEarlierStep checks that a main step's
// default reads the nearest earlier step on its node that isn't expected to
// fail, and depends on that step.
func TestInstantiate_MainStepReadsTheNearestEarlierStep(t *testing.T) {
	failing := &ExpectFailure{Status: []int{400}}
	p := &Plan{Execution: Execution{Steps: []Step{
		{ID: "authorize", Node: "createPaymentIntent"},
		{ID: "refund", Node: "createRefund"},
		{ID: "chargeAfterRefund", Node: "getCharge"},
		{ID: "refundTwice", Node: "createRefund", ExpectFailure: failing},
		{ID: "chargeAfterRefundTwice", Node: "getCharge"},
	}}}

	inst, err := InstantiateAndValidate(p, refundGraph())
	require.NoError(t, err)

	byID := map[string]Step{}
	for _, step := range inst.Execution.Steps {
		byID[step.StepID()] = step
	}
	assert.Equal(t, "refund.charge", byID["chargeAfterRefund"].Values["charge"].From)
	assert.Equal(t, "refund.charge", byID["chargeAfterRefundTwice"].Values["charge"].From, "the refund expected to fail is passed over")
	assert.Equal(t, []string{"refund"}, byID["chargeAfterRefundTwice"].DependsOn)
}

func TestInstantiate_VerificationValues(t *testing.T) {
	base := func() *Plan {
		return &Plan{Execution: Execution{Steps: []Step{
			{ID: "authorize", Node: "createPaymentIntent"},
			{ID: "firstRefund", Node: "createRefund"},
			{ID: "secondRefund", Node: "createRefund"},
		}}}
	}

	t.Run("a value takes the place of the default", func(t *testing.T) {
		p := base()
		p.Execution.Verification = []VerificationStep{{Node: "getCharge", Values: map[string]StepValue{"charge": {From: "firstRefund.charge"}}}}
		inst, err := InstantiateAndValidate(p, refundGraph())
		require.NoError(t, err)
		assert.Equal(t, "firstRefund.charge", VerificationSteps(inst, refundGraph(), nil)[0].Values["charge"].From)
	})

	t.Run("values are validated", func(t *testing.T) {
		p := base()
		p.Execution.Verification = []VerificationStep{{Node: "getCharge", Values: map[string]StepValue{
			"chrage": {From: "firstRefund.charge"},
			"charge": {From: "noSuchStep.charge"},
			"expand": {FromSelection: "offer.id"},
		}}}
		_, err := InstantiateAndValidate(p, refundGraph())
		require.Error(t, err)
		msg := err.Error()
		assert.Contains(t, msg, `verification step 0 (getCharge): value "chrage" does not match any input on node "getCharge"`)
		assert.Contains(t, msg, `verification step 0 (getCharge): 'from' reference "noSuchStep.charge" for "charge": "noSuchStep" is not a step in this plan`)
		assert.Contains(t, msg, `verification step 0 (getCharge): value "expand" uses fromSelection`)
	})
}

// TestInstantiate_VerificationSkipsMutationClones checks that a verification
// default binds before mutations expand, so an isolated mutation's clone of a
// prerequisite, which isn't expected to fail, is never chosen.
func TestInstantiate_VerificationSkipsMutationClones(t *testing.T) {
	p := &Plan{Execution: Execution{
		Steps: []Step{
			{ID: "authorize", Node: "createPaymentIntent"},
			{ID: "refund", Node: "createRefund"},
			{
				ID: "readCharge", Node: "getCharge", MutationScope: "isolated",
				Mutations: []Mutation{{Name: "bad", Set: map[string]any{"expand": "nonsense"}, ExpectStatus: []int{400}}},
			},
		},
		Verification: []VerificationStep{{Node: "getCharge"}},
	}}

	inst, err := InstantiateAndValidate(p, refundGraph())
	require.NoError(t, err)

	var ids []string
	for _, step := range inst.Execution.Steps {
		ids = append(ids, step.StepID())
	}
	require.Contains(t, ids, "refund__bad", "the isolated mutation clones its prerequisites")
	assert.Equal(t, "refund.charge", VerificationSteps(inst, refundGraph(), nil)[0].Values["charge"].From)
}

func TestInstantiate_ReferencesImplyDependsOn(t *testing.T) {
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"search": {
			Name:   "search",
			Inputs: []graph.Input{{Name: "q", Type: "string"}},
			Outputs: []graph.Output{
				{Name: "offers", Type: "object[]", ElementFields: []graph.Field{{Name: "id", Type: "string"}}},
				{Name: "token", Type: "string"},
			},
		},
		"price": {
			Name:   "price",
			Inputs: []graph.Input{{Name: "offerId", Type: "string"}, {Name: "q", Type: "string"}, {Name: "token", Type: "string"}},
		},
	}}

	t.Run("from, fromInput, and a selection each add their step", func(t *testing.T) {
		p := &Plan{Execution: Execution{Steps: []Step{
			{ID: "search", Node: "search", Values: map[string]StepValue{"q": {Default: "tent"}}},
			{ID: "check", Node: "search", Values: map[string]StepValue{"q": {Default: "stove"}}},
			{
				ID: "price", Node: "price",
				Selections: map[string]StepSelection{"offer": {From: "search.offers", Strategy: "first"}},
				Values: map[string]StepValue{
					"offerId": {FromSelection: "offer.id"},
					"q":       {FromInput: "check.q"},
					"token":   {From: "check.token"},
				},
			},
		}}}
		inst, err := InstantiateAndValidate(p, g)
		require.NoError(t, err)
		assert.Equal(t, []string{"check", "search"}, inst.Execution.Steps[2].DependsOn)
		assert.Empty(t, p.Execution.Steps[2].DependsOn, "the caller's plan is unchanged")
	})

	t.Run("a reference that closes a cycle says so", func(t *testing.T) {
		p := &Plan{Execution: Execution{Steps: []Step{
			{ID: "a", Node: "search", DependsOn: []string{"b"}, Values: map[string]StepValue{"q": {Default: "x"}}},
			{ID: "b", Node: "search", Values: map[string]StepValue{"q": {FromInput: "a.q"}}},
		}}}
		_, err := InstantiateAndValidate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `(value "q" reads "a", which implies the dependency)`)
	})
}

// TestInstantiate_CloneCollisionAfterImpliedDependency checks that an isolated
// mutation's clone IDs are checked against the prerequisites a reference
// implies, not only those dependsOn lists.
func TestInstantiate_CloneCollisionAfterImpliedDependency(t *testing.T) {
	p := &Plan{Execution: Execution{Steps: []Step{
		{ID: "authorize", Node: "createPaymentIntent"},
		{ID: "authorize__bad", Node: "createPaymentIntent"},
		{
			ID: "refund", Node: "createRefund",
			Values:        map[string]StepValue{"intent": {From: "authorize.intent"}},
			MutationScope: "isolated",
			Mutations:     []Mutation{{Name: "bad", Set: map[string]any{"intent": "pi_missing"}, ExpectStatus: []int{404}}},
		},
	}}}
	_, err := InstantiateAndValidate(p, refundGraph())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `cloned step id "authorize__bad" collides with an existing step`)
}

func TestInstantiateWithLayers_RecordsWhereValuesCameFrom(t *testing.T) {
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"createPaymentIntent": {Name: "createPaymentIntent", Inputs: []graph.Input{
			{Name: "currency", Type: "string", Default: &graph.InputDefault{Value: "usd"}},
			{Name: "amount", Type: "integer", Default: &graph.InputDefault{Value: 2000}},
			{Name: "description", Type: "string"},
		}},
	}}
	layered, err := graph.ApplyLayers(g, []string{"currency-jpy"}, map[string]*graph.Layer{
		"currency-jpy": {Name: "currency-jpy", Inputs: map[string]*graph.InputDefault{"createPaymentIntent.currency": {Value: "jpy"}}},
	})
	require.NoError(t, err)

	p := &Plan{Execution: Execution{Steps: []Step{
		{Node: "createPaymentIntent", Values: map[string]StepValue{"description": {Default: "test"}}},
	}}}
	values := InstantiateWithLayers(p, g, layered).Execution.Steps[0].Values
	assert.Equal(t, StepValue{Default: "jpy", Origin: "layer", Layer: "currency-jpy"}, values["currency"])
	assert.Equal(t, StepValue{Default: 2000, Origin: "graph"}, values["amount"])
	assert.Equal(t, StepValue{Default: "test"}, values["description"])
}
