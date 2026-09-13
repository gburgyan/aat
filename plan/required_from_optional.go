package plan

import (
	"fmt"
	"strings"

	"github.com/gburgyan/aat/graph"
)

// RequiredFromOptionalValues warns about a step value on a required input that
// takes from: an optional output of another step. When that output is missing,
// the step fails on the required input. It checks the values a plan or workflow
// file writes, before graph defaults are merged in (graph.RequiredFromOptionalDefaults
// covers those), and isn't for recipes, whose wiring comes from composition.
func RequiredFromOptionalValues(p *Plan, g *graph.Graph) []string {
	stepNodes := make(map[string]string, len(p.Execution.Steps))
	for _, step := range p.Execution.Steps {
		stepNodes[step.StepID()] = step.Node
	}

	var warnings []string
	for i, step := range p.Execution.Steps {
		node := g.Nodes[step.Node]
		if node == nil {
			continue
		}
		for _, in := range node.Inputs {
			sv, ok := step.Values[in.Name]
			if !ok || in.Optional || sv.From == "" {
				continue
			}
			stepID, output, found := strings.Cut(sv.From, ".")
			if !found {
				continue
			}
			nodeName := stepNodes[stepID]
			if nodeName == "" {
				nodeName = stepID
			}
			if graph.OptionalOutput(g, nodeName+"."+output) {
				warnings = append(warnings, fmt.Sprintf("step %d (%s): required input %q takes %s, an optional output; when it's missing the step fails (mark the input optional to leave it out, or give it a value)", i, step.StepID(), in.Name, sv.From))
			}
		}
	}
	return warnings
}
