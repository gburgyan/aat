package engine

import (
	"context"
	"fmt"
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

// chainGraph models an order whose cleanup takes two calls (request a refund,
// then confirm it with the refund's id) and a cart deleted in one. lookupThing
// produces a generic id that no cleanup may confuse with the refund's.
func chainGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"createCart": {
				Name: "createCart", Adapter: "chain.createCart", Cleanup: graph.CleanupPairing{Node: "deleteCart"},
				Outputs: []graph.Output{{Name: "cartId", Type: "string"}},
			},
			"deleteCart": {
				Name: "deleteCart", Adapter: "chain.deleteCart",
				Inputs: []graph.Input{{Name: "cartId", Type: "string"}, {Name: "id", Type: "string"}},
			},
			"createOrder": {
				Name: "createOrder", Adapter: "chain.createOrder", Cleanup: graph.CleanupPairing{Node: "requestRefund"},
				Outputs: []graph.Output{{Name: "orderId", Type: "string"}},
			},
			"requestRefund": {
				Name: "requestRefund", Adapter: "chain.requestRefund", Cleanup: graph.CleanupPairing{Node: "confirmRefund"},
				Inputs:  []graph.Input{{Name: "orderId", Type: "string"}},
				Outputs: []graph.Output{{Name: "id", Type: "string"}},
			},
			"confirmRefund": {
				Name: "confirmRefund", Adapter: "chain.confirmRefund",
				Inputs: []graph.Input{{Name: "id", Type: "string"}},
			},
			"lookupThing": {
				Name: "lookupThing", Adapter: "chain.lookupThing",
				Outputs: []graph.Output{{Name: "id", Type: "string"}},
			},
			"failStep": {Name: "failStep", Adapter: "chain.failStep"},
		},
	}
}

// chainResponse is what chainServer answers for one path.
type chainResponse struct {
	status int
	body   string
}

// chainServer records request paths and answers each path with its configured
// response, or 200 {}.
type chainServer struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
}

func newChainServer(t *testing.T, responses map[string]chainResponse) *chainServer {
	t.Helper()
	cs := &chainServer{}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cs.mu.Lock()
		cs.paths = append(cs.paths, r.URL.Path)
		cs.mu.Unlock()
		resp, ok := responses[r.URL.Path]
		if !ok {
			resp = chainResponse{status: http.StatusOK, body: `{}`}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
	t.Cleanup(cs.Close)
	return cs
}

func (cs *chainServer) requestPaths() []string {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return append([]string(nil), cs.paths...)
}

// counterAdapter numbers its outputs: {key: prefix1}, {key: prefix2}, ...
type counterAdapter struct {
	stubAdapter
	key, prefix string
	mu          sync.Mutex
	n           int
}

func (a *counterAdapter) ExtractOutputs(*adapter.Response) (map[string]any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n++
	return map[string]any{a.key: fmt.Sprintf("%s%d", a.prefix, a.n)}, nil
}

// refundAdapter answers with a refund id derived from the order it was sent.
type refundAdapter struct {
	stubAdapter
	mu    sync.Mutex
	order any
}

func (a *refundAdapter) BuildRequest(inputs map[string]any, cfg *adapter.EnvironmentConfig) (*adapter.Request, error) {
	a.mu.Lock()
	a.order = inputs["orderId"]
	a.mu.Unlock()
	return a.stubAdapter.BuildRequest(inputs, cfg)
}

func (a *refundAdapter) ExtractOutputs(*adapter.Response) (map[string]any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return map[string]any{"id": fmt.Sprintf("refund-%v", a.order)}, nil
}

func chainEngine(t *testing.T, serverURL string) *Engine {
	t.Helper()
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("chain.createCart", &stubAdapter{method: "POST", path: "/createCart", response: map[string]any{"cartId": "cart-1"}}))
	require.NoError(t, registry.Register("chain.deleteCart", &stubAdapter{method: "DELETE", path: "/deleteCart", response: map[string]any{}}))
	require.NoError(t, registry.Register("chain.createOrder", &counterAdapter{stubAdapter: stubAdapter{method: "POST", path: "/createOrder"}, key: "orderId", prefix: "o"}))
	require.NoError(t, registry.Register("chain.requestRefund", &refundAdapter{stubAdapter: stubAdapter{method: "POST", path: "/requestRefund"}}))
	require.NoError(t, registry.Register("chain.confirmRefund", &stubAdapter{method: "POST", path: "/confirmRefund", response: map[string]any{}}))
	require.NoError(t, registry.Register("chain.lookupThing", &stubAdapter{method: "GET", path: "/lookup", response: map[string]any{"id": "main-id"}}))
	require.NoError(t, registry.Register("chain.failStep", &stubAdapter{method: "POST", path: "/fail", response: map[string]any{}}))
	return NewEngine(chainGraph(), registry, NewExecutorRouter(adapter.NewHTTPExecutor(serverURL), &adapter.EnvironmentConfig{}))
}

