package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/llm"
	"github.com/gburgyan/aat/plan"
	"github.com/tidwall/gjson"
)

// ResolveContext provides optional context for enhanced value resolution,
// including expression evaluation, constraint checking, fallback pools,
// and LLM-assisted value selection.
type ResolveContext struct {
	Mode      config.ExecutionMode
	Now       time.Time
	EnvLookup func(string) string
	KB        *domain.KnowledgeBase // may be nil
	LLM       llm.Client            // may be nil
	Node      *graph.Node
}

// ResolveInputs resolves all input values for a step using the basic resolution
// chain. This is a backward-compatible wrapper around ResolveInputsWithContext
// with a nil ResolveContext.
func ResolveInputs(step plan.Step, node *graph.Node, g *graph.Graph, state *RunState) (map[string]any, []SelectionDecision, error) {
	inputs, decisions, _, err := ResolveInputsWithContext(context.Background(), step, node, g, state, nil)
	return inputs, decisions, err
}

// ResolveInputsWithContext resolves all input values for a step by checking
// sources in priority order:
//  1. Graph edge targeting this input → upstream output from RunState
//  2. SELECT edge → select from upstream array using strategy, extract field via gjson
//  3. Plan StepValue.Default (with expression evaluation, constraint checking, fallback pools)
//  4. Graph node Input.Default
//  5. Optional → skip (not included in result)
//  6. None → error: required input has no value
//
// When rctx is non-nil, expression evaluation ({{...}} templates), constraint
// checking, and fallback pool iteration are activated at priority 3. When rctx
// is nil, the behavior is identical to the basic ResolveInputs.
func ResolveInputsWithContext(ctx context.Context, step plan.Step, node *graph.Node, g *graph.Graph, state *RunState, rctx *ResolveContext) (map[string]any, []SelectionDecision, []ValueResolution, error) {
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
	var resolutions []ValueResolution

	// Dedup cache: keyed by "from|strategy|filter|index" → cached selectionResult
	dedupCache := make(map[string]*selectionResult)

	// Build expression context if rctx is provided
	var ectx *plan.ExprContext
	if rctx != nil {
		ectx = &plan.ExprContext{
			Now:    rctx.Now,
			Env:    rctx.EnvLookup,
			Values: make(map[string]any),
		}
	}

	for _, input := range node.Inputs {
		val, decision, resolution, err := resolveInputEnhanced(ctx, input, step, g, edgeMap, state, dedupCache, rctx, ectx, inputs)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("resolving input %q for node %q: %w", input.Name, step.Node, err)
		}
		if val != nil {
			val = coerceValue(val, input.Type)
			inputs[input.Name] = val
			// Update expression context so later inputs can reference this one
			if ectx != nil {
				ectx.Values[input.Name] = val
			}
		}
		if decision != nil {
			decisions = append(decisions, *decision)
		}
		if resolution != nil {
			// Update FinalValue with coerced value
			resolution.FinalValue = inputs[input.Name]
			resolutions = append(resolutions, *resolution)
		}
		// val == nil means optional input with no value → skip
	}

	return inputs, decisions, resolutions, nil
}

// dedupKey builds a cache key for selection deduplication.
func dedupKey(fromNode, fromField string, sel *plan.SelectionConfig) string {
	if sel == nil {
		return fmt.Sprintf("%s|%s|||", fromNode, fromField)
	}
	return fmt.Sprintf("%s|%s|%s|%s|%d", fromNode, fromField, sel.Strategy, sel.Filter, sel.Index)
}

