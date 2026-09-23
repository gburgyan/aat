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
		{name: "set a query parameter", where: FieldQuery, path: "limit", op: PatchSet, value: "-1", wantPath: "/items?limit=-1&source=web"},
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
