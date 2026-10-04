package engine

import (
	"encoding/json"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/validate"
	"github.com/tidwall/gjson"
)

// ResponseBodyError captures a detected error in a 2xx response body. Its
// fields are archive.ResponseBodyErrorRecord's, in the same order.
type ResponseBodyError struct {
	RulePath string // the gjson path that triggered the rule
	Rule     string // the rule type: "exists", "non-empty", "equals"
	Message  string // extracted from details.message path
	Code     string // extracted from details.code path
	Category string // extracted from details.category path
	// Status is the HTTP status the error stands for, from its rule or the
	// graph's errorStatus; 0 when neither gives one.
	Status int
}

// Summary returns a human-readable summary of the detected error.
func (e *ResponseBodyError) Summary() string {
	return archive.ResponseBodyErrorRecord(*e).Summary()
}

// FailureStatus returns the status a step's response stands for, and its
// gRPC status name: the status a body error in it was given, which has no
// name, or else the response's own status and name ("" for HTTP). Retries,
// expectFailure, status assertions, and fuzz judging read it.
func (r *StepResult) FailureStatus() (int, string) {
	if r.ResponseBodyError != nil && r.ResponseBodyError.Status != 0 {
		return r.ResponseBodyError.Status, ""
	}
	return r.StatusCode, grpcStatusName(r.Response)
}

// detectBodyError looks for an error a successful response's body reports, by
// node's effective rules, and gives one whose rule names no status for it the
// status the graph's errorStatus gives its category.
func (e *Engine) detectBodyError(node *graph.Node, body []byte) *ResponseBodyError {
	rbe := CheckErrorDetection(effectiveErrorRules(node, e.graph), body)
	if rbe != nil && rbe.Status == 0 && e.graph.ErrorStatus != nil {
		rbe.Status = e.graph.ErrorStatus.StatusFor(rbe.Category)
	}
	return rbe
}

// CheckErrorDetection evaluates error detection rules against a response body.
// Returns the first matching rule as a ResponseBodyError, or nil if no rules trigger.
// Non-JSON bodies return nil (graceful degradation). The error gets the status
// its rule gives its category; the graph's errorStatus is applied by the
// engine (see Engine.detectBodyError).
func CheckErrorDetection(rules []graph.ErrorDetectionRule, body []byte) *ResponseBodyError {
	if len(rules) == 0 || len(body) == 0 {
		return nil
	}

	// Verify the body is valid JSON
	if !json.Valid(body) {
		return nil
	}

	for _, rule := range rules {
		if ruleTriggered(rule, body) {
			rbe := &ResponseBodyError{
				RulePath: rule.Path,
				Rule:     rule.Rule,
			}
			if rule.Details != nil {
				if rule.Details.Message != "" {
					rbe.Message = gjson.GetBytes(body, rule.Details.Message).String()
				}
				if rule.Details.Code != "" {
					rbe.Code = gjson.GetBytes(body, rule.Details.Code).String()
				}
				if rule.Details.Category != "" {
					rbe.Category = gjson.GetBytes(body, rule.Details.Category).String()
				}
			}
			rbe.Status = rule.ErrorStatus.StatusFor(rbe.Category)
			return rbe
		}
	}

	return nil
}

// ruleTriggered checks whether a single error detection rule matches the body.
func ruleTriggered(rule graph.ErrorDetectionRule, body []byte) bool {
	result := gjson.GetBytes(body, rule.Path)

	switch rule.Rule {
	case "exists":
		return result.Exists() && result.Type != gjson.Null

	case "non-empty":
		if !result.Exists() || result.Type == gjson.Null {
			return false
		}
		switch {
		case result.IsArray():
			return len(result.Array()) > 0
		case result.IsObject():
			return result.Raw != "{}"
		case result.Type == gjson.String:
			return result.String() != ""
		default:
			// For numbers, bools, etc. — existence is enough
			return true
		}

	case "equals":
		if !result.Exists() {
			return false
		}
		// Numbers compare by value whatever their Go type, so a YAML integer
		// such as 0 matches a JSON 0; strings and booleans compare strictly.
		return validate.ValuesEqual(result, rule.Value)

	default:
		return false
	}
}

// effectiveErrorRules returns the error detection rules to apply for a given node.
// Node-level rules override graph-level (no merge) for simple, predictable semantics.
func effectiveErrorRules(node *graph.Node, g *graph.Graph) []graph.ErrorDetectionRule {
	if len(node.ErrorDetection) > 0 {
		return node.ErrorDetection
	}
	return g.ErrorDetection
}
