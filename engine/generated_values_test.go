package engine

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// generatedUUID matches a lowercase version 4 UUID.
var generatedUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestResolveInputsWithContext_GeneratedExpressions(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"target": {
				Name: "target",
				Inputs: []graph.Input{
					{Name: "requestKey", Type: "string"},
					{Name: "since", Type: "integer"},
				},
			},
		},
	}
	step := plan.Step{
		Node: "target",
		Values: map[string]plan.StepValue{
			"requestKey": {Default: "{{uuid}}"},
			"since":      {Default: "{{unixtime - 1 hours}}"},
		},
	}
	rctx := &ResolveContext{Now: fixedNow(), Random: bytes.NewReader(make([]byte, 16))}

	inputs, _, resolutions, err := ResolveInputsWithContext(context.Background(), step, g.Nodes["target"], g, NewRunState(), rctx)

	require.NoError(t, err)
	assert.Equal(t, "00000000-0000-4000-8000-000000000000", inputs["requestKey"])
	assert.Equal(t, fixedNow().Unix()-3600, inputs["since"])
	require.Len(t, resolutions, 2)
	for _, r := range resolutions {
		assert.Equal(t, "expression", r.Source, r.InputName)
	}
}

// recordingServer answers every request with an empty JSON object and returns
// a function that lists the request bodies it has received.
func recordingServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
}

// refAdapter sends a node's ref input in a JSON body.
func refAdapter(name string) *adapter.TemplateAdapter {
	return adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  name,
		Protocol: "http",
		Request: adapter.TemplateRequest{
			Method:  "POST",
			Path:    "/" + name,
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    `{"ref": "{{ref}}"}`,
		},
	})
}

func TestEngine_Run_FromInputCopiesGeneratedValue(t *testing.T) {
	srv, bodies := recordingServer(t)
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"createOrder": {Name: "createOrder", Adapter: "test.createOrder", Inputs: []graph.Input{{Name: "ref", Type: "string"}}},
			"getOrder":    {Name: "getOrder", Adapter: "test.getOrder", Inputs: []graph.Input{{Name: "ref", Type: "string"}}},
		},
	}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.createOrder", refAdapter("createOrder")))
	require.NoError(t, registry.Register("test.getOrder", refAdapter("getOrder")))
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(srv.URL), &adapter.EnvironmentConfig{}))
	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{
			{Node: "createOrder", Values: map[string]plan.StepValue{"ref": {Default: "{{uuid}}"}}},
			{Node: "getOrder", DependsOn: []string{"createOrder"}, Values: map[string]plan.StepValue{"ref": {FromInput: "createOrder.ref"}}},
		}},
	}

	result := eng.Run(context.Background(), p)

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	got := bodies()
	require.Len(t, got, 2)
	assert.Equal(t, got[0], got[1], "getOrder sends the value createOrder generated")
	require.NotEmpty(t, result.Steps)
	assert.Regexp(t, generatedUUID, result.Steps[0].Inputs["ref"])
}

func TestEngine_Run_GeneratedValuesDifferBetweenRuns(t *testing.T) {
	srv, _ := recordingServer(t)
	eng := buildOrderEngine(t, srv.URL)
	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{
			{Node: "createOrder", Values: map[string]plan.StepValue{"ref": {Default: "{{uuid}}"}}},
		}},
	}

	first := eng.Run(context.Background(), p)
	second := eng.Run(context.Background(), p)

	require.Equal(t, OutcomePassed, first.Outcome, "error: %v", first.Error)
	require.Equal(t, OutcomePassed, second.Outcome, "error: %v", second.Error)
	assert.Regexp(t, generatedUUID, first.Steps[0].Inputs["ref"])
	assert.NotEqual(t, first.Steps[0].Inputs["ref"], second.Steps[0].Inputs["ref"])
}

// TestRetry_ResendsSameGeneratedValue checks that an idempotency-key style value
// stays the same across a step's attempts.
func TestRetry_ResendsSameGeneratedValue(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		first := len(bodies) == 1
		mu.Unlock()
		if first {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{{
			Node:   "createOrder",
			Retry:  &plan.RetryConfig{Max: 2, On: []string{"transient"}},
			Values: map[string]plan.StepValue{"ref": {Default: "{{uuid}}"}},
		}}},
	}

	result := buildOrderEngine(t, server.URL).Run(context.Background(), p)

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 2)
	assert.Equal(t, bodies[0], bodies[1])
	assert.Regexp(t, regexp.MustCompile(`"ref": "[0-9a-f-]{36}"`), bodies[0])
}

// TestEngine_Run_ListValueOfMapsWithExpressions sends a plan's list of maps
// through an iteration block, with an expression in each item.
func TestEngine_Run_ListValueOfMapsWithExpressions(t *testing.T) {
	srv, bodies := recordingServer(t)
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"createOrder": {Name: "createOrder", Adapter: "test.createOrder", Inputs: []graph.Input{{Name: "lineItems", Type: "lineItem[]"}}},
		},
	}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.createOrder", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "createOrder",
		Protocol: "http",
		Request: adapter.TemplateRequest{
			Method:  "POST",
			Path:    "/createOrder",
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    `{"lineItems": [{{#lineItems}}{"sku": "{{.sku}}", "ref": "{{.ref}}"}{{/lineItems}}]}`,
		},
	})))
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(srv.URL), &adapter.EnvironmentConfig{}))
	p, err := plan.Parse([]byte(`
metadata: {graphVersion: "1.0.0"}
execution:
  steps:
    - node: createOrder
      values:
        lineItems:
          - {sku: SKU-1004, ref: "a-{{random 6}}"}
          - {sku: SKU-1006, ref: "b-{{random 6}}"}
`))
	require.NoError(t, err)

	result := eng.Run(context.Background(), p)

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	got := bodies()
	require.Len(t, got, 1)
	assert.Regexp(t, `^\{"lineItems": \[\{"sku": "SKU-1004", "ref": "a-[0-9a-z]{6}"\},\s*\{"sku": "SKU-1006", "ref": "b-[0-9a-z]{6}"\}\]\}$`, got[0])
	items := p.Execution.Steps[0].Values["lineItems"].Default.([]any)
	assert.Equal(t, "a-{{random 6}}", items[0].(map[string]any)["ref"], "the plan keeps its expressions for the next run")
}
