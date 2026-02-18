package engine

import (
	"encoding/json"
	"fmt"

	"github.com/gburgyan/aat/graph"
	"github.com/tidwall/gjson"
)

// ResponseBodyError captures a detected error in a 2xx response body.
type ResponseBodyError struct {
	RulePath string // the gjson path that triggered the rule
	Rule     string // the rule type: "exists", "non-empty", "equals"
	Message  string // extracted from details.message path
	Code     string // extracted from details.code path
	Category string // extracted from details.category path
}

// Summary returns a human-readable summary of the detected error.
func (e *ResponseBodyError) Summary() string {
	parts := fmt.Sprintf("response body error detected at %q (rule: %s)", e.RulePath, e.Rule)
	if e.Message != "" {
		parts += fmt.Sprintf(": %s", e.Message)
	}
	if e.Code != "" {
		parts += fmt.Sprintf(" [code: %s]", e.Code)
	}
	return parts
}

// CheckErrorDetection evaluates error detection rules against a response body.
// Returns the first matching rule as a ResponseBodyError, or nil if no rules trigger.
// Non-JSON bodies return nil (graceful degradation).
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
		return result.Value() == rule.Value

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
