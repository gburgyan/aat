package plan

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/internal/httpstatus"
	"github.com/gburgyan/aat/internal/predicate"
)

// RetryCategories lists the error category names accepted in retry.on and
// retry.failOn. The engine's ErrorCategory names must stay in sync with this list.
var RetryCategories = []string{"transient", "client", "auth", "server", "adapter", "network", "timeout", "response_error"}

// ValidRetryRule reports whether a retry rule is a known category name or an
// HTTP status code in the range 100-599.
func ValidRetryRule(rule string) bool {
	rule = strings.TrimSpace(rule)
	if code, err := strconv.Atoi(rule); err == nil {
		return code >= 100 && code <= 599
	}
	for _, c := range RetryCategories {
		if strings.EqualFold(rule, c) {
			return true
		}
	}
	return false
}

// validateRetryConfig returns validation errors for a step's retry block.
func validateRetryConfig(prefix string, rc *RetryConfig) []string {
	if rc == nil {
		return nil
	}
	var errs []string
	if rc.Max < 0 {
		errs = append(errs, fmt.Sprintf("%s: retry.max must be >= 0", prefix))
	}
	for _, r := range rc.On {
		if !ValidRetryRule(r) {
			errs = append(errs, fmt.Sprintf("%s: retry.on has unknown rule %q (use a category: %s, or an HTTP status code)", prefix, r, strings.Join(RetryCategories, ", ")))
		}
	}
	for _, r := range rc.FailOn {
		if !ValidRetryRule(r) {
			errs = append(errs, fmt.Sprintf("%s: retry.failOn has unknown rule %q (use a category: %s, or an HTTP status code)", prefix, r, strings.Join(RetryCategories, ", ")))
		}
	}
	return errs
}

