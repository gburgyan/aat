package validate

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubPredicateEval is a test predicate evaluator that delegates to plan.EvalPredicate-compatible logic.
func stubPredicateEval(expr string, context map[string]any) (bool, error) {
	// Simple evaluator for tests: supports "true", "false", and field comparisons
	switch expr {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	// For more complex expressions, return an error to indicate unsupported
	return false, fmt.Errorf("stubPredicateEval: unsupported expression %q", expr)
}

func TestCheckStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		expect     any
		passed     bool
		msgContain string
	}{
		{name: "match 200", statusCode: 200, expect: 200, passed: true, msgContain: "status code is 200"},
		{name: "match 201", statusCode: 201, expect: 201, passed: true, msgContain: "status code is 201"},
		{name: "mismatch", statusCode: 404, expect: 200, passed: false, msgContain: "expected status 200, got 404"},
		{name: "float64 coercion from YAML", statusCode: 200, expect: float64(200), passed: true, msgContain: "status code is 200"},
		{name: "nil expect", statusCode: 200, expect: nil, passed: false, msgContain: "missing 'expect'"},
		{name: "string expect fails", statusCode: 200, expect: "200", passed: false, msgContain: "cannot coerce"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := MechanicalAssertion{Type: AssertStatus, Expect: tt.expect}
			ar := checkStatus(tt.statusCode, a)
			assert.Equal(t, tt.passed, ar.Passed)
			assert.Contains(t, ar.Message, tt.msgContain)
			assert.Equal(t, AssertStatus, ar.Type)
		})
	}
}

func TestCheckSchema(t *testing.T) {
	ar := checkSchema(MechanicalAssertion{Type: AssertSchema, Ref: "some-schema.json"})
	assert.True(t, ar.Passed)
	assert.Contains(t, ar.Message, "not yet implemented")
	assert.Equal(t, AssertSchema, ar.Type)
}

func TestCheckFieldExists(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		path       string
		passed     bool
		msgContain string
	}{
		{
			name:       "field present",
			body:       `{"name": "Alice"}`,
			path:       "name",
			passed:     true,
			msgContain: `field "name" exists`,
		},
		{
			name:       "nested field present",
			body:       `{"price": {"amount": 99.99}}`,
			path:       "price.amount",
			passed:     true,
			msgContain: `field "price.amount" exists`,
		},
		{
			name:       "field null",
			body:       `{"name": null}`,
			path:       "name",
			passed:     false,
			msgContain: "does not exist or is null",
		},
		{
			name:       "field absent",
			body:       `{"name": "Alice"}`,
			path:       "age",
			passed:     false,
			msgContain: "does not exist or is null",
		},
		{
			name:       "dollar prefix stripped",
			body:       `{"name": "Alice"}`,
			path:       "$.name",
			passed:     true,
			msgContain: `field "$.name" exists`,
		},
		{
			name:       "bracket notation",
			body:       `{"items": [{"id": "a"}, {"id": "b"}]}`,
			path:       "items[0].id",
			passed:     true,
			msgContain: "exists",
		},
		{
			name:       "empty body",
			body:       "",
			path:       "name",
			passed:     false,
			msgContain: "does not exist",
		},
		{
			name:       "invalid JSON",
			body:       "not json",
			path:       "name",
			passed:     false,
			msgContain: "does not exist",
		},
		{
			name:       "boolean false exists",
			body:       `{"active": false}`,
			path:       "active",
			passed:     true,
			msgContain: "exists",
		},
		{
			name:       "empty string exists",
			body:       `{"name": ""}`,
			path:       "name",
			passed:     true,
			msgContain: "exists",
		},
		{
			name:       "zero exists",
			body:       `{"count": 0}`,
			path:       "count",
			passed:     true,
			msgContain: "exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := MechanicalAssertion{Type: AssertFieldExists, Path: tt.path}
			ar := checkFieldExists([]byte(tt.body), a)
			assert.Equal(t, tt.passed, ar.Passed)
			assert.Contains(t, ar.Message, tt.msgContain)
			assert.Equal(t, AssertFieldExists, ar.Type)
			assert.Equal(t, tt.path, ar.Path)
		})
	}
}

