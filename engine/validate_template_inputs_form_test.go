package engine

import (
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateTemplateInputs_WholeValueNames checks that a form field or header
// whose whole value names no input of the node is reported, since it would
// never be sent, and that one naming an optional input without a default passes,
// since the field or header is left out without a value.
func TestValidateTemplateInputs_WholeValueNames(t *testing.T) {
	g := &graph.Graph{Nodes: map[string]*graph.Node{
		"capture": {
			Name:    "capture",
			Adapter: "capture",
			Inputs: []graph.Input{
				{Name: "intent", Type: "string"},
				{Name: "amountToCapture", Type: "integer", Optional: true},
				{Name: "requestKey", Type: "string", Optional: true},
			},
		},
	}}
	register := func(t *testing.T, formInput, headerInput string) *adapter.Registry {
		t.Helper()
		tmpl, err := adapter.ParseTemplate([]byte("adapter: capture\nrequest:\n  method: POST\n  path: /intents/{{intent}}/capture\n" +
			"  headers:\n    Idempotency-Key: \"{{" + headerInput + "}}\"\n" +
			"  form:\n    amount_to_capture: \"{{" + formInput + "}}\"\n"))
		require.NoError(t, err)
		registry := adapter.NewRegistry()
		require.NoError(t, registry.Register("capture", adapter.NewTemplateAdapter(*tmpl)))
		return registry
	}

	t.Run("optional inputs without defaults pass", func(t *testing.T) {
		assert.NoError(t, ValidateTemplateInputs(g, register(t, "amountToCapture", "requestKey")))
	})
	t.Run("a misspelled form value", func(t *testing.T) {
		err := ValidateTemplateInputs(g, register(t, "amountToCaptur", "requestKey"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `node "capture": template sends {{amountToCaptur}} as the whole value of a form field or header, but the node has no input "amountToCaptur", so it is never sent`)
	})
	t.Run("a misspelled header value", func(t *testing.T) {
		err := ValidateTemplateInputs(g, register(t, "amountToCapture", "requestKy"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `template sends {{requestKy}}`)
	})
}
