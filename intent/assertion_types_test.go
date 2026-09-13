package intent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

func TestLoadWorkflowTemplate_UnknownAssertionType(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "checkout.yaml"), []byte(`execution:
  steps:
    - node: createCart
      assertions:
        mechanical:
          - type: fieldEqual
            path: cartId
            value: cart-1
`), 0o644))
	g := &graph.Graph{Nodes: map[string]*graph.Node{"createCart": {Name: "createCart"}}}

	_, err := LoadWorkflowTemplate("checkout.yaml", dir, g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `workflow template step "createCart": assertion 0 has unknown type "fieldEqual"`)
}

func TestReconstitute_UnknownOverrideAssertionType(t *testing.T) {
	recipe := &plan.Recipe{
		Kind:      "recipe",
		Selection: plan.RecipeSelection{Workflow: "Checkout Plain"},
		Overrides: plan.RecipeOverrides{
			Values:     map[string]any{"checkout.giftMessage": "Happy birthday"},
			Assertions: map[string][]plan.RecipeAssertion{"checkout": {{Type: "fieldEqual", Path: "status", Value: "paid"}}},
		},
	}

	_, err := Reconstitute(recipe, autowireTestGraph(), ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `override assertion 0 for step "checkout" has unknown type "fieldEqual"`)
}
