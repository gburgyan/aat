package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// runFilterRefs creates customer cus_2, lists an event about cus_1 and one
// about cus_2, and runs last as the step that reads an event.
func runFilterRefs(t *testing.T, last plan.Step) *RunResult {
	t.Helper()
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"createCustomer": {Name: "createCustomer", Adapter: "filters.create", Outputs: []graph.Output{
			{Name: "customerId", Type: "string"},
			{Name: "coupon", Type: "string", Optional: true},
		}},
		"listEvents": {Name: "listEvents", Adapter: "filters.list", Outputs: []graph.Output{
			{Name: "events", Type: "event[]", ElementFields: []graph.Field{{Name: "id", Type: "string"}, {Name: "objectId", Type: "string"}}},
		}},
		"getEvent": {Name: "getEvent", Adapter: "filters.get",
			Inputs:  []graph.Input{{Name: "eventId", Type: "string"}},
			Outputs: []graph.Output{{Name: "eventType", Type: "string"}},
		},
	}}
	srv := newChainServer(t, nil)
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("filters.create", &stubAdapter{method: "POST", path: "/customers", response: map[string]any{"customerId": "cus_2"}}))
	require.NoError(t, registry.Register("filters.list", &stubAdapter{method: "GET", path: "/events", response: map[string]any{"events": []any{
		map[string]any{"id": "evt_1", "objectId": "cus_1"},
		map[string]any{"id": "evt_2", "objectId": "cus_2"},
	}}}))
	require.NoError(t, registry.Register("filters.get", &stubAdapter{method: "GET", path: "/event", response: map[string]any{"eventType": "customer.created"}}))
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(srv.URL), &adapter.EnvironmentConfig{}))

	last.ID, last.Node, last.DependsOn = "event", "getEvent", []string{"create", "events"}
	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{
			{ID: "create", Node: "createCustomer"},
			{ID: "events", Node: "listEvents", DependsOn: []string{"create"}},
			last,
		}},
	}
	return eng.Run(context.Background(), p)
}

func TestSelectionFilter_ReadsEarlierStepOutput(t *testing.T) {
	t.Run("a value's select", func(t *testing.T) {
		result := runFilterRefs(t, plan.Step{Values: map[string]plan.StepValue{
			"eventId": {From: "events.events", Select: &plan.SelectionConfig{Strategy: "first", Field: "id", Filter: `objectId == "{{create.customerId}}"`}},
		}})

		require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
		event := result.Steps[2]
		assert.Equal(t, "evt_2", event.Inputs["eventId"])
		require.Len(t, event.Selections, 1)
		assert.Equal(t, `objectId == "cus_2"`, event.Selections[0].FilterExpr, "the decision records the filter as it compared")
	})

	t.Run("a named selection", func(t *testing.T) {
		result := runFilterRefs(t, plan.Step{
			Selections: map[string]plan.StepSelection{"about": {From: "events.events", Filter: `objectId == "{{create.customerId}}"`}},
			Values:     map[string]plan.StepValue{"eventId": {FromSelection: "about.id"}},
		})

		require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
		event := result.Steps[2]
		assert.Equal(t, "evt_2", event.Inputs["eventId"])
		require.NotEmpty(t, event.Selections)
		assert.Equal(t, `objectId == "cus_2"`, event.Selections[0].FilterExpr)
	})

	t.Run("an output the earlier step didn't produce", func(t *testing.T) {
		result := runFilterRefs(t, plan.Step{Values: map[string]plan.StepValue{
			"eventId": {From: "events.events", Select: &plan.SelectionConfig{Strategy: "first", Field: "id", Filter: `objectId == "{{create.coupon}}"`}},
		}})

		require.NotEqual(t, OutcomePassed, result.Outcome)
		require.Len(t, result.Steps, 3)
		require.Error(t, result.Steps[2].Error)
		assert.Contains(t, result.Steps[2].Error.Error(), `step "create" produced no output "coupon"`)
	})
}
