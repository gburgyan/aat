package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMutations_EachSiblingProducesOwnResult runs a plan with a happy-path
// step plus three mutation siblings, asserting that each generates its own
// StepResult with the right pass/fail semantics.
func TestMutations_EachSiblingProducesOwnResult(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"createBooking": {
				Name:    "createBooking",
				Adapter: "test.createBooking",
				Inputs: []graph.Input{
					{Name: "lastName", Type: "string"},
					{Name: "age", Type: "integer"},
				},
			},
		},
	}

	var mu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad json"}`))
			return
		}
		if s, _ := parsed["lastName"].(string); s == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"lastName required"}`))
			return
		}
		if age, ok := parsed["age"].(float64); ok && age < 0 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"age must be non-negative"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"bookingId":"b-123"}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.createBooking", &stubAdapter{
		method:   "POST",
		path:     "/bookings",
		response: map[string]any{"bookingId": "b-123"},
	}))

	engine := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{}))

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					ID:   "happy",
					Node: "createBooking",
					Values: map[string]plan.StepValue{
						"lastName": {Default: "Smith"},
						"age":      {Default: 30},
					},
					Mutations: []plan.Mutation{
						{Name: "empty-lastName", Set: map[string]any{"lastName": ""}, ExpectStatus: []int{400}},
						{Name: "negative-age", Set: map[string]any{"age": -1}, ExpectStatus: []int{400}},
						{Name: "malformed-body", RawBody: `{"oops":`, ExpectStatus: []int{400}},
					},
				},
			},
		},
	}

	result := engine.Run(context.Background(), p)

	assert.Equal(t, OutcomePassed, result.Outcome, "all mutations expected-fail correctly so the run passes")
	require.Len(t, result.Steps, 4, "parent + 3 mutation siblings")

	assert.Equal(t, "happy", result.Steps[0].StepID)
	assert.Equal(t, 200, result.Steps[0].StatusCode)
	assert.Nil(t, result.Steps[0].ExpectFailure)

	assert.Equal(t, "happy--empty-lastName", result.Steps[1].StepID)
	assert.Equal(t, 400, result.Steps[1].StatusCode)
	require.NotNil(t, result.Steps[1].ExpectFailure)
	assert.True(t, result.Steps[1].ExpectFailure.Passed)

	assert.Equal(t, "happy--negative-age", result.Steps[2].StepID)
	assert.Equal(t, 400, result.Steps[2].StatusCode)
	require.NotNil(t, result.Steps[2].ExpectFailure)
	assert.True(t, result.Steps[2].ExpectFailure.Passed)

	assert.Equal(t, "happy--malformed-body", result.Steps[3].StepID)
	assert.Equal(t, 400, result.Steps[3].StatusCode)
	require.NotNil(t, result.Steps[3].ExpectFailure)
	assert.True(t, result.Steps[3].ExpectFailure.Passed)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 4)
	assert.Equal(t, `{"oops":`, bodies[3], "rawBody bypasses adapter substitution")
}

// extractingAdapter is a test adapter that reads the response body into a
// JSON map and returns it as outputs, so each HTTP call can propagate unique
// values (e.g., a server-generated id) to downstream steps.
type extractingAdapter struct {
	method string
	path   string
}

func (a *extractingAdapter) BuildRequest(inputs map[string]any, cfg *adapter.EnvironmentConfig) (*adapter.Request, error) {
	body, _ := json.Marshal(inputs)
	headers := map[string]string{"Content-Type": "application/json"}
	if cfg != nil {
		for k, v := range cfg.Headers {
			headers[k] = v
		}
	}
	return &adapter.Request{Method: a.method, Path: a.path, Headers: headers, Body: body}, nil
}

func (a *extractingAdapter) ExtractOutputs(resp *adapter.Response) (map[string]any, error) {
	if len(resp.Body) == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *extractingAdapter) ValidateInputs(map[string]any) *adapter.ValidationResult { return nil }
func (a *extractingAdapter) ValidateResponse(*adapter.Response) *adapter.ValidationResult {
	return nil
}

// TestMutations_Isolated_PrereqRunsOncePerMutation proves that isolated scope
// gives each mutation its own fresh copy of the prereq chain. login and
// createCart run 1 + N times (once for the happy path, once per mutation),
// and each addItem mutation carries the cart id from its own createCart
// clone — not the happy path's cart id or an earlier mutation's.
func TestMutations_Isolated_PrereqRunsOncePerMutation(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"login": {
				Name:    "login",
				Adapter: "test.login",
				Outputs: []graph.Output{{Name: "token", Type: "string"}},
			},
			"createCart": {
				Name:    "createCart",
				Adapter: "test.createCart",
				Inputs:  []graph.Input{{Name: "token", Type: "string"}},
				Outputs: []graph.Output{{Name: "cartId", Type: "string"}},
			},
			"addItem": {
				Name:    "addItem",
				Adapter: "test.addItem",
				Inputs: []graph.Input{
					{Name: "cartId", Type: "string"},
					{Name: "productId", Type: "string"},
				},
			},
		},
	}

	var mu sync.Mutex
	var loginCount, cartCount int
	var addItemBodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login":
			mu.Lock()
			loginCount++
			n := loginCount
			mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"token":"t-%d"}`, n)
		case "/createCart":
			mu.Lock()
			cartCount++
			n := cartCount
			mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"cartId":"cart-%d"}`, n)
		case "/addItem":
			body, _ := io.ReadAll(r.Body)
			var parsed map[string]any
			_ = json.Unmarshal(body, &parsed)
			mu.Lock()
			addItemBodies = append(addItemBodies, parsed)
			mu.Unlock()
			if s, _ := parsed["productId"].(string); s == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"productId required"}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.login", &extractingAdapter{method: "POST", path: "/login"}))
	require.NoError(t, registry.Register("test.createCart", &extractingAdapter{method: "POST", path: "/createCart"}))
	require.NoError(t, registry.Register("test.addItem", &extractingAdapter{method: "POST", path: "/addItem"}))

	engine := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{}))

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{ID: "login", Node: "login"},
				{
					ID:        "createCart",
					Node:      "createCart",
					DependsOn: []string{"login"},
					Values:    map[string]plan.StepValue{"token": {From: "login.token"}},
				},
				{
					ID:            "addItem",
					Node:          "addItem",
					DependsOn:     []string{"createCart"},
					MutationScope: "isolated",
					Values: map[string]plan.StepValue{
						"cartId":    {From: "createCart.cartId"},
						"productId": {Default: "P1"},
					},
					Mutations: []plan.Mutation{
						{Name: "empty-productId", Set: map[string]any{"productId": ""}, ExpectStatus: []int{400}},
						{Name: "another", Set: map[string]any{"productId": ""}, ExpectStatus: []int{400}},
					},
				},
			},
		},
	}

	result := engine.Run(context.Background(), p)
	require.Equal(t, OutcomePassed, result.Outcome, "happy path plus two expected-failure siblings; overall pass")
	require.Len(t, result.Steps, 1+1+1+2*(1+1+1), "login+createCart+addItem + 2 × (login+createCart+addItem-sibling)")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 3, loginCount, "one happy path + one clone per mutation")
	assert.Equal(t, 3, cartCount, "same: prereq chain runs once per mutation")
	require.Len(t, addItemBodies, 3)

	// Happy path sends cartId = cart-1 (or whichever the first createCart
	// produced). Each mutation sends the cart id from its *own* cloned
	// createCart — all three ids must be distinct.
	seen := map[string]bool{}
	for _, body := range addItemBodies {
		id, _ := body["cartId"].(string)
		assert.NotEmpty(t, id, "every addItem call must carry a cartId")
		seen[id] = true
	}
	assert.Len(t, seen, 3, "happy path and each mutation use distinct cart ids — state is isolated")
}

// TestMutations_UnexpectedSuccessFailsRun: if a mutation returns 200 when
// negative-failure was expected, the run must fail.
func TestMutations_UnexpectedSuccessFailsRun(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"createBooking": {
				Name:    "createBooking",
				Adapter: "test.createBooking",
				Inputs:  []graph.Input{{Name: "lastName", Type: "string"}},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"bookingId":"b-1"}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.createBooking", &stubAdapter{
		method: "POST", path: "/bookings",
		response: map[string]any{"bookingId": "b-1"},
	}))

	engine := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{}))

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{
					ID:   "happy",
					Node: "createBooking",
					Values: map[string]plan.StepValue{
						"lastName": {Default: "Smith"},
					},
					Mutations: []plan.Mutation{
						{Name: "empty-lastName", Set: map[string]any{"lastName": ""}, ExpectStatus: []int{400}},
					},
				},
			},
		},
	}
	result := engine.Run(context.Background(), p)
	assert.Equal(t, OutcomeFailed, result.Outcome, "server returned 200 instead of expected 400 — run must fail")
	require.NotNil(t, result.Error)
	assert.Contains(t, result.Error.Error(), "expected failure status")
}
