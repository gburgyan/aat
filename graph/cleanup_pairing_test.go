package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/gburgyan/aat/internal/yamlx"
)

type cleanupHolder struct {
	Name    string         `yaml:"name"`
	Cleanup CleanupPairing `yaml:"cleanup,omitempty"`
}

func TestCleanupPairing_Decode(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    CleanupPairing
		wantErr string
	}{
		{name: "node name", yaml: "cleanup: voidPayment", want: CleanupPairing{Node: "voidPayment"}},
		{
			name: "mapping",
			yaml: "cleanup:\n  node: voidPayment\n  when: 'status == \"authorized\"'\n  releasedBy: [capturePayment, refundPayment]",
			want: CleanupPairing{Node: "voidPayment", When: `status == "authorized"`, ReleasedBy: []string{"capturePayment", "refundPayment"}},
		},
		{name: "unknown key", yaml: "cleanup:\n  node: voidPayment\n  releaseBy: [capturePayment]", wantErr: "releaseBy"},
		{name: "list", yaml: "cleanup: [voidPayment]", wantErr: "cleanup"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var holder cleanupHolder
			err := yamlx.Decode([]byte(tt.yaml), &holder)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, holder.Cleanup)
		})
	}
}

func TestCleanupPairing_Marshal(t *testing.T) {
	out, err := yaml.Marshal(cleanupHolder{Name: "createCart", Cleanup: CleanupPairing{Node: "deleteCart"}})
	require.NoError(t, err)
	assert.Equal(t, "name: createCart\ncleanup: deleteCart\n", string(out), "a pairing with only a node is written as its name")

	out, err = yaml.Marshal(cleanupHolder{Name: "getCart"})
	require.NoError(t, err)
	assert.Equal(t, "name: getCart\n", string(out), "no pairing, no key")

	full := cleanupHolder{Name: "createPayment", Cleanup: CleanupPairing{Node: "voidPayment", When: `status == "authorized"`, ReleasedBy: []string{"capturePayment"}}}
	out, err = yaml.Marshal(full)
	require.NoError(t, err)
	assert.Contains(t, string(out), "releasedBy:")
	var back cleanupHolder
	require.NoError(t, yamlx.Decode(out, &back))
	assert.Equal(t, full, back)
}

func TestCleanupPairing_String(t *testing.T) {
	assert.Equal(t, "deleteCart", CleanupPairing{Node: "deleteCart"}.String())
	assert.Equal(t, `voidPayment (when status == "authorized"; released by capturePayment, refundPayment)`,
		CleanupPairing{Node: "voidPayment", When: `status == "authorized"`, ReleasedBy: []string{"capturePayment", "refundPayment"}}.String())
}

func TestValidate_CleanupReleasedBy(t *testing.T) {
	g := &Graph{Version: "1.0.0", Nodes: map[string]*Node{
		"createPayment": {Name: "createPayment", Adapter: "createPayment", Cleanup: CleanupPairing{
			Node:       "voidPayment",
			ReleasedBy: []string{"capturePayment", "createPayment", "refundPayment", "capturePayment"},
		}},
		"capturePayment": {Name: "capturePayment", Adapter: "capturePayment"},
		"voidPayment":    {Name: "voidPayment", Adapter: "voidPayment"},
		"getPayment":     {Name: "getPayment", Adapter: "getPayment", Cleanup: CleanupPairing{When: `status == "open"`}},
	}}

	err := Validate(g)
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, `node "createPayment": cleanup releasedBy cannot name the node itself`)
	assert.Contains(t, msg, `node "createPayment": cleanup releasedBy references unknown node "refundPayment"`)
	assert.Contains(t, msg, `node "createPayment": cleanup releasedBy lists "capturePayment" more than once`)
	assert.Contains(t, msg, `node "getPayment": cleanup sets when or releasedBy but no node`)
}

func TestValidate_CleanupWhen(t *testing.T) {
	g := &Graph{Version: "1.0.0", Nodes: map[string]*Node{
		"createPayment": {
			Name: "createPayment", Adapter: "createPayment",
			Cleanup: CleanupPairing{Node: "voidPayment", When: `status == "authorized" && amount.value > 0`},
			Outputs: []Output{{Name: "status", Type: "string"}, {Name: "amount", Type: "money"}},
		},
		"createOrder": {
			Name: "createOrder", Adapter: "createOrder",
			Cleanup: CleanupPairing{Node: "voidPayment", When: `state == "open"`},
			Outputs: []Output{{Name: "status", Type: "string"}},
		},
		"createCart":  {Name: "createCart", Adapter: "createCart", Cleanup: CleanupPairing{Node: "voidPayment", When: `status ==`}},
		"voidPayment": {Name: "voidPayment", Adapter: "voidPayment"},
	}}

	var verr *ValidationError
	require.ErrorAs(t, Validate(g), &verr)
	require.Len(t, verr.Errors, 2, "%v", verr.Errors)
	assert.Contains(t, verr.Errors[0], `node "createCart": cleanup when "status ==": parse:`)
	assert.Equal(t, `node "createOrder": cleanup when "state == \"open\"" names "state", which is not an output of "createOrder"`, verr.Errors[1])
}

func TestGenerateDocs_CleanupConditionColumns(t *testing.T) {
	g := &Graph{Version: "1.0.0", Nodes: map[string]*Node{
		"createCart": {Name: "createCart", Cleanup: CleanupPairing{Node: "deleteCart"}},
		"deleteCart": {Name: "deleteCart"},
	}}
	doc := GenerateDocs(g, nil)
	assert.Contains(t, doc, "| Node | Cleans Up | Description |")
	assert.NotContains(t, doc, "Released By", "the columns appear only when a pairing uses them")

	g.Nodes["createPayment"] = &Node{Name: "createPayment", Cleanup: CleanupPairing{
		Node: "voidPayment", When: `status == "a" || status == "b"`, ReleasedBy: []string{"capturePayment"},
	}}
	g.Nodes["voidPayment"] = &Node{Name: "voidPayment"}
	doc = GenerateDocs(g, nil)
	assert.Contains(t, doc, "| Node | Cleans Up | When | Released By | Description |")
	assert.Contains(t, doc, "| voidPayment | createPayment | `status == \"a\" \\|\\| status == \"b\"` | capturePayment |  |")
	assert.Contains(t, doc, "| deleteCart | createCart |  |  |  |")
}
