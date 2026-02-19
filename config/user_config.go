package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// UserConfig holds persistent user-level settings stored outside any project.
type UserConfig struct {
	DefaultProject string `yaml:"default_project,omitempty"`
}

// UserConfigPath returns the platform-native path for the AAT user config file.
// On macOS this is ~/Library/Application Support/aat/config.yaml,
// on Linux ~/.config/aat/config.yaml, on Windows %AppData%/aat/config.yaml.
func UserConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("determining user config directory: %w", err)
	}
	return filepath.Join(dir, "aat", "config.yaml"), nil
}

// LoadUserConfig reads the user-level config file. If the file does not exist,
// a zero-value config is returned (not an error).
func LoadUserConfig() (*UserConfig, error) {
	path, err := UserConfigPath()
	if err != nil {
		return &UserConfig{}, nil
	}
	return loadUserConfigFrom(path)
}

// loadUserConfigFrom reads and parses a user config from the given path.
// Missing file returns a zero-value config; other errors are returned.
func loadUserConfigFrom(path string) (*UserConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &UserConfig{}, nil
		}
		return nil, fmt.Errorf("reading user config: %w", err)
	}

	var cfg UserConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing user config: %w", err)
	}
	return &cfg, nil
}