// resolveInputEnhanced resolves a single input with optional enhanced features.
// When rctx/ectx are nil, it behaves identically to the original resolveInput.
func resolveInputEnhanced(ctx context.Context, input graph.Input, step plan.Step, g *graph.Graph, edgeMap map[string]graph.Edge, state *RunState, dedupCache map[string]*selectionResult, rctx *ResolveContext, ectx *plan.ExprContext, resolvedInputs map[string]any) (any, *SelectionDecision, *ValueResolution, error) {
	// 1. Check for graph edge
	if edge, ok := edgeMap[input.Name]; ok {
		fromNode, fromField, err := splitRef(edge.From)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("invalid edge source %q: %w", edge.From, err)
		}

		if edge.Select {
			// 2. SELECT edge: select from upstream array using strategy
			val, decision, err := resolveSelectEdge(ctx, fromNode, fromField, input.Name, step, g, state, dedupCache, rctx)
			if err != nil {
				return nil, nil, nil, err
			}
			res := &ValueResolution{
				InputName:  input.Name,
				Source:     "select_edge",
				FinalValue: val,
				FromStep:   fromNode,
				FromOutput: fromField,
				PoolIndex:  -1,
			}
			return val, decision, res, nil
		}

		// Regular edge: get the output value directly
		// Edge-resolved values are NOT subject to expression evaluation.
		val, err := state.GetOutput(fromNode, fromField)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("edge from %q: %w", edge.From, err)
		}
		res := &ValueResolution{
			InputName:  input.Name,
			Source:     "edge",
			FinalValue: val,
			FromStep:   fromNode,
			FromOutput: fromField,
			PoolIndex:  -1,
		}
		return val, nil, res, nil
	}

	// 3. Plan StepValue (via plan values or "from" with select)
	if sv, ok := step.Values[input.Name]; ok {
		if sv.From != "" && sv.Select != nil {
			// Plan-defined selection from upstream output
			fromNode, fromField, err := splitRef(sv.From)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("invalid 'from' reference %q: %w", sv.From, err)
			}
			val, decision, err := resolveSelectValue(ctx, fromNode, fromField, input.Name, sv.Select, g, state, dedupCache, rctx)
			if err != nil {
				return nil, nil, nil, err
			}
			res := &ValueResolution{
				InputName:  input.Name,
				Source:     "select_edge",
				FinalValue: val,
				FromStep:   fromNode,
				FromOutput: fromField,
				PoolIndex:  -1,
			}
			return val, decision, res, nil
		}
		if sv.Default != nil {
			// Enhanced path: evaluate expressions, check constraints, try fallback pool
			if rctx != nil && ectx != nil {
				return resolveWithFallback(ctx, sv, input, *ectx, resolvedInputs, rctx)
			}
			// Basic path: return raw default
			res := &ValueResolution{
				InputName:  input.Name,
				Source:     "plan_default",
				RawValue:   sv.Default,
				FinalValue: sv.Default,
				PoolIndex:  -1,
			}
			return sv.Default, nil, res, nil
		}
		// StepValue exists but no Default and no From+Select:
		// try fallback pool if enhanced context is available
		if rctx != nil && ectx != nil && len(sv.FallbackPool) > 0 {
			return resolveWithFallback(ctx, sv, input, *ectx, resolvedInputs, rctx)
		}
	}

	// 4. Graph node Input.Default
	if input.Default != nil {
		res := &ValueResolution{
			InputName:  input.Name,
			Source:     "graph_default",
			FinalValue: input.Default,
			PoolIndex:  -1,
		}
		return input.Default, nil, res, nil
	}

	// 5. Optional → skip
	if input.Optional {
		res := &ValueResolution{
			InputName: input.Name,
			Source:    "optional_skip",
			PoolIndex: -1,
		}
		return nil, nil, res, nil
	}

	// 6. No value → error
	return nil, nil, nil, fmt.Errorf("required input has no value")
}

