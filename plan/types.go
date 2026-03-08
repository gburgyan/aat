package plan

import (
	"strings"
	"time"

	"github.com/gburgyan/aat/config"
)

// Plan is the top-level execution plan parsed from YAML.
// It describes what steps to execute, their input values,
// and optional metadata about the intent and graph context.
type Plan struct {
	Metadata  Metadata           `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Graph     string             `yaml:"graph,omitempty" json:"graph,omitempty"`
	Auth      *config.AuthConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
	Headers   map[string]string  `yaml:"headers,omitempty" json:"headers,omitempty"`
	Intent    Intent             `yaml:"intent,omitempty" json:"intent,omitempty"`
	Execution Execution          `yaml:"execution" json:"execution"`
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
// When multiple steps target the same graph node (e.g., multi-leg flights),
// use the ID field to give each step a unique identifier. DependsOn and
// from references use step IDs, which default to the node name when ID is empty.
//
// When Slot is set, the step is a placeholder marker for a slot choice point.
// Node is empty for slot markers. At composition time, the marker is replaced
// by the chosen option's steps.
type Step struct {
	ID            string                   `yaml:"id,omitempty" json:"id,omitempty"`
	Node          string                   `yaml:"node,omitempty" json:"node,omitempty"`
	Slot          string                   `yaml:"slot,omitempty" json:"slot,omitempty"` // slot marker name
	DependsOn     []string                 `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	Description   string                   `yaml:"description,omitempty" json:"description,omitempty"`
	IsGoal        bool                     `yaml:"isGoal,omitempty" json:"isGoal,omitempty"`
	Selections    map[string]StepSelection `yaml:"selections,omitempty" json:"selections,omitempty"`
	Values        map[string]StepValue     `yaml:"values,omitempty" json:"values,omitempty"`
	Retry         *RetryConfig             `yaml:"retry,omitempty" json:"retry,omitempty"`
	Fallback      *FallbackConfig          `yaml:"fallback,omitempty" json:"fallback,omitempty"`
	Assertions    *Assertions              `yaml:"assertions,omitempty" json:"assertions,omitempty"`
	ExpectFailure *ExpectFailure           `yaml:"expectFailure,omitempty" json:"expectFailure,omitempty"`
}

// StepID returns the effective step identifier.
// If Slot is set, returns Slot. If ID is set, returns ID; otherwise returns Node.
func (s *Step) StepID() string {
	if s.Slot != "" {
		return s.Slot
	}
	if s.ID != "" {
		return s.ID
	}
	return s.Node
}

// IsSlotMarker returns true if this step is a slot placeholder marker.
func (s *Step) IsSlotMarker() bool {
	return s.Slot != ""
}

// StepSelection describes a named element selection from an upstream array.
// Multiple values can reference the same selection to extract different fields
// from the same element, ensuring coordinated multi-field extraction.
type StepSelection struct {
	From      string `yaml:"from" json:"from"`
	Strategy  string `yaml:"strategy,omitempty" json:"strategy,omitempty"`
	Filter    string `yaml:"filter,omitempty" json:"filter,omitempty"`
	Index     int    `yaml:"index,omitempty" json:"index,omitempty"`
	SortField string `yaml:"sortField,omitempty" json:"sortField,omitempty"`
	Prompt    string `yaml:"prompt,omitempty" json:"prompt,omitempty"`
}

// StepValue represents a value assignment for a step input.
// When a bare scalar appears in YAML (e.g., origin: "DEN"), only Default is set.
// When a mapping appears, the full struct is unmarshalled.
type StepValue struct {
	Default       any              `yaml:"default,omitempty" json:"default,omitempty"`
	Pool          []any            `yaml:"pool,omitempty" json:"pool,omitempty"`
	PoolStrategy  *string          `yaml:"poolStrategy,omitempty" json:"poolStrategy,omitempty"`
	Constraint    string           `yaml:"constraint,omitempty" json:"constraint,omitempty"`
	From          string           `yaml:"from,omitempty" json:"from,omitempty"`
	Select        *SelectionConfig `yaml:"select,omitempty" json:"select,omitempty"`
	FromSelection string           `yaml:"fromSelection,omitempty" json:"fromSelection,omitempty"`
	FromResolved  string           `yaml:"fromResolved,omitempty" json:"fromResolved,omitempty"`
	FromInput     string           `yaml:"fromInput,omitempty" json:"fromInput,omitempty"`
	Locked        bool             `yaml:"locked,omitempty" json:"locked,omitempty"`
}

// IsEmpty returns true when the StepValue carries no resolution information.
// An empty StepValue (e.g., from YAML `returnDate: {}` or `returnDate: null`)
// should be treated as "explicitly absent" — the engine should skip this input
// rather than falling through to graph-edge auto-wiring.
func (sv StepValue) IsEmpty() bool {
	return sv.Default == nil &&
		sv.From == "" &&
		sv.FromSelection == "" &&
		sv.FromResolved == "" &&
		sv.FromInput == "" &&
		sv.Select == nil &&
		sv.Constraint == "" &&
		len(sv.Pool) == 0 &&
		sv.PoolStrategy == nil
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
	Raw    bool   `yaml:"raw,omitempty" json:"raw,omitempty"`
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

// ParseFromSelection splits a fromSelection reference like "offering.offeringId"
// into (selectionName, fieldName). If there is no dot, fieldName is empty
// (meaning the whole element). Returns empty strings for empty input.
func ParseFromSelection(ref string) (selectionName, fieldName string) {
	if ref == "" {
		return "", ""
	}
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}
