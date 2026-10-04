package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// withReservationTools adds two nodes to the travel engine: readReservation,
// whose id input has no default, and cancelReservation, which takes an id.
func withReservationTools(t *testing.T, eng *Engine) {
	t.Helper()
	for name, method := range map[string]string{"readReservation": "GET", "cancelReservation": "DELETE"} {
		eng.graph.Nodes[name] = &graph.Node{Name: name, Adapter: "t." + name, Inputs: []graph.Input{{Name: "id", Type: "string"}}}
		require.NoError(t, eng.registry.Register("t."+name, adapter.NewTemplateAdapter(adapter.Template{
			Adapter: "t." + name, Protocol: "http",
			Request: adapter.TemplateRequest{Method: method, Path: "/reservations/{{id}}"},
		})))
	}
}

// TestFuzz_OutputLookupsKeepToTheHappyPath checks that an input matched by
// output name, as a verification step's or a plan-level cleanup's is, reads
// the happy path's resource, not a fuzz case's copy of it.
func TestFuzz_OutputLookupsKeepToTheHappyPath(t *testing.T) {
	eng, api := buildTravelEngine(t)
	withReservationTools(t, eng)
	p := travelPlan(name("name.empty", plan.FuzzNegative, ""), name("name.empty2", plan.FuzzNegative, ""))
	p.Execution.Verification = []plan.VerificationStep{{Node: "readReservation"}}
	p.Execution.Cleanup = []plan.CleanupStep{{Node: "cancelReservation"}}
	result := eng.Run(context.Background(), p)
	require.NoError(t, result.Error)
	require.Equal(t, 2, api.creates, "the happy path's reservation, and the cases' copy")
	var reads []string
	for _, req := range api.log {
		if strings.HasPrefix(req, "GET /reservations/") || strings.HasPrefix(req, "DELETE ") {
			reads = append(reads, req)
		}
	}
	assert.Equal(t, []string{"GET /reservations/r1", "DELETE /reservations/r1"}, reads)
}

// TestFuzz_CoveredSetupFailure checks that a setup step whose failure a
// knownIssue covers on the happy path is used on a copy too, rather than
// failing every case's setup.
func TestFuzz_CoveredSetupFailure(t *testing.T) {
	eng, _ := buildTravelEngine(t)
	eng.Now = clockAt("2026-10-01")
	p := travelPlan(name("name.empty", plan.FuzzNegative, ""))
	p.Execution.Steps[2].Assertions = &plan.Assertions{Mechanical: []plan.MechanicalAssertion{{Type: "status", Expect: 200}}} // it's a 201
	p.Execution.Steps[2].KnownIssue = &plan.KnownIssue{Until: mustDate(t, "2026-10-06"), Reason: "returns 201"}
	result := eng.Run(context.Background(), p)
	require.NoError(t, result.Error)
	assert.Equal(t, OutcomePassed, result.Outcome)
	assert.Equal(t, map[string]string{"name.empty": ""}, findings(result), "the case was sent, and refused")
	assert.Equal(t, []string{SetupFresh}, setups(result))
	for _, s := range result.Steps {
		if s.FuzzSetup != "" && s.Node == "addTraveler" {
			require.NotNil(t, s.KnownIssue)
			assert.True(t, s.KnownIssue.Applied, "the copy says why its failure didn't count")
			assert.False(t, s.FuzzSetupFailed)
		}
	}
	require.Len(t, result.KnownIssues, 1, "the entry is reported once, for the step it is on")
}

