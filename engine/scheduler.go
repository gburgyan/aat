package engine

import (
	"fmt"
	"strings"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// TopologicalSort returns plan steps in a valid execution order using Kahn's
// algorithm. Dependencies come from two sources:
//  1. Explicit step.DependsOn (references step IDs)
//  2. Graph edges: if an edge goes from A.output → B.input and both A and B
//     have exactly one step in the plan, B depends on A. When a graph node
//     appears in multiple steps, graph-edge auto-wiring is skipped (the plan
//     must use explicit dependsOn).
//
// Returns an error if a cycle is detected.
func TopologicalSort(steps []plan.Step, g *graph.Graph) ([]plan.Step, error) {
	// Build set of step IDs
	stepSet := make(map[string]bool, len(steps))
	stepIndex := make(map[string]int, len(steps))
	for i, step := range steps {
		sid := step.StepID()
		stepSet[sid] = true
		stepIndex[sid] = i
	}

	// Build node → step IDs mapping for graph edge resolution
	nodeToStepIDs := make(map[string][]string, len(steps))
	for _, step := range steps {
		nodeToStepIDs[step.Node] = append(nodeToStepIDs[step.Node], step.StepID())
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

	// Source 1: explicit dependsOn (step IDs)
	for _, step := range steps {
		sid := step.StepID()
		for _, dep := range step.DependsOn {
			if stepSet[dep] {
				addEdge(dep, sid)
			}
		}
	}

	// Source 2: graph edges between plan steps.
	// Only auto-wire when both source and target nodes appear exactly once.
	for _, edge := range g.Edges {
		fromNode, _, err := splitRef(edge.From)
		if err != nil {
			continue
		}
		toNode, _, err := splitRef(edge.To)
		if err != nil {
			continue
		}
		fromSteps := nodeToStepIDs[fromNode]
		toSteps := nodeToStepIDs[toNode]
		if len(fromSteps) == 1 && len(toSteps) == 1 {
			addEdge(fromSteps[0], toSteps[0])
		}
	}

	// Kahn's BFS
	var queue []string
	for _, step := range steps {
		sid := step.StepID()
		if inDegree[sid] == 0 {
			queue = append(queue, sid)
		}
	}

	var sorted []plan.Step
	for len(queue) > 0 {
		sid := queue[0]
		queue = queue[1:]
		sorted = append(sorted, steps[stepIndex[sid]])

		for _, dep := range dependents[sid] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
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
