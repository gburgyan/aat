package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPrintSections_RepeatedErrorPrintedOnce: an error repeated word for word
// in a section is printed once, while the same detail under another file's
// heading is kept.
func TestPrintSections_RepeatedErrorPrintedOnce(t *testing.T) {
	var out bytes.Buffer
	printSections(&out, []sectionResult{{
		Name:   "Plans",
		Status: "FAILED",
		Errors: []string{
			"plans/a.yaml: plan validation failed:\n  - step 0 (x): bad",
			"plans/b.yaml: plan validation failed:\n  - step 0 (x): bad",
			"plans/a.yaml: plan validation failed:\n  - step 0 (x): bad",
		},
	}})

	assert.Equal(t, "Plans: FAILED\n"+
		"  plans/a.yaml: plan validation failed:\n"+
		"    - step 0 (x): bad\n"+
		"  plans/b.yaml: plan validation failed:\n"+
		"    - step 0 (x): bad\n", out.String())
}
