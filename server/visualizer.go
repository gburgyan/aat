package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// VisualizerDef describes a single visualizer plugin.
type VisualizerDef struct {
	ID    string          `yaml:"id" json:"id"`
	Name  string          `yaml:"name" json:"name"`
	File  string          `yaml:"file" json:"file"`
	Match VisualizerMatch `yaml:"match" json:"match"`
}

// VisualizerMatch defines the rules for when a visualizer applies to a step.
type VisualizerMatch struct {
	BodyContains string `yaml:"bodyContains,omitempty" json:"bodyContains,omitempty"`
	Node         string `yaml:"node,omitempty" json:"node,omitempty"`
}

// VisualizerManifest is the top-level structure of visualizers.yaml.
type VisualizerManifest struct {
	Visualizers []VisualizerDef `yaml:"visualizers"`
}

// VisualizerHit is returned by matchVisualizers to identify which visualizers
// apply to a given step response.
type VisualizerHit struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing visualizer manifest: %w", err)
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

// matchVisualizers returns the visualizers that match a given step's node name
// and response body.
func matchVisualizers(defs []VisualizerDef, stepNode string, responseBody json.RawMessage) []VisualizerHit {
	if len(defs) == 0 || len(responseBody) == 0 {
		return nil
	}

	var hits []VisualizerHit
	for _, def := range defs {
		if matchesVisualizer(def, stepNode, responseBody) {
			hits = append(hits, VisualizerHit{ID: def.ID, Name: def.Name})
		}
	}
	return hits
}

func matchesVisualizer(def VisualizerDef, stepNode string, responseBody json.RawMessage) bool {
	// Node filter: if specified, must match.
	if def.Match.Node != "" && def.Match.Node != stepNode {
		return false
	}

	// bodyContains: check if the response body contains the specified top-level key.
	if def.Match.BodyContains != "" {
		if !bodyContainsKey(responseBody, def.Match.BodyContains) {
			return false
		}
	}

	// At least one match criterion must be specified.
	if def.Match.Node == "" && def.Match.BodyContains == "" {
		return false
	}

	return true
}

// bodyContainsKey checks whether the JSON response body contains a top-level key.
func bodyContainsKey(body json.RawMessage, key string) bool {
	// Fast path: check if the body looks like it contains the key as a string.
	// This avoids full JSON parsing for large responses.
	needle := `"` + key + `"`
	if !strings.Contains(string(body), needle) {
		return false
	}

	// Parse just the top-level keys to confirm.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return false
	}
	_, ok := top[key]
	return ok
}