func chainPlan(steps ...plan.Step) *plan.Plan {
	return &plan.Plan{
		Metadata:  plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: steps},
	}
}

// cartOrderLookupPlan creates a cart, then an order, then looks up a thing
// with a generic id, so cleanup unwinds the order's chain before the cart.
func cartOrderLookupPlan() *plan.Plan {
	return chainPlan(
		plan.Step{Node: "createCart"},
		plan.Step{Node: "createOrder", DependsOn: []string{"createCart"}},
		plan.Step{Node: "lookupThing", DependsOn: []string{"createOrder"}},
	)
}

func TestCleanupChain_RunsDepthFirstAfterParent(t *testing.T) {
	srv := newChainServer(t, nil)
	result := chainEngine(t, srv.URL).Run(context.Background(), cartOrderLookupPlan())

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	assert.Equal(t, []string{"requestRefund", "confirmRefund", "deleteCart"}, cleanupNodes(result.CleanupResults))
	assert.Equal(t, []string{"/createCart", "/createOrder", "/lookup", "/requestRefund", "/confirmRefund", "/deleteCart"}, srv.requestPaths(),
		"the refund is confirmed before the next resource is released")
}

func TestCleanupChain_ParentOutputWinsOverMainStepOutput(t *testing.T) {
	srv := newChainServer(t, nil)
	result := chainEngine(t, srv.URL).Run(context.Background(), cartOrderLookupPlan())

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	require.Len(t, result.CleanupResults, 3)
	assert.Equal(t, "refund-o1", result.CleanupResults[1].Inputs["id"], "the refund's id, not lookupThing's")
}

func TestCleanupChain_OutputsInvisibleToSiblings(t *testing.T) {
	srv := newChainServer(t, nil)
	result := chainEngine(t, srv.URL).Run(context.Background(), cartOrderLookupPlan())

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	require.Len(t, result.CleanupResults, 3)
	deleteCart := result.CleanupResults[2]
	assert.Equal(t, "cart-1", deleteCart.Inputs["cartId"])
	assert.Equal(t, "main-id", deleteCart.Inputs["id"], "a later cleanup never sees the refund's id")
}

func TestCleanupChain_EachChildTakesItsParentsOutputs(t *testing.T) {
	srv := newChainServer(t, nil)
	result := chainEngine(t, srv.URL).Run(context.Background(), chainPlan(
		plan.Step{ID: "first", Node: "createOrder"},
		plan.Step{ID: "second", Node: "createOrder", DependsOn: []string{"first"}},
	))

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	require.Len(t, result.CleanupResults, 4)
	assert.Equal(t, []string{"requestRefund", "confirmRefund", "requestRefund", "confirmRefund"}, cleanupNodes(result.CleanupResults))
	got := []any{
		result.CleanupResults[0].Inputs["orderId"], result.CleanupResults[1].Inputs["id"],
		result.CleanupResults[2].Inputs["orderId"], result.CleanupResults[3].Inputs["id"],
	}
	assert.Equal(t, []any{"o2", "refund-o2", "o1", "refund-o1"}, got, "each confirmation takes its own refund")
}

