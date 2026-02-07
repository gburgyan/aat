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
//  2. SELECT edge → first element of upstream array, extract field via gjson
//  3. Plan StepValue.Default
//  4. Graph node Input.Default
//  5. Optional → skip (not included in result)
//  6. None → error: required input has no value
func ResolveInputs(step plan.Step, node *graph.Node, g *graph.Graph, state *RunState) (map[string]any, error) {
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

	for _, input := range node.Inputs {
		val, err := resolveInput(input, step, edgeMap, state)
		if err != nil {
			return nil, fmt.Errorf("resolving input %q for node %q: %w", input.Name, step.Node, err)
		}
		if val != nil {
			inputs[input.Name] = val
		}
		// val == nil means optional input with no value → skip
	}

	return inputs, nil
}

func resolveInput(input graph.Input, step plan.Step, edgeMap map[string]graph.Edge, state *RunState) (any, error) {
	// 1. Check for graph edge
	if edge, ok := edgeMap[input.Name]; ok {
		fromNode, fromField, err := splitRef(edge.From)
		if err != nil {
			return nil, fmt.Errorf("invalid edge source %q: %w", edge.From, err)
		}

		if edge.Select {
			// 2. SELECT edge: extract from first array element
			return resolveSelectEdge(fromNode, fromField, input.Name, step, state)
		}

		// Regular edge: get the output value directly
		val, err := state.GetOutput(fromNode, fromField)
		if err != nil {
			return nil, fmt.Errorf("edge from %q: %w", edge.From, err)
		}
		return val, nil
	}

	// 3. Plan StepValue.Default (via plan values or "from" with select)
	if sv, ok := step.Values[input.Name]; ok {
		if sv.From != "" && sv.Select != nil {
			// Plan-defined selection from upstream output
			fromNode, fromField, err := splitRef(sv.From)
			if err != nil {
				return nil, fmt.Errorf("invalid 'from' reference %q: %w", sv.From, err)
			}
			return resolveSelectValue(fromNode, fromField, sv.Select, state)
		}
		if sv.Default != nil {
			return sv.Default, nil
		}
	}

	// 4. Graph node Input.Default
	if input.Default != nil {
		return input.Default, nil
	}

	// 5. Optional → skip
	if input.Optional {
		return nil, nil
	}

	// 6. No value → error
	return nil, fmt.Errorf("required input has no value")
}

// resolveSelectEdge handles a SELECT edge: the source is an array, take the
// first element, and if a plan-level select config exists for this input,
// extract the specified field.
func resolveSelectEdge(fromNode, fromField, inputName string, step plan.Step, state *RunState) (any, error) {
	arr, err := getArrayFromState(fromNode, fromField, state)
	if err != nil {
		return nil, err
	}

	if len(arr) == 0 {
		return nil, fmt.Errorf("select edge from %s.%s: array is empty", fromNode, fromField)
	}

	element := arr[0]

	// Check if there's a plan-level select config for this input
	if sv, ok := step.Values[inputName]; ok && sv.Select != nil && sv.Select.Field != "" {
		return extractField(element, sv.Select.Field)
	}

	return element, nil
}

// resolveSelectValue handles plan-defined "from" + "select" value resolution.
func resolveSelectValue(fromNode, fromField string, sel *plan.SelectionConfig, state *RunState) (any, error) {
	arr, err := getArrayFromState(fromNode, fromField, state)
	if err != nil {
		return nil, err
	}

	if len(arr) == 0 {
		return nil, fmt.Errorf("select from %s.%s: array is empty", fromNode, fromField)
	}

	// Task 6: only "first" strategy supported
	element := arr[0]

	if sel.Field != "" {
		return extractField(element, sel.Field)
	}
	return element, nil
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