// validateRepeatConfig checks a step's repeat block against its node: until is
// a predicate that reads only the node's outputs, next maps the node's inputs
// to its cursor outputs, collect names list or number outputs, the limits are
// in range, and the step is a read that expects to succeed.
func validateRepeatConfig(prefix string, rc *RepeatConfig, expectsFailure bool, node *graph.Node) []string {
	if rc == nil {
		return nil
	}
	outputs := make(map[string]graph.Output, len(node.Outputs))
	for _, out := range node.Outputs {
		outputs[out.Name] = out
	}
	outputList := strings.Join(slices.Sorted(maps.Keys(outputs)), ", ")

	var errs []string
	if strings.TrimSpace(rc.Until) == "" {
		if len(rc.Next) == 0 {
			errs = append(errs, fmt.Sprintf("%s: repeat.until is required unless repeat.next is set: a predicate over each response's outputs that ends the repeats, such as status == \"complete\"", prefix))
		}
	} else {
		if err := predicate.Validate(rc.Until); err != nil {
			errs = append(errs, fmt.Sprintf("%s: invalid repeat.until %q: %v", prefix, rc.Until, err))
		} else if err := ValidatePredicateExprs(rc.Until); err != nil {
			errs = append(errs, fmt.Sprintf("%s: invalid expression in repeat.until %q: %v", prefix, rc.Until, err))
		}
		for _, field := range predicate.Fields(rc.Until) {
			name, _, _ := strings.Cut(field, ".")
			if _, ok := outputs[name]; !ok {
				errs = append(errs, fmt.Sprintf("%s: repeat.until reads %q, which is not an output of %s (outputs: %s)", prefix, name, node.Name, outputList))
			}
		}
	}
	if len(rc.Next) > 0 {
		inputs := make(map[string]bool, len(node.Inputs))
		for _, in := range node.Inputs {
			inputs[in.Name] = true
		}
		inputList := strings.Join(slices.Sorted(maps.Keys(inputs)), ", ")
		for _, input := range slices.Sorted(maps.Keys(rc.Next)) {
			if !inputs[input] {
				errs = append(errs, fmt.Sprintf("%s: repeat.next sets %q, which is not an input of %s (inputs: %s)", prefix, input, node.Name, inputList))
			}
			name := rc.Next[input]
			out, ok := outputs[name]
			switch {
			case !ok:
				errs = append(errs, fmt.Sprintf("%s: repeat.next reads %q, which is not an output of %s (outputs: %s)", prefix, name, node.Name, outputList))
			case !cursorType(out.Type):
				errs = append(errs, fmt.Sprintf("%s: repeat.next can't send %q, a %s output: a cursor is a string or integer output, such as nextCursor: {path: meta.after, default: \"\"}", prefix, name, out.Type))
			}
		}
	}
	for _, name := range rc.Collect {
		out, ok := outputs[name]
		switch {
		case !ok:
			errs = append(errs, fmt.Sprintf("%s: repeat.collect names %q, which is not an output of %s (outputs: %s)", prefix, name, node.Name, outputList))
		case !collectable(out.Type):
			errs = append(errs, fmt.Sprintf("%s: repeat.collect can't gather %q, a %s output: it appends lists and adds integers and floats", prefix, name, out.Type))
		}
	}
	if rc.Max < 0 || rc.Max > MaxRepeatRequests {
		errs = append(errs, fmt.Sprintf("%s: repeat.max must be from 1 to %d, or left out for %d", prefix, MaxRepeatRequests, DefaultRepeatMax))
	}
	interval, intervalErr := rc.IntervalDuration()
	if intervalErr != nil {
		errs = append(errs, fmt.Sprintf("%s: %v", prefix, intervalErr))
	}
	timeout, timeoutErr := rc.TimeoutDuration()
	if timeoutErr != nil {
		errs = append(errs, fmt.Sprintf("%s: %v", prefix, timeoutErr))
	}
	if intervalErr == nil && timeoutErr == nil && timeout > 0 && timeout < interval {
		errs = append(errs, fmt.Sprintf("%s: repeat.timeout %s is shorter than repeat.interval %s, so the step could send only one request", prefix, timeout, interval))
	}
	if expectsFailure {
		errs = append(errs, fmt.Sprintf("%s: repeat can't be combined with expectFailure: it waits for a response that succeeds", prefix))
	}
	if !node.Cleanup.IsZero() {
		errs = append(errs, fmt.Sprintf("%s: repeat is for reads, but %s has a cleanup pairing (%s), so each request could create a resource", prefix, node.Name, node.Cleanup))
	}
	return errs
}

// collectable reports whether repeat.collect can gather an output of type typ:
// a list, whose items are appended, or an integer or float, whose values are
// added.
func collectable(typ string) bool {
	ft, err := graph.ParseFieldType(typ)
	if err != nil {
		return false
	}
	return ft.IsArray || (ft.Kind == graph.TypeScalar && (ft.Name == "integer" || ft.Name == "float"))
}

// cursorType reports whether repeat.next can send an output of type typ as a
// cursor: a single string or integer.
func cursorType(typ string) bool {
	ft, err := graph.ParseFieldType(typ)
	if err != nil {
		return false
	}
	return !ft.IsArray && ft.Kind == graph.TypeScalar && (ft.Name == "string" || ft.Name == "integer")
}

// ValidationError collects all validation errors for a plan.
type ValidationError struct {
	Errors []string
}

// Error lists each distinct problem once, in the order found.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("plan validation failed:\n  - %s", strings.Join(distinctErrors(e.Errors), "\n  - "))
}

// AssertionTypes returns the mechanical assertion types the engine evaluates, in
// the order documentation lists them.
func AssertionTypes() []string {
	return []string{"status", "fieldExists", "fieldEquals", "predicate", "schema"}
}

// IsAssertionType reports whether t names a mechanical assertion type.
func IsAssertionType(t string) bool {
	return slices.Contains(AssertionTypes(), t)
}

