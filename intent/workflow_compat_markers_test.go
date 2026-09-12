package intent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckWorkflowCompat_BaseMarkers(t *testing.T) {
	values := func(name, marker string) map[string]plan.StepValue {
		return map[string]plan.StepValue{name: {Default: marker}}
	}
	tests := []struct {
		name              string
		base              []plan.Step
		options           [][]plan.Step // one template per option of a "cart" slot; nil: no slot
		addon             []plan.Step   // nil: no addon
		optionalItinerary bool          // commit.itineraryId is optional in the graph
		want              []string      // prefixes of "workflow step.input: message"
		wantUnfed         [][]string    // addon warnings' unfed inputs
	}{
		{
			name: "fed by the base",
			base: []plan.Step{{Node: "search"}, {Node: "book"}, {Node: "commit", Values: values("itineraryId", "AUTOWIRE")}},
		},
		{
			name:  "fed only by an addon",
			base:  []plan.Step{{Node: "search"}, {Node: "commit", Values: values("itineraryId", "AUTOWIRE")}},
			addon: []plan.Step{{Node: "book"}},
			want:  []string{`Base commit.itineraryId: unfed AUTOWIRE: only addon "Addon" produces it; use AUTOWIRE? if the input is optional`},
		},
		{
			name: "fed by nothing",
			base: []plan.Step{{Node: "search"}, {Node: "commit", Values: values("itineraryId", "AUTOWIRE")}},
			want: []string{`Base commit.itineraryId: unfed AUTOWIRE: no step of the base or its slots produces it`},
		},
		{
			name: "a name no node produces is left to overrides",
			base: []plan.Step{{Node: "book", Values: values("itemId", "AUTOWIRE")}},
		},
		{
			name: "a slot option's own step feeds its marker",
			base: []plan.Step{{Node: "search"}, {Slot: "cart"}},
			options: [][]plan.Step{
				{{Node: "book"}, {Node: "commit", Values: values("itineraryId", "AUTOWIRE")}},
				{{Node: "search"}},
			},
		},
		{
			name: "a slot option's marker nothing in the base feeds",
			base: []plan.Step{{Node: "search"}, {Slot: "cart"}},
			options: [][]plan.Step{
				{{Node: "book"}},
				{{Node: "commit", Values: values("itineraryId", "AUTOWIRE")}},
			},
			want: []string{`Base commit.itineraryId: unfed AUTOWIRE`},
		},
		{
			name:              "an optional marker on an optional input never warns",
			base:              []plan.Step{{Node: "search"}, {Node: "commit", Values: values("itineraryId", "AUTOWIRE?")}},
			addon:             []plan.Step{{Node: "book"}},
			optionalItinerary: true,
		},
		{
			name: "an optional marker on a required input",
			base: []plan.Step{{Node: "search"}, {Node: "book"}, {Node: "commit", Values: values("itineraryId", "AUTOWIRE?")}},
			want: []string{`Base commit.itineraryId: AUTOWIRE? on a required input with no graph default`},
		},
		{
			name: "an addon's optional marker is never unfed",
			base: []plan.Step{{Node: "search"}},
			addon: []plan.Step{{Node: "addonNode", Values: map[string]plan.StepValue{
				"itineraryId":  {Default: "AUTOWIRE"},
				"specialInput": {Default: "AUTOWIRE?"},
			}}},
			want:      []string{`Addon addonNode.specialInput: AUTOWIRE? on a required input`},
			wantUnfed: [][]string{{"itineraryId"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := buildCompatTestGraph()
			if tt.optionalItinerary {
				g.Nodes["commit"].Inputs[0].Optional = true
			}
			base := graph.Workflow{Name: "Base", Template: "base"}
			plans := map[string]*plan.Plan{"base": {Execution: plan.Execution{Steps: tt.base}}}
			var others []graph.Workflow
			var optionNames []string
			for i, steps := range tt.options {
				name := fmt.Sprintf("Opt%d", i)
				optionNames = append(optionNames, name)
				others = append(others, graph.Workflow{Name: name, Kind: "slot", Template: name})
				plans[name] = &plan.Plan{Execution: plan.Execution{Steps: steps}}
			}
			if len(optionNames) > 0 {
				base.Slots = []graph.SlotDef{{Name: "cart", Options: optionNames, Default: optionNames[0]}}
			}
			if tt.addon != nil {
				others = append(others, graph.Workflow{Name: "Addon", Kind: "addon", Template: "addon", After: graph.AfterSpec{"search"}})
				plans["addon"] = &plan.Plan{Execution: plan.Execution{Steps: tt.addon}}
			}
			g.Workflows = append([]graph.Workflow{base}, others...)

			result := checkWorkflowCompat(g, plans)

			var got []string
			for _, w := range result.MarkerWarnings {
				got = append(got, fmt.Sprintf("%s %s.%s: %s", w.Workflow, w.Step, w.Input, w.Message))
			}
			require.Len(t, got, len(tt.want), "marker warnings: %v", got)
			for i, want := range tt.want {
				assert.True(t, strings.HasPrefix(got[i], want), "warning %q should start with %q", got[i], want)
			}
			var unfed [][]string
			for _, w := range result.Warnings {
				unfed = append(unfed, w.UnfedInputs)
			}
			assert.Equal(t, tt.wantUnfed, unfed)
		})
	}
}

func TestWorkflowCompatResult_FormatMarkers(t *testing.T) {
	r := &WorkflowCompatResult{MarkerWarnings: []WorkflowMarkerWarning{{
		Workflow: "Checkout",
		Step:     "checkout",
		Input:    "giftMessage",
		Message:  `unfed AUTOWIRE: only addon "Gift Wrap" produces it; use AUTOWIRE? if the input is optional`,
	}}}

	assert.True(t, r.HasWarnings())
	assert.Equal(t,
		"AUTOWIRE marker warnings:\n"+
			`  workflow "Checkout": checkout.giftMessage: unfed AUTOWIRE: only addon "Gift Wrap" produces it; use AUTOWIRE? if the input is optional`+"\n",
		r.Format())
}
