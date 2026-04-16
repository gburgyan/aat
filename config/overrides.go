package config

import (
	"os"
	"path/filepath"
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
