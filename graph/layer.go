package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Layer is a named set of input default overrides that can be applied on top
// of graph-level defaults. Layers are stacked in order (later overrides earlier)
// and sit between graph defaults and plan values in the priority chain.
type Layer struct {
	Name        string                   `yaml:"name"`
	Description string                   `yaml:"description,omitempty"`
	Inputs      map[string]*InputDefault `yaml:"inputs"`
}

// ParseLayer unmarshals YAML bytes into a Layer with basic validation.
func ParseLayer(data []byte) (*Layer, error) {
	var l Layer
	if err := yaml.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("layer YAML parse error: %w", err)
	}
	if l.Name == "" {
		return nil, fmt.Errorf("layer must have a name")
	}
	return &l, nil
}

// ParseLayerFile reads a YAML file and parses it as a Layer.
func ParseLayerFile(path string) (*Layer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading layer file: %w", err)
	}
	return ParseLayer(data)
}

// LoadLayersFromDir scans a directory for *.yaml and *.yml files, parses each
// as a Layer, and returns them indexed by name. Returns an error if two files
// define layers with the same name.
func LoadLayersFromDir(dir string) (map[string]*Layer, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading layers directory %s: %w", dir, err)
	}

	layers := make(map[string]*Layer)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		layer, err := ParseLayerFile(path)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}

		if existing, ok := layers[layer.Name]; ok {
			_ = existing
			return nil, fmt.Errorf("duplicate layer name %q in %s", layer.Name, dir)
		}
		layers[layer.Name] = layer
	}

	return layers, nil
}

// ResolveLayerNames loads only the named layer files from a directory.
// It scans the directory for YAML files and returns the subset that match
// the requested names. Returns an error if any requested name is not found.
func ResolveLayerNames(names []string, dir string) (map[string]*Layer, error) {
	all, err := LoadLayersFromDir(dir)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*Layer, len(names))
	for _, name := range names {
		layer, ok := all[name]
		if !ok {
			available := make([]string, 0, len(all))
			for k := range all {
				available = append(available, k)
			}
			return nil, fmt.Errorf("layer %q not found in %s; available: %s", name, dir, strings.Join(available, ", "))
		}
		result[name] = layer
	}

	return result, nil
}

// MergeInputDefault merges an overlay InputDefault on top of a base, producing
// a new InputDefault. Fields set in the overlay replace the corresponding base
// fields. Nil/zero overlay fields leave the base value unchanged.
//
// Switching strategies (e.g., overlay sets Pool → clears Value, and vice versa)
// is handled automatically.
func MergeInputDefault(base, overlay *InputDefault) *InputDefault {
	if overlay == nil {
		if base == nil {
			return nil
		}
		cp := *base
		return &cp
	}
	if base == nil {
		cp := *overlay
		return &cp
	}

	result := *base

	if overlay.Pool != nil {
		result.Pool = make([]any, len(overlay.Pool))
		copy(result.Pool, overlay.Pool)
		result.Value = nil // switching to pool strategy
	}

	if overlay.Value != nil {
		result.Value = overlay.Value
		result.Pool = nil // switching to literal strategy
	}

	if overlay.PoolStrategy != nil {
		s := *overlay.PoolStrategy
		result.PoolStrategy = &s
	}

	if overlay.Constraint != "" {
		result.Constraint = overlay.Constraint
	}

	if overlay.From != "" {
		result.From = overlay.From
	}

	if overlay.Select != nil {
		sel := *overlay.Select
		result.Select = &sel
	}

	return &result
}

// LayerTouchedKeys returns the set of "nodeName.inputName" keys that are
// directly specified by at least one of the named layers (bare or qualified).
// Unlike ApplyLayers, this does NOT seed from graph defaults — only inputs
// that layers explicitly override are included.
// Used by the intent pipeline to classify inputs as "layer-handled" for prompting.
func LayerTouchedKeys(g *Graph, layerNames []string, available map[string]*Layer) map[string]bool {
	touched := make(map[string]bool)
	for _, layerName := range layerNames {
		layer, ok := available[layerName]
		if !ok || layer == nil || len(layer.Inputs) == 0 {
			continue
		}
		for key := range layer.Inputs {
			if strings.Contains(key, ".") {
				// Qualified "node.input" entry — verify the node/input exist before adding.
				parts := strings.SplitN(key, ".", 2)
				if len(parts) != 2 {
					continue
				}
				nodeName, inputName := parts[0], parts[1]
				node, ok := g.Nodes[nodeName]
				if !ok {
					continue
				}
				for _, inp := range node.Inputs {
					if inp.Name == inputName {
						touched[key] = true
						break
					}
				}
			} else {
				// Bare "input" entry — match all nodes with this input name.
				for nodeName, node := range g.Nodes {
					for _, inp := range node.Inputs {
						if inp.Name == key {
							touched[nodeName+"."+inp.Name] = true
						}
					}
				}
			}
		}
	}
	return touched
}

// ApplyLayers computes the effective InputDefaults for every (node, input) pair
// after stacking the named layers on top of the graph defaults. Layers are
// applied in order — later layers override earlier ones.
//
// Within a single layer, qualified entries (node.input) take priority over bare
// entries (input) for the same node.
//
// The returned map is keyed by "nodeName.inputName" and contains only entries
// that differ from (or are absent from) the graph defaults.
func ApplyLayers(g *Graph, layerNames []string, available map[string]*Layer) (map[string]*InputDefault, error) {
	// Validate that all named layers exist.
	for _, name := range layerNames {
		if _, ok := available[name]; !ok {
			avail := make([]string, 0, len(available))
			for k := range available {
				avail = append(avail, k)
			}
			return nil, fmt.Errorf("layer %q not found; available: %s", name, strings.Join(avail, ", "))
		}
	}

	// Build the effective defaults map, starting from graph defaults.
	// Key: "nodeName.inputName"
	effective := make(map[string]*InputDefault)

	// Seed with graph defaults.
	for nodeName, node := range g.Nodes {
		for _, input := range node.Inputs {
			key := nodeName + "." + input.Name
			if input.Default != nil {
				cp := *input.Default
				effective[key] = &cp
			}
		}
	}

	// Apply each layer in order.
	for _, layerName := range layerNames {
		layer := available[layerName]
		if layer == nil || len(layer.Inputs) == 0 {
			continue
		}

		// Separate bare and qualified entries.
		bare := make(map[string]*InputDefault)      // input name → default
		qualified := make(map[string]*InputDefault) // node.input → default

		for key, def := range layer.Inputs {
			if strings.Contains(key, ".") {
				qualified[key] = def
			} else {
				bare[key] = def
			}
		}

		// Apply bare entries: match any node that has an input with this name.
		for nodeName, node := range g.Nodes {
			for _, input := range node.Inputs {
				if bareDef, ok := bare[input.Name]; ok {
					key := nodeName + "." + input.Name
					effective[key] = MergeInputDefault(effective[key], bareDef)
				}
			}
		}

		// Apply qualified entries: override specific node.input pairs.
		for qualKey, def := range qualified {
			parts := strings.SplitN(qualKey, ".", 2)
			if len(parts) != 2 {
				continue
			}
			nodeName, inputName := parts[0], parts[1]

			// Verify the node and input exist.
			node, ok := g.Nodes[nodeName]
			if !ok {
				continue
			}
			inputExists := false
			for _, input := range node.Inputs {
				if input.Name == inputName {
					inputExists = true
					break
				}
			}
			if !inputExists {
				continue
			}

			key := nodeName + "." + inputName
			effective[key] = MergeInputDefault(effective[key], def)
		}
	}

	return effective, nil
}
