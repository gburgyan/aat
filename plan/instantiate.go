package plan

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/gburgyan/aat/graph"
)

// Instantiate deep-copies a plan and merges graph-level input defaults into
// step values. It also injects implicit dependsOn entries for graph default
// from-references and translates node-name from-refs to step-ID from-refs.
// The original plan is not modified.
func Instantiate(p *Plan, g *graph.Graph) *Plan {
	return InstantiateWithLayers(p, g, nil)
}

// InstantiateWithLayers deep-copies a plan and merges input defaults into step
// values, using layered defaults (if provided) in place of graph defaults.
// The layeredDefaults map is keyed by "nodeName.inputName". When a layered
// default exists for a (node, input) pair, it takes priority over the graph
// default. When layeredDefaults is nil, this behaves identically to Instantiate.
func InstantiateWithLayers(p *Plan, g *graph.Graph, layeredDefaults map[string]*graph.InputDefault) *Plan {
	cp := prepareInstance(p, g, layeredDefaults)
	if cp == nil {
		return nil
	}
	expandMutations(cp)
	return cp
}

// prepareInstance deep-copies p and fills in what the plan leaves implicit: the
// dependencies its references imply, graph and layer defaults with their
// references bound to steps, and the steps verification defaults read. It stops
// short of expanding mutations.
func prepareInstance(p *Plan, g *graph.Graph, layeredDefaults map[string]*graph.InputDefault) *Plan {
	if p == nil || g == nil {
		return nil
	}
	cp := deepCopyPlan(p)
	InjectReferenceDeps(cp, false)
	mergeGraphDefaultsWithLayers(cp, g, layeredDefaults)
	bindVerificationDefaults(cp, g, layeredDefaults)
	return cp
}

// InstantiateAndValidate is the standard entry point for plan preparation.
// It instantiates the plan (merging graph defaults) and validates the result.
// Returns the instantiated plan on success.
func InstantiateAndValidate(p *Plan, g *graph.Graph) (*Plan, error) {
	return InstantiateAndValidateWithLayers(p, g, nil)
}

// InstantiateAndValidateWithLayers instantiates a plan with layered defaults
// and validates the result. See InstantiateWithLayers for details.
func InstantiateAndValidateWithLayers(p *Plan, g *graph.Graph, layeredDefaults map[string]*graph.InputDefault) (*Plan, error) {
	if errs := validateMutationsSyntax(p); len(errs) > 0 {
		return nil, &ValidationError{Errors: errs}
	}
	inst := prepareInstance(p, g, layeredDefaults)
	if inst == nil {
		return nil, &ValidationError{Errors: []string{"plan or graph is nil"}}
	}
	// Detect pre-existing step ids that would collide with isolated-mutation
	// clone ids produced during expansion. Raise early with a specific message
	// rather than letting the generic "duplicate step id" surface later. The
	// clones follow the prerequisite closure, which implied dependencies widen,
	// so this runs after they are added.
	if errs := detectCloneIdCollisions(inst); len(errs) > 0 {
		return nil, &ValidationError{Errors: errs}
	}
	expandMutations(inst)
	if err := Validate(inst, g); err != nil {
		return nil, err
	}
	return inst, nil
}

// detectCloneIdCollisions reports when a user's existing step id would conflict
// with a clone id that expansion would produce for an isolated mutation.
func detectCloneIdCollisions(p *Plan) []string {
	if p == nil {
		return nil
	}
	ids := make(map[string]bool, len(p.Execution.Steps))
	for _, s := range p.Execution.Steps {
		ids[s.StepID()] = true
	}
	byID := make(map[string]Step, len(p.Execution.Steps))
	for _, s := range p.Execution.Steps {
		byID[s.StepID()] = s
	}
	var errs []string
	for _, step := range p.Execution.Steps {
		if step.MutationScope != "isolated" || len(step.Mutations) == 0 {
			continue
		}
		closure := transitivePrereqClosure(step, byID)
		for _, m := range step.Mutations {
			for _, c := range closure {
				cloneID := c.StepID() + "__" + m.Name
				if ids[cloneID] {
					errs = append(errs, fmt.Sprintf(
						"isolated mutation %q on step %q: cloned step id %q collides with an existing step",
						m.Name, step.StepID(), cloneID))
				}
			}
		}
	}
	return errs
}

