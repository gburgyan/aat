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

	// Build output lookup: node → output name → Output
	outputsByNode := make(map[string]map[string]graph.Output)
	for name, node := range g.Nodes {
		outs := make(map[string]graph.Output, len(node.Outputs))
		for _, out := range node.Outputs {
			outs[out.Name] = out
		}
		outputsByNode[name] = outs
	}

	// Build edge-by-target lookup: "node.input" → edges (for finding select edges)
	edgesByTarget := make(map[string][]graph.Edge)
	for _, edge := range g.Edges {
		edgesByTarget[edge.To] = append(edgesByTarget[edge.To], edge)
	}

	for i, step := range p.Execution.Steps {
		// Check node exists in graph
		node, nodeExists := g.Nodes[step.Node]
		if !nodeExists {
			errs = append(errs, fmt.Sprintf("step %d: node %q not found in graph", i, step.Node))
			continue
		}

		// Check dependsOn references valid plan steps
		depsSet := make(map[string]bool, len(step.DependsOn))
		for _, dep := range step.DependsOn {
			depsSet[dep] = true
			if !stepNodes[dep] {
				errs = append(errs, fmt.Sprintf("step %d (%s): dependsOn references unknown step %q", i, step.Node, dep))
			}
			if dep == step.Node {
				errs = append(errs, fmt.Sprintf("step %d (%s): dependsOn references itself", i, step.Node))
			}
		}

		// Build input name set for this node
		inputNames := make(map[string]bool, len(node.Inputs))
		for _, input := range node.Inputs {
			inputNames[input.Name] = true
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

		// Gap 6: Check value names match node inputs
		for name := range step.Values {
			if !inputNames[name] {
				errs = append(errs, fmt.Sprintf("step %d (%s): value %q does not match any input on node %q", i, step.Node, name, step.Node))
			}
		}

		// Per-value validation: From references, array selection, sortField, dependsOn completeness
		for name, sv := range step.Values {
			// Gap 1: Validate From field references
			if sv.From != "" {
				srcNode, srcField, err := splitRef(sv.From)
				if err != nil {
					errs = append(errs, fmt.Sprintf("step %d (%s): invalid 'from' reference %q for %q: %v", i, step.Node, sv.From, name, err))
				} else {
					if !stepNodes[srcNode] {
						errs = append(errs, fmt.Sprintf("step %d (%s): 'from' reference %q for %q: %q is not a step in this plan", i, step.Node, sv.From, name, srcNode))
					} else if outs, ok := outputsByNode[srcNode]; ok {
						if _, outExists := outs[srcField]; !outExists {
							errs = append(errs, fmt.Sprintf("step %d (%s): 'from' reference %q for %q: output %q does not exist on node %q", i, step.Node, sv.From, name, srcField, srcNode))
						}
					}

					// Gap 9: From implies dependsOn
					if stepNodes[srcNode] && !depsSet[srcNode] {
						errs = append(errs, fmt.Sprintf("step %d (%s): has 'from' reference to %q but does not list it in dependsOn", i, step.Node, srcNode))
					}
				}
			}

			// Gap 2 & 3: Array selection validation
			if sv.Select != nil {
				var sourceOutput *graph.Output

				if sv.From != "" {
					srcNode, srcField, err := splitRef(sv.From)
					if err == nil {
						if outs, ok := outputsByNode[srcNode]; ok {
							if out, outExists := outs[srcField]; outExists {
								sourceOutput = &out
								ft, ftErr := graph.ParseFieldType(out.Type)
								if ftErr == nil && !ft.IsArray {
									errs = append(errs, fmt.Sprintf("step %d (%s): selection on %q references 'from' output %q which is not an array type", i, step.Node, name, sv.From))
								}
							}
						}
					}
				} else {
					// No From: look for a select edge targeting this input
					targetKey := step.Node + "." + name
					edges := edgesByTarget[targetKey]
					hasSelectEdge := false
					for _, e := range edges {
						if e.Select {
							hasSelectEdge = true
							srcNode, srcField, err := splitRef(e.From)
							if err == nil {
								if outs, ok := outputsByNode[srcNode]; ok {
									if out, outExists := outs[srcField]; outExists {
										sourceOutput = &out
									}
								}
							}
							break
						}
					}
					if !hasSelectEdge {
						errs = append(errs, fmt.Sprintf("step %d (%s): selection on %q has no 'from' and no select edge in the graph", i, step.Node, name))
					}
				}

				// Gap 3: SortField validation against elementFields
				sel := sv.Select
				if (sel.Strategy == "min" || sel.Strategy == "max") && sel.SortField != "" && sourceOutput != nil {
					if len(sourceOutput.ElementFields) > 0 && !strings.Contains(sel.SortField, ".") {
						found := false
						for _, ef := range sourceOutput.ElementFields {
							if ef.Name == sel.SortField {
								found = true
								break
							}
						}
						if !found {
							errs = append(errs, fmt.Sprintf("step %d (%s): sortField %q for %q not found in elementFields of %q", i, step.Node, sel.SortField, name, sourceOutput.Name))
						}
					}
				}
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

	// Valid strategy names
	validStrategies := map[string]bool{
		"":       true,
		"first":  true,
		"last":   true,
		"index":  true,
		"random": true,
		"min":    true,
		"max":    true,
		"match":  true,
	}

	// Validate predicate expressions and selection strategies in step values and assertions
	for i, step := range p.Execution.Steps {
		for name, sv := range step.Values {
			if sv.Select != nil {
				sel := sv.Select
				if !validStrategies[sel.Strategy] {
					errs = append(errs, fmt.Sprintf("step %d (%s): unknown selection strategy %q for %q", i, step.Node, sel.Strategy, name))
				}
				if sel.Filter != "" {
					if err := ValidatePredicate(sel.Filter); err != nil {
						errs = append(errs, fmt.Sprintf("step %d (%s): invalid filter expression for %q: %v", i, step.Node, name, err))
					}
				}
				if sel.Strategy == "min" || sel.Strategy == "max" {
					if sel.Field == "" && sel.SortField == "" {
						errs = append(errs, fmt.Sprintf("step %d (%s): %s strategy requires field or sortField for %q", i, step.Node, sel.Strategy, name))
					}
				}
				if sel.Strategy == "match" && sel.Filter == "" {
					errs = append(errs, fmt.Sprintf("step %d (%s): match strategy requires filter for %q", i, step.Node, name))
				}
				if sel.Strategy == "index" && sel.Index < 0 {
					errs = append(errs, fmt.Sprintf("step %d (%s): index strategy requires non-negative index for %q", i, step.Node, name))
				}
			}
			if sv.Constraint != "" {
				if err := ValidatePredicate(sv.Constraint); err != nil {
					errs = append(errs, fmt.Sprintf("step %d (%s): invalid constraint expression for %q: %v", i, step.Node, name, err))
				}
			}
		}
		if step.Assertions != nil {
			for j, ma := range step.Assertions.Mechanical {
				if ma.Type == "predicate" && ma.Expr != "" {
					if err := ValidatePredicate(ma.Expr); err != nil {
						errs = append(errs, fmt.Sprintf("step %d (%s): invalid predicate assertion %d: %v", i, step.Node, j, err))
					}
				}
			}
		}
	}

	// Gap 4: Validate constraint AppliesTo references
	// AppliesTo entries may be "node" or "node.input" — extract the node part.
	if p.Intent.Constraints != nil {
		for _, c := range p.Intent.Constraints.Hard {
			for _, ref := range c.AppliesTo {
				nodeName := ref
				if idx := strings.Index(ref, "."); idx > 0 {
					nodeName = ref[:idx]
				}
				if !stepNodes[nodeName] {
					errs = append(errs, fmt.Sprintf("hard constraint %q: appliesTo references unknown step %q", c.Name, ref))
				}
			}
		}
		for _, c := range p.Intent.Constraints.Soft {
			for _, ref := range c.AppliesTo {
				nodeName := ref
				if idx := strings.Index(ref, "."); idx > 0 {
					nodeName = ref[:idx]
				}
				if !stepNodes[nodeName] {
					errs = append(errs, fmt.Sprintf("soft constraint %q: appliesTo references unknown step %q", c.Name, ref))
				}
			}
		}
	}

	// Gap 5: Validate cleanup steps
	validRunOn := map[string]bool{"": true, "always": true, "failure": true, "success": true}
	for i, cs := range p.Execution.Cleanup {
		if _, exists := g.Nodes[cs.Node]; !exists {
			errs = append(errs, fmt.Sprintf("cleanup step %d: node %q not found in graph", i, cs.Node))
		}
		if !validRunOn[cs.RunOn] {
			errs = append(errs, fmt.Sprintf("cleanup step %d (%s): invalid runOn value %q (must be always, failure, or success)", i, cs.Node, cs.RunOn))
		}
	}

	// Gap 5: Validate verification steps
	for i, vs := range p.Execution.Verification {
		if _, exists := g.Nodes[vs.Node]; !exists {
			errs = append(errs, fmt.Sprintf("verification step %d: node %q not found in graph", i, vs.Node))
		}
		if vs.Assertions != nil {
			for j, ma := range vs.Assertions.Mechanical {
				if ma.Type == "predicate" && ma.Expr != "" {
					if err := ValidatePredicate(ma.Expr); err != nil {
						errs = append(errs, fmt.Sprintf("verification step %d (%s): invalid predicate assertion %d: %v", i, vs.Node, j, err))
					}
				}
			}
		}
	}

	// Gap 8: Goal consistency validation
	if p.Intent.Goal != "" {
		if !stepNodes[p.Intent.Goal] {
			errs = append(errs, fmt.Sprintf("intent goal references unknown step %q", p.Intent.Goal))
		}
	}
	var goalSteps []string
	for _, step := range p.Execution.Steps {
		if step.IsGoal {
			goalSteps = append(goalSteps, step.Node)
		}
	}
	if len(goalSteps) > 1 {
		errs = append(errs, fmt.Sprintf("multiple steps marked as isGoal: %s", strings.Join(goalSteps, ", ")))
	}
	if p.Intent.Goal != "" && len(goalSteps) == 1 && goalSteps[0] != p.Intent.Goal {
		errs = append(errs, fmt.Sprintf("isGoal on step %q does not match intent goal %q", goalSteps[0], p.Intent.Goal))
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

// splitRef splits a "node.field" reference into its components.
func splitRef(ref string) (string, string, error) {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected format \"node.field\", got %q", ref)
	}
	return parts[0], parts[1], nil
}
