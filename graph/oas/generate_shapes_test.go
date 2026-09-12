package oas

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadRequestShapes(t *testing.T) *GenerateResult {
	t.Helper()
	model, err := LoadSpec("testdata/request_shapes.yaml")
	require.NoError(t, err)
	result, err := Generate(model, "request_shapes.yaml")
	require.NoError(t, err)
	return result
}

func shapeTemplate(t *testing.T, result *GenerateResult, adapter string) *ScaffoldTemplate {
	t.Helper()
	for _, tmpl := range result.Templates {
		if tmpl.Adapter == adapter {
			return tmpl
		}
	}
	require.Failf(t, "template not found", "no template %q", adapter)
	return nil
}

func inputTypes(t *testing.T, result *GenerateResult, node string) map[string]string {
	t.Helper()
	n := result.Graph.Nodes[node]
	require.NotNil(t, n, "node %q", node)
	types := make(map[string]string, len(n.Inputs))
	for _, in := range n.Inputs {
		types[in.Name] = in.Type
	}
	return types
}

func TestGenerate_ObjectBodyProperty(t *testing.T) {
	result := loadRequestShapes(t)

	types := inputTypes(t, result, "createOrder")
	assert.Equal(t, "object", types["shipping"])
	assert.Equal(t, "object", types["meta"], "an untyped schema with properties is an object")
	assert.Equal(t, "string", types["note"])

	body := shapeTemplate(t, result, "createOrder").Request.Body
	assert.Contains(t, body, `"shipping": {{shipping}}`, "an object property is inserted as a JSON literal")
	assert.Contains(t, body, `"meta": {{meta}}`)
	assert.Contains(t, body, `"note": "{{note}}"`)
}

func TestGenerate_NodeLeavesNameToTheMapKey(t *testing.T) {
	result := loadRequestShapes(t)
	require.Contains(t, result.Graph.Nodes, "createOrder")
	assert.Empty(t, result.Graph.Nodes["createOrder"].Name)
}
