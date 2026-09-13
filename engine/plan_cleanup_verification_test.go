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

// lifecycleGraph models create → get → delete with a graph-level cleanup
// pairing on createThing, plus two nodes only reachable via plan-level cleanup.
func lifecycleGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"createThing": {
				Name: "createThing", Adapter: "test.create", Cleanup: graph.CleanupPairing{Node: "deleteThing"},
				Outputs: []graph.Output{{Name: "thingId", Type: "string"}},
			},
			"getThing": {
				Name: "getThing", Adapter: "test.get",
				Inputs:  []graph.Input{{Name: "thingId", Type: "string"}},
				Outputs: []graph.Output{{Name: "status", Type: "string"}},
			},
			"deleteThing": {
				Name: "deleteThing", Adapter: "test.delete",
				Inputs: []graph.Input{{Name: "thingId", Type: "string"}},
			},
			"cancelThing": {
				Name: "cancelThing", Adapter: "test.cancel",
				Inputs: []graph.Input{{Name: "thingId", Type: "string"}},
			},
			"notifyFailure": {Name: "notifyFailure", Adapter: "test.notify"},
		},
	}
}

// lifecycleServer records request paths in order and lets a test fail specific paths.
type lifecycleServer struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
	fail  map[string]int // path → status to return
}

func newLifecycleServer(t *testing.T, fail map[string]int) *lifecycleServer {
	t.Helper()
	ls := &lifecycleServer{fail: fail}
	ls.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ls.mu.Lock()
		ls.paths = append(ls.paths, r.URL.Path)
		ls.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if code, ok := ls.fail[r.URL.Path]; ok {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(ls.Close)
	return ls
}

func (ls *lifecycleServer) requestPaths() []string {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	out := make([]string, len(ls.paths))
	copy(out, ls.paths)
	return out
}

func lifecycleEngine(t *testing.T, serverURL string) *Engine {
	t.Helper()
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.create", &stubAdapter{method: "POST", path: "/create", response: map[string]any{"thingId": "t1"}}))
	require.NoError(t, registry.Register("test.get", &stubAdapter{method: "GET", path: "/get", response: map[string]any{"status": "active"}}))
	require.NoError(t, registry.Register("test.delete", &stubAdapter{method: "DELETE", path: "/delete", response: map[string]any{}}))
	require.NoError(t, registry.Register("test.cancel", &stubAdapter{method: "POST", path: "/cancel", response: map[string]any{}}))
	require.NoError(t, registry.Register("test.notify", &stubAdapter{method: "POST", path: "/notify", response: map[string]any{}}))
	executor := adapter.NewHTTPExecutor(serverURL)
	return NewEngine(lifecycleGraph(), registry, NewExecutorRouter(executor, &adapter.EnvironmentConfig{}))
}

func lifecyclePlan(cleanup []plan.CleanupStep, verification []plan.VerificationStep) *plan.Plan {
	return &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps:        []plan.Step{{Node: "createThing"}},
			Cleanup:      cleanup,
			Verification: verification,
		},
	}
}

func cleanupNodes(results []StepResult) []string {
	names := make([]string, 0, len(results))
	for _, r := range results {
		names = append(names, r.Node)
	}
	return names
}

func TestPlanCleanup_RunsInDeclarationOrderBeforeGraphStack(t *testing.T) {
	srv := newLifecycleServer(t, nil)
	eng := lifecycleEngine(t, srv.URL)

	result := eng.Run(context.Background(), lifecyclePlan([]plan.CleanupStep{
		{Node: "cancelThing", RunOn: "always"},
		{Node: "notifyFailure"},
	}, nil))

	require.Equal(t, OutcomePassed, result.Outcome)
	// Plan-level entries first (declaration order), then the graph FILO stack.
	assert.Equal(t, []string{"cancelThing", "notifyFailure", "deleteThing"}, cleanupNodes(result.CleanupResults))
	assert.Equal(t, []string{"/create", "/cancel", "/notify", "/delete"}, srv.requestPaths())
	// Inputs are matched by output name against earlier steps.
	assert.Equal(t, "t1", result.CleanupResults[0].Inputs["thingId"])
}

