package intent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// TargetedResponse is the JSON structure returned by the targeted phase 2 LLM call.
type TargetedResponse struct {
	Values       map[string]any                 `json:"values"`
	Selections   map[string]TargetedSelection   `json:"selections"`
	Assertions   map[string][]TargetedAssertion `json:"assertions"`
	Descriptions map[string]string              `json:"descriptions"`
	WrongPlan    *WrongPlanSignal               `json:"wrongPlan,omitempty"`
}

// WrongPlanSignal is returned by the Call 2 LLM when it determines the selected
// workflow fundamentally doesn't match the user's intent. This triggers a
// re-selection via Call 1 with feedback.
type WrongPlanSignal struct {
	Reason    string `json:"reason"`
	Suggested string `json:"suggested,omitempty"`
}

// TargetedSelection represents a selection strategy override from the LLM.
type TargetedSelection struct {
	Strategy  string `json:"strategy,omitempty"`
	Filter    string `json:"filter,omitempty"`
	SortField string `json:"sortField,omitempty"`
	Index     int    `json:"index,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
}

// TargetedAssertion represents a single mechanical assertion from the LLM.
type TargetedAssertion struct {
	Type   string `json:"type"`
	Expect any    `json:"expect,omitempty"`
	Path   string `json:"path,omitempty"`
	Value  any    `json:"value,omitempty"`
	Expr   string `json:"expr,omitempty"`
}

// InputContext provides rich per-input context for the LLM prompt.
type InputContext struct {
	StepID         string
	InputName      string
	InputType      string
	NodeDesc       string
	DomainType     string   // from TypeDef.Description
	Format         string   // from TypeDef.Format
	PoolValues     []string // sample values from value pool
	GraphConstr    string   // graph-level constraint annotation
	IsDate         bool
	IsConfigurable bool   // true if graph.Input.Configurable
	CurrentDefault string // template default value, if any (e.g., "DEN")

	// Validation metadata from graph.Input.Constraints — used by validateTargetedResponse.
	ConstraintPattern   string // regex pattern (e.g., "^[A-Z]{3}$")
	ConstraintMinLength *int   // minimum string length
	ConstraintMaxLength *int   // maximum string length
}

// SelectionContext provides per-selection context for the LLM prompt.
type SelectionContext struct {
	StepID          string
	SelectionName   string
	Source          string   // "searchFlights.catalogOfferings"
	CurrentStrategy string
	ElementFields   []string // "offeringId (string), carrier (string), ..."
	FeedsInto       []string // inputs that reference this selection
	IsNamed         bool     // true for named selections, false for inline from+select
}

// buildInputContexts walks unfed inputs in the skeleton and resolves domain
// type/pool/concepts per input.
func buildInputContexts(skeleton *plan.Plan, g *graph.Graph, kb *domain.KnowledgeBase) []InputContext {
	var contexts []InputContext

	for _, step := range skeleton.Execution.Steps {
		node := g.Nodes[step.Node]
		if node == nil {
			continue
		}
		for _, inp := range node.Inputs {
			if isInputFed(step, inp) {
				continue
			}

			ic := InputContext{
				StepID:         step.StepID(),
				InputName:      inp.Name,
				InputType:      inp.Type,
				NodeDesc:       node.Description,
				IsConfigurable: inp.Configurable,
			}

			// Check for existing template default value.
			if sv, exists := step.Values[inp.Name]; exists && sv.Default != nil {
				ic.CurrentDefault = fmt.Sprintf("%v", sv.Default)
			}

			// For configurable inputs, also show graph-level default.
			if ic.CurrentDefault == "" && inp.Default != nil {
				ic.CurrentDefault = fmt.Sprintf("%v", inp.Default)
			}

			// Check for date type.
			if inp.Type == "date" || inp.Type == "datetime" {
				ic.IsDate = true
			}

			// Graph-level constraint annotation + validation metadata.
			if inp.Constraints != nil {
				if inp.Constraints.Description != "" {
					ic.GraphConstr = inp.Constraints.Description
				}
				if inp.Constraints.Pattern != "" {
					if ic.GraphConstr != "" {
						ic.GraphConstr += " "
					}
					ic.GraphConstr += fmt.Sprintf("(pattern: %s)", inp.Constraints.Pattern)
					ic.ConstraintPattern = inp.Constraints.Pattern
				}
				ic.ConstraintMinLength = inp.Constraints.MinLength
				ic.ConstraintMaxLength = inp.Constraints.MaxLength
			}

			// Domain enrichment from KB.
			if kb != nil {
				td := kb.TypeForField(inp.Type)
				if td != nil {
					ic.DomainType = td.Description
					ic.Format = td.Format
					if td.Pool != "" {
						ic.PoolValues = kb.SampleValues(td.Pool, 8)
					}
				}

				// Check concepts that apply to this input.
				concepts := kb.ConceptsForField(inp.Name)
				for _, c := range concepts {
					if c.Description != "" {
						ic.DomainType = c.Description
					}
					if c.Constraint != "" {
						ic.Format = c.Constraint
					}
					// Connect concept → TypeDef → pool.
					// Concept name often matches TypeDef name (e.g., "airportCode").
					if len(ic.PoolValues) == 0 {
						if ctd := kb.GetType(c.Name); ctd != nil && ctd.Pool != "" {
							ic.PoolValues = kb.SampleValues(ctd.Pool, 8)
							if ic.Format == "" {
								ic.Format = ctd.Format
							}
						}
					}
					// Include concept examples as fallback pool values.
					if len(c.Examples) > 0 && len(ic.PoolValues) == 0 {
						var examples []string
						for _, vals := range c.Examples {
							examples = append(examples, vals...)
						}
						if len(examples) > 8 {
							examples = examples[:8]
						}
						ic.PoolValues = examples
					}
				}
			}

				contexts = append(contexts, ic)
		}
	}

	return contexts
}

// buildSelectionContexts walks named and inline selections in the skeleton,
// collecting element fields and downstream feeds.
func buildSelectionContexts(skeleton *plan.Plan, g *graph.Graph) []SelectionContext {
	var contexts []SelectionContext

	for _, step := range skeleton.Execution.Steps {
		// Named selections.
		for selName, sel := range step.Selections {
			sc := SelectionContext{
				StepID:          step.StepID(),
				SelectionName:   selName,
				Source:          sel.From,
				CurrentStrategy: sel.Strategy,
				IsNamed:         true,
			}

			// Resolve element fields from graph.
			sc.ElementFields = resolveElementFields(g, sel.From)

			// Find inputs that reference this selection.
			for inputName, sv := range step.Values {
				if sv.FromSelection != "" {
					selRef, _ := plan.ParseFromSelection(sv.FromSelection)
					if selRef == selName {
						sc.FeedsInto = append(sc.FeedsInto, inputName)
					}
				}
			}

			contexts = append(contexts, sc)
		}

		// Inline selections (from + select on a value).
		for inputName, sv := range step.Values {
			if sv.From != "" && sv.Select != nil {
				sc := SelectionContext{
					StepID:          step.StepID(),
					SelectionName:   inputName,
					Source:          sv.From,
					CurrentStrategy: sv.Select.Strategy,
					IsNamed:         false,
				}
				sc.ElementFields = resolveElementFields(g, sv.From)
				sc.FeedsInto = []string{inputName}
				contexts = append(contexts, sc)
			}
		}
	}

	return contexts
}

// resolveElementFields resolves the element fields for a source reference like
// "searchFlights.catalogOfferings" from the graph.
func resolveElementFields(g *graph.Graph, source string) []string {
	parts := strings.SplitN(source, ".", 2)
	if len(parts) != 2 {
		return nil
	}
	node := g.Nodes[parts[0]]
	if node == nil {
		return nil
	}
	for _, out := range node.Outputs {
		if out.Name == parts[1] {
			var fields []string
			for _, ef := range out.ElementFields {
				fields = append(fields, fmt.Sprintf("%s (%s)", ef.Name, ef.Type))
			}
			return fields
		}
	}
	return nil
}

// parseTargetedResponse strips JSON fencing and unmarshals the LLM response
// into a TargetedResponse.
func parseTargetedResponse(content string) (*TargetedResponse, error) {
	content = strings.TrimSpace(content)
	content = stripJSONFencing(content)

	var resp TargetedResponse
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return nil, fmt.Errorf("parsing targeted response: %w", err)
	}

	// Initialize nil maps to empty.
	if resp.Values == nil {
		resp.Values = map[string]any{}
	}
	if resp.Selections == nil {
		resp.Selections = map[string]TargetedSelection{}
	}
	if resp.Assertions == nil {
		resp.Assertions = map[string][]TargetedAssertion{}
	}
	if resp.Descriptions == nil {
		resp.Descriptions = map[string]string{}
	}

	return &resp, nil
}

// applyTargetedResponse applies the LLM's targeted decisions to the skeleton.
// It sets values for unfed inputs, overrides selection strategies, adds
// assertions, and sets step descriptions. Wired inputs (not in unfedSet)
// are rejected to prevent the LLM from shadowing auto-wired edges.
func applyTargetedResponse(skeleton *plan.Plan, resp *TargetedResponse, unfedSet map[string]bool) {
	// Build step index by ID.
	stepByID := map[string]*plan.Step{}
	for i := range skeleton.Execution.Steps {
		stepByID[skeleton.Execution.Steps[i].StepID()] = &skeleton.Execution.Steps[i]
	}

	// Apply values.
	for key, val := range resp.Values {
		stepID, inputName := splitStepInput(key)
		step, ok := stepByID[stepID]
		if !ok {
			continue
		}

		// Only accept values for unfed inputs.
		if !unfedSet[key] {
			continue
		}

		if step.Values == nil {
			step.Values = map[string]plan.StepValue{}
		}
		step.Values[inputName] = plan.StepValue{Default: val}
	}

	// Apply selection overrides.
	for key, sel := range resp.Selections {
		stepID, selName := splitStepInput(key)
		step, ok := stepByID[stepID]
		if !ok {
			continue
		}

		// Try named selections first.
		if step.Selections != nil {
			if existing, exists := step.Selections[selName]; exists {
				if sel.Strategy != "" {
					existing.Strategy = sel.Strategy
				}
				if sel.Filter != "" {
					existing.Filter = sel.Filter
				}
				if sel.SortField != "" {
					existing.SortField = sel.SortField
				}
				if sel.Prompt != "" {
					existing.Prompt = sel.Prompt
				}
				if sel.Index != 0 {
					existing.Index = sel.Index
				}
				step.Selections[selName] = existing
				continue
			}
		}

		// Try inline selection (from+select on a value).
		if sv, exists := step.Values[selName]; exists && sv.Select != nil {
			if sel.Strategy != "" {
				sv.Select.Strategy = sel.Strategy
			}
			if sel.Filter != "" {
				sv.Select.Filter = sel.Filter
			}
			if sel.SortField != "" {
				sv.Select.SortField = sel.SortField
			}
			if sel.Prompt != "" {
				sv.Select.Prompt = sel.Prompt
			}
			if sel.Index != 0 {
				sv.Select.Index = sel.Index
			}
			step.Values[selName] = sv
		}
	}

	// Apply assertions (sanitized to filter out LLM mistakes).
	for stepID, assertions := range resp.Assertions {
		step, ok := stepByID[stepID]
		if !ok {
			continue
		}

		sanitized := sanitizeAssertions(assertions)
		if len(sanitized) == 0 {
			continue
		}

		if step.Assertions == nil {
			step.Assertions = &plan.Assertions{}
		}
		step.Assertions.Mechanical = append(step.Assertions.Mechanical, sanitized...)
	}

	// Apply descriptions.
	for stepID, desc := range resp.Descriptions {
		step, ok := stepByID[stepID]
		if !ok {
			continue
		}
		step.Description = desc
	}
}

// sanitizeAssertions validates and fixes LLM-generated assertions, filtering
// out any that are malformed or use unknown types. This catches common LLM
// mistakes like putting a predicate expression in the "expect" field instead
// of "expr", or omitting required fields.
func sanitizeAssertions(assertions []TargetedAssertion) []plan.MechanicalAssertion {
	validTypes := map[string]bool{
		"status":      true,
		"fieldExists": true,
		"fieldEquals": true,
		"predicate":   true,
		"schema":      true,
	}

	var result []plan.MechanicalAssertion
	for _, a := range assertions {
		// Skip unknown assertion types.
		if !validTypes[a.Type] {
			continue
		}

		switch a.Type {
		case "status":
			// Status requires an expect value.
			if a.Expect == nil {
				continue
			}

		case "fieldExists":
			// fieldExists requires a non-empty path.
			if a.Path == "" {
				continue
			}
			// Strip jsonpath "$." prefix — LLMs often add it.
			a.Path = stripJSONPathPrefix(a.Path)

		case "fieldEquals":
			// fieldEquals requires a path.
			if a.Path == "" {
				continue
			}
			a.Path = stripJSONPathPrefix(a.Path)

		case "predicate":
			// Common LLM mistake: putting the expression in "expect" instead of "expr".
			if a.Expr == "" {
				if s, ok := a.Expect.(string); ok && s != "" {
					a.Expr = s
					a.Expect = nil
				} else {
					// No expression at all — skip.
					continue
				}
			}
			// Validate the predicate expression can be parsed.
			if err := plan.ValidatePredicate(a.Expr); err != nil {
				continue
			}

		case "schema":
			// Schema requires an expect value (schema name or path).
			if a.Expect == nil {
				continue
			}
		}

		result = append(result, plan.MechanicalAssertion{
			Type:   a.Type,
			Expect: a.Expect,
			Path:   a.Path,
			Value:  a.Value,
			Expr:   a.Expr,
		})
	}
	return result
}

// stripJSONPathPrefix removes the "$." prefix that LLMs often add to field paths.
func stripJSONPathPrefix(path string) string {
	if strings.HasPrefix(path, "$.") {
		return path[2:]
	}
	return path
}

// TargetedValidationIssue describes a problem with the LLM's targeted response
// that can be caught before applying it to the skeleton.
type TargetedValidationIssue struct {
	Key     string // "stepID.inputName" or "stepID.selectionName"
	Kind    string // e.g. "missing_value", "non_literal_value", "invalid_strategy"
	Message string // human-readable description for retry prompt
}

// validateTargetedResponse checks the LLM response for common mistakes before
// applying it to the skeleton. Returns nil if the response is clean.
func validateTargetedResponse(
	resp *TargetedResponse,
	unfedSet map[string]bool,
	inputContexts []InputContext,
	selectionContexts []SelectionContext,
) []TargetedValidationIssue {
	var issues []TargetedValidationIssue

	// Valid selection strategies (matches plan/validate.go).
	validStrategies := map[string]bool{
		"":       true,
		"first":  true,
		"last":   true,
		"index":  true,
		"random": true,
		"min":    true,
		"max":    true,
		"match":  true,
		"llm":    true,
	}

	// Build element field name set per selection key for filter field validation.
	selectionFields := map[string]map[string]bool{}
	for _, sc := range selectionContexts {
		key := sc.StepID + "." + sc.SelectionName
		fields := map[string]bool{}
		for _, ef := range sc.ElementFields {
			// Element fields are formatted as "name (type)" — extract just the name.
			name := ef
			if idx := strings.Index(ef, " ("); idx >= 0 {
				name = ef[:idx]
			}
			fields[name] = true
		}
		selectionFields[key] = fields
	}

	// Check 1: Every unfed input has a value.
	for _, ic := range inputContexts {
		key := ic.StepID + "." + ic.InputName
		if !unfedSet[key] {
			continue
		}
		if ic.IsConfigurable {
			continue // configurable inputs are optional in the response
		}
		if ic.CurrentDefault != "" {
			// Has a template default — not strictly required from LLM.
			continue
		}
		if _, provided := resp.Values[key]; !provided {
			issues = append(issues, TargetedValidationIssue{
				Key:     key,
				Kind:    "missing_value",
				Message: fmt.Sprintf("%s: no value provided for required input (type: %s)", key, ic.InputType),
			})
		}
	}

	// Check 2: Values are plain scalars, not objects or arrays.
	for key, val := range resp.Values {
		if !unfedSet[key] {
			continue
		}
		switch v := val.(type) {
		case map[string]any:
			// LLM returned an object — likely {from: ..., select: ...}.
			issues = append(issues, TargetedValidationIssue{
				Key:     key,
				Kind:    "non_literal_value",
				Message: fmt.Sprintf("%s: expected a literal value (string, number, or date expression), got an object with keys %v", key, mapKeys(v)),
			})
		case []any:
			issues = append(issues, TargetedValidationIssue{
				Key:     key,
				Kind:    "non_literal_value",
				Message: fmt.Sprintf("%s: expected a literal value, got an array", key),
			})
		}
	}

	// Build input context lookup for constraint checks.
	icByKey := map[string]*InputContext{}
	for i := range inputContexts {
		ic := &inputContexts[i]
		icByKey[ic.StepID+"."+ic.InputName] = ic
	}

	// Check 2b: Regex pattern, expression syntax, string length.
	for key, val := range resp.Values {
		if !unfedSet[key] {
			continue
		}
		strVal, isStr := val.(string)
		if !isStr {
			continue
		}

		ic := icByKey[key]
		if ic == nil {
			continue
		}

		// Skip all constraint checks for expression values.
		if plan.ContainsExpr(strVal) {
			// Validate expression syntax.
			if err := plan.ValidateExpr(strVal); err != nil {
				issues = append(issues, TargetedValidationIssue{
					Key:     key,
					Kind:    "invalid_expression",
					Message: fmt.Sprintf("%s: invalid expression syntax %q: %v", key, strVal, err),
				})
			}
			continue
		}

		// Regex pattern check.
		if ic.ConstraintPattern != "" {
			if re, err := regexp.Compile(ic.ConstraintPattern); err == nil {
				if !re.MatchString(strVal) {
					issues = append(issues, TargetedValidationIssue{
						Key:     key,
						Kind:    "pattern_mismatch",
						Message: fmt.Sprintf("%s: value %q does not match pattern %s", key, strVal, ic.ConstraintPattern),
					})
				}
			}
		}

		// String length checks.
		if ic.ConstraintMinLength != nil && len(strVal) < *ic.ConstraintMinLength {
			issues = append(issues, TargetedValidationIssue{
				Key:     key,
				Kind:    "length_violation",
				Message: fmt.Sprintf("%s: value %q has length %d, minimum is %d", key, strVal, len(strVal), *ic.ConstraintMinLength),
			})
		}
		if ic.ConstraintMaxLength != nil && len(strVal) > *ic.ConstraintMaxLength {
			issues = append(issues, TargetedValidationIssue{
				Key:     key,
				Kind:    "length_violation",
				Message: fmt.Sprintf("%s: value %q has length %d, maximum is %d", key, strVal, len(strVal), *ic.ConstraintMaxLength),
			})
		}
	}

	// Check 3–5: Selection overrides.
	for key, sel := range resp.Selections {
		// Check 3: Strategy is valid.
		if sel.Strategy != "" && !validStrategies[sel.Strategy] {
			issues = append(issues, TargetedValidationIssue{
				Key:     key,
				Kind:    "invalid_strategy",
				Message: fmt.Sprintf("%s: unknown selection strategy %q; valid: first, last, random, index, min, max, match, llm", key, sel.Strategy),
			})
		}

		// Check 4: Filter predicate parses.
		if sel.Filter != "" {
			if err := plan.ValidatePredicate(sel.Filter); err != nil {
				issues = append(issues, TargetedValidationIssue{
					Key:     key,
					Kind:    "invalid_filter_syntax",
					Message: fmt.Sprintf("%s: filter expression %q does not parse: %v", key, sel.Filter, err),
				})
				continue // Skip field check if syntax is bad.
			}

			// Check 5: Filter field references are in element fields.
			if fields, ok := selectionFields[key]; ok && len(fields) > 0 {
				filterFields := plan.PredicateFields(sel.Filter)
				for _, ff := range filterFields {
					if !fields[ff] {
						var available []string
						for f := range fields {
							available = append(available, f)
						}
						issues = append(issues, TargetedValidationIssue{
							Key:     key,
							Kind:    "invalid_filter_field",
							Message: fmt.Sprintf("%s: filter references field %q which is not in element fields; available: %s", key, ff, strings.Join(available, ", ")),
						})
					}
				}
			}
		}
	}

	return issues
}

// mapKeys returns the keys of a map for error messages.
func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// formatValidationIssues formats issues as a string list for retry prompts.
func formatValidationIssues(issues []TargetedValidationIssue) []string {
	msgs := make([]string, len(issues))
	for i, iss := range issues {
		msgs[i] = iss.Message
	}
	return msgs
}

// splitStepInput splits "stepID.inputName" into its components.
func splitStepInput(key string) (stepID, inputName string) {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return key, ""
	}
	return parts[0], parts[1]
}
