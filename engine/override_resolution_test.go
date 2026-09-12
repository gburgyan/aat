package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordOverrideValue(t *testing.T) {
	resolutions := []ValueResolution{{InputName: "query", Source: "plan_default", FinalValue: "from plan", PoolIndex: -1}}
	resolutions = recordOverrideValue(resolutions, "query", "")
	resolutions = recordOverrideValue(resolutions, "limit", 5)

	require.Len(t, resolutions, 2)
	assert.Equal(t, ValueResolution{InputName: "query", Source: "override_value", RawValue: "", FinalValue: "", PoolIndex: -1}, resolutions[0])
	assert.Equal(t, "override_value", resolutions[1].Source)
	assert.Equal(t, 5, resolutions[1].FinalValue)
}

// TestOverlayValueOverride_RecordedAsResolution checks that a step's resolution
// records, which the archive stores, show the overlay value that was sent
// instead of the plan value it replaced.
func TestOverlayValueOverride_RecordedAsResolution(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.search", &stubAdapter{method: "POST", path: "/search", response: map[string]any{"ok": true}}))
	router := NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{})
	router.AddValueOverride("search", map[string]any{"query": "from overlay"}, nil)

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{
			{Node: "search", Values: map[string]plan.StepValue{"query": {Default: "from plan"}}},
		}},
	}
	result := NewEngine(g, registry, router).Run(context.Background(), p)
	require.Len(t, result.Steps, 1)

	var query *ValueResolution
	for i, r := range result.Steps[0].Resolutions {
		if r.InputName == "query" {
			query = &result.Steps[0].Resolutions[i]
		}
	}
	require.NotNil(t, query)
	assert.Equal(t, "override_value", query.Source)
	assert.Equal(t, "from overlay", query.FinalValue)
}
