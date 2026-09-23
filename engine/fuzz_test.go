package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
