package validate

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/tidwall/gjson"
)

// AssertionType enumerates the kinds of mechanical assertion.
type AssertionType string

const (
	AssertStatus      AssertionType = "status"
	AssertSchema      AssertionType = "schema"
	AssertFieldExists AssertionType = "fieldExists"
	AssertFieldEquals AssertionType = "fieldEquals"
	AssertPredicate   AssertionType = "predicate"
)

// MechanicalAssertion describes a structured check on a response.
type MechanicalAssertion struct {
	Type   AssertionType
	Expect any
	Ref    string
	Path   string
	Value  any
	Expr   string
	Raw    bool
}

// AssertionResult records the outcome of a single assertion.
type AssertionResult struct {
	Type    AssertionType
	Passed  bool
	Skipped bool // true when assertion was not evaluated (e.g., not yet implemented)
	Message string
	Path    string
	Expr    string
	Raw     bool
}

// MechanicalResult aggregates the outcomes of all mechanical assertions for a step.
type MechanicalResult struct {
	Passed  bool
	Results []AssertionResult
}

// PredicateEvalFunc evaluates a predicate expression against a context map.
// It returns true/false or an error if the expression is invalid.
type PredicateEvalFunc func(expr string, context map[string]any) (bool, error)

// RunMechanical evaluates all mechanical assertions against a response.
// It returns an aggregate result indicating whether all assertions passed.
func RunMechanical(statusCode int, body []byte, assertions []MechanicalAssertion, predicateEval PredicateEvalFunc) *MechanicalResult {
	result := &MechanicalResult{Passed: true}

	for _, a := range assertions {
		var ar AssertionResult
		switch a.Type {
		case AssertStatus:
			ar = checkStatus(statusCode, a)
		case AssertSchema:
			ar = checkSchema(a)
		case AssertFieldExists:
			ar = checkFieldExists(body, a)
		case AssertFieldEquals:
			ar = checkFieldEquals(body, a)
		case AssertPredicate:
			ar = checkPredicate(body, a, predicateEval)
		default:
			ar = AssertionResult{
				Type:    a.Type,
				Passed:  false,
				Message: fmt.Sprintf("unknown assertion type %q", a.Type),
			}
		}

		ar.Raw = a.Raw
		result.Results = append(result.Results, ar)
		if !ar.Passed {
			result.Passed = false
		}
	}

	return result
}

// checkStatus compares the response status code against the expected value.
func checkStatus(statusCode int, a MechanicalAssertion) AssertionResult {
	ar := AssertionResult{Type: AssertStatus}

	if a.Expect == nil {
		ar.Passed = false
		ar.Message = "status assertion missing 'expect' value"
		return ar
	}

	expected, ok := toInt(a.Expect)
	if !ok {
		ar.Passed = false
		ar.Message = fmt.Sprintf("cannot coerce expect value %v (%T) to int", a.Expect, a.Expect)
		return ar
	}

	if statusCode == expected {
		ar.Passed = true
		ar.Message = fmt.Sprintf("status code is %d", statusCode)
	} else {
		ar.Passed = false
		ar.Message = fmt.Sprintf("expected status %d, got %d", expected, statusCode)
	}
	return ar
}

// checkSchema is a stub for Stage 1 — always returns passed with a not-yet-implemented message.
// Skipped is true so archive consumers can distinguish "passed" from "not evaluated".
func checkSchema(a MechanicalAssertion) AssertionResult {
	return AssertionResult{
		Type:    AssertSchema,
		Passed:  true,
		Skipped: true,
		Message: "schema validation not yet implemented",
	}
}

// checkFieldExists verifies that a field exists and is not null in the response body.
func checkFieldExists(body []byte, a MechanicalAssertion) AssertionResult {
	ar := AssertionResult{Type: AssertFieldExists, Path: a.Path}

	path := NormalizeJSONPath(a.Path)
	if path == "" {
		ar.Passed = false
		ar.Message = "fieldExists assertion requires a non-empty path"
		return ar
	}

	r := gjson.GetBytes(body, path)
	if !r.Exists() || r.Type == gjson.Null {
		ar.Passed = false
		ar.Message = fmt.Sprintf("field %q does not exist or is null", a.Path)
	} else {
		ar.Passed = true
		ar.Message = fmt.Sprintf("field %q exists", a.Path)
	}
	return ar
}

// checkFieldEquals verifies that a field in the response body equals the expected value.
func checkFieldEquals(body []byte, a MechanicalAssertion) AssertionResult {
	ar := AssertionResult{Type: AssertFieldEquals, Path: a.Path}

	path := NormalizeJSONPath(a.Path)
	if path == "" {
		ar.Passed = false
		ar.Message = "fieldEquals assertion requires a non-empty path"
		return ar
	}

	r := gjson.GetBytes(body, path)
	if !r.Exists() {
		ar.Passed = false
		ar.Message = fmt.Sprintf("field %q does not exist", a.Path)
		return ar
	}

	if valuesEqual(r, a.Value) {
		ar.Passed = true
		ar.Message = fmt.Sprintf("field %q equals %v", a.Path, a.Value)
	} else {
		ar.Passed = false
		ar.Message = fmt.Sprintf("field %q: expected %v, got %v", a.Path, a.Value, r.Value())
	}
	return ar
}

// checkPredicate evaluates a predicate expression against the response body.
func checkPredicate(body []byte, a MechanicalAssertion, predicateEval PredicateEvalFunc) AssertionResult {
	ar := AssertionResult{Type: AssertPredicate, Expr: a.Expr}

	if predicateEval == nil {
		ar.Passed = false
		ar.Message = "predicate evaluator is not available"
		return ar
	}

	if a.Expr == "" {
		ar.Passed = false
		ar.Message = "predicate assertion requires a non-empty 'expr'"
		return ar
	}

	var bodyMap map[string]any
	if err := json.Unmarshal(body, &bodyMap); err != nil {
		ar.Passed = false
		ar.Message = fmt.Sprintf("response body is not a JSON object: %v", err)
		return ar
	}

	result, err := predicateEval(a.Expr, bodyMap)
	if err != nil {
		ar.Passed = false
		ar.Message = fmt.Sprintf("predicate evaluation error: %v", err)
		return ar
	}

	ar.Passed = result
	if result {
		ar.Message = fmt.Sprintf("predicate %q is true", a.Expr)
	} else {
		ar.Message = fmt.Sprintf("predicate %q is false", a.Expr)
	}
	return ar
}

// toInt converts numeric values to int. Handles int, float64, and json.Number.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n == math.Trunc(n) {
			return int(n), true
		}
		return 0, false
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return int(i), true
		}
		return 0, false
	default:
		return 0, false
	}
}

// valuesEqual compares a gjson.Result with an expected value, handling numeric coercion.
func valuesEqual(r gjson.Result, expected any) bool {
	switch e := expected.(type) {
	case string:
		return r.Type == gjson.String && r.Str == e
	case bool:
		return r.Type == gjson.True && e || r.Type == gjson.False && !e
	case float64:
		return r.Type == gjson.Number && r.Num == e
	case int:
		return r.Type == gjson.Number && r.Num == float64(e)
	case int64:
		return r.Type == gjson.Number && r.Num == float64(e)
	default:
		// Fallback: compare string representations
		return fmt.Sprintf("%v", r.Value()) == fmt.Sprintf("%v", expected)
	}
}
