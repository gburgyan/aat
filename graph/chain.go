package graph

import (
	"fmt"
	"strings"
)

// PredicateFunc evaluates a predicate expression against a context map.
// It is injected to avoid importing the plan package (graph is a leaf package).
type PredicateFunc func(expr string, ctx map[string]any) (bool, error)

// ChainOptions configures a backward chaining operation.
type ChainOptions struct {
	Goals            []string       // required, at least one
	ConditionContext map[string]any // values for evaluating Condition.When
	EvalPredicate    PredicateFunc  // nil → all conditions treated as false
}

// ChainResult contains the output of a backward chaining operation.
type ChainResult struct {
	Nodes              []string          // dependency-ordered (topo sort)
	RequiresEdges      []RequiresEdge    // virtual edges from requires/satisfies relationships
	EntryNodes         []string          // in-degree 0 within the chain
	Decisions          []ChainDecision   // cycle breaks, satisfier selections
	IncludedConditions []ConditionResult // conditions that evaluated true
	ExcludedConditions []ConditionResult // conditions that evaluated false
}

// RequiresEdge represents a virtual ordering edge from a satisfier node to a
// requiring node, created by the requires/satisfies mechanism.
type RequiresEdge struct {
	From  string // satisfier node name
	To    string // requiring node name
	Token string // the requirement token
}

// ChainDecision records a decision made during backward chaining.
type ChainDecision struct {
	Type         DecisionType
	Node         string
	Detail       string
	Alternatives []string
}

// DecisionType classifies a backward chaining decision.
type DecisionType int

const (
	// DecisionCycleBreak indicates traversal stopped at a CycleBreaker node.
	DecisionCycleBreak DecisionType = iota
	// DecisionPathChoice indicates a path was chosen among alternatives.
	DecisionPathChoice
	// DecisionSatisfier indicates a satisfier was chosen for a requirement token.
	DecisionSatisfier
)

// ConditionResult records the evaluation result of a graph condition.
type ConditionResult struct {
	Condition Condition
	Result    bool
}

// ChainError collects all errors from backward chaining.
type ChainError struct {
	Errors []string
}

func (e *ChainError) Error() string {
	return fmt.Sprintf("backward chaining failed:\n  - %s", strings.Join(e.Errors, "\n  - "))
}

// orderingEdge represents a virtual ordering constraint between two nodes.
type orderingEdge struct {
	from string
	to   string
}

