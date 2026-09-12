package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildRequest_HeaderPrecedence checks that template headers replace the
// headers a request starts with, whatever the case of their names, and that
// protected headers (such as the credential) replace template headers.
func TestBuildRequest_HeaderPrecedence(t *testing.T) {
	a := NewTemplateAdapter(Template{Request: TemplateRequest{
		Method: "GET",
		Path:   "/orders",
		Headers: map[string]string{
			"content-type":  "application/json",
			"Authorization": "Bearer from-template",
			"X-Trace":       "template",
		},
	}})
	cfg := &EnvironmentConfig{
		Headers: map[string]string{
			"Content-Type":  "text/plain",
			"Authorization": "Bearer credential",
			"X-Trace":       "env",
			"Accept":        "application/json",
		},
		Protected: map[string]string{
			"authorization": "Bearer credential",
		},
	}

	req, err := a.BuildRequest(map[string]any{}, cfg)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"content-type":  "application/json",
		"authorization": "Bearer credential",
		"X-Trace":       "template",
		"Accept":        "application/json",
	}, req.Headers)
}
