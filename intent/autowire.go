package intent

import (
	"slices"
	"sort"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// resolveRemainingAutowire wires the AUTOWIRE markers that the slot and addon
// passes left, once every step of the composed plan is in place. A base or slot
// step can then take an output that only an addon's step produces, and a base
// without slots or addons resolves its markers at all. Each remaining marker
// takes the nearest step before it that produces an output of the same name,
// passing over a step that depends on the marker's own step. An AUTOWIRE? marker
// that no step feeds becomes an empty value, which leaves the optional input
// unset; a plain AUTOWIRE stays for a recipe override or aat prompt to fill, and
// plan validation rejects it if nothing does.
func resolveRemainingAutowire(p *plan.Plan, g *graph.Graph) {
	ensureFromDeps(p, false)
	steps := p.Execution.Steps
	for i := range steps {
		step := &steps[i]
		var names []string
		for name, sv := range step.Values {
			if marker, _ := plan.AutowireMarker(sv); marker {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			_, optional := plan.AutowireMarker(step.Values[name])
			if src := nearestProducer(steps, i, name, g); src != "" {
				step.Values[name] = plan.StepValue{From: src + "." + name}
				if !slices.Contains(step.DependsOn, src) {
					step.DependsOn = append(step.DependsOn, src)
				}
				continue
			}
			if optional {
				step.Values[name] = plan.StepValue{}
			}
		}
	}
}

// nearestProducer returns the ID of the last step before steps[i] whose node
// produces output, passing over any step that depends, directly or through
// other steps, on steps[i]. It returns "" when no step qualifies.
func nearestProducer(steps []plan.Step, i int, output string, g *graph.Graph) string {
	consumer := steps[i].StepID()
	for j := i - 1; j >= 0; j-- {
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
func dependsOnStep(steps []plan.Step, from, to string) bool {
	deps := make(map[string][]string, len(steps))
	for _, s := range steps {
		deps[s.StepID()] = s.DependsOn
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
