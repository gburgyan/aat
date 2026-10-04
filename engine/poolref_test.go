package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

func refKB() *domain.KnowledgeBase {
	return &domain.KnowledgeBase{ValuePools: map[string]*domain.ValuePool{
		"refs": {Groups: map[string][]string{"eu": {"eu-1", "eu-2", "eu-3"}, "us": {"us-1"}}},
	}}
}

func orderPlan(sv plan.StepValue) *plan.Plan {
	p := &plan.Plan{Metadata: plan.Metadata{GraphVersion: "1.0.0"}}
	step := plan.Step{Node: "createOrder"}
	if !sv.IsEmpty() {
		step.Values = map[string]plan.StepValue{"ref": sv}
	}
	p.Execution.Steps = []plan.Step{step}
	return p
}

func TestPoolRef_StepValueDrawsFromDomain(t *testing.T) {
	server := okServer(t)
	eng := buildOrderEngine(t, server.URL).WithDomain(refKB()).WithSeed(3)

	result := eng.Run(context.Background(), orderPlan(plan.StepValue{PoolRef: "refs.eu"}))
	require.Equal(t, OutcomePassed, result.Outcome, "run error: %v", result.Error)
	step := result.Steps[0]
	assert.Contains(t, []any{"eu-1", "eu-2", "eu-3"}, step.Inputs["ref"])

	res := step.Resolutions[0]
	assert.Equal(t, "fallback_pool", res.Source)
	assert.Equal(t, "refs.eu", res.PoolRef)
	assert.Equal(t, 3, res.PoolSize)
	assert.True(t, result.DrewRandomly())
}

func TestPoolRef_GraphDefaultAndLayer(t *testing.T) {
	server := okServer(t)
	eng := buildOrderEngine(t, server.URL).WithDomain(refKB())
	eng.graph.Nodes["createOrder"].Inputs[0].Default = &graph.InputDefault{PoolRef: "refs.us"}

	result := eng.Run(context.Background(), orderPlan(plan.StepValue{}))
	require.Equal(t, OutcomePassed, result.Outcome, "run error: %v", result.Error)
	assert.Equal(t, "us-1", result.Steps[0].Inputs["ref"])

	layered := buildOrderEngine(t, server.URL).WithDomain(refKB()).
		WithLayers(map[string]*graph.InputDefault{"createOrder.ref": {PoolRef: "refs.eu", Layer: "eu"}})
	layered.graph.Nodes["createOrder"].Inputs[0].Default = &graph.InputDefault{PoolRef: "refs.us"}
	result = layered.Run(context.Background(), orderPlan(plan.StepValue{}))
	require.Equal(t, OutcomePassed, result.Outcome, "run error: %v", result.Error)
	assert.Contains(t, []any{"eu-1", "eu-2", "eu-3"}, result.Steps[0].Inputs["ref"])
}

func TestPoolRef_ConstraintFiltersDomainValues(t *testing.T) {
	server := okServer(t)
	for seed := range uint64(10) {
		result := buildOrderEngine(t, server.URL).WithDomain(refKB()).WithSeed(seed).
			Run(context.Background(), orderPlan(plan.StepValue{PoolRef: "refs", Constraint: `value != "eu-1" && value != "eu-2"`}))
		require.Equal(t, OutcomePassed, result.Outcome, "run error: %v", result.Error)
		assert.Contains(t, []any{"eu-3", "us-1"}, result.Steps[0].Inputs["ref"])
	}
}

func TestPoolRef_UnknownPoolFailsTheStep(t *testing.T) {
	server := okServer(t)
	result := buildOrderEngine(t, server.URL).WithDomain(refKB()).
		Run(context.Background(), orderPlan(plan.StepValue{PoolRef: "ref"}))
	require.NotEqual(t, OutcomePassed, result.Outcome)
	require.Len(t, result.Steps, 1)
	require.Error(t, result.Steps[0].Error)
	assert.Contains(t, result.Steps[0].Error.Error(), `no value pool "ref" (did you mean "refs"?)`)
}
