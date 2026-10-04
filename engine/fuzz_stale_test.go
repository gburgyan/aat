package engine

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// staleRule marks an error code in the travel API's body as one that means
// the reservation is used up.
func staleRule(code string) graph.ErrorDetectionRule {
	return graph.ErrorDetectionRule{Path: "error.code", Rule: "equals", Value: code, Stale: true,
		Details: &graph.ErrorDetailMapping{Code: "error.code"}}
}

// TestFuzzStale_ExpiredSetupIsRebuilt checks that a case whose reused
// reservation expired under it is sent again on a fresh one, and judged on
// that answer.
func TestFuzzStale_ExpiredSetupIsRebuilt(t *testing.T) {
	eng, api := buildTravelEngine(t)
	api.expireAfter = 3 // the copy's own traveler, then two cases
	result := eng.Run(context.Background(), travelPlan(
		name("name.empty", plan.FuzzNegative, ""), name("name.empty2", plan.FuzzNegative, ""), name("name.empty3", plan.FuzzNegative, "")))
	require.NoError(t, result.Error)

	assert.Equal(t, []string{SetupFresh, SetupReused, SetupRebuilt}, setups(result))
	require.Len(t, fuzzSteps(result), 3, "the stale answer is not a step of its own")
	last := fuzzSteps(result)[2]
	assert.Equal(t, http.StatusBadRequest, last.StatusCode)
	assert.Empty(t, last.Fuzz.Finding)
	require.NotNil(t, last.Fuzz.Stale)
	assert.Equal(t, http.StatusGone, last.Fuzz.Stale.Status)
	assert.Nil(t, last.Fuzz.Stale.BodyError)
	assert.Equal(t, 3, api.creates, "the happy path's, the first copy's, and the rebuilt one")
	assert.Equal(t, 2, result.FuzzCopiesSkipped, "only the second case's copies were reused in the end")
	for _, s := range fuzzSteps(result)[:2] {
		assert.Nil(t, s.Fuzz.Stale)
	}
}

// TestFuzzStale_StaleBodyErrorIsRebuilt checks that an error a 200's body
// reports, which a rule marks stale, rebuilds the setup as a 410 does.
func TestFuzzStale_StaleBodyErrorIsRebuilt(t *testing.T) {
	eng, api := buildTravelEngine(t)
	api.expireAfter, api.expiredBody = 3, true
	eng.graph.ErrorDetection = []graph.ErrorDetectionRule{staleRule("EXPIRED")}
	result := eng.Run(context.Background(), travelPlan(
		name("name.empty", plan.FuzzNegative, ""), name("name.empty2", plan.FuzzNegative, ""), name("name.empty3", plan.FuzzNegative, "")))
	require.NoError(t, result.Error)

	assert.Equal(t, []string{SetupFresh, SetupReused, SetupRebuilt}, setups(result))
	st := fuzzSteps(result)[2].Fuzz.Stale
	require.NotNil(t, st)
	assert.Equal(t, http.StatusOK, st.Status)
	require.NotNil(t, st.BodyError)
	assert.True(t, st.BodyError.Stale)
	assert.Equal(t, "EXPIRED", st.BodyError.Code)
}

// TestFuzzStale_LeakyRefusal checks that a refused case that changed the
// reservation anyway doesn't get the next case misjudged once a rule says
// what that looks like.
func TestFuzzStale_LeakyRefusal(t *testing.T) {
	cases := []plan.PinnedFuzzCase{name("name.empty", plan.FuzzNegative, ""), name("name.ok", plan.FuzzPositive, "Lin")}

	// Without the rule, the second case is judged on a full reservation.
	eng, api := buildTravelEngine(t)
	api.leaky = true
	eng.graph.ErrorDetection = []graph.ErrorDetectionRule{{Path: "error.code", Rule: "equals", Value: "FULL"}}
	result := eng.Run(context.Background(), travelPlan(cases...))
	assert.Equal(t, []string{SetupFresh, SetupReused}, setups(result))
	assert.Equal(t, FindingRejectedValid, fuzzSteps(result)[1].Fuzz.Finding)

	// With it, the second case is sent again on a fresh reservation.
	eng, api = buildTravelEngine(t)
	api.leaky = true
	eng.graph.ErrorDetection = []graph.ErrorDetectionRule{staleRule("FULL")}
	result = eng.Run(context.Background(), travelPlan(cases...))
	require.NoError(t, result.Error)
	assert.Equal(t, []string{SetupFresh, SetupRebuilt}, setups(result))
	ok := fuzzSteps(result)[1]
	assert.Equal(t, http.StatusCreated, ok.StatusCode)
	assert.Empty(t, ok.Fuzz.Finding)
	require.NotNil(t, ok.Fuzz.Stale)
	assert.Equal(t, "FULL", ok.Fuzz.Stale.BodyError.Code)
}

