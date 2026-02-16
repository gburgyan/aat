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

const tpGraphPathCompose = "../travelport/graph.yaml"
const tpGraphDirCompose = "../travelport"

func loadTravelportForCompose(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.ParseFile(tpGraphPathCompose)
	require.NoError(t, err)
	return g
}

// TestCompose_FullPayloadWithSeatSelection verifies that composing the
// Full-Payload Booking with the Seat Selection addon produces a valid plan
// with the seat steps spliced in at the right place.
func TestCompose_FullPayloadWithSeatSelection(t *testing.T) {
	g := loadTravelportForCompose(t)

	// Find the pre-declared composed workflow.
	var wf graph.Workflow
	for _, w := range g.Workflows {
		if w.Name == "Full-Payload Booking with Seat Selection" {
			wf = w
			break
		}
	}
	require.NotEmpty(t, wf.Name, "composed workflow not found in graph")
	require.Len(t, wf.Includes, 1)
	assert.Equal(t, "Seat Selection", wf.Includes[0].Workflow)
	assert.Equal(t, "addTraveler", wf.Includes[0].After)

	// Compose it.
	p, err := ComposeWorkflowTemplate(wf, tpGraphDirCompose, g)
	require.NoError(t, err)

	// Validate the composed plan against the graph.
	err = plan.Validate(p, g)
	assert.NoError(t, err, "composed plan should validate")

	// Check step count: parent has 8 steps, addon has 2 = 10 total.
	require.Len(t, p.Execution.Steps, 10, "expected 8 parent + 2 addon steps")

	// Verify step order: seat steps should appear after addTraveler.
	stepIDs := make([]string, len(p.Execution.Steps))
	for i, s := range p.Execution.Steps {
		stepIDs[i] = s.StepID()
	}
	t.Logf("Step order: %s", strings.Join(stepIDs, " → "))

	// Find addTraveler index and seat step indices.
	travelerIdx := indexOf(stepIDs, "addTraveler")
	seatSearchIdx := indexOf(stepIDs, "inc0_searchSeatMap")
	seatAddIdx := indexOf(stepIDs, "inc0_addSeatOffer")
	require.Greater(t, travelerIdx, -1, "addTraveler not found")
	require.Greater(t, seatSearchIdx, -1, "inc0_searchSeatMap not found")
	require.Greater(t, seatAddIdx, -1, "inc0_addSeatOffer not found")

	assert.Greater(t, seatSearchIdx, travelerIdx, "seat search should be after addTraveler")
	assert.Greater(t, seatAddIdx, seatSearchIdx, "addSeat should be after seat search")

	// Verify seat steps depend on addTraveler.
	seatSearch := p.Execution.Steps[seatSearchIdx]
	assert.Contains(t, seatSearch.DependsOn, "addTraveler", "seat search should depend on addTraveler")

	// Verify PLACEHOLDER auto-wiring: workbenchId should be wired from parent.
	wbValue, hasWB := seatSearch.Values["workbenchId"]
	require.True(t, hasWB, "workbenchId should exist in seat search values")
	assert.NotEqual(t, "PLACEHOLDER", wbValue.Default, "workbenchId should not be PLACEHOLDER")
	assert.NotEmpty(t, wbValue.From, "workbenchId should have a from reference")
	t.Logf("workbenchId wired to: %s", wbValue.From)

	// Cleanup should still have ignoreWorkbench.
	require.NotEmpty(t, p.Execution.Cleanup)
	assert.Equal(t, "ignoreWorkbench", p.Execution.Cleanup[0].Node)
}