func TestCheckFieldEquals(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		path       string
		value      any
		passed     bool
		msgContain string
	}{
		{
			name:       "string match",
			body:       `{"name": "Alice"}`,
			path:       "name",
			value:      "Alice",
			passed:     true,
			msgContain: "equals expected",
		},
		{
			name:       "number match",
			body:       `{"price": 99.99}`,
			path:       "price",
			value:      99.99,
			passed:     true,
			msgContain: "equals expected",
		},
		{
			name:       "bool match",
			body:       `{"active": true}`,
			path:       "active",
			value:      true,
			passed:     true,
			msgContain: "equals expected",
		},
		{
			name:       "int vs float64 coercion",
			body:       `{"count": 42}`,
			path:       "count",
			value:      int(42),
			passed:     true,
			msgContain: "equals expected",
		},
		{
			name:       "wrong value",
			body:       `{"name": "Alice"}`,
			path:       "name",
			value:      "Bob",
			passed:     false,
			msgContain: "expected Bob, got Alice",
		},
		{
			name:       "missing field",
			body:       `{"name": "Alice"}`,
			path:       "age",
			value:      25,
			passed:     false,
			msgContain: "does not exist",
		},
		{
			name:       "nested field match",
			body:       `{"price": {"amount": 100}}`,
			path:       "price.amount",
			value:      float64(100),
			passed:     true,
			msgContain: "equals expected",
		},
		{
			name:       "dollar prefix",
			body:       `{"name": "Alice"}`,
			path:       "$.name",
			value:      "Alice",
			passed:     true,
			msgContain: "equals expected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := MechanicalAssertion{Type: AssertFieldEquals, Path: tt.path, Value: tt.value}
			ar := checkFieldEquals([]byte(tt.body), a)
			assert.Equal(t, tt.passed, ar.Passed)
			assert.Contains(t, ar.Message, tt.msgContain)
			assert.Equal(t, AssertFieldEquals, ar.Type)
			assert.Equal(t, tt.path, ar.Path)
		})
	}
}

func TestCheckPredicate(t *testing.T) {
	// Use a real-ish predicate evaluator for these tests
	realEval := func(expr string, ctx map[string]any) (bool, error) {
		// Minimal evaluator that handles the test cases
		switch expr {
		case "status == 'active'":
			if v, ok := ctx["status"]; ok {
				return v == "active", nil
			}
			return false, fmt.Errorf("unknown field \"status\"")
		case "price > 0":
			if v, ok := ctx["price"]; ok {
				switch n := v.(type) {
				case float64:
					return n > 0, nil
				}
			}
			return false, fmt.Errorf("unknown field \"price\"")
		case "count > 0 && active == true":
			count, _ := ctx["count"].(float64)
			active, _ := ctx["active"].(bool)
			return count > 0 && active, nil
		case "bad syntax !!!":
			return false, fmt.Errorf("parse error")
		}
		return false, fmt.Errorf("unsupported expression %q", expr)
	}

	tests := []struct {
		name       string
		body       string
		expr       string
		eval       PredicateEvalFunc
		passed     bool
		msgContain string
	}{
		{
			name:       "true expression",
			body:       `{"status": "active"}`,
			expr:       "status == 'active'",
			eval:       realEval,
			passed:     true,
			msgContain: "is true",
		},
		{
			name:       "false expression",
			body:       `{"status": "inactive"}`,
			expr:       "status == 'active'",
			eval:       realEval,
			passed:     false,
			msgContain: "is false",
		},
		{
			name:       "complex expression",
			body:       `{"count": 5, "active": true}`,
			expr:       "count > 0 && active == true",
			eval:       realEval,
			passed:     true,
			msgContain: "is true",
		},
		{
			name:       "invalid syntax",
			body:       `{"x": 1}`,
			expr:       "bad syntax !!!",
			eval:       realEval,
			passed:     false,
			msgContain: "evaluation error",
		},
		{
			name:       "nil evaluator",
			body:       `{"x": 1}`,
			expr:       "x > 0",
			eval:       nil,
			passed:     false,
			msgContain: "evaluator is not available",
		},
		{
			name:       "non-object body",
			body:       `[1, 2, 3]`,
			expr:       "x > 0",
			eval:       realEval,
			passed:     false,
			msgContain: "not a JSON object",
		},
		{
			name:       "empty expr",
			body:       `{"x": 1}`,
			expr:       "",
			eval:       realEval,
			passed:     false,
			msgContain: "non-empty",
		},
		{
			name:       "empty body",
			body:       "",
			expr:       "x > 0",
			eval:       realEval,
			passed:     false,
			msgContain: "not a JSON object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := MechanicalAssertion{Type: AssertPredicate, Expr: tt.expr}
			ar := checkPredicate([]byte(tt.body), a, tt.eval)
			assert.Equal(t, tt.passed, ar.Passed)
			assert.Contains(t, ar.Message, tt.msgContain)
			assert.Equal(t, AssertPredicate, ar.Type)
			assert.Equal(t, tt.expr, ar.Expr)
		})
	}
}