func TestPlanCleanup_ListedPairingRunsOncePerResource(t *testing.T) {
	srv := newLifecycleServer(t, nil)
	eng := lifecycleEngine(t, srv.URL)

	result := eng.Run(context.Background(), lifecyclePlan([]plan.CleanupStep{{Node: "deleteThing"}}, nil))

	require.Equal(t, OutcomePassed, result.Outcome)
	assert.Equal(t, []string{"deleteThing"}, cleanupNodes(result.CleanupResults))
	assert.Equal(t, []string{"/create", "/delete"}, srv.requestPaths())
}

func TestPlanCleanup_RunOnSuccessAndFailure(t *testing.T) {
	cleanup := []plan.CleanupStep{
		{Node: "notifyFailure", RunOn: "failure"},
		{Node: "cancelThing", RunOn: "success"},
	}

	t.Run("passed run", func(t *testing.T) {
		srv := newLifecycleServer(t, nil)
		result := lifecycleEngine(t, srv.URL).Run(context.Background(), lifecyclePlan(cleanup, nil))
		require.Equal(t, OutcomePassed, result.Outcome)
		assert.Equal(t, []string{"cancelThing", "deleteThing"}, cleanupNodes(result.CleanupResults))
	})

	t.Run("failed run", func(t *testing.T) {
		srv := newLifecycleServer(t, map[string]int{"/create": 500})
		result := lifecycleEngine(t, srv.URL).Run(context.Background(), lifecyclePlan(cleanup, nil))
		require.Equal(t, OutcomeFailed, result.Outcome)
		// createThing failed, so no graph-level entry was pushed; only the failure hook runs.
		assert.Equal(t, []string{"notifyFailure"}, cleanupNodes(result.CleanupResults))
	})
}

// sequenceAdapter returns a new thingId on every call: t1, t2, ...
type sequenceAdapter struct {
	stubAdapter
	mu sync.Mutex
	n  int
}

func (a *sequenceAdapter) ExtractOutputs(*adapter.Response) (map[string]any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n++
	return map[string]any{"thingId": fmt.Sprintf("t%d", a.n)}, nil
}

func cleanupInputs(results []StepResult, name string) []any {
	values := make([]any, 0, len(results))
	for _, r := range results {
		values = append(values, r.Inputs[name])
	}
	return values
}

func TestCleanup_EachStepCleansUpItsOwnResource(t *testing.T) {
	srv := newLifecycleServer(t, nil)
	eng := lifecycleEngine(t, srv.URL)
	require.NoError(t, eng.registry.Register("test.createSeq", &sequenceAdapter{stubAdapter: stubAdapter{method: "POST", path: "/create"}}))
	eng.graph.Nodes["createThing"].Adapter = "test.createSeq"

	result := eng.Run(context.Background(), &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{
			{ID: "first", Node: "createThing"},
			{ID: "second", Node: "createThing", DependsOn: []string{"first"}},
		}},
	})

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	assert.Equal(t, []string{"deleteThing", "deleteThing"}, cleanupNodes(result.CleanupResults))
	// Last created, first deleted, and each deletion targets its own resource.
	assert.Equal(t, []any{"t2", "t1"}, cleanupInputs(result.CleanupResults, "thingId"))
}

func TestCleanup_RegisteringStepWinsOverLaterOutputs(t *testing.T) {
	srv := newLifecycleServer(t, nil)
	eng := lifecycleEngine(t, srv.URL)
	eng.graph.Nodes["findThing"] = &graph.Node{
		Name: "findThing", Adapter: "test.find",
		Outputs: []graph.Output{{Name: "thingId", Type: "string"}},
	}
	require.NoError(t, eng.registry.Register("test.find", &stubAdapter{method: "GET", path: "/find", response: map[string]any{"thingId": "t-other"}}))

	result := eng.Run(context.Background(), &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{
			{ID: "made", Node: "createThing"},
			{ID: "lookup", Node: "findThing", DependsOn: []string{"made"}},
		}},
	})

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	require.Len(t, result.CleanupResults, 1)
	assert.Equal(t, "t1", result.CleanupResults[0].Inputs["thingId"], "the step that created the resource, not the latest thingId")
}

