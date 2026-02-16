package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlanMain_NoSubcommand(t *testing.T) {
	code := planMain(nil)
	assert.Equal(t, 1, code)
}

func TestPlanMain_UnknownSubcommand(t *testing.T) {
	code := planMain([]string{"unknown"})
	assert.Equal(t, 1, code)
}

func TestPlanValidate_MissingGraphFlag(t *testing.T) {
	code := planValidateMain([]string{})
	assert.Equal(t, 1, code)
}

func TestPlanValidate_SinglePlan_Valid(t *testing.T) {
	code := planValidateCommand(&planValidateArgs{
		GraphPath: "testdata/test_graph.yaml",
		PlanPath:  "testdata/test_plan.yaml",
	})
	assert.Equal(t, 0, code)
}

func TestPlanValidate_SinglePlan_Invalid(t *testing.T) {
	code := planValidateCommand(&planValidateArgs{
		GraphPath: "testdata/test_graph.yaml",
		PlanPath:  "testdata/test_plan_invalid.yaml",
	})
	assert.Equal(t, 1, code)
}

func TestPlanValidate_SinglePlan_MissingPlanFile(t *testing.T) {
	code := planValidateCommand(&planValidateArgs{
		GraphPath: "testdata/test_graph.yaml",
		PlanPath:  "testdata/nonexistent.yaml",
	})
	assert.Equal(t, 1, code)
}

func TestPlanValidate_SinglePlan_MissingGraphFile(t *testing.T) {
	code := planValidateCommand(&planValidateArgs{
		GraphPath: "testdata/nonexistent.yaml",
		PlanPath:  "testdata/test_plan.yaml",
	})
	assert.Equal(t, 1, code)
}

func TestPlanValidate_SinglePlan_WithUnfed(t *testing.T) {
	code := planValidateCommand(&planValidateArgs{
		GraphPath: "testdata/test_graph.yaml",
		PlanPath:  "testdata/test_plan.yaml",
		Unfed:     true,
	})
	assert.Equal(t, 0, code)
}

func TestPlanValidate_AllTemplates_NoWorkflows(t *testing.T) {
	// test_graph.yaml has no workflows, should succeed with "no templates" message.
	code := planValidateCommand(&planValidateArgs{
		GraphPath: "testdata/test_graph.yaml",
	})
	assert.Equal(t, 0, code)
}

func TestPlanValidate_AllTemplates_Valid(t *testing.T) {
	code := planValidateCommand(&planValidateArgs{
		GraphPath: "testdata/test_graph_with_workflows.yaml",
	})
	assert.Equal(t, 0, code)
}

func TestPlanValidate_AllTemplates_SomeInvalid(t *testing.T) {
	code := planValidateCommand(&planValidateArgs{
		GraphPath: "testdata/test_graph_with_bad_workflows.yaml",
	})
	assert.Equal(t, 1, code)
}

func TestPlanValidate_AllTemplates_WithUnfed(t *testing.T) {
	code := planValidateCommand(&planValidateArgs{
		GraphPath: "testdata/test_graph_with_workflows.yaml",
		Unfed:     true,
	})
	assert.Equal(t, 0, code)
}

func TestPlanValidateMain_FlagParsing(t *testing.T) {
	code := planValidateMain([]string{
		"--graph", "testdata/test_graph.yaml",
		"--plan", "testdata/test_plan.yaml",
		"--unfed",
	})
	assert.Equal(t, 0, code)
}

func TestPlanValidate_TravelportWorkflows(t *testing.T) {
	graphPath := "../../travelport/graph.yaml"
	if _, err := os.Stat(graphPath); os.IsNotExist(err) {
		t.Skip("travelport graph not available")
	}
	code := planValidateCommand(&planValidateArgs{
		GraphPath: graphPath,
		Unfed:     true,
	})
	assert.Equal(t, 0, code)
}
