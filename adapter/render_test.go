package adapter

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildRequest_EscapesValuesForWhereTheyLand sends a value with every
// character that could change the shape of a request through each place a
// placeholder can sit, and checks that it arrives as the same value.
func TestBuildRequest_EscapesValuesForWhereTheyLand(t *testing.T) {
	tricky := "a/b c?d&e=f#g%h\"i\\j\nk"

	t.Run("path segments and query values", func(t *testing.T) {
		a := NewTemplateAdapter(Template{Request: TemplateRequest{
			Method: "GET",
			Path:   "/carts/{{id}}/items{{?q}}?q={{q}}&page={{page}}{{/q}}",
		}})
		req, err := a.BuildRequest(map[string]any{"id": tricky, "q": tricky, "page": 2}, nil)
		require.NoError(t, err)

		u, err := url.Parse("http://api.example.com" + req.Path)
		require.NoError(t, err)
		segments := strings.Split(u.EscapedPath(), "/")
		require.Len(t, segments, 4, u.EscapedPath())
		segment, err := url.PathUnescape(segments[2])
		require.NoError(t, err)
		assert.Equal(t, tricky, segment)
		assert.Equal(t, "items", segments[3])
		assert.Equal(t, tricky, u.Query().Get("q"))
		assert.Equal(t, "2", u.Query().Get("page"))
	})

	t.Run("JSON body", func(t *testing.T) {
		a := NewTemplateAdapter(Template{Request: TemplateRequest{
			Method:  "POST",
			Path:    "/orders",
			Headers: map[string]string{"Content-Type": "application/json"},
			Body: `{"notes": "{{notes}}", "auth": "Bearer {{token}}", "quantity": {{quantity}}, "total": {{total}},` +
				` "tags": {{tags}}, "meta": {{meta}}, "none": {{none}}, "ids": [{{#ids}}"{{.}}"{{/ids}}]}`,
		}})
		req, err := a.BuildRequest(map[string]any{
			"notes":    tricky,
			"token":    `t"k`,
			"quantity": 2,
			"total":    1200000.0,
			"tags":     []any{"x<y"},
			"meta":     map[string]any{"k": tricky},
			"none":     nil,
			"ids":      []any{`a"b`, "c"},
		}, nil)
		require.NoError(t, err)

		var body map[string]any
		require.NoError(t, json.Unmarshal(req.Body, &body), string(req.Body))
		assert.Equal(t, tricky, body["notes"])
		assert.Equal(t, `Bearer t"k`, body["auth"])
		assert.Equal(t, float64(2), body["quantity"])
		assert.Contains(t, string(req.Body), `"total": 1200000,`)
		assert.Equal(t, []any{"x<y"}, body["tags"])
		assert.Equal(t, map[string]any{"k": tricky}, body["meta"])
		assert.Contains(t, body, "none")
		assert.Nil(t, body["none"])
		assert.Equal(t, []any{`a"b`, "c"}, body["ids"])
	})

	t.Run("JSON body without a content type", func(t *testing.T) {
		a := NewTemplateAdapter(Template{Request: TemplateRequest{Method: "POST", Path: "/orders", Body: `{"notes": "{{notes}}"}`}})
		req, err := a.BuildRequest(map[string]any{"notes": tricky}, nil)
		require.NoError(t, err)
		var body map[string]string
		require.NoError(t, json.Unmarshal(req.Body, &body), string(req.Body))
		assert.Equal(t, tricky, body["notes"])
	})

	t.Run("form body", func(t *testing.T) {
		a := NewTemplateAdapter(Template{Request: TemplateRequest{
			Method:  "POST",
			Path:    "/token",
			Headers: map[string]string{"content-type": "application/x-www-form-urlencoded"},
			Body:    "grant_type=password&username={{user}}&password={{pass}}",
		}})
		req, err := a.BuildRequest(map[string]any{"user": "a&b", "pass": tricky}, nil)
		require.NoError(t, err)
		form, err := url.ParseQuery(string(req.Body))
		require.NoError(t, err)
		assert.Equal(t, "password", form.Get("grant_type"))
		assert.Equal(t, "a&b", form.Get("username"))
		assert.Equal(t, tricky, form.Get("password"))
	})

	t.Run("headers and other bodies are text", func(t *testing.T) {
		a := NewTemplateAdapter(Template{Request: TemplateRequest{
			Method:  "POST",
			Path:    "/notes",
			Headers: map[string]string{"Content-Type": "text/plain", "X-Note": "note {{v}}"},
			Body:    "note: {{v}}",
		}})
		req, err := a.BuildRequest(map[string]any{"v": `a "b" & c`}, nil)
		require.NoError(t, err)
		assert.Equal(t, `note a "b" & c`, req.Headers["X-Note"])
		assert.Equal(t, `note: a "b" & c`, string(req.Body))
	})
}

func TestFormatValue_NumbersAndStructures(t *testing.T) {
	assert.Equal(t, "1200000", formatValue(1200000.0))
	assert.Equal(t, "0.5", formatValue(0.5))
	assert.Equal(t, "9007199254740993", formatValue(json.Number("9007199254740993")))
	assert.Equal(t, `{"k":"v"}`, formatValue(map[string]any{"k": "v"}))
	assert.Equal(t, `["a","b"]`, formatValue([]string{"a", "b"}))
	assert.Equal(t, "", formatValue(nil))
}
