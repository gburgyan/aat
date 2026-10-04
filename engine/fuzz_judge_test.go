package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// buildThingsEngine serves POST /things, which answers a code of "html" with
// a 200 HTML page, "bodyerr" with a 200 whose body is an error, and anything
// else with a 201 and the thing's id; DELETE /things/{id} counts deletes.
func buildThingsEngine(t *testing.T) (*Engine, *atomic.Int32) {
	t.Helper()
	var deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes.Add(1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch body.Code {
		case "html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>blocked</html>"))
		case "bodyerr":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"error":"no"}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"t1"}`))
		}
	}))
	t.Cleanup(server.Close)

	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"createThing": {Name: "createThing", Adapter: "t.createThing",
			Inputs:         []graph.Input{{Name: "code", Type: "string", Default: &graph.InputDefault{Value: "ok"}}},
			Outputs:        []graph.Output{{Name: "id", Type: "string"}},
			Cleanup:        graph.CleanupPairing{Node: "deleteThing"},
			ErrorDetection: []graph.ErrorDetectionRule{{Path: "error", Rule: "exists"}}},
		"deleteThing": {Name: "deleteThing", Adapter: "t.deleteThing", Inputs: []graph.Input{{Name: "id", Type: "string"}}},
	}}
	registry := adapter.NewRegistry()
	for name, tmpl := range map[string]adapter.Template{
		"t.createThing": {Request: adapter.TemplateRequest{Method: "POST", Path: "/things",
			Headers: map[string]string{"Content-Type": "application/json"}, Body: `{"code": "{{code}}"}`},
			Response: adapter.TemplateResponse{Extract: map[string]adapter.ExtractRule{"id": {Path: "id"}}}},
		"t.deleteThing": {Request: adapter.TemplateRequest{Method: "DELETE", Path: "/things/{{id}}"}},
	} {
		tmpl.Adapter, tmpl.Protocol = name, "http"
		require.NoError(t, registry.Register(name, adapter.NewTemplateAdapter(tmpl)))
	}
	return NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{})), &deletes
}

func code(id, mode, value string) plan.PinnedFuzzCase {
	return plan.PinnedFuzzCase{ID: id, Mode: mode, Input: "code", Value: value}
}

// TestFuzz_JudgedOnTheResponse checks that a case is judged on the response
// it got: a success whose outputs can't be read is still a success, and one
// whose body the graph reads as an error is a refusal.
func TestFuzz_JudgedOnTheResponse(t *testing.T) {
	eng, deletes := buildThingsEngine(t)
	p := &plan.Plan{Metadata: plan.Metadata{GraphVersion: "1.0.0"}, Execution: plan.Execution{Steps: []plan.Step{
		{ID: "thing", Node: "createThing", FuzzSettings: &plan.FuzzSettings{Pinned: []plan.PinnedFuzzCase{
			code("code.html", plan.FuzzNegative, "html"),
			code("code.refused", plan.FuzzNegative, "bodyerr"),
			code("code.refused-valid", plan.FuzzPositive, "bodyerr"),
		}}},
	}}}
	result := eng.Run(context.Background(), p)
	require.NoError(t, result.Error)
	got := findings(result)
	assert.Equal(t, FindingAcceptedInvalid, got["code.html"], "a 200 is an answer, even one aat can't read")
	assert.Equal(t, "", got["code.refused"], "an error in the body is a refusal")
	assert.Equal(t, FindingRejectedValid, got["code.refused-valid"])
	for _, s := range fuzzSteps(result) {
		if s.Fuzz.Case.ID == "code.html" {
			assert.NotEmpty(t, s.OutputsError)
		}
	}
	assert.Equal(t, int32(1), deletes.Load(), "only the happy path's thing is cleaned up: the others have no id to delete")
}

// TestFuzz_NodeTargetSkipsMutationSiblings checks that --fuzz naming a node
// fuzzes the plan's own step of it, not the mutation sibling made from it.
func TestFuzz_NodeTargetSkipsMutationSiblings(t *testing.T) {
	server := buggyQuantityServer(t)
	p := quantityPlan()
	p.Execution.Steps[0].Mutations = []plan.Mutation{{Name: "neg", Set: map[string]any{"quantity": -5}, ExpectStatus: plan.ExpectedStatuses{{Code: 400}}}}
	result := buildQuantityEngine(t, server.URL).WithFuzz(&FuzzConfig{Targets: []string{"addItem"}, Cases: []string{"quantity.above-max"}}).
		Run(context.Background(), p)
	require.NoError(t, result.Error)
	var targets []string
	for _, s := range fuzzSteps(result) {
		targets = append(targets, s.Fuzz.Case.Target)
	}
	assert.Equal(t, []string{"add"}, targets)
}

// TestFuzz_CaseFilterInABatchIgnoresPlanBlocks checks that --fuzz-case over
// a batch plan without the --fuzz target leaves the plan's own block alone.
func TestFuzz_CaseFilterInABatchIgnoresPlanBlocks(t *testing.T) {
	server := buggyQuantityServer(t)
	p := quantityPlan()
	p.Execution.Steps[0].FuzzSettings = &plan.FuzzSettings{Only: []string{"quantity.above-max"}}
	result := buildQuantityEngine(t, server.URL).WithFuzz(&FuzzConfig{Targets: []string{"checkout"}, AllowNoTarget: true, Cases: []string{"x.y"}}).
		Run(context.Background(), p)
	require.NoError(t, result.Error)
	assert.Equal(t, map[string]string{"quantity.above-max": FindingAcceptedInvalid}, findings(result))
}

// TestFuzz_PinnedNull checks that a pinned null is sent as null, not as the
// input's default.
func TestFuzz_PinnedNull(t *testing.T) {
	server := buggyQuantityServer(t)
	p := quantityPlan()
	p.Execution.Steps[0].FuzzSettings = &plan.FuzzSettings{Pinned: []plan.PinnedFuzzCase{
		{ID: "quantity.null", Mode: plan.FuzzNegative, Input: "quantity"},
	}}
	result := buildQuantityEngine(t, server.URL).Run(context.Background(), p)
	require.NoError(t, result.Error)
	steps := fuzzSteps(result)
	require.Len(t, steps, 1)
	assert.JSONEq(t, `{"quantity": null}`, string(steps[0].Request.Body))
	assert.Equal(t, "", steps[0].Fuzz.Finding)
}
