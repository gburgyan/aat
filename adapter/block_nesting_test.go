package adapter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// optionalIDs is a conditional that leaves the list out when it's absent,
// wrapping the iteration that writes it: both blocks close with {{/ids}}.
const optionalIDs = `{"a": 1{{?ids}}, "ids": [{{#ids}}"{{.}}"{{/ids}}]{{/ids}}}`

func TestSubstitutePlaceholders_BlocksOfTheSameKeyNest(t *testing.T) {
	tests := []struct {
		name   string
		tmpl   string
		inputs map[string]any
		want   string
	}{
		{name: "conditional around iteration, list present", tmpl: optionalIDs, inputs: map[string]any{"ids": []any{"p", "q"}}, want: `{"a": 1, "ids": ["p","q"]}`},
		{name: "conditional around iteration, list absent", tmpl: optionalIDs, inputs: map[string]any{}, want: `{"a": 1}`},
		{name: "conditional around iteration, list empty", tmpl: optionalIDs, inputs: map[string]any{"ids": []any{}}, want: `{"a": 1, "ids": []}`},
		{name: "conditional inside a conditional of the same key", tmpl: `{{?a}}X{{?a}}Y{{/a}}Z{{/a}}!`, inputs: map[string]any{"a": "1"}, want: `XYZ!`},
		{name: "conditional inside a conditional of the same key, absent", tmpl: `{{?a}}X{{?a}}Y{{/a}}Z{{/a}}!`, inputs: map[string]any{}, want: `!`},
		{name: "compound conditional around iteration", tmpl: `{{?ids|all}}[{{#ids}}"{{.}}"{{/ids}}]{{/ids|all}}`, inputs: map[string]any{"ids": []any{"p"}}, want: `["p"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := substitutePlaceholders(tt.tmpl, tt.inputs, renderRaw)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestBuildRequest_TemplatesGuideNestedExample renders the example of a
// conditional wrapping an iteration in docs/user/templates.md.
func TestBuildRequest_TemplatesGuideNestedExample(t *testing.T) {
	a := NewTemplateAdapter(Template{Adapter: "createOrder", Protocol: "http", Request: TemplateRequest{
		Method:  "POST",
		Path:    "/orders",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body: `{
  "order": {
    "primaryItem": "{{primaryItemId}}"
    {{?additionalItemIds}},
    "additionalItems": [
      {{#additionalItemIds}}
      {"itemId": "{{.}}"}
      {{/additionalItemIds}}
    ]
    {{/additionalItemIds}}
  }
}`,
	}})

	type order struct {
		Order struct {
			PrimaryItem     string              `json:"primaryItem"`
			AdditionalItems []map[string]string `json:"additionalItems"`
		} `json:"order"`
	}
	for _, tt := range []struct {
		name   string
		inputs map[string]any
		want   int
	}{
		{name: "with additional items", inputs: map[string]any{"primaryItemId": "item-1", "additionalItemIds": []any{"item-2", "item-3"}}, want: 2},
		{name: "without", inputs: map[string]any{"primaryItemId": "item-1"}, want: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req, err := a.BuildRequest(tt.inputs, &EnvironmentConfig{})
			require.NoError(t, err)
			var got order
			require.NoError(t, json.Unmarshal(req.Body, &got), "the body is valid JSON:\n%s", req.Body)
			assert.Equal(t, "item-1", got.Order.PrimaryItem)
			assert.Len(t, got.Order.AdditionalItems, tt.want)
		})
	}
}

func TestWithoutBlocks_SameKeyNesting(t *testing.T) {
	assert.Equal(t, `{"a": 1}`, withoutBlocks(optionalIDs))
}

func TestClassifyInputs_ConditionalAroundIterationOfTheSameKey(t *testing.T) {
	required, conditional, iterable := ClassifyInputs(&Template{Request: TemplateRequest{Body: optionalIDs}})

	assert.Empty(t, required, "a list the conditional leaves out isn't required")
	assert.Contains(t, conditional, "ids")
	assert.Contains(t, iterable, "ids")
}

func TestFindBlockClose(t *testing.T) {
	tests := []struct {
		name string
		s    string
		key  string
		want int
	}{
		{name: "first close", s: `x{{/a}}y{{/a}}`, key: "a", want: 1},
		{name: "skips a nested iteration of the key", s: `[{{#a}}.{{/a}}]{{/a}}`, key: "a", want: 15},
		{name: "ignores other keys", s: `{{?b}}{{/b}}{{/a}}`, key: "a", want: 12},
		{name: "compound key is its own name", s: `{{#a}}{{/a}}{{/a|b}}`, key: "a|b", want: 12},
		{name: "unclosed", s: `{{#a}}{{/a}}`, key: "a", want: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, findBlockClose(tt.s, tt.key))
		})
	}
}
