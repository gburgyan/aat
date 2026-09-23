package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/graph/oas"
	"github.com/gburgyan/aat/plan"
)

// buggyQuantityServer refuses a quantity that is not a whole number or is
// below 0, fails on 0 (the bug), and accepts anything else, 100 included,
// though the graph caps it at 99.
func buggyQuantityServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Quantity json.Number `json:"quantity"`
		}
		w.Header().Set("Content-Type", "application/json")
		dec := json.NewDecoder(r.Body)
		dec.UseNumber()
		if err := dec.Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad json"}`))
			return
		}
		q, err := body.Quantity.Int64()
		switch {
		case err != nil || q < 0:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad quantity"}`))
		case q == 0:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"division by zero"}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func buildQuantityEngine(t *testing.T, serverURL string) *Engine {
	t.Helper()
	min, max := 1.0, 99.0
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"addItem": {Name: "addItem", Adapter: "test.addItem", Inputs: []graph.Input{
			{Name: "quantity", Type: "integer", Default: &graph.InputDefault{Value: 2}, Constraints: &graph.Constraint{Min: &min, Max: &max}},
		}},
	}}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.addItem", adapter.NewTemplateAdapter(adapter.Template{
		Adapter:  "test.addItem",
		Protocol: "http",
		Request: adapter.TemplateRequest{
			Method:  "POST",
			Path:    "/items",
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    `{"quantity": {{quantity}}}`,
		},
	})))
	return NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(serverURL), &adapter.EnvironmentConfig{}))
}

func quantityPlan() *plan.Plan {
	return &plan.Plan{
		Metadata:  plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{Steps: []plan.Step{{ID: "add", Node: "addItem"}}},
	}
}

func findings(result *RunResult) map[string]string {
	out := map[string]string{}
	for _, s := range result.Steps {
		if s.Fuzz != nil {
			out[s.Fuzz.Case.ID] = s.Fuzz.Finding
		}
	}
	return out
}

func TestFuzz_FindsTheBugAndCarriesOn(t *testing.T) {
	server := buggyQuantityServer(t)
	result := buildQuantityEngine(t, server.URL).WithFuzz(&FuzzConfig{Targets: []string{"add"}}).
		Run(context.Background(), quantityPlan())

	assert.Equal(t, OutcomeFailed, result.Outcome, "a server error fails the run")
	assert.EqualError(t, result.Error, "fuzzing found 1 server-error")
	assert.Equal(t, "add", result.Steps[0].StepID, "the happy path runs first")
	assert.Nil(t, result.Steps[0].Fuzz)

	got := findings(result)
	assert.Equal(t, FindingServerError, got["quantity.below-min"])
	assert.Equal(t, FindingAcceptedInvalid, got["quantity.above-max"])
	assert.Equal(t, "", got["quantity.at-max"])
	assert.Equal(t, "", got["quantity.wrong-type"], "refused, as a negative case should be")
	assert.Equal(t, "", got["quantity.overflow"])

	for _, s := range result.Steps[1:] {
		require.NotNil(t, s.Fuzz, s.StepID)
		assert.Equal(t, s.Fuzz.Finding == FindingServerError, s.Fuzz.Fails, s.StepID)
		assert.Equal(t, plan.FuzzStepID("add", s.Fuzz.Case.ID), s.StepID)
	}

	// The raw value is sent as generated, not coerced: a JSON string where the
	// number belongs.
	for _, s := range result.Steps {
		if s.Fuzz != nil && s.Fuzz.Case.ID == "quantity.wrong-type" {
			assert.Equal(t, `"not-a-number"`, s.Inputs["quantity"])
			assert.JSONEq(t, `{"quantity": "not-a-number"}`, string(s.Request.Body))
		}
	}
}

