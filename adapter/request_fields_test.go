package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplate_RequestFields(t *testing.T) {
	tmpl := &Template{Protocol: "http", Request: TemplateRequest{
		Method:  "POST",
		Path:    "/carts/{{cartId}}/items?source=web&limit={{limit}}",
		Headers: map[string]string{"Content-Type": "application/json", "X-Request-Id": "{{requestId}}"},
		Body: `{
  "sku": "{{sku}}",
  "quantity": {{quantity}},
  "note": "gift {{name}}",
  "channel": "web",
  "rush": false,
  "count": 3,
  "shipping": {"country": "US", "lines": [1, 2]},
  {{?coupon}}"coupon": "{{coupon}}",{{/coupon}}
  "tags": null
}`,
	}}
	fields, ok := tmpl.RequestFields()
	require.True(t, ok)

	want := []RequestField{
		{Where: FieldBody, Path: "sku", Input: "sku"},
		{Where: FieldBody, Path: "quantity", Input: "quantity"},
		{Where: FieldBody, Path: "channel", Kind: "string"},
		{Where: FieldBody, Path: "rush", Kind: "boolean"},
		{Where: FieldBody, Path: "count", Kind: "number"},
		{Where: FieldBody, Path: "shipping", Kind: "object"},
		{Where: FieldBody, Path: "shipping.country", Kind: "string"},
		{Where: FieldBody, Path: "shipping.lines", Kind: "array"},
		{Where: FieldBody, Path: "shipping.lines.0", Kind: "number"},
		{Where: FieldBody, Path: "shipping.lines.1", Kind: "number"},
		{Where: FieldBody, Path: "coupon", Input: "coupon", InBlock: true},
		{Where: FieldBody, Path: "tags", Kind: "null"},
		{Where: FieldQuery, Path: "source", Kind: "string"},
		{Where: FieldQuery, Path: "limit", Input: "limit"},
		{Where: FieldHeader, Path: "X-Request-Id", Input: "requestId"},
	}
	assert.Equal(t, want, fields, "a placeholder inside a longer string (note) is not a field of its own; a path segment is not a field")
}

// TestTemplate_RequestFields_DynamicKeys checks that a value under a key an
// input picks is not a field: there is no path to patch it at.
func TestTemplate_RequestFields_DynamicKeys(t *testing.T) {
	tmpl := &Template{Protocol: "http", Request: TemplateRequest{Method: "POST", Path: "/x",
		Body: `{"metadata": {"{{k}}": {"source": "x"}}, "{{other}}": "v", "kept": 1}`}}
	fields, ok := tmpl.RequestFields()
	require.True(t, ok)
	assert.Equal(t, []RequestField{
		{Where: FieldBody, Path: "metadata", Kind: "object"},
		{Where: FieldBody, Path: "kept", Kind: "number"},
	}, fields)
}

// TestTemplate_DropsEmpty checks which inputs the builder leaves out when
// they are empty.
func TestTemplate_DropsEmpty(t *testing.T) {
	tmpl := &Template{Protocol: "http", Request: TemplateRequest{
		Method:  "POST",
		Path:    "/products{{?category}}?category={{ category }}{{/category}}{{?a|b}}&a={{a}}{{/a|b}}",
		Headers: map[string]string{"X-Hint": "{{hint}}", "Authorization": "Bearer {{token}}"},
		Body:    `{"name": "{{name}}", {{?note}}"note": "{{note}}",{{/note}} "x": 1}`,
		Form:    nil,
	}}
	for input, want := range map[string]bool{
		"category": true,  // in its own block, even with spaces in the placeholder
		"note":     true,  // in its own block in the body
		"hint":     true,  // the whole value of a header
		"a":        false, // its block is sent when b has a value
		"token":    false, // part of a header's value
		"name":     false, // sent as "" in the body
		"unused":   false,
	} {
		assert.Equal(t, want, tmpl.DropsEmpty(input), input)
	}

	form := &Template{Protocol: "http", Request: TemplateRequest{Method: "POST", Path: "/x",
		Form: FormFields{{Key: "email", Value: "{{email}}"}, {Key: "label", Value: "user {{label}}"}}}}
	assert.True(t, form.DropsEmpty("email"))
	assert.False(t, form.DropsEmpty("label"))
}

// TestTemplate_HeaderRefuses checks which values a header the template fills
// from an input can't carry.
func TestTemplate_HeaderRefuses(t *testing.T) {
	tmpl := &Template{Protocol: "http", Request: TemplateRequest{Method: "GET", Path: "/x",
		Headers: map[string]string{"X-Hint": "v={{hint}}"}}}
	assert.True(t, tmpl.HeaderRefuses("hint", "a\x00b"))
	assert.False(t, tmpl.HeaderRefuses("hint", "Zoë"))
	assert.False(t, tmpl.HeaderRefuses("other", "a\x00b"), "no header sends it")

	grpc := &Template{Protocol: ProtocolGRPC, Request: TemplateRequest{
		Metadata: map[string]string{"x-hint": "{{hint}}", "x-blob-bin": "{{blob}}"}}}
	assert.True(t, grpc.HeaderRefuses("hint", "Zoë"))
	assert.False(t, grpc.HeaderRefuses("blob", "Zoë"), "a -bin value is bytes")
}

