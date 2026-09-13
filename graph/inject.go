package graph

import (
	"fmt"
	"strings"

	"github.com/gburgyan/aat/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// InjectValue is a value a slot option injects into the steps of a composed
// plan. It decodes like a graph input default, with one difference: a bare list
// is the list itself, not a pool. So a scalar is a value, a list is a list, and
// a mapping takes an input default's keys (value, pool, poolStrategy,
// constraint, from, fromResolved, select), where any other key, such as
// default, is an error.
type InjectValue struct {
	InputDefault
}

// InjectLiteral returns an InjectValue that injects v as it is.
func InjectLiteral(v any) InjectValue {
	return InjectValue{InputDefault: InputDefault{Value: v}}
}

// UnmarshalYAML decodes a scalar or a list as a literal value, and a mapping as
// an input default. It uses the callback form so strict decoding reaches the
// mapping (see internal/yamlx).
func (v *InjectValue) UnmarshalYAML(unmarshal func(any) error) error {
	n, err := yamlx.Node(unmarshal)
	if err != nil {
		return err
	}
	switch n.Kind {
	case yaml.ScalarNode:
		*v = InjectValue{}
		return unmarshal(&v.Value)
	case yaml.SequenceNode:
		var list []any
		if err := unmarshal(&list); err != nil {
			return err
		}
		*v = InjectLiteral(list)
		return nil
	case yaml.MappingNode:
		var d InputDefault
		if err := unmarshal(&d); err != nil {
			return err
		}
		*v = InjectValue{InputDefault: d}
		return nil
	default:
		return yamlx.KindError(n, "an inject value", "a scalar, a list, or a mapping")
	}
}

// validateValueShapes checks that node input defaults and slot inject values fit
// the inputs they set (see DefaultShapeError and injectShapeErrors), and that
// inject appears only on slot options, the only workflows composition reads it
// from.
func validateValueShapes(g *Graph) []string {
	nodeNames := sortedKeys(g.Nodes)

	var errs []string
	for _, name := range nodeNames {
		for _, in := range g.Nodes[name].Inputs {
			if msg := DefaultShapeError(in.Default, in.Type); msg != "" {
				errs = append(errs, fmt.Sprintf("node %q: input %q default: %s", name, in.Name, msg))
			}
		}
	}

	for i, wf := range g.Workflows {
		if len(wf.Inject) == 0 {
			continue
		}
		if !wf.IsSlot() {
			kind := "a base workflow"
			if wf.IsAddon() {
				kind = "an addon"
			}
			errs = append(errs, fmt.Sprintf("workflow %d (%q): inject applies only to slot options (kind: slot), and %s ignores it", i, wf.Name, kind))
			continue
		}
		for _, key := range sortedKeys(wf.Inject) {
			errs = append(errs, injectShapeErrors(g, nodeNames, i, wf.Name, key, wf.Inject[key])...)
		}
	}
	return errs
}

// injectShapeErrors checks a slot inject value against the inputs with its
// name, in nodeNames order. Composition decides which steps the value reaches,
// and plan validation checks those, so it is an error only when no such input
// takes it; then each input's mismatch is reported.
func injectShapeErrors(g *Graph, nodeNames []string, i int, workflow, key string, value InjectValue) []string {
	var errs []string
	for _, name := range nodeNames {
		for _, in := range g.Nodes[name].Inputs {
			if in.Name != key {
				continue
			}
			msg := DefaultShapeError(&value.InputDefault, in.Type)
			if msg == "" {
				return nil
			}
			errs = append(errs, fmt.Sprintf("workflow %d (%q): inject %q for %s.%s: %s", i, workflow, key, name, key, msg))
		}
	}
	return errs
}

// DefaultShapeError checks a default's literal value, and each entry of its
// pool, against an input type (see ValueShapeError). It returns "" when they
// fit.
func DefaultShapeError(d *InputDefault, typ string) string {
	if d == nil {
		return ""
	}
	if msg := ValueShapeError(d.Value, typ); msg != "" {
		return msg
	}
	for i, entry := range d.Pool {
		msg := ValueShapeError(entry, typ)
		if msg == "" {
			continue
		}
		msg = fmt.Sprintf("pool entry %d: %s", i, msg)
		if _, isList := entry.([]any); !isList {
			if ft, err := ParseFieldType(typ); err == nil && ft.IsArray {
				msg += " (each pool entry is one choice, so write a single list as {value: [...]})"
			}
		}
		return msg
	}
	return ""
}

// ValueShapeError describes how a literal value doesn't fit an input type, or
// returns "" when it fits or can't be judged. It checks the shape only: an array
// type takes a list whose items fit the element type, and a scalar or enum type
// takes no map. A list is allowed for a scalar type, since a template can send a
// list for one field, as repeated query pairs. A string holding an expression
// ({{…}}) fits any type, since it is evaluated at run time, and custom types
// aren't checked.
func ValueShapeError(v any, typ string) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok && strings.Contains(s, "{{") {
		return ""
	}
	ft, err := ParseFieldType(typ)
	if err != nil {
		return ""
	}
	switch ft.Kind {
	case TypeArray:
		items, ok := v.([]any)
		if !ok {
			shape := "a single value"
			if _, isMap := v.(map[string]any); isMap {
				shape = "a map"
			}
			return fmt.Sprintf("%s, where %s takes a list", shape, typ)
		}
		for i, item := range items {
			if msg := ValueShapeError(item, ft.Name); msg != "" {
				return fmt.Sprintf("item %d: %s", i, msg)
			}
		}
	case TypeScalar, TypeEnum:
		if _, ok := v.(map[string]any); ok {
			return fmt.Sprintf("a map, where %s takes a single value", typ)
		}
	}
	return ""
}
