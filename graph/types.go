package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gburgyan/aat/internal/protoreg"
	"github.com/gburgyan/aat/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// Graph is the top-level API graph model parsed from YAML.
// It describes the logical operations (nodes), requires/satisfies
// relationships, and conditional requirements (conditions) for an API workflow.
type Graph struct {
	Version        string               `yaml:"version"`
	Title          string               `yaml:"title,omitempty"`
	Description    string               `yaml:"description,omitempty"`
	Workflows      []Workflow           `yaml:"workflows,omitempty"`
	Examples       []WorkflowExample    `yaml:"examples,omitempty"`
	Notes          string               `yaml:"notes,omitempty"`
	OAS            string               `yaml:"oas,omitempty"`
	Proto          string               `yaml:"proto,omitempty"`
	ErrorDetection []ErrorDetectionRule `yaml:"errorDetection,omitempty"`
	Nodes          map[string]*Node     `yaml:"nodes"`
	Conditions     []Condition          `yaml:"conditions,omitempty"`

	// Computed index (not serialized). Built by BuildSatisfierIndex().
	SatisfiersByToken map[string][]string `yaml:"-"` // requirement token → node names that satisfy it
}

// WorkflowExample is an input/output pair for few-shot prompting in
// the workflow selection LLM call. Projects define these in graph.yaml.
type WorkflowExample struct {
	Input  string `yaml:"input"`
	Output string `yaml:"output"`
}

// AfterSpec holds one or more node names that an addon can splice after.
// YAML accepts both scalar ("nodeA") and list (["nodeA", "nodeB"]) forms.
// During composition the first matching node in the base plan wins.
type AfterSpec []string

// UnmarshalYAML accepts both scalar and list forms.
func (a *AfterSpec) UnmarshalYAML(unmarshal func(any) error) error {
	n, err := yamlx.Node(unmarshal)
	if err != nil {
		return err
	}
	switch n.Kind {
	case yaml.ScalarNode:
		var s string
		if err := unmarshal(&s); err != nil {
			return err
		}
		if s != "" {
			*a = AfterSpec{s}
		}
		return nil
	case yaml.SequenceNode:
		return unmarshal((*[]string)(a))
	default:
		return yamlx.KindError(n, "after", "a node name or a list of node names")
	}
}

// MarshalYAML emits a scalar for single value, list for multiple.
func (a AfterSpec) MarshalYAML() (interface{}, error) {
	if len(a) == 1 {
		return a[0], nil
	}
	return []string(a), nil
}

// First returns the first value or empty string.
func (a AfterSpec) First() string {
	if len(a) == 0 {
		return ""
	}
	return a[0]
}

// IsSet returns true if at least one value is present.
func (a AfterSpec) IsSet() bool {
	return len(a) > 0
}

// Contains returns true if nodeName is in the spec.
func (a AfterSpec) Contains(nodeName string) bool {
	for _, v := range a {
		if v == nodeName {
			return true
		}
	}
	return false
}

// String returns a human-readable representation.
func (a AfterSpec) String() string {
	if len(a) == 1 {
		return a[0]
	}
	return strings.Join([]string(a), ", ")
}

// Workflow describes a named workflow (sequence of operations) within the graph.
type Workflow struct {
	Name          string                 `yaml:"name"`
	Description   string                 `yaml:"description,omitempty"`
	Kind          string                 `yaml:"kind,omitempty"`          // "addon" for sub-workflows, "slot" for slot options
	Template      string                 `yaml:"template,omitempty"`      // path to plan template YAML (relative to graph file)
	After         AfterSpec              `yaml:"after,omitempty"`         // addon: node(s) to splice after in the base workflow
	Wire          map[string]string      `yaml:"wire,omitempty"`          // addon: default AUTOWIRE overrides
	Priority      int                    `yaml:"priority,omitempty"`      // addon: composition ordering (lower = earlier, default 0)
	Slots         []SlotDef              `yaml:"slots,omitempty"`         // choice points (only on base workflows)
	Inject        map[string]InjectValue `yaml:"inject,omitempty"`        // slot option: input values to apply across the composed plan
	SelectionHint string                 `yaml:"selectionHint,omitempty"` // guidance for LLM workflow selection
	Deprecated    bool                   `yaml:"deprecated,omitempty"`    // when true, excluded from menus and rejected during selection
}

