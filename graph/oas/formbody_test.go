package oas

import (
	"net/http"
	"testing"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
)

// refundFormSchema returns the form body schema and encoding of the fixture's
// createRefund operation.
func refundFormSchema(t *testing.T) (*base.Schema, formEncoding) {
	t.Helper()
	model, err := LoadSpec("testdata/form_body.yaml")
	require.NoError(t, err)
	_, _, _, op, err := FindOperation(model, "createRefund")
	require.NoError(t, err)
	mediaType := op.RequestBody.Content.GetOrZero(formMediaType)
	require.NotNil(t, mediaType)
	return mediaType.Schema.Schema(), mediaType.Encoding
}

func TestDecodeFormBody(t *testing.T) {
	schema, encoding := refundFormSchema(t)

	tests := []struct {
		name    string
		body    string
		want    map[string]any
		wantErr string
	}{
		{
			name: "values take the types the schema declares",
			body: "orderId=ord-1&amount=500&partial=true&rate=1.5",
			want: map[string]any{"orderId": "ord-1", "amount": int64(500), "partial": true, "rate": 1.5},
		},
		{
			name: "a value that doesn't parse as its type stays a string",
			body: "orderId=ord-1&amount=five",
			want: map[string]any{"orderId": "ord-1", "amount": "five"},
		},
		{
			name: "escapes decode, and reserved characters are fine",
			body: "orderId=ord+1&email=a%40b.com&metadata[return]=https%3A%2F%2Fshop.example%2Fdone",
			want: map[string]any{"orderId": "ord 1", "email": "a@b.com", "metadata": map[string]any{"return": "https://shop.example/done"}},
		},
		{
			name: "bracketed keys nest",
			body: "metadata[source]=web&metadata[channel]=app",
			want: map[string]any{"metadata": map[string]any{"source": "web", "channel": "app"}},
		},
		{
			name: "indexed keys make an array of objects",
			body: "items[1][sku]=B&items[0][sku]=A&items[0][quantity]=2",
			want: map[string]any{"items": []any{map[string]any{"sku": "A", "quantity": int64(2)}, map[string]any{"sku": "B"}}},
		},
		{
			name: "a key ending in [] collects an array",
			body: "expand[]=customer&expand[]=charge",
			want: map[string]any{"expand": []any{"customer", "charge"}},
		},
		{
			name: "a repeated key collects an array",
			body: "expand=customer&expand=charge",
			want: map[string]any{"expand": []any{"customer", "charge"}},
		},
		{
			name: "explode: false splits one value on commas",
			body: "reasons=damaged,late",
			want: map[string]any{"reasons": []any{"damaged", "late"}},
		},
		{
			name: "an integer or an empty string",
			body: "note=5",
			want: map[string]any{"note": int64(5)},
		},
		{
			name: "the empty string alternative stays a string",
			body: "note=&shipping=",
			want: map[string]any{"note": "", "shipping": ""},
		},
		{
			name: "the object alternative nests",
			body: "shipping[city]=Paris",
			want: map[string]any{"shipping": map[string]any{"city": "Paris"}},
		},
		{
			name: "empty pairs and a leading & are ignored",
			body: "&orderId=ord-1&&amount=1",
			want: map[string]any{"orderId": "ord-1", "amount": int64(1)},
		},
		{
			name:    "a key that is both a value and an object",
			body:    "note=x&note[a]=y",
			wantErr: "both a value and an object",
		},
		{
			name:    "[] in the middle of a key",
			body:    "items[][sku]=A",
			wantErr: "[] must come last",
		},
		{
			name:    "a bad escape",
			body:    "orderId=%zz",
			wantErr: "form body",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeFormBody(tt.body, schema, encoding)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseFormKey(t *testing.T) {
	tests := []struct {
		key        string
		wantPath   []string
		wantAppend bool
	}{
		{"amount", []string{"amount"}, false},
		{"metadata[source]", []string{"metadata", "source"}, false},
		{"items[0][sku]", []string{"items", "0", "sku"}, false},
		{"expand[]", []string{"expand"}, true},
		{"items[0][tags][]", []string{"items", "0", "tags"}, true},
		{"odd[key", []string{"odd[key"}, false},
		{"[leading]", []string{"[leading]"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			path, appendValue, err := parseFormKey(tt.key)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPath, path)
			assert.Equal(t, tt.wantAppend, appendValue)
		})
	}
}

// TestValidateStep_FormRequestBody validates form-encoded request bodies against
// their schema, through the validator the spec cache builds.
func TestValidateStep_FormRequestBody(t *testing.T) {
	cache := NewSpecCache()
	require.NoError(t, cache.Load("refunds.yaml", "testdata/form_body.yaml"))
	node := &graph.Node{Name: "createRefund", OAS: &graph.OASRef{OperationID: "createRefund", Spec: "refunds.yaml"}}
	form := map[string]string{"Content-Type": formMediaType}
	respHeaders := http.Header{"Content-Type": []string{"application/json"}}

	tests := []struct {
		name       string
		body       string
		wantErrors bool
	}{
		{"a valid body", "orderId=ord-1&amount=500&metadata[source]=web&items[0][sku]=A&items[0][quantity]=2&expand[]=order", false},
		{"reserved characters in values", "orderId=ord-1&amount=500&email=a%40b.com&metadata[return]=https%3A%2F%2Fshop.example%2Fdone", false},
		{"a wrong type in an array of objects", "orderId=ord-1&amount=500&items[0][sku]=A&items[0][quantity]=two", true},
		{"a missing required field", "orderId=ord-1", true},
		{"a field the schema doesn't declare", "orderId=ord-1&amount=500&colour=red", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateStep(node, "", cache, "POST", "/refunds", form, []byte(tt.body), 200, respHeaders, []byte(`{"refundId": "re-1"}`))
			require.NotNil(t, result)
			require.NotNil(t, result.Request)
			assert.False(t, result.Request.Skipped, "request: %+v", result.Request)
			assert.Equal(t, tt.wantErrors, len(result.Request.Errors) > 0, "request: %+v", result.Request)
		})
	}
}
