package plan

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Marshal serializes a Plan to YAML bytes.
func Marshal(p *Plan) ([]byte, error) {
	data, err := yaml.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshalling plan to YAML: %w", err)
	}
	return data, nil
}

// WriteFile serializes a Plan to YAML and writes it to the given path,
// creating parent directories as needed.
func WriteFile(p *Plan, path string) error {
	data, err := Marshal(p)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing plan file %s: %w", path, err)
	}
	return nil
}
