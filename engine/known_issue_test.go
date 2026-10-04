package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/gburgyan/aat/validate"
)

// clockAt pins the clock so an expiry can be tested either side of its date
// without the test depending on when it runs.
func clockAt(day string) func() time.Time {
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return d }
}

func mustDate(t *testing.T, s string) plan.Date {
	t.Helper()
	d, err := plan.ParseDate(s)
	require.NoError(t, err)
	return d
}

// twoStepEngine builds a graph of two independent nodes, each answering 200
// with the body given, so a plan can fail one step and still have another to
// reach.
func twoStepEngine(t *testing.T, now func() time.Time) *Engine {
	t.Helper()
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"first": {
			Name: "first", Adapter: "ki.first",
			Outputs: []graph.Output{{Name: "status", Type: "string"}},
		},
		"second": {
			Name: "second", Adapter: "ki.second",
			Outputs: []graph.Output{{Name: "status", Type: "string"}},
		},
	}}
	srv := newChainServer(t, nil)
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("ki.first", &stubAdapter{method: "POST", path: "/first", response: map[string]any{"status": "ERROR"}}))
	require.NoError(t, registry.Register("ki.second", &stubAdapter{method: "POST", path: "/second", response: map[string]any{"status": "ok"}}))
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(srv.URL), &adapter.EnvironmentConfig{}))
	eng.Now = now
	return eng
}

// failingAssertion is false against the body the "first" node returns.
func failingAssertion() *plan.Assertions {
	return &plan.Assertions{Mechanical: []plan.MechanicalAssertion{
		{Type: "fieldEquals", Path: "status", Value: "PENDING"},
	}}
}

func TestKnownIssue_UnexpiredSuppressesAndRunContinues(t *testing.T) {
	eng := twoStepEngine(t, clockAt("2026-09-22"))
	result := eng.Run(context.Background(), chainPlan(
		plan.Step{
			Node:       "first",
			Assertions: failingAssertion(),
			KnownIssue: &plan.KnownIssue{Until: mustDate(t, "2026-10-06"), Reason: "vendor defect"},
		},
		plan.Step{Node: "second", DependsOn: []string{"first"}},
	))

	assert.Equal(t, OutcomePassed, result.Outcome, "a covered failure must not turn the run red")
	assert.NoError(t, result.Error)

	// The step itself still reads as failed: the run is forgiving, the record is not.
	require.Len(t, result.Steps, 2, "the run must carry on to the second step")
	require.NotNil(t, result.Steps[0].Validation)
	assert.False(t, result.Steps[0].Validation.Passed, "the assertion still failed")
	require.NotNil(t, result.Steps[0].KnownIssue)
	assert.True(t, result.Steps[0].KnownIssue.Applied)
	assert.False(t, result.Steps[0].KnownIssue.Expired)

	require.Len(t, result.KnownIssues, 1)
	assert.Equal(t, "first", result.KnownIssues[0].StepID)
	assert.Equal(t, "vendor defect", result.KnownIssues[0].Reason)
	assert.False(t, result.EndedEarly)
}

func TestKnownIssue_ExpiredFailsAndNamesTheDate(t *testing.T) {
	eng := twoStepEngine(t, clockAt("2026-10-07")) // the day after
	result := eng.Run(context.Background(), chainPlan(
		plan.Step{
			Node:       "first",
			Assertions: failingAssertion(),
			KnownIssue: &plan.KnownIssue{Until: mustDate(t, "2026-10-06"), Reason: "vendor defect"},
		},
		plan.Step{Node: "second", DependsOn: []string{"first"}},
	))

	assert.Equal(t, OutcomeFailed, result.Outcome, "a lapsed entry stops covering the failure")
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "knownIssue expired 2026-10-06",
		"the error must say why the build turned, not just that it did")

	require.NotEmpty(t, result.Steps)
	require.NotNil(t, result.Steps[0].KnownIssue)
	assert.True(t, result.Steps[0].KnownIssue.Expired)
	assert.False(t, result.Steps[0].KnownIssue.Applied)
	assert.Empty(t, result.KnownIssues)
}

