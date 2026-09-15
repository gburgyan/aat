package plan

import (
	"fmt"
	"os"
	"reflect"

	"github.com/gburgyan/aat/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// Parse unmarshals YAML bytes into a Plan. Keys that no plan field accepts
// are errors.
func Parse(data []byte) (*Plan, error) {
	var p Plan
	if err := yamlx.Decode(data, &p); err != nil {
		return nil, err
	}
	if len(p.Execution.Steps) == 0 {
		return nil, fmt.Errorf("plan must have at least one execution step")
	}
	return &p, nil
}

// ParseFile reads a YAML file and parses it into a Plan. Errors name the file.
func ParseFile(path string) (*Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading plan file: %w", err)
	}
	p, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

// UnmarshalYAML implements custom YAML unmarshalling for Assertions.
// Handles two formats:
//   - Mapping with a mechanical key (the documented form)
//   - Flat list of assertion objects (treated as mechanical)
//
// It uses the callback form so strict decoding reaches the assertions (see
// internal/yamlx).
func (a *Assertions) UnmarshalYAML(unmarshal func(any) error) error {
	n, err := yamlx.Node(unmarshal)
	if err != nil {
		return err
	}
	switch n.Kind {
	case yaml.SequenceNode:
		return unmarshal(&a.Mechanical)
	case yaml.MappingNode:
		type rawAssertions Assertions
		var raw rawAssertions
		if err := unmarshal(&raw); err != nil {
			return err
		}
		*a = Assertions(raw)
		return nil
	default:
		return yamlx.KindError(n, "assertions", "a mapping or a list")
	}
}

// MarshalYAML implements custom YAML marshalling for StepValue.
// When only Default is set and it is a scalar or a list, it is written bare,
// which reads back the same way. A map stays under default:, since a bare
// mapping reads as the step value's own keys.
func (sv StepValue) MarshalYAML() (interface{}, error) {
	if !sv.Locked && sv.From == "" && sv.Select == nil && sv.Constraint == "" &&
		len(sv.Pool) == 0 && sv.PoolStrategy == nil &&
		sv.FromSelection == "" && sv.FromResolved == "" && sv.FromInput == "" && sv.Default != nil &&
		reflect.ValueOf(sv.Default).Kind() != reflect.Map {
		return sv.Default, nil
	}
	type rawStepValue StepValue
	return rawStepValue(sv), nil
}

// UnmarshalYAML implements custom YAML unmarshalling for StepValue.
//   - A bare scalar (origin: "DEN") or list (skus: [SKU-1, SKU-2]) is the
//     literal value, and sets Default only. A list is the list itself, as in a
//     slot's inject; pool: lists alternatives.
//   - A mapping unmarshals into the full StepValue struct. Its value key, the
//     literal form of graph defaults, layers, and inject, is read as default.
//
// It uses the callback form so strict decoding reaches the mapping (see
// internal/yamlx).
func (sv *StepValue) UnmarshalYAML(unmarshal func(any) error) error {
	n, err := yamlx.Node(unmarshal)
	if err != nil {
		return err
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return unmarshal(&sv.Default)
	case yaml.SequenceNode:
		var list []any
		if err := unmarshal(&list); err != nil {
			return err
		}
		sv.Default = list
		return nil
	case yaml.MappingNode:
		if err := readValueKeyAsDefault(n); err != nil {
			return err
		}
		// The raw type has no methods, which avoids infinite recursion.
		type rawStepValue StepValue
		var raw rawStepValue
		if err := unmarshal(&raw); err != nil {
			return err
		}
		*sv = StepValue(raw)
		return nil
	default:
		return yamlx.KindError(n, "a step value", "a scalar, a list, or a mapping")
	}
}

// readValueKeyAsDefault renames a step value mapping's value key to default, so
// {value: [a, b]} means in a plan what it means in a graph default, a layer, or
// inject. The decode callback reads this same node, so renaming the key in
// place keeps strict decoding for every other key. A mapping with both keys is
// an error.
func readValueKeyAsDefault(n *yaml.Node) error {
	var valueKey *yaml.Node
	hasDefault := false
	for i := 0; i+1 < len(n.Content); i += 2 {
		switch key := n.Content[i]; key.Value {
		case "value":
			valueKey = key
		case "default":
			hasDefault = true
		}
	}
	if valueKey == nil {
		return nil
	}
	if hasDefault {
		return &yaml.TypeError{Errors: []string{
			fmt.Sprintf("line %d: a step value takes value or default, not both", valueKey.Line),
		}}
	}
	valueKey.Value = "default"
	return nil
}
