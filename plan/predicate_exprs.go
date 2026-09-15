package plan

import (
	"fmt"
	"slices"

	"github.com/gburgyan/aat/internal/predicate"
)

// EvalPredicateWithExprs evaluates a predicate like predicate.Eval, after
// expanding each quoted string literal that holds a {{…}} expression with ectx,
// so an assertion can compare a field with a computed value, as in
// `deliveryDate == "{{today + 3 days}}"`. A literal whose expression evaluates
// to a number or a boolean becomes that value. A selection filter's literals
// are expanded the same way, with ExpandPredicateText, before it is evaluated;
// cleanup conditions use predicate.Eval, where such a literal stays text.
func EvalPredicateWithExprs(expr string, context map[string]any, ectx ExprContext) (bool, error) {
	return predicate.EvalExpanding(expr, context, exprExpander(ectx))
}

// ExpandPredicateText returns expr with the {{…}} expressions in its quoted
// literals replaced by their values, as EvalPredicateWithExprs compares them, so
// an assertion message can show what was compared.
func ExpandPredicateText(expr string, ectx ExprContext) (string, error) {
	return predicate.ExpandText(expr, exprExpander(ectx))
}

// exprExpander evaluates a quoted predicate literal that holds a {{…}}
// expression, and leaves any other literal as it is.
func exprExpander(ectx ExprContext) func(literal string) (any, bool, error) {
	return func(literal string) (any, bool, error) {
		if !ContainsExpr(literal) {
			return nil, false, nil
		}
		v, err := EvalExpr(literal, ectx)
		if err != nil {
			return nil, false, fmt.Errorf("evaluating %q: %w", literal, err)
		}
		return v, true, nil
	}
}

// PredicateOutputRefs returns the {{step.output}} references in a predicate's
// quoted string literals, in order.
func PredicateOutputRefs(expr string) []OutputRef {
	if !ContainsExpr(expr) {
		return nil
	}
	literals, err := predicate.Literals(expr)
	if err != nil {
		return nil
	}
	var refs []OutputRef
	for _, literal := range literals {
		refs = append(refs, ExprOutputRefs(literal)...)
	}
	return refs
}

// AssertionOutputRefs returns the {{step.output}} references an assertion
// reads: in a predicate's quoted literals, or in a fieldEquals value.
func AssertionOutputRefs(a MechanicalAssertion) []OutputRef {
	switch a.Type {
	case "predicate":
		return PredicateOutputRefs(a.Expr)
	case "fieldEquals":
		return ExprValueOutputRefs(a.Value)
	}
	return nil
}

// StepOutputRefs returns the {{step.output}} references a step reads in its
// assertions and its repeat condition.
func StepOutputRefs(assertions *Assertions, repeat *RepeatConfig) []OutputRef {
	var refs []OutputRef
	if assertions != nil {
		for _, a := range assertions.Mechanical {
			refs = append(refs, AssertionOutputRefs(a)...)
		}
	}
	if repeat != nil {
		refs = append(refs, PredicateOutputRefs(repeat.Until)...)
	}
	return refs
}

// RewriteAssertionRefs returns a copy of assertions whose {{step.output}}
// references name the steps idMap maps their old IDs to, or assertions itself
// when none changes.
func RewriteAssertionRefs(assertions *Assertions, idMap map[string]string) *Assertions {
	if assertions == nil {
		return nil
	}
	var out *Assertions
	for i, a := range assertions.Mechanical {
		expr := RewriteExprRefs(a.Expr, idMap)
		value := a.Value
		if s, ok := a.Value.(string); ok {
			value = RewriteExprRefs(s, idMap)
		}
		if expr == a.Expr && value == a.Value {
			continue
		}
		if out == nil {
			cp := *assertions
			cp.Mechanical = slices.Clone(assertions.Mechanical)
			out = &cp
		}
		out.Mechanical[i].Expr = expr
		out.Mechanical[i].Value = value
	}
	if out == nil {
		return assertions
	}
	return out
}

// RewriteRepeatRefs returns a copy of repeat whose condition's {{step.output}}
// references name the steps idMap maps their old IDs to, or repeat itself when
// none changes.
func RewriteRepeatRefs(repeat *RepeatConfig, idMap map[string]string) *RepeatConfig {
	if repeat == nil {
		return nil
	}
	until := RewriteExprRefs(repeat.Until, idMap)
	if until == repeat.Until {
		return repeat
	}
	cp := repeat.Clone()
	cp.Until = until
	return cp
}

// ValidatePredicateExprs checks the syntax of the {{…}} expressions in a
// predicate's quoted string literals, without evaluating them.
func ValidatePredicateExprs(expr string) error {
	literals, err := predicate.Literals(expr)
	if err != nil {
		return err
	}
	for _, literal := range literals {
		if !ContainsExpr(literal) {
			continue
		}
		if err := ValidateExpr(literal); err != nil {
			return fmt.Errorf("%q: %w", literal, err)
		}
	}
	return nil
}
