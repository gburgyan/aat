package adapter

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplateAdapter_ExtractOutputs_Headers(t *testing.T) {
	a := NewTemplateAdapter(Template{Response: TemplateResponse{Extract: map[string]ExtractRule{
		"remaining": {Header: "ratelimit-remaining"},
		"vary":      {Header: "Vary"},
		"location":  {Header: "Location", Optional: true},
		"reset":     {Header: "RateLimit-Reset", Default: ""},
	}}})
	headers := http.Header{}
	headers.Set("RateLimit-Remaining", "59")
	headers.Add("Vary", "Accept")
	headers.Add("Vary", "Accept-Encoding")

	outputs, err := a.ExtractOutputs(&Response{StatusCode: http.StatusNoContent, Headers: headers})

	require.NoError(t, err, "header rules need no JSON body")
	assert.Equal(t, map[string]any{"remaining": "59", "vary": "Accept, Accept-Encoding", "reset": ""}, outputs,
		"an optional header that's missing leaves its output out")
}

func TestTemplateAdapter_ExtractOutputs_HeaderNamesMatchInAnyCase(t *testing.T) {
	a := NewTemplateAdapter(Template{Response: TemplateResponse{Extract: map[string]ExtractRule{"requestId": {Header: "X-REQUEST-ID"}}}})

	outputs, err := a.ExtractOutputs(&Response{StatusCode: 200, Headers: http.Header{"x-request-id": {"req-1"}}, Body: []byte(`{}`)})

	require.NoError(t, err)
	assert.Equal(t, "req-1", outputs["requestId"], "a header map whose keys aren't canonical still matches")
}

func TestTemplateAdapter_ExtractOutputs_MissingHeaderNamesRemedies(t *testing.T) {
	a := NewTemplateAdapter(Template{Response: TemplateResponse{Extract: map[string]ExtractRule{"requestId": {Header: "X-Request-Id"}}}})

	_, err := a.ExtractOutputs(&Response{StatusCode: 200, Body: []byte(`{}`)})

	assert.EqualError(t, err, `extract header "requestId" (X-Request-Id) not found in response; mark the rule optional: true or give it a default`)
}

func TestTemplateAdapter_ExtractOutputs_BodyRulesStillNeedJSON(t *testing.T) {
	a := NewTemplateAdapter(Template{Response: TemplateResponse{Extract: map[string]ExtractRule{
		"requestId": {Header: "X-Request-Id", Optional: true},
		"orderId":   {Path: "id"},
	}}})

	_, err := a.ExtractOutputs(&Response{StatusCode: http.StatusNoContent})

	assert.EqualError(t, err, "response body is not valid JSON")
}

func TestParseTemplate_HeaderRules(t *testing.T) {
	tmpl, err := ParseTemplate([]byte("adapter: createCart\nrequest: {method: POST, path: /carts}\nresponse:\n  extract:\n    cartUrl: {header: Location}\n"))
	require.NoError(t, err)
	assert.Equal(t, ExtractRule{Header: "Location"}, tmpl.Response.Extract["cartUrl"])
	assert.Equal(t, "header Location", tmpl.Response.Extract["cartUrl"].Source())

	tests := []struct {
		name    string
		extract string
		want    string
	}{
		{name: "path and header", extract: `cartUrl: {path: url, header: Location}`, want: `line 5: an extract rule takes path or header, not both`},
		{name: "header with fields", extract: `cartUrl: {header: Location, fields: {id: id}}`, want: `line 5: a header extract rule takes no fields`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTemplate([]byte("adapter: createCart\nrequest: {method: POST, path: /carts}\nresponse:\n  extract:\n    " + tt.extract + "\n"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestRunTransform_Header(t *testing.T) {
	headers := http.Header{}
	headers.Set("RateLimit-Remaining", "59")

	result, err := runTransformWithLog(`
		return {remaining = tonumber(header("ratelimit-remaining")), missing = header("Location") == nil}
	`, map[string]any{}, "{}", headers, io.Discard)

	require.NoError(t, err)
	assert.Equal(t, map[string]any{"remaining": float64(59), "missing": true}, result)
}
