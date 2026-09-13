package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// skipGraph models resources whose cleanup a main step can make unnecessary: a
// cart deleted in one call, an order cancelled and then confirmed, and a payment
// voided only while it is authorized and not once it is captured.
func skipGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"createCart": {
				Name: "createCart", Adapter: "skip.createCart", Cleanup: graph.CleanupPairing{Node: "deleteCart"},
				Outputs: []graph.Output{{Name: "cartId", Type: "string"}},
			},
			"deleteCart": {
				Name: "deleteCart", Adapter: "skip.deleteCart",
				Inputs: []graph.Input{{Name: "cartId", Type: "string"}},
			},
			"createOrder": {
				Name: "createOrder", Adapter: "skip.createOrder", Cleanup: graph.CleanupPairing{Node: "cancelOrder"},
				Outputs: []graph.Output{{Name: "orderId", Type: "string"}, {Name: "reason", Type: "string", Optional: true}},
			},
			"cancelOrder": {
				Name: "cancelOrder", Adapter: "skip.cancelOrder", Cleanup: graph.CleanupPairing{Node: "confirmCancel"},
				Inputs:  []graph.Input{{Name: "orderId", Type: "string"}, {Name: "reason", Type: "string", Optional: true}},
				Outputs: []graph.Output{{Name: "cancellationId", Type: "string"}, {Name: "cancellationStatus", Type: "string", Optional: true}},
			},
			"confirmCancel": {
				Name: "confirmCancel", Adapter: "skip.confirmCancel",
				Inputs: []graph.Input{{Name: "cancellationId", Type: "string"}},
			},
			"createPayment": {
				Name: "createPayment", Adapter: "skip.createPayment",
				Cleanup: graph.CleanupPairing{Node: "voidPayment", When: `status == "authorized"`, ReleasedBy: []string{"capturePayment"}},
				Outputs: []graph.Output{{Name: "paymentId", Type: "string"}, {Name: "status", Type: "string", Optional: true}},
			},
			"capturePayment": {
				Name: "capturePayment", Adapter: "skip.capturePayment",
				Inputs:  []graph.Input{{Name: "paymentId", Type: "string"}},
				Outputs: []graph.Output{{Name: "status", Type: "string"}},
			},
			"voidPayment": {
				Name: "voidPayment", Adapter: "skip.voidPayment",
				Inputs: []graph.Input{{Name: "paymentId", Type: "string"}},
			},
		},
	}
}

// skipResponses are the outputs each skipGraph node's adapter extracts.
func skipResponses() map[string]map[string]any {
	return map[string]map[string]any{
		"createCart":     {"cartId": "cart-1"},
		"createOrder":    {"orderId": "order-1"},
		"cancelOrder":    {"cancellationId": "cancel-1"},
		"createPayment":  {"paymentId": "pay-1", "status": "authorized"},
		"capturePayment": {"status": "captured"},
	}
}

func skipEngine(t *testing.T, serverURL string, g *graph.Graph, responses map[string]map[string]any) *Engine {
	t.Helper()
	registry := adapter.NewRegistry()
	for name := range g.Nodes {
		response := responses[name]
		if response == nil {
			response = map[string]any{}
		}
		require.NoError(t, registry.Register("skip."+name, &stubAdapter{method: "POST", path: "/" + name, response: response}))
	}
	return NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(serverURL), &adapter.EnvironmentConfig{}))
}

