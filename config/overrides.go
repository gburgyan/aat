package config

import (
	"os"
	"path/filepath"
)

// AutoOverridesFile is the filename for auto-discovered local override dotfiles.
const AutoOverridesFile = ".aat-overrides.yaml"

// FindAutoOverrides searches for a .aat-overrides.yaml file starting from the
// current working directory and walking up to parent directories. Returns the
// path to the first file found, or an empty string if none is found.
// Unlike FindManifest, absence is the normal case — no error on not-found.
func FindAutoOverrides() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		candidate := filepath.Join(dir, AutoOverridesFile)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
