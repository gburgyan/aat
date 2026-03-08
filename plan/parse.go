package plan

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Parse unmarshals YAML bytes into a Plan.
func Parse(data []byte) (*Plan, error) {
	var p Plan
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("YAML parse error: %w", err)
	}
	if len(p.Execution.Steps) == 0 {
		return nil, fmt.Errorf("plan must have at least one execution step")
	}
	return &p, nil
}

// ParseFile reads a YAML file and parses it into a Plan.
func ParseFile(path string) (*Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading plan file: %w", err)
	}
	return Parse(data)
}

// UnmarshalYAML implements custom YAML unmarshalling for Assertions.
// Handles two formats:
//   - Mapping with mechanical/semantic keys (correct form)
//   - Flat list of assertion objects (common LLM mistake → treated as mechanical)
func (a *Assertions) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.SequenceNode:
		// Flat list → treat as mechanical assertions.
		var items []MechanicalAssertion
		if err := value.Decode(&items); err != nil {
			return err
		}
		a.Mechanical = items
		return nil
	case yaml.MappingNode:
		// Mapping → unmarshal normally.
		type rawAssertions Assertions
		var raw rawAssertions
		if err := value.Decode(&raw); err != nil {
			return err
		}
		*a = Assertions(raw)
		return nil
	default:
		return fmt.Errorf("unexpected YAML node kind %d for Assertions", value.Kind)
	}
}

// MarshalYAML implements custom YAML marshalling for StepValue.
// When only Default is set, marshal as a bare scalar instead of a mapping.
func (sv StepValue) MarshalYAML() (interface{}, error) {
	if !sv.Locked && sv.From == "" && sv.Select == nil && sv.Constraint == "" &&
		len(sv.Pool) == 0 && sv.PoolStrategy == nil &&
		sv.FromSelection == "" && sv.FromResolved == "" && sv.FromInput == "" && sv.Default != nil {
		return sv.Default, nil
	}
	type rawStepValue StepValue
	return rawStepValue(sv), nil
}

// UnmarshalYAML implements custom YAML unmarshalling for StepValue.
// Bare scalars (e.g., origin: "DEN") set Default only.
// Mappings unmarshal into the full StepValue struct.
func (sv *StepValue) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		// Bare scalar → set Default only
		var v any
		if err := value.Decode(&v); err != nil {
			return err
		}
		sv.Default = v
		return nil
	case yaml.MappingNode:
		// Mapping → unmarshal into the full struct.
		// Use a type alias to avoid infinite recursion.
		type rawStepValue StepValue
		var raw rawStepValue
		if err := value.Decode(&raw); err != nil {
			return err
		}
		*sv = StepValue(raw)
		return nil
	default:
		return fmt.Errorf("unexpected YAML node kind %d for StepValue", value.Kind)
	}
}
