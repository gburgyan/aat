package mcp

import (
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatTemplate_Form(t *testing.T) {
	tmpl, err := adapter.ParseTemplate([]byte(`adapter: createRefund
request:
  method: POST
  path: /refunds
  headers:
    Idempotency-Key: "{{requestKey}}"
  form:
    orderId: "{{orderId}}"
    metadata:
      source: aat
`))
	require.NoError(t, err)

	text := formatTemplate(tmpl)
	assert.Contains(t, text, "## Form")
	assert.Contains(t, text, "orderId: '{{orderId}}'\nmetadata:\n  source: aat\n")
	assert.NotContains(t, text, "## Body")
	assert.Contains(t, text, "**Conditional:** orderId, requestKey")
}
