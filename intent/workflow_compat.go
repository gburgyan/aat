package intent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// WorkflowCompatResult captures the results of workflow compatibility checking.
//   - Warnings: structural AUTOWIRE inputs that the graph CAN produce but a
//     specific base workflow doesn't satisfy.
//   - MarkerWarnings: AUTOWIRE markers in base workflows and their slot options
//     that the base cannot feed, and AUTOWIRE? markers on required inputs with
//     no graph default.
//   - NonProducible: AUTOWIRE inputs whose name doesn't match any output or
//     elementField in the graph. These indicate either misuse of AUTOWIRE
//     (should be LLM-filled) or a missing graph elementField.
//   - Errors: template loading failures (non-fatal).
type WorkflowCompatResult struct {
	Warnings       []WorkflowCompatWarning
	MarkerWarnings []WorkflowMarkerWarning
	NonProducible  []WorkflowNonProducible
	Errors         []WorkflowCompatError
}

// WorkflowNonProducible records an addon with AUTOWIRE inputs that no node
// in the graph can produce. These inputs either need a graph elementField
// or shouldn't be marked AUTOWIRE.
type WorkflowNonProducible struct {
	Addon  string
	Inputs []string
}

// WorkflowCompatWarning records an addon+base pair where some AUTOWIRE
// inputs in the addon cannot be auto-wired from the base workflow's outputs.
type WorkflowCompatWarning struct {
	Addon        string
	BaseWorkflow string
	UnfedInputs  []string
}

// WorkflowMarkerWarning records an AUTOWIRE marker in a workflow template that
// composition cannot resolve as written.
type WorkflowMarkerWarning struct {
	Workflow string // the base workflow, or the workflow whose template holds an AUTOWIRE? marker
	Step     string // the template step's ID
	Input    string
	Message  string
}

// WorkflowCompatError records a template loading failure for a workflow.
type WorkflowCompatError struct {
	Workflow string
	Err      error
}

// HasWarnings returns true if any compatibility or marker warnings were found.
func (r *WorkflowCompatResult) HasWarnings() bool {
	return len(r.Warnings) > 0 || len(r.MarkerWarnings) > 0
}

// HasErrors returns true if any template loading errors occurred.
func (r *WorkflowCompatResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// HasNonProducible returns true if any AUTOWIRE inputs reference names
// that no graph node can produce.
func (r *WorkflowCompatResult) HasNonProducible() bool {
	return len(r.NonProducible) > 0
}

// HasIssues returns true if any warnings, non-producible entries, or errors were found.
func (r *WorkflowCompatResult) HasIssues() bool {
	return r.HasWarnings() || r.HasNonProducible() || r.HasErrors()
}

// Format returns a human-readable summary of compatibility issues.
func (r *WorkflowCompatResult) Format() string {
	if !r.HasWarnings() && !r.HasNonProducible() {
		return ""
	}

	var sb strings.Builder
	if len(r.Warnings) > 0 {
		sb.WriteString("Workflow compatibility warnings:\n")
		for _, w := range r.Warnings {
			fmt.Fprintf(&sb, "  addon %q + base %q: unfed AUTOWIRE inputs: %s\n",
				w.Addon, w.BaseWorkflow, strings.Join(w.UnfedInputs, ", "))
		}
	}
	if len(r.MarkerWarnings) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("AUTOWIRE marker warnings:\n")
		for _, w := range r.MarkerWarnings {
			fmt.Fprintf(&sb, "  workflow %q: %s.%s: %s\n", w.Workflow, w.Step, w.Input, w.Message)
		}
	}
	if r.HasNonProducible() {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("Non-producible AUTOWIRE inputs (no graph output or elementField matches):\n")
		for _, np := range r.NonProducible {
			fmt.Fprintf(&sb, "  addon %q: %s\n", np.Addon, strings.Join(np.Inputs, ", "))
		}
	}
	return sb.String()
}