func TestTemplate_RequestFields_NotJSON(t *testing.T) {
	fields, ok := (&Template{Request: TemplateRequest{Body: `name={{name}}`, Path: "/x?a={{a}}"}}).RequestFields()
	assert.False(t, ok)
	assert.Equal(t, []RequestField{{Where: FieldQuery, Path: "a", Input: "a"}}, fields, "the query is still listed")
}

func TestTemplate_RequestFields_GRPC(t *testing.T) {
	tmpl := &Template{Protocol: "grpc", Request: TemplateRequest{
		RPC:      "shop.v1.Payments/Charge",
		Message:  `{"orderId": "{{orderId}}", "amount": {"units": {{amount}}, "currency": "USD"}}`,
		Metadata: map[string]string{"x-key": "{{apiKey}}"},
	}}
	fields, ok := tmpl.RequestFields()
	require.True(t, ok)
	assert.Equal(t, []RequestField{
		{Where: FieldBody, Path: "orderId", Input: "orderId"},
		{Where: FieldBody, Path: "amount", Kind: "object"},
		{Where: FieldBody, Path: "amount.units", Input: "amount"},
		{Where: FieldBody, Path: "amount.currency", Kind: "string"},
		{Where: FieldHeader, Path: "x-key", Input: "apiKey"},
	}, fields)
}

