package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildTestGraph creates a simple 3-node graph for testing:
// search → select → book
func buildTestGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"search": {
				Name:    "search",
				Adapter: "test.search",
				Inputs: []graph.Input{
					{Name: "query", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "results", Type: "item[]"},
					{Name: "sessionId", Type: "string"},
				},
			},
			"select": {
				Name:    "select",
				Adapter: "test.select",
				Inputs: []graph.Input{
					{Name: "sessionId", Type: "string"},
					{Name: "itemId", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "confirmed", Type: "boolean"},
					{Name: "price", Type: "money"},
				},
			},
			"book": {
				Name:    "book",
				Adapter: "test.book",
				Inputs: []graph.Input{
					{Name: "sessionId", Type: "string"},
					{Name: "confirmed", Type: "boolean"},
				},
				Outputs: []graph.Output{
					{Name: "bookingId", Type: "string"},
				},
			},
		},
		Edges: []graph.Edge{
			{From: "search.sessionId", To: "select.sessionId"},
			{From: "search.results", To: "select.itemId", Select: true},
			{From: "search.sessionId", To: "book.sessionId"},
			{From: "select.confirmed", To: "book.confirmed"},
		},
	}
}

// stubAdapter returns canned responses for testing.
type stubAdapter struct {
	method   string
	path     string
	response map[string]any
}

func (a *stubAdapter) BuildRequest(inputs map[string]any, config *adapter.EnvironmentConfig) (*adapter.Request, error) {
	body, _ := json.Marshal(inputs)
	headers := make(map[string]string)
	if config != nil {
		for k, v := range config.Headers {
			headers[k] = v
		}
	}
	headers["Content-Type"] = "application/json"
	return &adapter.Request{
		Method:  a.method,
		Path:    a.path,
		Headers: headers,
		Body:    body,
	}, nil
}

func (a *stubAdapter) ExtractOutputs(resp *adapter.Response) (map[string]any, error) {
	return a.response, nil
}

func (a *stubAdapter) ValidateInputs(inputs map[string]any) *adapter.ValidationResult {
	return nil
}

func (a *stubAdapter) ValidateResponse(resp *adapter.Response) *adapter.ValidationResult {
	return nil
}

func TestEngine_Run_ThreeNodeFlow(t *testing.T) {
	g := buildTestGraph()

	// Set up httptest server that always returns 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.search", &stubAdapter{
		method: "POST",
		path:   "/search",
		response: map[string]any{
			"results": []any{
				map[string]any{"id": "item-1", "name": "First"},
				map[string]any{"id": "item-2", "name": "Second"},
			},
			"sessionId": "session-abc",
		},
	}))
	require.NoError(t, registry.Register("test.select", &stubAdapter{
		method: "POST",
		path:   "/select",
		response: map[string]any{
			"confirmed": true,
			"price":     99.99,
		},
	}))
	require.NoError(t, registry.Register("test.book", &stubAdapter{
		method: "POST",
		path:   "/book",
		response: map[string]any{
			"bookingId": "booking-xyz",
		},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	config := &adapter.EnvironmentConfig{}

	engine := NewEngine(g, registry, executor, config)

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "search",
					Values: map[string]plan.StepValue{
						"query": {Default: "test query"},
					},
				},
				{
					Node:      "select",
					DependsOn: []string{"search"},
					Values: map[string]plan.StepValue{
						"itemId": {
							Select: &plan.SelectionConfig{
								Strategy: "first",
								Field:    "id",
							},
						},
					},
				},
				{
					Node:      "book",
					DependsOn: []string{"select"},
				},
			},
		},
	}

	result := engine.Run(context.Background(), p)

	assert.Equal(t, OutcomePassed, result.Outcome)
	assert.Nil(t, result.Error)
	require.Len(t, result.Steps, 3)

	// Verify step order
	assert.Equal(t, "search", result.Steps[0].Node)
	assert.Equal(t, "select", result.Steps[1].Node)
	assert.Equal(t, "book", result.Steps[2].Node)

	// Verify all steps got 200
	for _, sr := range result.Steps {
		assert.Equal(t, 200, sr.StatusCode)
		assert.NoError(t, sr.Error)
	}

	// Verify outputs were extracted
	assert.Equal(t, "session-abc", result.Steps[0].Outputs["sessionId"])
	assert.Equal(t, true, result.Steps[1].Outputs["confirmed"])
	assert.Equal(t, "booking-xyz", result.Steps[2].Outputs["bookingId"])
}

