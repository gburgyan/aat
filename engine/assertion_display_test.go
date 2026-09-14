package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/plan"
)

// TestExecuteStep_AssertionMessagesShowExpandedValues checks that an assertion
// message says what was compared: a predicate with the expressions in its
// literals expanded, and a fieldEquals value after expansion.
func TestExecuteStep_AssertionMessagesShowExpandedValues(t *testing.T) {
	expr := `quantity == "{{quantity}}" && sku == "{{sku}}"`
	v := runAssertions(t,
		plan.MechanicalAssertion{Type: "predicate", Expr: expr},
		plan.MechanicalAssertion{Type: "fieldEquals", Path: "sku", Value: "{{sku}}"},
	)
	require.Len(t, v.Results, 2)
	assert.Equal(t, `predicate "quantity == 3 && sku == \"SKU-1\"" is true`, v.Results[0].Message)
	assert.Equal(t, expr, v.Results[0].Expr, "the result keeps the predicate as written")
	assert.Equal(t, `field "sku" equals SKU-1`, v.Results[1].Message)
}
