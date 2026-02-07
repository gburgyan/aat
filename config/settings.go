package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Settings holds the credentials and endpoint configuration loaded from a JSON file.
type Settings struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	AccessGroup  string `json:"access_group"`
	AuthURL      string `json:"auth_url"`
	APIBase      string `json:"api_base"`
}

// LoadSettings reads a JSON settings file from disk.
func LoadSettings(path string) (*Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading settings file: %w", err)
	}

	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing settings JSON: %w", err)
	}

	if s.AuthURL == "" {
		return nil, fmt.Errorf("settings missing required field: auth_url")
	}
	if s.APIBase == "" {
		return nil, fmt.Errorf("settings missing required field: api_base")
	}

	return &s, nil
}
