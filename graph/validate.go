package graph

import (
	"fmt"
	"strings"
)

// ValidationError collects all structural validation errors for a graph.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("graph validation failed:\n  - %s", strings.Join(e.Errors, "\n  - "))
}

// Validate performs structural validation on a parsed Graph.
// It collects all errors rather than failing fast, returning a
// *ValidationError if any problems are found.
func Validate(g *Graph) error {
	var errs []string

	// 1. Version: present and valid semver
	if g.Version == "" {
		errs = append(errs, "version is required")
	} else if _, err := ParseVersion(g.Version); err != nil {
		errs = append(errs, fmt.Sprintf("invalid version: %v", err))
	}

	// Build lookup maps for later checks
	outputTypes := map[string]string{} // "node.field" → type string

	// 2. Nodes: validate structure
	for name, node := range g.Nodes {
		if node.Adapter == "" {
			errs = append(errs, fmt.Sprintf("node %q: adapter is required", name))
		}

		// Input validation
		inputNames := map[string]bool{}
		for _, inp := range node.Inputs {
			if inp.Name == "" {
				errs = append(errs, fmt.Sprintf("node %q: input missing name", name))
				continue
			}
			if inp.Type == "" {
				errs = append(errs, fmt.Sprintf("node %q: input %q missing type", name, inp.Name))
			} else if _, err := ParseFieldType(inp.Type); err != nil {
				errs = append(errs, fmt.Sprintf("node %q: input %q: invalid type %q: %v", name, inp.Name, inp.Type, err))
			}
			if inputNames[inp.Name] {
				errs = append(errs, fmt.Sprintf("node %q: duplicate input name %q", name, inp.Name))
			}
			inputNames[inp.Name] = true
		}

		// Output validation
		outputNames := map[string]bool{}
		for _, out := range node.Outputs {
			if out.Name == "" {
				errs = append(errs, fmt.Sprintf("node %q: output missing name", name))
				continue
			}
			if out.Type == "" {
				errs = append(errs, fmt.Sprintf("node %q: output %q missing type", name, out.Name))
			} else {
				ft, err := ParseFieldType(out.Type)
				if err != nil {
					errs = append(errs, fmt.Sprintf("node %q: output %q: invalid type %q: %v", name, out.Name, out.Type, err))
				} else {
					outputTypes[name+"."+out.Name] = out.Type

					// 4. Array outputs with elementFields
					if len(out.ElementFields) > 0 && !ft.IsArray {
						errs = append(errs, fmt.Sprintf("node %q: output %q: elementFields specified but type %q is not an array", name, out.Name, out.Type))
					}
					for _, ef := range out.ElementFields {
						if ef.Name == "" {
							errs = append(errs, fmt.Sprintf("node %q: output %q: elementField missing name", name, out.Name))
						}
						if ef.Type == "" {
							errs = append(errs, fmt.Sprintf("node %q: output %q: elementField %q missing type", name, out.Name, ef.Name))
						} else if _, err := ParseFieldType(ef.Type); err != nil {
							errs = append(errs, fmt.Sprintf("node %q: output %q: elementField %q: invalid type %q: %v", name, out.Name, ef.Name, ef.Type, err))
						}
					}
				}
			}
			if outputNames[out.Name] {
				errs = append(errs, fmt.Sprintf("node %q: duplicate output name %q", name, out.Name))
			}
			outputNames[out.Name] = true
		}

		// 5. Cleanup: references an existing node, not self
		if node.Cleanup != "" {
			if node.Cleanup == name {
				errs = append(errs, fmt.Sprintf("node %q: cleanup cannot reference itself", name))
			} else if g.Nodes[node.Cleanup] == nil {
				errs = append(errs, fmt.Sprintf("node %q: cleanup references unknown node %q", name, node.Cleanup))
			}
		}
	}

	// Build input lookup for edge target validation
	inputExists := map[string]bool{} // "node.field"
	for name, node := range g.Nodes {
		for _, inp := range node.Inputs {
			if inp.Name != "" {
				inputExists[name+"."+inp.Name] = true
			}
		}
	}

	// 6. Edges: validate format and references
	edgeSeen := map[string]bool{}
	for i, edge := range g.Edges {
		fromNode, fromField, err := splitRef(edge.From)
		if err != nil {
			errs = append(errs, fmt.Sprintf("edge %d: invalid 'from' %q: %v", i, edge.From, err))
		} else {
			if g.Nodes[fromNode] == nil {
				errs = append(errs, fmt.Sprintf("edge %d: 'from' references unknown node %q", i, fromNode))
			} else if _, ok := outputTypes[edge.From]; !ok {
				errs = append(errs, fmt.Sprintf("edge %d: 'from' references unknown output %q on node %q", i, fromField, fromNode))
			}
		}

		toNode, toField, err := splitRef(edge.To)
		if err != nil {
			errs = append(errs, fmt.Sprintf("edge %d: invalid 'to' %q: %v", i, edge.To, err))
		} else {
			if g.Nodes[toNode] == nil {
				errs = append(errs, fmt.Sprintf("edge %d: 'to' references unknown node %q", i, toNode))
			} else if !inputExists[edge.To] {
				errs = append(errs, fmt.Sprintf("edge %d: 'to' references unknown input %q on node %q", i, toField, toNode))
			}
		}

		// Duplicate edge check
		edgeKey := edge.From + " -> " + edge.To
		if edgeSeen[edgeKey] {
			errs = append(errs, fmt.Sprintf("edge %d: duplicate edge from %q to %q", i, edge.From, edge.To))
		}
		edgeSeen[edgeKey] = true

		// 7. Select edges: source output must be array type
		if edge.Select {
			if typ, ok := outputTypes[edge.From]; ok {
				ft, err := ParseFieldType(typ)
				if err == nil && !ft.IsArray {
					errs = append(errs, fmt.Sprintf("edge %d: select is true but source output %q is not an array type", i, edge.From))
				}
			}
		}
	}

	// 8. Conditions: validate references
	for i, cond := range g.Conditions {
		if cond.When == "" {
			errs = append(errs, fmt.Sprintf("condition %d: 'when' is required", i))
		}
		for _, req := range cond.Require {
			if g.Nodes[req] == nil {
				errs = append(errs, fmt.Sprintf("condition %d: 'require' references unknown node %q", i, req))
			}
		}
		for _, bef := range cond.Before {
			if g.Nodes[bef] == nil {
				errs = append(errs, fmt.Sprintf("condition %d: 'before' references unknown node %q", i, bef))
			}
		}
	}

	// 9. Cycle detection
	if cycles := detectCycles(g); len(cycles) > 0 {
		for _, c := range cycles {
			errs = append(errs, c)
		}
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// detectCycles uses DFS with three-color marking to detect cycles
// in the node-level adjacency graph built from edges.
func detectCycles(g *Graph) []string {
	// Build adjacency list at node level
	adj := map[string]map[string]bool{}
	for name := range g.Nodes {
		adj[name] = map[string]bool{}
	}
	for _, edge := range g.Edges {
		fromNode, _, err1 := splitRef(edge.From)
		toNode, _, err2 := splitRef(edge.To)
		if err1 != nil || err2 != nil {
			continue // skip malformed edges; they're reported by edge validation
		}
		if g.Nodes[fromNode] == nil || g.Nodes[toNode] == nil {
			continue
		}
		adj[fromNode][toNode] = true
	}

	const (
		white = 0 // unvisited
		gray  = 1 // in current path
		black = 2 // fully explored
	)

	color := map[string]int{}
	parent := map[string]string{}
	var cycles []string

	var dfs func(node string)
	dfs = func(node string) {
		color[node] = gray
		for neighbor := range adj[node] {
			if color[neighbor] == gray {
				// Found a cycle — reconstruct the path
				path := []string{neighbor, node}
				cur := node
				for cur != neighbor {
					cur = parent[cur]
					path = append(path, cur)
				}
				// Reverse to get forward order
				for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}
				// Suppress cycle if any node in the path is a CycleBreaker
				hasCycleBreaker := false
				for _, p := range path {
					if n := g.Nodes[p]; n != nil && n.CycleBreaker {
						hasCycleBreaker = true
						break
					}
				}
				if !hasCycleBreaker {
					cycles = append(cycles, fmt.Sprintf("cycle detected: %s", strings.Join(path, " → ")))
				}
			} else if color[neighbor] == white {
				parent[neighbor] = node
				dfs(neighbor)
			}
		}
		color[node] = black
	}

	for name := range g.Nodes {
		if color[name] == white {
			dfs(name)
		}
	}

	return cycles
}
