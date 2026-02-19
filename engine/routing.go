package engine

import (
	"path/filepath"

	"github.com/gburgyan/aat/adapter"
)

// ExecutorRouter routes API calls to different executors based on node name.
// It supports exact-match and glob-pattern overrides, falling back to a default executor.
type ExecutorRouter struct {
	defaultExec   *adapter.HTTPExecutor
	defaultConfig *adapter.EnvironmentConfig
	overrides     []routeEntry
}

type routeEntry struct {
	pattern     string
	isGlob      bool
	executor    *adapter.HTTPExecutor
	config      *adapter.EnvironmentConfig
	pathRewrite *adapter.PathRewrite
}

// NewExecutorRouter creates a router with the given default executor and config.
func NewExecutorRouter(exec *adapter.HTTPExecutor, cfg *adapter.EnvironmentConfig) *ExecutorRouter {
	return &ExecutorRouter{
		defaultExec:   exec,
		defaultConfig: cfg,
	}
}

// AddOverride registers a named or glob-pattern override. Overrides are checked
// in the order they are added. Exact matches are checked before glob patterns.
func (r *ExecutorRouter) AddOverride(pattern string, exec *adapter.HTTPExecutor, cfg *adapter.EnvironmentConfig, rewrite *adapter.PathRewrite) {
	r.overrides = append(r.overrides, routeEntry{
		pattern:     pattern,
		isGlob:      isGlobPattern(pattern),
		executor:    exec,
		config:      cfg,
		pathRewrite: rewrite,
	})
}

// Resolve returns the executor, config, and optional path rewrite for the given node name.
// Resolution order: exact matches first (in order added), then glob matches (first wins), then default.
func (r *ExecutorRouter) Resolve(nodeName string) (*adapter.HTTPExecutor, *adapter.EnvironmentConfig, *adapter.PathRewrite) {
	// Pass 1: exact matches
	for _, entry := range r.overrides {
		if !entry.isGlob && entry.pattern == nodeName {
			return entry.executor, entry.config, entry.pathRewrite
		}
	}

	// Pass 2: glob matches
	for _, entry := range r.overrides {
		if entry.isGlob {
			if matched, _ := filepath.Match(entry.pattern, nodeName); matched {
				return entry.executor, entry.config, entry.pathRewrite
			}
		}
	}

	return r.defaultExec, r.defaultConfig, nil
}

// HasOverrides returns true if any overrides have been configured.
func (r *ExecutorRouter) HasOverrides() bool {
	return len(r.overrides) > 0
}

// OverridePatterns returns the list of override patterns for logging/diagnostics.
func (r *ExecutorRouter) OverridePatterns() []string {
	patterns := make([]string, len(r.overrides))
	for i, entry := range r.overrides {
		patterns[i] = entry.pattern
	}
	return patterns
}

// isGlobPattern returns true if the pattern contains glob meta-characters.
func isGlobPattern(pattern string) bool {
	for _, c := range pattern {
		if c == '*' || c == '?' || c == '[' {
			return true
		}
	}
	return false
}
