package graph

// ComputeAutoWireEdges generates edges for matching output→input name pairs
// when AutoWire is enabled on the graph. Generated edges are appended to
// g.Edges with AutoWired=true. Explicit edges take precedence: if any explicit
// edge targets a given "node.input", no auto-wire edge is created for that input.
// Array outputs are skipped (they require select edges with explicit configuration).
//
// In AutoWireSources mode, pass-through outputs (where the same node also has an
// input with the same field name) are skipped, so only true source nodes produce edges.
// In AutoWireAll mode, all matching outputs are used (full Cartesian product).
func ComputeAutoWireEdges(g *Graph) {
	if !g.AutoWire.IsEnabled() {
		return
	}

	// Build index: output field name → list of "node.output" refs (non-array only)
	outputsByName := make(map[string][]string)
	for name, node := range g.Nodes {
		for _, out := range node.Outputs {
			if out.Name == "" {
				continue
			}
			ft, err := ParseFieldType(out.Type)
			if err != nil || ft.IsArray {
				continue // skip array outputs — they need select edges
			}

			// In sources mode, skip pass-through outputs (node also inputs this field)
			if g.AutoWire == AutoWireSources {
				isPassThrough := false
				for _, inp := range node.Inputs {
					if inp.Name == out.Name {
						isPassThrough = true
						break
					}
				}
				if isPassThrough {
					continue
				}
			}

			outputsByName[out.Name] = append(outputsByName[out.Name], name+"."+out.Name)
		}
	}

	// Build set of explicit edge targets
	explicitTargets := make(map[string]bool, len(g.Edges))
	for _, edge := range g.Edges {
		explicitTargets[edge.To] = true
	}

	// For each input, look for matching outputs
	var generated []Edge
	for nodeName, node := range g.Nodes {
		for _, inp := range node.Inputs {
			if inp.Name == "" || inp.Optional || inp.NoAutoWire {
				continue
			}
			targetRef := nodeName + "." + inp.Name
			if explicitTargets[targetRef] {
				continue // explicit edge already covers this input
			}

			producers := outputsByName[inp.Name]
			for _, fromRef := range producers {
				// Don't wire a node to itself
				fromNode := fromRef[:len(fromRef)-len(inp.Name)-1]
				if fromNode == nodeName {
					continue
				}
				generated = append(generated, Edge{
					From:      fromRef,
					To:        targetRef,
					AutoWired: true,
				})
			}
		}
	}

	g.Edges = append(g.Edges, generated...)
}
