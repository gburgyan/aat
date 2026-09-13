package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeOptionalOutputProject writes a project where a graph default and a
// plan value each take from: an optional output into a required input.
func writeOptionalOutputProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"aat-project.yaml": "name: carts\ngraph: graph.yaml\ntemplates: templates/\nplans: plans/\n",
		"graph.yaml": `version: "1.0.0"
nodes:
  getCart:
    adapter: getCart
    outputs:
      - name: cartId
        type: string
      - name: couponCode
        type: string
        optional: true
  checkoutCart:
    adapter: checkoutCart
    inputs:
      - name: cartId
        type: string
        default:
          from: getCart.cartId
      - name: couponCode
        type: string
        default:
          from: getCart.couponCode
      - name: note
        type: string
`,
		"templates/getCart.yaml":      "adapter: getCart\nrequest:\n  method: GET\n  path: /carts/current\nresponse:\n  extract:\n    cartId: id\n    couponCode: coupon\n",
		"templates/checkoutCart.yaml": "adapter: checkoutCart\nrequest:\n  method: POST\n  path: /carts/{{cartId}}/checkout?coupon={{couponCode}}&note={{note}}\n",
		"plans/checkout.yaml":         "execution:\n  steps:\n    - id: cart\n      node: getCart\n    - node: checkoutCart\n      dependsOn: [cart]\n      values:\n        note:\n          from: cart.couponCode\n",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return filepath.Join(dir, "aat-project.yaml")
}

func TestValidate_RequiredInputFromOptionalOutputWarns(t *testing.T) {
	manifest := writeOptionalOutputProject(t)

	var buf bytes.Buffer
	code := validateCommand(&validateArgs{ManifestPath: manifest}, &buf)
	out := buf.String()
	assert.Equal(t, 0, code, out)
	assert.Regexp(t, `Graph structure:\s+WARN`, out)
	assert.Contains(t, out, `node "checkoutCart": required input "couponCode" defaults from getCart.couponCode, an optional output`)
	assert.Regexp(t, `Plans:\s+WARN`, out)
	assert.Contains(t, out, `step 1 (checkoutCart): required input "note" takes cart.couponCode, an optional output`)
	assert.Equal(t, 1, strings.Count(out, `required input "couponCode"`), "a graph default is reported once, not again for the plan")
	assert.Contains(t, out, "PASSED with warnings in 2 sections (--strict fails on them)")

	buf.Reset()
	code = validateCommand(&validateArgs{ManifestPath: manifest, Strict: true}, &buf)
	assert.Equal(t, 1, code, buf.String())
	assert.Regexp(t, `Graph structure:\s+FAILED`, buf.String())
	assert.Regexp(t, `Plans:\s+FAILED`, buf.String())
}

func TestGraphValidate_RequiredInputFromOptionalOutput(t *testing.T) {
	dir := filepath.Dir(writeOptionalOutputProject(t))
	args := graphValidateArgs{
		GraphPath:     filepath.Join(dir, "graph.yaml"),
		TemplatesPath: filepath.Join(dir, "templates"),
	}
	assert.Equal(t, 0, graphValidateCommand(&args), "a warning passes")

	args.Strict = true
	assert.Equal(t, 1, graphValidateCommand(&args), "--strict fails on it")
}