// newSequenceServer answers each path with its responses in turn, repeating the
// last, or 200 {} for a path it has none for, and records the request paths.
func newSequenceServer(t *testing.T, responses map[string][]chainResponse) *chainServer {
	t.Helper()
	cs := &chainServer{}
	calls := map[string]int{}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cs.mu.Lock()
		cs.paths = append(cs.paths, r.URL.Path)
		n := calls[r.URL.Path]
		calls[r.URL.Path]++
		cs.mu.Unlock()
		resp := chainResponse{status: http.StatusOK, body: `{}`}
		if seq := responses[r.URL.Path]; len(seq) > 0 {
			resp = seq[min(n, len(seq)-1)]
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
	t.Cleanup(cs.Close)
	return cs
}

func skipFrom(ref string) plan.StepValue { return plan.StepValue{From: ref} }

func cancelOrderStep() plan.Step {
	return plan.Step{Node: "cancelOrder", DependsOn: []string{"createOrder"}, Values: map[string]plan.StepValue{"orderId": skipFrom("createOrder.orderId")}}
}

func TestCleanupSkip_ExplicitReleaseSkipsEachEntry(t *testing.T) {
	srv := newChainServer(t, nil)
	result := skipEngine(t, srv.URL, skipGraph(), skipResponses()).Run(context.Background(), chainPlan(
		plan.Step{Node: "createOrder"},
		cancelOrderStep(),
		plan.Step{Node: "confirmCancel", DependsOn: []string{"cancelOrder"}, Values: map[string]plan.StepValue{"cancellationId": skipFrom("cancelOrder.cancellationId")}},
	))

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	assert.Empty(t, result.CleanupResults)
	assert.Equal(t, []CleanupSkip{
		{Node: "confirmCancel", CleanupFor: "cancelOrder", Reason: CleanupSkipReleased, ReleasedBy: "confirmCancel"},
		{Node: "cancelOrder", CleanupFor: "createOrder", Reason: CleanupSkipReleased, ReleasedBy: "cancelOrder"},
	}, result.CleanupSkipped)
	assert.Equal(t, []string{"/createOrder", "/cancelOrder", "/confirmCancel"}, srv.requestPaths())
}

func TestCleanupSkip_ReleasedEntrySkipsItsChain(t *testing.T) {
	srv := newChainServer(t, nil)
	result := skipEngine(t, srv.URL, skipGraph(), skipResponses()).Run(context.Background(), chainPlan(
		plan.Step{Node: "createOrder"},
		cancelOrderStep(),
	))

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	assert.Equal(t, []string{"confirmCancel"}, cleanupNodes(result.CleanupResults), "the explicit cancel is confirmed once")
	assert.Equal(t, []CleanupSkip{{Node: "cancelOrder", CleanupFor: "createOrder", Reason: CleanupSkipReleased, ReleasedBy: "cancelOrder"}}, result.CleanupSkipped)
	assert.Equal(t, []string{"/createOrder", "/cancelOrder", "/confirmCancel"}, srv.requestPaths())
}

func TestCleanupSkip_FailedOrExpectedFailureDoesNotRelease(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		expect      *plan.ExpectFailure
		wantOutcome Outcome
	}{
		{name: "failed cancel", status: http.StatusUnprocessableEntity, wantOutcome: OutcomeFailed},
		{name: "cancel expected to fail", status: http.StatusConflict, expect: &plan.ExpectFailure{Status: []int{http.StatusConflict}}, wantOutcome: OutcomePassed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newSequenceServer(t, map[string][]chainResponse{"/cancelOrder": {{status: tt.status, body: `{}`}, {status: http.StatusOK, body: `{}`}}})
			cancel := cancelOrderStep()
			cancel.ExpectFailure = tt.expect
			result := skipEngine(t, srv.URL, skipGraph(), skipResponses()).Run(context.Background(), chainPlan(plan.Step{Node: "createOrder"}, cancel))

			require.Equal(t, tt.wantOutcome, result.Outcome, "error: %v", result.Error)
			assert.Equal(t, []string{"cancelOrder", "confirmCancel"}, cleanupNodes(result.CleanupResults))
			assert.Empty(t, result.CleanupSkipped)
		})
	}
}

func TestCleanupSkip_OnlyALaterStepOnTheSameResourceReleases(t *testing.T) {
	t.Run("a resource created again after the release is still cleaned", func(t *testing.T) {
		srv := newChainServer(t, nil)
		result := skipEngine(t, srv.URL, skipGraph(), skipResponses()).Run(context.Background(), chainPlan(
			plan.Step{ID: "first", Node: "createCart"},
			plan.Step{Node: "deleteCart", DependsOn: []string{"first"}, Values: map[string]plan.StepValue{"cartId": skipFrom("first.cartId")}},
			plan.Step{ID: "second", Node: "createCart", DependsOn: []string{"deleteCart"}},
		))

		require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
		require.Equal(t, []string{"deleteCart"}, cleanupNodes(result.CleanupResults))
		assert.Equal(t, "second", result.CleanupResults[0].CleanupFor)
		assert.Equal(t, []CleanupSkip{{Node: "deleteCart", CleanupFor: "first", Reason: CleanupSkipReleased, ReleasedBy: "deleteCart"}}, result.CleanupSkipped)
	})

	t.Run("a release of another resource does not count", func(t *testing.T) {
		srv := newChainServer(t, nil)
		result := skipEngine(t, srv.URL, skipGraph(), skipResponses()).Run(context.Background(), chainPlan(
			plan.Step{Node: "createCart"},
			plan.Step{Node: "deleteCart", DependsOn: []string{"createCart"}, Values: map[string]plan.StepValue{"cartId": {Default: "cart-other"}}},
		))

		require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
		require.Equal(t, []string{"deleteCart"}, cleanupNodes(result.CleanupResults))
		assert.Equal(t, "cart-1", result.CleanupResults[0].Inputs["cartId"])
		assert.Empty(t, result.CleanupSkipped)
	})

	t.Run("a verification step does not release", func(t *testing.T) {
		srv := newChainServer(t, nil)
		p := chainPlan(plan.Step{Node: "createCart"})
		p.Execution.Verification = []plan.VerificationStep{{Node: "deleteCart"}}
		result := skipEngine(t, srv.URL, skipGraph(), skipResponses()).Run(context.Background(), p)

		require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
		assert.Equal(t, []string{"deleteCart"}, cleanupNodes(result.CleanupResults))
		assert.Empty(t, result.CleanupSkipped)
		assert.Equal(t, []string{"/createCart", "/deleteCart", "/deleteCart"}, srv.requestPaths())
	})
}

