package main

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/graph/oas"
)

// loadOASCache loads the OAS specs a graph references, for runtime validation
// in mode. It returns nil when the mode is off, the graph references no spec, or
// no spec loads. Under strict, a spec that fails to load is an error: the run
// could not validate the steps that use it. Under auto, the failure is a warning
// written to warn; callers pass stderr so it shows under --quiet and --json.
func loadOASCache(g *graph.Graph, graphPath, mode string, warn io.Writer) (*oas.SpecCache, error) {
	if mode == "off" {
		return nil, nil
	}
	specPaths := collectOASSpecPaths(g)
	if len(specPaths) == 0 {
		return nil, nil
	}

	graphDir := filepath.Dir(graphPath)
	cache := oas.NewSpecCache()
	for _, sp := range specPaths {
		fsPath := sp
		if !filepath.IsAbs(sp) {
			fsPath = filepath.Join(graphDir, sp)
		}
		if err := cache.Load(sp, fsPath); err != nil {
			if mode == "strict" {
				return nil, fmt.Errorf("strict OAS validation: %w", err)
			}
			_, _ = fmt.Fprintf(warn, "aat: warning: %s (steps that use this spec are not validated)\n", err)
		}
	}
	if cache.Len() == 0 {
		return nil, nil
	}
	return cache, nil
}
