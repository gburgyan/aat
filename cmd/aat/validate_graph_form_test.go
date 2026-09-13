package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGraphValidate_FormInputsNamedApartFromFields checks that inputs a form
// template sends as fields of other names, such as order sent as orderId and a
// map input sent as metadata, pass --strict: each counts as its field for the
// unknown-input and required-field checks.
func TestGraphValidate_FormInputsNamedApartFromFields(t *testing.T) {
	code := graphValidateCommand(&graphValidateArgs{
		GraphPath:     "testdata/test_graph_form_renamed.yaml",
		TemplatesPath: "testdata/templates_form_renamed",
		Strict:        true,
	})
	assert.Equal(t, 0, code)
}

// TestGraphValidate_StringFormBodyMatchesInputNames checks that the matching is
// for form: fields only: a body string that sends the same inputs still fails
// --strict, since the check reads their names.
func TestGraphValidate_StringFormBodyMatchesInputNames(t *testing.T) {
	code := graphValidateCommand(&graphValidateArgs{
		GraphPath:     "testdata/test_graph_form_renamed.yaml",
		TemplatesPath: "testdata/templates_form_renamed_string",
		Strict:        true,
	})
	assert.Equal(t, 1, code)
}
