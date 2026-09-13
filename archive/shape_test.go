package archive

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestShape(t *testing.T) {
	doc := `{
	  "data": {
	    "items": [
	      {"sku": "SKU-1", "price": "12.50", "tags": ["a", "b"], "discount": null},
	      {"sku": "SKU-2", "price": "8.00", "tags": [], "discount": "1.00", "note": "fragile"},
	      {"sku": "SKU-3", "price": "3.10", "tags": ["c"], "discount": null}
	    ],
	    "count": 3,
	    "more": false
	  },
	  "meta.version": "v2"
	}`

	assert.Equal(t, []ShapeLine{
		{Path: "data", Type: "object"},
		{Path: "data.items", Type: "array", Items: "3"},
		{Path: "data.items.#", Type: "object"},
		{Path: "data.items.#.sku", Type: "string", Sample: `"SKU-1"`},
		{Path: "data.items.#.price", Type: "string", Sample: `"12.50"`},
		{Path: "data.items.#.tags", Type: "array", Items: "0-2"},
		{Path: "data.items.#.tags.#", Type: "string", Sample: `"a"`},
		{Path: "data.items.#.discount", Type: "string|null", Sample: `"1.00"`},
		{Path: "data.items.#.note", Type: "string", Present: 1, Of: 3, Sample: `"fragile"`},
		{Path: "data.count", Type: "number", Sample: "3"},
		{Path: "data.more", Type: "boolean", Sample: "false"},
		{Path: `meta\.version`, Type: "string", Sample: `"v2"`},
	}, Shape([]byte(doc)))
}

// TestShape_PathsResolve: every path Shape reports reads something with gjson,
// including keys that need escaping and arrays of arrays.
func TestShape_PathsResolve(t *testing.T) {
	doc := []byte(`{"a.b": {"#": [{"x": 1}]}, "list": [[1, 2], [3]], "w*ld?": true}`)
	lines := Shape(doc)
	require.NotEmpty(t, lines)
	for _, line := range lines {
		assert.True(t, gjson.GetBytes(doc, line.Path).Exists(), "path %q", line.Path)
	}
}

func TestShape_Roots(t *testing.T) {
	assert.Equal(t, []ShapeLine{
		{Path: "@this", Type: "array", Items: "2"},
		{Path: "#", Type: "object"},
		{Path: "#.id", Type: "number", Sample: "1"},
	}, Shape([]byte(`[{"id": 1}, {"id": 2}]`)))
	assert.Equal(t, []ShapeLine{{Path: "@this", Type: "string", Sample: `"plain text body"`}}, Shape([]byte(`"plain text body"`)))
	assert.Equal(t, []ShapeLine{{Path: "@this", Type: "array", Items: "0"}}, Shape([]byte(`[]`)))
	assert.Empty(t, Shape([]byte(`{}`)), "an empty object has no paths")
	assert.Nil(t, Shape(nil))
	assert.Nil(t, Shape([]byte(`{"unterminated": `)))
}

func TestShape_LongSample(t *testing.T) {
	lines := Shape([]byte(`{"text": "` + strings.Repeat("é", 100) + `"}`))
	require.Len(t, lines, 1)
	assert.Equal(t, maxSampleRunes, utf8.RuneCountInString(lines[0].Sample))
	assert.True(t, strings.HasSuffix(lines[0].Sample, "…"))
}

func TestRenderShape(t *testing.T) {
	got := RenderShape([]ShapeLine{
		{Path: "items", Type: "array", Items: "1"},
		{Path: "items.#.note", Type: "string|null", Present: 2, Of: 5, Sample: `"x"`},
	})
	want := "items" + strings.Repeat(" ", 9) + "array" + strings.Repeat(" ", 8) + "1 item\n" +
		`items.#.note  string|null  in 2 of 5  "x"` + "\n"
	assert.Equal(t, want, got)
}