// validateAssertions checks a step's mechanical assertions: each type exists,
// and the expressions in a predicate and in a fieldEquals value parse. prefix
// names the step in each message.
func validateAssertions(prefix string, assertions *Assertions) []string {
	if assertions == nil {
		return nil
	}
	var errs []string
	for j, ma := range assertions.Mechanical {
		switch {
		case !IsAssertionType(ma.Type):
			errs = append(errs, fmt.Sprintf("%s: assertion %d has unknown type %q (use %s)", prefix, j, ma.Type, strings.Join(AssertionTypes(), ", ")))
		case ma.Type == "predicate" && ma.Expr != "":
			if err := predicate.Validate(ma.Expr); err != nil {
				errs = append(errs, fmt.Sprintf("%s: invalid predicate assertion %d: %v", prefix, j, err))
			} else if err := ValidatePredicateExprs(ma.Expr); err != nil {
				errs = append(errs, fmt.Sprintf("%s: invalid expression in predicate assertion %d: %v", prefix, j, err))
			}
		case ma.Type == "fieldEquals":
			if err := ValidateExprValue(ma.Value); err != nil {
				errs = append(errs, fmt.Sprintf("%s: invalid expression in fieldEquals assertion %d: %v", prefix, j, err))
			}
		}
	}
	return errs
}

// valueOutputRefError says why a step value can't read a {{step.output}}
// reference, and what to write instead.
func valueOutputRefError(ref OutputRef) string {
	return fmt.Sprintf("%s reads a step's output, which only assertions and repeat.until can; use from: %s.%s", ref, ref.Step, ref.Output)
}

// outputRefScope checks the {{step.output}} references in a plan's assertions
// and repeat conditions against the plan's main steps and their nodes.
type outputRefScope struct {
	steps        map[string]*Step // main steps by ID
	verification map[string]bool  // verification step IDs, such as verify_getOrder
	graph        *graph.Graph
}

func newOutputRefScope(p *Plan, g *graph.Graph) outputRefScope {
	sc := outputRefScope{steps: map[string]*Step{}, verification: map[string]bool{}, graph: g}
	for i := range p.Execution.Steps {
		sc.steps[p.Execution.Steps[i].StepID()] = &p.Execution.Steps[i]
	}
	for _, vs := range p.Execution.Verification {
		sc.verification["verify_"+vs.Node] = true
	}
	return sc
}

// check describes what is wrong with ref, read by the step stepID ("" for a
// verification step), or returns "". The description starts with the
// punctuation that joins it to "reads {{step.output}}".
func (sc outputRefScope) check(ref OutputRef, stepID string) string {
	if stepID != "" && ref.Step == stepID {
		return fmt.Sprintf(", the step's own output: name it directly, as %s", ref.Output)
	}
	step, ok := sc.steps[ref.Step]
	if !ok {
		if stepID == "" && sc.verification[ref.Step] {
			return ": a verification step reads main steps only"
		}
		return fmt.Sprintf(": %q is not a step in this plan", ref.Step)
	}
	if step.ExpectFailure != nil {
		return fmt.Sprintf(": %s expects failure, so it stores no outputs", ref.Step)
	}
	if sc.graph == nil || sc.graph.Nodes[step.Node] == nil {
		return "" // a missing node is reported on its own step
	}
	node := sc.graph.Nodes[step.Node]
	for _, out := range node.Outputs {
		if out.Name != ref.Output {
			continue
		}
		if ft, err := graph.ParseFieldType(out.Type); err == nil && ft.IsArray {
			return fmt.Sprintf(", a %s output: an assertion compares a string, number, or boolean", out.Type)
		}
		return ""
	}
	return fmt.Sprintf(": output %q does not exist on node %s", ref.Output, step.Node)
}

// validateOutputRefs checks the {{step.output}} references in a step's
// assertions and repeat condition. stepID is the main step that reads them, or
// "" for a verification step.
func (sc outputRefScope) validateOutputRefs(prefix, stepID string, assertions *Assertions, repeat *RepeatConfig) []string {
	var errs []string
	if assertions != nil {
		for j, a := range assertions.Mechanical {
			for _, ref := range AssertionOutputRefs(a) {
				if msg := sc.check(ref, stepID); msg != "" {
					errs = append(errs, fmt.Sprintf("%s: assertion %d reads %s%s", prefix, j, ref, msg))
				}
			}
		}
	}
	if repeat != nil {
		for _, ref := range PredicateOutputRefs(repeat.Until) {
			if msg := sc.check(ref, stepID); msg != "" {
				errs = append(errs, fmt.Sprintf("%s: repeat.until reads %s%s", prefix, ref, msg))
			}
		}
	}
	return errs
}

