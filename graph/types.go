package graph

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// AutoWireMode controls how auto-wiring generates edges in a graph.
type AutoWireMode string

const (
	// AutoWireOff disables auto-wiring (default).
	AutoWireOff AutoWireMode = ""
	// AutoWireSources only wires from source nodes — nodes that produce a field
	// without also consuming it. Pass-through nodes are skipped as producers.
	AutoWireSources AutoWireMode = "sources"
	// AutoWireAll creates edges for every matching output→input pair (full Cartesian product).
	AutoWireAll AutoWireMode = "all"
)

// IsEnabled returns true if auto-wiring is active (sources or all).
func (m AutoWireMode) IsEnabled() bool {
	return m == AutoWireSources || m == AutoWireAll
}

// UnmarshalYAML handles bool and string values for AutoWireMode.
// YAML true maps to AutoWireSources, false maps to AutoWireOff.
// String values "sources" and "all" are accepted directly.
func (m *AutoWireMode) UnmarshalYAML(value *yaml.Node) error {
	// Try bool first (YAML true/false)
	var b bool
	if err := value.Decode(&b); err == nil {
		if b {
			*m = AutoWireSources
		} else {
			*m = AutoWireOff
		}
		return nil
	}
	// Try string
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("autoWire must be a boolean or string (\"sources\", \"all\"): %w", err)
	}
	switch s {
	case "sources":
		*m = AutoWireSources
	case "all":
		*m = AutoWireAll
	case "":
		*m = AutoWireOff
	default:
		return fmt.Errorf("invalid autoWire value %q: must be true, false, \"sources\", or \"all\"", s)
	}
	return nil
}

// MarshalYAML serializes AutoWireMode. Sources mode is written as true
// for backward compatibility; all mode is written as "all".
func (m AutoWireMode) MarshalYAML() (interface{}, error) {
	switch m {
	case AutoWireSources:
		return true, nil
	case AutoWireAll:
		return "all", nil
	default:
		return false, nil
	}
}

// Graph is the top-level API graph model parsed from YAML.
// It describes the logical operations (nodes), data flow (edges),
// and conditional requirements (conditions) for an API workflow.
type Graph struct {
	Version     string           `yaml:"version"`
	Title       string           `yaml:"title,omitempty"`
	Description string           `yaml:"description,omitempty"`
	AutoWire    AutoWireMode     `yaml:"autoWire,omitempty"`
	Workflows   []Workflow       `yaml:"workflows,omitempty"`
	Notes       string           `yaml:"notes,omitempty"`
	OAS         string           `yaml:"oas,omitempty"`
	Nodes       map[string]*Node `yaml:"nodes"`
	Edges       []Edge           `yaml:"edges"`
	Conditions  []Condition      `yaml:"conditions,omitempty"`

	// Computed indices (not serialized). Built by BuildEdgeIndex().
	edgesByTarget    map[string][]int     `yaml:"-"` // "node.input" → edge indices
	edgesBySource    map[string][]int     `yaml:"-"` // "node.output" → edge indices
	SatisfiersByToken map[string][]string `yaml:"-"` // requirement token → node names that satisfy it
}

// Workflow describes a named workflow (sequence of operations) within the graph.
type Workflow struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Steps       []string `yaml:"steps,omitempty"`
}

// Node represents a single logical API operation in the graph.
type Node struct {
	Name         string   // populated from map key during parsing, not from YAML
	Description  string   `yaml:"description"`
	Adapter      string   `yaml:"adapter"`
	Tags         []string `yaml:"tags,omitempty"`
	Inputs       []Input  `yaml:"inputs"`
	Outputs      []Output `yaml:"outputs"`
	Cleanup      string   `yaml:"cleanup,omitempty"`
	CycleBreaker bool     `yaml:"cycleBreaker,omitempty"`
	OAS          *OASRef  `yaml:"oas,omitempty"`
	Requires     []string `yaml:"requires,omitempty"`
	Satisfies    []string `yaml:"satisfies,omitempty"`
	Preferred    bool     `yaml:"preferred,omitempty"`
}

// OASRef links a graph node to an OAS operation.
type OASRef struct {
	OperationID string `yaml:"operationId"`
	Spec        string `yaml:"spec,omitempty"`
}

// Input describes a single input parameter for a node.
type Input struct {
	Name        string      `yaml:"name"`
	Type        string      `yaml:"type"`
	Description string      `yaml:"description,omitempty"`
	Optional    bool        `yaml:"optional,omitempty"`
	Default     any         `yaml:"default,omitempty"`
	NoAutoWire  bool        `yaml:"noAutoWire,omitempty"`
	Constraints *Constraint `yaml:"constraints,omitempty"`
}

// Constraint captures validation rules for an input value.
type Constraint struct {
	Min         *float64 `yaml:"min,omitempty"`
	Max         *float64 `yaml:"max,omitempty"`
	MinLength   *int     `yaml:"minLength,omitempty"`
	MaxLength   *int     `yaml:"maxLength,omitempty"`
	Pattern     string   `yaml:"pattern,omitempty"`
	Description string   `yaml:"description,omitempty"`
}

// Output describes a single output value produced by a node.
type Output struct {
	Name          string  `yaml:"name"`
	Type          string  `yaml:"type"`
	Description   string  `yaml:"description,omitempty"`
	ElementFields []Field `yaml:"elementFields,omitempty"`
}