// ValidateWorkflowCompat checks workflow templates for AUTOWIRE markers that
// composition cannot resolve:
//   - an addon's AUTOWIRE inputs must be fed in every base it composes into;
//   - a plain AUTOWIRE in a base or slot option must be fed by the base or its
//     slots;
//   - AUTOWIRE? must not mark an input that the node requires and gives no
//     graph default.
//
// Only AUTOWIRE inputs that are "structural" (the graph produces a matching
// output name or elementField name somewhere) are checked for feeding. Value
// inputs that no node can produce (e.g., email, commentText) are assumed to be
// filled by a recipe override or aat prompt and are not flagged.
//
// Slots count the way composition uses them: a base's slots are filled before
// addons are spliced, so an input that every option of a slot produces is fed,
// and an addon may attach after a node that only a slot option contributes.
func ValidateWorkflowCompat(g *graph.Graph, graphDir string) *WorkflowCompatResult {
	if len(g.Workflows) == 0 {
		return &WorkflowCompatResult{}
	}

	// Load all templates, caching by template path.
	templates := make(map[string]*plan.Plan)
	var loadErrs []WorkflowCompatError
	for _, wf := range g.Workflows {
		if wf.Template == "" {
			continue
		}
		if _, cached := templates[wf.Template]; cached {
			continue
		}
		p, err := LoadWorkflowTemplate(wf.Template, graphDir, g)
		if err != nil {
			loadErrs = append(loadErrs, WorkflowCompatError{
				Workflow: wf.Name,
				Err:      err,
			})
			continue
		}
		templates[wf.Template] = p
	}

	result := checkWorkflowCompat(g, templates)
	result.Errors = loadErrs
	return result
}

// checkWorkflowCompat runs the compatibility checks against templates that are
// already loaded, keyed by workflow template path. A base whose template, or
// any of whose slot option templates, is missing from the map is skipped.
func checkWorkflowCompat(g *graph.Graph, templates map[string]*plan.Plan) *WorkflowCompatResult {
	result := &WorkflowCompatResult{}

	bases, addons := partitionWorkflows(g)

	// Build the set of names that the graph can produce (outputs + elementFields).
	producible := buildProducibleNames(g)

	result.MarkerWarnings = checkOptionalMarkers(g, templates)
	for _, base := range bases {
		shape, ok := composedShape(g, base, templates)
		if !ok {
			continue // base or slot option template failed to load
		}
		result.MarkerWarnings = append(result.MarkerWarnings, checkBaseMarkers(g, base, shape, addons, templates, producible)...)
	}
	sort.SliceStable(result.MarkerWarnings, func(i, j int) bool {
		a, b := result.MarkerWarnings[i], result.MarkerWarnings[j]
		if a.Workflow != b.Workflow {
			return a.Workflow < b.Workflow
		}
		if a.Step != b.Step {
			return a.Step < b.Step
		}
		return a.Input < b.Input
	})

	if len(addons) == 0 || len(bases) == 0 {
		return result
	}

	// For each addon, check compatibility with each base workflow.
	for _, addon := range addons {
		addonPlan := templates[addon.Template]
		if addonPlan == nil {
			continue // template failed to load
		}

		// Collect AUTOWIRE inputs from the addon plan.
		autowireInputs := collectAutowireInputs(addonPlan)
		if len(autowireInputs) == 0 {
			continue // nothing to check
		}

		// Remove inputs covered by explicit Wire entries (including MANUAL).
		for inputName := range addon.Wire {
			delete(autowireInputs, inputName)
		}

		// Separate structural vs non-producible AUTOWIRE inputs.
		// Non-producible: name doesn't match any output or elementField in the graph.
		var nonProducibleInputs []string
		for inputName := range autowireInputs {
			if !producible[inputName] {
				nonProducibleInputs = append(nonProducibleInputs, inputName)
				delete(autowireInputs, inputName)
			}
		}
		if len(nonProducibleInputs) > 0 {
			sort.Strings(nonProducibleInputs)
			result.NonProducible = append(result.NonProducible, WorkflowNonProducible{
				Addon:  addon.Name,
				Inputs: nonProducibleInputs,
			})
		}

		// An AUTOWIRE? input may stay unset, so it is never unfed.
		for inputName := range optionalAutowireInputs(addonPlan) {
			delete(autowireInputs, inputName)
		}

		if len(autowireInputs) == 0 {
			continue
		}

		// Check each base workflow for compatibility.
		for _, base := range bases {
			shape, ok := composedShape(g, base, templates)
			if !ok {
				continue // base or slot option template failed to load
			}

			// The addon only composes into bases that contain one of its After nodes.
			if addon.After.IsSet() && !shape.hasAnyNode(addon.After) {
				continue
			}

			// Check which structural AUTOWIRE inputs are not satisfied.
			var unfed []string
			for inputName := range autowireInputs {
				if !shape.feeds(inputName) {
					unfed = append(unfed, inputName)
				}
			}

			if len(unfed) > 0 {
				sort.Strings(unfed)
				result.Warnings = append(result.Warnings, WorkflowCompatWarning{
					Addon:        addon.Name,
					BaseWorkflow: base.Name,
					UnfedInputs:  unfed,
				})
			}
		}
	}

	return result
}