// onTieError describes what is wrong with a selection's onTie, or returns "".
// onTie takes first or fail, and only the min and max strategies can tie.
func onTieError(onTie, strategy string) string {
	switch {
	case onTie == "":
		return ""
	case onTie != "first" && onTie != "fail":
		return fmt.Sprintf("unknown onTie %q (use first or fail)", onTie)
	case strategy != "min" && strategy != "max":
		if strategy == "" {
			strategy = "first"
		}
		return fmt.Sprintf("onTie applies only to the min and max strategies, not %q", strategy)
	default:
		return ""
	}
}

// distinctErrors returns the messages without repeats, keeping the first
// occurrence of each.
func distinctErrors(errs []string) []string {
	seen := make(map[string]bool, len(errs))
	out := make([]string, 0, len(errs))
	for _, msg := range errs {
		if !seen[msg] {
			seen[msg] = true
			out = append(out, msg)
		}
	}
	return out
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
	refScope := newOutputRefScope(p, g)
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

		// Check dependsOn references valid plan steps (by step ID). A reference
		// adds the dependency it implies at instantiation (InjectReferenceDeps).
		for _, dep := range step.DependsOn {
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

		// Composition leaves an AUTOWIRE marker only when no step produces the
		// output, and the marker is never a value to send.
		for _, name := range unresolvedAutowireInputs(step.Values) {
			errs = append(errs, fmt.Sprintf("step %d (%s): input %q is an unresolved AUTOWIRE: no step before it produces an output named %q; wire it or set it in a recipe override", i, sid, name, name))
		}

		// Validate step.Selections (named selections) — from uses step IDs
		for _, selName := range slices.Sorted(maps.Keys(step.Selections)) {
			sel := step.Selections[selName]
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
		for _, name := range slices.Sorted(maps.Keys(step.Values)) {
			if !inputNames[name] {
				errs = append(errs, fmt.Sprintf("step %d (%s): value %q does not match any input on node %q", i, sid, name, step.Node))
			}
		}

		// Literal values and pool entries fit their input's type. An AUTOWIRE
		// marker is reported on its own, and a step expected to fail, such as a
		// mutation, may send a wrong shape on purpose.
		for _, in := range node.Inputs {
			sv, ok := step.Values[in.Name]
			marker, _ := AutowireMarker(sv)
			if !ok || marker || step.ExpectFailure != nil {
				continue
			}
			if msg := graph.DefaultShapeError(&graph.InputDefault{Value: sv.Default, Pool: sv.Pool}, in.Type); msg != "" {
				errs = append(errs, fmt.Sprintf("step %d (%s): value %q: %s", i, sid, in.Name, msg))
			}
		}

		// Per-value validation: From references, array selection, sortField, dependsOn completeness
		for _, name := range slices.Sorted(maps.Keys(step.Values)) {
			sv := step.Values[name]
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

	// Validate predicate expressions and selection strategies in step selections and values
	for i, step := range p.Execution.Steps {
		sid := step.StepID()

		// Validate named selection strategies and filter fields
		for _, selName := range slices.Sorted(maps.Keys(step.Selections)) {
			sel := step.Selections[selName]
			strategy := sel.Strategy
			if !IsSelectionStrategy(strategy) {
				errs = append(errs, fmt.Sprintf("step %d (%s): unknown selection strategy %q for selection %q", i, sid, strategy, selName))
			}
			if sel.Filter != "" {
				if err := predicate.Validate(sel.Filter); err != nil {
					errs = append(errs, fmt.Sprintf("step %d (%s): invalid filter expression for selection %q: %v", i, sid, selName, err))
				}
			}
			if strategy == "min" || strategy == "max" {
				if sel.SortField == "" {
					errs = append(errs, fmt.Sprintf("step %d (%s): %s strategy requires sortField for selection %q", i, sid, strategy, selName))
				}
			}
			if msg := onTieError(sel.OnTie, strategy); msg != "" {
				errs = append(errs, fmt.Sprintf("step %d (%s): selection %q: %s", i, sid, selName, msg))
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
							for _, field := range predicate.Fields(sel.Filter) {
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

		for _, name := range slices.Sorted(maps.Keys(step.Values)) {
			sv := step.Values[name]
			if sv.Select != nil {
				sel := sv.Select
				if !IsSelectionStrategy(sel.Strategy) {
					errs = append(errs, fmt.Sprintf("step %d (%s): unknown selection strategy %q for %q", i, sid, sel.Strategy, name))
				}
				if sel.Filter != "" {
					if err := predicate.Validate(sel.Filter); err != nil {
						errs = append(errs, fmt.Sprintf("step %d (%s): invalid filter expression for %q: %v", i, sid, name, err))
					}
				}
				if sel.Strategy == "min" || sel.Strategy == "max" {
					if sel.Field == "" && sel.SortField == "" {
						errs = append(errs, fmt.Sprintf("step %d (%s): %s strategy requires field or sortField for %q", i, sid, sel.Strategy, name))
					}
				}
				if msg := onTieError(sel.OnTie, sel.Strategy); msg != "" {
					errs = append(errs, fmt.Sprintf("step %d (%s): select for %q: %s", i, sid, name, msg))
				}
				if sel.Strategy == "match" && sel.Filter == "" {
					errs = append(errs, fmt.Sprintf("step %d (%s): match strategy requires filter for %q", i, sid, name))
				}
				if sel.Strategy == "index" && sel.Index < 0 {
					errs = append(errs, fmt.Sprintf("step %d (%s): index strategy requires non-negative index for %q", i, sid, name))
				}
			}
			if sv.Constraint != "" {
				if err := predicate.Validate(sv.Constraint); err != nil {
					errs = append(errs, fmt.Sprintf("step %d (%s): invalid constraint expression for %q: %v", i, sid, name, err))
				}
			}
			if err := ValidateExprValue(sv.Default); err != nil {
				errs = append(errs, fmt.Sprintf("step %d (%s): invalid expression for %q: %v", i, sid, name, err))
			} else if refs := ExprValueOutputRefs(sv.Default); len(refs) > 0 {
				errs = append(errs, fmt.Sprintf("step %d (%s): invalid expression for %q: %s", i, sid, name, valueOutputRefError(refs[0])))
			}
			for k, entry := range sv.Pool {
				if err := ValidateExprValue(entry); err != nil {
					errs = append(errs, fmt.Sprintf("step %d (%s): invalid expression in pool entry %d for %q: %v", i, sid, k, name, err))
				} else if refs := ExprValueOutputRefs(entry); len(refs) > 0 {
					errs = append(errs, fmt.Sprintf("step %d (%s): invalid expression in pool entry %d for %q: %s", i, sid, k, name, valueOutputRefError(refs[0])))
				}
			}
		}
		errs = append(errs, validateAssertions(fmt.Sprintf("step %d (%s)", i, sid), step.Assertions)...)
		errs = append(errs, refScope.validateOutputRefs(fmt.Sprintf("step %d (%s)", i, sid), sid, step.Assertions, step.Repeat)...)

		// Validate expectFailure
		errs = append(errs, validateRetryConfig(fmt.Sprintf("step %d (%s)", i, step.StepID()), step.Retry)...)
		if node, ok := g.Nodes[step.Node]; ok {
			errs = append(errs, validateRepeatConfig(fmt.Sprintf("step %d (%s)", i, sid), step.Repeat, step.ExpectFailure != nil, node)...)
		}

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
					if ma.Type == "status" && httpstatus.ContradictsFailure(ma.Expect) {
						errs = append(errs, fmt.Sprintf("step %d (%s): status assertion expecting %v contradicts expectFailure", i, sid, ma.Expect))
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
		node, exists := g.Nodes[vs.Node]
		if !exists {
			errs = append(errs, fmt.Sprintf("verification step %d: node %q not found in graph", i, vs.Node))
		}
		where := fmt.Sprintf("verification step %d (%s)", i, vs.Node)
		errs = append(errs, validateAssertions(where, vs.Assertions)...)
		errs = append(errs, refScope.validateOutputRefs(where, "", vs.Assertions, vs.Repeat)...)
		if exists {
			errs = append(errs, validateVerificationValues(where, vs.Node, node, vs.Values, stepIDToNode, g)...)
			errs = append(errs, validateRepeatConfig(where, vs.Repeat, false, node)...)
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

// validateVerificationValues checks a verification step's values: each names an
// input of its node, a from or fromInput reference names a main step and what
// that step's node declares, and a literal fits its input's shape. A
// verification step has no selections, so fromSelection is an error.
func validateVerificationValues(where, nodeName string, node *graph.Node, values map[string]StepValue, stepIDToNode map[string]string, g *graph.Graph) []string {
	var errs []string
	inputs := make(map[string]graph.Input, len(node.Inputs))
	for _, in := range node.Inputs {
		inputs[in.Name] = in
	}
	for _, name := range slices.Sorted(maps.Keys(values)) {
		sv := values[name]
		in, ok := inputs[name]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s: value %q does not match any input on node %q", where, name, nodeName))
			continue
		}
		if sv.FromSelection != "" {
			errs = append(errs, fmt.Sprintf("%s: value %q uses fromSelection, but a verification step has no selections; use from with select", where, name))
		}
		for _, ref := range []struct{ kind, value string }{{"from", sv.From}, {"fromInput", sv.FromInput}} {
			if ref.value == "" {
				continue
			}
			srcStep, field, err := splitRef(ref.value)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: invalid '%s' reference %q for %q: %v", where, ref.kind, ref.value, name, err))
				continue
			}
			srcNode, isStep := stepIDToNode[srcStep]
			if !isStep {
				errs = append(errs, fmt.Sprintf("%s: '%s' reference %q for %q: %q is not a step in this plan", where, ref.kind, ref.value, name, srcStep))
				continue
			}
			source := g.Nodes[srcNode]
			if source == nil {
				continue
			}
			if ref.kind == "from" && !producesOutput(source, field) {
				errs = append(errs, fmt.Sprintf("%s: 'from' reference %q for %q: output %q does not exist on node %q", where, ref.value, name, field, srcNode))
			}
			if ref.kind == "fromInput" && !slices.ContainsFunc(source.Inputs, func(in graph.Input) bool { return in.Name == field }) {
				errs = append(errs, fmt.Sprintf("%s: 'fromInput' reference %q for %q: input %q does not exist on node %q", where, ref.value, name, field, srcNode))
			}
		}
		if marker, _ := AutowireMarker(sv); !marker {
			if msg := graph.DefaultShapeError(&graph.InputDefault{Value: sv.Default, Pool: sv.Pool}, in.Type); msg != "" {
				errs = append(errs, fmt.Sprintf("%s: value %q: %s", where, name, msg))
			}
		}
	}
	return errs
}

// detectDependsOnCycles checks for cycles in the explicit dependsOn graph.
// Uses step IDs for cycle detection to support step aliasing.
func detectDependsOnCycles(p *Plan) []string {
	// Build adjacency: stepID → dependsOn step IDs
	adj := make(map[string][]string)
	byID := make(map[string]Step, len(p.Execution.Steps))
	for _, step := range p.Execution.Steps {
		adj[step.StepID()] = step.DependsOn
		byID[step.StepID()] = step
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
				cycles = append(cycles, fmt.Sprintf("dependsOn cycle detected involving %q and %q%s", node, dep, referenceNote(byID[node], dep)))
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

// splitRef splits a "node.field" reference into its components.
func splitRef(ref string) (string, string, error) {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected format \"node.field\", got %q", ref)
	}
	return parts[0], parts[1], nil
}
