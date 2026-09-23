package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

const poolRefDomain = `valuePools:
  airports:
    description: Airports
    type: string
    groups:
      us: [JFK, LAX]
      eu: [CDG]
`

func TestValidate_PoolRefs(t *testing.T) {
	manifestWithDomain := "name: layered\ngraph: graph.yaml\ntemplates: templates/\nworkflows: workflows/\nlayers: layers/\nplans: plans/\ndomain: domain.yaml\n"

	t.Run("known pools pass", func(t *testing.T) {
		manifest := writeLayeredProject(t, map[string]string{
			"aat-project.yaml": manifestWithDomain,
			"domain.yaml":      poolRefDomain,
			"layers/west.yaml": "name: west\ninputs:\n  origin: {poolRef: airports.us}\n",
		})
		var buf bytes.Buffer
		code := validateCommand(&validateArgs{ManifestPath: manifest}, &buf)
		assert.Equal(t, 0, code, buf.String())
		assert.Regexp(t, `Pool refs:\s+OK \(1 reference\)`, buf.String())
	})

	t.Run("an unknown group fails", func(t *testing.T) {
		manifest := writeLayeredProject(t, map[string]string{
			"aat-project.yaml": manifestWithDomain,
			"domain.yaml":      poolRefDomain,
			"layers/west.yaml": "name: west\ninputs:\n  origin: {poolRef: airports.asia}\n",
		})
		var buf bytes.Buffer
		code := validateCommand(&validateArgs{ManifestPath: manifest}, &buf)
		assert.Equal(t, 1, code, buf.String())
		assert.Contains(t, buf.String(), `layer west: origin: poolRef "airports.asia": value pool "airports" has no group "asia" (it has eu, us)`)
	})

	t.Run("no domain file fails", func(t *testing.T) {
		manifest := writeLayeredProject(t, map[string]string{
			"layers/west.yaml": "name: west\ninputs:\n  origin: {poolRef: airports}\n",
		})
		var buf bytes.Buffer
		code := validateCommand(&validateArgs{ManifestPath: manifest}, &buf)
		assert.Equal(t, 1, code, buf.String())
		assert.Contains(t, buf.String(), "needs a domain file")
	})

	t.Run("no poolRef, no section", func(t *testing.T) {
		manifest := writeLayeredProject(t, nil)
		var buf bytes.Buffer
		validateCommand(&validateArgs{ManifestPath: manifest}, &buf)
		assert.NotContains(t, buf.String(), "Pool refs")
	})
}
