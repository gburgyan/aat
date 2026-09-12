package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubstitutePlaceholders_ListsInURL(t *testing.T) {
	tests := []struct {
		name   string
		tmpl   string
		inputs map[string]any
		want   string
	}{
		{
			name:   "a list after key= repeats the pair",
			tmpl:   "/items?tags={{tags}}",
			inputs: map[string]any{"tags": []any{"a", "b"}},
			want:   "/items?tags=a&tags=b",
		},
		{
			name:   "each element is escaped",
			tmpl:   "/items?tags={{tags}}",
			inputs: map[string]any{"tags": []string{"a&b", "c d"}},
			want:   "/items?tags=a%26b&tags=c+d",
		},
		{
			name:   "after other parameters",
			tmpl:   "/items?q={{q}}&tags={{tags}}",
			inputs: map[string]any{"q": "x", "tags": []any{1, 2}},
			want:   "/items?q=x&tags=1&tags=2",
		},
		{
			name:   "an empty list leaves the key",
			tmpl:   "/items?tags={{tags}}",
			inputs: map[string]any{"tags": []any{}},
			want:   "/items?tags=",
		},
		{
			name:   "in a path segment elements are joined with commas",
			tmpl:   "/items/{{ids}}",
			inputs: map[string]any{"ids": []any{"a/b", "c"}},
			want:   "/items/a%2Fb,c",
		},
		{
			name:   "without a key elements are joined with commas",
			tmpl:   "/items?{{pairs}}",
			inputs: map[string]any{"pairs": []any{"a", "b"}},
			want:   "/items?a,b",
		},
		{
			name:   "after another value in the same pair elements are joined with commas",
			tmpl:   "/items?a={{x}}{{tags}}",
			inputs: map[string]any{"x": "1", "tags": []any{"b", "c"}},
			want:   "/items?a=1b,c",
		},
		{
			name:   "a scalar is escaped as before",
			tmpl:   "/items?tag={{tag}}",
			inputs: map[string]any{"tag": "a&b"},
			want:   "/items?tag=a%26b",
		},
		{
			name:   "an object is still JSON text",
			tmpl:   "/items?filter={{filter}}",
			inputs: map[string]any{"filter": map[string]any{"k": "v"}},
			want:   "/items?filter=%7B%22k%22%3A%22v%22%7D",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := substitutePlaceholders(tt.tmpl, tt.inputs, renderPath)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSubstitutePlaceholders_ListInJSONBodyUnchanged(t *testing.T) {
	got, err := substitutePlaceholders(`{"tags": {{tags}}}`, map[string]any{"tags": []any{"a", "b"}}, renderJSON)
	require.NoError(t, err)
	assert.JSONEq(t, `{"tags": ["a", "b"]}`, got)
}
