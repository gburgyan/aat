package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// travelAPI is a fake booking API: POST /reservations makes a reservation,
// and POST /reservations/{id}/travelers adds a traveler to it, at most two per
// reservation (409 beyond that). A name that is empty or longer than 20 is
// refused with a 400, and "crash" fails with a 500. GET /airports lists
// airports, and GET /reservations/{id} reads a reservation. With
// failAfterFirst, every reservation request after the first fails with a 503;
// with roomyCopies, every reservation after the first takes three travelers.
type travelAPI struct {
	mu             sync.Mutex
	reservations   map[string]int
	creates        int
	airports       int
	reads          int
	failAfterFirst bool
	roomyCopies    bool
}

func (a *travelAPI) handler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/airports":
		a.airports++
		_, _ = w.Write([]byte(`{"airports":["DEN","SFO"]}`))
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/reservations/"):
		a.reads++
		_, _ = fmt.Fprintf(w, `{"travelers":%d}`, a.reservations[strings.TrimPrefix(r.URL.Path, "/reservations/")])
	case r.Method == http.MethodPost && r.URL.Path == "/reservations":
		a.creates++
		if a.failAfterFirst && a.creates > 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		id := fmt.Sprintf("r%d", a.creates)
		a.reservations[id] = 0
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, `{"id":%q}`, id)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/travelers"):
		id := strings.Split(r.URL.Path, "/")[2]
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		limit := 2
		if a.roomyCopies && id != "r1" {
			limit = 3
		}
		switch {
		case body.Name == "crash":
			w.WriteHeader(http.StatusInternalServerError)
		case body.Name == "" || len(body.Name) > 20:
			w.WriteHeader(http.StatusBadRequest)
		case a.reservations[id] >= limit:
			w.WriteHeader(http.StatusConflict)
		default:
			a.reservations[id]++
			w.WriteHeader(http.StatusCreated)
		}
		_, _ = w.Write([]byte(`{}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func buildTravelEngine(t *testing.T) (*Engine, *travelAPI) {
	t.Helper()
	api := &travelAPI{reservations: map[string]int{}}
	server := httptest.NewServer(http.HandlerFunc(api.handler))
	t.Cleanup(server.Close)

	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"listAirports":      {Name: "listAirports", Adapter: "t.listAirports", Outputs: []graph.Output{{Name: "airports", Type: "string[]"}}},
		"createReservation": {Name: "createReservation", Adapter: "t.createReservation", Outputs: []graph.Output{{Name: "id", Type: "string"}}},
		"getReservation": {Name: "getReservation", Adapter: "t.getReservation", Inputs: []graph.Input{
			{Name: "reservationId", Type: "string", Default: &graph.InputDefault{From: "createReservation.id"}},
		}},
		"addTraveler": {Name: "addTraveler", Adapter: "t.addTraveler", Inputs: []graph.Input{
			{Name: "reservationId", Type: "string", Default: &graph.InputDefault{From: "createReservation.id"}},
			{Name: "name", Type: "string", Default: &graph.InputDefault{Value: "Ada"}},
		}},
	}}
	registry := adapter.NewRegistry()
	for name, tmpl := range map[string]adapter.Template{
		"t.listAirports": {Request: adapter.TemplateRequest{Method: "GET", Path: "/airports"},
			Response: adapter.TemplateResponse{Extract: map[string]adapter.ExtractRule{"airports": {Path: "airports"}}}},
		"t.getReservation": {Request: adapter.TemplateRequest{Method: "GET", Path: "/reservations/{{reservationId}}"}},
		"t.createReservation": {Request: adapter.TemplateRequest{Method: "POST", Path: "/reservations"},
			Response: adapter.TemplateResponse{Extract: map[string]adapter.ExtractRule{"id": {Path: "id"}}}},
		"t.addTraveler": {Request: adapter.TemplateRequest{Method: "POST", Path: "/reservations/{{reservationId}}/travelers",
			Headers: map[string]string{"Content-Type": "application/json"}, Body: `{"name": "{{name}}"}`}},
	} {
		tmpl.Adapter, tmpl.Protocol = name, "http"
		require.NoError(t, registry.Register(name, adapter.NewTemplateAdapter(tmpl)))
	}
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{}))
	return eng, api
}

// travelPlan lists airports, makes a reservation, and adds two travelers: the
// second is the fuzz target, one away from the limit.
func travelPlan(cases ...plan.PinnedFuzzCase) *plan.Plan {
	p := &plan.Plan{Metadata: plan.Metadata{GraphVersion: "1.0.0"}, Execution: plan.Execution{Steps: []plan.Step{
		{ID: "airports", Node: "listAirports"},
		{ID: "res", Node: "createReservation", DependsOn: []string{"airports"}},
		{ID: "t1", Node: "addTraveler", Values: map[string]plan.StepValue{"reservationId": {From: "res.id"}}},
		{ID: "t2", Node: "addTraveler", Values: map[string]plan.StepValue{"reservationId": {From: "res.id"}, "name": {Default: "Grace"}}},
	}}}
	if len(cases) > 0 {
		p.Execution.Steps[3].FuzzSettings = &plan.FuzzSettings{Pinned: cases}
	}
	return p
}

func name(id, mode, value string) plan.PinnedFuzzCase {
	return plan.PinnedFuzzCase{ID: id, Mode: mode, Input: "name", Value: value}
}

func fuzzSteps(r *RunResult) []StepResult {
	var out []StepResult
	for _, s := range r.Steps {
		if s.Fuzz != nil {
			out = append(out, s)
		}
	}
	return out
}

func setups(r *RunResult) []string {
	var out []string
	for _, s := range fuzzSteps(r) {
		out = append(out, s.Fuzz.Setup)
	}
	return out
}

func TestFuzzReuse_RefusedCasesShareASetup(t *testing.T) {
	eng, api := buildTravelEngine(t)
	result := eng.Run(context.Background(), travelPlan(
		name("name.empty", plan.FuzzNegative, ""),
		name("name.long", plan.FuzzNegative, strings.Repeat("x", 30)),
		name("name.empty2", plan.FuzzNegative, ""),
	))
	require.NoError(t, result.Error)
	assert.Equal(t, []string{SetupFresh, SetupReused, SetupReused}, setups(result))
	assert.Equal(t, 2, api.creates, "the happy path's reservation, and one for all three refused cases")
	assert.Equal(t, 1, api.airports, "a read-only setup step is never copied")
	assert.Equal(t, 4, result.FuzzCopiesSkipped, "two copies (reservation, first traveler) reused twice")
}

func TestFuzzReuse_AcceptedOrFailedCaseMakesTheNextFresh(t *testing.T) {
	eng, api := buildTravelEngine(t)
	result := eng.Run(context.Background(), travelPlan(
		name("name.empty", plan.FuzzNegative, ""),
		name("name.ok", plan.FuzzPositive, "Linus"), // accepted: the reservation changed
		name("name.empty2", plan.FuzzNegative, ""),  // so this one sets up afresh
		name("name.crash", plan.FuzzEdge, "crash"),  // a 500 may have half-written something
		name("name.empty3", plan.FuzzNegative, ""),  // afresh again
	))
	assert.Equal(t, []string{SetupFresh, SetupReused, SetupFresh, SetupReused, SetupFresh}, setups(result))
	assert.Equal(t, 4, api.creates)
	assert.EqualError(t, result.Error, "fuzzing found 1 server-error")
}

func TestFuzzReuse_TravelerLimitNeverReached(t *testing.T) {
	cases := []plan.PinnedFuzzCase{
		name("name.a", plan.FuzzPositive, "Ann"), name("name.b", plan.FuzzPositive, "Bob"),
		name("name.c", plan.FuzzPositive, "Cy"), name("name.d", plan.FuzzPositive, "Di"),
	}

	eng, _ := buildTravelEngine(t)
	result := eng.Run(context.Background(), travelPlan(cases...))
	require.NoError(t, result.Error)
	for _, s := range fuzzSteps(result) {
		assert.Equal(t, http.StatusCreated, s.StatusCode, "%s: each accepted traveler got a reservation of its own", s.Fuzz.Case.ID)
		assert.Empty(t, s.Fuzz.Finding)
	}

	// Shared scope piles them onto the happy path's reservation, and warns.
	eng, _ = buildTravelEngine(t)
	p := travelPlan(cases...)
	p.Execution.Steps[3].FuzzSettings.Scope = plan.FuzzScopeShared
	result = eng.Run(context.Background(), p)
	for _, s := range fuzzSteps(result) {
		assert.Equal(t, http.StatusConflict, s.StatusCode, "the reservation already has two travelers")
		assert.Equal(t, FindingRejectedValid, s.Fuzz.Finding)
	}
	require.Len(t, result.FuzzWarnings, 1)
	assert.Contains(t, result.FuzzWarnings[0], "fuzz scope shared on t2")
}

func TestFuzzReuse_Isolated(t *testing.T) {
	eng, api := buildTravelEngine(t)
	p := travelPlan(name("name.empty", plan.FuzzNegative, ""), name("name.empty2", plan.FuzzNegative, ""), name("name.empty3", plan.FuzzNegative, ""))
	p.Execution.Steps[3].FuzzSettings.Scope = plan.FuzzScopeIsolated
	result := eng.Run(context.Background(), p)
	require.NoError(t, result.Error)
	assert.Equal(t, []string{SetupFresh, SetupFresh, SetupFresh}, setups(result))
	assert.Equal(t, 4, api.creates)
}

func TestFuzzReuse_SetupKeepsFailing(t *testing.T) {
	eng, api := buildTravelEngine(t)
	api.failAfterFirst = true // the happy path gets its reservation; every copy is refused
	result := eng.Run(context.Background(), travelPlan(
		name("name.a", plan.FuzzPositive, "Ann"), name("name.b", plan.FuzzPositive, "Bob"),
		name("name.c", plan.FuzzPositive, "Cy"), name("name.d", plan.FuzzPositive, "Di"), name("name.e", plan.FuzzPositive, "Ed"),
	))
	require.NoError(t, result.Error, "a case whose setup failed doesn't fail the run")
	assert.Equal(t, []string{SetupFailed, SetupFailed, SetupFailed, SetupFailed, SetupFailed}, setups(result))
	for _, s := range fuzzSteps(result) {
		assert.Equal(t, FindingNotSent, s.Fuzz.Finding)
	}
	assert.Equal(t, 4, api.creates, "the happy path's, then three that failed; the last two cases were not tried")
	assert.Contains(t, fuzzSteps(result)[4].Error.Error(), "the setup for t2 failed 3 times in a row")
}

func TestReadOnlyStep(t *testing.T) {
	eng, _ := buildTravelEngine(t)
	assert.True(t, eng.readOnlyStep(plan.Step{Node: "listAirports"}))
	assert.False(t, eng.readOnlyStep(plan.Step{Node: "createReservation"}))
	eng.graph.Nodes["listAirports"].Cleanup.Node = "x"
	assert.False(t, eng.readOnlyStep(plan.Step{Node: "listAirports"}), "a node with a cleanup pairing makes something")
}

// TestFuzzReuse_CopiedExpressionsParse checks that a setup copy whose value
// reads an earlier step by name, as Duffel's getOffer assertion reads
// {{search.firstSliceOrigin}}, still parses once the name is the copy's.
func TestFuzzReuse_CopiedExpressionsParse(t *testing.T) {
	eng, _ := buildTravelEngine(t)
	p := travelPlan(name("name.empty", plan.FuzzNegative, ""))
	p.Execution.Steps[2].Values["name"] = plan.StepValue{Default: "{{res.id}}"}
	result := eng.Run(context.Background(), p)
	require.NoError(t, result.Error)
	assert.Equal(t, []string{SetupFresh}, setups(result), "the copy of t1 resolved its expression")
	for _, st := range result.Steps {
		if st.FuzzSetup != "" && st.Node == "addTraveler" {
			assert.Equal(t, "r2", st.Inputs["name"], "it read its own reservation's copy")
		}
	}
}

func header(id string) plan.PinnedFuzzCase {
	return plan.PinnedFuzzCase{ID: id, Mode: plan.FuzzEdge, Patch: []plan.RequestPatch{{Where: "header", Path: "X-Case", Op: "set", Value: id}}}
}

// TestFuzzReuse_ReadOnlyTargetKeepsItsSetup checks that cases the API accepts
// on a target that only reads leave the setup as it was.
func TestFuzzReuse_ReadOnlyTargetKeepsItsSetup(t *testing.T) {
	eng, api := buildTravelEngine(t)
	p := travelPlan()
	p.Execution.Steps = append(p.Execution.Steps, plan.Step{ID: "read", Node: "getReservation",
		Values:       map[string]plan.StepValue{"reservationId": {From: "res.id"}},
		FuzzSettings: &plan.FuzzSettings{Pinned: []plan.PinnedFuzzCase{header("h.a"), header("h.b"), header("h.c")}}})
	result := eng.Run(context.Background(), p)
	require.NoError(t, result.Error)
	assert.Equal(t, []string{SetupFresh, SetupReused, SetupReused}, setups(result))
	assert.Equal(t, 2, api.creates)
}

// TestFuzzReuse_ExpectFailureCopyEndsOnlyItsCase checks that a setup copy of
// an expectFailure step that gets a success ends its case, not the run.
func TestFuzzReuse_ExpectFailureCopyEndsOnlyItsCase(t *testing.T) {
	eng, api := buildTravelEngine(t)
	api.roomyCopies = true // the happy path's third traveler is refused; a copy's is not
	p := travelPlan()
	p.Execution.Steps = append(p.Execution.Steps,
		plan.Step{ID: "t3", Node: "addTraveler", Values: map[string]plan.StepValue{"reservationId": {From: "res.id"}, "name": {Default: "Cy"}},
			ExpectFailure: &plan.ExpectFailure{Status: plan.ExpectedStatuses{{Code: 409}}}},
		plan.Step{ID: "read", Node: "getReservation", Values: map[string]plan.StepValue{"reservationId": {From: "res.id"}},
			FuzzSettings: &plan.FuzzSettings{Pinned: []plan.PinnedFuzzCase{header("h.a")}}},
	)
	result := eng.Run(context.Background(), p)
	require.NoError(t, result.Error)
	assert.Equal(t, OutcomePassed, result.Outcome)
	require.Len(t, fuzzSteps(result), 1)
	assert.Equal(t, FindingNotSent, fuzzSteps(result)[0].Fuzz.Finding)
	assert.Equal(t, []string{SetupFailed}, setups(result))
}