// SlotDef describes a named decision point in a workflow with mutually exclusive options.
type SlotDef struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Options     []string `yaml:"options"`           // workflow names (kind: slot)
	Default     string   `yaml:"default,omitempty"` // default option name
}

// IsAddon returns true if this workflow is a bolt-on sub-workflow.
func (w Workflow) IsAddon() bool {
	return w.Kind == "addon"
}

// IsSlot returns true if this workflow is a slot option template.
func (w Workflow) IsSlot() bool {
	return w.Kind == "slot"
}

// Node represents a single logical API operation in the graph.
type Node struct {
	Name           string               `yaml:"name,omitempty"` // populated from the map key during parsing; not written when empty
	Description    string               `yaml:"description"`
	Adapter        string               `yaml:"adapter"`
	Tags           []string             `yaml:"tags,omitempty"`
	Inputs         []Input              `yaml:"inputs"`
	Outputs        []Output             `yaml:"outputs"`
	Cleanup        CleanupPairing       `yaml:"cleanup,omitempty"`
	CycleBreaker   bool                 `yaml:"cycleBreaker,omitempty"`
	OAS            *OASRef              `yaml:"oas,omitempty"`
	Proto          *ProtoRef            `yaml:"proto,omitempty"`
	Requires       []string             `yaml:"requires,omitempty"`
	Satisfies      []string             `yaml:"satisfies,omitempty"`
	Preferred      bool                 `yaml:"preferred,omitempty"`
	ErrorDetection []ErrorDetectionRule `yaml:"errorDetection,omitempty"`
}

// OASRef links a graph node to an OAS operation.
type OASRef struct {
	OperationID string `yaml:"operationId"`
	Spec        string `yaml:"spec,omitempty"`
}

// ProtoRef links a graph node to a gRPC method. It is written either as a
// scalar naming the method the way the wire does,
//
//	proto: shop.v1.Carts/CreateCart
//
// or as a mapping, which a node needs only to name its own descriptor set:
//
//	proto:
//	  service: shop.v1.Carts
//	  method: CreateCart
//	  descriptor: carts.protoset
type ProtoRef struct {
	Service string `yaml:"service"`
	Method  string `yaml:"method"`
	// Descriptor names the descriptor set holding the service, for a node
	// whose service is not in the graph's own. It mirrors OASRef.Spec.
	Descriptor string `yaml:"descriptor,omitempty"`
}

// FullMethod returns the method as gRPC names it on the wire.
func (r ProtoRef) FullMethod() string {
	return "/" + r.Service + "/" + r.Method
}

// String returns the method as a graph writes it in the scalar form.
func (r ProtoRef) String() string {
	return r.Service + "/" + r.Method
}

// rawProtoRef drops the custom unmarshaler so the mapping form decodes
// strictly; see internal/yamlx.
type rawProtoRef ProtoRef

// UnmarshalYAML accepts the scalar and mapping forms. It uses the callback
// form so strict decoding reaches the mapping (see internal/yamlx).
func (r *ProtoRef) UnmarshalYAML(unmarshal func(any) error) error {
	n, err := yamlx.Node(unmarshal)
	if err != nil {
		return err
	}
	switch n.Kind {
	case yaml.ScalarNode:
		var ref string
		if err := unmarshal(&ref); err != nil {
			return err
		}
		service, method, ok := protoreg.SplitFullMethod(ref)
		if !ok {
			return &yaml.TypeError{Errors: []string{fmt.Sprintf(
				"line %d: proto %q must name a service and a method, as in \"shop.v1.Carts/CreateCart\"", n.Line, ref)}}
		}
		r.Service, r.Method = service, method
		return nil
	case yaml.MappingNode:
		var raw rawProtoRef
		if err := unmarshal(&raw); err != nil {
			return err
		}
		if raw.Service == "" || raw.Method == "" {
			return &yaml.TypeError{Errors: []string{fmt.Sprintf(
				"line %d: proto needs both a service and a method", n.Line)}}
		}
		*r = ProtoRef(raw)
		return nil
	default:
		return yamlx.KindError(n, "proto", "a service/method reference or a mapping")
	}
}

