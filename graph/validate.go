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
			if inp.Configurable && !inp.Optional {
				errs = append(errs, fmt.Sprintf("node %q: input %q: configurable requires optional", name, inp.Name))
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

	// 6. Error detection rules: graph-level
	for i, rule := range g.ErrorDetection {
		for _, msg := range validateErrorDetectionRule(rule) {
			errs = append(errs, fmt.Sprintf("errorDetection[%d]: %s", i, msg))
		}
	}

	// 6a. Error detection rules: per-node
	for name, node := range g.Nodes {
		for i, rule := range node.ErrorDetection {
			for _, msg := range validateErrorDetectionRule(rule) {
				errs = append(errs, fmt.Sprintf("node %q: errorDetection[%d]: %s", name, i, msg))
			}
		}
	}

	// 7. Conditions: validate references
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

	// 7. Requires/satisfies validation
	// Build satisfier index for validation.
	satisfierIndex := map[string][]string{} // token → node names
	for name, node := range g.Nodes {
		for _, token := range node.Satisfies {
			satisfierIndex[token] = append(satisfierIndex[token], name)
		}
	}

	// 7a. Unsatisfied requirements
	for name, node := range g.Nodes {
		for _, token := range node.Requires {
			if len(satisfierIndex[token]) == 0 {
				errs = append(errs, fmt.Sprintf("node %q: requires token %q but no node satisfies it", name, token))
			}
		}
	}

	// 7b. Cycle detection in requires/satisfies graph
	if reqCycles := detectRequiresCycles(g, satisfierIndex); len(reqCycles) > 0 {
		errs = append(errs, reqCycles...)
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// ValidateWarnings returns non-fatal warnings about the graph.
// These do not prevent the graph from being used, but may indicate
// configuration issues (e.g., addon After referencing unknown nodes,
// multiple satisfiers without preferred).
func ValidateWarnings(g *Graph) []string {
	var warnings []string

	// Build workflow name index for slot option validation.
	workflowByName := make(map[string]Workflow, len(g.Workflows))
	for _, wf := range g.Workflows {
		workflowByName[wf.Name] = wf
	}

	for i, wf := range g.Workflows {
		// Validate kind value
		if wf.Kind != "" && wf.Kind != "addon" && wf.Kind != "slot" {
			warnings = append(warnings, fmt.Sprintf("workflow %d (%q): unknown kind %q (expected \"addon\" or \"slot\")", i, wf.Name, wf.Kind))
		}

		// Validate Priority
		if wf.Priority < 0 {
			warnings = append(warnings, fmt.Sprintf("workflow %d (%q): negative priority %d", i, wf.Name, wf.Priority))
		}

		// Validate After field
		if wf.After.IsSet() {
			if !wf.IsAddon() {
				warnings = append(warnings, fmt.Sprintf("workflow %d (%q): after is set but kind is not \"addon\"", i, wf.Name))
			}
			for _, afterNode := range wf.After {
				if g.Nodes[afterNode] == nil {
					warnings = append(warnings, fmt.Sprintf("workflow %d (%q): after references unknown node %q", i, wf.Name, afterNode))
				}
			}
		}

		// Validate slot definitions
		for j, sd := range wf.Slots {
			if sd.Name == "" {
				warnings = append(warnings, fmt.Sprintf("workflow %d (%q): slot %d has no name", i, wf.Name, j))
			}
			if len(sd.Options) == 0 {
				warnings = append(warnings, fmt.Sprintf("workflow %d (%q): slot %q has no options", i, wf.Name, sd.Name))
			}
			for _, optName := range sd.Options {
				opt, optExists := workflowByName[optName]
				if !optExists {
					warnings = append(warnings, fmt.Sprintf("workflow %d (%q): slot %q references unknown option workflow %q", i, wf.Name, sd.Name, optName))
				} else if !opt.IsSlot() {
					warnings = append(warnings, fmt.Sprintf("workflow %d (%q): slot %q option %q is not kind \"slot\"", i, wf.Name, sd.Name, optName))
				}
			}
			if sd.Default != "" {
				found := false
				for _, optName := range sd.Options {
					if optName == sd.Default {
						found = true
						break
					}
				}
				if !found {
					warnings = append(warnings, fmt.Sprintf("workflow %d (%q): slot %q default %q is not in options list", i, wf.Name, sd.Name, sd.Default))
				}
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

	return warnings
}

// validErrorDetectionRules is the set of supported error detection rule types.
var validErrorDetectionRules = map[string]bool{
	"exists":    true,
	"non-empty": true,
	"equals":    true,
}

// validateErrorDetectionRule checks a single ErrorDetectionRule and returns
// any validation error messages.
func validateErrorDetectionRule(rule ErrorDetectionRule) []string {
	var errs []string
	if rule.Path == "" {
		errs = append(errs, "path is required")
	} else if msg := validateGjsonPath(rule.Path); msg != "" {
		errs = append(errs, fmt.Sprintf("invalid path %q: %s", rule.Path, msg))
	}
	if rule.Rule == "" {
		errs = append(errs, "rule is required")
	} else if !validErrorDetectionRules[rule.Rule] {
		errs = append(errs, fmt.Sprintf("unknown rule %q (must be exists, non-empty, or equals)", rule.Rule))
	}
	if rule.Rule == "equals" && rule.Value == nil {
		errs = append(errs, "equals rule requires a value")
	}
	if rule.Details != nil {
		if rule.Details.Message != "" {
			if msg := validateGjsonPath(rule.Details.Message); msg != "" {
				errs = append(errs, fmt.Sprintf("invalid details.message path %q: %s", rule.Details.Message, msg))
			}
		}
		if rule.Details.Code != "" {
			if msg := validateGjsonPath(rule.Details.Code); msg != "" {
				errs = append(errs, fmt.Sprintf("invalid details.code path %q: %s", rule.Details.Code, msg))
			}
		}
		if rule.Details.Category != "" {
			if msg := validateGjsonPath(rule.Details.Category); msg != "" {
				errs = append(errs, fmt.Sprintf("invalid details.category path %q: %s", rule.Details.Category, msg))
			}
		}
	}
	return errs
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

