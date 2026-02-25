package domain

// KnowledgeBase is the top-level container for domain knowledge.
// It holds concepts (semantic rules), type definitions, and value pools.
type KnowledgeBase struct {
	Concepts   map[string]*Concept   `yaml:"concepts"   json:"concepts"`
	Types      map[string]*TypeDef   `yaml:"types"      json:"types"`
	ValuePools map[string]*ValuePool `yaml:"valuePools"  json:"valuePools"`
}

// Concept describes a domain-specific rule or constraint that applies
// to one or more fields in the API graph. Concepts are used in LLM
// prompts to guide plan generation and value selection.
type Concept struct {
	Name        string              `yaml:"-"           json:"name"`
	Description string              `yaml:"description" json:"description"`
	AppliesTo   []string            `yaml:"applies_to"  json:"appliesTo"`
	Constraint  string              `yaml:"constraint"  json:"constraint"`
	Examples    map[string][]string `yaml:"examples"    json:"examples,omitempty"`
}

// TypeDef describes a domain-specific data type with format, validation,
// and optional composite fields. Types map to custom type names used in
// graph node inputs/outputs.
type TypeDef struct {
	Name        string               `yaml:"-"           json:"name"`
	Description string               `yaml:"description" json:"description"`
	Format      string               `yaml:"format"      json:"format"`
	Validation  string               `yaml:"validation"  json:"validation,omitempty"`
	Fields      map[string]*FieldDef `yaml:"fields"     json:"fields,omitempty"`
	Pool        string               `yaml:"pool"        json:"pool,omitempty"`
}

// FieldDef describes a field within a composite TypeDef.
type FieldDef struct {
	Type        string `yaml:"type"        json:"type"`
	Description string `yaml:"description" json:"description,omitempty"`
	Constraint  string `yaml:"constraint"  json:"constraint,omitempty"`
	Strategy    string `yaml:"strategy"    json:"strategy,omitempty"`
}

// ValuePool provides representative values for a domain type.
// At least one of Values (flat list) or Groups (categorized) must be present.
type ValuePool struct {
	Name        string              `yaml:"-"           json:"name"`
	Description string              `yaml:"description" json:"description"`
	Type        string              `yaml:"type"        json:"type"`
	Values      []string            `yaml:"values"      json:"values,omitempty"`
	Groups      map[string][]string `yaml:"groups"      json:"groups,omitempty"`
	// Annotations maps value → comment text extracted from YAML inline comments.
	// For example, {"ADT": "Adult", "CNN": "Child (2-11)"}.
	Annotations map[string]string `yaml:"-" json:"annotations,omitempty"`
	// SectionLabels maps the first value of a section → section label text
	// extracted from YAML head comments. For example, {"JFK": "Major US hubs"}.
	SectionLabels map[string]string `yaml:"-" json:"sectionLabels,omitempty"`
}