// BackwardChain computes the minimal subgraph needed to reach the given goal
// nodes. It returns the nodes in dependency order (topological sort), the
// requires/satisfies edges, and any decisions made during traversal.
func BackwardChain(g *Graph, opts ChainOptions) (*ChainResult, error) {
	var errs []string

	// Validate goals
	if len(opts.Goals) == 0 {
		errs = append(errs, "at least one goal is required")
		return nil, &ChainError{Errors: errs}
	}
	for _, goal := range opts.Goals {
		if g.Nodes[goal] == nil {
			errs = append(errs, fmt.Sprintf("unknown goal node %q", goal))
		}
	}
	if len(errs) > 0 {
		return nil, &ChainError{Errors: errs}
	}

	result := &ChainResult{}

	// --- Phase 1: Condition Evaluation ---

	// Evaluate conditions
	conditionRequired := map[string]bool{}
	for _, cond := range g.Conditions {
		if opts.EvalPredicate == nil {
			result.ExcludedConditions = append(result.ExcludedConditions, ConditionResult{
				Condition: cond,
				Result:    false,
			})
			continue
		}
		val, err := opts.EvalPredicate(cond.When, opts.ConditionContext)
		if err != nil {
			errs = append(errs, fmt.Sprintf("evaluating condition %q: %v", cond.When, err))
			continue
		}
		if val {
			result.IncludedConditions = append(result.IncludedConditions, ConditionResult{
				Condition: cond,
				Result:    true,
			})
			for _, req := range cond.Require {
				conditionRequired[req] = true
			}
		} else {
			result.ExcludedConditions = append(result.ExcludedConditions, ConditionResult{
				Condition: cond,
				Result:    false,
			})
		}
	}
	if len(errs) > 0 {
		return nil, &ChainError{Errors: errs}
	}

	// --- Phase 2: BFS Backward from Goals (requires/satisfies only) ---

	queue := make([]string, 0, len(opts.Goals)+len(conditionRequired))
	queue = append(queue, opts.Goals...)
	for req := range conditionRequired {
		queue = append(queue, req)
	}

	visited := map[string]bool{}
	includedNodes := map[string]bool{}

	// Track requirement tokens encountered and their candidate satisfiers.
	tokenCandidates := map[string]map[string]bool{}
	tokenRequirers := map[string]map[string]bool{}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]

		if visited[node] {
			continue
		}
		visited[node] = true
		includedNodes[node] = true

		n := g.Nodes[node]
		if n != nil && n.CycleBreaker {
			result.Decisions = append(result.Decisions, ChainDecision{
				Type:   DecisionCycleBreak,
				Node:   node,
				Detail: fmt.Sprintf("stopped traversal at cycle-breaker node %q", node),
			})
			continue // don't traverse upstream
		}

		// Traverse requires tokens: enqueue all candidate satisfiers.
		if n != nil {
			for _, token := range n.Requires {
				if tokenRequirers[token] == nil {
					tokenRequirers[token] = map[string]bool{}
				}
				tokenRequirers[token][node] = true

				satisfiers := g.SatisfiersByToken[token]
				if tokenCandidates[token] == nil {
					tokenCandidates[token] = map[string]bool{}
				}
				for _, s := range satisfiers {
					tokenCandidates[token][s] = true
					if !visited[s] {
						queue = append(queue, s)
					}
				}
			}
		}
	}

	// --- Phase 3: Satisfier Resolution + Topo Sort ---

	// Pick one satisfier per requirement token.
	var requiresEdges []RequiresEdge
	for _, token := range sortedKeys(tokenCandidates) {
		candidates := tokenCandidates[token]
		requirers := tokenRequirers[token]

		// Filter to candidates still in includedNodes.
		var validCandidates []string
		for c := range candidates {
			if includedNodes[c] {
				validCandidates = append(validCandidates, c)
			}
		}
		sortStrings(validCandidates)

		if len(validCandidates) == 0 {
			continue
		}

		chosen := pickSatisfier(validCandidates, g)

		// Record decision if there were alternatives.
		if len(validCandidates) > 1 {
			var alternatives []string
			for _, c := range validCandidates {
				if c != chosen {
					alternatives = append(alternatives, c)
				}
			}
			result.Decisions = append(result.Decisions, ChainDecision{
				Type:         DecisionSatisfier,
				Node:         chosen,
				Detail:       fmt.Sprintf("token %q: chose satisfier %q", token, chosen),
				Alternatives: alternatives,
			})
		}

		// Create requires edges from the chosen satisfier to all requirers.
		for req := range requirers {
			if includedNodes[req] {
				requiresEdges = append(requiresEdges, RequiresEdge{
					From:  chosen,
					To:    req,
					Token: token,
				})
			}
		}

		// Prune unchosen satisfiers if they're not needed for other reasons.
		for _, c := range validCandidates {
			if c != chosen {
				neededElsewhere := false
				for _, otherToken := range sortedKeys(tokenCandidates) {
					if otherToken == token {
						continue
					}
					if tokenCandidates[otherToken][c] {
						neededElsewhere = true
						break
					}
				}
				// Check if it's a goal or condition-required node.
				for _, goal := range opts.Goals {
					if c == goal {
						neededElsewhere = true
						break
					}
				}
				if conditionRequired[c] {
					neededElsewhere = true
				}
				if !neededElsewhere {
					delete(includedNodes, c)
				}
			}
		}
	}

	// Prune nodes that become unreachable after satisfier selection.
	reachable := computeReachable(opts.Goals, conditionRequired, requiresEdges)
	for node := range includedNodes {
		if !reachable[node] {
			delete(includedNodes, node)
		}
	}

	// Filter requires edges to only those with both endpoints still included.
	var filteredRequiresEdges []RequiresEdge
	for _, re := range requiresEdges {
		if includedNodes[re.From] && includedNodes[re.To] {
			filteredRequiresEdges = append(filteredRequiresEdges, re)
		}
	}
	requiresEdges = filteredRequiresEdges

	// Condition Before ordering
	var virtualEdges []orderingEdge
	for _, cr := range result.IncludedConditions {
		for _, req := range cr.Condition.Require {
			if !includedNodes[req] {
				continue
			}
			for _, bef := range cr.Condition.Before {
				if !includedNodes[bef] {
					continue
				}
				virtualEdges = append(virtualEdges, orderingEdge{from: req, to: bef})
			}
		}
	}

	// Add requires edges as virtual ordering edges for topo sort.
	for _, re := range requiresEdges {
		virtualEdges = append(virtualEdges, orderingEdge{from: re.From, to: re.To})
	}

	// Topological sort
	sorted, err := chainTopoSort(includedNodes, virtualEdges)
	if err != nil {
		return nil, &ChainError{Errors: []string{err.Error()}}
	}

	result.Nodes = sorted
	result.RequiresEdges = requiresEdges

	// Identify entry nodes (in-degree 0)
	inDegree := map[string]int{}
	for _, node := range sorted {
		inDegree[node] = 0
	}
	for _, ve := range virtualEdges {
		if includedNodes[ve.from] && includedNodes[ve.to] {
			inDegree[ve.to]++
		}
	}
	for _, node := range sorted {
		if inDegree[node] == 0 {
			result.EntryNodes = append(result.EntryNodes, node)
		}
	}

	return result, nil
}