// mergeGraphDefaultsWithLayers merges input defaults into step values for any
// inputs the plan doesn't set, a layered default taking priority over the
// graph's, and records where each came from. A default's reference to a node
// reads the step it binds to (see DefaultRefStep), which joins the step's
// dependsOn. Every binding is decided before any dependency is added, so a value
// and its dependency always name the same step.
func mergeGraphDefaultsWithLayers(p *Plan, g *graph.Graph, layeredDefaults map[string]*graph.InputDefault) {
	steps := p.Execution.Steps
	merged := make([]map[string]StepValue, len(steps))
	for i, step := range steps {
		node, ok := g.Nodes[step.Node]
		if !ok {
			continue
		}
		merged[i] = make(map[string]StepValue)
		for _, input := range node.Inputs {
			if _, exists := step.Values[input.Name]; exists {
				continue // the plan sets it
			}
			d := EffectiveDefault(step.Node, input, layeredDefaults)
			if d == nil || !d.HasValue() {
				continue
			}
			sv := defaultStepValue(d)
			if sv.From != "" {
				sv.From = TranslateFromRefAt(sv.From, p, i)
			}
			merged[i][input.Name] = sv
		}
	}

	stepIDs := make(map[string]bool, len(steps))
	for i := range steps {
		stepIDs[steps[i].StepID()] = true
	}
	for i, values := range merged {
		if values == nil {
			continue
		}
		step := &steps[i]
		if step.Values == nil {
			step.Values = make(map[string]StepValue, len(values))
		}
		for _, name := range slices.Sorted(maps.Keys(values)) {
			sv := values[name]
			step.Values[name] = sv
			dep := splitFromNodeName(sv.From)
			if sv.From != "" && dep != step.StepID() && stepIDs[dep] && !slices.Contains(step.DependsOn, dep) {
				step.DependsOn = append(step.DependsOn, dep)
			}
		}
	}
}

// defaultStepValue converts a graph or layer default into the step value
// instantiation merges in, recording where it came from.
func defaultStepValue(d *graph.InputDefault) StepValue {
	sv := StepValueFromDefault(d)
	sv.Origin, sv.Layer = "graph", d.Layer
	if d.Layer != "" {
		sv.Origin = "layer"
	}
	return sv
}

// bindVerificationDefaults records, for each verification step, the step that
// each default reference to a node binds to (see VerificationStep.BoundDefaults):
// the last step on the node that isn't expected to fail. It runs before mutations
// expand, so a mutation's clone is never chosen.
func bindVerificationDefaults(p *Plan, g *graph.Graph, layeredDefaults map[string]*graph.InputDefault) {
	for v := range p.Execution.Verification {
		vs := &p.Execution.Verification[v]
		node, ok := g.Nodes[vs.Node]
		if !ok {
			continue
		}
		for _, input := range node.Inputs {
			if _, set := vs.Values[input.Name]; set {
				continue
			}
			d := EffectiveDefault(vs.Node, input, layeredDefaults)
			if d == nil || d.From == "" {
				continue
			}
			if vs.BoundDefaults == nil {
				vs.BoundDefaults = make(map[string]string)
			}
			vs.BoundDefaults[input.Name] = verificationRefStep(p.Execution.Steps, splitFromNodeName(d.From))
		}
	}
}