// The date itself is included: an entry marked until the sixth is still in
// force all through the sixth.
func TestKnownIssue_ExpiryIncludesItsOwnDay(t *testing.T) {
	eng := twoStepEngine(t, clockAt("2026-10-06"))
	result := eng.Run(context.Background(), chainPlan(plan.Step{
		Node:       "first",
		Assertions: failingAssertion(),
		KnownIssue: &plan.KnownIssue{Until: mustDate(t, "2026-10-06"), Reason: "vendor defect"},
	}))
	assert.Equal(t, OutcomePassed, result.Outcome)
}

// A step that passes while an entry is in force has outlived it, and says so
// without ever breaking the build.
func TestKnownIssue_ResolvedIsReportedNotFailed(t *testing.T) {
	eng := twoStepEngine(t, clockAt("2026-09-22"))
	result := eng.Run(context.Background(), chainPlan(plan.Step{
		Node: "first",
		Assertions: &plan.Assertions{Mechanical: []plan.MechanicalAssertion{
			{Type: "fieldEquals", Path: "status", Value: "ERROR"}, // true
		}},
		KnownIssue: &plan.KnownIssue{Until: mustDate(t, "2026-10-06"), Reason: "vendor defect"},
	}))

	assert.Equal(t, OutcomePassed, result.Outcome)
	assert.Empty(t, result.KnownIssues, "nothing was suppressed")
	require.Len(t, result.KnownIssuesResolved, 1)
	assert.Equal(t, "first", result.KnownIssuesResolved[0].StepID)
	require.NotNil(t, result.Steps[0].KnownIssue)
	assert.True(t, result.Steps[0].KnownIssue.Resolved)
}

// A plan-level entry covers a step that declares none.
func TestKnownIssue_PlanLevelAppliesToEveryStep(t *testing.T) {
	eng := twoStepEngine(t, clockAt("2026-09-22"))
	p := chainPlan(plan.Step{Node: "first", Assertions: failingAssertion()})
	p.KnownIssue = &plan.KnownIssue{Until: mustDate(t, "2026-10-06"), Reason: "whole scenario blocked"}

	result := eng.Run(context.Background(), p)
	assert.Equal(t, OutcomePassed, result.Outcome)
	require.Len(t, result.KnownIssues, 1)
	assert.Equal(t, "whole scenario blocked", result.KnownIssues[0].Reason)
}

// A step's own entry wins over the plan's.
func TestKnownIssue_StepEntryOverridesPlanEntry(t *testing.T) {
	p := chainPlan(plan.Step{
		Node:       "first",
		KnownIssue: &plan.KnownIssue{Until: mustDate(t, "2026-10-06"), Reason: "the step's own"},
	})
	p.KnownIssue = &plan.KnownIssue{Until: mustDate(t, "2026-12-31"), Reason: "the plan's"}

	got := p.KnownIssueFor(p.Execution.Steps[0])
	require.NotNil(t, got)
	assert.Equal(t, "the step's own", got.Reason)
}

// A step with no entry is unaffected: an ordinary failure still fails.
func TestKnownIssue_AbsentLeavesFailureAlone(t *testing.T) {
	eng := twoStepEngine(t, clockAt("2026-09-22"))
	result := eng.Run(context.Background(), chainPlan(plan.Step{
		Node: "first", Assertions: failingAssertion(),
	}))
	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.Nil(t, result.Steps[0].KnownIssue)
}

