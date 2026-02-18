package engine

import (
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckErrorDetection_NoRules(t *testing.T) {
	body := []byte(`{"data": "ok"}`)
	assert.Nil(t, CheckErrorDetection(nil, body))
	assert.Nil(t, CheckErrorDetection([]graph.ErrorDetectionRule{}, body))
}

func TestCheckErrorDetection_EmptyBody(t *testing.T) {
	rules := []graph.ErrorDetectionRule{{Path: "error", Rule: "exists"}}
	assert.Nil(t, CheckErrorDetection(rules, nil))
	assert.Nil(t, CheckErrorDetection(rules, []byte{}))
}

func TestCheckErrorDetection_NonJSONBody(t *testing.T) {
	rules := []graph.ErrorDetectionRule{{Path: "error", Rule: "exists"}}
	assert.Nil(t, CheckErrorDetection(rules, []byte("not json at all")))
}

func TestCheckErrorDetection_ExistsRule(t *testing.T) {
	rules := []graph.ErrorDetectionRule{{Path: "error", Rule: "exists"}}

	tests := []struct {
		name    string
		body    string
		trigger bool
	}{
		{"field exists with string", `{"error": "something went wrong"}`, true},
		{"field exists with number", `{"error": 42}`, true},
		{"field exists with boolean", `{"error": true}`, true},
		{"field exists with object", `{"error": {"code": 1}}`, true},
		{"field exists with array", `{"error": [1]}`, true},
		{"field is null", `{"error": null}`, false},
		{"field missing", `{"data": "ok"}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckErrorDetection(rules, []byte(tt.body))
			if tt.trigger {
				require.NotNil(t, result)
				assert.Equal(t, "error", result.RulePath)
				assert.Equal(t, "exists", result.Rule)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

func TestCheckErrorDetection_NonEmptyRule(t *testing.T) {
	rules := []graph.ErrorDetectionRule{{Path: "errors", Rule: "non-empty"}}

	tests := []struct {
		name    string
		body    string
		trigger bool
	}{
		{"non-empty array", `{"errors": [{"message": "bad"}]}`, true},
		{"empty array", `{"errors": []}`, false},
		{"non-empty string", `{"errors": "something"}`, true},
		{"empty string", `{"errors": ""}`, false},
		{"non-empty object", `{"errors": {"code": 1}}`, true},
		{"empty object", `{"errors": {}}`, false},
		{"null", `{"errors": null}`, false},
		{"missing", `{"data": "ok"}`, false},
		{"number value", `{"errors": 42}`, true},
		{"boolean true", `{"errors": true}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckErrorDetection(rules, []byte(tt.body))
			if tt.trigger {
				require.NotNil(t, result)
				assert.Equal(t, "errors", result.RulePath)
				assert.Equal(t, "non-empty", result.Rule)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

func TestCheckErrorDetection_EqualsRule(t *testing.T) {
	tests := []struct {
		name    string
		rule    graph.ErrorDetectionRule
		body    string
		trigger bool
	}{
		{
			"string match",
			graph.ErrorDetectionRule{Path: "status", Rule: "equals", Value: "error"},
			`{"status": "error"}`,
			true,
		},
		{
			"string mismatch",
			graph.ErrorDetectionRule{Path: "status", Rule: "equals", Value: "error"},
			`{"status": "ok"}`,
			false,
		},
		{
			"boolean match",
			graph.ErrorDetectionRule{Path: "success", Rule: "equals", Value: false},
			`{"success": false}`,
			true,
		},
		{
			"number match",
			graph.ErrorDetectionRule{Path: "code", Rule: "equals", Value: float64(0)},
			`{"code": 0}`,
			true,
		},
		{
			"field missing",
			graph.ErrorDetectionRule{Path: "status", Rule: "equals", Value: "error"},
			`{"data": "ok"}`,
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckErrorDetection([]graph.ErrorDetectionRule{tt.rule}, []byte(tt.body))
			if tt.trigger {
				require.NotNil(t, result)
				assert.Equal(t, tt.rule.Path, result.RulePath)
				assert.Equal(t, "equals", result.Rule)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

func TestCheckErrorDetection_DetailExtraction(t *testing.T) {
	rules := []graph.ErrorDetectionRule{{
		Path: "ErrorResponse.Result.Error",
		Rule: "non-empty",
		Details: &graph.ErrorDetailMapping{
			Message:  "ErrorResponse.Result.Error.0.Message",
			Code:     "ErrorResponse.Result.Error.0.SourceCode",
			Category: "ErrorResponse.Result.Error.0.category",
		},
	}}

	body := `{
		"ErrorResponse": {
			"Result": {
				"Error": [
					{
						"Message": "Invalid workbench ID",
						"SourceCode": "INVALID_INPUT",
						"category": "validation"
					}
				]
			}
		}
	}`

	result := CheckErrorDetection(rules, []byte(body))
	require.NotNil(t, result)
	assert.Equal(t, "Invalid workbench ID", result.Message)
	assert.Equal(t, "INVALID_INPUT", result.Code)
	assert.Equal(t, "validation", result.Category)
}

func TestCheckErrorDetection_DetailExtractionMissing(t *testing.T) {
	rules := []graph.ErrorDetectionRule{{
		Path: "errors",
		Rule: "non-empty",
		Details: &graph.ErrorDetailMapping{
			Message: "errors.0.message",
			Code:    "errors.0.code",
		},
	}}

	body := `{"errors": [{"message": "something failed"}]}`

	result := CheckErrorDetection(rules, []byte(body))
	require.NotNil(t, result)
	assert.Equal(t, "something failed", result.Message)
	assert.Equal(t, "", result.Code) // code path doesn't exist
}

func TestCheckErrorDetection_FirstMatchWins(t *testing.T) {
	rules := []graph.ErrorDetectionRule{
		{Path: "warning", Rule: "exists"},
		{Path: "errors", Rule: "non-empty"},
	}

	body := `{"errors": [{"msg": "bad"}], "warning": "also here"}`
	result := CheckErrorDetection(rules, []byte(body))
	require.NotNil(t, result)
	assert.Equal(t, "warning", result.RulePath, "first matching rule should win")
}

func TestCheckErrorDetection_NoMatchReturnsNil(t *testing.T) {
	rules := []graph.ErrorDetectionRule{
		{Path: "error", Rule: "exists"},
		{Path: "errors", Rule: "non-empty"},
	}

	body := `{"data": "ok", "errors": []}`
	assert.Nil(t, CheckErrorDetection(rules, []byte(body)))
}

func TestCheckErrorDetection_UnknownRuleDoesNotTrigger(t *testing.T) {
	rules := []graph.ErrorDetectionRule{{Path: "error", Rule: "unknown-rule"}}
	body := `{"error": "something"}`
	assert.Nil(t, CheckErrorDetection(rules, []byte(body)))
}

func TestCheckErrorDetection_NestedPath(t *testing.T) {
	rules := []graph.ErrorDetectionRule{{
		Path: "response.errors",
		Rule: "non-empty",
	}}

	body := `{"response": {"errors": [{"msg": "nested error"}]}}`
	result := CheckErrorDetection(rules, []byte(body))
	require.NotNil(t, result)
	assert.Equal(t, "response.errors", result.RulePath)
}

func TestEffectiveErrorRules_NodeOverridesGraph(t *testing.T) {
	graphRules := []graph.ErrorDetectionRule{{Path: "graph-error", Rule: "exists"}}
	nodeRules := []graph.ErrorDetectionRule{{Path: "node-error", Rule: "exists"}}

	g := &graph.Graph{ErrorDetection: graphRules}
	node := &graph.Node{ErrorDetection: nodeRules}

	rules := effectiveErrorRules(node, g)
	require.Len(t, rules, 1)
	assert.Equal(t, "node-error", rules[0].Path)
}

func TestEffectiveErrorRules_InheritsFromGraph(t *testing.T) {
	graphRules := []graph.ErrorDetectionRule{{Path: "graph-error", Rule: "exists"}}

	g := &graph.Graph{ErrorDetection: graphRules}
	node := &graph.Node{} // no node-level rules

	rules := effectiveErrorRules(node, g)
	require.Len(t, rules, 1)
	assert.Equal(t, "graph-error", rules[0].Path)
}

func TestEffectiveErrorRules_NoRules(t *testing.T) {
	g := &graph.Graph{}
	node := &graph.Node{}

	rules := effectiveErrorRules(node, g)
	assert.Empty(t, rules)
}

func TestResponseBodyError_Summary(t *testing.T) {
	rbe := &ResponseBodyError{
		RulePath: "errors",
		Rule:     "non-empty",
		Message:  "Something failed",
		Code:     "ERR_001",
	}
	summary := rbe.Summary()
	assert.Contains(t, summary, "errors")
	assert.Contains(t, summary, "non-empty")
	assert.Contains(t, summary, "Something failed")
	assert.Contains(t, summary, "ERR_001")
}

func TestResponseBodyError_SummaryMinimal(t *testing.T) {
	rbe := &ResponseBodyError{
		RulePath: "error",
		Rule:     "exists",
	}
	summary := rbe.Summary()
	assert.Contains(t, summary, "error")
	assert.NotContains(t, summary, "code:")
}
