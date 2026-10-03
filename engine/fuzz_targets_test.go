package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/plan"
)

func caseIDs(result *RunResult) []string {
	var ids []string
	for _, s := range fuzzSteps(result) {
		ids = append(ids, s.Fuzz.Case.ID)
	}
	return ids
}

// TestFuzz_ExpectFailureStepsAreNotNodeTargets checks that naming a node
// fuzzes the plan's steps of it that should succeed, not a negative step
// written for one bad request, and that naming such a step is an error.
func TestFuzz_ExpectFailureStepsAreNotNodeTargets(t *testing.T) {
	server := buggyQuantityServer(t)
	p := quantityPlan()
	p.Execution.Steps = append(p.Execution.Steps,
		plan.Step{ID: "addBad", Node: "addItem", RawBody: `{"quantity": `, ExpectFailure: &plan.ExpectFailure{Status: plan.ExpectedStatuses{{Code: 400}}}},
		plan.Step{ID: "addNeg", Node: "addItem", Values: map[string]plan.StepValue{"quantity": {Default: -3}},
			ExpectFailure: &plan.ExpectFailure{Status: plan.ExpectedStatuses{{Code: 400}}}},
	)
	result := buildQuantityEngine(t, server.URL).WithFuzz(&FuzzConfig{Targets: []string{"addItem"}, Cases: []string{"quantity.at-max"}}).
		Run(context.Background(), p)
	require.NoError(t, result.Error)
	var targets []string
	for _, s := range fuzzSteps(result) {
		targets = append(targets, s.Fuzz.Case.Target)
	}
	assert.Equal(t, []string{"add"}, targets)

	for _, id := range []string{"addBad", "addNeg"} {
		result = buildQuantityEngine(t, server.URL).WithFuzz(&FuzzConfig{Targets: []string{id}}).Run(context.Background(), p)
		assert.Equal(t, OutcomeError, result.Outcome, id)
		assert.ErrorContains(t, result.Error, fmt.Sprintf("--fuzz %s:", id))
	}

	p.Execution.Steps[1].FuzzSettings = &plan.FuzzSettings{}
	result = buildQuantityEngine(t, server.URL).Run(context.Background(), p)
	assert.Equal(t, OutcomeError, result.Outcome)
	assert.ErrorContains(t, result.Error, "addBad")
}

// TestFuzz_InputsAcrossTargets checks that --fuzz-input names inputs of any
// target: each takes those its node has, and only a name no target has is an
// error.
func TestFuzz_InputsAcrossTargets(t *testing.T) {
	eng, _ := buildTravelEngine(t)
	p := travelPlan()
	p.Execution.Steps = append(p.Execution.Steps, plan.Step{ID: "read", Node: "getReservation", Values: map[string]plan.StepValue{"reservationId": {From: "res.id"}}})
	result := eng.WithFuzz(&FuzzConfig{Targets: []string{"t2", "read"}, Inputs: []string{"name"}, Cases: []string{"name.empty"}}).
		Run(context.Background(), p)
	require.NoError(t, result.Error)
	assert.Equal(t, []string{"name.empty"}, caseIDs(result), "read has no name input, and nothing else of it is fuzzed")

	eng, _ = buildTravelEngine(t)
	result = eng.WithFuzz(&FuzzConfig{Targets: []string{"t2"}, Inputs: []string{"name", "notes"}}).Run(context.Background(), travelPlan())
	assert.Equal(t, OutcomeError, result.Outcome)
	assert.EqualError(t, result.Error, "--fuzz-input notes: no step --fuzz names has that input")
}

// TestFuzz_CaseFilterBeforeTheCap checks that --fuzz-case names cases the
// step has, whichever of them the cap then keeps.
func TestFuzz_CaseFilterBeforeTheCap(t *testing.T) {
	server := buggyQuantityServer(t)
	for seed := uint64(1); seed <= 5; seed++ {
		result := buildQuantityEngine(t, server.URL).WithSeed(seed).
			WithFuzz(&FuzzConfig{Targets: []string{"add"}, Cases: []string{"quantity.below-min", "quantity.fraction"}, Max: 1}).
			Run(context.Background(), quantityPlan())
		require.NotEqual(t, OutcomeError, result.Outcome, "seed %d: %v", seed, result.Error)
		assert.Len(t, caseIDs(result), 1)
		assert.True(t, result.FuzzCapped)
	}
}