func TestEngine_Run_FailOnNon2xx(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs: []graph.Input{
					{Name: "input", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "output", Type: "string"},
				},
			},
		},
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method:   "POST",
		path:     "/step1",
		response: map[string]any{"output": "val"},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	engine := NewEngine(g, registry, executor, &adapter.EnvironmentConfig{})

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Values: map[string]plan.StepValue{
						"input": {Default: "test"},
					},
				},
			},
		},
	}

	result := engine.Run(context.Background(), p)
	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "status 400")
	assert.Equal(t, 1, callCount)
}

func TestEngine_Run_ValidationError(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes:   map[string]*graph.Node{},
	}

	engine := NewEngine(g, adapter.NewRegistry(), nil, nil)

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "nonexistent"},
			},
		},
	}

	result := engine.Run(context.Background(), p)
	assert.Equal(t, OutcomeError, result.Outcome)
	assert.Error(t, result.Error)
}

func TestEngine_Run_OutputPropagation(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"producer": {
				Name:    "producer",
				Adapter: "test.producer",
				Inputs:  []graph.Input{},
				Outputs: []graph.Output{
					{Name: "value", Type: "string"},
				},
			},
			"consumer": {
				Name:    "consumer",
				Adapter: "test.consumer",
				Inputs: []graph.Input{
					{Name: "value", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "result", Type: "string"},
				},
			},
		},
		Edges: []graph.Edge{
			{From: "producer.value", To: "consumer.value"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()

	var capturedConsumerInputs map[string]any
	require.NoError(t, registry.Register("test.producer", &stubAdapter{
		method: "POST",
		path:   "/produce",
		response: map[string]any{
			"value": "produced-value",
		},
	}))
	consumerAdapter := &captureAdapter{
		stubAdapter: stubAdapter{
			method: "POST",
			path:   "/consume",
			response: map[string]any{
				"result": "done",
			},
		},
		capturedInputs: &capturedConsumerInputs,
	}
	require.NoError(t, registry.Register("test.consumer", consumerAdapter))

	executor := adapter.NewHTTPExecutor(server.URL)
	engine := NewEngine(g, registry, executor, &adapter.EnvironmentConfig{})

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "producer"},
				{Node: "consumer", DependsOn: []string{"producer"}},
			},
		},
	}

	result := engine.Run(context.Background(), p)
	assert.Equal(t, OutcomePassed, result.Outcome)
	assert.Nil(t, result.Error)

	// Verify the consumer received the producer's output
	require.NotNil(t, capturedConsumerInputs)
	assert.Equal(t, "produced-value", capturedConsumerInputs["value"])
}

// captureAdapter wraps stubAdapter but captures inputs for verification.
type captureAdapter struct {
	stubAdapter
	capturedInputs *map[string]any
}

func (a *captureAdapter) BuildRequest(inputs map[string]any, config *adapter.EnvironmentConfig) (*adapter.Request, error) {
	*a.capturedInputs = inputs
	return a.stubAdapter.BuildRequest(inputs, config)
}

func TestEngine_Run_StatusAssertionPasses(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs:  []graph.Input{},
				Outputs: []graph.Output{{Name: "id", Type: "string"}},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": "abc-123"}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method:   "GET",
		path:     "/step1",
		response: map[string]any{"id": "abc-123"},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	engine := NewEngine(g, registry, executor, &adapter.EnvironmentConfig{})

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Assertions: &plan.Assertions{
						Mechanical: []plan.MechanicalAssertion{
							{Type: "status", Expect: 200},
						},
					},
				},
			},
		},
	}

	result := engine.Run(context.Background(), p)
	assert.Equal(t, OutcomePassed, result.Outcome)
	assert.Nil(t, result.Error)
	require.Len(t, result.Steps, 1)
	require.NotNil(t, result.Steps[0].Validation)
	assert.True(t, result.Steps[0].Validation.Passed)
}