// TestFuzzStale_SentOnceMore checks that a case is rebuilt once: an answer
// that is stale on the rebuilt setup too is judged.
func TestFuzzStale_SentOnceMore(t *testing.T) {
	eng, api := buildTravelEngine(t)
	api.goneName = "Gone"
	result := eng.Run(context.Background(), travelPlan(name("name.empty", plan.FuzzNegative, ""), name("name.gone", plan.FuzzPositive, "Gone")))
	require.NoError(t, result.Error)

	assert.Equal(t, []string{SetupFresh, SetupRebuilt}, setups(result))
	assert.Equal(t, 2, api.gone)
	gone := fuzzSteps(result)[1]
	assert.Equal(t, http.StatusGone, gone.StatusCode)
	assert.Equal(t, FindingRejectedValid, gone.Fuzz.Finding)
	require.NotNil(t, gone.Fuzz.Stale)
}

// TestFuzzStale_FreshSetupIsJudged checks that a stale answer on a fresh
// setup is judged as it is: the case's value may be why. It still leaves the
// setup unfit for the next case.
func TestFuzzStale_FreshSetupIsJudged(t *testing.T) {
	eng, api := buildTravelEngine(t)
	api.goneName = "Gone"
	result := eng.Run(context.Background(), travelPlan(name("name.gone", plan.FuzzPositive, "Gone"), name("name.empty", plan.FuzzNegative, "")))
	require.NoError(t, result.Error)
	assert.Equal(t, []string{SetupFresh, SetupFresh}, setups(result), "a used-up setup is not reused")
	assert.Equal(t, 1, api.gone)
	assert.Nil(t, fuzzSteps(result)[0].Fuzz.Stale)

	eng, api = buildTravelEngine(t)
	api.goneName = "Gone"
	p := travelPlan(name("name.empty", plan.FuzzNegative, ""), name("name.gone", plan.FuzzPositive, "Gone"))
	p.Execution.Steps[3].FuzzSettings.Scope = plan.FuzzScopeIsolated
	result = eng.Run(context.Background(), p)
	require.NoError(t, result.Error)
	assert.Equal(t, []string{SetupFresh, SetupFresh}, setups(result))
	assert.Equal(t, 1, api.gone, "an isolated case is never rebuilt")
}

// TestFuzzStale_SharedNeverRebuilds checks that a case on the target's own
// state is judged on a stale answer: there is no copy to build afresh.
func TestFuzzStale_SharedNeverRebuilds(t *testing.T) {
	eng, api := buildTravelEngine(t)
	api.goneName = "Gone"
	p := travelPlan(name("name.empty", plan.FuzzNegative, ""), name("name.gone", plan.FuzzPositive, "Gone"))
	p.Execution.Steps[3].FuzzSettings.Scope = plan.FuzzScopeShared
	result := eng.Run(context.Background(), p)
	assert.Equal(t, []string{"", ""}, setups(result))
	assert.Equal(t, 1, api.gone)
	assert.Nil(t, fuzzSteps(result)[1].Fuzz.Stale)
}

// TestFuzzStale_FailedRebuildIsNotSent checks that a case whose rebuilt
// setup failed is not sent, and says what the reused one answered.
func TestFuzzStale_FailedRebuildIsNotSent(t *testing.T) {
	eng, api := buildTravelEngine(t)
	api.expireAfter, api.maxCreates = 2, 2 // the copy's traveler and one case; no third reservation
	result := eng.Run(context.Background(), travelPlan(name("name.empty", plan.FuzzNegative, ""), name("name.empty2", plan.FuzzNegative, "")))
	require.NoError(t, result.Error)

	assert.Equal(t, []string{SetupFresh, SetupFailed}, setups(result))
	second := fuzzSteps(result)[1]
	assert.Equal(t, FindingNotSent, second.Fuzz.Finding)
	require.NotNil(t, second.Fuzz.Stale)
	assert.Equal(t, http.StatusGone, second.Fuzz.Stale.Status)
}

// TestFuzzStale_ProgressKeepsTheCaseNumber checks that a rebuilt case keeps
// its number, with its copies taking it as they always do.
func TestFuzzStale_ProgressKeepsTheCaseNumber(t *testing.T) {
	eng, api := buildTravelEngine(t)
	api.goneName = "Gone"
	obs := &recordingObserver{}
	eng.WithProgress(obs)
	result := eng.Run(context.Background(), travelPlan(name("name.empty", plan.FuzzNegative, ""), name("name.gone", plan.FuzzPositive, "Gone")))
	require.NoError(t, result.Error)

	var numbers []int
	for _, ev := range obs.events {
		if ev.kind == "step_complete" {
			numbers = append(numbers, ev.index)
		}
	}
	require.Len(t, numbers, len(result.Steps), "the stale answer is not reported")
	for i, s := range result.Steps {
		if s.FuzzSetup != "" || s.Fuzz != nil {
			assert.Contains(t, []int{4, 5}, numbers[i], s.StepID)
		}
	}
	assert.Equal(t, 5, numbers[len(numbers)-1])
}

// TestDescribeFuzz_Setup checks that the fuzz summary counts the setups in
// the order they come about.
func TestDescribeFuzz_Setup(t *testing.T) {
	s := &archive.FuzzSummary{}
	for _, setup := range []string{SetupFailed, SetupRebuilt, SetupReused, SetupFresh, SetupReused} {
		s.Add("", setup, false)
	}
	assert.Equal(t, "5 cases: 5 as expected · setup: 1 fresh, 2 reused, 1 rebuilt, 1 failed", DescribeFuzz(s))
}
