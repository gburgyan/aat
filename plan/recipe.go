package plan

import (
	"fmt"
	"os"
	"path/filepath"

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
	Description string           `yaml:"description,omitempty"`
	Choices     map[string]string `yaml:"choices,omitempty"`
	Addons      []string          `yaml:"addons,omitempty"`
	Repetitions map[string]int    `yaml:"repetitions,omitempty"`
}

// RecipeOverrides captures the LLM's creative decisions: literal values,
// selection strategy overrides, assertions, and step descriptions.
type RecipeOverrides struct {
	Values       map[string]any                         `yaml:"values,omitempty"`
	Selections   map[string]RecipeSelectionOverride      `yaml:"selections,omitempty"`
	Assertions   map[string][]RecipeAssertion            `yaml:"assertions,omitempty"`
	Descriptions map[string]string                       `yaml:"descriptions,omitempty"`
}

// RecipeSelectionOverride mirrors intent.TargetedSelection for YAML serialization
// without creating a dependency from plan/ to intent/.
type RecipeSelectionOverride struct {
	Strategy  string `yaml:"strategy,omitempty"`
	Filter    string `yaml:"filter,omitempty"`
	SortField string `yaml:"sortField,omitempty"`
	Index     int    `yaml:"index,omitempty"`
	Prompt    string `yaml:"prompt,omitempty"`
}

// RecipeAssertion mirrors intent.TargetedAssertion for YAML serialization.
type RecipeAssertion struct {
	Type   string `yaml:"type"`
	Expect any    `yaml:"expect,omitempty"`
	Path   string `yaml:"path,omitempty"`
	Value  any    `yaml:"value,omitempty"`
	Expr   string `yaml:"expr,omitempty"`
}

// ParseRecipe unmarshals YAML bytes into a Recipe with basic validation.
func ParseRecipe(data []byte) (*Recipe, error) {
	var r Recipe
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("recipe YAML parse error: %w", err)
	}
	if r.Kind != "recipe" {
		return nil, fmt.Errorf("expected kind: recipe, got %q", r.Kind)
	}
	if r.Selection.Workflow == "" {
		return nil, fmt.Errorf("recipe must specify selection.workflow")
	}
	return &r, nil
}

// ParseRecipeFile reads a YAML file and parses it as a Recipe.
func ParseRecipeFile(path string) (*Recipe, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading recipe file: %w", err)
	}
	return ParseRecipe(data)
}

// IsRecipeFile probes YAML bytes to check if they represent a recipe.
func IsRecipeFile(data []byte) bool {
	var probe struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.Kind == "recipe"
}

// ParseAny probes the kind field and dispatches to ParseRecipe or Parse.
// Returns *Recipe or *Plan.
func ParseAny(data []byte) (any, error) {
	if IsRecipeFile(data) {
		return ParseRecipe(data)
	}
	return Parse(data)
}

// ParseAnyFile reads a YAML file and dispatches to ParseRecipe or ParseFile.
// Returns *Recipe or *Plan.
func ParseAnyFile(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}
	return ParseAny(data)
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
