package plan

import "time"

// Plan is the top-level execution plan parsed from YAML.
// It describes what steps to execute, their input values,
// and optional metadata about the intent and graph context.
type Plan struct {
	Metadata  Metadata  `yaml:"metadata,omitempty"`
	Graph     string    `yaml:"graph,omitempty"`
	Intent    Intent    `yaml:"intent,omitempty"`
	Execution Execution `yaml:"execution"`
}

// Metadata captures provenance information about when and why a plan was created.
type Metadata struct {
	Created      time.Time `yaml:"created,omitempty"`
	Prompt       string    `yaml:"prompt,omitempty"`
	GraphVersion string    `yaml:"graphVersion,omitempty"`
}

// Intent describes the high-level goal and constraints of the plan.
type Intent struct {
	Goal        string       `yaml:"goal,omitempty"`
	Description string       `yaml:"description,omitempty"`
	Constraints *Constraints `yaml:"constraints,omitempty"`
}

// Constraints classifies plan constraints by enforcement level.
type Constraints struct {
	Hard []Constraint `yaml:"hard,omitempty"`
	Soft []Constraint `yaml:"soft,omitempty"`
	Free []string     `yaml:"free,omitempty"`
}

// Constraint describes a single requirement or preference.
type Constraint struct {
	Type        string   `yaml:"type"`
	Name        string   `yaml:"name,omitempty"`
	Description string   `yaml:"description,omitempty"`
	AppliesTo   []string `yaml:"applies_to,omitempty"`
}

// Execution holds the ordered steps, verification, and cleanup for a plan.
type Execution struct {
	Steps        []Step             `yaml:"steps"`
	Verification []VerificationStep `yaml:"verification,omitempty"`
	Cleanup      []CleanupStep      `yaml:"cleanup,omitempty"`
}

// Step describes a single execution step targeting a graph node.
type Step struct {
	Node          string               `yaml:"node"`
	DependsOn     []string             `yaml:"dependsOn,omitempty"`
	Description   string               `yaml:"description,omitempty"`
	IsGoal        bool                 `yaml:"isGoal,omitempty"`
	Values        map[string]StepValue `yaml:"values,omitempty"`
	Retry         *RetryConfig         `yaml:"retry,omitempty"`
	Fallback      *FallbackConfig      `yaml:"fallback,omitempty"`
	Assertions    *Assertions          `yaml:"assertions,omitempty"`
	ExpectFailure *ExpectFailure       `yaml:"expectFailure,omitempty"`
}

// StepValue represents a value assignment for a step input.
// When a bare scalar appears in YAML (e.g., origin: "DEN"), only Default is set.
// When a mapping appears, the full struct is unmarshalled.
type StepValue struct {
	Default          any              `yaml:"default,omitempty"`
	FallbackPool     []any            `yaml:"fallbackPool,omitempty"`
	FallbackStrategy *string          `yaml:"fallbackStrategy,omitempty"`
	Constraint       string           `yaml:"constraint,omitempty"`
	From             string           `yaml:"from,omitempty"`
	Select           *SelectionConfig `yaml:"select,omitempty"`
}

// SelectionConfig describes how to select an element from an array output.
type SelectionConfig struct {
	Strategy string `yaml:"strategy"`
	Field    string `yaml:"field,omitempty"`
	Filter   string `yaml:"filter,omitempty"`
	Index    int    `yaml:"index,omitempty"`
}

// RetryConfig controls retry behavior for a step.
type RetryConfig struct {
	Max    int      `yaml:"max"`
	On     []string `yaml:"on,omitempty"`
	FailOn []string `yaml:"failOn,omitempty"`
}

// FallbackConfig describes what to do when a step fails after retries.
type FallbackConfig struct {
	Action      string `yaml:"action"`
	MaxAttempts int    `yaml:"maxAttempts,omitempty"`
}

// Assertions holds mechanical and semantic assertions for a step.
type Assertions struct {
	Mechanical []MechanicalAssertion `yaml:"mechanical,omitempty"`
	Semantic   []string              `yaml:"semantic,omitempty"`
}

// MechanicalAssertion describes a structured check on a response.
type MechanicalAssertion struct {
	Type   string `yaml:"type"`
	Expect any    `yaml:"expect,omitempty"`
	Ref    string `yaml:"ref,omitempty"`
	Path   string `yaml:"path,omitempty"`
	Value  any    `yaml:"value,omitempty"`
	Expr   string `yaml:"expr,omitempty"`
}

// ExpectFailure indicates that a step is expected to fail with specific status codes.
type ExpectFailure struct {
	Status      []int  `yaml:"status"`
	Description string `yaml:"description,omitempty"`
}

// VerificationStep is an additional step run after the main flow for validation.
type VerificationStep struct {
	Node       string      `yaml:"node"`
	Purpose    string      `yaml:"purpose,omitempty"`
	Assertions *Assertions `yaml:"assertions,omitempty"`
}

// CleanupStep describes a step to run during cleanup.
type CleanupStep struct {
	Node  string `yaml:"node"`
	RunOn string `yaml:"runOn,omitempty"` // always, failure, success
}
