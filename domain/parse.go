package domain

import (
	"fmt"
	"os"
	"strings"

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

	extractValueAnnotations(data, &kb)

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

// extractValueAnnotations does a second YAML parse using yaml.Node to capture
// inline comments (LineComment) from value pool entries, storing them as
// annotations on the corresponding ValuePool.
func extractValueAnnotations(data []byte, kb *KnowledgeBase) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return
	}

	// Find the "valuePools" key in the top-level mapping.
	var poolsNode *yaml.Node
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value == "valuePools" {
			poolsNode = doc.Content[i+1]
			break
		}
	}
	if poolsNode == nil || poolsNode.Kind != yaml.MappingNode {
		return
	}

	// Iterate each pool: key=poolName, value=pool mapping
	for i := 0; i+1 < len(poolsNode.Content); i += 2 {
		poolName := poolsNode.Content[i].Value
		poolNode := poolsNode.Content[i+1]
		if poolNode.Kind != yaml.MappingNode {
			continue
		}
		pool := kb.ValuePools[poolName]
		if pool == nil {
			continue
		}

		for j := 0; j+1 < len(poolNode.Content); j += 2 {
			key := poolNode.Content[j].Value
			valNode := poolNode.Content[j+1]

			switch key {
			case "values":
				extractSequenceAnnotations(valNode, pool)
			case "groups":
				if valNode.Kind != yaml.MappingNode {
					continue
				}
				for k := 0; k+1 < len(valNode.Content); k += 2 {
					extractSequenceAnnotations(valNode.Content[k+1], pool)
				}
			}
		}
	}
}

// extractSequenceAnnotations extracts LineComment annotations from scalar items
// in a YAML sequence node and stores them in pool.Annotations.
func extractSequenceAnnotations(seqNode *yaml.Node, pool *ValuePool) {
	if seqNode.Kind != yaml.SequenceNode {
		return
	}
	for _, item := range seqNode.Content {
		if item.Kind != yaml.ScalarNode || item.LineComment == "" {
			continue
		}
		comment := strings.TrimSpace(strings.TrimLeft(item.LineComment, "#"))
		if comment == "" {
			continue
		}
		if pool.Annotations == nil {
			pool.Annotations = make(map[string]string)
		}
		pool.Annotations[item.Value] = comment
	}
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
				if p.Annotations != nil {
					merged.Annotations = make(map[string]string, len(p.Annotations))
					for k, v := range p.Annotations {
						merged.Annotations[k] = v
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
				if p.Annotations != nil {
					if existing.Annotations == nil {
						existing.Annotations = make(map[string]string)
					}
					for k, v := range p.Annotations {
						existing.Annotations[k] = v
					}
				}
			}
		}
	}

	return result
}
