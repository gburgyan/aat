package oas

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gburgyan/aat/graph"
	valerrors "github.com/pb33f/libopenapi-validator/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSpecYAML is a minimal OpenAPI 3.0 spec for testing.
const testSpecYAML = `openapi: "3.0.0"
info:
  title: Test API
  version: "1.0"
paths:
  /pets:
    post:
      operationId: createPet
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required:
                - name
              properties:
                name:
                  type: string
                age:
                  type: integer
      responses:
        "201":
          description: Created
          content:
            application/json:
              schema:
                type: object
                required:
                  - id
                  - name
                properties:
                  id:
                    type: integer
                  name:
                    type: string
        "400":
          description: Bad request
  /pets/{petId}:
    get:
      operationId: getPet
      parameters:
        - name: petId
          in: path
          required: true
          schema:
            type: integer
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                required:
                  - id
                  - name
                properties:
                  id:
                    type: integer
                  name:
                    type: string
`

func writeTestSpec(t *testing.T) (dir string, specPath string) {
	t.Helper()
	dir = t.TempDir()
	specPath = filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(testSpecYAML), 0644))
	return dir, specPath
}

func TestSpecCache_LoadAndGet(t *testing.T) {
	_, specPath := writeTestSpec(t)

	cache := NewSpecCache()
	assert.Equal(t, 0, cache.Len())

	err := cache.Load("spec.yaml", specPath)
	require.NoError(t, err)
	assert.Equal(t, 1, cache.Len())

	entry := cache.Get("spec.yaml")
	require.NotNil(t, entry)
	assert.NotNil(t, entry.Document)
	assert.NotNil(t, entry.Model)
	assert.NotNil(t, entry.Validator)
}

func TestSpecCache_GetMissing(t *testing.T) {
	cache := NewSpecCache()
	assert.Nil(t, cache.Get("nonexistent"))
}

func TestSpecCache_LoadError(t *testing.T) {
	cache := NewSpecCache()
	err := cache.Load("bad.yaml", "/nonexistent/path.yaml")
	assert.Error(t, err)
	assert.Equal(t, 0, cache.Len())
}

func TestValidateStep_NoOASRef(t *testing.T) {
	node := &graph.Node{Name: "test"}
	result := ValidateStep(node, "", NewSpecCache(), "POST", "/pets", nil, nil, 200, nil, nil)
	assert.Nil(t, result)
}

func TestValidateStep_NoOperationID(t *testing.T) {
	node := &graph.Node{
		Name: "test",
		OAS:  &graph.OASRef{OperationID: ""},
	}
	result := ValidateStep(node, "", NewSpecCache(), "POST", "/pets", nil, nil, 200, nil, nil)
	assert.Nil(t, result)
}

func TestValidateStep_SpecNotLoaded(t *testing.T) {
	node := &graph.Node{
		Name: "test",
		OAS:  &graph.OASRef{OperationID: "createPet", Spec: "spec.yaml"},
	}
	cache := NewSpecCache()
	result := ValidateStep(node, "", cache, "POST", "/pets", nil, nil, 200, nil, nil)
	require.NotNil(t, result)
	assert.True(t, result.Skipped)
	assert.Contains(t, result.SkipReason, "spec not loaded")
}

func TestValidateStep_NoSpecPath(t *testing.T) {
	node := &graph.Node{
		Name: "test",
		OAS:  &graph.OASRef{OperationID: "createPet"},
	}
	cache := NewSpecCache()
	result := ValidateStep(node, "", cache, "POST", "/pets", nil, nil, 200, nil, nil)
	require.NotNil(t, result)
	assert.True(t, result.Skipped)
	assert.Contains(t, result.SkipReason, "no OAS spec path")
}

func TestValidateStep_OperationNotFound(t *testing.T) {
	_, specPath := writeTestSpec(t)
	cache := NewSpecCache()
	require.NoError(t, cache.Load("spec.yaml", specPath))

	node := &graph.Node{
		Name: "test",
		OAS:  &graph.OASRef{OperationID: "nonexistentOp", Spec: "spec.yaml"},
	}
	result := ValidateStep(node, "", cache, "POST", "/pets", nil, nil, 200, nil, nil)
	require.NotNil(t, result)
	assert.True(t, result.Skipped)
	assert.Contains(t, result.SkipReason, "not found")
}

func TestValidateStep_ValidRequestAndResponse(t *testing.T) {
	_, specPath := writeTestSpec(t)
	cache := NewSpecCache()
	require.NoError(t, cache.Load("spec.yaml", specPath))

	node := &graph.Node{
		Name: "test",
		OAS:  &graph.OASRef{OperationID: "createPet", Spec: "spec.yaml"},
	}

	reqBody := []byte(`{"name": "Fido", "age": 3}`)
	respBody := []byte(`{"id": 1, "name": "Fido"}`)
	respHeaders := http.Header{"Content-Type": []string{"application/json"}}

	result := ValidateStep(node, "", cache, "POST", "/pets", nil, reqBody, 201, respHeaders, respBody)
	require.NotNil(t, result)
	assert.False(t, result.Skipped)
	assert.Equal(t, "createPet", result.OperationID)
	assert.False(t, result.HasErrors())
	assert.Equal(t, 0, result.ErrorCount())

	if result.Request != nil {
		assert.True(t, result.Request.Valid)
	}
	if result.Response != nil {
		assert.True(t, result.Response.Valid)
	}
}

