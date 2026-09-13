package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
)

func assertionGraph() *graph.Graph {
	return &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"getCart": {Name: "getCart", Inputs: []graph.Input{
			{Name: "cartId", Type: "string"},
			{Name: "note", Type: "string", Optional: true},
		}},
	}}
}

func TestValidate_UnknownAssertionType(t *testing.T) {
	p := &Plan{Execution: Execution{
		Steps: []Step{{
			Node:       "getCart",
			Values:     map[string]StepValue{"cartId": {Default: "cart-1"}},
			Assertions: &Assertions{Mechanical: []MechanicalAssertion{{Type: "fieldEqual", Path: "status", Value: "open"}}},
		}},
		Verification: []VerificationStep{{
			Node:       "getCart",
			Assertions: &Assertions{Mechanical: []MechanicalAssertion{{Type: "status", Expect: 200}, {Type: "jsonSchema"}}},
		}},
	}}

	err := Validate(p, assertionGraph())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `step 0 (getCart): assertion 0 has unknown type "fieldEqual" (use status, fieldExists, fieldEquals, predicate, schema)`)
	assert.Contains(t, err.Error(), `verification step 0 (getCart): assertion 1 has unknown type "jsonSchema"`)
}

func TestValidate_ExpressionSyntax(t *testing.T) {
	p := &Plan{Execution: Execution{Steps: []Step{{
		Node: "getCart",
		Values: map[string]StepValue{
			"cartId": {Default: "cart-{{today +}}"},
			"note":   {Pool: []any{"gift", "{{now +}}"}},
		},
		Assertions: &Assertions{Mechanical: []MechanicalAssertion{
			{Type: "fieldEquals", Path: "createdOn", Value: "{{today +}}"},
			{Type: "predicate", Expr: `status == "{{unixtime -}}"`},
			{Type: "fieldEquals", Path: "cartId", Value: "{{cartId}}"},
		}},
	}}}}

	err := Validate(p, assertionGraph())
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, `step 0 (getCart): invalid expression for "cartId":`)
	assert.Contains(t, msg, `step 0 (getCart): invalid expression in pool entry 1 for "note":`)
	assert.Contains(t, msg, `step 0 (getCart): invalid expression in fieldEquals assertion 0:`)
	assert.Contains(t, msg, `step 0 (getCart): invalid expression in predicate assertion 1:`)
	assert.NotContains(t, msg, "assertion 2", "a reference to an input is valid")
}