func TestEngine_Run_FailingAssertionTriggersCleanup(t *testing.T) {
	cleanupCalled := false
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs:  []graph.Input{},
				Outputs: []graph.Output{{Name: "id", Type: "string"}},
				Cleanup: "cleanup1",
			},
			"cleanup1": {
				Name:    "cleanup1",
				Adapter: "test.cleanup1",
				Inputs:  []graph.Input{},
			},
			"step2": {
				Name:    "step2",
				Adapter: "test.step2",
				Inputs:  []graph.Input{},
				Outputs: []graph.Output{{Name: "result", Type: "string"}},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cleanup" {
			cleanupCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": "abc"}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method: "POST", path: "/step1",
		response: map[string]any{"id": "abc"},
	}))
	require.NoError(t, registry.Register("test.cleanup1", &stubAdapter{
		method: "DELETE", path: "/cleanup",
		response: map[string]any{},
	}))
	require.NoError(t, registry.Register("test.step2", &stubAdapter{
		method: "POST", path: "/step2",
		response: map[string]any{"result": "done"},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	engine := NewEngine(g, registry, executor, &adapter.EnvironmentConfig{})

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Assertions: &plan.Assertions{
						Mechanical: []plan.MechanicalAssertion{
							{Type: "status", Expect: 201}, // fails: server returns 200
						},
					},
				},
				{
					Node:      "step2",
					DependsOn: []string{"step1"},
				},
			},
		},
	}

	result := engine.Run(context.Background(), p)
	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "failed mechanical validation")
	// step2 should NOT have executed
	require.Len(t, result.Steps, 1)
	// Cleanup should have run
	assert.True(t, cleanupCalled)
}

func TestEngine_Run_FieldExistsAssertionOnBody(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs:  []graph.Input{},
				Outputs: []graph.Output{{Name: "name", Type: "string"}},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"name": "Alice", "age": 30}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method: "GET", path: "/step1",
		response: map[string]any{"name": "Alice"},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	engine := NewEngine(g, registry, executor, &adapter.EnvironmentConfig{})

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Assertions: &plan.Assertions{
						Mechanical: []plan.MechanicalAssertion{
							{Type: "fieldExists", Path: "name"},
							{Type: "fieldExists", Path: "age"},
						},
					},
				},
			},
		},
	}

	result := engine.Run(context.Background(), p)
	assert.Equal(t, OutcomePassed, result.Outcome)
	require.NotNil(t, result.Steps[0].Validation)
	assert.True(t, result.Steps[0].Validation.Passed)
	assert.Len(t, result.Steps[0].Validation.Results, 2)
}

func TestEngine_Run_NoAssertions(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs:  []graph.Input{},
				Outputs: []graph.Output{{Name: "id", Type: "string"}},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method: "GET", path: "/step1",
		response: map[string]any{"id": "x"},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	engine := NewEngine(g, registry, executor, &adapter.EnvironmentConfig{})

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{{Node: "step1"}},
		},
	}

	result := engine.Run(context.Background(), p)
	assert.Equal(t, OutcomePassed, result.Outcome)
	require.Len(t, result.Steps, 1)
	assert.Nil(t, result.Steps[0].Validation)
}

func TestEngine_Run_PredicateAssertion(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs:  []graph.Input{},
				Outputs: []graph.Output{{Name: "count", Type: "integer"}},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"count": 5, "active": true}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method: "GET", path: "/step1",
		response: map[string]any{"count": 5},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	engine := NewEngine(g, registry, executor, &adapter.EnvironmentConfig{})

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Assertions: &plan.Assertions{
						Mechanical: []plan.MechanicalAssertion{
							{Type: "predicate", Expr: "count > 0"},
							{Type: "predicate", Expr: "active == true"},
						},
					},
				},
			},
		},
	}

	result := engine.Run(context.Background(), p)
	assert.Equal(t, OutcomePassed, result.Outcome)
	require.NotNil(t, result.Steps[0].Validation)
	assert.True(t, result.Steps[0].Validation.Passed)
	assert.Len(t, result.Steps[0].Validation.Results, 2)
}

