package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// AutoOverridesFile is the filename for auto-discovered local override dotfiles.
const AutoOverridesFile = ".aat-overrides.yaml"

// AutoOverridesFileNoDot is the non-dotfile variant also accepted by auto-discovery.
const AutoOverridesFileNoDot = "aat-overrides.yaml"

// FindAutoOverrides searches for an overrides file starting from the current
// working directory and walking up to parent directories. In each directory it
// checks for .aat-overrides.yaml first, then aat-overrides.yaml (dotfile wins
// if both exist). Returns the path to the first file found, or an empty string
// if none is found.
// Unlike FindManifest, absence is the normal case — no error on not-found.
func FindAutoOverrides() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		for _, name := range []string{AutoOverridesFile, AutoOverridesFileNoDot} {
			candidate := filepath.Join(dir, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// PeekOverlayEnvironment reads only the `environment` field from an overlay
// file without running full validation. Returns "" if the field is absent.
// Used to resolve env-name precedence before the environment is loaded.
func PeekOverlayEnvironment(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading overlay file: %w", err)
	}
	var stub struct {
		Environment string `yaml:"environment"`
	}
	if err := yaml.Unmarshal(data, &stub); err != nil {
		return "", fmt.Errorf("parsing overlay YAML: %w", err)
	}
	return stub.Environment, nil
}
