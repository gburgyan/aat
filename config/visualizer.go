package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gburgyan/aat/internal/yamlx"
)

// VisualizerDef describes a single visualizer plugin.
type VisualizerDef struct {
	ID    string          `yaml:"id" json:"id"`
	Name  string          `yaml:"name" json:"name"`
	File  string          `yaml:"file" json:"file"`
	Match VisualizerMatch `yaml:"match" json:"match"`
}

// VisualizerMatch defines the rules for when a visualizer applies to a step.
// Every rule given must hold.
type VisualizerMatch struct {
	// BodyContains is a top-level key the response body must have.
	BodyContains string `yaml:"bodyContains,omitempty" json:"bodyContains,omitempty"`
	// BodyPath is a gjson path, such as data.booking_reference, that must reach
	// a value other than null in the response body.
	BodyPath string `yaml:"bodyPath,omitempty" json:"bodyPath,omitempty"`
	// Node is the graph node the step must run.
	Node string `yaml:"node,omitempty" json:"node,omitempty"`
}

// VisualizerManifest is the top-level structure of visualizers.yaml.
type VisualizerManifest struct {
	Visualizers []VisualizerDef `yaml:"visualizers"`
}

// LoadVisualizers reads the visualizer manifest from dir/visualizers.yaml
// and validates that referenced HTML files exist.
func LoadVisualizers(dir string) ([]VisualizerDef, error) {
	if dir == "" {
		return nil, nil
	}

	manifestPath := filepath.Join(dir, "visualizers.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading visualizer manifest: %w", err)
	}

	var manifest VisualizerManifest
	if err := yamlx.Decode(data, &manifest); err != nil {
		return nil, fmt.Errorf("%s: %w", manifestPath, err)
	}

	for i, v := range manifest.Visualizers {
		if v.ID == "" {
			return nil, fmt.Errorf("visualizer at index %d missing required field: id", i)
		}
		if v.File == "" {
			return nil, fmt.Errorf("visualizer %q missing required field: file", v.ID)
		}
		if v.Name == "" {
			manifest.Visualizers[i].Name = v.ID
		}

		// Validate the HTML file exists.
		filePath := filepath.Join(dir, v.File)
		if _, err := os.Stat(filePath); err != nil {
			return nil, fmt.Errorf("visualizer %q: file %q not found in %s", v.ID, v.File, dir)
		}
	}

	return manifest.Visualizers, nil
}
