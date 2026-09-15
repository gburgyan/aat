package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
)

func outputRefsGraph() *graph.Graph {
	return &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"getCart":       {Name: "getCart", Outputs: []graph.Output{{Name: "subtotal", Type: "integer"}, {Name: "lines", Type: "cartLine[]"}}},
		"checkoutCart":  {Name: "checkoutCart", Outputs: []graph.Output{{Name: "total", Type: "integer"}, {Name: "subtotal", Type: "integer"}}},
		"paymentCharge": {Name: "paymentCharge", Outputs: []graph.Output{{Name: "status", Type: "string"}}},
		"paymentRefund": {Name: "paymentRefund",
			Inputs:  []graph.Input{{Name: "amount", Type: "integer", Optional: true}},
			Outputs: []graph.Output{{Name: "amount", Type: "integer"}}},
		"getOrder": {Name: "getOrder", Outputs: []graph.Output{{Name: "total", Type: "integer"}}},
	}}
}

// outputRefsPlan builds getCart, checkout, a declined charge that expects
// failure, and a refund, followed by a verification read of the order.
func outputRefsPlan(checkout, refund, verify []MechanicalAssertion) *Plan {
	return &Plan{Execution: Execution{
		Steps: []Step{
			{Node: "getCart"},
			{ID: "checkout", Node: "checkoutCart", Assertions: &Assertions{Mechanical: checkout}},
			{ID: "declinedCard", Node: "paymentCharge", ExpectFailure: &ExpectFailure{Status: []int{402}}},
			{ID: "refund", Node: "paymentRefund", Assertions: &Assertions{Mechanical: refund}},
		},
		Verification: []VerificationStep{{Node: "getOrder", Assertions: &Assertions{Mechanical: verify}}},
	}}
}

func pred(expr string) []MechanicalAssertion {
	return []MechanicalAssertion{{Type: "predicate", Expr: expr}}
}