// The trap this feature most easily falls into. Cleanup gated on runOn:
// failure must still fire when the failure was covered by a knownIssue: the
// resources are in the state the real failure left them, whatever the run
// chose to report. A naive implementation rewrites the outcome to passed and
// silently stops running recovery cleanup.
func TestKnownIssue_CleanupRunOnFailureStillFires(t *testing.T) {
	for _, tt := range []struct {
		name        string
		runOn       string
		wantCleanup bool
	}{
		{"failure cleanup still runs", "failure", true},
		{"success cleanup still does not", "success", false},
		{"always runs either way", "always", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			eng := twoStepEngine(t, clockAt("2026-09-22"))
			p := chainPlan(plan.Step{
				Node:       "first",
				Assertions: failingAssertion(),
				KnownIssue: &plan.KnownIssue{Until: mustDate(t, "2026-10-06"), Reason: "vendor defect"},
			})
			p.Execution.Cleanup = []plan.CleanupStep{{Node: "second", RunOn: tt.runOn}}

			result := eng.Run(context.Background(), p)

			require.Equal(t, OutcomePassed, result.Outcome, "the run still reports passed")
			if tt.wantCleanup {
				assert.NotEmpty(t, result.CleanupResults,
					"runOn %q must see the real failure, not the reported outcome", tt.runOn)
			} else {
				assert.Empty(t, result.CleanupResults,
					"runOn %q must not be fooled into running by the reported outcome", tt.runOn)
			}
		})
	}
}

// A failure with no outputs to carry on from ends the run, but still does not
// turn it red. The run says it ended early so a short run is not mistaken for
// a complete one.
func TestKnownIssue_StatusFailureEndsRunButStaysGreen(t *testing.T) {
	g := &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"boom":   {Name: "boom", Adapter: "ki.boom"},
		"second": {Name: "second", Adapter: "ki.second", Outputs: []graph.Output{{Name: "status", Type: "string"}}},
	}}
	srv := newChainServer(t, map[string]chainResponse{
		"/boom": {status: 500, body: `{"error":"nope"}`},
	})
	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("ki.boom", &stubAdapter{method: "POST", path: "/boom", response: map[string]any{}}))
	require.NoError(t, registry.Register("ki.second", &stubAdapter{method: "POST", path: "/second", response: map[string]any{"status": "ok"}}))
	eng := NewEngine(g, registry, NewExecutorRouter(adapter.NewHTTPExecutor(srv.URL), &adapter.EnvironmentConfig{}))
	eng.Now = clockAt("2026-09-22")

	result := eng.Run(context.Background(), chainPlan(
		plan.Step{
			Node:       "boom",
			KnownIssue: &plan.KnownIssue{Until: mustDate(t, "2026-10-06"), Reason: "500s while the vendor rolls back"},
		},
		plan.Step{Node: "second", DependsOn: []string{"boom"}},
	))

	assert.Equal(t, OutcomePassed, result.Outcome)
	assert.True(t, result.EndedEarly, "a short run must say it is short")
	assert.Len(t, result.Steps, 1, "there were no outputs to continue from")
	require.Len(t, result.KnownIssues, 1)
}

// TestCoverSetupFailure_ExpectedBodyError checks that a setup copy whose
// expected failure came back as an error in a 200's body can be covered when
// only an assertion failed.
func TestCoverSetupFailure_ExpectedBodyError(t *testing.T) {
	eng := &Engine{Now: clockAt("2026-10-01")}
	p := &plan.Plan{}
	step := plan.Step{ID: "neg", ExpectFailure: &plan.ExpectFailure{Status: plan.ExpectedStatuses{{Class: 4}}},
		KnownIssue: &plan.KnownIssue{Until: mustDate(t, "2026-10-06"), Reason: "assertion is off"}}
	r := &StepResult{StatusCode: 200, ResponseBodyError: &ResponseBodyError{Status: 400},
		ExpectFailure: &ExpectFailureResult{ActualStatus: 400, Passed: true}, Validation: &validate.MechanicalResult{Passed: false}}
	assert.True(t, eng.coverSetupFailure(p, step, r))

	step.ExpectFailure = nil
	r = &StepResult{StatusCode: 200, ResponseBodyError: &ResponseBodyError{Status: 400}}
	assert.False(t, eng.coverSetupFailure(p, step, r), "a body error on a step meant to succeed is not covered")
}