// VerificationSteps converts the plan's verification entries into executable
// steps. Each step targets the verification node, carries the declared
// assertions and values, and receives graph (or layered) defaults for the inputs
// its values leave unset. A default's reference to a node reads the step
// instantiation bound it to: the last step on the node that isn't expected to
// fail. Inputs without a value or default are left unset; the engine matches
// them by output name against earlier steps. Step IDs are "verify_<node>", suffixed with an
// ordinal when the same node is verified more than once.
func VerificationSteps(p *Plan, g *graph.Graph, layeredDefaults map[string]*graph.InputDefault) []Step {
	if p == nil || g == nil || len(p.Execution.Verification) == 0 {
		return nil
	}
	steps := make([]Step, 0, len(p.Execution.Verification))
	seen := make(map[string]int)
	for _, vs := range p.Execution.Verification {
		seen[vs.Node]++
		id := "verify_" + vs.Node
		if seen[vs.Node] > 1 {
			id = fmt.Sprintf("%s_%d", id, seen[vs.Node])
		}
		step := Step{
			ID:          id,
			Node:        vs.Node,
			Description: vs.Purpose,
			Assertions:  vs.Assertions,
			Repeat:      vs.Repeat.Clone(),
			Values:      make(map[string]StepValue, len(vs.Values)),
		}
		for name, sv := range vs.Values {
			step.Values[name] = deepCopyStepValue(sv)
		}
		if node, ok := g.Nodes[vs.Node]; ok {
			for _, input := range node.Inputs {
				if _, set := step.Values[input.Name]; set {
					continue
				}
				effectiveDefault := EffectiveDefault(vs.Node, input, layeredDefaults)
				if effectiveDefault == nil || !effectiveDefault.HasValue() {
					continue
				}
				sv := defaultStepValue(effectiveDefault)
				if sv.From != "" {
					nodeName := splitFromNodeName(sv.From)
					stepID, bound := vs.BoundDefaults[input.Name]
					if !bound {
						stepID = verificationRefStep(p.Execution.Steps, nodeName)
					}
					sv.From = stepID + sv.From[len(nodeName):]
				}
				step.Values[input.Name] = sv
			}
		}
		steps = append(steps, step)
	}
	return steps
}

// EffectiveDefault returns the default of a node's input after layers: the
// layered default for nodeName.inputName when layeredDefaults has one, and the
// graph default otherwise. layeredDefaults is keyed as graph.ApplyLayers keys
// it, and may be nil.
func EffectiveDefault(nodeName string, input graph.Input, layeredDefaults map[string]*graph.InputDefault) *graph.InputDefault {
	if ld, ok := layeredDefaults[nodeName+"."+input.Name]; ok {
		return ld
	}
	return input.Default
}

// StepValueFromDefault converts a graph InputDefault to a plan StepValue.
func StepValueFromDefault(d *graph.InputDefault) StepValue {
	sv := StepValue{}

	if d.Value != nil {
		sv.Default = d.Value
	}

	if len(d.Pool) > 0 {
		sv.Pool = make([]any, len(d.Pool))
		copy(sv.Pool, d.Pool)
	}

	if d.PoolStrategy != nil {
		s := *d.PoolStrategy
		sv.PoolStrategy = &s
	}

	if d.Constraint != "" {
		sv.Constraint = d.Constraint
	}

	if d.From != "" {
		sv.From = d.From
	}

	if d.FromResolved != "" {
		sv.FromResolved = d.FromResolved
	}

	if d.Select != nil {
		sv.Select = &SelectionConfig{
			Strategy:  d.Select.Strategy,
			Field:     d.Select.Field,
			Filter:    d.Select.Filter,
			Index:     d.Select.Index,
			SortField: d.Select.SortField,
			OnTie:     d.Select.OnTie,
		}
	}

	return sv
}

