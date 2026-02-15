package domain

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
)

// GetConcept returns the concept with the given name, or nil if not found.
func (kb *KnowledgeBase) GetConcept(name string) *Concept {
	if kb == nil {
		return nil
	}
	return kb.Concepts[name]
}

// GetType returns the type definition with the given name, or nil if not found.
func (kb *KnowledgeBase) GetType(name string) *TypeDef {
	if kb == nil {
		return nil
	}
	return kb.Types[name]
}

// GetPool returns the value pool with the given name, or nil if not found.
func (kb *KnowledgeBase) GetPool(name string) *ValuePool {
	if kb == nil {
		return nil
	}
	return kb.ValuePools[name]
}

// ConceptsForField returns all concepts whose AppliesTo list contains fieldName.
func (kb *KnowledgeBase) ConceptsForField(fieldName string) []*Concept {
	if kb == nil {
		return nil
	}
	var result []*Concept
	for _, c := range kb.Concepts {
		for _, a := range c.AppliesTo {
			if a == fieldName {
				result = append(result, c)
				break
			}
		}
	}
	// Sort by name for deterministic output
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// TypeForField returns the type definition matching the given type name,
// or nil if not found. This is an alias for GetType provided for semantic
// clarity when resolving graph field types.
func (kb *KnowledgeBase) TypeForField(typeName string) *TypeDef {
	return kb.GetType(typeName)
}

// AllValues returns all values from the named pool, combining both flat
// Values and all Groups entries. Returns nil if the pool is not found.
func (kb *KnowledgeBase) AllValues(poolName string) []string {
	if kb == nil {
		return nil
	}
	p := kb.ValuePools[poolName]
	if p == nil {
		return nil
	}

	var all []string
	all = append(all, p.Values...)

	// Add groups in sorted key order for determinism
	keys := make([]string, 0, len(p.Groups))
	for k := range p.Groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		all = append(all, p.Groups[k]...)
	}

	return all
}

// SampleValues returns up to n random values from the named pool.
// If the pool has fewer values than n, all values are returned (shuffled).
// Returns nil if the pool is not found.
func (kb *KnowledgeBase) SampleValues(poolName string, n int) []string {
	all := kb.AllValues(poolName)
	if len(all) == 0 {
		return nil
	}

	if n >= len(all) {
		// Shuffle a copy and return all
		result := make([]string, len(all))
		copy(result, all)
		rand.Shuffle(len(result), func(i, j int) {
			result[i], result[j] = result[j], result[i]
		})
		return result
	}

	// Fisher-Yates partial shuffle: pick n elements
	result := make([]string, len(all))
	copy(result, all)
	for i := 0; i < n; i++ {
		j := i + rand.IntN(len(result)-i)
		result[i], result[j] = result[j], result[i]
	}
	return result[:n]
}

