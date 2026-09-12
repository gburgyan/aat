package engine

import (
	"path/filepath"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/plan"
)

// ExecutorRouter routes API calls to different executors based on node name.
// It supports exact-match and glob-pattern overrides, falling back to a default executor.
type ExecutorRouter struct {
	defaultExec    *adapter.HTTPExecutor
	defaultConfig  *adapter.EnvironmentConfig
	overrides      []routeEntry
	valueOverrides []valueOverrideEntry
}

type routeEntry struct {
	pattern     string
	isGlob      bool
	executor    *adapter.HTTPExecutor
	config      *adapter.EnvironmentConfig
	pathRewrite *adapter.PathRewrite
}

// valueOverrideEntry holds per-node input-value and expected-failure overrides
// that the engine applies during step execution. Resolved independently of
// executor routing so that a node can have one without the other.
type valueOverrideEntry struct {
	pattern       string
	isGlob        bool
	values        map[string]any
	expectFailure *plan.ExpectFailure
}

// NewExecutorRouter creates a router with the given default executor and config.
func NewExecutorRouter(exec *adapter.HTTPExecutor, cfg *adapter.EnvironmentConfig) *ExecutorRouter {
	return &ExecutorRouter{
		defaultExec:   exec,
		defaultConfig: cfg,
	}
}

// AddOverride registers a named or glob-pattern override. Exact matches are
// checked before glob patterns; within each kind the last registered match
// wins, so later sources (.aat-overrides.yaml, --overlay, --override) take
// precedence over earlier ones (env.yaml overrides).
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
// Resolution order: exact matches first, then glob matches, then the default.
// Within each pass the last registered match wins.
func (r *ExecutorRouter) Resolve(nodeName string) (*adapter.HTTPExecutor, *adapter.EnvironmentConfig, *adapter.PathRewrite) {
	// Pass 1: exact matches (last registered wins)
	for i := len(r.overrides) - 1; i >= 0; i-- {
		entry := r.overrides[i]
		if !entry.isGlob && entry.pattern == nodeName {
			return entry.executor, entry.config, entry.pathRewrite
		}
	}

	// Pass 2: glob matches (last registered wins)
	for i := len(r.overrides) - 1; i >= 0; i-- {
		entry := r.overrides[i]
		if entry.isGlob {
			if matched, _ := filepath.Match(entry.pattern, nodeName); matched {
				return entry.executor, entry.config, entry.pathRewrite
			}
		}
	}

	return r.defaultExec, r.defaultConfig, nil
}

// AddResolvedOverride registers an environment or overlay override: a route
// when the override sets baseUrl, auth, headers, or pathRewrite, and its
// values and expectFailure in every case. A value-only override therefore
// leaves the node on whatever route a broader match (or the default) gives it.
func (r *ExecutorRouter) AddResolvedOverride(ov config.ResolvedOverride) {
	if ov.Routes {
		exec := adapter.NewHTTPExecutor(ov.APIConfig.BaseURL)
		cfg := &adapter.EnvironmentConfig{
			BaseURL:   ov.APIConfig.BaseURL,
			Headers:   ov.APIConfig.Headers,
			Protected: ov.APIConfig.Protected,
		}
		var rewrite *adapter.PathRewrite
		if ov.PathRewrite != nil {
			rewrite = &adapter.PathRewrite{
				Strip:  ov.PathRewrite.Strip,
				Prefix: ov.PathRewrite.Prefix,
			}
		}
		r.AddOverride(ov.Pattern, exec, cfg, rewrite)
	}

	var ef *plan.ExpectFailure
	if ov.ExpectFailure != nil {
		ef = &plan.ExpectFailure{
			Status:      ov.ExpectFailure.Status,
			Description: ov.ExpectFailure.Description,
		}
	}
	r.AddValueOverride(ov.Pattern, ov.Values, ef)
}

// AddValueOverride registers per-node input-value and expected-failure
// overrides. Both arguments are optional: pass nil when the override carries
// only one of them. Entries are applied by ResolveValueOverride using the same
// exact-before-glob ordering as executor routing.
func (r *ExecutorRouter) AddValueOverride(pattern string, values map[string]any, ef *plan.ExpectFailure) {
	if len(values) == 0 && ef == nil {
		return
	}
	r.valueOverrides = append(r.valueOverrides, valueOverrideEntry{
		pattern:       pattern,
		isGlob:        isGlobPattern(pattern),
		values:        values,
		expectFailure: ef,
	})
}

// ResolveValueOverride returns the merged value and expected-failure overrides
// for a node. Matches are applied in glob-first, exact-last order so that exact
// matches win over glob matches on key conflict for values, and later
// registrations overwrite earlier ones. For expectFailure, the last exact match
// wins; if no exact match, the last glob match wins.
func (r *ExecutorRouter) ResolveValueOverride(nodeName string) (map[string]any, *plan.ExpectFailure) {
	if len(r.valueOverrides) == 0 {
		return nil, nil
	}
	var values map[string]any
	var exactEF, globEF *plan.ExpectFailure

	mergeValues := func(src map[string]any) {
		if len(src) == 0 {
			return
		}
		if values == nil {
			values = make(map[string]any, len(src))
		}
		for k, v := range src {
			values[k] = v
		}
	}

	// Globs first so exacts can overwrite their values on conflict.
	for _, entry := range r.valueOverrides {
		if entry.isGlob {
			if matched, _ := filepath.Match(entry.pattern, nodeName); matched {
				mergeValues(entry.values)
				if entry.expectFailure != nil {
					globEF = entry.expectFailure
				}
			}
		}
	}
	for _, entry := range r.valueOverrides {
		if !entry.isGlob && entry.pattern == nodeName {
			mergeValues(entry.values)
			if entry.expectFailure != nil {
				exactEF = entry.expectFailure
			}
		}
	}
	ef := exactEF
	if ef == nil {
		ef = globEF
	}
	return values, ef
}

// HasOverrides returns true if any overrides have been configured.
func (r *ExecutorRouter) HasOverrides() bool {
	return len(r.overrides) > 0 || len(r.valueOverrides) > 0
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