// splitFromNodeName extracts the node name from a "node.field" reference.
func splitFromNodeName(ref string) string {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// expandMutations replaces each step carrying Mutations with the original step
// followed by one sibling step per mutation.
//
// In the default "shared" scope, siblings share the parent's dependsOn,
// selections, and values (with Set overrides applied) and reference the same
// prereq-chain execution as the parent. In "isolated" scope, each mutation
// additionally receives a deep clone of the transitive prereq closure so that
// stateful APIs can be exercised without leakage between the happy path and
// the mutations or between sibling mutations.
//
// Steps without mutations (or slot markers) pass through unchanged.
func expandMutations(p *Plan) {
	if p == nil {
		return
	}
	hasMutations := false
	for _, s := range p.Execution.Steps {
		if len(s.Mutations) > 0 {
			hasMutations = true
			break
		}
	}
	if !hasMutations {
		return
	}

	// Pre-index steps by id for closure computation under isolated scope.
	byID := make(map[string]Step, len(p.Execution.Steps))
	for _, s := range p.Execution.Steps {
		byID[s.StepID()] = s
	}

	expanded := make([]Step, 0, len(p.Execution.Steps))
	for _, step := range p.Execution.Steps {
		if step.IsSlotMarker() || len(step.Mutations) == 0 {
			expanded = append(expanded, step)
			continue
		}

		parentID := step.StepID()
		muts := step.Mutations
		isolated := step.MutationScope == "isolated"

		// Parent step emitted as the happy path; mutations are cleared so
		// validation and execution don't re-expand.
		parent := step
		parent.Mutations = nil
		parent.MutationScope = ""
		expanded = append(expanded, parent)

		var closure []Step
		if isolated {
			closure = transitivePrereqClosure(step, byID)
		}

		for _, m := range muts {
			suffix := "__" + m.Name

			var idMap map[string]string
			if isolated {
				clones := cloneClosureWithSuffix(closure, suffix)
				idMap = make(map[string]string, len(clones))
				for i, orig := range closure {
					idMap[orig.StepID()] = clones[i].StepID()
				}
				expanded = append(expanded, clones...)
			}

			child := deepCopyStep(step)
			child.Mutations = nil
			child.MutationScope = ""
			child.IsGoal = false
			child.Assertions = nil
			child.Repeat = nil
			child.ID = parentID + "--" + m.Name
			for k, v := range m.Set {
				child.Values[k] = StepValue{Default: v}
			}
			child.RawBody = m.RawBody
			child.ExpectFailure = &ExpectFailure{
				Status:      append([]int(nil), m.ExpectStatus...),
				Description: m.Description,
			}
			if isolated && len(idMap) > 0 {
				rewriteStepRefs(&child, idMap)
			}
			expanded = append(expanded, child)
		}
	}

	p.Execution.Steps = expanded
}

// transitivePrereqClosure walks dependsOn in reverse BFS from the given step
// and returns the prereqs in topological order (deepest ancestors first, so
// that the caller can append the closure and the resulting plan is valid).
// The starting step itself is not included. Missing dependsOn targets (e.g.
// dangling refs that Validate will later flag) are skipped silently — this
// function's job is expansion, not validation.
func transitivePrereqClosure(start Step, byID map[string]Step) []Step {
	visited := make(map[string]bool)
	var ordered []string

	var visit func(id string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		s, ok := byID[id]
		if !ok {
			return
		}
		for _, dep := range s.DependsOn {
			visit(dep)
		}
		ordered = append(ordered, id)
	}
	for _, dep := range start.DependsOn {
		visit(dep)
	}

	out := make([]Step, 0, len(ordered))
	for _, id := range ordered {
		out = append(out, byID[id])
	}
	return out
}

// cloneClosureWithSuffix deep-copies each step in the closure, appends suffix
// to its id, and rewrites any outbound step-id reference that points at
// another step in the closure. The closure must be topologically ordered so
// that the id map is complete for each step's refs — which transitivePrereqClosure
// guarantees.
func cloneClosureWithSuffix(closure []Step, suffix string) []Step {
	idMap := make(map[string]string, len(closure))
	for _, s := range closure {
		idMap[s.StepID()] = s.StepID() + suffix
	}

	clones := make([]Step, len(closure))
	for i, s := range closure {
		c := deepCopyStep(s)
		c.ID = s.StepID() + suffix
		c.IsGoal = false
		rewriteStepRefs(&c, idMap)
		clones[i] = c
	}
	return clones
}

// rewriteStepRefs mutates s in place, replacing any step-id reference whose
// old id appears as a key in idMap. Covers: DependsOn entries, Values' From
// and FromInput (which use "stepId.field" format), Selections' From, and the
// {{step.output}} expressions in selection filters, assertions, and repeat
// conditions. FromSelection (selection-name local) and FromResolved
// (intra-step) are not step-id references and are left untouched.
func rewriteStepRefs(s *Step, idMap map[string]string) {
	for i, dep := range s.DependsOn {
		if newID, ok := idMap[dep]; ok {
			s.DependsOn[i] = newID
		}
	}
	for name, sv := range s.Values {
		changed := false
		if sv.From != "" {
			if rewritten, ok := rewriteQualifiedRef(sv.From, idMap); ok {
				sv.From = rewritten
				changed = true
			}
		}
		if sv.FromInput != "" {
			if rewritten, ok := rewriteQualifiedRef(sv.FromInput, idMap); ok {
				sv.FromInput = rewritten
				changed = true
			}
		}
		if sv.Select != nil {
			if filter := RewriteExprRefs(sv.Select.Filter, idMap); filter != sv.Select.Filter {
				sel := *sv.Select // another step may share the select block
				sel.Filter = filter
				sv.Select = &sel
				changed = true
			}
		}
		if changed {
			s.Values[name] = sv
		}
	}
	for name, sel := range s.Selections {
		changed := false
		if sel.From != "" {
			if rewritten, ok := rewriteQualifiedRef(sel.From, idMap); ok {
				sel.From = rewritten
				changed = true
			}
		}
		if filter := RewriteExprRefs(sel.Filter, idMap); filter != sel.Filter {
			sel.Filter = filter
			changed = true
		}
		if changed {
			s.Selections[name] = sel
		}
	}
	s.Assertions = RewriteAssertionRefs(s.Assertions, idMap)
	s.Repeat = RewriteRepeatRefs(s.Repeat, idMap)
}

// rewriteQualifiedRef splits a "stepId.field" reference and replaces the
// step-id portion if it appears in idMap. Returns the (possibly rewritten)
// ref and true when any change was made.
func rewriteQualifiedRef(ref string, idMap map[string]string) (string, bool) {
	idx := strings.Index(ref, ".")
	if idx < 0 {
		// Bare id with no field — still rewrite if it matches.
		if newID, ok := idMap[ref]; ok {
			return newID, true
		}
		return ref, false
	}
	oldID := ref[:idx]
	if newID, ok := idMap[oldID]; ok {
		return newID + ref[idx:], true
	}
	return ref, false
}

// StripMutations clears the Mutations and MutationScope fields on every step,
// so that a subsequent Instantiate produces only happy-path steps. Intended
// for smoke-test runs where authors want to exercise the prereq chain plus
// the terminal step without running the declared negative variants. Safe on
// plans that have no mutations (no-op).
func StripMutations(p *Plan) {
	if p == nil {
		return
	}
	for i := range p.Execution.Steps {
		p.Execution.Steps[i].Mutations = nil
		p.Execution.Steps[i].MutationScope = ""
	}
}

// validateMutationsSyntax checks the pre-expansion mutation declarations on
// every step. Call before Instantiate (which consumes and clears Mutations).
func validateMutationsSyntax(p *Plan) []string {
	if p == nil {
		return nil
	}
	var errs []string
	for i, step := range p.Execution.Steps {
		if len(step.Mutations) == 0 {
			if step.MutationScope != "" {
				errs = append(errs, fmt.Sprintf("step %d (%s): mutationScope is set but step has no mutations", i, step.StepID()))
			}
			continue
		}
		sid := step.StepID()
		switch step.MutationScope {
		case "", "shared", "isolated":
			// ok
		default:
			errs = append(errs, fmt.Sprintf("step %d (%s): unknown mutationScope %q (expected \"shared\" or \"isolated\")", i, sid, step.MutationScope))
		}
		seen := make(map[string]bool, len(step.Mutations))
		for j, m := range step.Mutations {
			switch {
			case m.Name == "":
				errs = append(errs, fmt.Sprintf("step %d (%s): mutation %d has empty name", i, sid, j))
			case seen[m.Name]:
				errs = append(errs, fmt.Sprintf("step %d (%s): duplicate mutation name %q", i, sid, m.Name))
			default:
				seen[m.Name] = true
			}
			if len(m.Set) == 0 && m.RawBody == "" {
				errs = append(errs, fmt.Sprintf("step %d (%s): mutation %q must declare at least one of set or rawBody", i, sid, m.Name))
			}
			if len(m.ExpectStatus) == 0 {
				errs = append(errs, fmt.Sprintf("step %d (%s): mutation %q must declare at least one expectStatus", i, sid, m.Name))
			}
			for _, code := range m.ExpectStatus {
				if code < 400 {
					errs = append(errs, fmt.Sprintf("step %d (%s): mutation %q expectStatus %d must be >= 400", i, sid, m.Name, code))
				}
			}
		}
	}
	return errs
}

// deepCopyPlan creates a deep copy of a Plan, including all slices, maps,
// and pointer fields, so that mutations to the copy don't affect the original.
func deepCopyPlan(p *Plan) *Plan {
	cp := *p

	// Deep-copy headers map
	if p.Headers != nil {
		cp.Headers = make(map[string]string, len(p.Headers))
		for k, v := range p.Headers {
			cp.Headers[k] = v
		}
	}

	// Deep-copy intent constraints
	if p.Intent.Constraints != nil {
		cpc := *p.Intent.Constraints
		if len(p.Intent.Constraints.Hard) > 0 {
			cpc.Hard = make([]Constraint, len(p.Intent.Constraints.Hard))
			copy(cpc.Hard, p.Intent.Constraints.Hard)
		}
		if len(p.Intent.Constraints.Soft) > 0 {
			cpc.Soft = make([]Constraint, len(p.Intent.Constraints.Soft))
			copy(cpc.Soft, p.Intent.Constraints.Soft)
		}
		if len(p.Intent.Constraints.Free) > 0 {
			cpc.Free = make([]string, len(p.Intent.Constraints.Free))
			copy(cpc.Free, p.Intent.Constraints.Free)
		}
		cp.Intent.Constraints = &cpc
	}

	// Deep-copy execution steps
	if len(p.Execution.Steps) > 0 {
		cp.Execution.Steps = make([]Step, len(p.Execution.Steps))
		for i, s := range p.Execution.Steps {
			cp.Execution.Steps[i] = deepCopyStep(s)
		}
	}

	// Deep-copy verification steps
	if len(p.Execution.Verification) > 0 {
		cp.Execution.Verification = make([]VerificationStep, len(p.Execution.Verification))
		for i, vs := range p.Execution.Verification {
			cvs := vs
			if vs.Values != nil {
				cvs.Values = make(map[string]StepValue, len(vs.Values))
				for k, v := range vs.Values {
					cvs.Values[k] = deepCopyStepValue(v)
				}
			}
			cvs.BoundDefaults = maps.Clone(vs.BoundDefaults)
			cp.Execution.Verification[i] = cvs
		}
	}

	// Deep-copy cleanup steps
	if len(p.Execution.Cleanup) > 0 {
		cp.Execution.Cleanup = make([]CleanupStep, len(p.Execution.Cleanup))
		copy(cp.Execution.Cleanup, p.Execution.Cleanup)
	}

	return &cp
}

// deepCopyStep creates a deep copy of a Step.
func deepCopyStep(s Step) Step {
	cp := s

	if len(s.DependsOn) > 0 {
		cp.DependsOn = make([]string, len(s.DependsOn))
		copy(cp.DependsOn, s.DependsOn)
	}

	if s.Values != nil {
		cp.Values = make(map[string]StepValue, len(s.Values))
		for k, v := range s.Values {
			cp.Values[k] = deepCopyStepValue(v)
		}
	}

	if s.Selections != nil {
		cp.Selections = make(map[string]StepSelection, len(s.Selections))
		for k, v := range s.Selections {
			cp.Selections[k] = v
		}
	}

	cp.Repeat = s.Repeat.Clone()

	if len(s.Mutations) > 0 {
		cp.Mutations = make([]Mutation, len(s.Mutations))
		for i, m := range s.Mutations {
			cm := m
			if len(m.Set) > 0 {
				cm.Set = make(map[string]any, len(m.Set))
				for k, v := range m.Set {
					cm.Set[k] = v
				}
			}
			if len(m.ExpectStatus) > 0 {
				cm.ExpectStatus = append([]int(nil), m.ExpectStatus...)
			}
			cp.Mutations[i] = cm
		}
	}

	return cp
}

// deepCopyStepValue creates a deep copy of a StepValue.
func deepCopyStepValue(sv StepValue) StepValue {
	cp := sv

	if len(sv.Pool) > 0 {
		cp.Pool = make([]any, len(sv.Pool))
		copy(cp.Pool, sv.Pool)
	}

	if sv.PoolStrategy != nil {
		s := *sv.PoolStrategy
		cp.PoolStrategy = &s
	}

	if sv.Select != nil {
		sel := *sv.Select
		cp.Select = &sel
	}

	return cp
}
