package engine

import (
	"testing"

	"github.com/gburgyan/aat/graph/oas"
	"github.com/gburgyan/aat/validate"
	"github.com/stretchr/testify/assert"
)

func TestBuildSchemaCheck_NilValidation(t *testing.T) {
	cb := buildSchemaCheck(nil)
	assert.Nil(t, cb, "nil OAS validation should produce a nil callback so assertions report Skipped")
}

func TestBuildSchemaCheck_Skipped(t *testing.T) {
	cb := buildSchemaCheck(&oas.ValidationResult{
		OperationID: "createPet",
		Skipped:     true,
		SkipReason:  "no OAS spec path configured",
	})
	ar := cb(validate.MechanicalAssertion{Type: validate.AssertSchema})
	assert.True(t, ar.Passed)
	assert.True(t, ar.Skipped)
	assert.Contains(t, ar.Message, "no OAS spec path configured")
}

func TestBuildSchemaCheck_NoResponse(t *testing.T) {
	cb := buildSchemaCheck(&oas.ValidationResult{OperationID: "createPet"})
	ar := cb(validate.MechanicalAssertion{Type: validate.AssertSchema})
	assert.True(t, ar.Passed)
	assert.True(t, ar.Skipped)
}

func TestBuildSchemaCheck_Valid(t *testing.T) {
	cb := buildSchemaCheck(&oas.ValidationResult{
		OperationID: "createPet",
		Response:    &oas.PayloadResult{Valid: true},
	})
	ar := cb(validate.MechanicalAssertion{Type: validate.AssertSchema})
	assert.True(t, ar.Passed)
	assert.False(t, ar.Skipped)
	assert.Contains(t, ar.Message, "matches OAS schema")
}

func TestBuildSchemaCheck_Invalid(t *testing.T) {
	cb := buildSchemaCheck(&oas.ValidationResult{
		OperationID: "createPet",
		Response: &oas.PayloadResult{
			Valid: false,
			Errors: []oas.SchemaError{
				{Path: "/pets/0/id", Message: "expected integer, got string"},
				{Message: "missing required field name"},
			},
		},
	})
	ar := cb(validate.MechanicalAssertion{Type: validate.AssertSchema})
	assert.False(t, ar.Passed)
	assert.Contains(t, ar.Message, "/pets/0/id: expected integer, got string")
	assert.Contains(t, ar.Message, "missing required field name")
}
