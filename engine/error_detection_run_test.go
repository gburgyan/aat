package engine

import (
	"context"
	"encoding/json"
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

// ordersAPI answers every request with a 200 or 201 and reports errors in
// the body, in an envelope whose root key names the operation:
// {"orderResponse": {"result": {"errors": [...]}}}. An email of "bad" is a
// VALIDATION error, "busy" a TEMPORARY one the first time it is sent,
// "boom" an UNKNOWN one under a bare root, and anything else makes order o1.
// DELETE /orders/o1 reports an UNKNOWN error.
type ordersAPI struct {
	mu    sync.Mutex
	busy  int
	sends []string
}

func (a *ordersAPI) handler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	envelope := func(root, kind, message string) string {
		errs := `{"result": {"errors": [{"type": "` + kind + `", "message": "` + message + `", "code": "E102"}], "warnings": [{"message": "slow day"}]}}`
		if root == "" {
			return errs
		}
		return `{"` + root + `": ` + errs + `}`
	}
	if r.Method == http.MethodDelete {
		a.sends = append(a.sends, "DELETE "+r.URL.Path)
		_, _ = w.Write([]byte(envelope("cancelResponse", "UNKNOWN", "cancel failed")))
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	a.sends = append(a.sends, body.Email)
	switch body.Email {
	case "bad":
		_, _ = w.Write([]byte(envelope("orderResponse", "VALIDATION", "email is not valid")))
	case "busy":
		if a.busy++; a.busy == 1 {
			_, _ = w.Write([]byte(envelope("orderResponse", "TEMPORARY", "try again")))
			return
		}
		fallthrough
	default:
		if body.Email == "boom" {
			_, _ = w.Write([]byte(envelope("", "UNKNOWN", "system error")))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"orderResponse": {"order": {"id": "o1"}, "result": {"warnings": [{"message": "slow day"}]}}}`))
	}
}

// buildOrdersEngine builds an engine for ordersAPI. statuses, when set, is
// the graph's errorStatus.
func buildOrdersEngine(t *testing.T, rules bool, statuses *graph.ErrorStatus) (*Engine, *ordersAPI) {
	t.Helper()
	api := &ordersAPI{}
	server := httptest.NewServer(http.HandlerFunc(api.handler))
	t.Cleanup(server.Close)

	g := &graph.Graph{Version: "1.0.0", ErrorStatus: statuses, Nodes: map[string]*graph.Node{
		"createOrder": {Name: "createOrder", Adapter: "t.createOrder",
			Inputs:  []graph.Input{{Name: "email", Type: "string", Default: &graph.InputDefault{Value: "ada@example.com"}}},
			Outputs: []graph.Output{{Name: "orderId", Type: "string"}},
			Cleanup: graph.CleanupPairing{Node: "cancelOrder"}},
		"cancelOrder": {Name: "cancelOrder", Adapter: "t.cancelOrder", Inputs: []graph.Input{{Name: "orderId", Type: "string"}}},
	}}
	if rules {
		g.ErrorDetection = envelopeRules(graph.ErrorStatus{})
		for i := range g.ErrorDetection {
			g.ErrorDetection[i].Details.Code = strings.TrimSuffix(g.ErrorDetection[i].Path, "errors") + "errors.0.code"
		}
	}
	registry := adapter.NewRegistry()
	for name, tmpl := range map[string]adapter.Template{
		"t.createOrder": {Request: adapter.TemplateRequest{Method: "POST", Path: "/orders",
			Headers: map[string]string{"Content-Type": "application/json"}, Body: `{"email": "{{email}}"}`},
			Response: adapter.TemplateResponse{Extract: map[string]adapter.ExtractRule{"orderId": {Path: "orderResponse.order.id"}}}},
		"t.cancelOrder": {Request: adapter.TemplateRequest{Method: "DELETE", Path: "/orders/{{orderId}}"}},
	} {
		tmpl.Adapter, tmpl.Protocol = name, "http"
		require.NoError(t, registry.Register(name, adapter.NewTemplateAdapter(tmpl)))
	}
	return NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{})), api
}

var orderStatuses = &graph.ErrorStatus{Status: 500, Categories: map[string]int{"VALIDATION": 400, "TEMPORARY": 503}}

func emailPlan(email string, edit func(*plan.Step)) *plan.Plan {
	step := plan.Step{ID: "order", Node: "createOrder", Values: map[string]plan.StepValue{"email": {Default: email}}}
	if edit != nil {
		edit(&step)
	}
	return &plan.Plan{Metadata: plan.Metadata{GraphVersion: "1.0.0"}, Execution: plan.Execution{Steps: []plan.Step{step}}}
}

// TestRun_BodyErrorBeforeOutputs checks that a 200 whose body reports an
// error fails with the API's message, where its missing outputs used to make
// it an extraction error that hid the message.
func TestRun_BodyErrorBeforeOutputs(t *testing.T) {
	eng, _ := buildOrdersEngine(t, true, orderStatuses)
	result := eng.Run(context.Background(), emailPlan("bad", nil))
	assert.Equal(t, OutcomeFailed, result.Outcome)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "email is not valid [code: E102] [category: VALIDATION], treated as status 400")
	require.Len(t, result.Steps, 1)
	assert.NoError(t, result.Steps[0].Error)
	assert.Nil(t, result.Steps[0].Outputs)
	assert.Empty(t, result.CleanupResults, "a body error made nothing to clean up")

	eng, _ = buildOrdersEngine(t, false, nil)
	result = eng.Run(context.Background(), emailPlan("bad", nil))
	assert.Equal(t, OutcomeError, result.Outcome, "without rules, the body is just the wrong shape")
	assert.ErrorContains(t, result.Error, "extracting outputs")

	eng, _ = buildOrdersEngine(t, true, nil)
	result = eng.Run(context.Background(), emailPlan("ok@example.com", nil))
	require.NoError(t, result.Error, "warnings are not errors")
	assert.Equal(t, "o1", result.Steps[0].Outputs["orderId"])
}

// TestRun_RetryOnBodyError checks that retry.on: [response_error] retries a
// body error whose response lacks the step's outputs, and that one given a
// transient status is retried by default.
func TestRun_RetryOnBodyError(t *testing.T) {
	eng, api := buildOrdersEngine(t, true, nil)
	result := eng.Run(context.Background(), emailPlan("busy", func(s *plan.Step) {
		s.Retry = &plan.RetryConfig{Max: 1, On: []string{"response_error"}}
	}))
	require.NoError(t, result.Error)
	assert.Equal(t, 1, result.Steps[0].RetryCount)
	assert.Equal(t, []string{"busy", "busy"}, api.sends[:2])

	eng, _ = buildOrdersEngine(t, true, orderStatuses)
	result = eng.Run(context.Background(), emailPlan("busy", func(s *plan.Step) { s.Retry = &plan.RetryConfig{Max: 1} }))
	require.NoError(t, result.Error, "TEMPORARY stands for 503, which is retried by default")
	assert.Equal(t, []ErrorCategory{CategoryResponseError}, result.Steps[0].RetriedOn)

	eng, _ = buildOrdersEngine(t, true, orderStatuses)
	result = eng.Run(context.Background(), emailPlan("bad", func(s *plan.Step) { s.Retry = &plan.RetryConfig{Max: 1} }))
	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.Equal(t, 0, result.Steps[0].RetryCount, "VALIDATION stands for 400, which is not")
}

// TestRun_ExpectFailureMatchesABodyError checks that an expectFailure step
// matches an error the body reports by the status it stands for.
func TestRun_ExpectFailureMatchesABodyError(t *testing.T) {
	expect := func(statuses ...any) func(*plan.Step) {
		return func(s *plan.Step) {
			var want plan.ExpectedStatuses
			for _, st := range statuses {
				v, err := plan.ParseExpectedStatus(st)
				require.NoError(t, err)
				want = append(want, v)
			}
			s.ExpectFailure = &plan.ExpectFailure{Status: want}
		}
	}

	eng, _ := buildOrdersEngine(t, true, orderStatuses)
	result := eng.Run(context.Background(), emailPlan("bad", expect("4xx")))
	require.NoError(t, result.Error)
	assert.Equal(t, OutcomePassed, result.Outcome)
	require.NotNil(t, result.Steps[0].ExpectFailure)
	assert.Equal(t, 400, result.Steps[0].ExpectFailure.ActualStatus)

	eng, _ = buildOrdersEngine(t, true, orderStatuses)
	result = eng.Run(context.Background(), emailPlan("bad", expect(409)))
	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.ErrorContains(t, result.Error, "expected failure status 409 but got 400 (a body error in a 200 response)")

	eng, _ = buildOrdersEngine(t, true, nil)
	result = eng.Run(context.Background(), emailPlan("bad", expect("4xx")))
	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.ErrorContains(t, result.Error, "whose body reports an error that its errorDetection rule gives no status")

	// A status assertion that pins the failure holds for the body error too.
	eng, _ = buildOrdersEngine(t, true, orderStatuses)
	result = eng.Run(context.Background(), emailPlan("bad", func(s *plan.Step) {
		expect("4xx")(s)
		s.Assertions = &plan.Assertions{Mechanical: []plan.MechanicalAssertion{{Type: "status", Expect: 400}}}
	}))
	require.NoError(t, result.Error)
	require.NotNil(t, result.Steps[0].Validation)
	assert.True(t, result.Steps[0].Validation.Passed)

	// A mutation's expectStatus is an expectFailure.
	eng, _ = buildOrdersEngine(t, true, orderStatuses)
	result = eng.Run(context.Background(), emailPlan("ok@example.com", func(s *plan.Step) {
		s.Mutations = []plan.Mutation{{Name: "bad-email", Set: map[string]any{"email": "bad"}, ExpectStatus: plan.ExpectedStatuses{{Code: 400}}}}
	}))
	require.NoError(t, result.Error)
	assert.Equal(t, OutcomePassed, result.Outcome)
}

// TestRun_CleanupBodyErrorStatus checks that a cleanup step's body error
// carries its status, and still counts as a failed cleanup.
func TestRun_CleanupBodyErrorStatus(t *testing.T) {
	eng, _ := buildOrdersEngine(t, true, orderStatuses)
	result := eng.Run(context.Background(), emailPlan("ok@example.com", nil))
	require.NoError(t, result.Error)
	require.Len(t, result.CleanupResults, 1)
	rbe := result.CleanupResults[0].ResponseBodyError
	require.NotNil(t, rbe)
	assert.Equal(t, 500, rbe.Status, "UNKNOWN is the graph's 500")
	assert.False(t, cleanupSucceeded(result.CleanupResults[0]))
}

// TestFuzz_BodyErrorStatus checks that a fuzz case is judged by the status
// the error its body reports stands for.
func TestFuzz_BodyErrorStatus(t *testing.T) {
	cases := func(cs ...plan.PinnedFuzzCase) *plan.Plan {
		return emailPlan("ok@example.com", func(s *plan.Step) { s.FuzzSettings = &plan.FuzzSettings{Pinned: cs} })
	}
	email := func(id, mode, value string) plan.PinnedFuzzCase {
		return plan.PinnedFuzzCase{ID: id, Mode: mode, Input: "email", Value: value}
	}

	eng, _ := buildOrdersEngine(t, true, orderStatuses)
	result := eng.Run(context.Background(), cases(
		email("email.refused", plan.FuzzNegative, "bad"),
		email("email.crash", plan.FuzzEdge, "boom"),
		email("email.valid-refused", plan.FuzzPositive, "bad"),
	))
	assert.Equal(t, map[string]string{
		"email.refused":       "",
		"email.crash":         FindingServerError,
		"email.valid-refused": FindingRejectedValid,
	}, findings(result))
	assert.EqualError(t, result.Error, "fuzzing found 1 server-error")
	for _, s := range fuzzSteps(result) {
		assert.Empty(t, s.OutputsError, "the body error explains the missing outputs")
	}

	// accept clears a judgement call on the status the error stands for.
	eng, _ = buildOrdersEngine(t, true, orderStatuses)
	p := cases(email("email.valid-refused", plan.FuzzPositive, "bad"))
	p.Execution.Steps[0].FuzzSettings.Accept = plan.ExpectedStatuses{{Code: 400}}
	result = eng.Run(context.Background(), p)
	assert.Equal(t, map[string]string{"email.valid-refused": ""}, findings(result))

	// Without a status, a body error is a refusal, as before.
	eng, _ = buildOrdersEngine(t, true, nil)
	result = eng.Run(context.Background(), cases(email("email.crash", plan.FuzzEdge, "boom")))
	assert.Equal(t, map[string]string{"email.crash": ""}, findings(result))
}
