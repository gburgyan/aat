package graph

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gburgyan/aat/internal/predicate"
	"github.com/gburgyan/aat/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// CleanupPairing names the node that releases what a node creates. In YAML it is
// the node's name, or a mapping that also says when the cleanup is not needed:
//
//	cleanup: voidPayment
//
//	cleanup:
//	  node: voidPayment
//	  when: 'status == "authorized"'
//	  releasedBy: [capturePayment]
type CleanupPairing struct {
	// Node is the cleanup node.
	Node string `yaml:"node"`
	// When is a predicate over the outputs of the step that registered the
	// cleanup, or of the cleanup step before it in a chain. The cleanup is
	// skipped when it is false.
	When string `yaml:"when,omitempty"`
	// ReleasedBy lists nodes whose success, after the step that registered the
	// cleanup and with the same values for the inputs they share with it,
	// releases the resource, so the cleanup is skipped. A main step on the
	// cleanup node itself always counts.
	ReleasedBy []string `yaml:"releasedBy,omitempty"`
}

// IsZero reports whether the pairing is unset, so a node without a cleanup
// writes no cleanup key.
func (c CleanupPairing) IsZero() bool {
	return c.Node == "" && c.When == "" && len(c.ReleasedBy) == 0
}

// String renders the pairing for docs and prompts: the node name, followed by
// its condition and its releasing nodes when it has them.
func (c CleanupPairing) String() string {
	var details []string
	if c.When != "" {
		details = append(details, "when "+c.When)
	}
	if len(c.ReleasedBy) > 0 {
		details = append(details, "released by "+strings.Join(c.ReleasedBy, ", "))
	}
	if len(details) == 0 {
		return c.Node
	}
	return fmt.Sprintf("%s (%s)", c.Node, strings.Join(details, "; "))
}

// UnmarshalYAML accepts a node name or a mapping with node, when, and
// releasedBy. It uses the callback form so strict decoding reaches the mapping
// (see internal/yamlx).
func (c *CleanupPairing) UnmarshalYAML(unmarshal func(any) error) error {
	n, err := yamlx.Node(unmarshal)
	if err != nil {
		return err
	}
	switch n.Kind {
	case yaml.ScalarNode:
		*c = CleanupPairing{}
		return unmarshal(&c.Node)
	case yaml.MappingNode:
		// The raw type has no methods, which avoids infinite recursion.
		type rawCleanupPairing CleanupPairing
		var raw rawCleanupPairing
		if err := unmarshal(&raw); err != nil {
			return err
		}
		*c = CleanupPairing(raw)
		return nil
	default:
		return yamlx.KindError(n, "cleanup", "a node name or a mapping with node, when, and releasedBy")
	}
}

// MarshalYAML writes a pairing that sets only its node as the bare node name,
// and any other pairing as a mapping.
func (c CleanupPairing) MarshalYAML() (interface{}, error) {
	if c.When == "" && len(c.ReleasedBy) == 0 {
		return c.Node, nil
	}
	// The raw type has no methods, which avoids infinite recursion.
	type rawCleanupPairing CleanupPairing
	return rawCleanupPairing(c), nil
}

// validateCleanupPairings checks the parts of each cleanup pairing beyond its
// node: when and releasedBy need a node, releasedBy names other existing nodes,
// each once, and when parses and names only outputs of the node that declares
// it (see cleanupWhenErrors). Validate checks the node itself.
func validateCleanupPairings(g *Graph) []string {
	var errs []string
	for _, name := range sortedKeys(g.Nodes) {
		c := g.Nodes[name].Cleanup
		if c.Node == "" {
			if c.When != "" || len(c.ReleasedBy) > 0 {
				errs = append(errs, fmt.Sprintf("node %q: cleanup sets when or releasedBy but no node", name))
			}
			continue
		}
		if c.When != "" {
			errs = append(errs, cleanupWhenErrors(name, g.Nodes[name])...)
		}
		for i, released := range c.ReleasedBy {
			switch {
			case released == name:
				errs = append(errs, fmt.Sprintf("node %q: cleanup releasedBy cannot name the node itself", name))
			case g.Nodes[released] == nil:
				errs = append(errs, fmt.Sprintf("node %q: cleanup releasedBy references unknown node %q", name, released))
			case slices.Contains(c.ReleasedBy[:i], released):
				errs = append(errs, fmt.Sprintf("node %q: cleanup releasedBy lists %q more than once", name, released))
			}
		}
	}
	return errs
}

// cleanupWhenErrors checks a node's cleanup when condition, which reads the
// outputs of the node's step: it parses, and every field it names is one of the
// node's outputs.
func cleanupWhenErrors(name string, node *Node) []string {
	when := node.Cleanup.When
	if err := predicate.Validate(when); err != nil {
		return []string{fmt.Sprintf("node %q: cleanup when %q: %v", name, when, err)}
	}
	var errs []string
	for _, field := range predicate.Fields(when) {
		root, _, _ := strings.Cut(field, ".")
		if !slices.ContainsFunc(node.Outputs, func(out Output) bool { return out.Name == root }) {
			errs = append(errs, fmt.Sprintf("node %q: cleanup when %q names %q, which is not an output of %q", name, when, root, name))
		}
	}
	return errs
}
