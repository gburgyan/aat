package oas

import (
	"strings"
	"testing"

	"github.com/pb33f/libopenapi/datamodel/high/base"
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

// warningsFor returns the warnings about one operation.
func warningsFor(result *GenerateResult, operationID string) []string {
	var warnings []string
	for _, w := range result.Warnings {
		if strings.Contains(w, "("+operationID+")") {
			warnings = append(warnings, w)
		}
	}
	return warnings
}

func TestGenerate_NullableTypeLists(t *testing.T) {
	result := loadRequestShapes(t)

	assert.Equal(t, "integer", inputTypes(t, result, "createOrder")["quantity"], `"null" in a type list is skipped`)
	assert.Contains(t, shapeTemplate(t, result, "createOrder").Request.Body, `"quantity": {{quantity}}`)

	node := result.Graph.Nodes["listLabels"]
	require.NotNil(t, node)
	require.Len(t, node.Outputs, 1)
	assert.Equal(t, "labels", node.Outputs[0].Name)
	assert.Equal(t, "object[]", node.Outputs[0].Type)
	assert.Equal(t, "@this", shapeTemplate(t, result, "listLabels").Response.Extract["labels"].Path)
}

func TestGenerate_RequestBodyMediaTypes(t *testing.T) {
	result := loadRequestShapes(t)

	t.Run("a form body is a request.form", func(t *testing.T) {
		tmpl := shapeTemplate(t, result, "requestRefund")
		assert.NotContains(t, tmpl.Request.Headers, "Content-Type", "request.form sets it")
		assert.Empty(t, tmpl.Request.Body)
		assert.Equal(t, ScaffoldForm{{"orderId", "{{orderId}}"}, {"reasons", "{{reasons}}"}, {"note", "{{note}}"}}, tmpl.Request.Form)
		assert.Equal(t, map[string]string{"orderId": "string", "reasons": "string[]", "note": "string"}, inputTypes(t, result, "requestRefund"))
		assert.Empty(t, warningsFor(result, "requestRefund"))
	})

	t.Run("a multipart body gives inputs only", func(t *testing.T) {
		tmpl := shapeTemplate(t, result, "uploadReceipt")
		assert.Empty(t, tmpl.Request.Headers)
		assert.Empty(t, tmpl.Request.Body)
		assert.Equal(t, map[string]string{"file": "string", "caption": "string"}, inputTypes(t, result, "uploadReceipt"))
		assert.Equal(t, []string{
			"POST /receipts (uploadReceipt): the multipart/form-data body is not generated; its properties are inputs, so write the body by hand",
		}, warningsFor(result, "uploadReceipt"))
	})

	t.Run("another media type gives nothing", func(t *testing.T) {
		tmpl := shapeTemplate(t, result, "putExport")
		assert.Empty(t, tmpl.Request.Headers)
		assert.Empty(t, tmpl.Request.Body)
		assert.Equal(t, map[string]string{"exportId": "string"}, inputTypes(t, result, "putExport"))
		assert.Equal(t, []string{
			"PUT /exports/{exportId} (putExport): the application/octet-stream request body is not generated; write the body and its Content-Type by hand",
		}, warningsFor(result, "putExport"))
	})

	t.Run("a JSON type wins over one listed before it", func(t *testing.T) {
		tmpl := shapeTemplate(t, result, "createCoupon")
		assert.Equal(t, "application/vnd.shop+json", tmpl.Request.Headers["Content-Type"])
		assert.Empty(t, warningsFor(result, "createCoupon"))
	})

	t.Run("application/json wins over another JSON type", func(t *testing.T) {
		tmpl := shapeTemplate(t, result, "updateCart")
		assert.Equal(t, "application/json", tmpl.Request.Headers["Content-Type"])
	})

	t.Run("a schema without properties gives no body", func(t *testing.T) {
		tmpl := shapeTemplate(t, result, "mergeCart")
		assert.Empty(t, tmpl.Request.Headers, "no Content-Type without a body")
		assert.Empty(t, tmpl.Request.Body)
		assert.Equal(t, []string{
			"POST /carts/{cartId}/merge (mergeCart): the application/json body schema declares no properties; write the body by hand",
		}, warningsFor(result, "mergeCart"))
	})

	t.Run("oneOf warns", func(t *testing.T) {
		tmpl := shapeTemplate(t, result, "createGiftCard")
		assert.Empty(t, tmpl.Request.Headers)
		assert.Empty(t, tmpl.Request.Body)
		assert.Equal(t, []string{
			"POST /gift-cards (createGiftCard): the application/json body schema uses oneOf or anyOf; write the body by hand",
		}, warningsFor(result, "createGiftCard"))
	})
}

func TestGenerate_AllOfBody(t *testing.T) {
	result := loadRequestShapes(t)
	node := result.Graph.Nodes["createCoupon"]
	require.NotNil(t, node)

	optional := make(map[string]bool)
	for _, in := range node.Inputs {
		optional[in.Name] = in.Optional
	}
	assert.Equal(t, map[string]bool{"code": false, "expiresAt": true, "percentOff": false}, optional,
		"allOf branches contribute properties and required names")
	assert.Equal(t, "datetime", inputTypes(t, result, "createCoupon")["expiresAt"])

	assert.Equal(t, "{\n  \"code\": \"{{code}}\",\n  \"percentOff\": {{percentOff}}{{?expiresAt}},\n  \"expiresAt\": \"{{expiresAt}}\"{{/expiresAt}}\n}",
		shapeTemplate(t, result, "createCoupon").Request.Body)

	outputs := make(map[string]bool)
	for _, out := range node.Outputs {
		outputs[out.Name] = out.Optional
	}
	assert.Equal(t, map[string]bool{"code": false, "expiresAt": true, "couponId": false}, outputs)
}

func TestGenerate_CookieHeader(t *testing.T) {
	result := loadRequestShapes(t)

	tmpl := shapeTemplate(t, result, "getSession")
	assert.Equal(t, "sessionId={{sessionId}}{{?theme}}; theme={{theme}}{{/theme}}", tmpl.Request.Headers["Cookie"])
	types := inputTypes(t, result, "getSession")
	assert.Contains(t, types, "sessionId")
	assert.Contains(t, types, "theme")
}

func TestGenerate_ParameterStyleWarns(t *testing.T) {
	result := loadRequestShapes(t)

	assert.Equal(t, []string{
		`GET /session (getSession): parameter "ids" uses style pipeDelimited, which the template does not reproduce; rewrite it by hand`,
		`GET /session (getSession): parameter "fields" sets explode: false, which the template does not reproduce; rewrite it by hand`,
	}, warningsFor(result, "getSession"), "explode: false on a scalar changes nothing, so region is not reported")
}

func TestGenerate_HeadOptionsTrace(t *testing.T) {
	result := loadRequestShapes(t)

	for operationID, method := range map[string]string{"headHealth": "HEAD", "optionsHealth": "OPTIONS", "traceHealth": "TRACE"} {
		assert.Contains(t, result.Graph.Nodes, operationID)
		assert.Equal(t, method, shapeTemplate(t, result, operationID).Request.Method)
	}
}

func TestFindOperation_AllMethods(t *testing.T) {
	model, err := LoadSpec("testdata/request_shapes.yaml")
	require.NoError(t, err)

	for operationID, want := range map[string]string{
		"listLabels":    "GET",
		"createOrder":   "POST",
		"putExport":     "PUT",
		"updateCart":    "PATCH",
		"headHealth":    "HEAD",
		"optionsHealth": "OPTIONS",
		"traceHealth":   "TRACE",
	} {
		method, _, _, _, err := FindOperation(model, operationID)
		require.NoError(t, err, operationID)
		assert.Equal(t, want, method, operationID)
	}
}

func TestSchemaType(t *testing.T) {
	tests := []struct {
		name   string
		schema *base.Schema
		want   string
	}{
		{"nil schema", nil, ""},
		{"untyped", &base.Schema{}, ""},
		{"one type", &base.Schema{Type: []string{"integer"}}, "integer"},
		{"null first", &base.Schema{Type: []string{"null", "integer"}}, "integer"},
		{"null last", &base.Schema{Type: []string{"string", "null"}}, "string"},
		{"only null", &base.Schema{Type: []string{"null"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, schemaType(tt.schema))
		})
	}
}

func TestValidator_AllOfBodyInputs(t *testing.T) {
	model, err := LoadSpec("testdata/request_shapes.yaml")
	require.NoError(t, err)
	_, _, pathItem, op, err := FindOperation(model, "createCoupon")
	require.NoError(t, err)

	assert.Equal(t, map[string]bool{"code": true, "expiresAt": true, "percentOff": true}, collectInputNames(pathItem, op))
	assert.Equal(t, map[string]bool{"code": true, "percentOff": true}, collectRequiredInputs(pathItem, op))
}
