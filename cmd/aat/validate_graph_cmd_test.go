package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGraphValidate_MissingGraphFlag(t *testing.T) {
	code := graphValidateCommand(&graphValidateArgs{})
	assert.Equal(t, 1, code)
}

func TestGraphValidate_InvalidGraphPath(t *testing.T) {
	code := graphValidateCommand(&graphValidateArgs{
		GraphPath: "testdata/nonexistent.yaml",
	})
	assert.Equal(t, 1, code)
}

func TestGraphValidate_StructuralOnly(t *testing.T) {
	code := graphValidateCommand(&graphValidateArgs{
		GraphPath: "testdata/test_graph.yaml",
	})
	assert.Equal(t, 0, code)
}

func TestGraphValidate_WithOAS_Valid(t *testing.T) {
	code := graphValidateCommand(&graphValidateArgs{
		GraphPath: "testdata/test_graph_with_oas.yaml",
	})
	assert.Equal(t, 0, code)
}

func TestGraphValidate_WithOAS_Errors(t *testing.T) {
	code := graphValidateCommand(&graphValidateArgs{
		GraphPath: "testdata/test_graph_oas_bad_op.yaml",
	})
	assert.Equal(t, 1, code)
}

func TestGraphValidate_WithOAS_WarningsOnly(t *testing.T) {
	code := graphValidateCommand(&graphValidateArgs{
		GraphPath: "testdata/test_graph_oas_warnings.yaml",
	})
	assert.Equal(t, 0, code, "warnings only should exit 0")
}

func TestGraphValidate_WithOAS_WarningsStrict(t *testing.T) {
	code := graphValidateCommand(&graphValidateArgs{
		GraphPath: "testdata/test_graph_oas_warnings.yaml",
		Strict:    true,
	})
	assert.Equal(t, 1, code, "warnings with --strict should exit 1")
}

func TestGraphValidate_OASFlagOverride(t *testing.T) {
	code := graphValidateCommand(&graphValidateArgs{
		GraphPath: "testdata/test_graph_with_oas.yaml",
		OASPath:   "oas/petstore.yaml",
	})
	assert.Equal(t, 0, code)
}

func TestGraphValidate_OASFlagBadPath(t *testing.T) {
	code := graphValidateCommand(&graphValidateArgs{
		GraphPath: "testdata/test_graph.yaml",
		OASPath:   "nonexistent.yaml",
	})
	assert.Equal(t, 1, code)
}

func TestGraphValidate_WithTemplates_OK(t *testing.T) {
	code := graphValidateCommand(&graphValidateArgs{
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/templates",
	})
	assert.Equal(t, 0, code)
}

func TestGraphValidate_WithTemplates_Mismatch(t *testing.T) {
	code := graphValidateCommand(&graphValidateArgs{
		GraphPath:     "testdata/test_graph_bad_outputs.yaml",
		TemplatesPath: "testdata/templates",
	})
	assert.Equal(t, 1, code)
}

func TestGraphValidate_WithTemplates_BadPath(t *testing.T) {
	code := graphValidateCommand(&graphValidateArgs{
		GraphPath:     "testdata/test_graph.yaml",
		TemplatesPath: "testdata/nonexistent_dir",
	})
	assert.Equal(t, 1, code)
}
