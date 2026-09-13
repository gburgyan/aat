package plan

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gburgyan/aat/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// Recipe is a compact plan representation that stores only the LLM decisions
// (workflow selection + targeted overrides). At runtime it is reconstituted
// into a full Plan by replaying the composition pipeline deterministically.
type Recipe struct {
	Kind      string          `yaml:"kind"`
	Metadata  Metadata        `yaml:"metadata,omitempty"`
	Selection RecipeSelection `yaml:"selection"`
	Overrides RecipeOverrides `yaml:"overrides,omitempty"`
}

// RecipeSelection captures which workflow template to use and how to compose it.
type RecipeSelection struct {
	Workflow    string            `yaml:"workflow"`
	Description string            `yaml:"description,omitempty"`
	Layers      []string          `yaml:"layers,omitempty"`
	Choices     map[string]string `yaml:"choices,omitempty"`
	Addons      []string          `yaml:"addons,omitempty"`
}

// RecipeOverrides captures the LLM's creative decisions: literal values,
// selection strategy overrides, and assertions.
type RecipeOverrides struct {
	Values     map[string]any                     `yaml:"values,omitempty"`
	Selections map[string]RecipeSelectionOverride `yaml:"selections,omitempty"`
	Assertions map[string][]RecipeAssertion       `yaml:"assertions,omitempty"`
}

// RecipeSelectionOverride mirrors intent.TargetedSelection for YAML serialization
// without creating a dependency from plan/ to intent/.
type RecipeSelectionOverride struct {
	Strategy  string `yaml:"strategy,omitempty"`
	Filter    string `yaml:"filter,omitempty"`
	SortField string `yaml:"sortField,omitempty"`
	Index     int    `yaml:"index,omitempty"`
	OnTie     string `yaml:"onTie,omitempty"`
}

// RecipeAssertion mirrors intent.TargetedAssertion for YAML serialization.
type RecipeAssertion struct {
	Type   string `yaml:"type"`
	Expect any    `yaml:"expect,omitempty"`
	Path   string `yaml:"path,omitempty"`
	Value  any    `yaml:"value,omitempty"`
	Expr   string `yaml:"expr,omitempty"`
	Raw    bool   `yaml:"raw,omitempty"`
}

// ParseRecipe unmarshals YAML bytes into a Recipe with basic validation. Keys
// that no recipe field accepts are errors.
func ParseRecipe(data []byte) (*Recipe, error) {
	// Check the kind first, so a plan passed here reports the wrong kind rather
	// than every plan key as unknown.
	if kind, err := probeKind(data); err == nil && kind != "recipe" {
		return nil, fmt.Errorf("expected kind: recipe, got %q", kind)
	}
	var r Recipe
	if err := yamlx.Decode(data, &r); err != nil {
		return nil, err
	}
	if r.Kind != "recipe" {
		return nil, fmt.Errorf("expected kind: recipe, got %q", r.Kind)
	}
	if r.Selection.Workflow == "" {
		return nil, fmt.Errorf("recipe must specify selection.workflow")
	}
	return &r, nil
}

// ParseRecipeFile reads a YAML file and parses it as a Recipe. Errors name the
// file.
func ParseRecipeFile(path string) (*Recipe, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading recipe file: %w", err)
	}
	r, err := ParseRecipe(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

// IsRecipeFile probes YAML bytes to check if they represent a recipe.
func IsRecipeFile(data []byte) bool {
	kind, err := probeKind(data)
	return err == nil && kind == "recipe"
}

// probeKind reads only the top-level kind field.
func probeKind(data []byte) (string, error) {
	var probe struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil { //nolint:forbidigo // lenient probe: the full decode that follows is strict
		return "", err
	}
	return probe.Kind, nil
}

// ParseAny probes the kind field and dispatches to ParseRecipe or Parse.
// Returns *Recipe or *Plan.
func ParseAny(data []byte) (any, error) {
	if IsRecipeFile(data) {
		return ParseRecipe(data)
	}
	return Parse(data)
}

// ParseAnyFile reads a YAML file and dispatches to ParseRecipe or Parse.
// Returns *Recipe or *Plan. Errors name the file.
func ParseAnyFile(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}
	parsed, err := ParseAny(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return parsed, nil
}

// MarshalRecipe serializes a Recipe to YAML bytes.
func MarshalRecipe(r *Recipe) ([]byte, error) {
	data, err := yaml.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("marshalling recipe to YAML: %w", err)
	}
	return data, nil
}

// WriteRecipe serializes a Recipe to YAML and writes it to the given path,
// creating parent directories as needed.
func WriteRecipe(r *Recipe, path string) error {
	data, err := MarshalRecipe(r)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing recipe file %s: %w", path, err)
	}
	return nil
}
