package plan

import (
	"fmt"

	"github.com/gburgyan/aat/internal/predicate"
)

// EvalPredicateWithExprs evaluates a predicate like predicate.Eval, after
// expanding each quoted string literal that holds a {{…}} expression with ectx,
// so an assertion can compare a field with a computed value, as in
// `deliveryDate == "{{today + 3 days}}"`. A literal whose expression evaluates
// to a number or a boolean becomes that value. Selection filters and cleanup
// conditions use predicate.Eval, where such a literal stays text.
func EvalPredicateWithExprs(expr string, context map[string]any, ectx ExprContext) (bool, error) {
	return predicate.EvalExpanding(expr, context, func(literal string) (any, bool, error) {
		if !ContainsExpr(literal) {
			return nil, false, nil
		}
		v, err := EvalExpr(literal, ectx)
		if err != nil {
			return nil, false, fmt.Errorf("evaluating %q: %w", literal, err)
		}
		return v, true, nil
	})
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
