package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// varPattern matches ${varName} placeholders in strings.
var varPattern = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

// LoadNamedEnvironment loads a specific environment from a YAML file. If the file
// is in multi-environment format, envName selects which environment. If the file is
// in legacy single-environment format, envName must be empty.
func LoadNamedEnvironment(path, envName string) (*Environment, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading environment file: %w", err)
	}

	if isMultiEnv(data) {
		if envName == "" {
			names, _ := listEnvNamesFromData(data)
			return nil, fmt.Errorf("env file defines multiple environments (%s); specify one with --env-name or set defaultEnvironment in aat-project.yaml",
				strings.Join(names, ", "))
		}
		return loadMultiEnv(path, data, envName)
	}

	// Legacy single-env format
	if envName != "" {
		return nil, fmt.Errorf("env file is single-environment format; --env-name is not applicable")
	}
	return loadLegacyEnv(data)
}

// ListEnvironments returns the names of all non-abstract environments defined in
// a multi-environment file. For legacy files, it returns a single-element list with
// the environment name.
func ListEnvironments(path string) ([]string, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading environment file: %w", err)
	}

	if isMultiEnv(data) {
		mef, err := parseAndMergeIncludes(path, data)
		if err != nil {
			return nil, err
		}
		return listEnvNamesFromMEF(mef), nil
	}

	// Legacy format — return the environment name
	var env Environment
	if err := yaml.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parsing environment YAML: %w", err)
	}
	return []string{env.Name}, nil
}

// IsMultiEnvFile returns true if the file at path is a multi-environment format.
func IsMultiEnvFile(path string) (bool, error) {
	data, err := readFile(path)
	if err != nil {
		return false, err
	}
	return isMultiEnv(data), nil
}

