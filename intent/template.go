package intent

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// FindWorkflowTemplate looks up a workflow by name in the graph and returns
// the template path if one is configured. Returns ("", false) if no workflow
// matches or the matching workflow has no template.
func FindWorkflowTemplate(g *graph.Graph, name string) (templatePath string, found bool) {
	for _, wf := range g.Workflows {
		if strings.EqualFold(wf.Name, name) {
			if wf.Template == "" {
				return "", false
			}
			return wf.Template, true
		}
	}
	return "", false
}

// LoadWorkflowTemplate loads a plan template from the given path, resolving
// it relative to graphDir (the directory containing the graph file).
// It validates that all step and cleanup nodes exist in the graph.
func LoadWorkflowTemplate(templatePath, graphDir string, g *graph.Graph) (*plan.Plan, error) {
	resolved := templatePath
	if !filepath.IsAbs(templatePath) {
		resolved = filepath.Join(graphDir, templatePath)
	}

	p, err := plan.ParseFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("loading workflow template %s: %w", templatePath, err)
	}

	// Validate all step nodes exist in the graph (skip slot markers).
	for _, step := range p.Execution.Steps {
		if step.IsSlotMarker() {
			continue
		}
		if g.Nodes[step.Node] == nil {
			return nil, fmt.Errorf("workflow template references unknown node %q", step.Node)
		}
	}

	// Validate cleanup nodes.
	for _, cs := range p.Execution.Cleanup {
		if g.Nodes[cs.Node] == nil {
			return nil, fmt.Errorf("workflow template cleanup references unknown node %q", cs.Node)
		}
	}

	return p, nil
}

// UnfedInputsFromTemplate walks template steps and returns a list of
// "stepID.input (type)" strings for inputs that need LLM-provided values.
// An input is unfed if it has no from, fromSelection, select, or Default value,
// and is not optional or defaulted in the graph.
func UnfedInputsFromTemplate(p *plan.Plan, g *graph.Graph) []string {
	var unfed []string
	for _, step := range p.Execution.Steps {
		node := g.Nodes[step.Node]
		if node == nil {
			continue
		}
		for _, inp := range node.Inputs {
			if isInputFed(step, inp) {
				continue
			}
			label := fmt.Sprintf("%s.%s (%s)", step.StepID(), inp.Name, inp.Type)
			if inp.Configurable {
				label += " [configurable]"
			}
			unfed = append(unfed, label)
		}
	}
	return unfed
}

// unfedInputSet returns the set of "stepID.inputName" keys for inputs that the
// template does not wire and that need literal values from the LLM. This is
// used by MergeLLMValuesWithIDs to reject hallucinated literals for inputs
// that will be resolved at runtime (e.g., auto-wired graph edges).
func unfedInputSet(p *plan.Plan, g *graph.Graph) map[string]bool {
	set := map[string]bool{}
	for _, step := range p.Execution.Steps {
		node := g.Nodes[step.Node]
		if node == nil {
			continue
		}
		for _, inp := range node.Inputs {
			if isInputFed(step, inp) {
				continue
			}
			set[step.StepID()+"."+inp.Name] = true
		}
	}
	return set
}

// isInputFed reports whether a step already has wiring for the given input.
// An input is "fed" if it has structural wiring (from, fromSelection, select,
// named selection) or the input is optional / has a graph-level default.
// Literal template defaults (e.g., origin: DEN) are NOT considered fed —
// the LLM should be able to override them based on user intent.
// Configurable inputs are never auto-fed — they are surfaced to the LLM
// so it can set/override them based on user intent.
// fromResolved inputs are treated as overridable — they appear in the LLM
// prompt so the LLM can override them when user intent conflicts with the
// auto-wiring (e.g., "fly out of Nashville on BOTH legs").
func isInputFed(step plan.Step, inp graph.Input) bool {
	if sv, exists := step.Values[inp.Name]; exists {
		if sv.From != "" || sv.FromSelection != "" || sv.Select != nil {
			return true
		}
	}
	if isFromNamedSelection(step, inp.Name) {
		return true
	}
	// Configurable inputs are NOT fed — surface them to the LLM.
	if inp.Configurable {
		return false
	}
	if inp.Optional || (inp.Default != nil && inp.Default.HasValue()) {
		return true
	}
	return false
}