// MarshalYAML writes the scalar form unless the ref names its own descriptor
// set, so a generated graph reads the way one is written by hand.
func (r ProtoRef) MarshalYAML() (any, error) {
	if r.Descriptor == "" {
		return r.String(), nil
	}
	return rawProtoRef(r), nil
}

// Input describes a single input parameter for a node.
type Input struct {
	Name         string        `yaml:"name"`
	Type         string        `yaml:"type"`
	Description  string        `yaml:"description,omitempty"`
	Optional     bool          `yaml:"optional,omitempty"`
	Configurable bool          `yaml:"configurable,omitempty"`
	Default      *InputDefault `yaml:"default,omitempty"`
	Constraints  *Constraint   `yaml:"constraints,omitempty"`
}

// InputDefault describes a default value specification for a graph input.
// It supports literal values, pools, from references, and selection configs.
// Custom UnmarshalYAML/MarshalYAML preserves backward compatibility with
// `default: "literal"` syntax.
type InputDefault struct {
	Value any   `yaml:"value,omitempty"`
	Pool  []any `yaml:"pool,omitempty"`
	// PoolRef names a value pool of the domain file to draw from instead of
	// listing a pool here: "airportCodes", or "airportCodes.us" for one of
	// its groups. The engine reads the pool at run time.
	PoolRef      string              `yaml:"poolRef,omitempty"`
	PoolStrategy *string             `yaml:"poolStrategy,omitempty"`
	Constraint   string              `yaml:"constraint,omitempty"`
	From         string              `yaml:"from,omitempty"`
	FromResolved string              `yaml:"fromResolved,omitempty"`
	Select       *InputDefaultSelect `yaml:"select,omitempty"`
	// Layer names the layer that set this default, when ApplyLayers put one in
	// place. It isn't read from YAML.
	Layer string `yaml:"-" json:"-"`
}

// InputDefaultSelect describes an array selection within a graph input default.
// It mirrors plan.SelectionConfig to keep graph as a leaf package.
type InputDefaultSelect struct {
	Strategy  string `yaml:"strategy"`
	Field     string `yaml:"field,omitempty"`
	Filter    string `yaml:"filter,omitempty"`
	Index     int    `yaml:"index,omitempty"`
	SortField string `yaml:"sortField,omitempty"`
	OnTie     string `yaml:"onTie,omitempty"` // for min/max: "first" or "fail" when elements tie
}

// HasValue reports whether this InputDefault carries any meaningful value.
func (d *InputDefault) HasValue() bool {
	if d == nil {
		return false
	}
	return d.Value != nil || len(d.Pool) > 0 || d.PoolRef != "" || d.From != "" || d.FromResolved != ""
}

// IsLiteralOnly reports whether this InputDefault is a simple literal value
// with no pool, from, constraint, or selection.
func (d *InputDefault) IsLiteralOnly() bool {
	if d == nil {
		return false
	}
	return d.Value != nil && len(d.Pool) == 0 && d.PoolRef == "" && d.From == "" && d.FromResolved == "" && d.Select == nil && d.Constraint == ""
}

// EffectiveValue returns the literal value if this is a literal-only default,
// or nil otherwise.
func (d *InputDefault) EffectiveValue() any {
	if d == nil {
		return nil
	}
	if d.IsLiteralOnly() {
		return d.Value
	}
	return nil
}

