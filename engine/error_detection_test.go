package engine

import (
	"testing"

	"github.com/gburgyan/aat/adapter"
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
		Path: "orderResponse.result.errors",
		Rule: "non-empty",
		Details: &graph.ErrorDetailMapping{
			Message:  "orderResponse.result.errors.0.message",
			Code:     "orderResponse.result.errors.0.code",
			Category: "orderResponse.result.errors.0.type",
		},
	}}

	body := `{
		"orderResponse": {
			"result": {
				"errors": [
					{
						"message": "Invalid order ID",
						"code": "INVALID_INPUT",
						"type": "validation"
					}
				]
			}
		}
	}`

	result := CheckErrorDetection(rules, []byte(body))
	require.NotNil(t, result)
	assert.Equal(t, "Invalid order ID", result.Message)
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

// envelopeRules read errors from an envelope whose root key names the
// operation, and from one with no root key.
func envelopeRules(m graph.ErrorStatus) []graph.ErrorDetectionRule {
	rule := func(root string) graph.ErrorDetectionRule {
		return graph.ErrorDetectionRule{Path: root + "result.errors", Rule: "non-empty", ErrorStatus: m,
			Details: &graph.ErrorDetailMapping{Message: root + "result.errors.0.message", Category: root + "result.errors.0.type"}}
	}
	return []graph.ErrorDetectionRule{rule("*."), rule("")}
}

// TestCheckErrorDetection_EnvelopeRoots checks that a wildcard rule reads an
// error under any root key, that a bare root needs a rule of its own, and
// that warnings are not errors.
func TestCheckErrorDetection_EnvelopeRoots(t *testing.T) {
	rules := envelopeRules(graph.ErrorStatus{})
	for _, body := range []string{
		`{"orderResponse": {"result": {"errors": [{"type": "VALIDATION", "message": "email is not valid"}]}}}`,
		`{"paymentResponse": {"result": {"errors": [{"type": "VALIDATION", "message": "email is not valid"}]}}}`,
		`{"result": {"errors": [{"type": "VALIDATION", "message": "email is not valid"}]}}`,
	} {
		rbe := CheckErrorDetection(rules, []byte(body))
		require.NotNil(t, rbe, body)
		assert.Equal(t, "email is not valid", rbe.Message)
		assert.Equal(t, "VALIDATION", rbe.Category)
	}
	assert.Nil(t, CheckErrorDetection(rules[:1], []byte(`{"result": {"errors": [{"message": "x"}]}}`)),
		"the wildcard needs a key above result")
	for _, body := range []string{
		`{"orderResponse": {"result": {"warnings": [{"message": "no fares"}]}}}`,
		`{"orderResponse": {"result": {"errors": []}}}`,
		`{"result": {"status": "complete"}}`,
	} {
		assert.Nil(t, CheckErrorDetection(rules, []byte(body)), body)
	}
}

// TestDetectBodyError_Status checks the order a detected error's status is
// resolved in: the rule's categories, the rule's status, the graph's
// categories, the graph's status, and none.
func TestDetectBodyError_Status(t *testing.T) {
	body := func(category string) []byte {
		return []byte(`{"orderResponse": {"result": {"errors": [{"type": "` + category + `", "message": "m"}]}}}`)
	}
	graphStatus := &graph.ErrorStatus{Status: 500, Categories: map[string]int{"TEMPORARY": 503, "VALIDATION": 422}}
	tests := []struct {
		name     string
		rule     graph.ErrorStatus
		graph    *graph.ErrorStatus
		category string
		want     int
	}{
		{"rule category", graph.ErrorStatus{Status: 418, Categories: map[string]int{"VALIDATION": 400}}, graphStatus, "validation", 400},
		{"rule status", graph.ErrorStatus{Status: 418, Categories: map[string]int{"VALIDATION": 400}}, graphStatus, "TEMPORARY", 418},
		{"graph category", graph.ErrorStatus{Categories: map[string]int{"VALIDATION": 400}}, graphStatus, "TEMPORARY", 503},
		{"graph status", graph.ErrorStatus{}, graphStatus, "UNKNOWN", 500},
		{"no category", graph.ErrorStatus{}, graphStatus, "", 500},
		{"none", graph.ErrorStatus{}, nil, "VALIDATION", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &graph.Node{Name: "n"}
			e := &Engine{graph: &graph.Graph{ErrorDetection: envelopeRules(tt.rule), ErrorStatus: tt.graph}}
			rbe := e.detectBodyError(node, body(tt.category))
			require.NotNil(t, rbe)
			assert.Equal(t, tt.want, rbe.Status)
		})
	}

	// The graph's mapping covers a node's own rules too.
	node := &graph.Node{Name: "n", ErrorDetection: envelopeRules(graph.ErrorStatus{})}
	e := &Engine{graph: &graph.Graph{ErrorStatus: graphStatus}}
	assert.Equal(t, 503, e.detectBodyError(node, body("TEMPORARY")).Status)
}

func TestStepResult_FailureStatus(t *testing.T) {
	status, name := (&StepResult{StatusCode: 200}).FailureStatus()
	assert.Equal(t, 200, status)
	assert.Empty(t, name)

	status, _ = (&StepResult{StatusCode: 200, ResponseBodyError: &ResponseBodyError{}}).FailureStatus()
	assert.Equal(t, 200, status, "an error with no status leaves the response's own")

	status, name = (&StepResult{StatusCode: 200, Response: &adapter.Response{GRPC: &adapter.GRPCStatus{Name: "OK"}},
		ResponseBodyError: &ResponseBodyError{Status: 400}}).FailureStatus()
	assert.Equal(t, 400, status)
	assert.Empty(t, name, "a body error's status has no gRPC name")

	status, name = (&StepResult{StatusCode: 404, Response: &adapter.Response{GRPC: &adapter.GRPCStatus{Name: "NOT_FOUND"}}}).FailureStatus()
	assert.Equal(t, 404, status)
	assert.Equal(t, "NOT_FOUND", name)

	assert.Equal(t, "200, body error as 500", (&StepResult{StatusCode: 200, ResponseBodyError: &ResponseBodyError{Status: 500}}).StatusText())
	assert.Equal(t, "200, body error", (&StepResult{StatusCode: 200, ResponseBodyError: &ResponseBodyError{}}).StatusText())
	assert.Equal(t, "201", (&StepResult{StatusCode: 201}).StatusText())
}

func TestResponseBodyError_SummaryWithStatus(t *testing.T) {
	rbe := &ResponseBodyError{RulePath: "*.result.errors", Rule: "non-empty", Message: "email is not valid", Code: "E102",
		Category: "VALIDATION", Status: 400}
	assert.Equal(t, `response body error detected at "*.result.errors" (rule: non-empty): email is not valid [code: E102] [category: VALIDATION], treated as status 400`,
		rbe.Summary())
}
