package main

import (
	"testing"

	"github.com/gburgyan/aat/internal/primer"
	"github.com/stretchr/testify/assert"
)

func TestDocsPrimer(t *testing.T) {
	code, stdout, stderr := runAAT(t, t.TempDir(), "docs", "primer")
	assert.Equal(t, 0, code, stderr)
	assert.Equal(t, primer.Markdown(), stdout, "the primer, byte for byte")
}
