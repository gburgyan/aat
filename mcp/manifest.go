package mcp

import "github.com/gburgyan/aat/config"

// ProjectManifest is a type alias for config.ProjectManifest.
// The canonical definition lives in the config package; this alias
// preserves backward compatibility for existing MCP code.
type ProjectManifest = config.ProjectManifest

// LoadManifest delegates to config.LoadManifest.
func LoadManifest(path string) (*ProjectManifest, error) { return config.LoadManifest(path) }

// FindManifest delegates to config.FindManifest.
func FindManifest() (string, error) { return config.FindManifest() }