func TestRunMechanical_AllPass(t *testing.T) {
	body := []byte(`{"name": "Alice", "age": 30}`)
	assertions := []MechanicalAssertion{
		{Type: AssertStatus, Expect: 200},
		{Type: AssertFieldExists, Path: "name"},
		{Type: AssertFieldEquals, Path: "name", Value: "Alice"},
	}

	result := RunMechanical(200, body, assertions, nil)
	assert.True(t, result.Passed)
	require.Len(t, result.Results, 3)
	for _, r := range result.Results {
		assert.True(t, r.Passed)
	}
}

func TestRunMechanical_OneFails(t *testing.T) {
	body := []byte(`{"name": "Alice"}`)
	assertions := []MechanicalAssertion{
		{Type: AssertStatus, Expect: 200},
		{Type: AssertFieldExists, Path: "age"}, // fails
		{Type: AssertFieldEquals, Path: "name", Value: "Alice"},
	}

	result := RunMechanical(200, body, assertions, nil)
	assert.False(t, result.Passed)
	require.Len(t, result.Results, 3)
	assert.True(t, result.Results[0].Passed)
	assert.False(t, result.Results[1].Passed)
	assert.True(t, result.Results[2].Passed)
}

func TestRunMechanical_EmptyList(t *testing.T) {
	result := RunMechanical(200, []byte(`{}`), nil, nil)
	assert.True(t, result.Passed)
	assert.Empty(t, result.Results)
}

func TestRunMechanical_MixedResults(t *testing.T) {
	body := []byte(`{"status": "active", "count": 0}`)
	eval := func(expr string, ctx map[string]any) (bool, error) {
		// count > 0 evaluates to false
		if expr == "count > 0" {
			count, _ := ctx["count"].(float64)
			return count > 0, nil
		}
		return false, fmt.Errorf("unsupported")
	}

	assertions := []MechanicalAssertion{
		{Type: AssertStatus, Expect: 200},
		{Type: AssertFieldExists, Path: "status"},
		{Type: AssertPredicate, Expr: "count > 0"}, // fails: count is 0
	}

	result := RunMechanical(200, body, assertions, eval)
	assert.False(t, result.Passed)
	require.Len(t, result.Results, 3)
	assert.True(t, result.Results[0].Passed)
	assert.True(t, result.Results[1].Passed)
	assert.False(t, result.Results[2].Passed)
}

func TestRunMechanical_UnknownType(t *testing.T) {
	assertions := []MechanicalAssertion{
		{Type: "bogus"},
	}
	result := RunMechanical(200, []byte(`{}`), assertions, nil)
	assert.False(t, result.Passed)
	require.Len(t, result.Results, 1)
	assert.Contains(t, result.Results[0].Message, "unknown assertion type")
}
