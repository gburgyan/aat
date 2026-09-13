package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTemplate_HeaderOnlyInputs(t *testing.T) {
	tmpl := &Template{Request: TemplateRequest{
		Method: "POST",
		Path:   "/orders/{{orderId}}/refunds?channel={{channel}}",
		Headers: map[string]string{
			"Content-Type":    "application/json",
			"Idempotency-Key": "{{?requestKey}}{{requestKey}}{{/requestKey}}",
			"X-Trace":         "{{traceId}}-{{channel}}",
			"X-Tags":          "{{#tags}}{{.}}{{/tags}}",
			"X-Region":        "{{?region|zone}}{{region}}{{zone}}{{/region|zone}}",
			"X-Amount":        "{{amount}}",
		},
		Body: `{ "amount": {{amount}}{{?note}}, "note": "{{note}}"{{/note}} }`,
	}}

	assert.Equal(t, map[string]bool{
		"requestKey": true,
		"traceId":    true,
		"tags":       true,
		"region":     true,
		"zone":       true,
	}, tmpl.HeaderOnlyInputs())
}

func TestTemplate_HeaderOnlyInputsNone(t *testing.T) {
	tmpl := &Template{Request: TemplateRequest{
		Method:  "GET",
		Path:    "/orders?limit={{limit}}",
		Headers: map[string]string{"Accept": "application/json", "X-Limit": "{{limit}}"},
	}}

	assert.Empty(t, tmpl.HeaderOnlyInputs())
}