func TestFuzz_FailSetAndCaseFilter(t *testing.T) {
	server := buggyQuantityServer(t)

	result := buildQuantityEngine(t, server.URL).WithFuzz(&FuzzConfig{
		Targets: []string{"addItem"}, Cases: []string{"quantity.above-max"}, Fail: []string{FindingAcceptedInvalid},
	}).Run(context.Background(), quantityPlan())
	assert.EqualError(t, result.Error, "fuzzing found 1 accepted-invalid")
	require.Len(t, result.Steps, 2, "the happy path and the one case")
	assert.True(t, result.Steps[1].Fuzz.Fails)
	assert.Equal(t, OutcomeFailed, result.Outcome)

	result = buildQuantityEngine(t, server.URL).WithFuzz(&FuzzConfig{
		Targets: []string{"add"}, Cases: []string{"quantity.above-max"},
	}).Run(context.Background(), quantityPlan())
	assert.Equal(t, OutcomePassed, result.Outcome, "accepted-invalid is a warning by default")
}

func TestFuzz_UnknownTarget(t *testing.T) {
	server := buggyQuantityServer(t)
	result := buildQuantityEngine(t, server.URL).WithFuzz(&FuzzConfig{Targets: []string{"checkout"}}).
		Run(context.Background(), quantityPlan())
	assert.Equal(t, OutcomeError, result.Outcome)
	assert.ErrorContains(t, result.Error, "--fuzz checkout")

	result = buildQuantityEngine(t, server.URL).WithFuzz(&FuzzConfig{Targets: []string{"checkout"}, AllowNoTarget: true}).
		Run(context.Background(), quantityPlan())
	assert.Equal(t, OutcomePassed, result.Outcome)
	assert.Len(t, result.Steps, 1)
}

func TestParseFindings(t *testing.T) {
	got, err := ParseFindings([]string{"server-error", " accepted-invalid", ""})
	require.NoError(t, err)
	assert.Equal(t, []string{"server-error", "accepted-invalid"}, got)
	_, err = ParseFindings([]string{"crash"})
	assert.ErrorContains(t, err, `unknown finding "crash"`)
}

const fuzzTestSpec = `openapi: "3.0.3"
info: {title: fuzz, version: "1"}
paths:
  /items:
    post:
      operationId: addItem
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                quantity: {type: integer, minimum: 0, maximum: 50}
      responses:
        "200":
          description: Added
          content:
            application/json:
              schema:
                type: object
                required: [ok]
                properties:
                  ok: {type: boolean}
        default:
          description: Refused
          content:
            application/json:
              schema:
                type: object
                properties:
                  error: {type: string}
`

// TestFuzz_SpecDecidesWhatIsInvalid checks that a case whose request breaks
// the OpenAPI spec is judged as negative, whatever the graph's constraints
// made it, and that the violations are kept on the case, not as OAS warnings.
func TestFuzz_SpecDecidesWhatIsInvalid(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(fuzzTestSpec), 0o644))
	cache := oas.NewSpecCache()
	require.NoError(t, cache.Load("spec.yaml", specPath))

	server := buggyQuantityServer(t)
	eng := buildQuantityEngine(t, server.URL).WithOASSpecs(cache, "", false)
	eng.graph.Nodes["addItem"].OAS = &graph.OASRef{OperationID: "addItem", Spec: "spec.yaml"}

	result := eng.WithFuzz(&FuzzConfig{Targets: []string{"add"}, Cases: []string{"quantity.at-max", "quantity.at-min"}}).
		Run(context.Background(), quantityPlan())
	require.Len(t, result.Steps, 3)

	byID := map[string]StepResult{}
	for _, st := range result.Steps[1:] {
		byID[st.Fuzz.Case.ID] = st
	}

	atMax := byID["quantity.at-max"].Fuzz // 99: within the graph's 1..99, above the spec's 50
	require.NotNil(t, atMax)
	assert.Equal(t, plan.FuzzPositive, atMax.Case.Mode)
	assert.Equal(t, plan.FuzzNegative, atMax.JudgedAs)
	assert.Equal(t, FindingAcceptedInvalid, atMax.Finding, "the server took what its spec refuses")
	require.NotEmpty(t, atMax.SpecViolations)
	assert.Contains(t, atMax.SpecViolations[0], "quantity")
	assert.False(t, byID["quantity.at-max"].OASValidation.HasErrors(), "the request's violations are not OAS warnings")

	atMin := byID["quantity.at-min"].Fuzz
	assert.Equal(t, plan.FuzzPositive, atMin.JudgedAs)
	assert.Empty(t, atMin.Finding)
}