func TestEngine_Run_ContinueOnAssertionFailure(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs:  []graph.Input{},
				Outputs: []graph.Output{{Name: "id", Type: "string"}},
			},
			"step2": {
				Name:    "step2",
				Adapter: "test.step2",
				Inputs:  []graph.Input{},
				Outputs: []graph.Output{{Name: "result", Type: "string"}},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": "abc", "result": "done"}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method: "POST", path: "/step1",
		response: map[string]any{"id": "abc"},
	}))
	require.NoError(t, registry.Register("test.step2", &stubAdapter{
		method: "POST", path: "/step2",
		response: map[string]any{"result": "done"},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	engine := NewEngine(g, registry, executor, &adapter.EnvironmentConfig{})
	engine.ContinueOnAssertionFailure = true

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Assertions: &plan.Assertions{
						Mechanical: []plan.MechanicalAssertion{
							{Type: "status", Expect: 201}, // fails: server returns 200
						},
					},
				},
				{Node: "step2"},
			},
		},
	}

	result := engine.Run(context.Background(), p)
	assert.Equal(t, OutcomeFailed, result.Outcome)
	// Both steps should have executed
	require.Len(t, result.Steps, 2)
	assert.False(t, result.Steps[0].Validation.Passed)
}

func TestEngine_Run_StepDurations(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs:  []graph.Input{},
				Outputs: []graph.Output{},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method:   "GET",
		path:     "/step1",
		response: map[string]any{},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	engine := NewEngine(g, registry, executor, &adapter.EnvironmentConfig{})

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{{Node: "step1"}},
		},
	}

	result := engine.Run(context.Background(), p)
	require.Len(t, result.Steps, 1)
	assert.Greater(t, result.Steps[0].Duration.Nanoseconds(), int64(0))
}

// --- Step-level adaptive relaxation tests (Task 20d) ---

func TestAdaptiveMode_StepRelaxation_400WithSoftConstraint(t *testing.T) {
	// Step returns 400, then 200 after a soft constraint is relaxed.
	var callCount int

	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "result", Type: "string"},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount <= 1 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error": "bad request"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method:   "POST",
		path:     "/step1",
		response: map[string]any{"result": "ok"},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	eng := NewEngine(g, registry, executor, &adapter.EnvironmentConfig{}).
		WithMode("adaptive")

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Intent: plan.Intent{
			Constraints: &plan.Constraints{
				Soft: []plan.Constraint{
					{Type: "preference", Name: "origin_pref", AppliesTo: []string{"step1.origin"}},
				},
			},
		},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Values: map[string]plan.StepValue{
						"origin": {Default: "DEN"},
					},
				},
			},
		},
	}

	result := eng.Run(context.Background(), p)
	assert.Equal(t, OutcomePassed, result.Outcome)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, 200, result.Steps[0].StatusCode)

	// Should have relaxation records
	assert.Len(t, result.Steps[0].Relaxations, 1)
	assert.Equal(t, "origin_pref", result.Steps[0].Relaxations[0].ConstraintName)
	assert.Equal(t, "step_failed", result.Steps[0].Relaxations[0].Reason)
	assert.Equal(t, 2, callCount) // 1 fail + 1 success
}

func TestAdaptiveMode_HardConstraintOnly_NoRelaxation(t *testing.T) {
	// Step returns 400 with only hard constraints — no relaxation, just failure
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "result", Type: "string"},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method:   "POST",
		path:     "/step1",
		response: map[string]any{"result": "ok"},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	eng := NewEngine(g, registry, executor, &adapter.EnvironmentConfig{}).
		WithMode("adaptive")

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Intent: plan.Intent{
			Constraints: &plan.Constraints{
				Hard: []plan.Constraint{
					{Type: "requirement", Name: "origin_req", AppliesTo: []string{"step1.origin"}},
				},
			},
		},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Values: map[string]plan.StepValue{
						"origin": {Default: "DEN"},
					},
				},
			},
		},
	}

	result := eng.Run(context.Background(), p)
	assert.Equal(t, OutcomeFailed, result.Outcome)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, 400, result.Steps[0].StatusCode)
	assert.Empty(t, result.Steps[0].Relaxations)
}

