package adapter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// formTemplatePrefix is the template a form test's fields follow; the first
// field is on line 6.
const formTemplatePrefix = "adapter: charge\nrequest:\n  method: POST\n  path: /charges\n  form:\n"

// formTemplate parses a template whose request.form holds the given fields,
// indented by four spaces.
func formTemplate(t *testing.T, fields string) Template {
	t.Helper()
	tmpl, err := ParseTemplate([]byte(formTemplatePrefix + fields))
	require.NoError(t, err)
	return *tmpl
}

// buildForm builds a request from tmpl with inputs and no environment.
func buildForm(t *testing.T, tmpl Template, inputs map[string]any) *Request {
	t.Helper()
	req, err := NewTemplateAdapter(tmpl).BuildRequest(inputs, nil)
	require.NoError(t, err)
	return req
}

func TestBuildRequest_Form(t *testing.T) {
	tmpl := formTemplate(t, `    amount: "{{amount}}"
    currency: usd
    payment_method_types[]: card
    customer: "{{customer}}"
    description: "Order {{orderId}}"
    metadata:
      created_by: aat
      note: "{{note}}"
    expand[]: [customer, latest_charge]
    items:
      - sku: "{{sku}}"
        quantity: 1
    "first name": Ada
    empty: ""
`)
	req := buildForm(t, tmpl, map[string]any{"amount": 2000, "orderId": "o 1", "note": "a&b=c", "sku": "s1"})

	assert.Equal(t, "amount=2000&currency=usd&payment_method_types[]=card&description=Order+o+1"+
		"&metadata[created_by]=aat&metadata[note]=a%26b%3Dc&expand[]=customer&expand[]=latest_charge"+
		"&items[0][sku]=s1&items[0][quantity]=1&first+name=Ada&empty=", string(req.Body))
	assert.Equal(t, map[string]string{"Content-Type": FormContentType}, req.Headers)
}

func TestBuildRequest_FormInputValues(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value any
		want  string
	}{
		{"an absent input is left out", `value: "{{other}}"`, nil, ""},
		{"nil is left out", `value: "{{v}}"`, nil, ""},
		{"an empty string is left out", `value: "{{v}}"`, "", ""},
		{"an empty list is left out", `value: "{{v}}"`, []any{}, ""},
		{"an empty map is left out", `value: "{{v}}"`, map[string]any{}, ""},
		{"a number", `value: "{{v}}"`, 7, "value=7"},
		{"a boolean", `value: "{{v}}"`, true, "value=true"},
		{"a decimal", `value: "{{v}}"`, json.Number("19.99"), "value=19.99"},
		{"a list repeats a bracketed key", `tags[]: "{{v}}"`, []any{"a", "b c"}, "tags[]=a&tags[]=b+c"},
		{"a list repeats a plain key", `tags: "{{v}}"`, []string{"a", "b"}, "tags=a&tags=b"},
		{"a map writes bracketed keys in order", `metadata: "{{v}}"`, map[string]any{"b": "2", "a": "x y"}, "metadata[a]=x+y&metadata[b]=2"},
		{"a nested map", `address: "{{v}}"`, map[string]any{"shipping": map[string]any{"city": "Austin"}}, "address[shipping][city]=Austin"},
		{"a list of maps writes indexed keys", `items[]: "{{v}}"`, []any{map[string]any{"sku": "s1"}, map[string]any{"sku": "s2"}}, "items[0][sku]=s1&items[1][sku]=s2"},
		{"a map's nil value is left out", `metadata: "{{v}}"`, map[string]any{"a": nil, "b": 1}, "metadata[b]=1"},
		{"a map's empty string is sent", `metadata: "{{v}}"`, map[string]any{"a": ""}, "metadata[a]="},
		{"a key inside a map is escaped", `metadata: "{{v}}"`, map[string]any{"a&b": "c"}, "metadata[a%26b]=c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl := formTemplate(t, "    "+tt.field+"\n")
			req := buildForm(t, tmpl, map[string]any{"v": tt.value})
			assert.Equal(t, tt.want, string(req.Body))
		})
	}
}

