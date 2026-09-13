package adapter

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubstitutePlaceholders_IterationInQueryAndForm(t *testing.T) {
	twoTags := map[string]any{"sku": "A", "tags": []any{"a", "b"}}
	tests := []struct {
		name   string
		tmpl   string
		inputs map[string]any
		ctx    renderContext
		want   string
	}{
		{"a form block that starts a pair joins copies with &", "{{#tags}}tags[]={{.}}{{/tags}}", twoTags, renderForm, "tags[]=a&tags[]=b"},
		{"a form block after & joins copies with &", "sku={{sku}}&{{#tags}}tags[]={{.}}{{/tags}}", twoTags, renderForm, "sku=A&tags[]=a&tags[]=b"},
		{"a form block whose body starts with & is concatenated", "sku={{sku}}{{#tags}}&tags[]={{.}}{{/tags}}", twoTags, renderForm, "sku=A&tags[]=a&tags[]=b"},
		{"an empty list in such a block sends nothing", "sku={{sku}}{{#tags}}&tags[]={{.}}{{/tags}}", map[string]any{"sku": "A", "tags": []any{}}, renderForm, "sku=A"},
		{"values in form copies are escaped", "{{#tags}}tags[]={{.}}{{/tags}}", map[string]any{"tags": []any{"b&c", "d e"}}, renderForm, "tags[]=b%26c&tags[]=d+e"},
		{"a value-only block after key= keeps commas", "ids={{#ids}}{{.}}{{/ids}}", map[string]any{"ids": []any{"a", "b"}}, renderForm, "ids=a,b"},
		{
			"a block in the middle of a pair keeps commas",
			"filter={{#c}}{{.k}}:{{.v}}{{/c}}",
			map[string]any{"c": []any{map[string]any{"k": "a", "v": "1"}, map[string]any{"k": "b", "v": "2"}}},
			renderForm,
			"filter=a:1,b:2",
		},
		{"a query block after ? joins copies with &", "/items?{{#tags}}tag={{.}}{{/tags}}&limit=10", twoTags, renderPath, "/items?tag=a&tag=b&limit=10"},
		{"a path block before ? keeps commas", "/items/{{#tags}}{{.}}{{/tags}}", twoTags, renderPath, "/items/a,b"},
		{"a JSON block keeps commas", `[{{#tags}}"{{.}}"{{/tags}}]`, twoTags, renderJSON, `["a","b"]`},
		{"a header block keeps commas", "{{#tags}}{{.}}={{.}}{{/tags}}", twoTags, renderRaw, "a=a,b=b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := substitutePlaceholders(tt.tmpl, tt.inputs, tt.ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExpandIterationBlocks_Index(t *testing.T) {
	items := map[string]any{"items": []any{map[string]any{"sku": "A"}, map[string]any{"sku": "B"}}}

	t.Run("in form keys", func(t *testing.T) {
		got, err := substitutePlaceholders("{{#items}}&items[{{@index}}][sku]={{.sku}}{{/items}}", items, renderForm)
		require.NoError(t, err)
		assert.Equal(t, "&items[0][sku]=A&items[1][sku]=B", got)
	})

	t.Run("in a JSON body", func(t *testing.T) {
		got, err := substitutePlaceholders(`[{{#items}}{"n": {{ @index }}, "sku": "{{.sku}}"}{{/items}}]`, items, renderJSON)
		require.NoError(t, err)
		assert.Equal(t, `[{"n": 0, "sku": "A"},{"n": 1, "sku": "B"}]`, got)
	})

	t.Run("outside a block", func(t *testing.T) {
		_, err := substitutePlaceholders("n={{@index}}", items, renderForm)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "@index")
	})
}

// TestBuildRequest_FormAndQueryOnTheWire renders a template with iteration
// blocks in its query string and its form body, and checks the exact bytes and
// how a form parser reads them.
func TestBuildRequest_FormAndQueryOnTheWire(t *testing.T) {
	tmpl, err := ParseTemplate([]byte(`adapter: createRefund
protocol: http
request:
  method: POST
  path: '/refunds?{{#expand}}expand[]={{.}}{{/expand}}&limit=10'
  headers:
    Content-Type: application/x-www-form-urlencoded
  body: |
    orderId={{orderId}}{{#items}}&items[{{@index}}][sku]={{.sku}}&items[{{@index}}][quantity]={{.quantity}}{{/items}}{{#tags}}&tags[]={{.}}{{/tags}}
response:
  extract:
    refundId: id
`))
	require.NoError(t, err)

	req, err := NewTemplateAdapter(*tmpl).BuildRequest(map[string]any{
		"orderId": "ord 1",
		"expand":  []any{"order", "payment"},
		"items":   []any{map[string]any{"sku": "A&B", "quantity": 2}, map[string]any{"sku": "C", "quantity": 1}},
		"tags":    []any{"gift", "rush"},
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, "/refunds?expand[]=order&expand[]=payment&limit=10", req.Path)
	assert.Equal(t, "orderId=ord+1&items[0][sku]=A%26B&items[0][quantity]=2&items[1][sku]=C&items[1][quantity]=1&tags[]=gift&tags[]=rush", string(req.Body))

	form, err := url.ParseQuery(string(req.Body))
	require.NoError(t, err)
	assert.Equal(t, "ord 1", form.Get("orderId"))
	assert.Equal(t, "A&B", form.Get("items[0][sku]"))
	assert.Equal(t, "1", form.Get("items[1][quantity]"))
	assert.Equal(t, []string{"gift", "rush"}, form["tags[]"])
}

// TestClassifyInputs_IndexIsNotAnInput checks that {{@index}} in an iteration
// block is not taken for an input the node must declare.
func TestClassifyInputs_IndexIsNotAnInput(t *testing.T) {
	required, conditional, iterable := ClassifyInputs(&Template{Request: TemplateRequest{
		Method: "POST",
		Path:   "/refunds",
		Body:   "orderId={{orderId}}{{#items}}&items[{{@index}}][sku]={{.sku}}{{/items}}{{?note}}{{#tags}}&tags[{{@index}}]={{.}}{{/tags}}{{/note}}",
	}})
	assert.Equal(t, []string{"orderId"}, required)
	assert.Equal(t, []string{"note", "tags"}, conditional)
	assert.Equal(t, []string{"items", "tags"}, iterable)
}

func TestBuildRequest_BodyWhitespace(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		want        string
	}{
		{"a form body loses the final newline of a block scalar", "application/x-www-form-urlencoded", "orderId={{orderId}}&amount=5\n", "orderId=ord-1&amount=5"},
		{"surrounding whitespace in a form body is removed", "application/x-www-form-urlencoded; charset=utf-8", "  orderId={{orderId}}  \n", "orderId=ord-1"},
		{"a JSON body keeps its final newline", "application/json", "{\"orderId\": \"{{orderId}}\"}\n", "{\"orderId\": \"ord-1\"}\n"},
		{"a text body keeps its whitespace", "text/plain", "order {{orderId}}\n", "order ord-1\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewTemplateAdapter(Template{Request: TemplateRequest{
				Method:  "POST",
				Path:    "/orders",
				Headers: map[string]string{"Content-Type": tt.contentType},
				Body:    tt.body,
			}})
			req, err := a.BuildRequest(map[string]any{"orderId": "ord-1"}, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(req.Body))
		})
	}
}