// TestFuzz_PinnedCasesFollowTheFilters checks that the modes, inputs, skip
// list, and cap apply to pinned cases as they do to generated ones.
func TestFuzz_PinnedCasesFollowTheFilters(t *testing.T) {
	server := buggyQuantityServer(t)
	pinned := []plan.PinnedFuzzCase{
		{ID: "quantity.ok", Mode: plan.FuzzPositive, Input: "quantity", Value: 3},
		{ID: "quantity.bad", Mode: plan.FuzzNegative, Input: "quantity", Value: -4},
		{ID: "body.odd", Mode: plan.FuzzEdge, Patch: []plan.RequestPatch{{Where: "body", Path: "odd", Op: "set", Value: true}}},
	}
	run := func(block plan.FuzzSettings, cfg *FuzzConfig) []string {
		p := quantityPlan()
		block.Pinned = pinned
		p.Execution.Steps[0].FuzzSettings = &block
		eng := buildQuantityEngine(t, server.URL)
		if cfg != nil {
			eng.WithFuzz(cfg)
		}
		result := eng.Run(context.Background(), p)
		require.NotEqual(t, OutcomeError, result.Outcome, "%v", result.Error)
		return caseIDs(result)
	}

	assert.Equal(t, []string{"quantity.ok", "quantity.bad", "body.odd"}, run(plan.FuzzSettings{}, nil))

	negative := run(plan.FuzzSettings{Mode: []string{plan.FuzzNegative}}, nil)
	assert.Contains(t, negative, "quantity.bad")
	assert.Contains(t, negative, "quantity.below-min", "the mode asks for generated cases too")
	assert.NotContains(t, negative, "quantity.ok")
	assert.NotContains(t, negative, "body.odd")

	assert.Equal(t, []string{"body.odd"}, run(plan.FuzzSettings{Skip: []string{"quantity"}, Only: []string{"body.odd"}}, nil))
	assert.Len(t, run(plan.FuzzSettings{Cases: 2, Only: []string{"quantity.ok", "quantity.bad", "body.odd"}}, nil), 2)

	fromCLI := run(plan.FuzzSettings{}, &FuzzConfig{Targets: []string{"add"}, Inputs: []string{"quantity"}, Modes: []string{plan.FuzzNegative}})
	assert.Contains(t, fromCLI, "quantity.bad")
	assert.NotContains(t, fromCLI, "quantity.ok")
	assert.NotContains(t, fromCLI, "body.odd", "a case of no input is left out when --fuzz-input names some")
}

// TestFuzz_BlockOnlyIsChecked checks that a fuzz: block's only: list naming a
// case the step doesn't have is an error, not a block that runs nothing.
func TestFuzz_BlockOnlyIsChecked(t *testing.T) {
	server := buggyQuantityServer(t)
	p := quantityPlan()
	p.Execution.Steps[0].FuzzSettings = &plan.FuzzSettings{Only: []string{"quantity.blow-min"}}
	result := buildQuantityEngine(t, server.URL).Run(context.Background(), p)
	assert.Equal(t, OutcomeError, result.Outcome)
	assert.EqualError(t, result.Error, `fuzz target add: only: the step has no case "quantity.blow-min"`)
}

// TestFuzzConfig_UnmatchedAcrossRuns checks that a batch's names are checked
// across its plans: one plan matching a name is enough, and a name no plan
// matched is an error.
func TestFuzzConfig_UnmatchedAcrossRuns(t *testing.T) {
	server := buggyQuantityServer(t)
	other := &plan.Plan{Metadata: plan.Metadata{GraphVersion: "1.0.0"}, Execution: plan.Execution{Steps: []plan.Step{
		{ID: "elsewhere", Node: "addItem"},
	}}}

	cfg := &FuzzConfig{Targets: []string{"add", "addItme"}, Cases: []string{"quantity.above-max"}, AllowNoTarget: true, Matched: &FuzzMatches{}}
	for _, p := range []*plan.Plan{quantityPlan(), other} {
		result := buildQuantityEngine(t, server.URL).WithFuzz(cfg).Run(context.Background(), p)
		require.NotEqual(t, OutcomeError, result.Outcome, "%v", result.Error)
	}
	assert.EqualError(t, cfg.Unmatched(), "--fuzz addItme: no step has that ID, and no step of that node is meant to succeed")

	cfg = &FuzzConfig{Targets: []string{"add"}, Cases: []string{"quantity.above-max", "quantity.x"}, AllowNoTarget: true, Matched: &FuzzMatches{}}
	result := buildQuantityEngine(t, server.URL).WithFuzz(cfg).Run(context.Background(), other)
	require.NotEqual(t, OutcomeError, result.Outcome, "a plan without the target runs as written")
	assert.EqualError(t, cfg.Unmatched(), "--fuzz add: no step has that ID, and no step of that node is meant to succeed")
	result = buildQuantityEngine(t, server.URL).WithFuzz(cfg).Run(context.Background(), quantityPlan())
	require.NotEqual(t, OutcomeError, result.Outcome, "%v", result.Error)
	assert.EqualError(t, cfg.Unmatched(), "--fuzz-case quantity.x: no step --fuzz names has a case with that ID")

	assert.NoError(t, (*FuzzConfig)(nil).Unmatched())
	assert.NoError(t, (&FuzzConfig{Targets: []string{"x"}}).Unmatched(), "nothing to check without Matched")
}

