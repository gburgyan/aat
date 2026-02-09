package plan

import "time"

// Plan is the top-level execution plan parsed from YAML.
// It describes what steps to execute, their input values,
// and optional metadata about the intent and graph context.
type Plan struct {
	Metadata  Metadata  `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Graph     string    `yaml:"graph,omitempty" json:"graph,omitempty"`
	Intent    Intent    `yaml:"intent,omitempty" json:"intent,omitempty"`
	Execution Execution `yaml:"execution" json:"execution"`
}

// Metadata captures provenance information about when and why a plan was created.
type Metadata struct {
	Created      time.Time `yaml:"created,omitempty" json:"created,omitempty"`
	Prompt       string    `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	GraphVersion string    `yaml:"graphVersion,omitempty" json:"graphVersion,omitempty"`
}

// Intent describes the high-level goal and constraints of the plan.
type Intent struct {
	Goal        string       `yaml:"goal,omitempty" json:"goal,omitempty"`
	Description string       `yaml:"description,omitempty" json:"description,omitempty"`
	Constraints *Constraints `yaml:"constraints,omitempty" json:"constraints,omitempty"`
}

// Constraints classifies plan constraints by enforcement level.
type Constraints struct {
	Hard []Constraint `yaml:"hard,omitempty" json:"hard,omitempty"`
	Soft []Constraint `yaml:"soft,omitempty" json:"soft,omitempty"`
	Free []string     `yaml:"free,omitempty" json:"free,omitempty"`
}

// Constraint describes a single requirement or preference.
type Constraint struct {
	Type        string   `yaml:"type" json:"type"`
	Name        string   `yaml:"name,omitempty" json:"name,omitempty"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	AppliesTo   []string `yaml:"applies_to,omitempty" json:"appliesTo,omitempty"`
}

// Execution holds the ordered steps, verification, and cleanup for a plan.
type Execution struct {
	Steps        []Step             `yaml:"steps" json:"steps"`
	Verification []VerificationStep `yaml:"verification,omitempty" json:"verification,omitempty"`
	Cleanup      []CleanupStep      `yaml:"cleanup,omitempty" json:"cleanup,omitempty"`
}

// Step describes a single execution step targeting a graph node.
type Step struct {
	Node          string               `yaml:"node" json:"node"`
	DependsOn     []string             `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	Description   string               `yaml:"description,omitempty" json:"description,omitempty"`
	IsGoal        bool                 `yaml:"isGoal,omitempty" json:"isGoal,omitempty"`
	Values        map[string]StepValue `yaml:"values,omitempty" json:"values,omitempty"`
	Retry         *RetryConfig         `yaml:"retry,omitempty" json:"retry,omitempty"`
	Fallback      *FallbackConfig      `yaml:"fallback,omitempty" json:"fallback,omitempty"`
	Assertions    *Assertions          `yaml:"assertions,omitempty" json:"assertions,omitempty"`
	ExpectFailure *ExpectFailure       `yaml:"expectFailure,omitempty" json:"expectFailure,omitempty"`
}

// StepValue represents a value assignment for a step input.
// When a bare scalar appears in YAML (e.g., origin: "DEN"), only Default is set.
// When a mapping appears, the full struct is unmarshalled.
type StepValue struct {
	Default          any              `yaml:"default,omitempty" json:"default,omitempty"`
	FallbackPool     []any            `yaml:"fallbackPool,omitempty" json:"fallbackPool,omitempty"`
	FallbackStrategy *string          `yaml:"fallbackStrategy,omitempty" json:"fallbackStrategy,omitempty"`
	Constraint       string           `yaml:"constraint,omitempty" json:"constraint,omitempty"`
	From             string           `yaml:"from,omitempty" json:"from,omitempty"`
	Select           *SelectionConfig `yaml:"select,omitempty" json:"select,omitempty"`
}

// SelectionConfig describes how to select an element from an array output.
type SelectionConfig struct {
	Strategy  string `yaml:"strategy" json:"strategy"`
	Field     string `yaml:"field,omitempty" json:"field,omitempty"`
	Filter    string `yaml:"filter,omitempty" json:"filter,omitempty"`
	Index     int    `yaml:"index,omitempty" json:"index,omitempty"`
	SortField string `yaml:"sortField,omitempty" json:"sortField,omitempty"` // For min/max: field to compare by
	Prompt    string `yaml:"prompt,omitempty" json:"prompt,omitempty"`       // For llm strategy: selection criteria
}

// RetryConfig controls retry behavior for a step.
type RetryConfig struct {
	Max    int      `yaml:"max" json:"max"`
	On     []string `yaml:"on,omitempty" json:"on,omitempty"`
	FailOn []string `yaml:"failOn,omitempty" json:"failOn,omitempty"`
}

// FallbackConfig describes what to do when a step fails after retries.
type FallbackConfig struct {
	Action      string `yaml:"action" json:"action"`
	MaxAttempts int    `yaml:"maxAttempts,omitempty" json:"maxAttempts,omitempty"`
}

// Assertions holds mechanical and semantic assertions for a step.
type Assertions struct {
	Mechanical []MechanicalAssertion `yaml:"mechanical,omitempty" json:"mechanical,omitempty"`
	Semantic   []string              `yaml:"semantic,omitempty" json:"semantic,omitempty"`
}

// MechanicalAssertion describes a structured check on a response.
type MechanicalAssertion struct {
	Type   string `yaml:"type" json:"type"`
	Expect any    `yaml:"expect,omitempty" json:"expect,omitempty"`
	Ref    string `yaml:"ref,omitempty" json:"ref,omitempty"`
	Path   string `yaml:"path,omitempty" json:"path,omitempty"`
	Value  any    `yaml:"value,omitempty" json:"value,omitempty"`
	Expr   string `yaml:"expr,omitempty" json:"expr,omitempty"`
}

// ExpectFailure indicates that a step is expected to fail with specific status codes.
type ExpectFailure struct {
	Status      []int  `yaml:"status" json:"status"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// VerificationStep is an additional step run after the main flow for validation.
type VerificationStep struct {
	Node       string      `yaml:"node" json:"node"`
	Purpose    string      `yaml:"purpose,omitempty" json:"purpose,omitempty"`
	Assertions *Assertions `yaml:"assertions,omitempty" json:"assertions,omitempty"`
}

// CleanupStep describes a step to run during cleanup.
type CleanupStep struct {
	Node  string `yaml:"node" json:"node"`
	RunOn string `yaml:"runOn,omitempty" json:"runOn,omitempty"` // always, failure, success
}