func TestCleanupChain_UniqueIDsAndCleanupFor(t *testing.T) {
	srv := newChainServer(t, nil)
	result := chainEngine(t, srv.URL).Run(context.Background(), chainPlan(
		plan.Step{ID: "first", Node: "createOrder"},
		plan.Step{ID: "second", Node: "createOrder", DependsOn: []string{"first"}},
		plan.Step{ID: "confirmRefund", Node: "lookupThing", DependsOn: []string{"second"}},
	))

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	var ids, links []string
	for _, r := range result.CleanupResults {
		ids = append(ids, r.StepID)
		links = append(links, r.CleanupFor)
	}
	assert.Equal(t, []string{"requestRefund", "confirmRefund_2", "requestRefund_2", "confirmRefund_3"}, ids,
		"unique within the run, and never a main step's ID")
	assert.Equal(t, []string{"second", "requestRefund", "first", "requestRefund_2"}, links)
}

func TestCleanupChain_SkippedWhenParentFails(t *testing.T) {
	tests := []struct {
		name     string
		response chainResponse
	}{
		{name: "error status", response: chainResponse{status: http.StatusInternalServerError, body: `{"error":"boom"}`}},
		{name: "error in a successful body", response: chainResponse{status: http.StatusOK, body: `{"errors":[{"message":"not refundable"}]}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newChainServer(t, map[string]chainResponse{"/requestRefund": tt.response})
			eng := chainEngine(t, srv.URL)
			eng.graph.Nodes["requestRefund"].ErrorDetection = []graph.ErrorDetectionRule{{Path: "errors", Rule: "non-empty"}}

			result := eng.Run(context.Background(), chainPlan(plan.Step{Node: "createOrder"}))

			require.Equal(t, OutcomePassed, result.Outcome, "a failed cleanup never changes the outcome")
			assert.Equal(t, []string{"requestRefund"}, cleanupNodes(result.CleanupResults))
			assert.NotContains(t, srv.requestPaths(), "/confirmRefund")
		})
	}
}

func TestPlanCleanup_ListedChainedNodeGatesChild(t *testing.T) {
	cleanup := []plan.CleanupStep{
		{Node: "requestRefund", RunOn: "always"},
		{Node: "confirmRefund", RunOn: "failure"},
	}

	t.Run("passed run", func(t *testing.T) {
		srv := newChainServer(t, nil)
		p := chainPlan(plan.Step{Node: "createOrder"})
		p.Execution.Cleanup = cleanup

		result := chainEngine(t, srv.URL).Run(context.Background(), p)

		require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
		assert.Equal(t, []string{"requestRefund"}, cleanupNodes(result.CleanupResults))
		assert.Equal(t, []string{"/createOrder", "/requestRefund"}, srv.requestPaths(), "a listed chained node never runs on its own")
	})

	t.Run("failed run", func(t *testing.T) {
		srv := newChainServer(t, map[string]chainResponse{"/fail": {status: http.StatusInternalServerError, body: `{}`}})
		p := chainPlan(plan.Step{Node: "createOrder"}, plan.Step{Node: "failStep", DependsOn: []string{"createOrder"}})
		p.Execution.Cleanup = cleanup

		result := chainEngine(t, srv.URL).Run(context.Background(), p)

		require.Equal(t, OutcomeFailed, result.Outcome)
		assert.Equal(t, []string{"requestRefund", "confirmRefund"}, cleanupNodes(result.CleanupResults))
	})
}

func TestCleanupChain_CycleGuard(t *testing.T) {
	srv := newChainServer(t, nil)
	eng := chainEngine(t, srv.URL)
	eng.graph.Nodes["confirmRefund"].Cleanup = graph.CleanupPairing{Node: "requestRefund"} // graph validation would reject this

	result := eng.Run(context.Background(), chainPlan(plan.Step{Node: "createOrder"}))

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	require.Len(t, result.CleanupResults, 3)
	assert.EqualError(t, result.CleanupResults[2].Error, "cleanup cycle: requestRefund → confirmRefund → requestRefund")
	assert.Equal(t, []string{"/createOrder", "/requestRefund", "/confirmRefund"}, srv.requestPaths())
}