func TestAdaptiveMode_500Error_NoRelaxation(t *testing.T) {
	// 500 errors should NOT trigger relaxation (only 4xx client errors)
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "result", Type: "string"},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "server error"}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method:   "POST",
		path:     "/step1",
		response: map[string]any{},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	eng := NewEngine(g, registry, executor, &adapter.EnvironmentConfig{}).
		WithMode("adaptive")

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Intent: plan.Intent{
			Constraints: &plan.Constraints{
				Soft: []plan.Constraint{
					{Type: "preference", Name: "origin_pref", AppliesTo: []string{"step1.origin"}},
				},
			},
		},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Values: map[string]plan.StepValue{
						"origin": {Default: "DEN"},
					},
				},
			},
		},
	}

	result := eng.Run(context.Background(), p)
	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.Empty(t, result.Steps[0].Relaxations)
}

func TestNonAdaptiveMode_NoStepRelaxation(t *testing.T) {
	// In lean mode, step-level relaxation should NOT happen
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "result", Type: "string"},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method:   "POST",
		path:     "/step1",
		response: map[string]any{},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	eng := NewEngine(g, registry, executor, &adapter.EnvironmentConfig{}).
		WithMode("lean")

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Intent: plan.Intent{
			Constraints: &plan.Constraints{
				Soft: []plan.Constraint{
					{Type: "preference", Name: "origin_pref", AppliesTo: []string{"step1.origin"}},
				},
			},
		},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Values: map[string]plan.StepValue{
						"origin": {Default: "DEN"},
					},
				},
			},
		},
	}

	result := eng.Run(context.Background(), p)
	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.Empty(t, result.Steps[0].Relaxations)
}

func TestAdaptiveMode_ExpectFailure_SkipsRelaxation(t *testing.T) {
	// ExpectFailure steps should not trigger relaxation
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "result", Type: "string"},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method:   "POST",
		path:     "/step1",
		response: map[string]any{},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	eng := NewEngine(g, registry, executor, &adapter.EnvironmentConfig{}).
		WithMode("adaptive")

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Intent: plan.Intent{
			Constraints: &plan.Constraints{
				Soft: []plan.Constraint{
					{Type: "preference", Name: "origin_pref", AppliesTo: []string{"step1.origin"}},
				},
			},
		},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					ExpectFailure: &plan.ExpectFailure{
						Status: []int{400},
					},
					Values: map[string]plan.StepValue{
						"origin": {Default: "DEN"},
					},
				},
			},
		},
	}

	result := eng.Run(context.Background(), p)
	// ExpectFailure step with matching status should not trigger relaxation
	require.Len(t, result.Steps, 1)
	assert.Empty(t, result.Steps[0].Relaxations)
}

func TestAdaptiveMode_BudgetExhausted(t *testing.T) {
	// With MaxRelaxationDepth=1 and 2 soft constraints, only 1 relaxation
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"step1": {
				Name:    "step1",
				Adapter: "test.step1",
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "destination", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "result", Type: "string"},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return 400
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.step1", &stubAdapter{
		method:   "POST",
		path:     "/step1",
		response: map[string]any{},
	}))

	executor := adapter.NewHTTPExecutor(server.URL)
	eng := NewEngine(g, registry, executor, &adapter.EnvironmentConfig{}).
		WithMode("adaptive").
		WithMaxRelaxationDepth(1)

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Intent: plan.Intent{
			Constraints: &plan.Constraints{
				Soft: []plan.Constraint{
					{Type: "preference", Name: "origin_pref", AppliesTo: []string{"step1.origin"}},
					{Type: "preference", Name: "dest_pref", AppliesTo: []string{"step1.destination"}},
				},
			},
		},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					Node: "step1",
					Values: map[string]plan.StepValue{
						"origin":      {Default: "DEN"},
						"destination": {Default: "LAX"},
					},
				},
			},
		},
	}

	result := eng.Run(context.Background(), p)
	assert.Equal(t, OutcomeFailed, result.Outcome)
	require.Len(t, result.Steps, 1)
	// Should have exactly 1 relaxation (budget is 1)
	assert.Len(t, result.Steps[0].Relaxations, 1)
}

func TestWithMaxRelaxationDepth(t *testing.T) {
	eng := &Engine{}
	eng.WithMaxRelaxationDepth(5)
	assert.Equal(t, 5, eng.MaxRelaxationDepth)
	assert.Equal(t, 5, eng.maxRelaxationDepth())
}

func TestMaxRelaxationDepth_Default(t *testing.T) {
	eng := &Engine{}
	assert.Equal(t, 3, eng.maxRelaxationDepth())
}
