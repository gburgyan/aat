package intent

import (
	"encoding/json"
	"fmt"
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
	Constraints    []string // matched user constraints from phase 1
	GraphConstr    string   // graph-level constraint annotation
	IsDate         bool
	CurrentDefault string // template default value, if any (e.g., "DEN")
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
// type/pool/concepts/constraints per input.
func buildInputContexts(skeleton *plan.Plan, g *graph.Graph, kb *domain.KnowledgeBase, ws *WorkflowSelection) []InputContext {
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
				StepID:    step.StepID(),
				InputName: inp.Name,
				InputType: inp.Type,
				NodeDesc:  node.Description,
			}

			// Check for existing template default value.
			if sv, exists := step.Values[inp.Name]; exists && sv.Default != nil {
				ic.CurrentDefault = fmt.Sprintf("%v", sv.Default)
			}

			// Check for date type.
			if inp.Type == "date" || inp.Type == "datetime" {
				ic.IsDate = true
			}

			// Graph-level constraint annotation.
			if inp.Constraints != nil && inp.Constraints.Description != "" {
				ic.GraphConstr = inp.Constraints.Description
			}
			if inp.Constraints != nil && inp.Constraints.Pattern != "" {
				if ic.GraphConstr != "" {
					ic.GraphConstr += " "
				}
				ic.GraphConstr += fmt.Sprintf("(pattern: %s)", inp.Constraints.Pattern)
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
				}
			}

			// Match user constraints from workflow selection.
			if ws != nil {
				key := step.StepID() + "." + inp.Name
				for _, h := range ws.Constraints.Hard {
					for _, a := range h.AppliesTo {
						if a == key || a == step.Node+"."+inp.Name || a == inp.Name {
							ic.Constraints = append(ic.Constraints, fmt.Sprintf("[hard] %s — %s", h.Name, h.Description))
						}
					}
				}
				for _, s := range ws.Constraints.Soft {
					for _, a := range s.AppliesTo {
						if a == key || a == step.Node+"."+inp.Name || a == inp.Name {
							ic.Constraints = append(ic.Constraints, fmt.Sprintf("[soft] %s — %s", s.Name, s.Description))
						}
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

// buildCompactPlanFlow formats a numbered step list with deps, goal markers,
// and cleanup for the LLM prompt.
func buildCompactPlanFlow(skeleton *plan.Plan, g *graph.Graph) string {
	var sb strings.Builder

	for i, step := range skeleton.Execution.Steps {
		node := g.Nodes[step.Node]
		desc := step.Node
		if node != nil && node.Description != "" {
			desc = node.Description
		}

		fmt.Fprintf(&sb, "%d. %s", i+1, step.StepID())
		if step.StepID() != step.Node {
			fmt.Fprintf(&sb, " [node: %s]", step.Node)
		}
		sb.WriteString(" — ")
		sb.WriteString(desc)

		if len(step.DependsOn) > 0 {
			fmt.Fprintf(&sb, " (depends on: %s)", strings.Join(step.DependsOn, ", "))
		}
		if step.IsGoal {
			sb.WriteString(" [GOAL]")
		}
		sb.WriteString("\n")

		// List output fields for assertion context.
		if node != nil && len(node.Outputs) > 0 {
			var outNames []string
			for _, out := range node.Outputs {
				outNames = append(outNames, out.Name)
			}
			fmt.Fprintf(&sb, "   Outputs: %s\n", strings.Join(outNames, ", "))
		}
	}

	if len(skeleton.Execution.Cleanup) > 0 {
		sb.WriteString("Cleanup: ")
		var cleanupParts []string
		for _, cs := range skeleton.Execution.Cleanup {
			part := cs.Node
			if cs.RunOn != "" {
				part += " (" + cs.RunOn + ")"
			}
			cleanupParts = append(cleanupParts, part)
		}
		sb.WriteString(strings.Join(cleanupParts, ", "))
		sb.WriteString("\n")
	}

	return sb.String()
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

// splitStepInput splits "stepID.inputName" into its components.
func splitStepInput(key string) (stepID, inputName string) {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return key, ""
	}
	return parts[0], parts[1]
}
