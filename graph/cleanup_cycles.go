package graph

import (
	"fmt"
	"sort"
	"strings"
)

// detectCleanupCycles reports cleanup pairings that loop back on themselves,
// such as a node cleaned up by b whose cleanup is a again. Each node names at
// most one cleanup node, so following the chain from every node finds every
// cycle. A node that names itself is reported by Validate's per-node check, not
// here.
func detectCleanupCycles(g *Graph) []string {
	found := map[string]bool{}
	for start := range g.Nodes {
		var path []string
		onPath := map[string]int{}
		for cur := start; cur != ""; {
			node := g.Nodes[cur]
			if node == nil {
				break // an unknown cleanup node, reported by the per-node check
			}
			if i, ok := onPath[cur]; ok {
				if cycle := path[i:]; len(cycle) > 1 {
					found[formatCleanupCycle(cycle)] = true
				}
				break
			}
			onPath[cur] = len(path)
			path = append(path, cur)
			cur = node.Cleanup
		}
	}

	cycles := make([]string, 0, len(found))
	for c := range found {
		cycles = append(cycles, c)
	}
	sort.Strings(cycles)
	return cycles
}

// formatCleanupCycle names a cycle from its alphabetically first node, so it
// reads the same wherever the chain that found it started.
func formatCleanupCycle(cycle []string) string {
	first := 0
	for i, name := range cycle {
		if name < cycle[first] {
			first = i
		}
	}
	ordered := append(append([]string(nil), cycle[first:]...), cycle[:first]...)
	ordered = append(ordered, ordered[0])
	return fmt.Sprintf("cleanup cycle detected: %s", strings.Join(ordered, " → "))
}
