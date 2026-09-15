package plan

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// outputsLookup returns an ExprContext.Outputs that reads the given steps'
// outputs.
func outputsLookup(outputs map[string]map[string]any) func(stepID, output string) (any, error) {
	return func(stepID, output string) (any, error) {
		step, ok := outputs[stepID]
		if !ok {
			return nil, errors.New("no outputs for " + stepID)
		}
		v, ok := step[output]
		if !ok {
			return nil, errors.New("no output " + output)
		}
		return v, nil
	}
}

func TestEvalExpr_OutputRefs(t *testing.T) {
	lookup := outputsLookup(map[string]map[string]any{
		"checkout": {
			"total":    json.Number("13066"),
			"subtotal": json.Number("120.5"),
			"currency": "USD",
			"paid":     true,
			"lines":    []any{"a"},
			"coupon":   nil,
		},
	})
	withOutputs := ExprContext{Env: testEnv, Outputs: lookup}
	tests := []struct {
		name    string
		raw     string
		ctx     ExprContext
		want    any
		wantErr string
	}{
		{name: "a whole number keeps its type", raw: "{{checkout.total}}", ctx: withOutputs, want: int64(13066)},
		{name: "a decimal number", raw: "{{checkout.subtotal}}", ctx: withOutputs, want: 120.5},
		{name: "text", raw: "{{checkout.currency}}", ctx: withOutputs, want: "USD"},
		{name: "a boolean, with spaces", raw: "{{ checkout.paid }}", ctx: withOutputs, want: true},
		{name: "in mixed text", raw: "{{checkout.total}} {{checkout.currency}}", ctx: withOutputs, want: "13066 USD"},
		{name: "env still reads the environment", raw: "{{env.MY_VAR}}", ctx: withOutputs, want: "my-value"},
		{
			name:    "outside assertions",
			raw:     "{{checkout.total}}",
			wantErr: "{{checkout.total}} reads a step's output, which only assertions and repeat.until can; in a step value use from: checkout.total",
		},
		{name: "a null output", raw: "{{checkout.coupon}}", ctx: withOutputs, wantErr: `step "checkout" output "coupon" is null`},
		{name: "a list output", raw: "{{checkout.lines}}", ctx: withOutputs, wantErr: `step "checkout" output "lines" is a list`},
		{name: "a lookup error", raw: "{{cart.total}}", ctx: withOutputs, wantErr: "no outputs for cart"},
		{name: "no date arithmetic on an output", raw: "{{checkout.total + 3 days}}", ctx: withOutputs, wantErr: "invalid expression base"},
		{name: "a hyphenated step ID", raw: "{{pay--declined.total}}", ctx: withOutputs, wantErr: "step IDs in {{step.output}} use letters, digits, and underscores"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvalExpr(tt.raw, tt.ctx)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExprOutputRefs(t *testing.T) {
	assert.Equal(t, []OutputRef{{Step: "checkout", Output: "total"}, {Step: "getCart", Output: "subtotal"}},
		ExprOutputRefs("{{checkout.total}} of {{ getCart.subtotal }} on {{today}} from {{env.HOME}} for {{quantity}}"))
	assert.Nil(t, ExprOutputRefs("no expressions"))
	assert.Equal(t, []OutputRef{{Step: "a", Output: "b"}}, ExprValueOutputRefs([]any{"x", map[string]any{"k": "{{a.b}}"}}))
	assert.Equal(t, "{{checkout.total}}", OutputRef{Step: "checkout", Output: "total"}.String())
	assert.Equal(t, []OutputRef{{Step: "price", Output: "totalAmount"}}, PredicateOutputRefs(`totalAmount == "{{price.totalAmount}}" && n > 0`))
}

func TestRewriteExprRefs(t *testing.T) {
	idMap := map[string]string{"price": "inc0_price", "env": "inc0_env"}

	got := RewriteExprRefs(`totalAmount == "{{price.totalAmount}}" && owner == "{{offer.owner}}" && key == "{{env.KEY}}"`, idMap)

	assert.Equal(t, `totalAmount == "{{inc0_price.totalAmount}}" && owner == "{{offer.owner}}" && key == "{{env.KEY}}"`, got,
		"a step idMap names is renamed; another step and the environment are left as they are")
}

func TestRewriteAssertionRefs(t *testing.T) {
	orig := &Assertions{Mechanical: []MechanicalAssertion{
		{Type: "predicate", Expr: `total == "{{checkout.total}}"`},
		{Type: "fieldEquals", Path: "amount", Value: "{{checkout.total}}"},
		{Type: "status", Expect: []any{200}},
	}}

	got := RewriteAssertionRefs(orig, map[string]string{"checkout": "inc0_checkout"})

	assert.Equal(t, `total == "{{inc0_checkout.total}}"`, got.Mechanical[0].Expr)
	assert.Equal(t, "{{inc0_checkout.total}}", got.Mechanical[1].Value)
	assert.Equal(t, `total == "{{checkout.total}}"`, orig.Mechanical[0].Expr, "the original assertions are left as they were")
	assert.Same(t, orig, RewriteAssertionRefs(orig, map[string]string{"other": "renamed"}), "nothing to rename returns the same assertions")
}

func TestRewriteStepRefs_AssertionsAndRepeat(t *testing.T) {
	s := Step{
		ID:         "price",
		Assertions: &Assertions{Mechanical: []MechanicalAssertion{{Type: "predicate", Expr: `n == "{{search.count}}"`}}},
		Repeat:     &RepeatConfig{Until: `n >= "{{search.count}}"`},
	}

	rewriteStepRefs(&s, map[string]string{"search": "search__declined"})

	assert.Equal(t, `n == "{{search__declined.count}}"`, s.Assertions.Mechanical[0].Expr)
	assert.Equal(t, `n >= "{{search__declined.count}}"`, s.Repeat.Until)
}
