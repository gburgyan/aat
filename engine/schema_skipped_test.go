package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph/oas"
	"github.com/gburgyan/aat/validate"
)

// TestBuildSchemaCheck_SkippedResponse checks that a schema assertion on a
// response the validator couldn't check is skipped with the reason, rather than
// failed with an empty message.
func TestBuildSchemaCheck_SkippedResponse(t *testing.T) {
	check := buildSchemaCheck(&oas.ValidationResult{
		OperationID: "getOrder",
		Response: &oas.PayloadResult{
			CompilationWarnings: []string{"schema for /orders failed schema compilation"},
			Skipped:             true,
			SkipReason:          "the schema could not be compiled: schema for /orders failed schema compilation",
		},
	})
	require.NotNil(t, check)

	result := check(validate.MechanicalAssertion{})
	assert.True(t, result.Passed)
	assert.True(t, result.Skipped)
	assert.Equal(t, "schema validation skipped: the schema could not be compiled: schema for /orders failed schema compilation", result.Message)
}

func TestConvertOASPayload_Skipped(t *testing.T) {
	rec := convertOASPayload(&oas.PayloadResult{Skipped: true, SkipReason: "application/xml request bodies are not validated"})
	assert.True(t, rec.Skipped)
	assert.Equal(t, "application/xml request bodies are not validated", rec.SkipReason)
}
