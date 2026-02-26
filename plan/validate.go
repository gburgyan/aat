package plan

import (
	"fmt"
	"strings"

	"github.com/gburgyan/aat/config"
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

	// Validate plan-level auth if present
	if p.Auth != nil {
		for _, e := range config.ValidateAuth(p.Auth) {
			errs = append(errs, fmt.Sprintf("plan auth: %s", e))
		}
	}

	// Build step ID set for uniqueness and reference validation.
	// stepIDs: stepID → true (for dependency and reference checks)
	// stepIDToNode: stepID → graph node name (for output validation)
	stepIDs := make(map[string]bool, len(p.Execution.Steps))
	stepIDToNode := make(map[string]string, len(p.Execution.Steps))
	for _, step := range p.Execution.Steps {
		sid := step.StepID()
		if stepIDs[sid] {
			errs = append(errs, fmt.Sprintf("duplicate step id %q", sid))
		}
		stepIDs[sid] = true
		stepIDToNode[sid] = step.Node
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

	for i, step := range p.Execution.Steps {
		// Check node exists in graph
		node, nodeExists := g.Nodes[step.Node]
		if !nodeExists {
			errs = append(errs, fmt.Sprintf("step %d: node %q not found in graph", i, step.Node))
			continue
		}

		sid := step.StepID()

		// Check dependsOn references valid plan steps (by step ID)
		depsSet := make(map[string]bool, len(step.DependsOn))
		for _, dep := range step.DependsOn {
			depsSet[dep] = true
			if !stepIDs[dep] {
				errs = append(errs, fmt.Sprintf("step %d (%s): dependsOn references unknown step %q", i, sid, dep))
			}
			if dep == sid {
				errs = append(errs, fmt.Sprintf("step %d (%s): dependsOn references itself", i, sid))
			}
		}

		// Build input name set and index for this node
		inputNames := make(map[string]bool, len(node.Inputs))
		inputIndex := make(map[string]int, len(node.Inputs))
		for idx, input := range node.Inputs {
			inputNames[input.Name] = true
			inputIndex[input.Name] = idx
		}

		// Check that required inputs have plan values.
		// After instantiation, graph defaults are in step.Values.
		for _, input := range node.Inputs {
			if input.Optional {
				continue
			}
			_, hasPlanValue := step.Values[input.Name]
			if !hasPlanValue {
				errs = append(errs, fmt.Sprintf("step %d (%s): required input %q has no plan value", i, sid, input.Name))
			}
		}

		// Validate step.Selections (named selections) — from uses step IDs
		for selName, sel := range step.Selections {
			if sel.From == "" {
				errs = append(errs, fmt.Sprintf("step %d (%s): selection %q has empty 'from'", i, sid, selName))
				continue
			}
			srcStepID, srcField, err := splitRef(sel.From)
			if err != nil {
				errs = append(errs, fmt.Sprintf("step %d (%s): selection %q has invalid 'from' reference %q: %v", i, sid, selName, sel.From, err))
				continue
			}
			if !stepIDs[srcStepID] {
				errs = append(errs, fmt.Sprintf("step %d (%s): selection %q references unknown step %q", i, sid, selName, srcStepID))
			} else {
				// From implies dependsOn
				if !depsSet[srcStepID] {
					errs = append(errs, fmt.Sprintf("step %d (%s): selection %q references %q but does not list it in dependsOn", i, sid, selName, srcStepID))
				}
			}
			// Resolve step ID → graph node for output validation
			srcGraphNode := srcStepID
			if gn, ok := stepIDToNode[srcStepID]; ok {
				srcGraphNode = gn
			}
			if outs, ok := outputsByNode[srcGraphNode]; ok {
				if out, outExists := outs[srcField]; !outExists {
					errs = append(errs, fmt.Sprintf("step %d (%s): selection %q references output %q which does not exist on node %q", i, sid, selName, srcField, srcGraphNode))
				} else {
					// Must be an array type
					ft, ftErr := graph.ParseFieldType(out.Type)
					if ftErr == nil && !ft.IsArray {
						errs = append(errs, fmt.Sprintf("step %d (%s): selection %q references output %q which is not an array type", i, sid, selName, sel.From))
					}
				}
			}
		}

		// Gap 6: Check value names match node inputs
		for name := range step.Values {
			if !inputNames[name] {
				errs = append(errs, fmt.Sprintf("step %d (%s): value %q does not match any input on node %q", i, sid, name, step.Node))
			}
		}

		// Per-value validation: From references, array selection, sortField, dependsOn completeness
		for name, sv := range step.Values {
			// Validate FromSelection
			if sv.FromSelection != "" {
				if sv.From != "" || sv.Select != nil {
					errs = append(errs, fmt.Sprintf("step %d (%s): value %q has fromSelection but also has from/select \u2014 these are mutually exclusive", i, sid, name))
				}
				selName, fieldName := ParseFromSelection(sv.FromSelection)
				if _, exists := step.Selections[selName]; !exists {
					errs = append(errs, fmt.Sprintf("step %d (%s): value %q references unknown selection %q", i, sid, name, selName))
				}

				// Validate fieldName against source output's elementFields
				if fieldName != "" {
					if sel, selExists := step.Selections[selName]; selExists && sel.From != "" {
						srcStepID, srcField, refErr := splitRef(sel.From)
						srcGraphNode := srcStepID
						if gn, ok := stepIDToNode[srcStepID]; ok {
							srcGraphNode = gn
						}
						if refErr == nil {
							if outs, ok := outputsByNode[srcGraphNode]; ok {
								if out, outExists := outs[srcField]; outExists && len(out.ElementFields) > 0 {
									found := false
									for _, ef := range out.ElementFields {
										if ef.Name == fieldName {
											found = true
											break
										}
									}
									if !found {
										errs = append(errs, fmt.Sprintf(
											"step %d (%s): fromSelection %q references field %q which is not an elementField of %s.%s",
											i, sid, sv.FromSelection, fieldName, srcStepID, srcField))
									}
								}
							}
						}
					}
				}
			}

			// Validate FromResolved: intra-step value reference
			if sv.FromResolved != "" {
				// Mutual exclusion: fromResolved cannot coexist with from, fromSelection, default, or pool
				if sv.From != "" || sv.FromSelection != "" || sv.Default != nil || len(sv.Pool) > 0 {
					errs = append(errs, fmt.Sprintf("step %d (%s): value %q has fromResolved but also has from/fromSelection/default/pool — these are mutually exclusive with fromResolved", i, sid, name))
				}
				// Referenced input must exist on the same graph node
				if !inputNames[sv.FromResolved] {
					errs = append(errs, fmt.Sprintf("step %d (%s): value %q fromResolved references %q which is not an input on node %q", i, sid, name, sv.FromResolved, step.Node))
				} else {
					// Ordering: referenced input must appear before the current input in node.Inputs
					refIdx, refExists := inputIndex[sv.FromResolved]
					curIdx, curExists := inputIndex[name]
					if refExists && curExists && refIdx >= curIdx {
						errs = append(errs, fmt.Sprintf("step %d (%s): value %q fromResolved references %q which is not defined before it in node inputs (forward reference)", i, sid, name, sv.FromResolved))
					}
				}
			}

			// Validate FromInput: cross-step input reference
			if sv.FromInput != "" {
				// Mutual exclusion: fromInput cannot coexist with from, fromSelection, fromResolved, default, or pool
				if sv.From != "" || sv.FromSelection != "" || sv.FromResolved != "" || sv.Default != nil || len(sv.Pool) > 0 {
					errs = append(errs, fmt.Sprintf("step %d (%s): value %q has fromInput but also has from/fromSelection/fromResolved/default/pool — these are mutually exclusive with fromInput", i, sid, name))
				}
				srcStepID, srcInputName, err := splitRef(sv.FromInput)
				if err != nil {
					errs = append(errs, fmt.Sprintf("step %d (%s): invalid 'fromInput' reference %q for %q: %v", i, sid, sv.FromInput, name, err))
				} else {
					// Step existence
					if !stepIDs[srcStepID] {
						errs = append(errs, fmt.Sprintf("step %d (%s): 'fromInput' reference %q for %q: %q is not a step in this plan", i, sid, sv.FromInput, name, srcStepID))
					} else {
						// DependsOn required
						if !depsSet[srcStepID] {
							errs = append(errs, fmt.Sprintf("step %d (%s): has 'fromInput' reference to %q but does not list it in dependsOn", i, sid, srcStepID))
						}
						// Input existence on source step's graph node
						srcGraphNode := srcStepID
						if gn, ok := stepIDToNode[srcStepID]; ok {
							srcGraphNode = gn
						}
						if srcNode, ok := g.Nodes[srcGraphNode]; ok {
							found := false
							for _, inp := range srcNode.Inputs {
								if inp.Name == srcInputName {
									found = true
									break
								}
							}
							if !found {
								errs = append(errs, fmt.Sprintf("step %d (%s): 'fromInput' reference %q for %q: input %q does not exist on node %q", i, sid, sv.FromInput, name, srcInputName, srcGraphNode))
							}
						}
					}
				}
			}

			// Gap 1: Validate From field references (from uses step IDs)
			if sv.From != "" {
				srcStepID, srcField, err := splitRef(sv.From)
				if err != nil {
					errs = append(errs, fmt.Sprintf("step %d (%s): invalid 'from' reference %q for %q: %v", i, sid, sv.From, name, err))
				} else {
					srcGraphNode := srcStepID
					if gn, ok := stepIDToNode[srcStepID]; ok {
						srcGraphNode = gn
					}
					if !stepIDs[srcStepID] {
						errs = append(errs, fmt.Sprintf("step %d (%s): 'from' reference %q for %q: %q is not a step in this plan", i, sid, sv.From, name, srcStepID))
					} else if outs, ok := outputsByNode[srcGraphNode]; ok {
						if _, outExists := outs[srcField]; !outExists {
							errs = append(errs, fmt.Sprintf("step %d (%s): 'from' reference %q for %q: output %q does not exist on node %q", i, sid, sv.From, name, srcField, srcGraphNode))
						}
					}

					// Gap 9: From implies dependsOn
					if stepIDs[srcStepID] && !depsSet[srcStepID] {
						errs = append(errs, fmt.Sprintf("step %d (%s): has 'from' reference to %q but does not list it in dependsOn", i, sid, srcStepID))
					}
				}
			}

			// Gap 2 & 3: Array selection validation
			if sv.Select != nil {
				var sourceOutput *graph.Output

				if sv.From != "" {
					srcStepID, srcField, err := splitRef(sv.From)
					srcGraphNode := srcStepID
					if gn, ok := stepIDToNode[srcStepID]; ok {
						srcGraphNode = gn
					}
					if err == nil {
						if outs, ok := outputsByNode[srcGraphNode]; ok {
							if out, outExists := outs[srcField]; outExists {
								sourceOutput = &out
								ft, ftErr := graph.ParseFieldType(out.Type)
								if ftErr == nil && !ft.IsArray {
									errs = append(errs, fmt.Sprintf("step %d (%s): selection on %q references 'from' output %q which is not an array type", i, sid, name, sv.From))
								}
							}
						}
					}
				} else {
					// Selection requires a 'from' reference
					errs = append(errs, fmt.Sprintf("step %d (%s): selection on %q has no 'from' reference", i, sid, name))
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
							errs = append(errs, fmt.Sprintf("step %d (%s): sortField %q for %q not found in elementFields of %q", i, sid, sel.SortField, name, sourceOutput.Name))
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

	// Validate predicate expressions and selection strategies in step selections and values
	for i, step := range p.Execution.Steps {
		sid := step.StepID()

		// Validate named selection strategies and filter fields
		for selName, sel := range step.Selections {
			strategy := sel.Strategy
			if !validStrategies[strategy] {
				errs = append(errs, fmt.Sprintf("step %d (%s): unknown selection strategy %q for selection %q", i, sid, strategy, selName))
			}
			if sel.Filter != "" {
				if err := ValidatePredicate(sel.Filter); err != nil {
					errs = append(errs, fmt.Sprintf("step %d (%s): invalid filter expression for selection %q: %v", i, sid, selName, err))
				}
			}
			if strategy == "min" || strategy == "max" {
				if sel.SortField == "" {
					errs = append(errs, fmt.Sprintf("step %d (%s): %s strategy requires sortField for selection %q", i, sid, strategy, selName))
				}
			}
			if strategy == "match" && sel.Filter == "" {
				errs = append(errs, fmt.Sprintf("step %d (%s): match strategy requires filter for selection %q", i, sid, selName))
			}
			if strategy == "index" && sel.Index < 0 {
				errs = append(errs, fmt.Sprintf("step %d (%s): index strategy requires non-negative index for selection %q", i, sid, selName))
			}
			// Validate filter field references against source output's elementFields
			if sel.Filter != "" && sel.From != "" {
				srcStepID, srcField, refErr := splitRef(sel.From)
				srcGraphNode := srcStepID
				if gn, ok := stepIDToNode[srcStepID]; ok {
					srcGraphNode = gn
				}
				if refErr == nil {
					if outs, ok := outputsByNode[srcGraphNode]; ok {
						if out, outExists := outs[srcField]; outExists && len(out.ElementFields) > 0 {
							for _, field := range PredicateFields(sel.Filter) {
								found := false
								for _, ef := range out.ElementFields {
									if ef.Name == field {
										found = true
										break
									}
								}
								if !found {
									errs = append(errs, fmt.Sprintf(
										"step %d (%s): filter for selection %q references field %q which is not an elementField of %s.%s",
										i, sid, selName, field, srcStepID, srcField))
								}
							}
						}
					}
				}
			}
		}

		for name, sv := range step.Values {
			if sv.Select != nil {
				sel := sv.Select
				if !validStrategies[sel.Strategy] {
					errs = append(errs, fmt.Sprintf("step %d (%s): unknown selection strategy %q for %q", i, sid, sel.Strategy, name))
				}
				if sel.Filter != "" {
					if err := ValidatePredicate(sel.Filter); err != nil {
						errs = append(errs, fmt.Sprintf("step %d (%s): invalid filter expression for %q: %v", i, sid, name, err))
					}
				}
				if sel.Strategy == "min" || sel.Strategy == "max" {
					if sel.Field == "" && sel.SortField == "" {
						errs = append(errs, fmt.Sprintf("step %d (%s): %s strategy requires field or sortField for %q", i, sid, sel.Strategy, name))
					}
				}
				if sel.Strategy == "match" && sel.Filter == "" {
					errs = append(errs, fmt.Sprintf("step %d (%s): match strategy requires filter for %q", i, sid, name))
				}
				if sel.Strategy == "index" && sel.Index < 0 {
					errs = append(errs, fmt.Sprintf("step %d (%s): index strategy requires non-negative index for %q", i, sid, name))
				}
			}
			if sv.Constraint != "" {
				if err := ValidatePredicate(sv.Constraint); err != nil {
					errs = append(errs, fmt.Sprintf("step %d (%s): invalid constraint expression for %q: %v", i, sid, name, err))
				}
			}
		}
		if step.Assertions != nil {
			for j, ma := range step.Assertions.Mechanical {
				if ma.Type == "predicate" && ma.Expr != "" {
					if err := ValidatePredicate(ma.Expr); err != nil {
						errs = append(errs, fmt.Sprintf("step %d (%s): invalid predicate assertion %d: %v", i, sid, j, err))
					}
				}
			}
		}

		// Validate expectFailure
		if step.ExpectFailure != nil {
			if len(step.ExpectFailure.Status) == 0 {
				errs = append(errs, fmt.Sprintf("step %d (%s): expectFailure must have at least one status code", i, sid))
			}
			for _, code := range step.ExpectFailure.Status {
				if code < 400 {
					errs = append(errs, fmt.Sprintf("step %d (%s): expectFailure status %d must be >= 400", i, sid, code))
				}
			}
			// Check for contradicting status assertion
			if step.Assertions != nil {
				for _, ma := range step.Assertions.Mechanical {
					if ma.Type == "status" {
						if expectInt, ok := toInt(ma.Expect); ok && expectInt < 400 {
							errs = append(errs, fmt.Sprintf("step %d (%s): status assertion expecting %d contradicts expectFailure", i, sid, expectInt))
						}
					}
				}
			}
		}
	}

	// Gap 4: Validate constraint AppliesTo references
	// AppliesTo entries may be "stepID" or "stepID.input" — extract the step ID part.
	if p.Intent.Constraints != nil {
		for _, c := range p.Intent.Constraints.Hard {
			for _, ref := range c.AppliesTo {
				stepRef := ref
				if idx := strings.Index(ref, "."); idx > 0 {
					stepRef = ref[:idx]
				}
				if !stepIDs[stepRef] {
					errs = append(errs, fmt.Sprintf("hard constraint %q: appliesTo references unknown step %q", c.Name, ref))
				}
			}
		}
		for _, c := range p.Intent.Constraints.Soft {
			for _, ref := range c.AppliesTo {
				stepRef := ref
				if idx := strings.Index(ref, "."); idx > 0 {
					stepRef = ref[:idx]
				}
				if !stepIDs[stepRef] {
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

	// Gap 8: Goal consistency validation (uses step IDs)
	if p.Intent.Goal != "" {
		if !stepIDs[p.Intent.Goal] {
			errs = append(errs, fmt.Sprintf("intent goal references unknown step %q", p.Intent.Goal))
		}
	}
	var goalSteps []string
	for _, step := range p.Execution.Steps {
		if step.IsGoal {
			goalSteps = append(goalSteps, step.StepID())
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
// Uses step IDs for cycle detection to support step aliasing.
func detectDependsOnCycles(p *Plan) []string {
	// Build adjacency: stepID → dependsOn step IDs
	adj := make(map[string][]string)
	for _, step := range p.Execution.Steps {
		adj[step.StepID()] = step.DependsOn
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
		sid := step.StepID()
		if color[sid] == white {
			dfs(sid)
		}
	}

	return cycles
}

// toInt converts a value to int if possible. Handles int, float64 (JSON numbers).
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	}
	return 0, false
}

// splitRef splits a "node.field" reference into its components.
func splitRef(ref string) (string, string, error) {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected format \"node.field\", got %q", ref)
	}
	return parts[0], parts[1], nil
}