func TestBuildRequest_FormText(t *testing.T) {
	tmpl := formTemplate(t, `    note: "{{?reason}}because {{reason}}{{/reason}}"
    description: "Order {{orderId}}"
`)

	t.Run("a conditional block that renders nothing leaves the field out", func(t *testing.T) {
		req := buildForm(t, tmpl, map[string]any{"orderId": "o1"})
		assert.Equal(t, "description=Order+o1", string(req.Body))
	})
	t.Run("a conditional block with a value", func(t *testing.T) {
		req := buildForm(t, tmpl, map[string]any{"orderId": "o1", "reason": "late"})
		assert.Equal(t, "note=because+late&description=Order+o1", string(req.Body))
	})
	t.Run("a placeholder inside text still needs a value", func(t *testing.T) {
		_, err := NewTemplateAdapter(tmpl).BuildRequest(map[string]any{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `form field "description": unresolved placeholders: orderId`)
	})
}

func TestBuildRequest_FormContentType(t *testing.T) {
	fields := "    orderId: \"{{orderId}}\"\n"
	inputs := map[string]any{"orderId": "o1"}

	t.Run("replaces an environment Content-Type", func(t *testing.T) {
		tmpl := formTemplate(t, fields)
		req, err := NewTemplateAdapter(tmpl).BuildRequest(inputs, &EnvironmentConfig{
			Headers: map[string]string{"content-type": "application/json", "Accept": "application/json"},
		})
		require.NoError(t, err)
		contentType, _ := headerValue(req.Headers, "Content-Type")
		assert.Equal(t, FormContentType, contentType)
		assert.Equal(t, "orderId=o1", string(req.Body))
	})

	t.Run("keeps the template's charset", func(t *testing.T) {
		tmpl, err := ParseTemplate([]byte("adapter: charge\nrequest:\n  method: POST\n  path: /charges\n" +
			"  headers:\n    Content-Type: application/x-www-form-urlencoded; charset=utf-8\n  form:\n" + fields))
		require.NoError(t, err)
		req := buildForm(t, *tmpl, inputs)
		contentType, _ := headerValue(req.Headers, "Content-Type")
		assert.Equal(t, "application/x-www-form-urlencoded; charset=utf-8", contentType)
	})

	t.Run("a protected Content-Type that isn't a form is an error", func(t *testing.T) {
		tmpl := formTemplate(t, fields)
		_, err := NewTemplateAdapter(tmpl).BuildRequest(inputs, &EnvironmentConfig{
			Protected: map[string]string{"Content-Type": "application/json"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `request.form sends application/x-www-form-urlencoded, but a credential or overlay header sets Content-Type "application/json"`)
	})

	t.Run("every field left out sends an empty body", func(t *testing.T) {
		tmpl := formTemplate(t, fields)
		req := buildForm(t, tmpl, map[string]any{})
		assert.Empty(t, req.Body)
		contentType, _ := headerValue(req.Headers, "Content-Type")
		assert.Equal(t, FormContentType, contentType)
	})
}

func TestParseTemplate_FormErrors(t *testing.T) {
	tests := []struct {
		name   string
		fields string
		want   string
	}{
		{"a duplicate field", "    a: x\n    a: y\n", `line 7: duplicate form field "a" (first at line 6)`},
		{"nesting that sends a key twice", "    metadata[source]: a\n    metadata:\n      source: b\n", `line 8: form field "metadata[source]" is also written at line 6`},
		{"a field without a value", "    customer:\n", `form field "customer" has no value`},
		{"an alias", "    a: &v x\n    b: *v\n", `form field "b": aliases aren't allowed`},
		{"a merge key", "    <<: {a: x}\n", "merge keys (<<) aren't allowed in a form"},
		{"a placeholder in a name", "    \"{{name}}\": x\n", "a field name can't hold a placeholder"},
		{"an iteration block", "    tags: \"{{#tags}}{{.}}{{/tags}}\"\n", `form field "tags": iteration blocks aren't allowed in a form; write tags: "{{tags}}"`},
		{"an element placeholder", "    tag: \"{{.}}\"\n", "works only inside an iteration block"},
		{"an unclosed conditional block", "    note: \"{{?reason}}x\"\n", `form field "note": unclosed conditional block: {{?reason}}`},
		{"a list in a list", "    a:\n      - [x]\n", "a list item must be a value or a mapping, not a list"},
		{"a form that isn't a mapping", "    - a\n", "line 6: request.form must be a mapping of field names to values, found a list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTemplate([]byte(formTemplatePrefix + tt.fields))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}

	t.Run("a body and a form", func(t *testing.T) {
		_, err := ParseTemplate([]byte(formTemplatePrefix + "    a: x\n  body: 'a=x'\n"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "both request.body and request.form")
	})

	t.Run("a Content-Type that isn't a form", func(t *testing.T) {
		_, err := ParseTemplate([]byte("adapter: charge\nrequest:\n  method: POST\n  path: /charges\n" +
			"  headers:\n    Content-Type: application/json\n  form:\n    a: x\n"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `request.form is sent as application/x-www-form-urlencoded, but the template's Content-Type header is "application/json"`)
	})
}

func TestBuildRequest_WholePlaceholderHeader(t *testing.T) {
	tmpl := Template{Request: TemplateRequest{
		Method: "POST",
		Path:   "/orders",
		Headers: map[string]string{
			"Idempotency-Key": "{{requestKey}}",
			"X-Trace":         "trace-{{traceId}}",
		},
	}}

	t.Run("not sent when the input has no value", func(t *testing.T) {
		for _, inputs := range []map[string]any{{"traceId": "t1"}, {"traceId": "t1", "requestKey": ""}, {"traceId": "t1", "requestKey": nil}} {
			req := buildForm(t, tmpl, inputs)
			assert.Equal(t, map[string]string{"X-Trace": "trace-t1"}, req.Headers)
		}
	})
	t.Run("sent when the input has a value", func(t *testing.T) {
		req := buildForm(t, tmpl, map[string]any{"traceId": "t1", "requestKey": "k1"})
		assert.Equal(t, "k1", req.Headers["Idempotency-Key"])
	})
	t.Run("an environment header of the same name stays", func(t *testing.T) {
		req, err := NewTemplateAdapter(tmpl).BuildRequest(map[string]any{"traceId": "t1"}, &EnvironmentConfig{
			Headers: map[string]string{"Idempotency-Key": "from-env"},
		})
		require.NoError(t, err)
		assert.Equal(t, "from-env", req.Headers["Idempotency-Key"])
	})
	t.Run("a placeholder inside other text still needs a value", func(t *testing.T) {
		_, err := NewTemplateAdapter(tmpl).BuildRequest(map[string]any{"requestKey": "k1"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `header "X-Trace" substitution: unresolved placeholders: traceId`)
	})
}

func TestTemplate_FormStaticChecks(t *testing.T) {
	tmpl := formTemplate(t, `    amount: "{{amount}}"
    currency: usd
    customer: "{{customerId}}"
    note: "{{?reason}}{{reason}}{{/reason}}"
    description: "Order {{orderId}}"
    metadata:
      source: "{{source}}"
    payment_method_types[]: "{{paymentMethodTypes}}"
    items:
      - sku: "{{sku}}"
      - sku: fixed
`)
	tmpl.Request.Headers = map[string]string{"Idempotency-Key": "{{requestKey}}", "X-Order": "{{orderId}}"}

	t.Run("SuppliedFields counts fields always sent", func(t *testing.T) {
		fields := tmpl.SuppliedFields()
		for _, name := range []string{"currency", "description", "items", "Idempotency-Key", "X-Order"} {
			assert.True(t, fields[name], "%s is supplied", name)
		}
		for _, name := range []string{"amount", "customer", "note", "metadata", "payment_method_types"} {
			assert.False(t, fields[name], "%s is not supplied", name)
		}
	})

	t.Run("FormInputFields names the field each whole-value input fills", func(t *testing.T) {
		assert.Equal(t, map[string][]string{
			"amount":             {"amount"},
			"customerId":         {"customer"},
			"source":             {"metadata"},
			"paymentMethodTypes": {"payment_method_types"},
			"sku":                {"items"},
		}, tmpl.FormInputFields())
	})

	t.Run("HeaderOnlyInputs looks at the form", func(t *testing.T) {
		assert.Equal(t, map[string]bool{"requestKey": true}, tmpl.HeaderOnlyInputs())
	})

	t.Run("ClassifyInputs treats whole values as conditional", func(t *testing.T) {
		required, conditional, iterable := ClassifyInputs(&tmpl)
		assert.Equal(t, []string{"orderId"}, required)
		assert.Equal(t, []string{"amount", "customerId", "paymentMethodTypes", "reason", "requestKey", "sku", "source"}, conditional)
		assert.Empty(t, iterable)
	})

	t.Run("YAML writes the mapping back in order", func(t *testing.T) {
		assert.Equal(t, `amount: '{{amount}}'
currency: usd
customer: '{{customerId}}'
note: '{{?reason}}{{reason}}{{/reason}}'
description: Order {{orderId}}
metadata:
  source: '{{source}}'
payment_method_types[]: '{{paymentMethodTypes}}'
items:
  - sku: '{{sku}}'
  - sku: fixed
`, tmpl.Request.Form.YAML())
	})
}