// Field describes a sub-field within an array element.
type Field struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
	Path string `yaml:"path,omitempty"` // JSON extraction path (gjson); defaults to Name
}

// EffectivePath returns the gjson extraction path for this field.
// If Path is set, it is returned; otherwise Name is used.
func (f Field) EffectivePath() string {
	if f.Path != "" {
		return f.Path
	}
	return f.Name
}

// Edge describes a data flow connection between a node output and a node input.
type Edge struct {
	From      string `yaml:"from"`
	To        string `yaml:"to"`
	Select    bool   `yaml:"select,omitempty"`
	Preferred bool   `yaml:"preferred,omitempty"`
	AutoWired bool   `yaml:"-"` // true if generated by auto-wire
}

// Condition describes a conditional requirement or ordering constraint.
type Condition struct {
	When    string   `yaml:"when"`
	Require []string `yaml:"require,omitempty"`
	Before  []string `yaml:"before,omitempty"`
}

// BuildEdgeIndex populates the computed edge indices for fast lookup.
func (g *Graph) BuildEdgeIndex() {
	g.edgesByTarget = make(map[string][]int, len(g.Edges))
	g.edgesBySource = make(map[string][]int, len(g.Edges))
	for i, edge := range g.Edges {
		g.edgesByTarget[edge.To] = append(g.edgesByTarget[edge.To], i)
		g.edgesBySource[edge.From] = append(g.edgesBySource[edge.From], i)
	}

	// Build satisfier index: token → node names that satisfy it.
	g.SatisfiersByToken = map[string][]string{}
	for name, node := range g.Nodes {
		for _, token := range node.Satisfies {
			g.SatisfiersByToken[token] = append(g.SatisfiersByToken[token], name)
		}
	}
}

// EdgesTo returns all edges targeting the given ref ("node.input").
func (g *Graph) EdgesTo(ref string) []Edge {
	if g.edgesByTarget == nil {
		return nil
	}
	indices := g.edgesByTarget[ref]
	edges := make([]Edge, len(indices))
	for i, idx := range indices {
		edges[i] = g.Edges[idx]
	}
	return edges
}

// EdgesFrom returns all edges sourced from the given ref ("node.output").
func (g *Graph) EdgesFrom(ref string) []Edge {
	if g.edgesBySource == nil {
		return nil
	}
	indices := g.edgesBySource[ref]
	edges := make([]Edge, len(indices))
	for i, idx := range indices {
		edges[i] = g.Edges[idx]
	}
	return edges
}

// TypeKind classifies a field type.
type TypeKind int

const (
	// TypeScalar represents built-in scalar types: string, integer, float, boolean, date, datetime, money.
	TypeScalar TypeKind = iota
	// TypeEnum represents an enumeration type: enum[v1, v2, v3].
	TypeEnum
	// TypeArray represents an array type: someType[].
	TypeArray
	// TypeCustom represents a domain-specific type like airportCode.
	TypeCustom
)

// FieldType is the parsed representation of a raw type string.
type FieldType struct {
	Kind       TypeKind
	Name       string   // scalar/custom name, or element type for arrays
	EnumValues []string // only for TypeEnum
	IsArray    bool     // true if original type ended with []
}

var scalarTypes = map[string]bool{
	"string":   true,
	"integer":  true,
	"float":    true,
	"boolean":  true,
	"date":     true,
	"datetime": true,
	"money":    true,
}

// ParseFieldType parses a raw type string into a FieldType.
//
// Parsing rules:
//   - Known scalars: string, integer, float, boolean, date, datetime, money
//   - enum[v1, v2, ...] → TypeEnum (whitespace trimmed around values, at least 1 value required)
//   - type[] suffix → TypeArray with IsArray=true, Name=element type
//   - Everything else → TypeCustom
func ParseFieldType(raw string) (FieldType, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return FieldType{}, fmt.Errorf("empty type string")
	}

	// Check for enum type
	if strings.HasPrefix(raw, "enum[") {
		if !strings.HasSuffix(raw, "]") {
			return FieldType{}, fmt.Errorf("malformed enum type: %q (missing closing bracket)", raw)
		}
		inner := raw[len("enum[") : len(raw)-1]
		parts := strings.Split(inner, ",")
		var values []string
		for _, p := range parts {
			v := strings.TrimSpace(p)
			if v != "" {
				values = append(values, v)
			}
		}
		if len(values) == 0 {
			return FieldType{}, fmt.Errorf("enum type has no values: %q", raw)
		}
		return FieldType{
			Kind:       TypeEnum,
			Name:       "enum",
			EnumValues: values,
		}, nil
	}

	// Check for array type
	if strings.HasSuffix(raw, "[]") {
		elemType := raw[:len(raw)-2]
		if elemType == "" {
			return FieldType{}, fmt.Errorf("array type has no element type: %q", raw)
		}
		ft := FieldType{
			Kind:    TypeArray,
			Name:    elemType,
			IsArray: true,
		}
		return ft, nil
	}

	// Check for scalar type
	if scalarTypes[raw] {
		return FieldType{
			Kind: TypeScalar,
			Name: raw,
		}, nil
	}

	// Everything else is a custom domain type
	return FieldType{
		Kind: TypeCustom,
		Name: raw,
	}, nil
}
