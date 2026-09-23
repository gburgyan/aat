package plan

import (
	"sort"

	"github.com/gburgyan/aat/graph"
)

// PoolRefs lists the poolRefs the plan's step values name, as
// "step ID value NAME" and the ref, sorted.
func (p *Plan) PoolRefs() []graph.PoolRefUse {
	var uses []graph.PoolRefUse
	for _, s := range p.Execution.Steps {
		for name, sv := range s.Values {
			if sv.PoolRef != "" {
				uses = append(uses, graph.PoolRefUse{Where: "step " + s.StepID() + " value " + name, Ref: sv.PoolRef})
			}
		}
	}
	sort.Slice(uses, func(i, j int) bool { return uses[i].Where < uses[j].Where })
	return uses
}