// TestFuzz_FailedCopyWithAnErrorBodyMadeNothing checks that a setup copy
// whose 2xx body is an error registers no cleanup, as on the happy path.
func TestFuzz_FailedCopyWithAnErrorBodyMadeNothing(t *testing.T) {
	var mu sync.Mutex
	creates := 0
	var deletes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodDelete:
			deletes = append(deletes, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/things":
			creates++
			if creates > 1 {
				_, _ = w.Write([]byte(`{"id":"req_abc","error":{"message":"no"}}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"t1"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)

	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"createThing": {Name: "createThing", Adapter: "t.createThing",
			Outputs:        []graph.Output{{Name: "id", Type: "string"}},
			Cleanup:        graph.CleanupPairing{Node: "deleteThing"},
			ErrorDetection: []graph.ErrorDetectionRule{{Path: "error", Rule: "exists"}}},
		"deleteThing": {Name: "deleteThing", Adapter: "t.deleteThing", Inputs: []graph.Input{{Name: "id", Type: "string"}}},
		"tagThing": {Name: "tagThing", Adapter: "t.tagThing", Inputs: []graph.Input{
			{Name: "id", Type: "string", Default: &graph.InputDefault{From: "createThing.id"}},
			{Name: "tag", Type: "string", Default: &graph.InputDefault{Value: "red"}},
		}},
	}}
	registry := adapter.NewRegistry()
	for name, tmpl := range map[string]adapter.Template{
		"t.createThing": {Request: adapter.TemplateRequest{Method: "POST", Path: "/things"},
			Response: adapter.TemplateResponse{Extract: map[string]adapter.ExtractRule{"id": {Path: "id"}}}},
		"t.deleteThing": {Request: adapter.TemplateRequest{Method: "DELETE", Path: "/things/{{id}}"}},
		"t.tagThing": {Request: adapter.TemplateRequest{Method: "POST", Path: "/things/{{id}}/tags",
			Headers: map[string]string{"Content-Type": "application/json"}, Body: `{"tag": "{{tag}}"}`}},
	} {
		tmpl.Adapter, tmpl.Protocol = name, "http"
		require.NoError(t, registry.Register(name, adapter.NewTemplateAdapter(tmpl)))
	}
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{}))
	p := &plan.Plan{Metadata: plan.Metadata{GraphVersion: "1.0.0"}, Execution: plan.Execution{Steps: []plan.Step{
		{ID: "thing", Node: "createThing"},
		{ID: "tag", Node: "tagThing", Values: map[string]plan.StepValue{"id": {From: "thing.id"}},
			FuzzSettings: &plan.FuzzSettings{Pinned: []plan.PinnedFuzzCase{{ID: "tag.blue", Mode: plan.FuzzPositive, Input: "tag", Value: "blue"}}}},
	}}}
	result := eng.Run(context.Background(), p)
	require.NoError(t, result.Error)
	assert.Equal(t, map[string]string{"tag.blue": FindingNotSent}, findings(result))
	for _, s := range result.Steps {
		if s.FuzzSetup != "" {
			assert.True(t, s.FuzzSetupFailed)
		}
	}
	assert.Equal(t, []string{"/things/t1"}, deletes, "the copy's error body named no thing that exists")
}

// fuzzThenAgainPlan fuzzes add with a case the buggy server fails on, then
// adds again.
func fuzzThenAgainPlan() *plan.Plan {
	p := quantityPlan()
	p.Execution.Steps[0].FuzzSettings = &plan.FuzzSettings{Pinned: []plan.PinnedFuzzCase{
		{ID: "quantity.zero", Mode: plan.FuzzPositive, Input: "quantity", Value: 0},
	}}
	p.Execution.Steps = append(p.Execution.Steps, plan.Step{ID: "again", Node: "addItem"})
	return p
}

// TestFuzz_StopAfter checks that --stop-after keeps a fuzz failure, and
// refuses to stop at a fuzz case.
func TestFuzz_StopAfter(t *testing.T) {
	server := buggyQuantityServer(t)

	result := buildQuantityEngine(t, server.URL).WithStopAfter("again").Run(context.Background(), fuzzThenAgainPlan())
	assert.Equal(t, OutcomeFailed, result.Outcome, "the fuzz case's server error is not lost")
	assert.True(t, result.Stopped)
	assert.Equal(t, "again", result.StoppedAt)
	assert.EqualError(t, result.Error, `fuzzing found 1 server-error (stopped at "again")`)
	assert.Empty(t, result.CleanupResults)

	result = buildQuantityEngine(t, server.URL).WithStopAfter("add__fuzz_quantity_zero").Run(context.Background(), fuzzThenAgainPlan())
	assert.Equal(t, OutcomeError, result.Outcome)
	assert.ErrorContains(t, result.Error, `"add__fuzz_quantity_zero" is a fuzz case of step "add"`)
}

// TestFuzz_FailureSurvivesAnEarlyEnd checks that a run a fuzz finding failed,
// and that a covered failure then ended, still says why it failed.
func TestFuzz_FailureSurvivesAnEarlyEnd(t *testing.T) {
	server := buggyQuantityServer(t)
	eng := buildQuantityEngine(t, server.URL)
	eng.Now = clockAt("2026-10-01")
	p := fuzzThenAgainPlan()
	p.Execution.Steps[1].Values = map[string]plan.StepValue{"quantity": {Default: -1}} // a 400
	p.Execution.Steps[1].KnownIssue = &plan.KnownIssue{Until: mustDate(t, "2026-10-06"), Reason: "refuses"}
	result := eng.Run(context.Background(), p)
	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.EqualError(t, result.Error, "fuzzing found 1 server-error")
}

// TestFuzz_ProgressCountsWhatAReaderFollows checks that progress numbers the
// plan's steps and fuzz cases one after another, with a total that holds no
// setup copies: a copy takes the number of the case it was made for.
func TestFuzz_ProgressCountsWhatAReaderFollows(t *testing.T) {
	eng, _ := buildTravelEngine(t)
	obs := &recordingObserver{}
	eng.WithProgress(obs)
	result := eng.Run(context.Background(), travelPlan(
		name("name.empty", plan.FuzzNegative, ""), name("name.ok", plan.FuzzPositive, "Lin"), name("name.empty2", plan.FuzzNegative, "")))
	require.NoError(t, result.Error)

	require.Equal(t, "run_start", obs.events[0].kind)
	assert.Equal(t, 7, obs.events[0].total, "four plan steps and three cases")
	var caseIndexes, copyIndexes []int
	for _, ev := range obs.events {
		if ev.kind != "step_complete" {
			continue
		}
		assert.Equal(t, 7, ev.total)
		r := result.Steps[len(caseIndexes)+len(copyIndexes)]
		if r.FuzzSetup != "" {
			copyIndexes = append(copyIndexes, ev.index)
			continue
		}
		caseIndexes = append(caseIndexes, ev.index)
	}
	assert.Equal(t, []int{0, 1, 2, 3, 4, 5, 6}, caseIndexes)
	assert.NotEmpty(t, copyIndexes)
	for _, idx := range copyIndexes {
		assert.Contains(t, []int{4, 5, 6}, idx, "a copy takes its case's number")
	}
}

// TestFuzz_VerificationNeverReleasesACleanup checks that when reuse leaves a
// setup copy unsent, a verification step is still not taken for a main step
// that released a resource: the steps are told apart by ID, not counted off.
func TestFuzz_VerificationNeverReleasesACleanup(t *testing.T) {
	var mu sync.Mutex
	var deletes []string
	creates := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodDelete:
			deletes = append(deletes, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/things":
			creates++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"t` + strconv.Itoa(creates) + `"}`))
		default:
			var body struct {
				Tag string `json:"tag"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Tag == "" {
				w.WriteHeader(http.StatusBadRequest)
			}
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"createThing": {Name: "createThing", Adapter: "t.createThing", Outputs: []graph.Output{{Name: "id", Type: "string"}},
			Cleanup: graph.CleanupPairing{Node: "deleteThing"}},
		"deleteThing": {Name: "deleteThing", Adapter: "t.deleteThing", Inputs: []graph.Input{{Name: "id", Type: "string"}}},
		"tagThing": {Name: "tagThing", Adapter: "t.tagThing", Inputs: []graph.Input{
			{Name: "id", Type: "string"}, {Name: "tag", Type: "string", Default: &graph.InputDefault{Value: "red"}}}},
	}}
	registry := adapter.NewRegistry()
	for name, tmpl := range map[string]adapter.Template{
		"t.createThing": {Request: adapter.TemplateRequest{Method: "POST", Path: "/things"},
			Response: adapter.TemplateResponse{Extract: map[string]adapter.ExtractRule{"id": {Path: "id"}}}},
		"t.deleteThing": {Request: adapter.TemplateRequest{Method: "DELETE", Path: "/things/{{id}}"}},
		"t.tagThing":    {Request: adapter.TemplateRequest{Method: "POST", Path: "/things/{{id}}/tags", Body: `{"tag": "{{tag}}"}`}},
	} {
		tmpl.Adapter, tmpl.Protocol = name, "http"
		require.NoError(t, registry.Register(name, adapter.NewTemplateAdapter(tmpl)))
	}
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{}))
	p := &plan.Plan{Metadata: plan.Metadata{GraphVersion: "1.0.0"}, Execution: plan.Execution{
		Steps: []plan.Step{
			{ID: "thing", Node: "createThing"},
			{ID: "tag", Node: "tagThing", Values: map[string]plan.StepValue{"id": {From: "thing.id"}},
				FuzzSettings: &plan.FuzzSettings{Pinned: []plan.PinnedFuzzCase{
					{ID: "tag.a", Mode: plan.FuzzNegative, Input: "tag", Value: ""},
					{ID: "tag.b", Mode: plan.FuzzNegative, Input: "tag", Value: ""},
				}}},
		},
		Verification: []plan.VerificationStep{{Node: "deleteThing", Values: map[string]plan.StepValue{"id": {From: "thing.id"}}}},
	}}
	result := eng.Run(context.Background(), p)
	require.NoError(t, result.Error)
	require.Equal(t, 1, result.FuzzCopiesSkipped, "the second case reused the first's copy")
	assert.Empty(t, result.CleanupSkipped, "the verification's DELETE released nothing a main step made")
	assert.ElementsMatch(t, []string{"/things/t1", "/things/t2", "/things/t1"}, deletes,
		"the verification deletes t1, then cleanup deletes the copy's t2 and t1 as registered")
}
