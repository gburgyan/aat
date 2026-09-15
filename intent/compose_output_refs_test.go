package intent

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gburgyan/aat/plan"
)

// TestPrefixStepRefs_OutputRefs checks that an addon's references to its own
// steps follow their prefixed IDs, in assertions, repeat conditions, and
// verification, while a reference to a base workflow step keeps its ID.
func TestPrefixStepRefs_OutputRefs(t *testing.T) {
	sub := &plan.Plan{Execution: plan.Execution{
		Steps: []plan.Step{
			{ID: "current", Node: "getOrder"},
			{
				ID:   "quote",
				Node: "createOrderCancellation",
				Assertions: &plan.Assertions{Mechanical: []plan.MechanicalAssertion{
					{Type: "predicate", Expr: `refundAmount == "{{current.totalAmount}}" && refundAmount <= "{{book.totalAmount}}"`},
				}},
			},
			{ID: "after", Node: "getOrder", Repeat: &plan.RepeatConfig{Until: `totalAmount != "{{current.totalAmount}}"`}},
		},
		Verification: []plan.VerificationStep{{
			Node: "getOrder",
			Assertions: &plan.Assertions{Mechanical: []plan.MechanicalAssertion{
				{Type: "fieldEquals", Path: "totalAmount", Value: "{{current.totalAmount}}"},
			}},
		}},
	}}

	prefixStepRefs(sub, "inc0_")

	assert.Equal(t, `refundAmount == "{{inc0_current.totalAmount}}" && refundAmount <= "{{book.totalAmount}}"`,
		sub.Execution.Steps[1].Assertions.Mechanical[0].Expr)
	assert.Equal(t, `totalAmount != "{{inc0_current.totalAmount}}"`, sub.Execution.Steps[2].Repeat.Until)
	assert.Equal(t, "{{inc0_current.totalAmount}}", sub.Execution.Verification[0].Assertions.Mechanical[0].Value)
}