// pickSatisfier selects the best satisfier node for a requirement token.
// Prefers nodes marked Preferred, then alphabetical.
func pickSatisfier(candidates []string, g *Graph) string {
	for _, c := range candidates {
		if n := g.Nodes[c]; n != nil && n.Preferred {
			return c
		}
	}
	return candidates[0] // already sorted alphabetically
}

// computeReachable finds all nodes reachable by backward traversal
// from goals, considering requires edges.
func computeReachable(goals []string, condRequired map[string]bool, requiresEdges []RequiresEdge) map[string]bool {
	reverseAdj := map[string]map[string]bool{}
	for _, re := range requiresEdges {
		if reverseAdj[re.To] == nil {
			reverseAdj[re.To] = map[string]bool{}
		}
		reverseAdj[re.To][re.From] = true
	}

	reachable := map[string]bool{}
	queue := make([]string, 0, len(goals)+len(condRequired))
	queue = append(queue, goals...)
	for req := range condRequired {
		queue = append(queue, req)
	}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if reachable[node] {
			continue
		}
		reachable[node] = true
		for upstream := range reverseAdj[node] {
			if !reachable[upstream] {
				queue = append(queue, upstream)
			}
		}
	}
	return reachable
}

// chainTopoSort performs Kahn's algorithm on the node set with deterministic ordering.
func chainTopoSort(nodes map[string]bool, virtual []orderingEdge) ([]string, error) {
	adj := map[string]map[string]bool{}
	inDeg := map[string]int{}
	for node := range nodes {
		adj[node] = map[string]bool{}
		inDeg[node] = 0
	}

	addEdge := func(from, to string) {
		if !nodes[from] || !nodes[to] || from == to {
			return
		}
		if !adj[from][to] {
			adj[from][to] = true
			inDeg[to]++
		}
	}

	for _, ve := range virtual {
		addEdge(ve.from, ve.to)
	}

	// Kahn's with sorted queue for determinism
	var queue []string
	for node := range nodes {
		if inDeg[node] == 0 {
			queue = append(queue, node)
		}
	}
	sortStrings(queue)

	var sorted []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)

		neighbors := sortedKeys(adj[node])
		for _, neighbor := range neighbors {
			inDeg[neighbor]--
			if inDeg[neighbor] == 0 {
				queue = insertSorted(queue, neighbor)
			}
		}
	}

	if len(sorted) != len(nodes) {
		return nil, fmt.Errorf("unresolvable cycle detected in chain (no cycle-breaker)")
	}

	return sorted, nil
}

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

// sortStrings sorts a string slice in place (insertion sort for small slices).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}

// insertSorted inserts a string into a sorted slice maintaining order.
func insertSorted(s []string, val string) []string {
	i := 0
	for i < len(s) && s[i] < val {
		i++
	}
	s = append(s, "")
	copy(s[i+1:], s[i:])
	s[i] = val
	return s
}