func TestValidate_OutputRefs(t *testing.T) {
	repeatReadsUnknownStep := outputRefsPlan(nil, nil, nil)
	repeatReadsUnknownStep.Execution.Steps[3].Repeat = &RepeatConfig{Until: `amount >= "{{chekout.total}}"`}
	stepValue := outputRefsPlan(nil, nil, nil)
	stepValue.Execution.Steps[3].Values = map[string]StepValue{"amount": {Default: "{{checkout.total}}"}}
	selectFilter := func(filter string) *Plan {
		p := outputRefsPlan(nil, nil, nil)
		p.Execution.Steps[3].Values = map[string]StepValue{"amount": {From: "getCart.lines", Select: &SelectionConfig{Strategy: "first", Field: "price", Filter: filter}}}
		return p
	}
	namedSelectionFilter := outputRefsPlan(nil, nil, nil)
	namedSelectionFilter.Execution.Steps[3].Selections = map[string]StepSelection{"line": {From: "getCart.lines", Filter: `sku == "{{chekout.total}}"`}}

	tests := []struct {
		name string
		plan *Plan
		want string // "" means no reference error
	}{
		{
			name: "main and verification steps read earlier main steps",
			plan: outputRefsPlan(pred(`subtotal == "{{getCart.subtotal}}"`), pred(`amount == "{{checkout.total}}"`), pred(`total == "{{checkout.total}}"`)),
		},
		{
			name: "a fieldEquals value",
			plan: outputRefsPlan(nil, []MechanicalAssertion{{Type: "fieldEquals", Path: "amount", Value: "{{checkout.total}}"}}, nil),
		},
		{
			name: "an unknown step",
			plan: outputRefsPlan(nil, pred(`amount == "{{chekout.total}}"`), nil),
			want: `step 3 (refund): assertion 0 reads {{chekout.total}}: "chekout" is not a step in this plan`,
		},
		{
			name: "the step's own output",
			plan: outputRefsPlan(pred(`total == "{{checkout.total}}"`), nil, nil),
			want: `step 1 (checkout): assertion 0 reads {{checkout.total}}, the step's own output: name it directly, as total`,
		},
		{
			name: "an output the node doesn't declare",
			plan: outputRefsPlan(nil, pred(`amount == "{{checkout.totl}}"`), nil),
			want: `step 3 (refund): assertion 0 reads {{checkout.totl}}: output "totl" does not exist on node checkoutCart`,
		},
		{
			name: "a list output",
			plan: outputRefsPlan(nil, pred(`amount == "{{getCart.lines}}"`), nil),
			want: `step 3 (refund): assertion 0 reads {{getCart.lines}}, a cartLine[] output: an assertion compares a string, number, or boolean`,
		},
		{
			name: "a step that expects failure",
			plan: outputRefsPlan(nil, pred(`amount == "{{declinedCard.status}}"`), nil),
			want: `step 3 (refund): assertion 0 reads {{declinedCard.status}}: declinedCard expects failure, so it stores no outputs`,
		},
		{
			name: "a verification step reads main steps only",
			plan: outputRefsPlan(nil, nil, pred(`total == "{{verify_getOrder.total}}"`)),
			want: `verification step 0 (getOrder): assertion 0 reads {{verify_getOrder.total}}: a verification step reads main steps only`,
		},
		{
			name: "repeat.until",
			plan: repeatReadsUnknownStep,
			want: `step 3 (refund): repeat.until reads {{chekout.total}}: "chekout" is not a step in this plan`,
		},
		{
			name: "a step value",
			plan: stepValue,
			want: `step 3 (refund): invalid expression for "amount": {{checkout.total}} reads a step's output, which only assertions, repeat.until, and selection filters can; use from: checkout.total`,
		},
		{
			name: "a selection filter reads an earlier step",
			plan: selectFilter(`sku == "{{checkout.total}}"`),
		},
		{
			name: "a selection filter that reads an unknown step",
			plan: selectFilter(`sku == "{{chekout.total}}"`),
			want: `step 3 (refund): filter for "amount" reads {{chekout.total}}: "chekout" is not a step in this plan`,
		},
		{
			name: "a named selection's filter",
			plan: namedSelectionFilter,
			want: `step 3 (refund): filter for selection "line" reads {{chekout.total}}: "chekout" is not a step in this plan`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.plan, outputRefsGraph())
			if tt.want == "" {
				if err != nil {
					assert.NotContains(t, err.Error(), "reads {{")
				}
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestInstantiate_OutputRefsImplyDependsOn(t *testing.T) {
	g := outputRefsGraph()

	t.Run("an assertion adds the steps it reads", func(t *testing.T) {
		p := &Plan{Execution: Execution{Steps: []Step{
			{Node: "getCart"},
			{ID: "checkout", Node: "checkoutCart"},
			{ID: "refund", Node: "paymentRefund", Assertions: &Assertions{Mechanical: pred(`amount == "{{checkout.total}}" && amount > "{{getCart.subtotal}}"`)}},
		}}}
		inst, err := InstantiateAndValidate(p, g)
		require.NoError(t, err)
		assert.Equal(t, []string{"checkout", "getCart"}, inst.Execution.Steps[2].DependsOn)
	})

	t.Run("a selection filter adds the step it reads", func(t *testing.T) {
		p := &Plan{Execution: Execution{Steps: []Step{
			{Node: "getCart"},
			{ID: "checkout", Node: "checkoutCart"},
			{ID: "refund", Node: "paymentRefund", Values: map[string]StepValue{
				"amount": {From: "getCart.lines", Select: &SelectionConfig{Strategy: "first", Field: "price", Filter: `sku == "{{checkout.total}}"`}},
			}},
		}}}
		inst, err := InstantiateAndValidate(p, g)
		require.NoError(t, err)
		assert.Equal(t, []string{"getCart", "checkout"}, inst.Execution.Steps[2].DependsOn)
	})

	t.Run("one that closes a cycle says so", func(t *testing.T) {
		p := &Plan{Execution: Execution{Steps: []Step{
			{ID: "checkout", Node: "checkoutCart", DependsOn: []string{"refund"}},
			{ID: "refund", Node: "paymentRefund", Assertions: &Assertions{Mechanical: pred(`amount == "{{checkout.total}}"`)}},
		}}}
		_, err := InstantiateAndValidate(p, g)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `(assertion 0 reads "checkout", which implies the dependency)`)
	})
}