func TestApplyPatch(t *testing.T) {
	base := func() *Request {
		return &Request{
			Method:  "POST",
			Path:    "/items?source=web&limit=5",
			Headers: map[string]string{"X-Request-Id": "r1"},
			Body:    []byte(`{"sku":"SKU-1","quantity":2,"shipping":{"country":"US","lines":[1,2,3]}}`),
		}
	}
	tests := []struct {
		name            string
		where, path, op string
		value           any
		wantBody        string
		wantPath        string
		wantHeaders     map[string]string
		wantErr         string
	}{
		{name: "remove a body field", where: FieldBody, path: "quantity", op: PatchRemove,
			wantBody: `{"shipping":{"country":"US","lines":[1,2,3]},"sku":"SKU-1"}`},
		{name: "null a nested field", where: FieldBody, path: "shipping.country", op: PatchSet, value: nil,
			wantBody: `{"quantity":2,"shipping":{"country":null,"lines":[1,2,3]},"sku":"SKU-1"}`},
		{name: "remove an array element", where: FieldBody, path: "shipping.lines.1", op: PatchRemove,
			wantBody: `{"quantity":2,"shipping":{"country":"US","lines":[1,3]},"sku":"SKU-1"}`},
		{name: "add a property", where: FieldBody, path: "aatFuzz", op: PatchSet, value: "x",
			wantBody: `{"aatFuzz":"x","quantity":2,"shipping":{"country":"US","lines":[1,2,3]},"sku":"SKU-1"}`},
		{name: "remove a missing field", where: FieldBody, path: "coupon", op: PatchRemove, wantErr: `nothing at "coupon"`},
		{name: "path through nothing", where: FieldBody, path: "billing.zip", op: PatchSet, wantErr: `nothing at "billing.zip"`},
		{name: "remove a query parameter", where: FieldQuery, path: "limit", op: PatchRemove, wantPath: "/items?source=web"},
		{name: "set a query parameter", where: FieldQuery, path: "limit", op: PatchSet, value: "-1", wantPath: "/items?source=web&limit=-1"},
		{name: "remove a header", where: FieldHeader, path: "x-request-id", op: PatchRemove, wantHeaders: map[string]string{}},
		{name: "remove a missing header", where: FieldHeader, path: "X-Trace", op: PatchRemove, wantErr: `no header "X-Trace"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base()
			err := ApplyPatch(req, tt.where, tt.path, tt.op, tt.value)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.wantBody != "" {
				assert.JSONEq(t, tt.wantBody, string(req.Body))
			}
			if tt.wantPath != "" {
				assert.Equal(t, tt.wantPath, req.Path)
			}
			if tt.wantHeaders != nil {
				assert.Equal(t, tt.wantHeaders, req.Headers)
			}
		})
	}

	req := &Request{Body: []byte("name=x")}
	assert.ErrorContains(t, ApplyPatch(req, FieldBody, "name", PatchRemove, nil), "not JSON")
}

// TestApplyPatch_QueryKeepsTheRest checks that a query patch changes its own
// parameter and leaves the rest of the query as the template wrote it.
func TestApplyPatch_QueryKeepsTheRest(t *testing.T) {
	for _, tt := range []struct {
		name, path, param, op string
		value                 any
		want                  string
	}{
		{"a semicolon elsewhere", "/x?q=a;b&limit=5", "limit", PatchRemove, nil, "/x?q=a;b"},
		{"a flag", "/x?verbose&limit=5", "limit", PatchSet, 7, "/x?verbose&limit=7"},
		{"an encoded name", "/x?filter%5Bstatus%5D=open&page=2", "filter[status]", PatchRemove, nil, "/x?page=2"},
		{"an encoded name in the patch", "/x?filter[status]=open", "filter%5Bstatus%5D", PatchSet, "a b", "/x?filter[status]=a+b"},
		{"a new parameter", "/x", "limit", PatchSet, -1, "/x?limit=-1"},
		{"a repeated parameter", "/x?tag=a&tag=b&n=1", "tag", PatchSet, "c", "/x?tag=c&n=1"},
		{"the last parameter", "/x?limit=5", "limit", PatchRemove, nil, "/x"},
		{"null", "/x?limit=5", "limit", PatchSet, nil, "/x?limit="},
	} {
		req := &Request{Path: tt.path}
		require.NoError(t, ApplyPatch(req, FieldQuery, tt.param, tt.op, tt.value), tt.name)
		assert.Equal(t, tt.want, req.Path, tt.name)
	}
}

// TestApplyPatch_HeaderHasOneSpelling checks that setting a header replaces
// it whatever its case, so the request sends one value, not a random one.
func TestApplyPatch_HeaderHasOneSpelling(t *testing.T) {
	req := &Request{Headers: map[string]string{"Authorization": "Bearer good"}}
	require.NoError(t, ApplyPatch(req, FieldHeader, "authorization", PatchSet, "Bearer bad"))
	assert.Equal(t, map[string]string{"authorization": "Bearer bad"}, req.Headers)
	require.NoError(t, ApplyPatch(req, FieldHeader, "X-Count", PatchSet, 1e22))
	assert.Equal(t, "10000000000000000000000", req.Headers["X-Count"])
}

// TestTemplate_RequestFields_Spaces checks that a placeholder written with
// spaces inside, {{ name }}, is the whole value as {{name}} is.
func TestTemplate_RequestFields_Spaces(t *testing.T) {
	tmpl := &Template{Protocol: "http", Request: TemplateRequest{Method: "POST", Path: "/x?limit={{ limit }}",
		Headers: map[string]string{"X-Hint": "{{ hint }}"},
		Body:    `{"name": "{{ name }}", "quantity": {{ quantity }}}`}}
	fields, ok := tmpl.RequestFields()
	require.True(t, ok)
	assert.Equal(t, []RequestField{
		{Where: FieldBody, Path: "name", Input: "name"},
		{Where: FieldBody, Path: "quantity", Input: "quantity"},
		{Where: FieldQuery, Path: "limit", Input: "limit"},
		{Where: FieldHeader, Path: "X-Hint", Input: "hint"},
	}, fields)
}

// TestTemplate_RequestFields_ElementsAfterABlock checks that an array element
// after a conditional or iteration block counts as being in one: the block
// may send any number of elements, so its index isn't known.
func TestTemplate_RequestFields_ElementsAfterABlock(t *testing.T) {
	tmpl := &Template{Protocol: "http", Request: TemplateRequest{Method: "POST", Path: "/x",
		Body: `{"items": [{"sku": "{{first}}"}, {{?primary}}{"sku": "{{primary}}"},{{/primary}} {"sku": "{{c}}"}],
"extras": [{{#more}}{"sku": "{{.}}"}{{/more}}, {"sku": "{{d}}"}], "after": "{{e}}"}`}}
	fields, ok := tmpl.RequestFields()
	require.True(t, ok)
	inBlock := map[string]bool{}
	for _, f := range fields {
		if f.Input != "" {
			inBlock[f.Input] = f.InBlock
		}
	}
	assert.Equal(t, map[string]bool{"first": false, "primary": true, "c": true, "d": true, "e": false}, inBlock)
}

// TestTemplate_RequestFields_QueryBlocks checks the query of paths with
// blocks: a "?" inside a block starts the query, and only the pairs a block
// holds are in one.
func TestTemplate_RequestFields_QueryBlocks(t *testing.T) {
	query := func(path string) []RequestField {
		tmpl := &Template{Request: TemplateRequest{Method: "GET", Path: path}}
		fields, _ := tmpl.RequestFields()
		return fields
	}
	assert.Equal(t, []RequestField{{Where: FieldQuery, Path: "category", Input: "category", InBlock: true}},
		query("/products{{?category}}?category={{category}}{{/category}}"))
	assert.Equal(t, []RequestField{
		{Where: FieldQuery, Path: "offer_request_id", Input: "offerRequestId"},
		{Where: FieldQuery, Path: "sort", Input: "sort"},
		{Where: FieldQuery, Path: "limit", Input: "limit"},
		{Where: FieldQuery, Path: "after", Input: "after", InBlock: true},
	}, query("/air/offers?offer_request_id={{offerRequestId}}&sort={{sort}}&limit={{limit}}{{?after}}&after={{after}}{{/after}}"))
	assert.Equal(t, []RequestField{{Where: FieldQuery, Path: "v", Kind: "string"}}, query("/x/{{id}}?v=2"))
}
