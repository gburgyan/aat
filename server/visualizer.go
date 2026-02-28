package server

import (
	"encoding/json"
	"strings"

	"github.com/gburgyan/aat/config"
)

// VisualizerHit is returned by matchVisualizers to identify which visualizers
// apply to a given step response.
type VisualizerHit struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// matchVisualizers returns the visualizers that match a given step's node name
// and response body.
func matchVisualizers(defs []config.VisualizerDef, stepNode string, responseBody json.RawMessage) []VisualizerHit {
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

func matchesVisualizer(def config.VisualizerDef, stepNode string, responseBody json.RawMessage) bool {
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