func TestCleanupSkip_ReleaseNeedsEverySharedInput(t *testing.T) {
	tests := []struct {
		name        string
		reason      *plan.StepValue
		wantCleanup []string
		wantSkipped []CleanupSkip
	}{
		{
			name:        "same reason",
			reason:      &plan.StepValue{Default: "duplicate"},
			wantCleanup: []string{"confirmCancel"},
			wantSkipped: []CleanupSkip{{Node: "cancelOrder", CleanupFor: "createOrder", Reason: CleanupSkipReleased, ReleasedBy: "cancelOrder"}},
		},
		{name: "another reason", reason: &plan.StepValue{Default: "customer request"}, wantCleanup: []string{"confirmCancel", "cancelOrder", "confirmCancel"}},
		{name: "reason left unset", wantCleanup: []string{"confirmCancel", "cancelOrder", "confirmCancel"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responses := skipResponses()
			responses["createOrder"] = map[string]any{"orderId": "order-1", "reason": "duplicate"}
			cancel := cancelOrderStep()
			if tt.reason != nil {
				cancel.Values["reason"] = *tt.reason
			}
			srv := newChainServer(t, nil)
			result := skipEngine(t, srv.URL, skipGraph(), responses).Run(context.Background(), chainPlan(plan.Step{Node: "createOrder"}, cancel))

			require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
			assert.Equal(t, tt.wantCleanup, cleanupNodes(result.CleanupResults))
			assert.Equal(t, tt.wantSkipped, result.CleanupSkipped)
		})
	}
}

