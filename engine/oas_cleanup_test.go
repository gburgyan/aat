package engine

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/graph/oas"
	"github.com/gburgyan/aat/plan"
)

const cleanupOASSpec = `openapi: "3.0.3"
info: {title: cleanup, version: "1"}
paths:
  /pets:
    post:
      operationId: createPet
      responses:
        "201":
          description: Created
          content:
            application/json:
              schema:
                type: object
                required: [id]
                properties:
                  id: {type: integer}
  /pets/{petId}:
    delete:
      operationId: deletePet
      parameters:
        - {name: petId, in: path, required: true, schema: {type: string}}
      responses:
        "200":
          description: Deleted
          content:
            application/json:
              schema:
                type: object
                required: [deleted]
                properties:
                  deleted: {type: boolean}
`

// cleanupOASEngine builds an engine whose createPet is cleaned up by deletePet,
// which answers with a body its spec doesn't allow, and whose deletePet has a
// cleanup of its own, auditPet, outside the spec.
func cleanupOASEngine(t *testing.T, strict bool) (*Engine, *plan.Plan) {
	t.Helper()
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(cleanupOASSpec), 0o644))
	cache := oas.NewSpecCache()
	require.NoError(t, cache.Load("spec.yaml", specPath))

	srv := newChainServer(t, map[string]chainResponse{
		"/pets":   {status: http.StatusCreated, body: `{"id": 1}`},
		"/pets/1": {status: http.StatusOK, body: `{"deleted": "yes"}`},
	})
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"createPet": {
			Name: "createPet", Adapter: "oascleanup.createPet", Cleanup: graph.CleanupPairing{Node: "deletePet"},
			OAS:     &graph.OASRef{OperationID: "createPet", Spec: "spec.yaml"},
			Outputs: []graph.Output{{Name: "petId", Type: "string"}},
		},
		"deletePet": {
			Name: "deletePet", Adapter: "oascleanup.deletePet", Cleanup: graph.CleanupPairing{Node: "auditPet"},
			OAS:    &graph.OASRef{OperationID: "deletePet", Spec: "spec.yaml"},
			Inputs: []graph.Input{{Name: "petId", Type: "string"}},
		},
		"auditPet": {
			Name: "auditPet", Adapter: "oascleanup.auditPet",
			Inputs: []graph.Input{{Name: "petId", Type: "string"}},
		},
	}}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("oascleanup.createPet", &stubAdapter{method: "POST", path: "/pets", response: map[string]any{"petId": "1"}}))
	require.NoError(t, registry.Register("oascleanup.deletePet", &stubAdapter{method: "DELETE", path: "/pets/1", response: map[string]any{}}))
	require.NoError(t, registry.Register("oascleanup.auditPet", &stubAdapter{method: "POST", path: "/audit", response: map[string]any{}}))
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(srv.URL), &adapter.EnvironmentConfig{})).
		WithOASSpecs(cache, "", strict)

	p := &plan.Plan{
		Metadata:  plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{{Node: "createPet"}}},
	}
	return eng, p
}

func TestCleanupOAS_StrictFailsTheStepAndTheChainRuns(t *testing.T) {
	eng, p := cleanupOASEngine(t, true)
	result := eng.Run(context.Background(), p)

	require.Len(t, result.CleanupResults, 2, "error: %v", result.Error)
	deletePet := result.CleanupResults[0]
	assert.Equal(t, "deletePet", deletePet.Node)
	require.NotNil(t, deletePet.OASValidation)
	assert.True(t, deletePet.OASValidation.HasErrors())
	require.Error(t, deletePet.Error)
	assert.Contains(t, deletePet.Error.Error(), "cleanup step deletePet: OAS validation failed in strict mode")
	assert.Equal(t, "auditPet", result.CleanupResults[1].Node, "the chain goes on, since the exchange released the resource")
}

func TestCleanupOAS_WarnModeOnlyRecords(t *testing.T) {
	eng, p := cleanupOASEngine(t, false)
	result := eng.Run(context.Background(), p)

	require.Len(t, result.CleanupResults, 2, "error: %v", result.Error)
	require.NotNil(t, result.CleanupResults[0].OASValidation)
	assert.True(t, result.CleanupResults[0].OASValidation.HasErrors())
	assert.NoError(t, result.CleanupResults[0].Error)
}