// checkBaseMarkers reports plain AUTOWIRE markers in a base template, or in one
// of its slot options, that composition without addons cannot feed: no step of
// the base produces the output, and no slot does in every option. A marker in a
// slot option is also fed by that option's own steps. Names that no graph node
// produces are left to recipe overrides and aat prompt, as for addons.
func checkBaseMarkers(g *graph.Graph, base graph.Workflow, shape *baseShape, addons []graph.Workflow, templates map[string]*plan.Plan, producible map[string]bool) []WorkflowMarkerWarning {
	var warnings []WorkflowMarkerWarning
	check := func(p *plan.Plan, ownOutputs map[string]string) {
		for _, step := range p.Execution.Steps {
			for _, name := range autowireMarkerNames(step.Values) {
				if _, optional := plan.AutowireMarker(step.Values[name]); optional {
					continue
				}
				if !producible[name] || shape.feeds(name) {
					continue
				}
				if _, own := ownOutputs[name]; own {
					continue
				}
				warnings = append(warnings, WorkflowMarkerWarning{
					Workflow: base.Name,
					Step:     step.StepID(),
					Input:    name,
					Message:  unfedMarkerMessage(g, name, addons, templates),
				})
			}
		}
	}

	check(shape.base, nil)
	for i, options := range shape.slots {
		for j, option := range options {
			check(option, shape.slotOutputs[i][j])
		}
	}
	return warnings
}

// unfedMarkerMessage explains an AUTOWIRE that a base cannot feed, naming the
// addons whose steps produce the output.
func unfedMarkerMessage(g *graph.Graph, name string, addons []graph.Workflow, templates map[string]*plan.Plan) string {
	var producers []string
	for _, addon := range addons {
		p := templates[addon.Template]
		if p == nil {
			continue
		}
		if _, ok := buildOutputMap(p, g)[name]; ok {
			producers = append(producers, fmt.Sprintf("%q", addon.Name))
		}
	}
	if len(producers) == 0 {
		return "unfed AUTOWIRE: no step of the base or its slots produces it; wire it, or set it in each recipe"
	}
	return fmt.Sprintf("unfed AUTOWIRE: only addon %s produces it; use AUTOWIRE? if the input is optional",
		strings.Join(producers, ", "))
}

// checkOptionalMarkers reports AUTOWIRE? on an input that its node requires and
// gives no graph default: when no step feeds it, the step has no value to send.
// Each template is checked once, under the first workflow that uses it.
func checkOptionalMarkers(g *graph.Graph, templates map[string]*plan.Plan) []WorkflowMarkerWarning {
	var warnings []WorkflowMarkerWarning
	seen := make(map[string]bool)
	for _, wf := range g.Workflows {
		p := templates[wf.Template]
		if p == nil || seen[wf.Template] {
			continue
		}
		seen[wf.Template] = true
		for _, step := range p.Execution.Steps {
			node := g.Nodes[step.Node]
			if node == nil {
				continue
			}
			for _, name := range autowireMarkerNames(step.Values) {
				if _, optional := plan.AutowireMarker(step.Values[name]); !optional {
					continue
				}
				inp := inputNamed(node, name)
				if inp == nil || inp.Optional || inp.Default.HasValue() {
					continue
				}
				warnings = append(warnings, WorkflowMarkerWarning{
					Workflow: wf.Name,
					Step:     step.StepID(),
					Input:    name,
					Message:  "AUTOWIRE? on a required input with no graph default: when nothing feeds it, the step has no value; use AUTOWIRE, or make the input optional",
				})
			}
		}
	}
	return warnings
}

// inputNamed returns node's input called name, or nil.
func inputNamed(node *graph.Node, name string) *graph.Input {
	for i := range node.Inputs {
		if node.Inputs[i].Name == name {
			return &node.Inputs[i]
		}
	}
	return nil
}

