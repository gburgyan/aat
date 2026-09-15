package engine

import (
	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
)

// convertHeaderOutputs converts the outputs a template reads from response
// headers, which arrive as strings, to their graph output's integer, float, or
// boolean type, so a predicate compares them as numbers. A value that doesn't
// parse stays a string.
func convertHeaderOutputs(outputs map[string]any, node *graph.Node, tmpl *adapter.Template) {
	for _, out := range node.Outputs {
		rule, ok := tmpl.Response.Extract[out.Name]
		if !ok || rule.Header == "" {
			continue
		}
		if s, ok := outputs[out.Name].(string); ok {
			outputs[out.Name] = coerceValue(s, out.Type)
		}
	}
}
