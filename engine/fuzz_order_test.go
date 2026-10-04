package engine

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/plan"
)

// readStep reads the plan's reservation, after t2.
func readStep() plan.Step {
	return plan.Step{ID: "read", Node: "getReservation", Values: map[string]plan.StepValue{"reservationId": {From: "res.id"}}, DependsOn: []string{"t2"}}
}

// verifyReservation reads the plan's reservation once the plan is done.
func verifyReservation() []plan.VerificationStep {
	return []plan.VerificationStep{{Node: "getReservation", Values: map[string]plan.StepValue{"reservationId": {From: "res.id"}}}}
}

// ownSteps returns the IDs of a run's steps up to its first fuzz case or setup
// copy, and checks that nothing else comes after that one.
func ownSteps(t *testing.T, r *RunResult) []string {
	t.Helper()
	var ids []string
	for i, s := range r.Steps {
		if s.Fuzz == nil && s.FuzzSetup == "" {
			ids = append(ids, s.StepID)
			continue
		}
		for _, rest := range r.Steps[i:] {
			assert.True(t, rest.Fuzz != nil || rest.FuzzSetup != "", "%s runs before the cases", rest.StepID)
		}
		break
	}
	return ids
}

func hasStep(r *RunResult, id string) bool {
	for _, s := range r.Steps {
		if s.StepID == id {
			return true
		}
	}
	return false
}

// TestFuzzOrder_CasesRunAfterThePlan checks that cases on copies of the setup
// wait for the plan's own steps and its verification, so they never hold the
// plan up.
func TestFuzzOrder_CasesRunAfterThePlan(t *testing.T) {
	eng, api := buildTravelEngine(t)
	p := travelPlan(name("name.empty", plan.FuzzNegative, ""), name("name.ok", plan.FuzzPositive, "Lin"))
	p.Execution.Steps = append(p.Execution.Steps, readStep())
	p.Execution.Verification = verifyReservation()
	result := eng.Run(context.Background(), p)
	require.NoError(t, result.Error)

	assert.Equal(t, []string{"airports", "res", "t1", "t2", "read", "verify_getReservation"}, ownSteps(t, result))
	require.Greater(t, len(api.log), 6)
	assert.Equal(t, []string{"GET /airports", "POST /reservations", "POST /reservations/r1/travelers", "POST /reservations/r1/travelers",
		"GET /reservations/r1", "GET /reservations/r1"}, api.log[:6], "the plan and its verification, before any case")
	assert.Len(t, fuzzSteps(result), 2)
}

// TestFuzzOrder_LaterFailureStillSendsCases checks that a plan step failing
// after the target ends the plan, not its cases: they are sent, and the run
// fails on the plan's error, without verification.
func TestFuzzOrder_LaterFailureStillSendsCases(t *testing.T) {
	eng, api := buildTravelEngine(t)
	p := travelPlan(name("name.empty", plan.FuzzNegative, ""), name("name.crash", plan.FuzzEdge, "crash"))
	p.Execution.Steps = append(p.Execution.Steps, plan.Step{ID: "t3", Node: "addTraveler", DependsOn: []string{"t2"},
		Values: map[string]plan.StepValue{"reservationId": {From: "res.id"}, "name": {Default: "Cy"}}}) // a third: 409
	p.Execution.Verification = verifyReservation()
	result := eng.Run(context.Background(), p)

	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.EqualError(t, result.Error, `step "t3" (addTraveler) returned status 409`, "the plan's own failure is the run's error")
	require.Len(t, fuzzSteps(result), 2, "the target got through, so its cases were sent")
	assert.Equal(t, FindingServerError, fuzzSteps(result)[1].Fuzz.Finding)
	assert.False(t, hasStep(result, "verify_getReservation"), "verification is skipped once the plan failed")
	assert.Zero(t, api.reads)
}

// TestFuzzOrder_FailedTargetSendsNoCases checks that a target that ended the
// run has its cases left unsent.
func TestFuzzOrder_FailedTargetSendsNoCases(t *testing.T) {
	eng, api := buildTravelEngine(t)
	p := travelPlan(name("name.empty", plan.FuzzNegative, ""))
	p.Execution.Steps[3].Values["name"] = plan.StepValue{Default: "crash"}
	result := eng.Run(context.Background(), p)

	assert.Equal(t, OutcomeFailed, result.Outcome)
	assert.Empty(t, fuzzSteps(result))
	assert.Equal(t, 1, api.creates, "no copy of the setup was made")
}