// TestCompose_FullPayloadWithAncillaries verifies ancillary addon composition.
func TestCompose_FullPayloadWithAncillaries(t *testing.T) {
	g := loadTravelportForCompose(t)

	var wf graph.Workflow
	for _, w := range g.Workflows {
		if w.Name == "Full-Payload Booking with Ancillaries" {
			wf = w
			break
		}
	}
	require.NotEmpty(t, wf.Name)

	p, err := ComposeWorkflowTemplate(wf, tpGraphDirCompose, g)
	require.NoError(t, err)

	err = plan.Validate(p, g)
	assert.NoError(t, err, "composed plan should validate")

	require.Len(t, p.Execution.Steps, 10, "expected 8 parent + 2 ancillary steps")

	// Verify ancillary steps present.
	stepIDs := collectStepIDs(p)
	t.Logf("Step order: %s", strings.Join(stepIDs, " → "))

	assert.Contains(t, stepIDs, "inc0_searchAncillaries")
	assert.Contains(t, stepIDs, "inc0_addAncillaryOffer")

	// Verify auto-wiring of workbenchId.
	ancSearch := findStep(p, "inc0_searchAncillaries")
	require.NotNil(t, ancSearch)
	wbValue, hasWB := ancSearch.Values["workbenchId"]
	require.True(t, hasWB)
	assert.NotEmpty(t, wbValue.From, "workbenchId should be auto-wired")
	t.Logf("ancillary workbenchId wired to: %s", wbValue.From)
}

// TestCompose_FullPayloadWithSeatAndAncillaries verifies double-addon composition.
func TestCompose_FullPayloadWithSeatAndAncillaries(t *testing.T) {
	g := loadTravelportForCompose(t)

	var wf graph.Workflow
	for _, w := range g.Workflows {
		if w.Name == "Full-Payload Booking with Seat and Ancillaries" {
			wf = w
			break
		}
	}
	require.NotEmpty(t, wf.Name)
	require.Len(t, wf.Includes, 2)

	p, err := ComposeWorkflowTemplate(wf, tpGraphDirCompose, g)
	require.NoError(t, err)

	err = plan.Validate(p, g)
	assert.NoError(t, err, "composed plan should validate")

	// 8 parent + 2 seat + 2 ancillary = 12 steps.
	require.Len(t, p.Execution.Steps, 12, "expected 8 parent + 2 seat + 2 ancillary steps")

	stepIDs := collectStepIDs(p)
	t.Logf("Step order: %s", strings.Join(stepIDs, " → "))

	// Both addon sets should be present.
	assert.Contains(t, stepIDs, "inc0_searchSeatMap")
	assert.Contains(t, stepIDs, "inc0_addSeatOffer")
	assert.Contains(t, stepIDs, "inc1_searchAncillaries")
	assert.Contains(t, stepIDs, "inc1_addAncillaryOffer")

	// Both should be after addTraveler.
	travelerIdx := indexOf(stepIDs, "addTraveler")
	assert.Greater(t, indexOf(stepIDs, "inc0_searchSeatMap"), travelerIdx)
	assert.Greater(t, indexOf(stepIDs, "inc1_searchAncillaries"), travelerIdx)
}

// TestCompose_DynamicSynthetic verifies dynamic composition (no pre-declared workflow).
func TestCompose_DynamicSynthetic(t *testing.T) {
	g := loadTravelportForCompose(t)

	// Find base workflow.
	var base graph.Workflow
	for _, w := range g.Workflows {
		if w.Name == "Full-Payload Booking" {
			base = w
			break
		}
	}
	require.NotEmpty(t, base.Name)

	// Build a synthetic workflow with Traveler Modification addon.
	synthetic := BuildSyntheticWorkflow(g, base, []string{"Traveler Modification"})
	require.Len(t, synthetic.Includes, 1)
	assert.Equal(t, "Traveler Modification", synthetic.Includes[0].Workflow)
	t.Logf("Synthetic insertion point: %s", synthetic.Includes[0].After)

	p, err := ComposeWorkflowTemplate(synthetic, tpGraphDirCompose, g)
	require.NoError(t, err)

	err = plan.Validate(p, g)
	assert.NoError(t, err, "dynamically composed plan should validate")

	stepIDs := collectStepIDs(p)
	t.Logf("Step order: %s", strings.Join(stepIDs, " → "))

	// 8 parent + 2 traveler mod = 10 steps.
	require.Len(t, p.Execution.Steps, 10)
	assert.Contains(t, stepIDs, "inc0_getUpdatableTravelerItems")
	assert.Contains(t, stepIDs, "inc0_updateTraveler")
}

