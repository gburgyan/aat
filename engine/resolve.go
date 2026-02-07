package engine

import (
	"encoding/json"
	"fmt"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/tidwall/gjson"
)

// ResolveInputs resolves all input values for a step by checking sources in
// priority order:
//  1. Graph edge targeting this input → upstream output from RunState
//  2. SELECT edge → select from upstream array using strategy, extract field via gjson
//  3. Plan StepValue.Default (via plan values or "from" with select)
//  4. Graph node Input.Default
//  5. Optional → skip (not included in result)
//  6. None → error: required input has no value
//
// Returns the resolved inputs and any selection decisions made.
func ResolveInputs(step plan.Step, node *graph.Node, g *graph.Graph, state *RunState) (map[string]any, []SelectionDecision, error) {
	// Build edge lookup for this node: inputName → edge
	edgeMap := make(map[string]graph.Edge)
	for _, edge := range g.Edges {
		toNode, toField, err := splitRef(edge.To)
		if err != nil {
			continue
		}
		if toNode == step.Node {
			edgeMap[toField] = edge
		}
	}

	inputs := make(map[string]any)
	var decisions []SelectionDecision

	// Dedup cache: keyed by "from|strategy|filter|index" → cached selectionResult
	dedupCache := make(map[string]*selectionResult)

	for _, input := range node.Inputs {
		val, decision, err := resolveInput(input, step, edgeMap, state, dedupCache)
		if err != nil {
			return nil, nil, fmt.Errorf("resolving input %q for node %q: %w", input.Name, step.Node, err)
		}
		if val != nil {
			inputs[input.Name] = val
		}
		if decision != nil {
			decisions = append(decisions, *decision)
		}
		// val == nil means optional input with no value → skip
	}

	return inputs, decisions, nil
}

// dedupKey builds a cache key for selection deduplication.
func dedupKey(fromNode, fromField string, sel *plan.SelectionConfig) string {
	if sel == nil {
		return fmt.Sprintf("%s|%s|||", fromNode, fromField)
	}
	return fmt.Sprintf("%s|%s|%s|%s|%d", fromNode, fromField, sel.Strategy, sel.Filter, sel.Index)
}

func resolveInput(input graph.Input, step plan.Step, edgeMap map[string]graph.Edge, state *RunState, dedupCache map[string]*selectionResult) (any, *SelectionDecision, error) {
	// 1. Check for graph edge
	if edge, ok := edgeMap[input.Name]; ok {
		fromNode, fromField, err := splitRef(edge.From)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid edge source %q: %w", edge.From, err)
		}

		if edge.Select {
			// 2. SELECT edge: select from upstream array using strategy
			val, decision, err := resolveSelectEdge(fromNode, fromField, input.Name, step, state, dedupCache)
			if err != nil {
				return nil, nil, err
			}
			return val, decision, nil
		}

		// Regular edge: get the output value directly
		val, err := state.GetOutput(fromNode, fromField)
		if err != nil {
			return nil, nil, fmt.Errorf("edge from %q: %w", edge.From, err)
		}
		return val, nil, nil
	}

	// 3. Plan StepValue.Default (via plan values or "from" with select)
	if sv, ok := step.Values[input.Name]; ok {
		if sv.From != "" && sv.Select != nil {
			// Plan-defined selection from upstream output
			fromNode, fromField, err := splitRef(sv.From)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid 'from' reference %q: %w", sv.From, err)
			}
			val, decision, err := resolveSelectValue(fromNode, fromField, input.Name, sv.Select, state, dedupCache)
			if err != nil {
				return nil, nil, err
			}
			return val, decision, nil
		}
		if sv.Default != nil {
			return sv.Default, nil, nil
		}
	}

	// 4. Graph node Input.Default
	if input.Default != nil {
		return input.Default, nil, nil
	}

	// 5. Optional → skip
	if input.Optional {
		return nil, nil, nil
	}

	// 6. No value → error
	return nil, nil, fmt.Errorf("required input has no value")
}

