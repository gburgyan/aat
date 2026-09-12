package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrManifestNotFound reports that a manifest named explicitly (--manifest)
// does not exist.
var ErrManifestNotFound = errors.New("manifest not found")

// ProjectPaths holds resolved file paths for a project's artifacts.
// Empty string means "not resolved from any source."
type ProjectPaths struct {
	GraphPath      string
	TemplatesPath  string
	EnvPath        string
	DomainPath     string
	WorkflowsDir   string
	LayersDir      string
	PlanDirs       []string
	ArchiveDir     string
	TracesDir      string
	VisualizersDir string
	ManifestPath   string // path to aat-project.yaml if found
	DefaultEnvName string // default environment name from manifest

	ExplicitManifest string // --manifest flag value; takes priority over auto-discovery
}

// ResolveProjectPaths resolves project artifact paths using a 4-level priority
// chain (ascending — a later manifest replaces an earlier one as a whole, so a
// field it leaves out is not filled in from another project):
//  1. User home config (default_project path)
//  2. AAT_PROJECT env var (directory or .yaml file path)
//  3. CWD manifest discovery (walk up from cwd for aat-project.yaml)
//  4. Explicit flag values in overrides (highest priority)
//
// A discovered level whose manifest does not exist is skipped, so a stale home
// config doesn't block operation when explicit flags are provided; a --manifest
// that does not exist is an error.
// A manifest that exists but fails to load is an error, unless a higher-priority
// level loads a manifest after it: otherwise a typo in aat-project.yaml would
// quietly run against a lower-priority project, or none.
func ResolveProjectPaths(overrides ProjectPaths) (*ProjectPaths, error) {
	result := &ProjectPaths{}
	var manifestErr error
	apply := func(pathOrDir string) {
		loaded, err := applyManifest(result, pathOrDir)
		switch {
		case err != nil:
			manifestErr = err
		case loaded:
			manifestErr = nil
		}
	}

	// 1. User home config
	userCfg, _ := LoadUserConfig()
	if userCfg != nil && userCfg.DefaultProject != "" {
		apply(userCfg.DefaultProject)
	}

	// 2. AAT_PROJECT env var
	if envProject := os.Getenv("AAT_PROJECT"); envProject != "" {
		apply(envProject)
	}

	// 3. CWD manifest discovery
	if found, err := FindManifest(); err == nil {
		apply(found)
	}

	// 3.5. Explicit --manifest flag (overrides auto-discovery). Unlike the
	// discovered levels, a manifest the user named must exist.
	if overrides.ExplicitManifest != "" {
		loaded, err := applyManifest(result, overrides.ExplicitManifest)
		switch {
		case err != nil:
			manifestErr = err
		case !loaded:
			manifestErr = fmt.Errorf("%w: %s", ErrManifestNotFound, resolveManifestPath(overrides.ExplicitManifest))
		default:
			manifestErr = nil
		}
	}
	if manifestErr != nil {
		return nil, manifestErr
	}

	// Default TracesDir to traces/ relative to manifest when not explicitly set
	if result.TracesDir == "" && result.ManifestPath != "" {
		result.TracesDir = filepath.Join(filepath.Dir(result.ManifestPath), "traces")
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
	if overrides.LayersDir != "" {
		result.LayersDir = overrides.LayersDir
	}
	if len(overrides.PlanDirs) > 0 {
		result.PlanDirs = overrides.PlanDirs
	}
	if overrides.ArchiveDir != "" {
		result.ArchiveDir = overrides.ArchiveDir
	}
	if overrides.TracesDir != "" {
		result.TracesDir = overrides.TracesDir
	}
	if overrides.VisualizersDir != "" {
		result.VisualizersDir = overrides.VisualizersDir
	}
	// ManifestPath is only set from discovery, not from overrides.

	return result, nil
}

// applyManifest loads a manifest from pathOrDir and replaces result with its
// paths. pathOrDir may be a directory (looks for aat-project.yaml inside) or a
// direct .yaml file path. It reports whether a manifest was applied; a manifest
// that does not exist is not an error.
func applyManifest(result *ProjectPaths, pathOrDir string) (bool, error) {
	manifestPath := resolveManifestPath(pathOrDir)
	if manifestPath == "" {
		return false, nil
	}
	if _, err := os.Stat(manifestPath); errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	m, err := LoadManifest(manifestPath)
	if err != nil {
		return false, err
	}

	// A manifest describes one project: nothing carries over from a
	// lower-priority project's manifest, such as its domain file or default
	// environment.
	*result = ProjectPaths{}
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
	if m.LayersDir != "" {
		result.LayersDir = m.LayersDir
	}
	if len(m.PlanDirs) > 0 {
		result.PlanDirs = []string(m.PlanDirs)
	}
	if m.ArchiveDir != "" {
		result.ArchiveDir = m.ArchiveDir
	}
	if m.TracesDir != "" {
		result.TracesDir = m.TracesDir
	}
	if m.VisualizersDir != "" {
		result.VisualizersDir = m.VisualizersDir
	}
	if m.DefaultEnvironment != "" {
		result.DefaultEnvName = m.DefaultEnvironment
	}
	return true, nil
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
