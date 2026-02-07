package plan

import (
	"fmt"
	"strings"

	"github.com/gburgyan/aat/graph"
)

// ValidationError collects all validation errors for a plan.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("plan validation failed:\n  - %s", strings.Join(e.Errors, "\n  - "))
}

// Validate checks a plan against a graph for structural correctness.
// It returns a *ValidationError collecting all problems found, or nil if valid.
func Validate(p *Plan, g *graph.Graph) error {
	var errs []string

	// Build a set of step node names for dependsOn validation
	stepNodes := make(map[string]bool, len(p.Execution.Steps))
	for _, step := range p.Execution.Steps {
		if stepNodes[step.Node] {
			errs = append(errs, fmt.Sprintf("duplicate step node %q", step.Node))
		}
		stepNodes[step.Node] = true
	}

	// Build edge lookup: targetNode.targetInput → sourceNode.sourceOutput
	edgeTargets := make(map[string]bool) // "node.input" that are wired by edges
	for _, edge := range g.Edges {
		edgeTargets[edge.To] = true
	}

	for i, step := range p.Execution.Steps {
		// Check node exists in graph
		node, nodeExists := g.Nodes[step.Node]
		if !nodeExists {
			errs = append(errs, fmt.Sprintf("step %d: node %q not found in graph", i, step.Node))
			continue
		}

		// Check dependsOn references valid plan steps
		for _, dep := range step.DependsOn {
			if !stepNodes[dep] {
				errs = append(errs, fmt.Sprintf("step %d (%s): dependsOn references unknown step %q", i, step.Node, dep))
			}
			if dep == step.Node {
				errs = append(errs, fmt.Sprintf("step %d (%s): dependsOn references itself", i, step.Node))
			}
		}

		// Check that required inputs without edges have plan values or defaults
		for _, input := range node.Inputs {
			if input.Optional {
				continue
			}
			edgeKey := step.Node + "." + input.Name
			hasEdge := edgeTargets[edgeKey]
			_, hasPlanValue := step.Values[input.Name]
			hasDefault := input.Default != nil

			if !hasEdge && !hasPlanValue && !hasDefault {
				errs = append(errs, fmt.Sprintf("step %d (%s): required input %q has no edge, plan value, or default", i, step.Node, input.Name))
			}
		}
	}

	// Check graphVersion compatibility if specified
	if p.Metadata.GraphVersion != "" {
		planVer, err := graph.ParseVersion(p.Metadata.GraphVersion)
		if err != nil {
			errs = append(errs, fmt.Sprintf("invalid plan graphVersion %q: %v", p.Metadata.GraphVersion, err))
		} else {
			graphVer, err := graph.ParseVersion(g.Version)
			if err != nil {
				errs = append(errs, fmt.Sprintf("invalid graph version %q: %v", g.Version, err))
			} else {
				compat := graph.CheckCompatibility(planVer, graphVer)
				if compat == graph.VersionIncompatible {
					errs = append(errs, fmt.Sprintf("plan graphVersion %q is incompatible with graph version %q (different major)", p.Metadata.GraphVersion, g.Version))
				}
			}
		}
	}

	// Check for dependsOn cycles
	if cycleErrs := detectDependsOnCycles(p); len(cycleErrs) > 0 {
		errs = append(errs, cycleErrs...)
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// detectDependsOnCycles checks for cycles in the explicit dependsOn graph.
func detectDependsOnCycles(p *Plan) []string {
	// Build adjacency: node → dependsOn nodes
	adj := make(map[string][]string)
	for _, step := range p.Execution.Steps {
		adj[step.Node] = step.DependsOn
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)

	color := make(map[string]int)
	var cycles []string

	var dfs func(node string) bool
	dfs = func(node string) bool {
		color[node] = gray
		for _, dep := range adj[node] {
			if color[dep] == gray {
				cycles = append(cycles, fmt.Sprintf("dependsOn cycle detected involving %q and %q", node, dep))
				return true
			}
			if color[dep] == white {
				if dfs(dep) {
					return true
				}
			}
		}
		color[node] = black
		return false
	}

	for _, step := range p.Execution.Steps {
		if color[step.Node] == white {
			dfs(step.Node)
		}
	}

	return cycles
}
