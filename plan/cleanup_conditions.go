package plan

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/gburgyan/aat/graph"
)

// ValidateCleanupConditions checks each cleanup pairing's when condition: it
// parses, and every field it names is an output of the node that declares the
// pairing, since the condition reads the outputs of that node's step. It returns
// one message per problem, in node order.
func ValidateCleanupConditions(g *graph.Graph) []string {
	var errs []string
	for _, name := range slices.Sorted(maps.Keys(g.Nodes)) {
		node := g.Nodes[name]
		when := node.Cleanup.When
		if when == "" {
			continue
		}
		if err := ValidatePredicate(when); err != nil {
			errs = append(errs, fmt.Sprintf("node %q: cleanup when %q: %v", name, when, err))
			continue
		}
		outputs := make(map[string]bool, len(node.Outputs))
		for _, out := range node.Outputs {
			outputs[out.Name] = true
		}
		for _, field := range PredicateFields(when) {
			root, _, _ := strings.Cut(field, ".")
			if !outputs[root] {
				errs = append(errs, fmt.Sprintf("node %q: cleanup when %q names %q, which is not an output of %q", name, when, root, name))
			}
		}
	}
	return errs
}
