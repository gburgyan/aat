package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/plan"
)

// seededPlan has steps independent of one another that each draw ref from
// the same 20-value pool.
func seededPlan(steps int) *plan.Plan {
	pool := make([]any, 20)
	for i := range pool {
		pool[i] = fmt.Sprintf("ref-%02d", i)
	}
	p := &plan.Plan{Metadata: plan.Metadata{GraphVersion: "1.0.0"}}
	for i := range steps {
		p.Execution.Steps = append(p.Execution.Steps, plan.Step{
			ID:     fmt.Sprintf("order%d", i),
			Node:   "createOrder",
			Values: map[string]plan.StepValue{"ref": {Pool: pool}},
		})
	}
	return p
}

// picks returns the ref each step of a run sent, by step ID.
func picks(t *testing.T, result *RunResult) map[string]any {
	t.Helper()
	require.Equal(t, OutcomePassed, result.Outcome, "run error: %v", result.Error)
	got := map[string]any{}
	for _, s := range result.Steps {
		got[s.StepID] = s.Inputs["ref"]
	}
	return got
}

func okServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestSeed_ReplaysPoolPicks(t *testing.T) {
	server := okServer(t)

	first := buildOrderEngine(t, server.URL).WithSeed(42).Run(context.Background(), seededPlan(6))
	second := buildOrderEngine(t, server.URL).WithSeed(42).Run(context.Background(), seededPlan(6))
	other := buildOrderEngine(t, server.URL).WithSeed(43).Run(context.Background(), seededPlan(6))

	assert.Equal(t, uint64(42), first.Seed)
	assert.Equal(t, picks(t, first), picks(t, second), "the same seed draws the same values")
	assert.NotEqual(t, picks(t, first), picks(t, other), "another seed draws other values")
	assert.True(t, first.DrewRandomly())
}

// TestSeed_PicksDependOnStepNotOrder checks that a step's pick depends on its
// ID, not on how many steps drew before it, so parallel scheduling cannot
// change what a seed replays.
func TestSeed_PicksDependOnStepNotOrder(t *testing.T) {
	server := okServer(t)

	full := picks(t, buildOrderEngine(t, server.URL).WithSeed(7).Run(context.Background(), seededPlan(6)))

	// The same steps in reverse order.
	reversed := seededPlan(6)
	steps := reversed.Execution.Steps
	for i, j := 0, len(steps)-1; i < j; i, j = i+1, j-1 {
		steps[i], steps[j] = steps[j], steps[i]
	}
	assert.Equal(t, full, picks(t, buildOrderEngine(t, server.URL).WithSeed(7).Run(context.Background(), reversed)))
}

func TestSeed_UnsetPicksAndReportsOne(t *testing.T) {
	server := okServer(t)

	result := buildOrderEngine(t, server.URL).Run(context.Background(), seededPlan(4))
	require.NotZero(t, result.Seed)
	assert.Less(t, result.Seed, uint64(1)<<53, "the seed survives a float64 JSON reader")

	replay := buildOrderEngine(t, server.URL).WithSeed(result.Seed).Run(context.Background(), seededPlan(4))
	assert.Equal(t, picks(t, result), picks(t, replay))
}

func TestStepDraws_EachResolutionDrawsAfresh(t *testing.T) {
	a, b := newStepDraws(1), newStepDraws(1)
	first, again := a.next("s").Uint64(), a.next("s").Uint64()
	assert.NotEqual(t, first, again, "a repeated step draws a new source")
	assert.Equal(t, first, b.next("s").Uint64(), "the same seed and step give the same source")
	assert.NotEqual(t, first, b.next("t").Uint64(), "another step gets another source")
}

func TestDrewRandomly(t *testing.T) {
	tests := []struct {
		name string
		step StepResult
		want bool
	}{
		{"no draws", StepResult{Resolutions: []ValueResolution{{Source: "graph_default"}}}, false},
		{"single-value pool", StepResult{Resolutions: []ValueResolution{{Source: "fallback_pool", PoolSize: 1}}}, false},
		{"pool", StepResult{Resolutions: []ValueResolution{{Source: "fallback_pool", PoolSize: 3}}}, true},
		{"random selection", StepResult{Selections: []SelectionDecision{{Strategy: "random"}}}, true},
		{"first selection", StepResult{Selections: []SelectionDecision{{Strategy: "first"}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, (&RunResult{Steps: []StepResult{tt.step}}).DrewRandomly())
		})
	}
}