// FormatForPrompt serializes the full KnowledgeBase as structured text
// suitable for inclusion in LLM prompts. Output is deterministic (sorted keys).
// Pool values are truncated to 10 entries for readability.
func (kb *KnowledgeBase) FormatForPrompt() string {
	if kb == nil {
		return ""
	}

	var b strings.Builder

	// Concepts
	if len(kb.Concepts) > 0 {
		b.WriteString("# Domain Concepts\n\n")
		for _, name := range sortedKeys(kb.Concepts) {
			c := kb.Concepts[name]
			fmt.Fprintf(&b, "## %s\n", name)
			fmt.Fprintf(&b, "%s\n", c.Description)
			fmt.Fprintf(&b, "Applies to: %s\n", strings.Join(c.AppliesTo, ", "))
			if c.Constraint != "" {
				fmt.Fprintf(&b, "Constraint: %s\n", c.Constraint)
			}
			if len(c.Examples) > 0 {
				b.WriteString("Examples:\n")
				for _, ek := range sortedKeys(c.Examples) {
					fmt.Fprintf(&b, "  %s: %s\n", ek, strings.Join(c.Examples[ek], ", "))
				}
			}
			b.WriteString("\n")
		}
	}

	// Types
	if len(kb.Types) > 0 {
		b.WriteString("# Domain Types\n\n")
		for _, name := range sortedKeys(kb.Types) {
			t := kb.Types[name]
			fmt.Fprintf(&b, "## %s\n", name)
			fmt.Fprintf(&b, "%s\n", t.Description)
			fmt.Fprintf(&b, "Format: %s\n", t.Format)
			if t.Validation != "" {
				fmt.Fprintf(&b, "Validation: %s\n", t.Validation)
			}
			if t.Pool != "" {
				fmt.Fprintf(&b, "Pool: %s\n", t.Pool)
			}
			if len(t.Fields) > 0 {
				b.WriteString("Fields:\n")
				for _, fname := range sortedKeys(t.Fields) {
					f := t.Fields[fname]
					fmt.Fprintf(&b, "  %s (%s): %s\n", fname, f.Type, f.Description)
				}
			}
			b.WriteString("\n")
		}
	}

	// Value Pools
	if len(kb.ValuePools) > 0 {
		b.WriteString("# Value Pools\n\n")
		for _, name := range sortedKeys(kb.ValuePools) {
			p := kb.ValuePools[name]
			fmt.Fprintf(&b, "## %s\n", name)
			fmt.Fprintf(&b, "%s\n", p.Description)
			fmt.Fprintf(&b, "Type: %s\n", p.Type)
			if len(p.Values) > 0 {
				b.WriteString("Values: ")
				writePoolValues(&b, p.Values, 10, p.Annotations)
				b.WriteString("\n")
			}
			if len(p.Groups) > 0 {
				b.WriteString("Groups:\n")
				for _, gk := range sortedKeys(p.Groups) {
					fmt.Fprintf(&b, "  %s: ", gk)
					writePoolValues(&b, p.Groups[gk], 10, p.Annotations)
					b.WriteString("\n")
				}
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

// FormatConceptsForPrompt formats a subset of concepts for LLM prompts.
// If no names are given, all concepts are included.
func (kb *KnowledgeBase) FormatConceptsForPrompt(names ...string) string {
	if kb == nil {
		return ""
	}

	var selected map[string]*Concept
	if len(names) == 0 {
		selected = kb.Concepts
	} else {
		selected = make(map[string]*Concept)
		for _, name := range names {
			if c, ok := kb.Concepts[name]; ok {
				selected[name] = c
			}
		}
	}

	if len(selected) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("# Domain Concepts\n\n")
	for _, name := range sortedKeys(selected) {
		c := selected[name]
		fmt.Fprintf(&b, "## %s\n", name)
		fmt.Fprintf(&b, "%s\n", c.Description)
		fmt.Fprintf(&b, "Applies to: %s\n", strings.Join(c.AppliesTo, ", "))
		if c.Constraint != "" {
			fmt.Fprintf(&b, "Constraint: %s\n", c.Constraint)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// FormatTypesForPrompt formats a subset of type definitions for LLM prompts.
// If no names are given, all types are included.
func (kb *KnowledgeBase) FormatTypesForPrompt(names ...string) string {
	if kb == nil {
		return ""
	}

	var selected map[string]*TypeDef
	if len(names) == 0 {
		selected = kb.Types
	} else {
		selected = make(map[string]*TypeDef)
		for _, name := range names {
			if t, ok := kb.Types[name]; ok {
				selected[name] = t
			}
		}
	}

	if len(selected) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("# Domain Types\n\n")
	for _, name := range sortedKeys(selected) {
		t := selected[name]
		fmt.Fprintf(&b, "## %s\n", name)
		fmt.Fprintf(&b, "%s\n", t.Description)
		fmt.Fprintf(&b, "Format: %s\n", t.Format)
		if t.Validation != "" {
			fmt.Fprintf(&b, "Validation: %s\n", t.Validation)
		}
		if t.Pool != "" {
			fmt.Fprintf(&b, "Pool: %s\n", t.Pool)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// writePoolValues writes up to max values, followed by "..." if truncated.
// When annotations are provided, values with annotations are formatted as "VAL (annotation)".
func writePoolValues(b *strings.Builder, values []string, max int, annotations map[string]string) {
	n := len(values)
	if n > max {
		n = max
	}
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(values[i])
		if ann, ok := annotations[values[i]]; ok {
			b.WriteString(" (")
			b.WriteString(ann)
			b.WriteString(")")
		}
	}
	if len(values) > max {
		b.WriteString(", ...")
	}
}

// sortedKeys returns the keys of a map in sorted order.
// It works with any map[string]V via a generic-free approach.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
