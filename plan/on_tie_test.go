package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
)

func TestValidate_OnTie(t *testing.T) {
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"listProducts": {Name: "listProducts", Outputs: []graph.Output{{Name: "products", Type: "string[]"}}},
		"addItem": {Name: "addItem", Inputs: []graph.Input{
			{Name: "sku", Type: "string"},
			{Name: "backupSku", Type: "string"},
			{Name: "giftSku", Type: "string"},
		}},
	}}
	p := &Plan{Execution: Execution{Steps: []Step{
		{Node: "listProducts"},
		{
			Node:      "addItem",
			DependsOn: []string{"listProducts"},
			Selections: map[string]StepSelection{
				"cheapest": {From: "listProducts.products", Strategy: "min", SortField: "price", OnTie: "fail"},
				"newest":   {From: "listProducts.products", OnTie: "first"},
			},
			Values: map[string]StepValue{
				"sku":       {FromSelection: "cheapest.sku"},
				"backupSku": {From: "listProducts.products", Select: &SelectionConfig{Strategy: "max", SortField: "price", Field: "sku", OnTie: "random"}},
				"giftSku":   {FromSelection: "newest.sku"},
			},
		},
	}}}

	err := Validate(p, g)
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, `step 1 (addItem): selection "newest": onTie applies only to the min and max strategies, not "first"`)
	assert.Contains(t, msg, `step 1 (addItem): select for "backupSku": unknown onTie "random" (use first or fail)`)
	assert.NotContains(t, msg, `"cheapest": onTie`, "fail is valid on min")
}
