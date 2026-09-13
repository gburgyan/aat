package plan

import (
	"fmt"
	"strings"

	"github.com/gburgyan/aat/graph"
)

// RequiredFromOptionalValues warns about a step value on a required input that
// takes from: an optional output of another step, directly or through a named
// selection. When that output is missing, the step fails on the required input.
// It checks the values a plan or workflow file writes, before graph defaults are
// merged in (graph.RequiredFromOptionalDefaults covers those), and isn't for
// recipes, whose wiring comes from composition.
func RequiredFromOptionalValues(p *Plan, g *graph.Graph) []string {
	stepNodes := make(map[string]string, len(p.Execution.Steps))
	for _, step := range p.Execution.Steps {
		stepNodes[step.StepID()] = step.Node
	}
	// optional reports whether ref, written stepID.output, names an optional
	// output of the step's node.
	optional := func(ref string) bool {
		stepID, output, found := strings.Cut(ref, ".")
		if !found {
			return false
		}
		nodeName := stepNodes[stepID]
		if nodeName == "" {
			nodeName = stepID
		}
		return graph.OptionalOutput(g, nodeName+"."+output)
	}
	const fix = "when it's missing the step fails (mark the input optional to leave it out, or give it a value)"

	var warnings []string
	for i, step := range p.Execution.Steps {
		node := g.Nodes[step.Node]
		if node == nil {
			continue
		}
		for _, in := range node.Inputs {
			sv, ok := step.Values[in.Name]
			if !ok || in.Optional {
				continue
			}
			switch {
			case sv.From != "":
				if optional(sv.From) {
					warnings = append(warnings, fmt.Sprintf("step %d (%s): required input %q takes %s, an optional output; %s", i, step.StepID(), in.Name, sv.From, fix))
				}
			case sv.FromSelection != "":
				selName, _ := ParseFromSelection(sv.FromSelection)
				if sel, ok := step.Selections[selName]; ok && sel.From != "" && optional(sel.From) {
					warnings = append(warnings, fmt.Sprintf("step %d (%s): required input %q reads selection %q from %s, an optional output; %s", i, step.StepID(), in.Name, selName, sel.From, fix))
				}
			}
		}
	}
	return warnings
}