// resolveSelectEdge handles a SELECT edge: the source is an array, select using
// strategy, and if a plan-level select config exists for this input, extract
// the specified field.
func resolveSelectEdge(fromNode, fromField, inputName string, step plan.Step, state *RunState, dedupCache map[string]*selectionResult) (any, *SelectionDecision, error) {
	arr, err := getArrayFromState(fromNode, fromField, state)
	if err != nil {
		return nil, nil, err
	}

	// Get plan-level select config for this input (if any)
	var sel *plan.SelectionConfig
	if sv, ok := step.Values[inputName]; ok && sv.Select != nil {
		sel = sv.Select
	}

	// Check dedup cache
	key := dedupKey(fromNode, fromField, sel)
	cached, hasCached := dedupCache[key]

	var result *selectionResult
	if hasCached {
		result = cached
	} else {
		result, err = applySelection(arr, sel)
		if err != nil {
			return nil, nil, fmt.Errorf("select edge from %s.%s: %w", fromNode, fromField, err)
		}
		dedupCache[key] = result
	}

	decision := &SelectionDecision{
		InputName:     inputName,
		SourceNode:    fromNode,
		SourceField:   fromField,
		SourceSize:    len(arr),
		FilteredSize:  result.filteredSize,
		Strategy:      strategyName(sel),
		SelectedIndex: result.index,
	}
	if sel != nil && sel.Filter != "" {
		decision.FilterExpr = sel.Filter
	}

	// Extract field if specified
	if sel != nil && sel.Field != "" {
		val, err := extractField(result.element, sel.Field)
		if err != nil {
			return nil, nil, err
		}
		return val, decision, nil
	}

	return result.element, decision, nil
}

// resolveSelectValue handles plan-defined "from" + "select" value resolution.
func resolveSelectValue(fromNode, fromField, inputName string, sel *plan.SelectionConfig, state *RunState, dedupCache map[string]*selectionResult) (any, *SelectionDecision, error) {
	arr, err := getArrayFromState(fromNode, fromField, state)
	if err != nil {
		return nil, nil, err
	}

	// Check dedup cache
	key := dedupKey(fromNode, fromField, sel)
	cached, hasCached := dedupCache[key]

	var result *selectionResult
	if hasCached {
		result = cached
	} else {
		result, err = applySelection(arr, sel)
		if err != nil {
			return nil, nil, fmt.Errorf("select from %s.%s: %w", fromNode, fromField, err)
		}
		dedupCache[key] = result
	}

	decision := &SelectionDecision{
		InputName:     inputName,
		SourceNode:    fromNode,
		SourceField:   fromField,
		SourceSize:    len(arr),
		FilteredSize:  result.filteredSize,
		Strategy:      strategyName(sel),
		SelectedIndex: result.index,
	}
	if sel != nil && sel.Filter != "" {
		decision.FilterExpr = sel.Filter
	}

	if sel.Field != "" {
		val, err := extractField(result.element, sel.Field)
		if err != nil {
			return nil, nil, err
		}
		return val, decision, nil
	}
	return result.element, decision, nil
}

// strategyName returns the effective strategy name, defaulting to "first".
func strategyName(sel *plan.SelectionConfig) string {
	if sel == nil || sel.Strategy == "" {
		return "first"
	}
	return sel.Strategy
}

// getArrayFromState retrieves an output value and asserts it is a []any.
func getArrayFromState(nodeName, outputName string, state *RunState) ([]any, error) {
	val, err := state.GetOutput(nodeName, outputName)
	if err != nil {
		return nil, err
	}

	arr, ok := val.([]any)
	if !ok {
		return nil, fmt.Errorf("output %s.%s is not an array (got %T)", nodeName, outputName, val)
	}
	return arr, nil
}

// extractField marshals an element to JSON and extracts a field using gjson.
func extractField(element any, field string) (any, error) {
	data, err := json.Marshal(element)
	if err != nil {
		return nil, fmt.Errorf("marshaling element for field extraction: %w", err)
	}
	result := gjson.GetBytes(data, field)
	if !result.Exists() {
		return nil, fmt.Errorf("field %q not found in element", field)
	}
	return result.Value(), nil
}
