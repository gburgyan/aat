package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/internal/yamlx"
)

func TestInjectValue_Decode(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    InjectValue
		wantErr string
	}{
		{name: "scalar", yaml: "giftWrap: true", want: InjectLiteral(true)},
		{name: "a bare list is the list", yaml: "quantities: [2, 1]", want: InjectLiteral([]any{2, 1})},
		{name: "value list", yaml: "quantities: {value: [2, 1]}", want: InjectValue{InputDefault{Value: []any{2, 1}}}},
		{name: "pool", yaml: "region: {pool: [us, eu]}", want: InjectValue{InputDefault{Pool: []any{"us", "eu"}}}},
		{name: "from", yaml: "cartId: {from: createCart.cartId}", want: InjectValue{InputDefault{From: "createCart.cartId"}}},
		{name: "expression", yaml: `deliveryDate: "{{today + 3 days}}"`, want: InjectLiteral("{{today + 3 days}}")},
		{name: "default is not a key", yaml: "quantities: {default: [2]}", wantErr: `"default"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var inject map[string]InjectValue
			err := yamlx.Decode([]byte(tt.yaml), &inject)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, inject, 1)
			for _, got := range inject {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func injectGraph(workflows ...Workflow) *Graph {
	return &Graph{
		Version: "1.0.0",
		Nodes: map[string]*Node{
			"addItem": {Name: "addItem", Adapter: "addItem", Inputs: []Input{
				{Name: "quantity", Type: "integer"},
				{Name: "skus", Type: "string[]"},
			}},
		},
		Workflows: workflows,
	}
}

func TestValidate_InjectOnBaseOrAddon(t *testing.T) {
	quantity := map[string]InjectValue{"quantity": InjectLiteral(2)}
	err := Validate(injectGraph(
		Workflow{Name: "Checkout", Template: "checkout.yaml", Inject: quantity},
		Workflow{Name: "Gift Wrap", Kind: "addon", Template: "gift.yaml", Inject: quantity},
		Workflow{Name: "Two Items", Kind: "slot", Template: "two.yaml", Inject: quantity},
	))

	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, `workflow 0 ("Checkout"): inject applies only to slot options (kind: slot), and a base workflow ignores it`)
	assert.Contains(t, msg, `workflow 1 ("Gift Wrap"): inject applies only to slot options (kind: slot), and an addon ignores it`)
	assert.NotContains(t, msg, `"Two Items"`)
}

func TestValidate_InjectShapeMismatch(t *testing.T) {
	err := Validate(injectGraph(Workflow{Name: "Two Items", Kind: "slot", Template: "two.yaml", Inject: map[string]InjectValue{
		"quantity": InjectLiteral(map[string]any{"n": 2}),
		"skus":     InjectLiteral([]any{"SKU-1", "SKU-2"}),
	}}))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `workflow 0 ("Two Items"): inject "quantity" for addItem.quantity: a map, where integer takes a single value`)
	assert.NotContains(t, err.Error(), `inject "skus"`)
}

func TestValidate_InjectFitsAnInputWithItsName(t *testing.T) {
	g := injectGraph(Workflow{Name: "Open Orders", Kind: "slot", Template: "open.yaml", Inject: map[string]InjectValue{
		"status": InjectLiteral("open"),
	}})
	g.Nodes["listOrders"] = &Node{Name: "listOrders", Adapter: "listOrders", Inputs: []Input{{Name: "status", Type: "string"}}}
	g.Nodes["bulkUpdate"] = &Node{Name: "bulkUpdate", Adapter: "bulkUpdate", Inputs: []Input{{Name: "status", Type: "string[]"}}}

	assert.NoError(t, Validate(g), "listOrders.status takes it, whichever steps the option composes with")

	g.Workflows[0].Inject["status"] = InjectLiteral(map[string]any{"is": "open"})
	err := Validate(g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `workflow 0 ("Open Orders"): inject "status" for bulkUpdate.status: a map, where string[] takes a list`)
	assert.Contains(t, err.Error(), `workflow 0 ("Open Orders"): inject "status" for listOrders.status: a map, where string takes a single value`)
}

func TestValidate_NodeDefaultShape(t *testing.T) {
	g := injectGraph()
	g.Nodes["addItem"].Inputs[1].Default = &InputDefault{Pool: []any{"SKU-1", "SKU-2"}}

	err := Validate(g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `node "addItem": input "skus" default: pool entry 0: a single value, where string[] takes a list (each pool entry is one choice, so write a single list as {value: [...]})`)
}

func TestValueShapeError(t *testing.T) {
	tests := []struct {
		v    any
		typ  string
		want string
	}{
		{v: 2, typ: "integer"},
		{v: "78701", typ: "integer"},
		{v: []any{2, 1}, typ: "integer[]"},
		{v: 2, typ: "integer[]", want: "a single value, where integer[] takes a list"},
		{v: map[string]any{"default": []any{35}}, typ: "integer[]", want: "a map, where integer[] takes a list"},
		{v: []any{"a", "b"}, typ: "string"}, // a list for one field, sent as repeated pairs
		{v: map[string]any{"a": 1}, typ: "enum[a, b]", want: "a map, where enum[a, b] takes a single value"},
		{v: []any{1, map[string]any{"n": 2}}, typ: "integer[]", want: "item 1: a map, where integer takes a single value"},
		{v: "{{today + 3 days}}", typ: "date[]"},
		{v: map[string]any{"line1": "1 Main St"}, typ: "address"},
		{v: nil, typ: "string"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, ValueShapeError(tt.v, tt.typ), "%v as %s", tt.v, tt.typ)
	}
}

func TestLayer_ShapeErrors(t *testing.T) {
	g := &Graph{Nodes: map[string]*Node{
		"addItem":    {Name: "addItem", Inputs: []Input{{Name: "quantity", Type: "integer"}, {Name: "skus", Type: "string[]"}}},
		"createCart": {Name: "createCart", Inputs: []Input{{Name: "skus", Type: "string[]"}}},
	}}
	layer := &Layer{Name: "bulk", Inputs: map[string]*InputDefault{
		"quantity":        {Value: map[string]any{"n": 5}},
		"addItem.skus":    {Pool: []any{"SKU-1", "SKU-2"}},
		"createCart.skus": {Value: []any{"SKU-1"}},
	}}

	assert.Equal(t, []string{
		`input "addItem.skus" for addItem.skus: pool entry 0: a single value, where string[] takes a list (each pool entry is one choice, so write a single list as {value: [...]})`,
		`input "quantity" for addItem.quantity: a map, where integer takes a single value`,
	}, layer.ShapeErrors(g))
}
