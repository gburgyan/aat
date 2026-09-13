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
	plan.InjectReferenceDeps(p, false)
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
			if src := plan.NearestProducer(steps, i, name, g); src != "" {
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
