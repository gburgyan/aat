package engine

import (
	"fmt"
	"strings"

	"github.com/gburgyan/aat/plan"
)

// TopologicalSort returns plan steps in a valid execution order using Kahn's
// algorithm, keeping to the plan's order wherever the dependencies allow.
// Dependencies come from explicit step.DependsOn references (step IDs).
//
// Returns an error if a cycle is detected.
func TopologicalSort(steps []plan.Step) ([]plan.Step, error) {
	// Build set of step IDs
	stepSet := make(map[string]bool, len(steps))
	stepIndex := make(map[string]int, len(steps))
	for i, step := range steps {
		sid := step.StepID()
		stepSet[sid] = true
		stepIndex[sid] = i
	}

	// Build adjacency: stepID → set of step IDs it must come after
	inDegree := make(map[string]int, len(steps))
	dependents := make(map[string][]string, len(steps))

	for _, step := range steps {
		inDegree[step.StepID()] = 0
	}

	// Track edges we've already counted to avoid double-counting
	edges := make(map[string]bool)

	addEdge := func(from, to string) {
		if from == to {
			return
		}
		key := from + "→" + to
		if edges[key] {
			return
		}
		edges[key] = true
		inDegree[to]++
		dependents[from] = append(dependents[from], to)
	}

	// Explicit dependsOn (step IDs)
	for _, step := range steps {
		sid := step.StepID()
		for _, dep := range step.DependsOn {
			if stepSet[dep] {
				addEdge(dep, sid)
			}
		}
	}

	// Kahn's algorithm, taking the ready step that comes first in the plan:
	// a plan whose order already respects its dependencies runs as written,
	// and steps inserted right after another (such as fuzz cases after their
	// target) run before the steps that follow it in the plan.
	var ready []int
	for i, step := range steps {
		if inDegree[step.StepID()] == 0 {
			ready = append(ready, i)
		}
	}

	var sorted []plan.Step
	for len(ready) > 0 {
		first := 0
		for j := range ready {
			if ready[j] < ready[first] {
				first = j
			}
		}
		idx := ready[first]
		ready = append(ready[:first], ready[first+1:]...)
		sid := steps[idx].StepID()
		sorted = append(sorted, steps[idx])

		for _, dep := range dependents[sid] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				ready = append(ready, stepIndex[dep])
			}
		}
	}

	if len(sorted) != len(steps) {
		var stuck []string
		for sid, deg := range inDegree {
			if deg > 0 {
				stuck = append(stuck, sid)
			}
		}
		return nil, fmt.Errorf("dependency cycle detected among steps: %s", strings.Join(stuck, ", "))
	}

	return sorted, nil
}

// splitRef splits a "node.field" reference into its components.
func splitRef(ref string) (string, string, error) {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected format \"node.field\", got %q", ref)
	}
	return parts[0], parts[1], nil
}
