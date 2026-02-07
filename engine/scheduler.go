package engine

import (
	"fmt"
	"strings"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// TopologicalSort returns plan steps in a valid execution order using Kahn's
// algorithm. Dependencies come from two sources:
//  1. Explicit step.DependsOn
//  2. Graph edges: if an edge goes from A.output → B.input and both A and B
//     are plan steps, B depends on A.
//
// Returns an error if a cycle is detected.
func TopologicalSort(steps []plan.Step, g *graph.Graph) ([]plan.Step, error) {
	// Build set of step node names
	stepSet := make(map[string]bool, len(steps))
	stepIndex := make(map[string]int, len(steps))
	for i, step := range steps {
		stepSet[step.Node] = true
		stepIndex[step.Node] = i
	}

	// Build adjacency: nodeName → set of nodes it must come after
	inDegree := make(map[string]int, len(steps))
	dependents := make(map[string][]string, len(steps)) // node → nodes that depend on it

	for _, step := range steps {
		inDegree[step.Node] = 0
	}

	// Track edges we've already counted to avoid double-counting
	edges := make(map[string]bool) // "from→to"

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

	// Source 1: explicit dependsOn
	for _, step := range steps {
		for _, dep := range step.DependsOn {
			if stepSet[dep] {
				addEdge(dep, step.Node)
			}
		}
	}

	// Source 2: graph edges between plan steps
	for _, edge := range g.Edges {
		fromNode, _, err := splitRef(edge.From)
		if err != nil {
			continue
		}
		toNode, _, err := splitRef(edge.To)
		if err != nil {
			continue
		}
		if stepSet[fromNode] && stepSet[toNode] {
			addEdge(fromNode, toNode)
		}
	}

	// Kahn's BFS
	var queue []string
	for _, step := range steps {
		if inDegree[step.Node] == 0 {
			queue = append(queue, step.Node)
		}
	}

	var sorted []plan.Step
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, steps[stepIndex[node]])

		for _, dep := range dependents[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(sorted) != len(steps) {
		// Find the nodes still with non-zero in-degree (cycle participants)
		var stuck []string
		for node, deg := range inDegree {
			if deg > 0 {
				stuck = append(stuck, node)
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
