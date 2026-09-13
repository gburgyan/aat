package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTemplate_SuppliedFieldsForm(t *testing.T) {
	tmpl := &Template{Request: TemplateRequest{
		Method:  "POST",
		Path:    "/refunds?expand[]=order&limit={{limit}}{{?page}}&page={{page}}{{/page}}",
		Headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		Body:    "orderId={{orderId}}&channel=web&metadata[source]=web{{?note}}&note={{note}}{{/note}}{{#tags}}&tags[]={{.}}{{/tags}}\n",
	}}

	fields := tmpl.SuppliedFields()
	for _, name := range []string{"expand", "limit", "Content-Type", "orderId", "channel", "metadata"} {
		assert.True(t, fields[name], "%s is supplied", name)
	}
	for _, name := range []string{"page", "note", "tags", "expand[]", "metadata[source]"} {
		assert.False(t, fields[name], "%s is not supplied", name)
	}
}