func TestFuzz_CapIsSeeded(t *testing.T) {
	server := buggyQuantityServer(t)
	run := func(seed uint64) *RunResult {
		return buildQuantityEngine(t, server.URL).WithSeed(seed).WithFuzz(&FuzzConfig{Targets: []string{"add"}, Max: 2}).
			Run(context.Background(), quantityPlan())
	}
	ids := func(r *RunResult) []string {
		var out []string
		for _, s := range r.Steps {
			if s.Fuzz != nil {
				out = append(out, s.Fuzz.Case.ID)
			}
		}
		return out
	}

	first := run(11)
	assert.True(t, first.FuzzCapped)
	assert.True(t, first.DrewRandomly(), "a capped run reports its seed")
	assert.Len(t, ids(first), 2)
	assert.Equal(t, ids(first), ids(run(11)), "the seed picks the same cases")

	uncapped := buildQuantityEngine(t, server.URL).WithFuzz(&FuzzConfig{Targets: []string{"add"}}).
		Run(context.Background(), quantityPlan())
	assert.False(t, uncapped.FuzzCapped)
}

// buildChannelEngine sends {"channel": "web", "quantity": N} and its server
// fails when channel is missing (the bug), refuses a quantity that isn't a
// number, and accepts anything else.
func buildChannelEngine(t *testing.T) *Engine {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad json"}`))
			return
		}
		if _, ok := body["channel"]; !ok {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"nil channel"}`))
			return
		}
		if _, ok := body["quantity"].(float64); !ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"quantity"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"addItem": {Name: "addItem", Adapter: "test.addItem", Inputs: []graph.Input{
			{Name: "quantity", Type: "integer", Default: &graph.InputDefault{Value: 2}},
		}},
	}}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.addItem", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "test.addItem", Protocol: "http",
		Request: adapter.TemplateRequest{
			Method: "POST", Path: "/items", Headers: map[string]string{"Content-Type": "application/json"},
			Body: `{"channel": "web", "quantity": {{quantity}}}`,
		},
	})))
	return NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{}))
}

func TestFuzz_TemplateFieldsAndMissingInputs(t *testing.T) {
	result := buildChannelEngine(t).WithFuzz(&FuzzConfig{Targets: []string{"add"}}).Run(context.Background(), quantityPlan())
	assert.EqualError(t, result.Error, "fuzzing found 1 server-error")

	byID := map[string]StepResult{}
	for _, s := range result.Steps {
		if s.Fuzz != nil {
			byID[s.Fuzz.Case.ID] = s
		}
	}
	remove := byID["body.channel.remove"]
	require.NotNil(t, remove.Fuzz, "the template's own field is fuzzed")
	assert.Equal(t, FindingServerError, remove.Fuzz.Finding)
	assert.JSONEq(t, `{"quantity": 2}`, string(remove.Request.Body))

	missing := byID["quantity.missing"]
	require.NotNil(t, missing.Fuzz)
	assert.Equal(t, plan.FuzzNegative, missing.Fuzz.Case.Mode, "quantity is required")
	assert.Empty(t, missing.Fuzz.Finding, "refused with a 400")
	assert.JSONEq(t, `{"channel": "web"}`, string(missing.Request.Body))

	assert.JSONEq(t, `{"channel": "web", "quantity": null}`, string(byID["quantity.null"].Request.Body))
	assert.JSONEq(t, `{"channel": 12345, "quantity": 2}`, string(byID["body.channel.wrong-type"].Request.Body))
	assert.JSONEq(t, `{"aatFuzzExtra": "x", "channel": "web", "quantity": 2}`, string(byID["body.extra-property"].Request.Body))
	assert.Empty(t, byID["body.extra-property"].Fuzz.Finding)
}

const undocumentedSpec = `openapi: "3.0.3"
info: {title: fuzz, version: "1"}
paths:
  /items:
    post:
      operationId: addItem
      requestBody:
        content:
          application/json:
            schema: {type: object}
      responses:
        "200":
          description: Added
          content:
            application/json:
              schema: {type: object}