// isMultiEnv detects whether YAML data is in multi-environment format by checking
// for the presence of an "environments" top-level key.
func isMultiEnv(data []byte) bool {
	var probe struct {
		Environments map[string]yaml.Node `yaml:"environments"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return false
	}
	return len(probe.Environments) > 0
}

// listEnvNamesFromData returns sorted non-abstract environment names from raw YAML data.
// This does NOT process includes — use for quick probing only (e.g., error messages).
func listEnvNamesFromData(data []byte) ([]string, error) {
	var mef MultiEnvironmentFile
	if err := yaml.Unmarshal(data, &mef); err != nil {
		return nil, fmt.Errorf("parsing multi-env YAML: %w", err)
	}
	return listEnvNamesFromMEF(&mef), nil
}

// listEnvNamesFromMEF returns sorted non-abstract environment names from a parsed file.
func listEnvNamesFromMEF(mef *MultiEnvironmentFile) []string {
	var names []string
	for name := range mef.Environments {
		if !strings.HasPrefix(name, "_") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// parseAndMergeIncludes parses a multi-env YAML file and merges any include files.
// Include paths are resolved relative to the base file's directory.
// Include files use the same format (shared + environments) but must not have their
// own include directives (no recursive includes).
func parseAndMergeIncludes(basePath string, data []byte) (*MultiEnvironmentFile, error) {
	var mef MultiEnvironmentFile
	if err := yaml.Unmarshal(data, &mef); err != nil {
		return nil, fmt.Errorf("parsing multi-env YAML: %w", err)
	}

	if len(mef.Include) == 0 {
		return &mef, nil
	}

	baseDir := filepath.Dir(basePath)
	for _, incPath := range mef.Include {
		resolvedPath := incPath
		if !filepath.IsAbs(incPath) {
			resolvedPath = filepath.Join(baseDir, incPath)
		}

		incData, err := os.ReadFile(resolvedPath)
		if err != nil {
			return nil, fmt.Errorf("reading include file %q: %w", incPath, err)
		}

		var incFile MultiEnvironmentFile
		if err := yaml.Unmarshal(incData, &incFile); err != nil {
			return nil, fmt.Errorf("parsing include file %q: %w", incPath, err)
		}

		if len(incFile.Include) > 0 {
			return nil, fmt.Errorf("include file %q must not contain its own include directives", incPath)
		}

		// Merge include's shared into base shared (include overlays base)
		mef.Shared = mergePartials(mef.Shared, incFile.Shared)

		// Merge include's environments into base environments
		if mef.Environments == nil {
			mef.Environments = make(map[string]EnvironmentPartial)
		}
		for name, incEnv := range incFile.Environments {
			if base, ok := mef.Environments[name]; ok {
				mef.Environments[name] = mergePartials(base, incEnv)
			} else {
				mef.Environments[name] = incEnv
			}
		}
	}

	return &mef, nil
}

// loadLegacyEnv loads a single-environment YAML file (existing format).
func loadLegacyEnv(data []byte) (*Environment, error) {
	var env Environment
	if err := yaml.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parsing environment YAML: %w", err)
	}
	applyDefaults(&env)
	if err := ValidateEnvironment(&env); err != nil {
		return nil, err
	}
	return &env, nil
}

// loadMultiEnv parses a multi-environment YAML file (with includes) and resolves
// the named environment.
func loadMultiEnv(basePath string, data []byte, envName string) (*Environment, error) {
	mef, err := parseAndMergeIncludes(basePath, data)
	if err != nil {
		return nil, err
	}

	if _, ok := mef.Environments[envName]; !ok {
		names := listEnvNamesFromMEF(mef)
		return nil, fmt.Errorf("environment %q not found; available: %s", envName, strings.Join(names, ", "))
	}

	if strings.HasPrefix(envName, "_") {
		return nil, fmt.Errorf("environment %q is abstract (underscore prefix) and cannot be selected directly", envName)
	}

	// Resolve inheritance chain
	resolved, err := resolveInheritance(mef.Environments, envName, make(map[string]bool))
	if err != nil {
		return nil, fmt.Errorf("resolving environment %q: %w", envName, err)
	}

	// Merge shared defaults underneath (shared is lowest priority)
	merged := mergePartials(mef.Shared, resolved)

	// Substitute vars
	if err := substituteVars(&merged); err != nil {
		return nil, fmt.Errorf("environment %q: %w", envName, err)
	}

	// Convert to Environment
	env := toEnvironment(envName, merged)
	applyDefaults(env)

	if err := validateMultiEnv(env); err != nil {
		return nil, err
	}

	return env, nil
}

// resolveInheritance recursively resolves the extends chain for an environment.
func resolveInheritance(envs map[string]EnvironmentPartial, name string, visited map[string]bool) (EnvironmentPartial, error) {
	if visited[name] {
		return EnvironmentPartial{}, fmt.Errorf("circular inheritance detected: %s", name)
	}
	visited[name] = true

	partial, ok := envs[name]
	if !ok {
		return EnvironmentPartial{}, fmt.Errorf("environment %q not found", name)
	}

	if partial.Extends == "" {
		return partial, nil
	}

	// Resolve parent first
	parent, err := resolveInheritance(envs, partial.Extends, visited)
	if err != nil {
		return EnvironmentPartial{}, fmt.Errorf("extending %q: %w", partial.Extends, err)
	}

	// Merge child on top of resolved parent
	return mergePartials(parent, partial), nil
}

// mergePartials deep-merges overlay on top of base according to per-field rules.
// See plan for merge semantics: maps merge, auth/llm replace, overrides prepend.
func mergePartials(base, overlay EnvironmentPartial) EnvironmentPartial {
	result := EnvironmentPartial{}

	// apiBaseUrl: overlay wins if non-empty
	result.APIBaseURL = base.APIBaseURL
	if overlay.APIBaseURL != "" {
		result.APIBaseURL = overlay.APIBaseURL
	}

	// auth: overlay wins entirely (full replace)
	if overlay.Auth != nil {
		result.Auth = overlay.Auth
	} else if base.Auth != nil {
		authCopy := *base.Auth
		result.Auth = &authCopy
	}

	// headers: map merge (overlay keys win, base keys preserved)
	result.Headers = mergeMaps(base.Headers, overlay.Headers)

	// llm: overlay wins entirely (full replace)
	if overlay.LLM != nil {
		result.LLM = overlay.LLM
	} else if base.LLM != nil {
		llmCopy := *base.LLM
		result.LLM = &llmCopy
	}

	// settings: field-level merge
	result.Settings = mergeSettings(base.Settings, overlay.Settings)

	// notes: overlay wins if non-empty
	result.Notes = base.Notes
	if overlay.Notes != "" {
		result.Notes = overlay.Notes
	}

	// overrides: child prepended before parent (child has higher match priority)
	result.Overrides = mergeOverrideSlices(base.Overrides, overlay.Overrides)

	// values: map merge
	result.Values = mergeMaps(base.Values, overlay.Values)

	// vars: map merge
	result.Vars = mergeMaps(base.Vars, overlay.Vars)

	// extends is not carried forward after resolution
	result.Extends = ""

	return result
}

// mergeMaps returns a new map with base entries overlaid by overlay entries.
func mergeMaps(base, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	result := make(map[string]string)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overlay {
		result[k] = v
	}
	return result
}

// mergeSettings does field-level merge of RuntimeSettings pointers.
func mergeSettings(base, overlay *RuntimeSettings) *RuntimeSettings {
	if base == nil && overlay == nil {
		return nil
	}
	if base == nil {
		sCopy := *overlay
		return &sCopy
	}
	if overlay == nil {
		sCopy := *base
		return &sCopy
	}

	result := *base
	if overlay.MaxRunDuration.Duration != 0 {
		result.MaxRunDuration = overlay.MaxRunDuration
	}
	if overlay.DefaultRetries != 0 {
		result.DefaultRetries = overlay.DefaultRetries
	}
	if overlay.ArchiveFormat != "" {
		result.ArchiveFormat = overlay.ArchiveFormat
	}
	if overlay.OASValidation != "" {
		result.OASValidation = overlay.OASValidation
	}
	return &result
}

// mergeOverrideSlices prepends child overrides before parent overrides.
func mergeOverrideSlices(parent, child []HostOverride) []HostOverride {
	if len(parent) == 0 && len(child) == 0 {
		return nil
	}
	result := make([]HostOverride, 0, len(child)+len(parent))
	result = append(result, child...)
	result = append(result, parent...)
	return result
}

// substituteVars replaces ${key} placeholders in all string fields of the partial
// using the vars map. Returns an error for any unresolved placeholders.
func substituteVars(p *EnvironmentPartial) error {
	if len(p.Vars) == 0 {
		// Check if there are any unresolved vars in the partial
		return checkUnresolvedVars(p)
	}

	p.APIBaseURL = substituteString(p.APIBaseURL, p.Vars)
	if p.Auth != nil {
		p.Auth.TokenURL = substituteString(p.Auth.TokenURL, p.Vars)
		p.Auth.HeaderName = substituteString(p.Auth.HeaderName, p.Vars)
		for k, v := range p.Auth.Credentials {
			v.Var = substituteString(v.Var, p.Vars)
			v.Value = substituteString(v.Value, p.Vars)
			p.Auth.Credentials[k] = v
		}
	}
	if p.LLM != nil {
		p.LLM.Endpoint = substituteString(p.LLM.Endpoint, p.Vars)
		p.LLM.Model = substituteString(p.LLM.Model, p.Vars)
	}
	for k, v := range p.Headers {
		p.Headers[k] = substituteString(v, p.Vars)
	}
	for k, v := range p.Values {
		p.Values[k] = substituteString(v, p.Vars)
	}
	p.Notes = substituteString(p.Notes, p.Vars)

	for i := range p.Overrides {
		p.Overrides[i].Match = substituteString(p.Overrides[i].Match, p.Vars)
		p.Overrides[i].BaseURL = substituteString(p.Overrides[i].BaseURL, p.Vars)
		if p.Overrides[i].PathRewrite != nil {
			p.Overrides[i].PathRewrite.Strip = substituteString(p.Overrides[i].PathRewrite.Strip, p.Vars)
			p.Overrides[i].PathRewrite.Prefix = substituteString(p.Overrides[i].PathRewrite.Prefix, p.Vars)
		}
		for k, v := range p.Overrides[i].Headers {
			p.Overrides[i].Headers[k] = substituteString(v, p.Vars)
		}
		if p.Overrides[i].Auth != nil {
			p.Overrides[i].Auth.TokenURL = substituteString(p.Overrides[i].Auth.TokenURL, p.Vars)
		}
	}

	return checkUnresolvedVars(p)
}

// substituteString replaces ${key} placeholders with values from vars.
func substituteString(s string, vars map[string]string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return varPattern.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-1] // strip ${ and }
		if val, ok := vars[key]; ok {
			return val
		}
		return match // leave unresolved for error checking
	})
}

// checkUnresolvedVars scans all string fields for remaining ${...} placeholders.
func checkUnresolvedVars(p *EnvironmentPartial) error {
	var unresolved []string
	check := func(s string) {
		unresolved = append(unresolved, varPattern.FindAllString(s, -1)...)
	}

	check(p.APIBaseURL)
	check(p.Notes)
	if p.Auth != nil {
		check(p.Auth.TokenURL)
		check(p.Auth.HeaderName)
	}
	if p.LLM != nil {
		check(p.LLM.Endpoint)
		check(p.LLM.Model)
	}
	for _, v := range p.Headers {
		check(v)
	}
	for _, v := range p.Values {
		check(v)
	}
	for _, ov := range p.Overrides {
		check(ov.Match)
		check(ov.BaseURL)
		if ov.PathRewrite != nil {
			check(ov.PathRewrite.Strip)
			check(ov.PathRewrite.Prefix)
		}
		for _, v := range ov.Headers {
			check(v)
		}
	}

	if len(unresolved) > 0 {
		// Deduplicate
		seen := make(map[string]bool)
		var unique []string
		for _, u := range unresolved {
			if !seen[u] {
				seen[u] = true
				unique = append(unique, u)
			}
		}
		return fmt.Errorf("unresolved variable(s): %s", strings.Join(unique, ", "))
	}
	return nil
}

// toEnvironment converts a fully-resolved EnvironmentPartial to an Environment.
func toEnvironment(name string, p EnvironmentPartial) *Environment {
	env := &Environment{
		Name:       name,
		APIBaseURL: p.APIBaseURL,
		Headers:    p.Headers,
		Notes:      p.Notes,
		Overrides:  p.Overrides,
		Values:     p.Values,
	}
	if p.Auth != nil {
		env.Auth = *p.Auth
	}
	if p.LLM != nil {
		env.LLM = *p.LLM
	}
	if p.Settings != nil {
		env.Settings = *p.Settings
	}
	return env
}

// validateMultiEnv validates an environment loaded from a multi-env file.
// Unlike ValidateEnvironment, apiBaseUrl is not required (direct-service-only envs
// may route entirely through overrides).
func validateMultiEnv(env *Environment) error {
	var errs []string

	if env.Name == "" {
		errs = append(errs, "environment name is required")
	}

	errs = append(errs, ValidateAuth(&env.Auth)...)
	errs = append(errs, validateSettings(&env.Settings)...)
	errs = append(errs, validateOverrides(env.Overrides)...)

	if len(errs) > 0 {
		return fmt.Errorf("environment validation failed:\n- %s", strings.Join(errs, "\n- "))
	}
	return nil
}
