package config

import (
	"os"
	"path/filepath"
	"strings"
)

// ProjectPaths holds resolved file paths for a project's artifacts.
// Empty string means "not resolved from any source."
type ProjectPaths struct {
	GraphPath     string
	TemplatesPath string
	EnvPath       string
	DomainPath    string
	WorkflowsDir  string
	ArchiveDir    string
	TracesDir     string
	ManifestPath  string // path to aat-project.yaml if found

	ExplicitManifest string // --manifest flag value; takes priority over auto-discovery
}

// ResolveProjectPaths resolves project artifact paths using a 4-level priority
// chain (ascending — later overwrites earlier):
//  1. User home config (default_project path)
//  2. AAT_PROJECT env var (directory or .yaml file path)
//  3. CWD manifest discovery (walk up from cwd for aat-project.yaml)
//  4. Explicit flag values in overrides (highest priority)
//
// Lower-priority errors are silently ignored so a stale home config or missing
// CWD manifest doesn't block operation when explicit flags are provided.
func ResolveProjectPaths(overrides ProjectPaths) (*ProjectPaths, error) {
	result := &ProjectPaths{}

	// 1. User home config
	userCfg, _ := LoadUserConfig()
	if userCfg != nil && userCfg.DefaultProject != "" {
		applyManifest(result, userCfg.DefaultProject)
	}

	// 2. AAT_PROJECT env var
	if envProject := os.Getenv("AAT_PROJECT"); envProject != "" {
		applyManifest(result, envProject)
	}

	// 3. CWD manifest discovery
	if found, err := FindManifest(); err == nil {
		applyManifest(result, found)
	}

	// 3.5. Explicit --manifest flag (overrides auto-discovery)
	if overrides.ExplicitManifest != "" {
		applyManifest(result, overrides.ExplicitManifest)
	}

	// 4. Explicit overrides (highest priority)
	if overrides.GraphPath != "" {
		result.GraphPath = overrides.GraphPath
	}
	if overrides.TemplatesPath != "" {
		result.TemplatesPath = overrides.TemplatesPath
	}
	if overrides.EnvPath != "" {
		result.EnvPath = overrides.EnvPath
	}
	if overrides.DomainPath != "" {
		result.DomainPath = overrides.DomainPath
	}
	if overrides.WorkflowsDir != "" {
		result.WorkflowsDir = overrides.WorkflowsDir
	}
	if overrides.ArchiveDir != "" {
		result.ArchiveDir = overrides.ArchiveDir
	}
	if overrides.TracesDir != "" {
		result.TracesDir = overrides.TracesDir
	}
	// ManifestPath is only set from discovery, not from overrides.

	return result, nil
}

// applyManifest loads a manifest from pathOrDir and applies its paths to result.
// pathOrDir may be a directory (looks for aat-project.yaml inside) or a direct
// .yaml file path. Errors are silently ignored.
func applyManifest(result *ProjectPaths, pathOrDir string) {
	manifestPath := resolveManifestPath(pathOrDir)
	if manifestPath == "" {
		return
	}

	m, err := LoadManifest(manifestPath)
	if err != nil {
		return
	}

	result.ManifestPath = manifestPath
	if m.GraphPath != "" {
		result.GraphPath = m.GraphPath
	}
	if m.TemplatesPath != "" {
		result.TemplatesPath = m.TemplatesPath
	}
	if m.EnvPath != "" {
		result.EnvPath = m.EnvPath
	}
	if m.DomainPath != "" {
		result.DomainPath = m.DomainPath
	}
	if m.WorkflowsDir != "" {
		result.WorkflowsDir = m.WorkflowsDir
	}
	if m.ArchiveDir != "" {
		result.ArchiveDir = m.ArchiveDir
	}
	if m.TracesDir != "" {
		result.TracesDir = m.TracesDir
	}
}

// resolveManifestPath converts a user-provided path to an aat-project.yaml
// file path. If pathOrDir is a directory, it looks for aat-project.yaml inside.
// If it's a .yaml file, it's used directly. Returns "" if nothing is found.
func resolveManifestPath(pathOrDir string) string {
	info, err := os.Stat(pathOrDir)
	if err != nil {
		// Path doesn't exist — could it be a .yaml that we just try?
		if strings.HasSuffix(pathOrDir, ".yaml") || strings.HasSuffix(pathOrDir, ".yml") {
			return pathOrDir
		}
		// Try directory + aat-project.yaml
		candidate := filepath.Join(pathOrDir, "aat-project.yaml")
		return candidate
	}

	if info.IsDir() {
		return filepath.Join(pathOrDir, "aat-project.yaml")
	}

	// It's a file — use directly
	return pathOrDir
}