func TestCleanupSkip_PaymentPairing(t *testing.T) {
	captureStep := func(paymentID plan.StepValue) *plan.Step {
		return &plan.Step{Node: "capturePayment", DependsOn: []string{"createPayment"}, Values: map[string]plan.StepValue{"paymentId": paymentID}}
	}
	tests := []struct {
		name          string
		createPayment map[string]any
		capture       *plan.Step
		captureStatus int
		wantOutcome   Outcome
		wantCleanup   []string
		wantSkipped   []CleanupSkip
		wantWhenError string
	}{
		{name: "authorized and never captured", wantCleanup: []string{"voidPayment"}},
		{
			name:          "captured when created",
			createPayment: map[string]any{"paymentId": "pay-1", "status": "succeeded"},
			wantSkipped:   []CleanupSkip{{Node: "voidPayment", CleanupFor: "createPayment", Reason: CleanupSkipWhen, When: `status == "authorized"`}},
		},
		{
			name:        "captured later by a releasedBy node",
			capture:     captureStep(skipFrom("createPayment.paymentId")),
			wantSkipped: []CleanupSkip{{Node: "voidPayment", CleanupFor: "createPayment", Reason: CleanupSkipReleased, ReleasedBy: "capturePayment"}},
		},
		{
			// capturePayment's own status output is "captured"; the condition
			// reads only createPayment's.
			name:        "another payment captured",
			capture:     captureStep(plan.StepValue{Default: "pay-other"}),
			wantCleanup: []string{"voidPayment"},
		},
		{
			name:          "capture declined",
			capture:       captureStep(skipFrom("createPayment.paymentId")),
			captureStatus: http.StatusPaymentRequired,
			wantOutcome:   OutcomeFailed,
			wantCleanup:   []string{"voidPayment"},
		},
		{
			name:          "condition cannot be evaluated",
			createPayment: map[string]any{"paymentId": "pay-1"},
			wantCleanup:   []string{"voidPayment"},
			wantWhenError: `unknown field "status"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responses := skipResponses()
			if tt.createPayment != nil {
				responses["createPayment"] = tt.createPayment
			}
			sequences := map[string][]chainResponse{}
			if tt.captureStatus != 0 {
				sequences["/capturePayment"] = []chainResponse{{status: tt.captureStatus, body: `{}`}}
			}
			steps := []plan.Step{{Node: "createPayment"}}
			if tt.capture != nil {
				steps = append(steps, *tt.capture)
			}
			srv := newSequenceServer(t, sequences)
			result := skipEngine(t, srv.URL, skipGraph(), responses).Run(context.Background(), chainPlan(steps...))

			require.Equal(t, tt.wantOutcome, result.Outcome, "error: %v", result.Error)
			assert.Equal(t, tt.wantCleanup, nilIfEmpty(cleanupNodes(result.CleanupResults)))
			assert.Equal(t, tt.wantSkipped, result.CleanupSkipped)
			if tt.wantWhenError != "" {
				require.Len(t, result.CleanupResults, 1)
				assert.Contains(t, result.CleanupResults[0].WhenError, tt.wantWhenError)
			}
		})
	}
}

func TestCleanupSkip_ChainedWhenReadsTheParentCleanupStep(t *testing.T) {
	tests := []struct {
		status      string
		wantCleanup []string
		wantSkipped []CleanupSkip
	}{
		{status: "pending", wantCleanup: []string{"cancelOrder", "confirmCancel"}},
		{
			status:      "complete",
			wantCleanup: []string{"cancelOrder"},
			wantSkipped: []CleanupSkip{{Node: "confirmCancel", CleanupFor: "cancelOrder", Reason: CleanupSkipWhen, When: `cancellationStatus == "pending"`}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			g := skipGraph()
			g.Nodes["cancelOrder"].Cleanup.When = `cancellationStatus == "pending"`
			responses := skipResponses()
			responses["cancelOrder"] = map[string]any{"cancellationId": "cancel-1", "cancellationStatus": tt.status}
			srv := newChainServer(t, nil)
			result := skipEngine(t, srv.URL, g, responses).Run(context.Background(), chainPlan(plan.Step{Node: "createOrder"}))

			require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
			assert.Equal(t, tt.wantCleanup, cleanupNodes(result.CleanupResults))
			assert.Equal(t, tt.wantSkipped, result.CleanupSkipped)
		})
	}
}

func TestCleanupSkip_RunOnDecidesFirst(t *testing.T) {
	srv := newChainServer(t, nil)
	p := chainPlan(
		plan.Step{Node: "createCart"},
		plan.Step{Node: "deleteCart", DependsOn: []string{"createCart"}, Values: map[string]plan.StepValue{"cartId": skipFrom("createCart.cartId")}},
	)
	p.Execution.Cleanup = []plan.CleanupStep{{Node: "deleteCart", RunOn: "failure"}}
	result := skipEngine(t, srv.URL, skipGraph(), skipResponses()).Run(context.Background(), p)

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	assert.Empty(t, result.CleanupResults)
	assert.Empty(t, result.CleanupSkipped, "a cleanup runOn leaves out is not reported as skipped")
}

// skipObserver records cleanup steps and skips as the engine reports them.
type skipObserver struct {
	cleanupSteps []string
	skips        []CleanupSkip
}

func (o *skipObserver) OnRunStart(int)                      {}
func (o *skipObserver) OnStepStart(int, int, plan.Step)     {}
func (o *skipObserver) OnStepComplete(int, int, StepResult) {}
func (o *skipObserver) OnCleanupStart(int)                  {}
func (o *skipObserver) OnCleanupStepComplete(_, _ int, r StepResult) {
	o.cleanupSteps = append(o.cleanupSteps, r.Node)
}
func (o *skipObserver) OnRunComplete(*RunResult)          {}
func (o *skipObserver) OnCleanupSkipped(skip CleanupSkip) { o.skips = append(o.skips, skip) }

func TestCleanupSkip_ObserverAndArchive(t *testing.T) {
	srv := newChainServer(t, nil)
	obs := &skipObserver{}
	result := skipEngine(t, srv.URL, skipGraph(), skipResponses()).WithProgress(obs).Run(context.Background(), chainPlan(
		plan.Step{Node: "createOrder"},
		cancelOrderStep(),
	))
	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)

	assert.Equal(t, []string{"confirmCancel"}, obs.cleanupSteps)
	assert.Equal(t, result.CleanupSkipped, obs.skips)

	a, err := ToArchive(result, archive.ArchiveMetadata{}, srv.URL, nil)
	require.NoError(t, err)
	assert.Equal(t, []archive.CleanupSkipRecord{{Node: "cancelOrder", CleanupFor: "createOrder", Reason: "released", ReleasedBy: "cancelOrder"}}, a.CleanupSkipped)
}

func TestSameInputValue(t *testing.T) {
	tests := []struct {
		a, b any
		typ  string
		want bool
	}{
		{"order-1", "order-1", "string", true},
		{"order-1", "order-2", "string", false},
		{42, 42.0, "integer", true},
		{"42", 42.0, "integer", true},
		{42, 43.0, "integer", false},
		{"x", 1, "integer", false},
		{12.5, float32(12.5), "float", true},
		{true, true, "boolean", true},
		{[]any{1, "a"}, []any{1.0, "a"}, "string[]", true},
		{map[string]any{"n": 1}, map[string]any{"n": 1.0}, "address", true},
		{map[string]any{"n": 1}, map[string]any{"n": 2.0}, "address", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, sameInputValue(tt.a, tt.b, tt.typ), "%v vs %v as %s", tt.a, tt.b, tt.typ)
	}
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}
