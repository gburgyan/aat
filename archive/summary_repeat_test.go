package archive

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildRunSummary_CountsEveryRequestOfARepeatedStep checks that OpenAPI
// counts cover each request a repeated step sent, not only its last, whose
// validation the step's own record repeats.
func TestBuildRunSummary_CountsEveryRequestOfARepeatedStep(t *testing.T) {
	valid := &OASValidationRecord{Request: &OASPayloadRecord{Valid: true}, Response: &OASPayloadRecord{Valid: true}}
	invalid := &OASValidationRecord{
		Request:  &OASPayloadRecord{Valid: true},
		Response: &OASPayloadRecord{Errors: []OASSchemaError{{Path: "/items", Message: "expected array"}}},
	}
	a := &Archive{
		Metadata: ArchiveMetadata{OASValidation: "auto"},
		Steps: []StepRecord{
			{Node: "createSearch", OASValidation: valid},
			{
				Node:          "getSearch",
				OASValidation: valid,
				Iterations:    []IterationRecord{{Index: 1, OASValidation: invalid}, {Index: 2, OASValidation: valid}},
				RepeatStop:    "until",
			},
		},
	}

	summary := BuildRunSummary(a)

	require.NotNil(t, summary.OAS)
	assert.Equal(t, OASSummary{Mode: "auto", ValidatedRequests: 3, ValidatedResponses: 3, Violations: 1}, *summary.OAS)
	assert.Equal(t, map[string]int{"oas": 1}, summary.Issues)
}
