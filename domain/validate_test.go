package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_TravelFixturePasses(t *testing.T) {
	kb, err := ParseFile("testdata/valid/travel.yaml")
	require.NoError(t, err)
	require.NotNil(t, kb)
	// ParseFile calls Validate internally; if we got here, validation passed
}

func TestValidate_EmptyKB(t *testing.T) {
	_, err := ParseFile("testdata/invalid/empty.yaml")
	require.Error(t, err)

	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Error(), "at least one concept, type, or value pool")
}

func TestValidate_MissingDescription(t *testing.T) {
	_, err := ParseFile("testdata/invalid/missing_fields.yaml")
	require.Error(t, err)

	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Error(), "concept \"noDescription\": description is required")
}

func TestValidate_MissingAppliesTo(t *testing.T) {
	_, err := ParseFile("testdata/invalid/missing_fields.yaml")
	require.Error(t, err)

	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Error(), "concept \"noAppliesTo\": at least one applies_to entry is required")
}

func TestValidate_MissingTypeDescription(t *testing.T) {
	_, err := ParseFile("testdata/invalid/missing_fields.yaml")
	require.Error(t, err)

	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Error(), "type \"noDesc\": description is required")
}

func TestValidate_MissingTypeFormat(t *testing.T) {
	_, err := ParseFile("testdata/invalid/missing_fields.yaml")
	require.Error(t, err)

	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Error(), "type \"noFormat\": format is required")
}

func TestValidate_BadRegex(t *testing.T) {
	_, err := ParseFile("testdata/invalid/bad_regex.yaml")
	require.Error(t, err)

	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Error(), "invalid validation regex")
}

func TestValidate_PoolRefNotFound(t *testing.T) {
	_, err := ParseFile("testdata/invalid/bad_pool_ref.yaml")
	require.Error(t, err)

	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Error(), "pool references unknown value pool \"nonexistentPool\"")
}

func TestValidate_PoolNoValues(t *testing.T) {
	_, err := ParseFile("testdata/invalid/missing_fields.yaml")
	require.Error(t, err)

	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Error(), "pool \"emptyPool\": must have values or groups")
}

func TestValidate_PoolMissingType(t *testing.T) {
	_, err := ParseFile("testdata/invalid/missing_fields.yaml")
	require.Error(t, err)

	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Error(), "pool \"noType\": type is required")
}

func TestValidate_MultiErrorCollection(t *testing.T) {
	_, err := ParseFile("testdata/invalid/missing_fields.yaml")
	require.Error(t, err)

	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	// Should collect multiple errors, not fail on first
	assert.True(t, len(ve.Errors) >= 6, "expected at least 6 errors, got %d: %s", len(ve.Errors), ve.Error())
}