func TestPlanCleanup_ListedPairingsUnwindLastInFirstOut(t *testing.T) {
	// Composition writes graph pairings into execution.cleanup in step order;
	// they must still release the newest resource first.
	srv := newLifecycleServer(t, nil)
	eng := lifecycleEngine(t, srv.URL)
	eng.graph.Nodes["createPart"] = &graph.Node{
		Name: "createPart", Adapter: "test.createPart", Cleanup: graph.CleanupPairing{Node: "deletePart"},
		Outputs: []graph.Output{{Name: "partId", Type: "string"}},
	}
	eng.graph.Nodes["deletePart"] = &graph.Node{
		Name: "deletePart", Adapter: "test.deletePart",
		Inputs: []graph.Input{{Name: "partId", Type: "string"}},
	}
	require.NoError(t, eng.registry.Register("test.createPart", &stubAdapter{method: "POST", path: "/createPart", response: map[string]any{"partId": "p1"}}))
	require.NoError(t, eng.registry.Register("test.deletePart", &stubAdapter{method: "DELETE", path: "/deletePart", response: map[string]any{}}))

	result := eng.Run(context.Background(), &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "createThing"},
				{Node: "createPart", DependsOn: []string{"createThing"}},
			},
			Cleanup: []plan.CleanupStep{
				{Node: "deleteThing", RunOn: "always"},
				{Node: "deletePart", RunOn: "always"},
			},
		},
	})

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	assert.Equal(t, []string{"deletePart", "deleteThing"}, cleanupNodes(result.CleanupResults))
	assert.Equal(t, []string{"/create", "/createPart", "/deletePart", "/delete"}, srv.requestPaths())
}

func TestPlanCleanup_ListedPairingSkippedWhenNothingWasCreated(t *testing.T) {
	srv := newLifecycleServer(t, map[string]int{"/create": 500})
	result := lifecycleEngine(t, srv.URL).Run(context.Background(), lifecyclePlan([]plan.CleanupStep{{Node: "deleteThing", RunOn: "always"}}, nil))

	require.Equal(t, OutcomeFailed, result.Outcome)
	assert.Empty(t, result.CleanupResults, "no request for a resource that was never created")
	assert.Equal(t, []string{"/create"}, srv.requestPaths())
}

func TestPlanCleanup_RunOnGatesListedPairing(t *testing.T) {
	cleanup := []plan.CleanupStep{{Node: "deleteThing", RunOn: "failure"}}

	t.Run("passed run", func(t *testing.T) {
		srv := newLifecycleServer(t, nil)
		result := lifecycleEngine(t, srv.URL).Run(context.Background(), lifecyclePlan(cleanup, nil))
		require.Equal(t, OutcomePassed, result.Outcome)
		assert.Empty(t, result.CleanupResults)
	})

	t.Run("failed run", func(t *testing.T) {
		// createThing succeeds, so its pairing is registered; the failing
		// verification makes the run fail.
		srv := newLifecycleServer(t, map[string]int{"/get": 500})
		result := lifecycleEngine(t, srv.URL).Run(context.Background(), lifecyclePlan(cleanup, verifyActive()))
		require.Equal(t, OutcomeFailed, result.Outcome)
		assert.Equal(t, []string{"deleteThing"}, cleanupNodes(result.CleanupResults))
	})
}

func TestPlanCleanup_FailureDoesNotChangeOutcome(t *testing.T) {
	srv := newLifecycleServer(t, map[string]int{"/cancel": 500})
	result := lifecycleEngine(t, srv.URL).Run(context.Background(), lifecyclePlan([]plan.CleanupStep{{Node: "cancelThing"}}, nil))

	assert.Equal(t, OutcomePassed, result.Outcome)
	require.Len(t, result.CleanupResults, 2)
	assert.Equal(t, 500, result.CleanupResults[0].StatusCode)
}

func verifyActive() []plan.VerificationStep {
	return []plan.VerificationStep{{
		Node:    "getThing",
		Purpose: "thing is active after creation",
		Assertions: &plan.Assertions{Mechanical: []plan.MechanicalAssertion{
			{Type: "fieldEquals", Path: "status", Value: "active"},
		}},
	}}
}