`

func TestFuzz_UndocumentedStatus(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(undocumentedSpec), 0o644))
	cache := oas.NewSpecCache()
	require.NoError(t, cache.Load("spec.yaml", specPath))

	eng := buildChannelEngine(t).WithOASSpecs(cache, "", false)
	eng.graph.Nodes["addItem"].OAS = &graph.OASRef{OperationID: "addItem", Spec: "spec.yaml"}
	result := eng.WithFuzz(&FuzzConfig{Targets: []string{"add"}, Cases: []string{"quantity.missing", "body.extra-property"}}).
		Run(context.Background(), quantityPlan())

	byID := map[string]*FuzzResult{}
	for _, s := range result.Steps {
		if s.Fuzz != nil {
			byID[s.Fuzz.Case.ID] = s.Fuzz
		}
	}
	require.NotNil(t, byID["quantity.missing"])
	assert.Equal(t, FindingUndocumentedStatus, byID["quantity.missing"].Finding, "the spec lists no 400")
	assert.False(t, byID["quantity.missing"].Fails, "a warning by default")
	assert.Empty(t, byID["body.extra-property"].Finding, "200 is documented")
	assert.Equal(t, OutcomePassed, result.Outcome, "error: %v", result.Error)
}

// TestFuzz_FailedSetupSkipsOnlyItsCase checks that when a case's copy of a
// setup step fails, that case is not sent and the run carries on.
func TestFuzz_FailedSetupSkipsOnlyItsCase(t *testing.T) {
	var carts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/carts" {
			// The second cart fails: the first case's setup.
			if carts.Add(1) == 2 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{}`))
				return
			}
			_, _ = w.Write([]byte(`{"cartId":"c1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	min := 1.0
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"createCart": {Name: "createCart", Adapter: "test.createCart", Outputs: []graph.Output{{Name: "cartId", Type: "string"}}},
		"addItem": {Name: "addItem", Adapter: "test.addItem", Inputs: []graph.Input{
			{Name: "cartId", Type: "string", Default: &graph.InputDefault{From: "createCart.cartId"}},
			{Name: "quantity", Type: "integer", Default: &graph.InputDefault{Value: 2}, Constraints: &graph.Constraint{Min: &min}},
		}},
	}}
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.createCart", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "test.createCart", Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "POST", Path: "/carts"},
		Response: adapter.TemplateResponse{Extract: map[string]adapter.ExtractRule{"cartId": {Path: "cartId"}}},
	})))
	require.NoError(t, registry.Register("test.addItem", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "test.addItem", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "POST", Path: "/carts/{{cartId}}/items", Body: `{"quantity": {{quantity}}}`},
	})))
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(server.URL), &adapter.EnvironmentConfig{}))
	p := &plan.Plan{Metadata: plan.Metadata{GraphVersion: "1.0.0"}, Execution: plan.Execution{Steps: []plan.Step{
		{ID: "cart", Node: "createCart"},
		{ID: "add", Node: "addItem"},
	}}}

	result := eng.WithFuzz(&FuzzConfig{Targets: []string{"add"}, Cases: []string{"quantity.at-min", "quantity.below-min"}}).
		Run(context.Background(), p)
	require.NoError(t, result.Error)
	assert.Equal(t, OutcomePassed, result.Outcome, "a failed setup copy does not fail the run")

	byID := map[string]*FuzzResult{}
	for _, s := range result.Steps {
		if s.Fuzz != nil {
			byID[s.Fuzz.Case.ID] = s.Fuzz
		}
	}
	assert.Equal(t, FindingNotSent, byID["quantity.at-min"].Finding, "its cart failed")
	assert.Equal(t, FindingAcceptedInvalid, byID["quantity.below-min"].Finding, "the other case ran: this API accepts anything")
}

func TestFuzz_NotSentErrorIsNotSent(t *testing.T) {
	eng := &Engine{fuzz: &FuzzConfig{}}
	step := plan.Step{Fuzz: &plan.FuzzCase{ID: "x.y", Mode: plan.FuzzEdge}}
	r := &StepResult{Request: &adapter.Request{}, Error: fmt.Errorf("executing: %w", &adapter.NotSentError{Err: errors.New("unknown field")})}
	assert.Equal(t, FindingNotSent, eng.judgeFuzz(step, nil, r).Finding)
	r = &StepResult{Request: &adapter.Request{}, Error: errors.New("connection refused")}
	assert.Equal(t, FindingNoResponse, eng.judgeFuzz(step, nil, r).Finding)
}
