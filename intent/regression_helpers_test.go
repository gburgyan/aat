package intent

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadGraph loads a graph fixture from a path relative to the repo root.
func loadGraph(t *testing.T, relPath string) *graph.Graph {
	t.Helper()
	g, err := graph.ParseFile(relPath)
	require.NoError(t, err, "failed to load graph %s", relPath)
	return g
}

// regressionFixtures holds the canned LLM response pair for a regression scenario.
type regressionFixtures struct {
	GoalJSON string
	PlanYAML string
}

// loadRegressionFixtures loads goal.json and plan.yaml from a testdata/regression/ subdirectory.
func loadRegressionFixtures(t *testing.T, dir string) regressionFixtures {
	t.Helper()
	base := filepath.Join("testdata", "regression", dir)

	goalPath := filepath.Join(base, "goal.json")
	goalData, err := os.ReadFile(goalPath)
	require.NoError(t, err, "failed to read %s", goalPath)

	planPath := filepath.Join(base, "plan.yaml")
	planData, err := os.ReadFile(planPath)
	require.NoError(t, err, "failed to read %s", planPath)

	return regressionFixtures{
		GoalJSON: string(goalData),
		PlanYAML: string(planData),
	}
}

// stepNodes extracts the set of node names from plan steps.
func stepNodes(p *plan.Plan) []string {
	names := make([]string, len(p.Execution.Steps))
	for i, s := range p.Execution.Steps {
		names[i] = s.Node
	}
	return names
}

// assertPlanNodes checks that the plan contains exactly the expected step nodes (order-independent).
func assertPlanNodes(t *testing.T, p *plan.Plan, expected []string) {
	t.Helper()
	actual := stepNodes(p)
	sort.Strings(actual)
	sorted := make([]string, len(expected))
	copy(sorted, expected)
	sort.Strings(sorted)
	assert.Equal(t, sorted, actual, "plan step nodes mismatch")
}

// assertGoalStep checks that exactly one step has IsGoal=true and its node matches name.
func assertGoalStep(t *testing.T, p *plan.Plan, name string) {
	t.Helper()
	var goals []string
	for _, s := range p.Execution.Steps {
		if s.IsGoal {
			goals = append(goals, s.Node)
		}
	}
	require.Len(t, goals, 1, "expected exactly one goal step, got %v", goals)
	assert.Equal(t, name, goals[0], "goal step node mismatch")
}

// assertCleanupNodes checks that the cleanup section contains the expected nodes (order-independent).
func assertCleanupNodes(t *testing.T, p *plan.Plan, expected []string) {
	t.Helper()
	actual := make([]string, len(p.Execution.Cleanup))
	for i, c := range p.Execution.Cleanup {
		actual[i] = c.Node
	}
	sort.Strings(actual)
	sorted := make([]string, len(expected))
	copy(sorted, expected)
	sort.Strings(sorted)
	assert.Equal(t, sorted, actual, "cleanup nodes mismatch")
}

// assertStepDeps checks that a step's DependsOn contains all listed deps.
func assertStepDeps(t *testing.T, p *plan.Plan, node string, expectedDeps []string) {
	t.Helper()
	for _, s := range p.Execution.Steps {
		if s.Node == node {
			for _, dep := range expectedDeps {
				assert.Contains(t, s.DependsOn, dep,
					"step %s should depend on %s, has %v", node, dep, s.DependsOn)
			}
			return
		}
	}
	t.Errorf("step %s not found in plan", node)
}

// assertHasSelection checks that a step has at least one select-based value
// (either from+select or fromSelection).
func assertHasSelection(t *testing.T, p *plan.Plan, node string) {
	t.Helper()
	for _, s := range p.Execution.Steps {
		if s.Node == node {
			for _, v := range s.Values {
				if v.Select != nil || v.FromSelection != "" {
					return // found
				}
			}
			// Also check named selections
			if len(s.Selections) > 0 {
				return // found
			}
			t.Errorf("step %s has no selection-based values", node)
			return
		}
	}
	t.Errorf("step %s not found in plan", node)
}

// assertConstraints checks that the GoalAnalysis constraint classification matches expectations.
func assertConstraints(t *testing.T, ga *GoalAnalysis, hard, soft int, free int) {
	t.Helper()
	assert.Len(t, ga.Constraints.Hard, hard, "hard constraint count mismatch")
	assert.Len(t, ga.Constraints.Soft, soft, "soft constraint count mismatch")
	assert.Len(t, ga.Constraints.Free, free, "free constraint count mismatch")
}

// assertPlanValid verifies plan.Validate() passes.
func assertPlanValid(t *testing.T, p *plan.Plan, g *graph.Graph) {
	t.Helper()
	err := plan.Validate(p, g)
	assert.NoError(t, err, "plan validation failed")
}

// assertStepHasValue checks that a step has a value for the given input name.
func assertStepHasValue(t *testing.T, p *plan.Plan, node, inputName string) {
	t.Helper()
	for _, s := range p.Execution.Steps {
		if s.Node == node {
			_, ok := s.Values[inputName]
			assert.True(t, ok, "step %s should have value for %s", node, inputName)
			return
		}
	}
	t.Errorf("step %s not found in plan", node)
}
