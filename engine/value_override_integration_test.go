package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOverlayValueOverride_InjectsMalformedValue verifies that overlay values
// replace plan-supplied values in the outgoing request, and that an overlay
// expectFailure flips the step's pass/fail logic — the primitive for authoring
// negative tests without editing the plan.
func TestOverlayValueOverride_InjectsMalformedValue(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"search": {
				Name:    "search",
				Adapter: "test.search",
				Inputs: []graph.Input{
					{Name: "query", Type: "string"},
				},
			},
		},
	}

	var seenBodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		seenBodies = append(seenBodies, parsed)

		w.Header().Set("Content-Type", "application/json")
		if q, _ := parsed["query"].(string); q == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"query is required"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.search", &stubAdapter{
		method:   "POST",
		path:     "/search",
		response: map[string]any{"ok": true},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	router := NewExecutorRouter(executor, &adapter.EnvironmentConfig{})

	// Overlay: rewrite query to empty, declare expected 400.
	router.AddValueOverride("search", map[string]any{"query": ""}, &plan.ExpectFailure{Status: []int{400}})

	engine := NewEngine(g, registry, router)

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "search",
					Values: map[string]plan.StepValue{
						"query": {Default: "this should be overwritten"},
					},
				},
			},
		},
	}

	result := engine.Run(context.Background(), p)

	assert.Equal(t, OutcomePassed, result.Outcome, "expected failure matched — run passes")
	require.Len(t, result.Steps, 1)
	sr := result.Steps[0]
	assert.Equal(t, 400, sr.StatusCode)
	require.NotNil(t, sr.ExpectFailure)
	assert.True(t, sr.ExpectFailure.Passed, "overlay expectFailure should be consulted when step declares none")

	require.Len(t, seenBodies, 1)
	assert.Equal(t, "", seenBodies[0]["query"], "overlay value must replace plan default in outgoing request")
}

// TestOverlayValueOverride_StepExpectFailureWins verifies that a plan-declared
// expectFailure is not overwritten by an overlay expectFailure.
func TestOverlayValueOverride_StepExpectFailureWins(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"search": {
				Name:    "search",
				Adapter: "test.search",
				Inputs:  []graph.Input{{Name: "query", Type: "string"}},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"nf"}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.search", &stubAdapter{
		method: "POST", path: "/search",
		response: map[string]any{},
	}))

	router := NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{})
	// Overlay declares 400 expected; plan step declares 404 expected — plan wins.
	router.AddValueOverride("search", nil, &plan.ExpectFailure{Status: []int{400}})

	engine := NewEngine(g, registry, router)

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node:          "search",
					Values:        map[string]plan.StepValue{"query": {Default: "x"}},
					ExpectFailure: &plan.ExpectFailure{Status: []int{404}},
				},
			},
		},
	}

	result := engine.Run(context.Background(), p)
	require.Len(t, result.Steps, 1)
	sr := result.Steps[0]
	require.NotNil(t, sr.ExpectFailure)
	assert.Equal(t, []int{404}, sr.ExpectFailure.ExpectedStatuses, "plan step expectFailure takes precedence over overlay")
	assert.True(t, sr.ExpectFailure.Passed)
}