// autowireMarkerNames returns, sorted, the names of the inputs in values that
// hold an AUTOWIRE marker.
func autowireMarkerNames(values map[string]plan.StepValue) []string {
	var names []string
	for name, sv := range values {
		if marker, _ := plan.AutowireMarker(sv); marker {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// partitionWorkflows splits the graph's workflows into base workflows and
// addons. Slot options are neither: they are checked through the bases whose
// slots offer them.
func partitionWorkflows(g *graph.Graph) (bases, addons []graph.Workflow) {
	for _, wf := range g.Workflows {
		switch {
		case wf.IsAddon():
			addons = append(addons, wf)
		case wf.IsSlot():
			// checked through its base
		default:
			bases = append(bases, wf)
		}
	}
	return bases, addons
}

// baseShape describes what composing a base workflow can contain before addons
// are spliced in: the base template's own steps, plus the steps each option of
// each slot would add.
type baseShape struct {
	base        *plan.Plan
	baseOutputs map[string]string
	slots       [][]*plan.Plan        // per slot, one template per option
	slotOutputs [][]map[string]string // per slot, one output map per option
}

// composedShape gathers the base's template and the templates of every option
// of its slots. It reports false when any of them is unavailable.
func composedShape(g *graph.Graph, base graph.Workflow, templates map[string]*plan.Plan) (*baseShape, bool) {
	basePlan := templates[base.Template]
	if basePlan == nil {
		return nil, false
	}
	shape := &baseShape{base: basePlan, baseOutputs: buildOutputMap(basePlan, g)}
	for _, sd := range base.Slots {
		var options []*plan.Plan
		var outputs []map[string]string
		for _, optionName := range sd.Options {
			option, found := findWorkflowByName(g, optionName)
			if !found || templates[option.Template] == nil {
				return nil, false
			}
			options = append(options, templates[option.Template])
			outputs = append(outputs, buildOutputMap(templates[option.Template], g))
		}
		shape.slots = append(shape.slots, options)
		shape.slotOutputs = append(shape.slotOutputs, outputs)
	}
	return shape, true
}

// hasAnyNode reports whether any of the nodes appears in the base template or
// in some slot option, i.e. whether an addon can find its insertion point in
// at least one composition of the base.
func (s *baseShape) hasAnyNode(nodes graph.AfterSpec) bool {
	for _, node := range nodes {
		if findStepByNode(s.base, node) != "" {
			return true
		}
		for _, options := range s.slots {
			for _, option := range options {
				if findStepByNode(option, node) != "" {
					return true
				}
			}
		}
	}
	return false
}

// feeds reports whether every composition of the base produces an output named
// input: the base template produces it, or every option of some slot does.
func (s *baseShape) feeds(input string) bool {
	if _, ok := s.baseOutputs[input]; ok {
		return true
	}
	for _, options := range s.slotOutputs {
		if len(options) == 0 {
			continue
		}
		everyOption := true
		for _, outputs := range options {
			if _, ok := outputs[input]; !ok {
				everyOption = false
				break
			}
		}
		if everyOption {
			return true
		}
	}
	return false
}

// buildProducibleNames returns the set of all names that the graph can
// produce: output names and elementField names across all nodes. An
// AUTOWIRE input is considered "structural" (expected to come from upstream)
// only if its name appears in this set.
func buildProducibleNames(g *graph.Graph) map[string]bool {
	names := make(map[string]bool)
	for _, node := range g.Nodes {
		for _, out := range node.Outputs {
			names[out.Name] = true
			for _, ef := range out.ElementFields {
				names[ef.Name] = true
			}
		}
	}
	return names
}

// collectAutowireInputs scans all steps in a plan and returns a set of
// input names that have AUTOWIRE placeholder values.
func collectAutowireInputs(p *plan.Plan) map[string]bool {
	inputs := make(map[string]bool)
	for _, step := range p.Execution.Steps {
		for inputName, sv := range step.Values {
			if isPlaceholder(sv) {
				inputs[inputName] = true
			}
		}
	}
	return inputs
}

// optionalAutowireInputs returns the input names in p whose every marker is
// AUTOWIRE?. Addon checks never report them as unfed.
func optionalAutowireInputs(p *plan.Plan) map[string]bool {
	optional := make(map[string]bool)
	plain := make(map[string]bool)
	for _, step := range p.Execution.Steps {
		for inputName, sv := range step.Values {
			switch marker, opt := plan.AutowireMarker(sv); {
			case !marker:
			case opt:
				optional[inputName] = true
			default:
				plain[inputName] = true
			}
		}
	}
	for inputName := range plain {
		delete(optional, inputName)
	}
	return optional
}
