package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
)

func TestValidateCleanupConditions(t *testing.T) {
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"createPayment": {
			Name:    "createPayment",
			Cleanup: graph.CleanupPairing{Node: "voidPayment", When: `status == "authorized" && amount.value > 0`},
			Outputs: []graph.Output{{Name: "status", Type: "string"}, {Name: "amount", Type: "object"}},
		},
		"createOrder": {
			Name:    "createOrder",
			Cleanup: graph.CleanupPairing{Node: "cancelOrder", When: `state == "open"`},
			Outputs: []graph.Output{{Name: "status", Type: "string"}},
		},
		"createCart": {
			Name:    "createCart",
			Cleanup: graph.CleanupPairing{Node: "deleteCart", When: `status ==`},
		},
		"createInvoice": {Name: "createInvoice", Cleanup: graph.CleanupPairing{Node: "voidInvoice"}},
	}}

	errs := ValidateCleanupConditions(g)
	require.Len(t, errs, 2, "%v", errs)
	assert.Contains(t, errs[0], `node "createCart": cleanup when "status ==": parse:`)
	assert.Equal(t, `node "createOrder": cleanup when "state == \"open\"" names "state", which is not an output of "createOrder"`, errs[1])
}
