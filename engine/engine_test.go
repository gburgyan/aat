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
