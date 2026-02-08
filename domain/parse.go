package domain

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Parse unmarshals YAML bytes into a KnowledgeBase, populates Name fields
// from map keys, and validates the result.
func Parse(data []byte) (*KnowledgeBase, error) {
	var kb KnowledgeBase
	if err := yaml.Unmarshal(data, &kb); err != nil {
		return nil, fmt.Errorf("YAML parse error: %w", err)
	}

	// Populate Name from map keys
	for name, c := range kb.Concepts {
		if c == nil {
			continue
		}
		c.Name = name
	}
	for name, t := range kb.Types {
		if t == nil {
			continue
		}
		t.Name = name
	}
	for name, p := range kb.ValuePools {
		if p == nil {
			continue
		}
		p.Name = name
	}

	if err := Validate(&kb); err != nil {
		return nil, err
	}

	return &kb, nil
}

// ParseFile reads a YAML file from disk and parses it into a KnowledgeBase.
func ParseFile(path string) (*KnowledgeBase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading domain file: %w", err)
	}
	return Parse(data)
}

// Merge combines multiple KnowledgeBases into one. For concepts and types,
// later entries override earlier ones with the same key. For value pools,
// values are merged additively: Values are appended and Groups entries
// from later bases are added (new groups) or appended (existing groups).
func Merge(bases ...*KnowledgeBase) *KnowledgeBase {
	result := &KnowledgeBase{
		Concepts:   make(map[string]*Concept),
		Types:      make(map[string]*TypeDef),
		ValuePools: make(map[string]*ValuePool),
	}

	for _, kb := range bases {
		if kb == nil {
			continue
		}

		for name, c := range kb.Concepts {
			result.Concepts[name] = c
		}
		for name, t := range kb.Types {
			result.Types[name] = t
		}
		for name, p := range kb.ValuePools {
			existing, ok := result.ValuePools[name]
			if !ok {
				// Copy the pool so we don't mutate the original
				merged := *p
				merged.Values = append([]string(nil), p.Values...)
				if p.Groups != nil {
					merged.Groups = make(map[string][]string, len(p.Groups))
					for k, v := range p.Groups {
						merged.Groups[k] = append([]string(nil), v...)
					}
				}
				result.ValuePools[name] = &merged
			} else {
				// Merge additively
				existing.Values = append(existing.Values, p.Values...)
				if p.Groups != nil {
					if existing.Groups == nil {
						existing.Groups = make(map[string][]string)
					}
					for k, v := range p.Groups {
						existing.Groups[k] = append(existing.Groups[k], v...)
					}
				}
			}
		}
	}

	return result
}
