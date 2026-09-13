package graph

import (
	"fmt"
	"strings"
)

// RequiredFromOptionalDefaults warns about a required input whose graph default
// takes from: an optional output. When that output is missing, the step fails
// on the required input. Marking the input optional leaves it out instead.
func RequiredFromOptionalDefaults(g *Graph) []string {
	var warnings []string
	for _, name := range sortedKeys(g.Nodes) {
		for _, in := range g.Nodes[name].Inputs {
			if in.Optional || in.Default == nil || in.Default.From == "" {
				continue
			}
			if OptionalOutput(g, in.Default.From) {
				warnings = append(warnings, fmt.Sprintf("node %q: required input %q defaults from %s, an optional output; when it's missing the step fails (mark the input optional to leave it out, or give it a value)", name, in.Name, in.Default.From))
			}
		}
	}
	return warnings
}

// OptionalOutput reports whether ref, written node.output or
// node.output.field, names an output its node declares optional.
func OptionalOutput(g *Graph, ref string) bool {
	nodeName, rest, ok := strings.Cut(ref, ".")
	if !ok {
		return false
	}
	outputName, _, _ := strings.Cut(rest, ".")
	n := g.Nodes[nodeName]
	if n == nil {
		return false
	}
	for _, out := range n.Outputs {
		if out.Name == outputName {
			return out.Optional
		}
	}
	return false
}
