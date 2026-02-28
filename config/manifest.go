package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// StringOrList is a YAML type that accepts either a single string or a list of strings.
// This allows manifest fields to be written as either:
//
//	plans: plans/
//	plans: [plans/, /ext/plans]
type StringOrList []string

// UnmarshalYAML implements custom YAML unmarshalling for StringOrList.
func (s *StringOrList) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try single string first
	var single string
	if err := unmarshal(&single); err == nil {
		*s = StringOrList{single}
		return nil
	}

	// Try list of strings
	var list []string
	if err := unmarshal(&list); err != nil {
		return err
	}
	*s = StringOrList(list)
	return nil
}

// ProjectManifest describes where a project's artifacts live.
// Paths are resolved relative to the manifest file's directory.
type ProjectManifest struct {
	Name           string       `yaml:"name"`
	Description    string       `yaml:"description,omitempty"`
	Tags           []string     `yaml:"tags,omitempty"`
	GraphPath      string       `yaml:"graph"`
	TemplatesPath  string       `yaml:"templates"`
	DomainPath     string       `yaml:"domain,omitempty"`
	DocsDir        string       `yaml:"docs,omitempty"`
	WorkflowsDir   string       `yaml:"workflows,omitempty"`
	LayersDir      string       `yaml:"layers,omitempty"`
	PlanDirs       StringOrList `yaml:"plans,omitempty"`
	OASPaths       StringOrList `yaml:"oas,omitempty"`
	ArchiveDir     string       `yaml:"archives,omitempty"`
	TracesDir      string       `yaml:"traces,omitempty"`
	VisualizersDir string       `yaml:"visualizers,omitempty"`
	EnvPath        string       `yaml:"environment,omitempty"`
}

// LoadManifest reads and parses an aat-project.yaml file. Paths in the manifest
// are resolved relative to the manifest file's directory.
func LoadManifest(path string) (*ProjectManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}

	var m ProjectManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	// Validate required fields
	if m.GraphPath == "" {
		return nil, fmt.Errorf("manifest missing required field: graph")
	}
	if m.TemplatesPath == "" {
		return nil, fmt.Errorf("manifest missing required field: templates")
	}

	// Resolve paths relative to manifest directory
	baseDir := filepath.Dir(path)
	m.GraphPath = resolvePath(baseDir, m.GraphPath)
	m.TemplatesPath = resolvePath(baseDir, m.TemplatesPath)
	if m.DomainPath != "" {
		m.DomainPath = resolvePath(baseDir, m.DomainPath)
	}
	if m.DocsDir != "" {
		m.DocsDir = resolvePath(baseDir, m.DocsDir)
	}
	if m.WorkflowsDir != "" {
		m.WorkflowsDir = resolvePath(baseDir, m.WorkflowsDir)
	}
	if m.LayersDir != "" {
		m.LayersDir = resolvePath(baseDir, m.LayersDir)
	}
	for i, dir := range m.PlanDirs {
		m.PlanDirs[i] = resolvePath(baseDir, dir)
	}
	for i, p := range m.OASPaths {
		m.OASPaths[i] = resolvePath(baseDir, p)
	}
	if m.ArchiveDir != "" {
		m.ArchiveDir = resolvePath(baseDir, m.ArchiveDir)
	}
	if m.TracesDir != "" {
		m.TracesDir = resolvePath(baseDir, m.TracesDir)
	}
	if m.VisualizersDir != "" {
		m.VisualizersDir = resolvePath(baseDir, m.VisualizersDir)
	}
	if m.EnvPath != "" {
		m.EnvPath = resolvePath(baseDir, m.EnvPath)
	}

	return &m, nil
}

// FindManifest searches for aat-project.yaml starting from the current working
// directory and walking up to parent directories. Returns the path to the first
// manifest found, or an error if none is found.
func FindManifest() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}

	for {
		candidate := filepath.Join(dir, "aat-project.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			return "", fmt.Errorf("aat-project.yaml not found (searched from working directory to filesystem root)")
		}
		dir = parent
	}
}

// resolvePath resolves a path relative to baseDir. If path is already absolute,
// it is returned unchanged.
func resolvePath(baseDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}
