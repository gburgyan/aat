package graph

import "sort"

// PoolRefUse is one poolRef in a project file: where it is, and the domain
// pool it names.
type PoolRefUse struct {
	Where string // such as "node addItem input quantity" or "layer eu: origin"
	Ref   string
}

// PoolRefs lists the poolRefs of the graph's input defaults, sorted by where
// they are.
func (g *Graph) PoolRefs() []PoolRefUse {
	var uses []PoolRefUse
	for name, node := range g.Nodes {
		for _, in := range node.Inputs {
			if in.Default != nil && in.Default.PoolRef != "" {
				uses = append(uses, PoolRefUse{Where: "node " + name + " input " + in.Name, Ref: in.Default.PoolRef})
			}
		}
	}
	sortPoolRefs(uses)
	return uses
}

// PoolRefs lists the poolRefs of the layer's inputs, sorted by where they are.
func (l *Layer) PoolRefs() []PoolRefUse {
	var uses []PoolRefUse
	for key, d := range l.Inputs {
		if d != nil && d.PoolRef != "" {
			uses = append(uses, PoolRefUse{Where: "layer " + l.Name + ": " + key, Ref: d.PoolRef})
		}
	}
	sortPoolRefs(uses)
	return uses
}

func sortPoolRefs(uses []PoolRefUse) {
	sort.Slice(uses, func(i, j int) bool { return uses[i].Where < uses[j].Where })
}