// TestFuzzOrder_SharedCasesStayAfterTheirTarget checks that cases on the
// target's own state run right after it, before the plan moves on.
func TestFuzzOrder_SharedCasesStayAfterTheirTarget(t *testing.T) {
	eng, _ := buildTravelEngine(t)
	p := travelPlan(name("name.empty", plan.FuzzNegative, ""), name("name.long", plan.FuzzNegative, "abcdefghijklmnopqrstuvwxyz"))
	p.Execution.Steps[3].FuzzSettings.Scope = plan.FuzzScopeShared
	p.Execution.Steps = append(p.Execution.Steps, readStep())
	result := eng.Run(context.Background(), p)
	require.NoError(t, result.Error)

	require.Len(t, result.Steps, 7)
	assert.Equal(t, "t2", result.Steps[3].StepID)
	assert.NotNil(t, result.Steps[4].Fuzz)
	assert.NotNil(t, result.Steps[5].Fuzz)
	assert.Equal(t, "read", result.Steps[6].StepID)
}

// TestFuzzOrder_ErrorOutcomeSurvivesAFailingCase checks that a case's
// finding sent after the plan ended on an error leaves the run's outcome an
// error.
func TestFuzzOrder_ErrorOutcomeSurvivesAFailingCase(t *testing.T) {
	eng, api := buildTravelEngine(t)
	api.dropReads = true
	p := travelPlan(name("name.crash", plan.FuzzEdge, "crash"))
	p.Execution.Steps = append(p.Execution.Steps, readStep())
	result := eng.Run(context.Background(), p)

	assert.Equal(t, OutcomeError, result.Outcome)
	assert.ErrorContains(t, result.Error, `step "read" (getReservation)`)
	require.Len(t, fuzzSteps(result), 1)
	assert.True(t, fuzzSteps(result)[0].Fuzz.Fails)
}

// TestFuzzOrder_StopAfterTheTarget checks that a checkpoint at a target still
// sends the cases that run on copies, and leaves the rest of the plan and its
// cleanup. Shared cases would change the state the checkpoint keeps, so they
// are not sent.
func TestFuzzOrder_StopAfterTheTarget(t *testing.T) {
	eng, api := buildTravelEngine(t)
	p := travelPlan(name("name.empty", plan.FuzzNegative, ""))
	p.Execution.Steps = append(p.Execution.Steps, readStep())
	result := eng.WithStopAfter("t2").Run(context.Background(), p)
	require.NoError(t, result.Error)
	assert.Equal(t, OutcomeStopped, result.Outcome)
	assert.Equal(t, "t2", result.StoppedAt)
	assert.Len(t, fuzzSteps(result), 1)
	assert.Zero(t, api.reads, "the rest of the plan is not sent")
	assert.Empty(t, result.CleanupResults)

	eng, _ = buildTravelEngine(t)
	p = travelPlan(name("name.empty", plan.FuzzNegative, ""))
	p.Execution.Steps[3].FuzzSettings.Scope = plan.FuzzScopeShared
	result = eng.WithStopAfter("t2").Run(context.Background(), p)
	assert.True(t, result.Stopped)
	assert.Empty(t, fuzzSteps(result))
}

// TestFuzzOrder_ProgressFollowsTheOrder checks that progress numbers the
// steps in the order they run: the plan, its verification, then the cases,
// each copy taking its case's number.
func TestFuzzOrder_ProgressFollowsTheOrder(t *testing.T) {
	eng, _ := buildTravelEngine(t)
	obs := &recordingObserver{}
	eng.WithProgress(obs)
	p := travelPlan(name("name.empty", plan.FuzzNegative, ""), name("name.ok", plan.FuzzPositive, "Lin"))
	p.Execution.Verification = verifyReservation()
	result := eng.Run(context.Background(), p)
	require.NoError(t, result.Error)

	require.Equal(t, "run_start", obs.events[0].kind)
	assert.Equal(t, 7, obs.events[0].total, "four plan steps, one verification step, and two cases")
	var numbers []int
	k := 0
	for _, ev := range obs.events {
		if ev.kind != "step_complete" {
			continue
		}
		r := result.Steps[k]
		k++
		if r.FuzzSetup != "" {
			assert.Contains(t, []int{5, 6}, ev.index, "a copy takes its case's number")
			continue
		}
		if r.StepID == "verify_getReservation" {
			assert.Equal(t, 4, ev.index)
		}
		numbers = append(numbers, ev.index)
	}
	assert.Equal(t, []int{0, 1, 2, 3, 4, 5, 6}, numbers)
}

// TestFuzzOrder_AbortDuringCases checks that an interrupt while the cases run
// ends the run as aborted, after the plan and its verification.
func TestFuzzOrder_AbortDuringCases(t *testing.T) {
	eng, api := buildTravelEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api.onRequest = func(r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/reservations" && api.creates == 1 {
			cancel() // the first copy of the reservation
		}
	}
	p := travelPlan(name("name.empty", plan.FuzzNegative, ""), name("name.empty2", plan.FuzzNegative, ""))
	p.Execution.Verification = verifyReservation()
	result := eng.Run(ctx, p)

	assert.Equal(t, OutcomeAborted, result.Outcome)
	assert.True(t, hasStep(result, "verify_getReservation"), "verification ran before the cases")
	assert.Empty(t, fuzzSteps(result), "no case was judged")
}
