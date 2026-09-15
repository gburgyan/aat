package adapter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTemplate_ExtractDefaults(t *testing.T) {
	tmpl, err := ParseTemplate([]byte(`
adapter: listOrders
request: {method: GET, path: /orders}
response:
  extract:
    nextCursor: {path: meta.after, default: ""}
    orders: {path: orders, fields: {id: id}, default: []}
`))
	require.NoError(t, err)
	assert.Equal(t, "", tmpl.Response.Extract["nextCursor"].Default)
	assert.Equal(t, []any{}, tmpl.Response.Extract["orders"].Default)

	tests := []struct {
		name    string
		extract string
		want    string
	}{
		{
			name:    "default and optional",
			extract: `nextCursor: {path: meta.after, default: "", optional: true}`,
			want:    `line 5: an extract rule takes optional or default, not both`,
		},
		{
			name:    "fields with a single default",
			extract: `orders: {path: orders, fields: {id: id}, default: none}`,
			want:    `line 5: an extract rule with fields takes a list default, such as []`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTemplate([]byte("adapter: listOrders\nrequest: {method: GET, path: /orders}\nresponse:\n  extract:\n    " + tt.extract + "\n"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestTemplateAdapter_ExtractOutputs_Defaults(t *testing.T) {
	a := NewTemplateAdapter(Template{Response: TemplateResponse{Extract: map[string]ExtractRule{
		"nextCursor":   {Path: "meta.after", Default: ""},
		"lineCount":    {Path: "lines.#", Default: 0},
		"lines":        {Path: "lines", Fields: map[string]string{"sku": "sku"}, Default: []any{}},
		"status":       {Path: "status", Default: "unknown"},
		"trackingNote": {Path: "shipment.note", Optional: true},
	}}})

	outputs, err := a.ExtractOutputs(&Response{StatusCode: 200, Body: []byte(`{"meta": {"after": null}, "status": "open"}`)})

	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"nextCursor": "",      // the path holds null
		"lineCount":  0,       // a count of a missing array
		"lines":      []any{}, // a missing array with fields
		"status":     "open",  // a value in the response wins
	}, outputs, "an optional rule whose path is missing leaves its output out")
}

func TestTemplateAdapter_ExtractOutputs_MissingPathNamesRemedies(t *testing.T) {
	a := NewTemplateAdapter(Template{Response: TemplateResponse{Extract: map[string]ExtractRule{"orderId": {Path: "id"}}}})

	_, err := a.ExtractOutputs(&Response{StatusCode: 200, Body: []byte(`{}`)})

	assert.EqualError(t, err, `extract path "orderId" (id) not found in response; mark the rule optional: true or give it a default`)
}

// TestTemplateAdapter_ExtractOutputs_CountsAndQueries pins the gjson forms the
// template docs teach, including the null match that doesn't work.
func TestTemplateAdapter_ExtractOutputs_CountsAndQueries(t *testing.T) {
	a := NewTemplateAdapter(Template{Response: TemplateResponse{Extract: map[string]ExtractRule{
		"orderCount":       {Path: "orders.#"},
		"openOrderIds":     {Path: `orders.#(status=="open")#.id`},
		"firstOpenOrderId": {Path: `orders.#(status=="open").id`},
		"openOrderCount":   {Path: `orders.#(status=="open")#|#`},
		"unshippedIds":     {Path: "orders.#(shippedAt==~null)#.id"},
		"shippedCount":     {Path: "orders.#(shippedAt!=~null)#|#"},
		"nullLiteralCount": {Path: "orders.#(shippedAt==null)#|#"},
	}}})
	body := `{"orders": [
		{"id": "o1", "status": "open", "shippedAt": null},
		{"id": "o2", "status": "shipped", "shippedAt": "2026-09-01"},
		{"id": "o3", "status": "open"}
	]}`

	outputs, err := a.ExtractOutputs(&Response{StatusCode: 200, Body: []byte(body)})

	require.NoError(t, err)
	assert.Equal(t, json.Number("3"), outputs["orderCount"])
	assert.Equal(t, []any{"o1", "o3"}, outputs["openOrderIds"], "#(…)# returns every match")
	assert.Equal(t, "o1", outputs["firstOpenOrderId"], "#(…) returns the first")
	assert.Equal(t, json.Number("2"), outputs["openOrderCount"])
	assert.Equal(t, []any{"o1", "o3"}, outputs["unshippedIds"], "==~null matches a null or missing field")
	assert.Equal(t, json.Number("1"), outputs["shippedCount"])
	assert.Equal(t, json.Number("0"), outputs["nullLiteralCount"], "==null compares a string, so it never matches a JSON null")
}
