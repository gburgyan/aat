package graph

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
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
			// Constraint validation
			if inp.Constraints != nil {
				c := inp.Constraints
				if c.Pattern != "" {
					if _, err := regexp.Compile(c.Pattern); err != nil {
						errs = append(errs, fmt.Sprintf("node %q: input %q: invalid constraint pattern %q: %v", name, inp.Name, c.Pattern, err))
					}
				}
				if c.Min != nil && c.Max != nil && *c.Min > *c.Max {
					errs = append(errs, fmt.Sprintf("node %q: input %q: constraint min (%v) > max (%v)", name, inp.Name, *c.Min, *c.Max))
				}
				if c.MinLength != nil && c.MaxLength != nil && *c.MinLength > *c.MaxLength {
					errs = append(errs, fmt.Sprintf("node %q: input %q: constraint minLength (%d) > maxLength (%d)", name, inp.Name, *c.MinLength, *c.MaxLength))
				}
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
						if ef.Path != "" {
							if msg := validateGjsonPath(ef.Path); msg != "" {
								errs = append(errs, fmt.Sprintf("node %q: output %q: elementField %q: invalid path %q: %s", name, out.Name, ef.Name, ef.Path, msg))
							}
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

	// 9. Cycle detection (data-flow edges)
	if cycles := detectCycles(g); len(cycles) > 0 {
		for _, c := range cycles {
			errs = append(errs, c)
		}
	}

	// 10. Requires/satisfies validation
	// Build satisfier index for validation.
	satisfierIndex := map[string][]string{} // token → node names
	for name, node := range g.Nodes {
		for _, token := range node.Satisfies {
			satisfierIndex[token] = append(satisfierIndex[token], name)
		}
	}

	// 10a. Unsatisfied requirements
	for name, node := range g.Nodes {
		for _, token := range node.Requires {
			if len(satisfierIndex[token]) == 0 {
				errs = append(errs, fmt.Sprintf("node %q: requires token %q but no node satisfies it", name, token))
			}
		}
	}

	// 10b. Cycle detection in requires/satisfies graph
	if reqCycles := detectRequiresCycles(g, satisfierIndex); len(reqCycles) > 0 {
		for _, c := range reqCycles {
			errs = append(errs, c)
		}
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// ValidateWarnings returns non-fatal warnings about the graph.
// These do not prevent the graph from being used, but may indicate
// configuration issues (e.g., addon After referencing unknown nodes,
// auto-wired edges with type mismatches, multiple satisfiers without preferred).
func ValidateWarnings(g *Graph) []string {
	var warnings []string

	for i, wf := range g.Workflows {
		// Validate kind value
		if wf.Kind != "" && wf.Kind != "addon" {
			warnings = append(warnings, fmt.Sprintf("workflow %d (%q): unknown kind %q (expected \"addon\")", i, wf.Name, wf.Kind))
		}

		// Validate Priority
		if wf.Priority < 0 {
			warnings = append(warnings, fmt.Sprintf("workflow %d (%q): negative priority %d", i, wf.Name, wf.Priority))
		}

		// Validate After field
		if wf.After != "" {
			if !wf.IsAddon() {
				warnings = append(warnings, fmt.Sprintf("workflow %d (%q): after is set but kind is not \"addon\"", i, wf.Name))
			}
			if g.Nodes[wf.After] == nil {
				warnings = append(warnings, fmt.Sprintf("workflow %d (%q): after references unknown node %q", i, wf.Name, wf.After))
			}
		}
	}

	// Multiple satisfiers for a token without a preferred node
	satisfierIndex := map[string][]string{}
	for name, node := range g.Nodes {
		for _, token := range node.Satisfies {
			satisfierIndex[token] = append(satisfierIndex[token], name)
		}
	}
	for token, satisfiers := range satisfierIndex {
		if len(satisfiers) <= 1 {
			continue
		}
		hasPreferred := false
		for _, s := range satisfiers {
			if g.Nodes[s] != nil && g.Nodes[s].Preferred {
				hasPreferred = true
				break
			}
		}
		if !hasPreferred {
			warnings = append(warnings, fmt.Sprintf("token %q has multiple satisfiers %v but none is marked preferred", token, satisfiers))
		}
	}

	// Warn on auto-wired edges where output type != input type
	if g.AutoWire.IsEnabled() {
		// Build input type index
		inputTypes := make(map[string]string) // "node.input" → type
		for name, node := range g.Nodes {
			for _, inp := range node.Inputs {
				if inp.Name != "" && inp.Type != "" {
					inputTypes[name+"."+inp.Name] = inp.Type
				}
			}
		}
		for _, edge := range g.Edges {
			if !edge.AutoWired {
				continue
			}
			fromNode, fromField, err1 := splitRef(edge.From)
			if err1 != nil {
				continue
			}
			// Find source output type
			var outType string
			if node := g.Nodes[fromNode]; node != nil {
				for _, out := range node.Outputs {
					if out.Name == fromField {
						outType = out.Type
						break
					}
				}
			}
			inType := inputTypes[edge.To]
			if outType != "" && inType != "" && outType != inType {
				warnings = append(warnings, fmt.Sprintf("auto-wired edge %s → %s: type mismatch (output %q, input %q)", edge.From, edge.To, outType, inType))
			}
		}
	}

	return warnings
}

// validateGjsonPath performs basic plausibility checks on a gjson path.
// Returns an error message if the path looks wrong, or empty string if OK.
func validateGjsonPath(path string) string {
	if strings.HasSuffix(path, ".") {
		return "trailing dot"
	}
	if strings.Contains(path, "..") {
		return "empty segment (consecutive dots)"
	}
	for _, r := range path {
		if !unicode.IsPrint(r) {
			return "contains non-printable character"
		}
	}
	return ""
}

// detectRequiresCycles detects cycles in the requires/satisfies graph.
// A cycle exists when A requires token X, B satisfies X and requires token Y,
// and C satisfies Y and requires X (or any longer cycle). Cycles involving
// CycleBreaker nodes are suppressed.
func detectRequiresCycles(g *Graph, satisfierIndex map[string][]string) []string {
	// Build adjacency: for each node that requires a token, add edges to
	// all nodes that satisfy it (these are the nodes it depends on).
	adj := map[string]map[string]bool{}
	for name := range g.Nodes {
		adj[name] = map[string]bool{}
	}
	for name, node := range g.Nodes {
		for _, token := range node.Requires {
			for _, satisfier := range satisfierIndex[token] {
				if satisfier != name {
					adj[satisfier][name] = true // satisfier → requirer (ordering direction)
				}
			}
		}
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)

	color := map[string]int{}
	parent := map[string]string{}
	var cycles []string

	var dfs func(node string)
	dfs = func(node string) {
		color[node] = gray
		for neighbor := range adj[node] {
			if color[neighbor] == gray {
				path := []string{neighbor, node}
				cur := node
				for cur != neighbor {
					cur = parent[cur]
					path = append(path, cur)
				}
				for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}
				hasCycleBreaker := false
				for _, p := range path {
					if n := g.Nodes[p]; n != nil && n.CycleBreaker {
						hasCycleBreaker = true
						break
					}
				}
				if !hasCycleBreaker {
					cycles = append(cycles, fmt.Sprintf("requires/satisfies cycle detected: %s", strings.Join(path, " → ")))
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