// MergeLLMValuesWithIDs is like MergeLLMValues but matches steps by StepID()
// instead of Node. This is required for multiplicity-expanded plans where
// multiple steps share the same node name.
//
// The unfed parameter is a set of "stepID.inputName" keys for inputs that
// genuinely need literal values from the LLM. When a value is not in the
// skeleton and the input is not in the unfed set, the LLM's literal is
// rejected (it would shadow auto-wired graph edges at runtime).
// If unfed is nil, all LLM-provided new values are accepted (legacy behavior).
func MergeLLMValuesWithIDs(skeleton, llmPlan *plan.Plan, unfed map[string]bool) {
	// Build index of LLM steps by step ID.
	llmSteps := map[string]*plan.Step{}
	for i := range llmPlan.Execution.Steps {
		s := &llmPlan.Execution.Steps[i]
		llmSteps[s.StepID()] = s
	}

	for i := range skeleton.Execution.Steps {
		skelStep := &skeleton.Execution.Steps[i]
		llmStep, ok := llmSteps[skelStep.StepID()]
		if !ok {
			continue
		}

		// Accept description from LLM.
		if llmStep.Description != "" {
			skelStep.Description = llmStep.Description
		}

		// Accept assertions from LLM.
		if llmStep.Assertions != nil {
			skelStep.Assertions = llmStep.Assertions
		}

		// Accept retry config from LLM.
		if llmStep.Retry != nil {
			skelStep.Retry = llmStep.Retry
		}

		// Merge named selection strategy overrides.
		if len(skelStep.Selections) > 0 && len(llmStep.Selections) > 0 {
			for selName, llmSel := range llmStep.Selections {
				skelSel, exists := skelStep.Selections[selName]
				if !exists {
					continue
				}
				if llmSel.Strategy != "" {
					skelSel.Strategy = llmSel.Strategy
				}
				if llmSel.Filter != "" {
					skelSel.Filter = llmSel.Filter
				}
				if llmSel.SortField != "" {
					skelSel.SortField = llmSel.SortField
				}
				if llmSel.Prompt != "" {
					skelSel.Prompt = llmSel.Prompt
				}
				if llmSel.Index != 0 {
					skelSel.Index = llmSel.Index
				}
				skelStep.Selections[selName] = skelSel
			}
		}

		// Merge values.
		if llmStep.Values == nil {
			continue
		}
		if skelStep.Values == nil {
			skelStep.Values = map[string]plan.StepValue{}
		}

		for inputName, llmVal := range llmStep.Values {
			skelVal, exists := skelStep.Values[inputName]

			if !exists {
				// Only accept new literals for inputs that genuinely need values.
				// Reject hallucinated literals for inputs that will be auto-wired
				// from graph edges at runtime.
				key := skelStep.StepID() + "." + inputName
				if llmVal.Default != nil && (unfed == nil || unfed[key]) {
					skelStep.Values[inputName] = plan.StepValue{
						Default: llmVal.Default,
					}
				}
				continue
			}

			if skelVal.FromSelection != "" {
				continue
			} else if skelVal.From != "" && skelVal.Select != nil {
				if llmVal.Select != nil {
					if llmVal.Select.Strategy != "" {
						skelVal.Select.Strategy = llmVal.Select.Strategy
					}
					if llmVal.Select.Filter != "" {
						skelVal.Select.Filter = llmVal.Select.Filter
					}
					if llmVal.Select.SortField != "" {
						skelVal.Select.SortField = llmVal.Select.SortField
					}
					if llmVal.Select.Index != 0 {
						skelVal.Select.Index = llmVal.Select.Index
					}
					if llmVal.Select.Prompt != "" {
						skelVal.Select.Prompt = llmVal.Select.Prompt
					}
				}
				skelStep.Values[inputName] = skelVal
			} else if skelVal.From != "" {
				// Scalar from ref: skeleton is authoritative.
			} else {
				if llmVal.Default != nil {
					skelVal.Default = llmVal.Default
				}
				skelStep.Values[inputName] = skelVal
			}
		}
	}
}

// isFromNamedSelection checks if an input is wired via a fromSelection
// reference in the step's values.
func isFromNamedSelection(step plan.Step, inputName string) bool {
	sv, exists := step.Values[inputName]
	if !exists {
		return false
	}
	return sv.FromSelection != ""
}
