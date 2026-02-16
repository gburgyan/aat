package intent

import (
	"strings"
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tpGraphPath = "../travelport/graph.yaml"
const tpGraphDir = "../travelport"

// TestTravelportTemplates_AllValidate loads every workflow template in the
// travelport graph and validates it against the graph. This catches regressions
// when graph nodes, edges, or outputs change.
func TestTravelportTemplates_AllValidate(t *testing.T) {
	g, err := graph.ParseFile(tpGraphPath)
	require.NoError(t, err)

	templatedWorkflows := 0
	for _, wf := range g.Workflows {
		if wf.Template == "" {
			continue
		}
		templatedWorkflows++
		t.Run(wf.Name, func(t *testing.T) {
			p, err := LoadWorkflowTemplate(wf.Template, tpGraphDir, g)
			require.NoError(t, err, "failed to load template for workflow %q", wf.Name)

			err = plan.Validate(p, g)
			assert.NoError(t, err, "template validation failed for workflow %q", wf.Name)
		})
	}

	// Ensure we found all 12 templated workflows (9 original + 3 composed).
	assert.Equal(t, 12, templatedWorkflows, "expected 12 workflows with templates")
}

// TestTravelportTemplates_UnfedInputs verifies the unfed inputs for each
// workflow template. Unfed inputs are values the LLM/user must provide.
func TestTravelportTemplates_UnfedInputs(t *testing.T) {
	g, err := graph.ParseFile(tpGraphPath)
	require.NoError(t, err)

	// Unfed inputs are those not wired in the plan template (no from, fromSelection,
	// select, or default value). Auto-wired graph edges don't count as plan wiring.
	// Templates provide literal defaults for user-facing inputs, so the only unfed
	// inputs are auto-wired ones like fopIdentifierValue and offerId on addPayment.
	tests := []struct {
		workflow string
		contains []string // substrings that should appear in unfed list
		exact    int      // exact expected count (-1 = don't check)
	}{
		{
			workflow:  "Full-Payload Booking",
			contains:  []string{"addPayment.fopIdentifierValue", "addPayment.offerId"},
			exact:     2,
		},
		{
			workflow:  "Reference-Based Booking",
			contains:  []string{"addPayment.fopIdentifierValue", "addPayment.offerId"},
			exact:     2,
		},
		{
			workflow:  "Post-Commit Ticketing",
			contains:  []string{"addPayment.fopIdentifierValue", "addPayment.offerId"},
			exact:     2,
		},
		{
			workflow:  "Exchange",
			contains:  []string{"addPayment.fopIdentifierValue", "addPayment.offerId"},
			exact:     2,
		},
		{
			workflow:  "Ancillary Booking",
			contains:  nil,
			exact:     0,
		},
		{
			workflow:  "Seat Selection",
			contains:  nil,
			exact:     0,
		},
		{
			workflow:  "Traveler Modification",
			contains:  nil,
			exact:     0,
		},
		{
			workflow:  "Round-Trip Full-Payload Booking",
			contains:  []string{"addPayment.fopIdentifierValue", "addPayment.offerId"},
			exact:     2,
		},
		{
			workflow:  "Round-Trip Leg-Based Booking",
			contains:  []string{"addPayment.fopIdentifierValue", "addPayment.offerId"},
			exact:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.workflow, func(t *testing.T) {
			tmplPath, found := FindWorkflowTemplate(g, tt.workflow)
			require.True(t, found, "workflow %q should have a template", tt.workflow)

			p, err := LoadWorkflowTemplate(tmplPath, tpGraphDir, g)
			require.NoError(t, err)

			unfed := UnfedInputsFromTemplate(p, g)
			for _, expected := range tt.contains {
				hasMatch := false
				for _, u := range unfed {
					if strings.Contains(u, expected) {
						hasMatch = true
						break
					}
				}
				assert.True(t, hasMatch, "expected unfed input containing %q for workflow %q, got: %v", expected, tt.workflow, unfed)
			}
			if tt.exact >= 0 {
				assert.Equal(t, tt.exact, len(unfed), "unfed count mismatch for workflow %q: %v", tt.workflow, unfed)
			}
		})
	}
}

// TestTravelportTemplates_GoalConsistency verifies that booking workflows
// have the correct goal step and sub-workflows handle goals appropriately.
func TestTravelportTemplates_GoalConsistency(t *testing.T) {
	g, err := graph.ParseFile(tpGraphPath)
	require.NoError(t, err)

	// Workflows that should have isGoal set on their final step.
	goalWorkflows := map[string]string{
		"Full-Payload Booking":           "commitReservation",
		"Reference-Based Booking":        "commitReservation",
		"Post-Commit Ticketing":          "commitTicket",
		"Exchange":                        "commitExchangeTicket",
		"Round-Trip Full-Payload Booking": "commitReservation",
		"Round-Trip Leg-Based Booking":    "commitReservation",
	}

	for wfName, expectedGoal := range goalWorkflows {
		t.Run(wfName, func(t *testing.T) {
			tmplPath, found := FindWorkflowTemplate(g, wfName)
			require.True(t, found)

			p, err := LoadWorkflowTemplate(tmplPath, tpGraphDir, g)
			require.NoError(t, err)

			// Find the goal step.
			var goalNode string
			goalCount := 0
			for _, step := range p.Execution.Steps {
				if step.IsGoal {
					goalNode = step.Node
					goalCount++
				}
			}
			assert.Equal(t, 1, goalCount, "workflow %q should have exactly one goal step", wfName)
			assert.Equal(t, expectedGoal, goalNode, "workflow %q goal mismatch", wfName)

			// Intent goal should match if set.
			if p.Intent.Goal != "" {
				assert.Equal(t, expectedGoal, p.Intent.Goal, "intent.goal mismatch for %q", wfName)
			}
		})
	}

	// Sub-workflows (no goal step expected).
	subWorkflows := []string{
		"Ancillary Booking",
		"Seat Selection",
		"Traveler Modification",
	}

	for _, wfName := range subWorkflows {
		t.Run(wfName+"_NoGoal", func(t *testing.T) {
			tmplPath, found := FindWorkflowTemplate(g, wfName)
			require.True(t, found)

			p, err := LoadWorkflowTemplate(tmplPath, tpGraphDir, g)
			require.NoError(t, err)

			for _, step := range p.Execution.Steps {
				assert.False(t, step.IsGoal, "sub-workflow %q should not have isGoal on step %q", wfName, step.Node)
			}
		})
	}
}

// TestTravelportTemplates_CleanupPresent verifies that workflows that create
// workbenches have ignoreWorkbench cleanup, and sub-workflows do not.
func TestTravelportTemplates_CleanupPresent(t *testing.T) {
	g, err := graph.ParseFile(tpGraphPath)
	require.NoError(t, err)

	// Workflows that should have ignoreWorkbench cleanup.
	workbenchWorkflows := []string{
		"Full-Payload Booking",
		"Reference-Based Booking",
		"Post-Commit Ticketing",
		"Exchange",
		"Round-Trip Full-Payload Booking",
		"Round-Trip Leg-Based Booking",
	}

	for _, wfName := range workbenchWorkflows {
		t.Run(wfName+"_HasCleanup", func(t *testing.T) {
			tmplPath, found := FindWorkflowTemplate(g, wfName)
			require.True(t, found)

			p, err := LoadWorkflowTemplate(tmplPath, tpGraphDir, g)
			require.NoError(t, err)

			hasIgnoreWorkbench := false
			for _, cs := range p.Execution.Cleanup {
				if cs.Node == "ignoreWorkbench" {
					hasIgnoreWorkbench = true
					assert.Equal(t, "always", cs.RunOn, "ignoreWorkbench should run on 'always' for %q", wfName)
				}
			}
			assert.True(t, hasIgnoreWorkbench, "workflow %q should have ignoreWorkbench cleanup", wfName)
		})
	}

	// Sub-workflows that should NOT have cleanup.
	noCleanupWorkflows := []string{
		"Ancillary Booking",
		"Seat Selection",
		"Traveler Modification",
	}

	for _, wfName := range noCleanupWorkflows {
		t.Run(wfName+"_NoCleanup", func(t *testing.T) {
			tmplPath, found := FindWorkflowTemplate(g, wfName)
			require.True(t, found)

			p, err := LoadWorkflowTemplate(tmplPath, tpGraphDir, g)
			require.NoError(t, err)

			assert.Empty(t, p.Execution.Cleanup, "sub-workflow %q should not have cleanup steps", wfName)
		})
	}
}