func TestValidateStep_InvalidResponse_MissingRequiredField(t *testing.T) {
	_, specPath := writeTestSpec(t)
	cache := NewSpecCache()
	require.NoError(t, cache.Load("spec.yaml", specPath))

	node := &graph.Node{
		Name: "test",
		OAS:  &graph.OASRef{OperationID: "createPet", Spec: "spec.yaml"},
	}

	reqBody := []byte(`{"name": "Fido"}`)
	// Missing required "id" field
	respBody := []byte(`{"name": "Fido"}`)
	respHeaders := http.Header{"Content-Type": []string{"application/json"}}

	result := ValidateStep(node, "", cache, "POST", "/pets", nil, reqBody, 201, respHeaders, respBody)
	require.NotNil(t, result)
	assert.False(t, result.Skipped)
	assert.True(t, result.HasErrors())
	assert.Greater(t, result.ErrorCount(), 0)

	require.NotNil(t, result.Response)
	assert.False(t, result.Response.Valid)
	assert.NotEmpty(t, result.Response.Errors)
}

func TestValidateStep_NoRequestBody(t *testing.T) {
	_, specPath := writeTestSpec(t)
	cache := NewSpecCache()
	require.NoError(t, cache.Load("spec.yaml", specPath))

	node := &graph.Node{
		Name: "test",
		OAS:  &graph.OASRef{OperationID: "getPet", Spec: "spec.yaml"},
	}

	respBody := []byte(`{"id": 1, "name": "Fido"}`)
	respHeaders := http.Header{"Content-Type": []string{"application/json"}}

	result := ValidateStep(node, "", cache, "GET", "/pets/1", nil, nil, 200, respHeaders, respBody)
	require.NotNil(t, result)
	assert.False(t, result.Skipped)
	assert.Nil(t, result.Request) // no request body to validate
}

func TestValidateStep_GraphOASFallback(t *testing.T) {
	_, specPath := writeTestSpec(t)
	cache := NewSpecCache()
	require.NoError(t, cache.Load("spec.yaml", specPath))

	// Node doesn't specify spec, but graph OAS does
	node := &graph.Node{
		Name: "test",
		OAS:  &graph.OASRef{OperationID: "createPet"},
	}

	reqBody := []byte(`{"name": "Fido"}`)
	respBody := []byte(`{"id": 1, "name": "Fido"}`)
	respHeaders := http.Header{"Content-Type": []string{"application/json"}}

	result := ValidateStep(node, "spec.yaml", cache, "POST", "/pets", nil, reqBody, 201, respHeaders, respBody)
	require.NotNil(t, result)
	assert.False(t, result.Skipped)
	assert.Equal(t, "createPet", result.OperationID)
}

func TestValidateStep_NonJSONRequestBody(t *testing.T) {
	_, specPath := writeTestSpec(t)
	cache := NewSpecCache()
	require.NoError(t, cache.Load("spec.yaml", specPath))

	node := &graph.Node{
		Name: "test",
		OAS:  &graph.OASRef{OperationID: "createPet", Spec: "spec.yaml"},
	}

	// Non-JSON request body
	reqBody := []byte(`<pet><name>Fido</name></pet>`)
	respBody := []byte(`{"id": 1, "name": "Fido"}`)
	respHeaders := http.Header{"Content-Type": []string{"application/json"}}

	result := ValidateStep(node, "", cache, "POST", "/pets", nil, reqBody, 201, respHeaders, respBody)
	require.NotNil(t, result)
	// Request validation should be skipped for non-JSON
	assert.Nil(t, result.Request)
}

func TestValidationResult_NilSafe(t *testing.T) {
	var r *ValidationResult
	assert.False(t, r.HasErrors())
	assert.Equal(t, 0, r.ErrorCount())
}

func TestConvertValidationErrors_CompilationWarnings_NotValid(t *testing.T) {
	// When all errors are compilation failures, Valid should be false
	// (not validated, not "valid").
	errs := []*valerrors.ValidationError{
		{Message: "schema for /foo failed schema compilation"},
	}
	result := convertValidationErrors(errs)
	assert.False(t, result.Valid, "should not report as valid when compilation warnings exist")
	assert.Empty(t, result.Errors)
	assert.Len(t, result.CompilationWarnings, 1)
}

func TestConvertValidationErrors_NoErrors_NoWarnings_Valid(t *testing.T) {
	result := convertValidationErrors(nil)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
	assert.Empty(t, result.CompilationWarnings)
}

func TestConvertValidationErrors_MixedErrorsAndWarnings(t *testing.T) {
	errs := []*valerrors.ValidationError{
		{Message: "schema for /foo failed schema compilation"},
		{Message: "missing required field", SpecPath: "/bar"},
	}
	result := convertValidationErrors(errs)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Len(t, result.CompilationWarnings, 1)
}

func TestIsJSON(t *testing.T) {
	assert.True(t, isJSON([]byte(`{"key": "val"}`)))
	assert.True(t, isJSON([]byte(`[1, 2, 3]`)))
	assert.True(t, isJSON([]byte(`  { "key": "val" }`)))
	assert.False(t, isJSON([]byte(`<xml/>`)))
	assert.False(t, isJSON([]byte(``)))
	assert.False(t, isJSON(nil))
}
