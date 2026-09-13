package oas

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"

	"github.com/gburgyan/aat/graph"
)

// nullableSpecBody is a spec without its openapi version line: an order whose
// customer is a nullable anyOf of an ID or an object, whose last payment is a
// nullable oneOf, and whose note is an anyOf that isn't nullable.
const nullableSpecBody = `info:
  title: Nullable Compositions
  version: "1.0.0"
paths:
  /orders/{orderId}:
    get:
      operationId: getOrder
      parameters:
        - name: orderId
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: The order
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Order"
components:
  schemas:
    Order:
      type: object
      required:
        - orderId
      properties:
        orderId:
          type: string
        customer:
          nullable: true
          anyOf:
            - type: string
            - $ref: "#/components/schemas/Customer"
        lastPayment:
          nullable: true
          oneOf:
            - $ref: "#/components/schemas/Payment"
        note:
          anyOf:
            - type: string
    Customer:
      type: object
      required:
        - customerId
      properties:
        customerId:
          type: string
    Payment:
      type: object
      required:
        - paymentId
      properties:
        paymentId:
          type: string
`

func writeNullableSpec(t *testing.T, version string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "orders.yaml")
	require.NoError(t, os.WriteFile(path, []byte("openapi: \""+version+"\"\n"+nullableSpecBody), 0o644))
	return path
}

func TestValidateStep_NullableCompositions(t *testing.T) {
	cache := NewSpecCache()
	require.NoError(t, cache.Load("orders.yaml", writeNullableSpec(t, "3.0.3")))
	node := &graph.Node{Name: "getOrder", OAS: &graph.OASRef{OperationID: "getOrder", Spec: "orders.yaml"}}
	headers := http.Header{"Content-Type": []string{"application/json"}}

	tests := []struct {
		name       string
		body       string
		wantErrors bool
	}{
		{"a nullable anyOf and oneOf accept null", `{"orderId": "ord-1", "customer": null, "lastPayment": null}`, false},
		{"a nullable anyOf and oneOf accept their alternatives", `{"orderId": "ord-1", "customer": "cus-1", "lastPayment": {"paymentId": "pay-1"}}`, false},
		{"a nullable anyOf rejects a wrong type", `{"orderId": "ord-1", "customer": 5}`, true},
		{"a nullable oneOf rejects a wrong shape", `{"orderId": "ord-1", "lastPayment": {"amount": 5}}`, true},
		{"an anyOf that isn't nullable rejects null", `{"orderId": "ord-1", "note": null}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateStep(node, "", cache, "GET", "/orders/ord-1", nil, nil, 200, headers, []byte(tt.body))
			require.NotNil(t, result)
			require.NotNil(t, result.Response)
			assert.Equal(t, tt.wantErrors, result.HasErrors(), "response: %+v", result.Response)
		})
	}
}

// TestLoadSpec_NullableCompositionsOnlyInOpenAPI30 checks that the null
// alternative is added for OpenAPI 3.0, where nullable is a keyword, and not for
// 3.1, which expresses null with type lists instead.
func TestLoadSpec_NullableCompositionsOnlyInOpenAPI30(t *testing.T) {
	tests := []struct {
		version          string
		wantAlternatives int
	}{
		{"3.0.3", 3},
		{"3.1.0", 2},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			model, err := LoadSpec(writeNullableSpec(t, tt.version))
			require.NoError(t, err)
			order := model.Components.Schemas.GetOrZero("Order").Schema()
			customer := order.Properties.GetOrZero("customer").Schema()
			assert.Len(t, customer.AnyOf, tt.wantAlternatives)
		})
	}
}

func TestAllowNullInCompositions_AddsOneAlternative(t *testing.T) {
	scalar := func(value string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: value} }
	alternatives := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{
		{Kind: yaml.MappingNode, Content: []*yaml.Node{scalar("type"), scalar("string")}},
	}}
	schema := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		scalar("nullable"), scalar("true"), scalar("anyOf"), alternatives,
	}}

	allowNullInCompositions(schema)
	allowNullInCompositions(schema)

	require.Len(t, alternatives.Content, 2)
	assert.True(t, hasNullAlternative(alternatives))
}
