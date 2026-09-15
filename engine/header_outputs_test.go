package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// TestEngine_Run_HeaderOutputsTakeTheirType reads outputs from the headers of a
// response with no body and compares one as a number in a predicate.
func TestEngine_Run_HeaderOutputsTakeTheirType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("RateLimit-Remaining", "59")
		w.Header().Set("X-Request-Id", "req-123")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"deleteCart": {Name: "deleteCart", Adapter: "test.deleteCart", Outputs: []graph.Output{
			{Name: "rateLimitRemaining", Type: "integer"},
			{Name: "requestId", Type: "string"},
		}},
	}}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.deleteCart", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "deleteCart",
		Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "DELETE", Path: "/carts/1"},
		Response: adapter.TemplateResponse{Extract: map[string]adapter.ExtractRule{
			"rateLimitRemaining": {Header: "ratelimit-remaining"},
			"requestId":          {Header: "x-request-id"},
		}},
	})))
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(srv.URL), &adapter.EnvironmentConfig{}))
	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{{
			Node: "deleteCart",
			Assertions: &plan.Assertions{Mechanical: []plan.MechanicalAssertion{
				{Type: "predicate", Expr: `rateLimitRemaining > 5 && requestId == "req-123"`},
			}},
		}}},
	}

	result := eng.Run(context.Background(), p)

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, 59, result.Steps[0].Outputs["rateLimitRemaining"])
	assert.Equal(t, "req-123", result.Steps[0].Outputs["requestId"])
}
