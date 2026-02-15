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
	Edges              []Edge            // subset of graph edges connecting chain nodes
	RequiresEdges      []RequiresEdge    // virtual edges from requires/satisfies relationships
	EntryNodes         []string          // in-degree 0 within the chain
	Decisions          []ChainDecision   // cycle breaks, path choices, satisfier selections
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

// inputKey identifies a specific input on a specific node.
type inputKey struct {
	node  string
	input string
}

// BackwardChain computes the minimal subgraph needed to reach the given goal
// nodes. It returns the nodes in dependency order (topological sort), the
// connecting edges, and any decisions made during traversal.
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

	// Build edge index by consumer node
	edgesByConsumer := map[string][]Edge{}
	for _, edge := range g.Edges {
		toNode, _, err := splitRef(edge.To)
		if err != nil {
			continue
		}
		edgesByConsumer[toNode] = append(edgesByConsumer[toNode], edge)
	}

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

	// --- Phase 2: BFS Backward from Goals ---

	queue := make([]string, 0, len(opts.Goals)+len(conditionRequired))
	queue = append(queue, opts.Goals...)
	for req := range conditionRequired {
		queue = append(queue, req)
	}

	visited := map[string]bool{}
	includedNodes := map[string]bool{}
	var includedEdges []Edge

	// Track requirement tokens encountered and their candidate satisfiers.
	// tokenCandidates maps each token to the set of candidate satisfier node names.
	tokenCandidates := map[string]map[string]bool{}
	// tokenRequirers maps each token to the set of nodes that require it.
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

		// Traverse data-flow edges backward.
		for _, edge := range edgesByConsumer[node] {
			fromNode, _, err := splitRef(edge.From)
			if err != nil {
				continue
			}
			includedEdges = append(includedEdges, edge)
			if !visited[fromNode] {
				queue = append(queue, fromNode)
			}
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

	// --- Phase 3: Path Selection + Topo Sort ---

	// 3a. Multiple-path resolution
	edgesByInput := map[inputKey][]Edge{}
	for _, edge := range includedEdges {
		toNode, toField, _ := splitRef(edge.To)
		key := inputKey{node: toNode, input: toField}
		edgesByInput[key] = append(edgesByInput[key], edge)
	}

	depth := computeDepth(includedNodes, includedEdges)

	var finalEdges []Edge
	for _, key := range sortedInputKeysFrom(edgesByInput) {
		edges := edgesByInput[key]
		if len(edges) <= 1 {
			finalEdges = append(finalEdges, edges...)
			continue
		}

		// Group by producer node
		producerEdges := map[string][]Edge{}
		for _, e := range edges {
			fromNode, _, _ := splitRef(e.From)
			producerEdges[fromNode] = append(producerEdges[fromNode], e)
		}

		if len(producerEdges) <= 1 {
			// Multiple edges from same producer — keep all
			finalEdges = append(finalEdges, edges...)
			continue
		}

		// Multiple producers for same input — pick one
		chosen := pickProducer(producerEdges, depth, g)
		finalEdges = append(finalEdges, producerEdges[chosen]...)

		var alternatives []string
		for producer := range producerEdges {
			if producer != chosen {
				alternatives = append(alternatives, producer)
			}
		}
		result.Decisions = append(result.Decisions, ChainDecision{
			Type:         DecisionPathChoice,
			Node:         key.node,
			Detail:       fmt.Sprintf("input %q: chose producer %q", key.input, chosen),
			Alternatives: alternatives,
		})
	}

	// 3a-ii. Satisfier resolution: pick one satisfier per requirement token.
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
			// No satisfier — will be caught by validation, skip here.
			continue
		}

		chosen := pickSatisfier(validCandidates, g, depth)

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
				// Check if this node is needed as a satisfier for another token
				// or as a data-flow source.
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
				if !neededElsewhere {
					// Check if any finalEdge references this node.
					for _, edge := range finalEdges {
						fromNode, _, _ := splitRef(edge.From)
						if fromNode == c {
							neededElsewhere = true
							break
						}
					}
				}
				// Also check if it's a goal or condition-required node.
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

	// Prune nodes that become unreachable after path selection.
	// Include requires edges in reachability.
	reachable := computeReachableWithRequires(opts.Goals, conditionRequired, finalEdges, requiresEdges)
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

	// 3b. Condition Before ordering
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

	// 3c. Topological sort
	sorted, err := chainTopoSort(includedNodes, finalEdges, virtualEdges)
	if err != nil {
		return nil, &ChainError{Errors: []string{err.Error()}}
	}

	result.Nodes = sorted
	result.Edges = finalEdges
	result.RequiresEdges = requiresEdges

	// 3d. Identify entry nodes (in-degree 0)
	inDegree := map[string]int{}
	for _, node := range sorted {
		inDegree[node] = 0
	}
	for _, edge := range finalEdges {
		fromNode, _, _ := splitRef(edge.From)
		toNode, _, _ := splitRef(edge.To)
		if includedNodes[fromNode] && includedNodes[toNode] {
			inDegree[toNode]++
		}
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

// computeDepth calculates BFS depth from entry nodes (in-degree 0) for each node.
func computeDepth(nodes map[string]bool, edges []Edge) map[string]int {
	adj := map[string]map[string]bool{}
	inDeg := map[string]int{}
	for node := range nodes {
		adj[node] = map[string]bool{}
		inDeg[node] = 0
	}
	for _, edge := range edges {
		fromNode, _, err1 := splitRef(edge.From)
		toNode, _, err2 := splitRef(edge.To)
		if err1 != nil || err2 != nil {
			continue
		}
		if !nodes[fromNode] || !nodes[toNode] {
			continue
		}
		if !adj[fromNode][toNode] {
			adj[fromNode][toNode] = true
			inDeg[toNode]++
		}
	}

	depth := map[string]int{}
	var queue []string
	for node := range nodes {
		if inDeg[node] == 0 {
			queue = append(queue, node)
			depth[node] = 0
		}
	}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		for neighbor := range adj[node] {
			inDeg[neighbor]--
			d := depth[node] + 1
			if existing, ok := depth[neighbor]; !ok || d < existing {
				depth[neighbor] = d
			}
			if inDeg[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	return depth
}

// pickProducer selects the best producer for an input.
// Prefers edges marked Preferred, then nodes marked Preferred, then shallowest depth.
func pickProducer(producerEdges map[string][]Edge, depth map[string]int, g *Graph) string {
	// 1. Edge-level preferred
	for producer, edges := range producerEdges {
		for _, e := range edges {
			if e.Preferred {
				return producer
			}
		}
	}

	// 2. Node-level preferred (reconciles with satisfier resolution)
	if g != nil {
		var preferredProducer string
		for producer := range producerEdges {
			if n := g.Nodes[producer]; n != nil && n.Preferred {
				if preferredProducer == "" || producer < preferredProducer {
					preferredProducer = producer
				}
			}
		}
		if preferredProducer != "" {
			return preferredProducer
		}
	}

	// 3. Shallowest depth, then alphabetical
	bestProducer := ""
	bestDepth := -1
	for producer := range producerEdges {
		d := depth[producer]
		if bestProducer == "" || d < bestDepth || (d == bestDepth && producer < bestProducer) {
			bestProducer = producer
			bestDepth = d
		}
	}
	return bestProducer
}

// pickSatisfier selects the best satisfier node for a requirement token.
// Prefers nodes marked Preferred, then shallowest depth, then alphabetical.
func pickSatisfier(candidates []string, g *Graph, depth map[string]int) string {
	// Check for preferred nodes.
	for _, c := range candidates {
		if n := g.Nodes[c]; n != nil && n.Preferred {
			return c
		}
	}

	// Pick shallowest depth, then alphabetical.
	best := candidates[0]
	bestDepth := depth[best]
	for _, c := range candidates[1:] {
		d := depth[c]
		if d < bestDepth || (d == bestDepth && c < best) {
			best = c
			bestDepth = d
		}
	}
	return best
}

// computeReachableWithRequires finds all nodes reachable by backward traversal
// from goals, considering both data-flow edges and requires edges.
func computeReachableWithRequires(goals []string, condRequired map[string]bool, edges []Edge, requiresEdges []RequiresEdge) map[string]bool {
	reverseAdj := map[string]map[string]bool{}
	for _, edge := range edges {
		fromNode, _, err1 := splitRef(edge.From)
		toNode, _, err2 := splitRef(edge.To)
		if err1 != nil || err2 != nil {
			continue
		}
		if reverseAdj[toNode] == nil {
			reverseAdj[toNode] = map[string]bool{}
		}
		reverseAdj[toNode][fromNode] = true
	}
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
func chainTopoSort(nodes map[string]bool, edges []Edge, virtual []orderingEdge) ([]string, error) {
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

	for _, edge := range edges {
		fromNode, _, err1 := splitRef(edge.From)
		toNode, _, err2 := splitRef(edge.To)
		if err1 != nil || err2 != nil {
			continue
		}
		addEdge(fromNode, toNode)
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

// sortedInputKeysFrom returns deterministically ordered keys for an inputKey map.
func sortedInputKeysFrom(m map[inputKey][]Edge) []inputKey {
	keys := make([]inputKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		key := keys[i]
		j := i - 1
		for j >= 0 && (keys[j].node > key.node || (keys[j].node == key.node && keys[j].input > key.input)) {
			keys[j+1] = keys[j]
			j--
		}
		keys[j+1] = key
	}
	return keys
}