// TestCompose_FindComposedWorkflow_Lookup verifies FindComposedWorkflow
// finds the right pre-declared workflow.
func TestCompose_FindComposedWorkflow_Lookup(t *testing.T) {
	g := loadTravelportForCompose(t)

	tests := []struct {
		addons   []string
		expected string
	}{
		{[]string{"Seat Selection"}, "Full-Payload Booking with Seat Selection"},
		{[]string{"Ancillary Booking"}, "Full-Payload Booking with Ancillaries"},
		{[]string{"Seat Selection", "Ancillary Booking"}, "Full-Payload Booking with Seat and Ancillaries"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			wf, found := FindComposedWorkflow(g, "Full-Payload Booking", tt.addons)
			require.True(t, found, "should find composed workflow for addons %v", tt.addons)
			assert.Equal(t, tt.expected, wf.Name)
		})
	}
}

// TestCompose_UnfedInputsAfterComposition checks what's still unfed
// after auto-wiring.
func TestCompose_UnfedInputsAfterComposition(t *testing.T) {
	g := loadTravelportForCompose(t)

	var wf graph.Workflow
	for _, w := range g.Workflows {
		if w.Name == "Full-Payload Booking with Seat Selection" {
			wf = w
			break
		}
	}
	require.NotEmpty(t, wf.Name)

	p, err := ComposeWorkflowTemplate(wf, tpGraphDirCompose, g)
	require.NoError(t, err)

	unfed := UnfedInputsFromTemplate(p, g)
	t.Logf("Unfed inputs after composition (%d):", len(unfed))
	for _, u := range unfed {
		t.Logf("  - %s", u)
	}

	// The parent template provides most values. After auto-wiring,
	// remaining unfed inputs are those the LLM/user must fill.
	// Just verify it's a reasonable number (not all inputs unfed).
	assert.Less(t, len(unfed), 20, "too many unfed inputs — auto-wiring may not be working")
}

// TestCompose_MarshalRoundTrip verifies the composed plan can be marshalled
// and re-parsed.
func TestCompose_MarshalRoundTrip(t *testing.T) {
	g := loadTravelportForCompose(t)

	var wf graph.Workflow
	for _, w := range g.Workflows {
		if w.Name == "Full-Payload Booking with Seat Selection" {
			wf = w
			break
		}
	}
	require.NotEmpty(t, wf.Name)

	p, err := ComposeWorkflowTemplate(wf, tpGraphDirCompose, g)
	require.NoError(t, err)

	// Marshal to YAML.
	yamlBytes, err := plan.Marshal(p)
	require.NoError(t, err)
	t.Logf("Composed plan YAML (%d bytes):\n%s", len(yamlBytes), string(yamlBytes))

	// Parse back.
	p2, err := plan.Parse(yamlBytes)
	require.NoError(t, err)

	// Validate the round-tripped plan.
	err = plan.Validate(p2, g)
	assert.NoError(t, err, "round-tripped plan should validate")

	assert.Equal(t, len(p.Execution.Steps), len(p2.Execution.Steps))
}

// --- helpers ---

func indexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}

func collectStepIDs(p *plan.Plan) []string {
	ids := make([]string, len(p.Execution.Steps))
	for i, s := range p.Execution.Steps {
		ids[i] = s.StepID()
	}
	return ids
}

func findStep(p *plan.Plan, id string) *plan.Step {
	for i := range p.Execution.Steps {
		if p.Execution.Steps[i].StepID() == id {
			return &p.Execution.Steps[i]
		}
	}
	return nil
}

// Prevent unused import warning.
var _ = fmt.Sprintf