// LiteralDefault creates an InputDefault with a simple literal value.
// This is a convenience constructor for code that previously assigned
// `input.Default = someValue`.
func LiteralDefault(v any) *InputDefault {
	if v == nil {
		return nil
	}
	return &InputDefault{Value: v}
}

// UnmarshalYAML handles scalar literals (`default: "ADT"`), the list
// shorthand for a pool (`default: [a, b]`), and rich map syntax
// (`default: {pool: [...], constraint: "..."}`). It uses the callback form so
// strict decoding reaches the mapping (see internal/yamlx).
func (d *InputDefault) UnmarshalYAML(unmarshal func(any) error) error {
	n, err := yamlx.Node(unmarshal)
	if err != nil {
		return err
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return unmarshal(&d.Value)
	case yaml.SequenceNode:
		return unmarshal(&d.Pool)
	case yaml.MappingNode:
		// The raw type has no methods, which avoids infinite recursion.
		type rawInputDefault InputDefault
		var raw rawInputDefault
		if err := unmarshal(&raw); err != nil {
			return err
		}
		*d = InputDefault(raw)
		return nil
	default:
		return yamlx.KindError(n, "an input default", "a scalar, a list, or a mapping")
	}
}

// MarshalYAML emits a bare scalar for literal-only defaults, preserving
// backward-compatible `default: "ADT"` syntax. Rich defaults emit the
// full map structure.
func (d InputDefault) MarshalYAML() (interface{}, error) {
	if d.IsLiteralOnly() {
		return d.Value, nil
	}
	// The raw type has no methods, which avoids infinite recursion.
	type rawInputDefault InputDefault
	return rawInputDefault(d), nil
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
//
// An output is extracted from the response by the node's template, unless
// FromInput names one of the node's inputs: then the output is that input as
// the step sent it. That serves an API where the client names what it creates
// and the reply doesn't say it back, so a later step, or the node's cleanup,
// can read the name as an output like any other.
type Output struct {
	Name          string  `yaml:"name"`
	Type          string  `yaml:"type"`
	Description   string  `yaml:"description,omitempty"`
	Optional      bool    `yaml:"optional,omitempty"`
	Display       string  `yaml:"display,omitempty"`
	FromInput     string  `yaml:"fromInput,omitempty"`
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

// Condition describes a conditional requirement or ordering constraint.
type Condition struct {
	When    string   `yaml:"when"`
	Require []string `yaml:"require,omitempty"`
	Before  []string `yaml:"before,omitempty"`
}

// ErrorDetectionRule defines a rule for detecting errors in response bodies.
// When a rule triggers on a 2xx response, the step is treated as a failure.
type ErrorDetectionRule struct {
	Path    string              `yaml:"path"`
	Rule    string              `yaml:"rule"`              // "exists", "non-empty", "equals"
	Value   any                 `yaml:"value,omitempty"`   // required for "equals" rule
	Details *ErrorDetailMapping `yaml:"details,omitempty"` // optional paths for extracting error details
}

// ErrorDetailMapping maps gjson paths for extracting error details from a response body.
type ErrorDetailMapping struct {
	Message  string `yaml:"message,omitempty"`
	Code     string `yaml:"code,omitempty"`
	Category string `yaml:"category,omitempty"`
}

// BuildSatisfierIndex populates the computed satisfier index for fast lookup.
// Each token's satisfiers are sorted by name, so everything that walks them,
// generated docs and backward chaining alike, comes out the same every time
// rather than in map order.
func (g *Graph) BuildSatisfierIndex() {
	g.SatisfiersByToken = map[string][]string{}
	for name, node := range g.Nodes {
		for _, token := range node.Satisfies {
			g.SatisfiersByToken[token] = append(g.SatisfiersByToken[token], name)
		}
	}
	for _, satisfiers := range g.SatisfiersByToken {
		sort.Strings(satisfiers)
	}
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
