package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/graph"
)

func repeatGraph() *graph.Graph {
	return &graph.Graph{Version: "1.0.0", Nodes: map[string]*graph.Node{
		"getSearch": {Name: "getSearch", Outputs: []graph.Output{
			{Name: "remainingBatches", Type: "integer"},
			{Name: "items", Type: "item[]"},
			{Name: "status", Type: "string"},
			{Name: "total", Type: "float"},
		}},
		"createSearch": {Name: "createSearch", Cleanup: graph.CleanupPairing{Node: "cancelSearch"},
			Outputs: []graph.Output{{Name: "status", Type: "string"}}},
		"cancelSearch": {Name: "cancelSearch"},
		"listOrders": {Name: "listOrders",
			Inputs: []graph.Input{{Name: "limit", Type: "integer", Optional: true}, {Name: "after", Type: "string", Optional: true}},
			Outputs: []graph.Output{
				{Name: "orders", Type: "order[]"},
				{Name: "orderCount", Type: "integer"},
				{Name: "nextCursor", Type: "string"},
			}},
	}}
}

func TestValidate_Repeat(t *testing.T) {
	tests := []struct {
		name   string
		node   string
		repeat RepeatConfig
		fail   bool
		want   string // "" means no repeat error
	}{
		{name: "valid", node: "getSearch", repeat: RepeatConfig{Until: `remainingBatches == 0 && status != "{{today}}"`, Collect: []string{"items", "total"}, Interval: "500ms", Max: 20, Timeout: "1m"}},
		{name: "until missing", node: "getSearch", repeat: RepeatConfig{}, want: "repeat.until is required"},
		{name: "until does not parse", node: "getSearch", repeat: RepeatConfig{Until: "remainingBatches =="}, want: `invalid repeat.until "remainingBatches =="`},
		{name: "until reads an unknown output", node: "getSearch", repeat: RepeatConfig{Until: "remaining == 0"}, want: `repeat.until reads "remaining", which is not an output of getSearch (outputs: items, remainingBatches, status, total)`},
		{name: "collect names an unknown output", node: "getSearch", repeat: RepeatConfig{Until: "remainingBatches == 0", Collect: []string{"pages"}}, want: `repeat.collect names "pages", which is not an output of getSearch`},
		{name: "collect names a string", node: "getSearch", repeat: RepeatConfig{Until: "remainingBatches == 0", Collect: []string{"status"}}, want: `repeat.collect can't gather "status", a string output`},
		{name: "max too large", node: "getSearch", repeat: RepeatConfig{Until: "remainingBatches == 0", Max: 1001}, want: "repeat.max must be from 1 to 1000, or left out for 50"},
		{name: "interval not a duration", node: "getSearch", repeat: RepeatConfig{Until: "remainingBatches == 0", Interval: "fast"}, want: `repeat.interval "fast" is not a duration`},
		{name: "timeout shorter than interval", node: "getSearch", repeat: RepeatConfig{Until: "remainingBatches == 0", Interval: "2s", Timeout: "1s"}, want: "repeat.timeout 1s is shorter than repeat.interval 2s"},
		{name: "with expectFailure", node: "getSearch", repeat: RepeatConfig{Until: "remainingBatches == 0"}, fail: true, want: "repeat can't be combined with expectFailure"},
		{name: "on a node with a cleanup pairing", node: "createSearch", repeat: RepeatConfig{Until: `status == "done"`}, want: "repeat is for reads, but createSearch has a cleanup pairing (cancelSearch)"},
		{name: "next without until", node: "listOrders", repeat: RepeatConfig{Next: map[string]string{"after": "nextCursor"}, Collect: []string{"orders", "orderCount"}, Max: 100}},
		{name: "next with a timeout and the default interval", node: "listOrders", repeat: RepeatConfig{Next: map[string]string{"after": "nextCursor"}, Timeout: "500ms"}},
		{name: "neither until nor next", node: "listOrders", repeat: RepeatConfig{Collect: []string{"orders"}}, want: "repeat.until is required unless repeat.next is set"},
		{name: "next sets an unknown input", node: "listOrders", repeat: RepeatConfig{Next: map[string]string{"cursor": "nextCursor"}}, want: `repeat.next sets "cursor", which is not an input of listOrders (inputs: after, limit)`},
		{name: "next reads an unknown output", node: "listOrders", repeat: RepeatConfig{Next: map[string]string{"after": "next"}}, want: `repeat.next reads "next", which is not an output of listOrders (outputs: nextCursor, orderCount, orders)`},
		{name: "next reads a list", node: "listOrders", repeat: RepeatConfig{Next: map[string]string{"after": "orders"}}, want: `repeat.next can't send "orders", a order[] output: a cursor is a string or integer output`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := tt.repeat
			step := Step{Node: tt.node, Repeat: &rc}
			if tt.fail {
				step.ExpectFailure = &ExpectFailure{Status: []int{404}}
			}
			err := Validate(&Plan{Execution: Execution{Steps: []Step{step}}}, repeatGraph())
			if tt.want == "" {
				if err != nil {
					assert.NotContains(t, err.Error(), "repeat")
				}
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestValidate_RepeatOnVerificationStep(t *testing.T) {
	p := &Plan{Execution: Execution{
		Steps:        []Step{{Node: "getSearch"}},
		Verification: []VerificationStep{{Node: "getSearch", Repeat: &RepeatConfig{Until: "done == true"}}},
	}}

	err := Validate(p, repeatGraph())

	require.Error(t, err)
	assert.Contains(t, err.Error(), `verification step 0 (getSearch): repeat.until reads "done"`)
}

func TestParse_RepeatBlock(t *testing.T) {
	p, err := Parse([]byte("execution:\n  steps:\n    - node: getSearch\n      repeat:\n        until: remainingBatches == 0\n        collect: [items]\n        interval: 250ms\n        max: 10\n        timeout: 30s\n"))
	require.NoError(t, err)
	assert.Equal(t, &RepeatConfig{Until: "remainingBatches == 0", Collect: []string{"items"}, Interval: "250ms", Max: 10, Timeout: "30s"}, p.Execution.Steps[0].Repeat)

	_, err = Parse([]byte("execution:\n  steps:\n    - node: getSearch\n      repeat: {until: x, sleep: 1s}\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown key "sleep"`)
}

func TestParse_RepeatNextRoundTrip(t *testing.T) {
	p, err := Parse([]byte("execution:\n  steps:\n    - node: listOrders\n      repeat:\n        next: {after: nextCursor}\n        collect: [orders]\n        max: 100\n"))
	require.NoError(t, err)
	want := &RepeatConfig{Next: map[string]string{"after": "nextCursor"}, Collect: []string{"orders"}, Max: 100}
	assert.Equal(t, want, p.Execution.Steps[0].Repeat)

	out, err := Marshal(p)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "until", "a repeat that pages writes no empty until")
	back, err := Parse(out)
	require.NoError(t, err)
	assert.Equal(t, want, back.Execution.Steps[0].Repeat)
}

func TestRepeatConfig_IntervalWithNext(t *testing.T) {
	paging := &RepeatConfig{Next: map[string]string{"after": "nextCursor"}}
	interval, err := paging.IntervalDuration()
	require.NoError(t, err)
	assert.Zero(t, interval, "pages are requested without a wait")

	polling := &RepeatConfig{Until: "done == true"}
	interval, err = polling.IntervalDuration()
	require.NoError(t, err)
	assert.Equal(t, DefaultRepeatInterval, interval)

	clone := paging.Clone()
	clone.Next["page"] = "nextPage"
	assert.Len(t, paging.Next, 1, "a clone shares no map with its original")
}

// TestInstantiate_MutationOfRepeatedStepDoesNotRepeat checks that a mutation's
// expected failure is sent once: repeat and expectFailure can't be combined.
func TestInstantiate_MutationOfRepeatedStepDoesNotRepeat(t *testing.T) {
	g := repeatGraph()
	g.Nodes["getSearch"].Inputs = []graph.Input{{Name: "searchId", Type: "string"}}
	p := &Plan{Execution: Execution{Steps: []Step{{
		Node:      "getSearch",
		Values:    map[string]StepValue{"searchId": {Default: "s1"}},
		Repeat:    &RepeatConfig{Until: "remainingBatches == 0"},
		Mutations: []Mutation{{Name: "unknown-search", Set: map[string]any{"searchId": "nope"}, ExpectStatus: []int{404}}},
	}}}}

	instantiated, err := InstantiateAndValidate(p, g)

	require.NoError(t, err)
	require.Len(t, instantiated.Execution.Steps, 2)
	assert.NotNil(t, instantiated.Execution.Steps[0].Repeat)
	assert.Nil(t, instantiated.Execution.Steps[1].Repeat, "the mutation sends its request once")
}
