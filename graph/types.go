package graph

import (
	"fmt"
	"strings"
)

// Graph is the top-level API graph model parsed from YAML.
// It describes the logical operations (nodes), data flow (edges),
// and conditional requirements (conditions) for an API workflow.
type Graph struct {
	Version    string           `yaml:"version"`
	Nodes      map[string]*Node `yaml:"nodes"`
	Edges      []Edge           `yaml:"edges"`
	Conditions []Condition      `yaml:"conditions,omitempty"`
}

// Node represents a single logical API operation in the graph.
type Node struct {
	Name        string   // populated from map key during parsing, not from YAML
	Description string   `yaml:"description"`
	Adapter     string   `yaml:"adapter"`
	Inputs      []Input  `yaml:"inputs"`
	Outputs     []Output `yaml:"outputs"`
	Cleanup     string   `yaml:"cleanup,omitempty"`
}

// Input describes a single input parameter for a node.
type Input struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Description string `yaml:"description,omitempty"`
	Optional    bool   `yaml:"optional,omitempty"`
	Default     any    `yaml:"default,omitempty"`
	Source      string `yaml:"source,omitempty"`
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
}

// Edge describes a data flow connection between a node output and a node input.
type Edge struct {
	From   string `yaml:"from"`
	To     string `yaml:"to"`
	Select bool   `yaml:"select,omitempty"`
}

// Condition describes a conditional requirement or ordering constraint.
type Condition struct {
	When    string   `yaml:"when"`
	Require []string `yaml:"require,omitempty"`
	Before  []string `yaml:"before,omitempty"`
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
