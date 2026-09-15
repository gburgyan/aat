package plan

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/gburgyan/aat/graph"
)

// DefaultRefStep returns the ID of the step that a graph or layer default's
// reference to node binds to, for the step at index i of steps. It is the
// nearest step before i on node that isn't expected to fail and doesn't depend
// on steps[i]. When there is none, it is the first step on node, not expected to
// fail, that doesn't depend on steps[i], which may be steps[i] itself; failing
// that, the first step on node. It returns node when no step runs it.
func DefaultRefStep(steps []Step, i int, node string) string {
	consumer := steps[i].StepID()
	for j := i - 1; j >= 0; j-- {
		if steps[j].Node == node && steps[j].ExpectFailure == nil && !dependsOnStep(steps, steps[j].StepID(), consumer) {
			return steps[j].StepID()
		}
	}
	first := ""
	for j := range steps {
		if steps[j].Node != node {
			continue
		}
		id := steps[j].StepID()
		if first == "" {
			first = id
		}
		if steps[j].ExpectFailure == nil && (id == consumer || !dependsOnStep(steps, id, consumer)) {
			return id
		}
	}
	if first != "" {
		return first
	}
	return node
}

// TranslateFromRefAt translates a default's "node.field" reference, for the step
// at index i of p, into a reference to the step it binds to (see
// DefaultRefStep), keeping the field.
func TranslateFromRefAt(fromRef string, p *Plan, i int) string {
	nodeName := splitFromNodeName(fromRef)
	if nodeName == "" || p == nil || i < 0 || i >= len(p.Execution.Steps) {
		return fromRef
	}
	return DefaultRefStep(p.Execution.Steps, i, nodeName) + fromRef[len(nodeName):]
}

// verificationRefStep returns the ID of the step that a verification step's
// default reference to node binds to: the last step on node that isn't expected
// to fail, else the last step on node, else node.
func verificationRefStep(steps []Step, node string) string {
	last := ""
	for j := len(steps) - 1; j >= 0; j-- {
		if steps[j].Node != node {
			continue
		}
		if steps[j].ExpectFailure == nil {
			return steps[j].StepID()
		}
		if last == "" {
			last = steps[j].StepID()
		}
	}
	if last != "" {
		return last
	}
	return node
}

// NearestProducer returns the ID of the last step before steps[i] whose node
// produces output and that isn't expected to fail, passing over any step that
// depends, directly or through other steps, on steps[i]. It returns "" when no
// step qualifies.
func NearestProducer(steps []Step, i int, output string, g *graph.Graph) string {
	consumer := steps[i].StepID()
	for j := i - 1; j >= 0; j-- {
		if steps[j].ExpectFailure != nil {
			continue
		}
		node := g.Nodes[steps[j].Node]
		if node == nil || !producesOutput(node, output) {
			continue
		}
		if src := steps[j].StepID(); !dependsOnStep(steps, src, consumer) {
			return src
		}
	}
	return ""
}

// producesOutput reports whether node declares an output named name.
func producesOutput(node *graph.Node, name string) bool {
	for _, out := range node.Outputs {
		if out.Name == name {
			return true
		}
	}
	return false
}

// dependsOnStep reports whether the step with ID from depends, directly or
// through other steps, on the step with ID to.
func dependsOnStep(steps []Step, from, to string) bool {
	deps := make(map[string][]string, len(steps))
	for j := range steps {
		deps[steps[j].StepID()] = steps[j].DependsOn
	}
	seen := map[string]bool{from: true}
	pending := []string{from}
	for len(pending) > 0 {
		cur := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, dep := range deps[cur] {
			if dep == to {
				return true
			}
			if !seen[dep] {
				seen[dep] = true
				pending = append(pending, dep)
			}
		}
	}
	return false
}

// InjectReferenceDeps adds to each step's dependsOn the steps its values and
// named selections read through from, fromInput, or a selection's from, and
// those its assertions and repeat condition read through {{step.output}}, so a
// reference implies its dependency. A reference to the step itself is left
// alone, and so is one to a step the plan doesn't have, which validation
// reports. With externalOnly, only references to steps outside the plan are
// added instead: an addon's steps take those before they are spliced into the
// plan that holds the steps.
func InjectReferenceDeps(p *Plan, externalOnly bool) {
	stepIDs := make(map[string]bool, len(p.Execution.Steps))
	for i := range p.Execution.Steps {
		stepIDs[p.Execution.Steps[i].StepID()] = true
	}
	for i := range p.Execution.Steps {
		step := &p.Execution.Steps[i]
		sid := step.StepID()
		for _, ref := range referencedSteps(*step) {
			if ref == sid || slices.Contains(step.DependsOn, ref) || stepIDs[ref] == externalOnly {
				continue
			}
			step.DependsOn = append(step.DependsOn, ref)
		}
	}
}

// referencedSteps returns the IDs of the steps that a step's values, named
// selections, and {{step.output}} references read, each once, in value name
// order, then selection name order, then the order the references appear.
func referencedSteps(step Step) []string {
	var refs []string
	add := func(ref string) {
		if id := splitFromNodeName(ref); id != "" && !slices.Contains(refs, id) {
			refs = append(refs, id)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(step.Values)) {
		sv := step.Values[name]
		if sv.From != "" {
			add(sv.From)
		}
		if sv.FromInput != "" {
			add(sv.FromInput)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(step.Selections)) {
		if from := step.Selections[name].From; from != "" {
			add(from)
		}
	}
	for _, ref := range append(StepOutputRefs(step.Assertions, step.Repeat), FilterOutputRefs(step)...) {
		if !slices.Contains(refs, ref.Step) {
			refs = append(refs, ref.Step)
		}
	}
	return refs
}

// FilterOutputRefs returns the {{step.output}} references in the filters of a
// step's value selections, in value name order, then in the filters of its
// named selections, in selection name order.
func FilterOutputRefs(step Step) []OutputRef {
	var refs []OutputRef
	for _, name := range slices.Sorted(maps.Keys(step.Values)) {
		if sel := step.Values[name].Select; sel != nil {
			refs = append(refs, PredicateOutputRefs(sel.Filter)...)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(step.Selections)) {
		refs = append(refs, PredicateOutputRefs(step.Selections[name].Filter)...)
	}
	return refs
}

// referenceNote says which of a step's values and selections read dep, for a
// cycle message, since a reference implies its dependency. It is "" when none
// does.
func referenceNote(step Step, dep string) string {
	var names []string
	for _, name := range slices.Sorted(maps.Keys(step.Values)) {
		sv := step.Values[name]
		if (sv.From != "" && splitFromNodeName(sv.From) == dep) || (sv.FromInput != "" && splitFromNodeName(sv.FromInput) == dep) {
			names = append(names, fmt.Sprintf("value %q", name))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(step.Selections)) {
		if from := step.Selections[name].From; from != "" && splitFromNodeName(from) == dep {
			names = append(names, fmt.Sprintf("selection %q", name))
		}
	}
	readsDep := func(r OutputRef) bool { return r.Step == dep }
	if step.Assertions != nil {
		for j, a := range step.Assertions.Mechanical {
			if slices.ContainsFunc(AssertionOutputRefs(a), readsDep) {
				names = append(names, fmt.Sprintf("assertion %d", j))
			}
		}
	}
	if step.Repeat != nil && slices.ContainsFunc(PredicateOutputRefs(step.Repeat.Until), readsDep) {
		names = append(names, "repeat.until")
	}
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf(" (%s reads %q, which implies the dependency)", strings.Join(names, " and "), dep)
}
