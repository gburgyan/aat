package graph

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gburgyan/aat/internal/yamlx"
)

// Layer is a named set of input default overrides that can be applied on top
// of graph-level defaults. Layers are stacked in order (later overrides earlier)
// and sit between graph defaults and plan values in the priority chain.
type Layer struct {
	Name          string                   `yaml:"name"`
	Description   string                   `yaml:"description,omitempty"`
	SelectionHint string                   `yaml:"selectionHint,omitempty"` // guidance for LLM layer selection
	Inputs        map[string]*InputDefault `yaml:"inputs"`
}

// UnknownInputs lists the layer's input keys that match nothing in the graph: a
// qualified key (node.input) naming a missing node or input, or a bare key that
// no node declares. ApplyLayers ignores such keys, so they are almost always
// typos. The result is sorted.
func (l *Layer) UnknownInputs(g *Graph) []string {
	hasInput := func(n *Node, name string) bool {
		for _, in := range n.Inputs {
			if in.Name == name {
				return true
			}
		}
		return false
	}

	var unknown []string
	for key := range l.Inputs {
		if nodeName, inputName, qualified := strings.Cut(key, "."); qualified {
			if n, ok := g.Nodes[nodeName]; !ok || n == nil || !hasInput(n, inputName) {
				unknown = append(unknown, key)
			}
			continue
		}
		found := false
		for _, n := range g.Nodes {
			if n != nil && hasInput(n, key) {
				found = true
				break
			}
		}
		if !found {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// ShapeErrors lists the layer's values that don't fit the type of an input they
// set (see DefaultShapeError), sorted by key. A bare key is checked against
// every node input with that name.
func (l *Layer) ShapeErrors(g *Graph) []string {
	nodeNames := sortedKeys(g.Nodes)
	var errs []string
	for _, key := range sortedKeys(l.Inputs) {
		nodeName, inputName, qualified := strings.Cut(key, ".")
		if !qualified {
			inputName = key
		}
		for _, name := range nodeNames {
			n := g.Nodes[name]
			if n == nil || (qualified && name != nodeName) {
				continue
			}
			for _, in := range n.Inputs {
				if in.Name != inputName {
					continue
				}
				if msg := DefaultShapeError(l.Inputs[key], in.Type); msg != "" {
					errs = append(errs, fmt.Sprintf("input %q for %s.%s: %s", key, name, inputName, msg))
				}
			}
		}
	}
	return errs
}

// ParseLayer unmarshals YAML bytes into a Layer with basic validation. Keys
// that no layer field accepts are errors.
func ParseLayer(data []byte) (*Layer, error) {
	var l Layer
	if err := yamlx.Decode(data, &l); err != nil {
		return nil, err
	}
	if l.Name == "" {
		return nil, fmt.Errorf("layer must have a name")
	}
	return &l, nil
}

// ParseLayerFile reads a YAML file and parses it as a Layer. Errors name the
// file.
func ParseLayerFile(path string) (*Layer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading layer file: %w", err)
	}
	l, err := ParseLayer(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return l, nil
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
			return nil, err
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
// the requested names. Returns an error if any requested name is not found,
// and an error wrapping ErrNoLayersDir when names are given without a
// directory: silently dropping them would run a different test than the one
// requested.
func ResolveLayerNames(names []string, dir string) (map[string]*Layer, error) {
	if len(names) > 0 && dir == "" {
		return nil, fmt.Errorf("layers %v requested but %w", names, ErrNoLayersDir)
	}
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

// ErrNoLayersDir reports layers that were requested without a layers directory
// to load them from.
var ErrNoLayersDir = errors.New("no layers directory is configured (set `layers:` in aat-project.yaml)")

// LayeredDefaults loads the named layers from dir and stacks them, in order, on
// the graph defaults (see ApplyLayers). It returns nil when no layers are named.
// Naming layers without a directory is an error wrapping ErrNoLayersDir (see
// ResolveLayerNames).
func LayeredDefaults(g *Graph, names []string, dir string) (map[string]*InputDefault, error) {
	if len(names) == 0 {
		return nil, nil
	}
	available, err := ResolveLayerNames(names, dir)
	if err != nil {
		if errors.Is(err, ErrNoLayersDir) {
			return nil, err
		}
		return nil, fmt.Errorf("loading layers: %w", err)
	}
	defaults, err := ApplyLayers(g, names, available)
	if err != nil {
		return nil, fmt.Errorf("applying layers: %w", err)
	}
	return defaults, nil
}

// MergeInputDefault merges an overlay InputDefault on top of a base, producing
// a new InputDefault. Fields set in the overlay replace the corresponding base
// fields. Nil/zero overlay fields leave the base value unchanged.
//
// An overlay that names a value source (value, pool, from, or fromResolved)
// replaces the base's sources entirely, so a layer's literal value is not
// shadowed by a graph default's from (which resolution prefers). The base's
// select is kept only when the overlay sets neither a source nor its own select
// — or sets a from that the select can apply to.
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

	if overlay.Value != nil || overlay.Pool != nil || overlay.From != "" || overlay.FromResolved != "" {
		result.Value, result.Pool, result.From, result.FromResolved = nil, nil, "", ""
		if overlay.From == "" {
			result.Select = nil // a select applies only to a from source
		}
	}

	if overlay.Pool != nil {
		result.Pool = make([]any, len(overlay.Pool))
		copy(result.Pool, overlay.Pool)
	}

	if overlay.Value != nil {
		result.Value = overlay.Value
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

	if overlay.FromResolved != "" {
		result.FromResolved = overlay.FromResolved
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