// resolveSelectEdge handles a SELECT edge: the source is an array, select using
// strategy, and if a plan-level select config exists for this input, extract
// the specified field.
func resolveSelectEdge(ctx context.Context, fromNode, fromField, inputName string, step plan.Step, g *graph.Graph, state *RunState, dedupCache map[string]*selectionResult, rctx *ResolveContext) (any, *SelectionDecision, error) {
	arr, err := getArrayFromState(fromNode, fromField, state)
	if err != nil {
		return nil, nil, err
	}

	// Get plan-level select config for this input (if any)
	var sel *plan.SelectionConfig
	if sv, ok := step.Values[inputName]; ok && sv.Select != nil {
		sel = sv.Select
	}

	// LLM selection strategy
	if sel != nil && sel.Strategy == "llm" {
		result, llmRec, llmErr := llmSelectElement(ctx, rctx, arr, sel, inputName, g, fromNode, fromField)
		if llmErr != nil {
			return nil, nil, fmt.Errorf("select edge from %s.%s: %w", fromNode, fromField, llmErr)
		}
		decision := &SelectionDecision{
			InputName:     inputName,
			SourceNode:    fromNode,
			SourceField:   fromField,
			SourceSize:    len(arr),
			FilteredSize:  result.filteredSize,
			Strategy:      "llm",
			SelectedIndex: result.index,
			LLMCall:       llmRec,
		}
		// Extract field if specified
		if sel.Field != "" {
			resolvedField := resolveElementFieldPath(g, fromNode, fromField, sel.Field)
			val, exErr := extractField(result.element, resolvedField)
			if exErr != nil {
				return nil, nil, exErr
			}
			return val, decision, nil
		}
		return result.element, decision, nil
	}

	// Resolve elementField names to gjson paths for applySelection (min/max SortField)
	resolvedSel := resolveSelectionFields(sel, g, fromNode, fromField)

	// Check dedup cache
	key := dedupKey(fromNode, fromField, sel)
	cached, hasCached := dedupCache[key]

	var result *selectionResult
	if hasCached {
		result = cached
	} else {
		result, err = applySelection(arr, resolvedSel)
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

	// Extract field if specified — use resolved gjson path
	if resolvedSel != nil && resolvedSel.Field != "" {
		val, err := extractField(result.element, resolvedSel.Field)
		if err != nil {
			return nil, nil, err
		}
		return val, decision, nil
	}

	return result.element, decision, nil
}

// resolveSelectValue handles plan-defined "from" + "select" value resolution.
func resolveSelectValue(ctx context.Context, fromNode, fromField, inputName string, sel *plan.SelectionConfig, g *graph.Graph, state *RunState, dedupCache map[string]*selectionResult, rctx *ResolveContext) (any, *SelectionDecision, error) {
	arr, err := getArrayFromState(fromNode, fromField, state)
	if err != nil {
		return nil, nil, err
	}

	// LLM selection strategy
	if sel != nil && sel.Strategy == "llm" {
		result, llmRec, llmErr := llmSelectElement(ctx, rctx, arr, sel, inputName, g, fromNode, fromField)
		if llmErr != nil {
			return nil, nil, fmt.Errorf("select from %s.%s: %w", fromNode, fromField, llmErr)
		}
		decision := &SelectionDecision{
			InputName:     inputName,
			SourceNode:    fromNode,
			SourceField:   fromField,
			SourceSize:    len(arr),
			FilteredSize:  result.filteredSize,
			Strategy:      "llm",
			SelectedIndex: result.index,
			LLMCall:       llmRec,
		}
		if sel.Field != "" {
			resolvedField := resolveElementFieldPath(g, fromNode, fromField, sel.Field)
			val, exErr := extractField(result.element, resolvedField)
			if exErr != nil {
				return nil, nil, exErr
			}
			return val, decision, nil
		}
		return result.element, decision, nil
	}

	// Resolve elementField names to gjson paths for applySelection (min/max SortField)
	resolvedSel := resolveSelectionFields(sel, g, fromNode, fromField)

	// Check dedup cache
	key := dedupKey(fromNode, fromField, sel)
	cached, hasCached := dedupCache[key]

	var result *selectionResult
	if hasCached {
		result = cached
	} else {
		result, err = applySelection(arr, resolvedSel)
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

	// Use resolved gjson path for field extraction
	if resolvedSel != nil && resolvedSel.Field != "" {
		val, err := extractField(result.element, resolvedSel.Field)
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

// resolveElementFieldPath looks up a field reference against the graph's
// elementFields for a given source output. If fieldRef matches an elementField
// name, returns its EffectivePath (the gjson path). Otherwise returns fieldRef
// unchanged for backward compatibility with raw gjson paths.
func resolveElementFieldPath(g *graph.Graph, sourceNode, sourceOutput, fieldRef string) string {
	if g == nil {
		return fieldRef
	}
	node := g.Nodes[sourceNode]
	if node == nil {
		return fieldRef
	}
	for _, out := range node.Outputs {
		if out.Name == sourceOutput {
			for _, ef := range out.ElementFields {
				if ef.Name == fieldRef {
					return ef.EffectivePath()
				}
			}
			break
		}
	}
	return fieldRef
}

// resolveSelectionFields returns a shallow copy of sel with Field and SortField
// resolved from elementField names to gjson paths using the graph. The original
// sel is not modified. Returns nil if sel is nil.
func resolveSelectionFields(sel *plan.SelectionConfig, g *graph.Graph, sourceNode, sourceOutput string) *plan.SelectionConfig {
	if sel == nil {
		return nil
	}
	resolved := *sel
	if resolved.Field != "" {
		resolved.Field = resolveElementFieldPath(g, sourceNode, sourceOutput, resolved.Field)
	}
	if resolved.SortField != "" {
		resolved.SortField = resolveElementFieldPath(g, sourceNode, sourceOutput, resolved.SortField)
	}
	return &resolved
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

// evaluateValue applies expression evaluation if the raw value is a string
// containing {{...}} templates. Returns the value unchanged if no expressions
// are found or if raw is not a string.
func evaluateValue(raw any, ectx plan.ExprContext) (any, error) {
	return plan.EvalExpr(raw, ectx)
}

// checkConstraint evaluates a constraint predicate against a candidate value.
// The candidate is available as "value" in the predicate context, and all
// previously resolved inputs are also available. Returns true if the constraint
// passes or if the constraint string is empty.
func checkConstraint(constraint string, candidate any, resolvedInputs map[string]any) (bool, error) {
	if constraint == "" {
		return true, nil
	}
	ctx := make(map[string]any, len(resolvedInputs)+1)
	for k, v := range resolvedInputs {
		ctx[k] = v
	}
	ctx["value"] = candidate
	return plan.EvalPredicate(constraint, ctx)
}

// resolveWithFallback tries the StepValue default (with expression evaluation
// and constraint checking), then iterates the fallback pool. If all deterministic
// values are exhausted and mode allows, delegates to the LLM. Returns the first
// value that passes the constraint. FallbackStrategy controls iteration order:
// nil/"sequential" = in order, "random" = shuffled.
func resolveWithFallback(ctx context.Context, sv plan.StepValue, input graph.Input, ectx plan.ExprContext, resolvedInputs map[string]any, rctx *ResolveContext) (any, *SelectionDecision, *ValueResolution, error) {
	inputName := input.Name
	var tried []any

	// Try the default value first
	if sv.Default != nil {
		evaluated, err := evaluateValue(sv.Default, ectx)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("evaluating expression for %q: %w", inputName, err)
		}
		passes, err := checkConstraint(sv.Constraint, evaluated, resolvedInputs)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("checking constraint for %q: %w", inputName, err)
		}
		if passes {
			source := "plan_default"
			if isExpression(sv.Default) {
				source = "expression"
			}
			res := &ValueResolution{
				InputName:    inputName,
				Source:       source,
				RawValue:     sv.Default,
				FinalValue:   evaluated,
				Constraint:   sv.Constraint,
				ConstraintOK: true,
				PoolIndex:    -1,
				PoolSize:     len(sv.FallbackPool),
			}
			if isExpression(sv.Default) {
				if s, ok := sv.Default.(string); ok {
					res.Expression = s
				}
			}
			return evaluated, nil, res, nil
		}
		// Default failed constraint — track and fall through to pool
		tried = append(tried, evaluated)
	}

	// Try the fallback pool
	if len(sv.FallbackPool) > 0 {
		pool := make([]any, len(sv.FallbackPool))
		copy(pool, sv.FallbackPool)

		// Track original indices for resolution record
		indices := make([]int, len(pool))
		for i := range pool {
			indices[i] = i
		}

		if sv.FallbackStrategy != nil && *sv.FallbackStrategy == "random" {
			rand.Shuffle(len(pool), func(i, j int) {
				pool[i], pool[j] = pool[j], pool[i]
				indices[i], indices[j] = indices[j], indices[i]
			})
		}

		for pi, candidate := range pool {
			evaluated, err := evaluateValue(candidate, ectx)
			if err != nil {
				tried = append(tried, candidate)
				continue // skip values that fail expression evaluation
			}
			passes, err := checkConstraint(sv.Constraint, evaluated, resolvedInputs)
			if err != nil {
				tried = append(tried, evaluated)
				continue // skip values that fail constraint evaluation
			}
			if passes {
				res := &ValueResolution{
					InputName:    inputName,
					Source:       "fallback_pool",
					RawValue:     candidate,
					FinalValue:   evaluated,
					Constraint:   sv.Constraint,
					ConstraintOK: true,
					PoolIndex:    indices[pi],
					PoolSize:     len(sv.FallbackPool),
					Tried:        tried,
				}
				if isExpression(candidate) {
					if s, ok := candidate.(string); ok {
						res.Expression = s
					}
				}
				return evaluated, nil, res, nil
			}
			tried = append(tried, evaluated)
		}

		// All pool values failed — try LLM if mode allows
		val, llmRec, err := llmSelectValue(ctx, rctx, input, sv, resolvedInputs)
		if err == nil {
			res := &ValueResolution{
				InputName:    inputName,
				Source:       "llm",
				FinalValue:   val,
				Constraint:   sv.Constraint,
				ConstraintOK: true,
				PoolIndex:    -1,
				PoolSize:     len(sv.FallbackPool),
				Tried:        tried,
				LLMCall:      llmRec,
			}
			return val, nil, res, nil
		}

		// LLM also failed (or mode doesn't allow it) — return original pool-exhausted error
		return nil, nil, nil, fmt.Errorf("all fallback pool values for %q failed constraint %q", inputName, sv.Constraint)
	}

	// No pool — try LLM if default failed constraint
	if sv.Default != nil {
		// Default existed but failed constraint, try LLM
		val, llmRec, err := llmSelectValue(ctx, rctx, input, sv, resolvedInputs)
		if err == nil {
			res := &ValueResolution{
				InputName:    inputName,
				Source:       "llm",
				FinalValue:   val,
				Constraint:   sv.Constraint,
				ConstraintOK: true,
				PoolIndex:    -1,
				Tried:        tried,
				LLMCall:      llmRec,
			}
			return val, nil, res, nil
		}
		return nil, nil, nil, fmt.Errorf("default value for %q failed constraint %q and no fallback pool is configured", inputName, sv.Constraint)
	}

	// No default and no pool
	return nil, nil, nil, fmt.Errorf("required input %q has no value", inputName)
}

// isExpression returns true if the value is a string containing {{...}} templates.
func isExpression(v any) bool {
	s, ok := v.(string)
	return ok && plan.ContainsExpr(s)
}

// coerceValue normalizes a resolved value based on the graph input's declared type.
// For example, a datetime string "2026-02-16T00:00:00Z" is truncated to "2026-02-16"
// when the input type is "date". On any parse error the original value is returned
// unchanged so the downstream API can report the mismatch.
func coerceValue(val any, inputType string) any {
	switch strings.ToLower(inputType) {
	case "date":
		switch v := val.(type) {
		case string:
			if strings.Contains(v, "T") {
				t, err := time.Parse(time.RFC3339, v)
				if err != nil {
					return val
				}
				return t.Format("2006-01-02")
			}
			return val
		case time.Time:
			return v.Format("2006-01-02")
		}

	case "datetime":
		switch v := val.(type) {
		case time.Time:
			return v.Format(time.RFC3339)
		}

	case "integer":
		switch v := val.(type) {
		case string:
			i, err := strconv.Atoi(v)
			if err != nil {
				return val
			}
			return i
		case float64:
			return int(v)
		}

	case "boolean":
		if s, ok := val.(string); ok {
			b, err := strconv.ParseBool(s)
			if err != nil {
				return val
			}
			return b
		}

	case "float":
		if s, ok := val.(string); ok {
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return val
			}
			return f
		}
	}

	return val
}