func TestVerification_RunsAfterMainStepsBeforeCleanup(t *testing.T) {
	srv := newLifecycleServer(t, nil)
	result := lifecycleEngine(t, srv.URL).Run(context.Background(), lifecyclePlan(nil, verifyActive()))

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	require.Len(t, result.Steps, 2)
	ver := result.Steps[1]
	assert.Equal(t, "verify_getThing", ver.StepID)
	assert.Equal(t, "getThing", ver.Node)
	assert.Equal(t, "t1", ver.Inputs["thingId"], "input matched by output name")
	require.NotNil(t, ver.Validation)
	assert.True(t, ver.Validation.Passed)
	assert.Equal(t, []string{"/create", "/get", "/delete"}, srv.requestPaths())
}

func TestVerification_AssertionFailureFailsRun(t *testing.T) {
	srv := newLifecycleServer(t, nil)
	eng := lifecycleEngine(t, srv.URL)
	verification := []plan.VerificationStep{{
		Node: "getThing",
		Assertions: &plan.Assertions{Mechanical: []plan.MechanicalAssertion{
			{Type: "fieldEquals", Path: "status", Value: "archived"},
		}},
	}}

	result := eng.Run(context.Background(), lifecyclePlan(nil, verification))

	assert.Equal(t, OutcomeFailed, result.Outcome)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "verify_getThing")
	assert.Equal(t, []string{"deleteThing"}, cleanupNodes(result.CleanupResults), "cleanup still runs")
}

func TestVerification_ErrorStatusFailsRun(t *testing.T) {
	srv := newLifecycleServer(t, map[string]int{"/get": 500})
	result := lifecycleEngine(t, srv.URL).Run(context.Background(), lifecyclePlan(nil, verifyActive()))

	assert.Equal(t, OutcomeFailed, result.Outcome)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "status 500")
}

func TestVerification_SkippedWhenMainFlowFails(t *testing.T) {
	srv := newLifecycleServer(t, map[string]int{"/create": 500})
	result := lifecycleEngine(t, srv.URL).Run(context.Background(), lifecyclePlan(nil, verifyActive()))

	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.Len(t, result.Steps, 1)
	assert.NotContains(t, srv.requestPaths(), "/get")
}

func TestVerification_SkippedAtCheckpoint(t *testing.T) {
	srv := newLifecycleServer(t, nil)
	eng := lifecycleEngine(t, srv.URL).WithStopAfter("createThing")
	result := eng.Run(context.Background(), lifecyclePlan([]plan.CleanupStep{{Node: "cancelThing"}}, verifyActive()))

	assert.Equal(t, OutcomeStopped, result.Outcome)
	assert.Len(t, result.Steps, 1)
	assert.Empty(t, result.CleanupResults)
	assert.Equal(t, []string{"/create"}, srv.requestPaths())
}

func TestVerification_UsesGraphDefaultFromRef(t *testing.T) {
	// getThing.thingId declares a graph default {from: createThing.thingId};
	// VerificationSteps must translate it exactly as main-step instantiation does.
	srv := newLifecycleServer(t, nil)
	eng := lifecycleEngine(t, srv.URL)
	eng.graph.Nodes["getThing"].Inputs[0].Default = &graph.InputDefault{From: "createThing.thingId"}

	result := eng.Run(context.Background(), lifecyclePlan(nil, verifyActive()))

	require.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
	require.Len(t, result.Steps, 2)
	assert.Equal(t, "t1", result.Steps[1].Inputs["thingId"])
}

func TestRunState_ExecutedStepsPreservesOrder(t *testing.T) {
	state := NewRunState()
	state.StoreOutputs("b", map[string]any{"x": 1})
	state.StoreOutputs("a", map[string]any{"x": 2})
	state.StoreOutputs("b", map[string]any{"x": 3})
	assert.Equal(t, []string{"b", "a"}, state.ExecutedSteps())
}

func TestCleanupRunOnMatches(t *testing.T) {
	assert.True(t, cleanupRunOnMatches("", OutcomeFailed))
	assert.True(t, cleanupRunOnMatches("always", OutcomeError))
	assert.True(t, cleanupRunOnMatches("success", OutcomePassed))
	assert.False(t, cleanupRunOnMatches("success", OutcomeFailed))
	assert.True(t, cleanupRunOnMatches("failure", OutcomeAborted))
	assert.False(t, cleanupRunOnMatches("failure", OutcomePassed))
	assert.False(t, cleanupRunOnMatches("bogus", OutcomePassed))
}
