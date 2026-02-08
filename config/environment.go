package config

import (
	"context"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ExecutionMode controls how the engine handles LLM involvement.
type ExecutionMode string

const (
	ModeStrict   ExecutionMode = "strict"
	ModeLean     ExecutionMode = "lean"
	ModeAdaptive ExecutionMode = "adaptive"
)

// ArchiveFormat controls the format for run archives.
type ArchiveFormat string

const (
	ArchiveJSON   ArchiveFormat = "json"
	ArchiveJSONGZ ArchiveFormat = "json.gz"
)

// Duration wraps time.Duration with custom YAML unmarshaling via time.ParseDuration.
type Duration struct {
	time.Duration
}

// UnmarshalYAML parses a duration string like "120s", "5m", or "2h30m".
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be a string, got %v", value.Tag)
	}
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value.Value, err)
	}
	d.Duration = parsed
	return nil
}

// SecretRef holds a reference to a secret value, resolved either from an
// environment variable or a literal value.
type SecretRef struct {
	Source string `yaml:"source"`          // "env" or "literal"
	Var    string `yaml:"var,omitempty"`   // environment variable name (when source=env)
	Value  string `yaml:"value,omitempty"` // literal value (when source=literal)
}

// Resolve returns the secret value by resolving the reference.
func (s SecretRef) Resolve() (string, error) {
	switch s.Source {
	case "env":
		val, ok := os.LookupEnv(s.Var)
		if !ok {
			return "", fmt.Errorf("environment variable %q not set", s.Var)
		}
		return val, nil
	case "literal":
		return s.Value, nil
	default:
		return "", fmt.Errorf("unknown secret source %q (expected \"env\" or \"literal\")", s.Source)
	}
}

// IsSet returns true if the SecretRef has a non-empty source.
func (s SecretRef) IsSet() bool {
	return s.Source != ""
}

// AuthConfig describes how to authenticate against the API.
type AuthConfig struct {
	Type        string               `yaml:"type"`                  // oauth2, apikey, bearer, none
	TokenURL    string               `yaml:"tokenUrl,omitempty"`    // token endpoint for oauth2
	HeaderName  string               `yaml:"headerName,omitempty"`  // custom header name for apikey
	Credentials map[string]SecretRef `yaml:"credentials,omitempty"` // named credential fields
}

// LLMConfig holds LLM provider configuration.
type LLMConfig struct {
	Endpoint string        `yaml:"endpoint"`
	APIKey   SecretRef     `yaml:"apiKey"`
	Model    string        `yaml:"model"`
	Mode     ExecutionMode `yaml:"mode"`
}

// RuntimeSettings holds execution-time configuration with sensible defaults.
type RuntimeSettings struct {
	MaxRunDuration     Duration      `yaml:"maxRunDuration"`
	DefaultRetries     int           `yaml:"defaultRetries"`
	MaxRelaxationDepth int           `yaml:"maxRelaxationDepth"`
	ArchiveFormat      ArchiveFormat `yaml:"archiveFormat"`
}

// Environment is the top-level configuration loaded from a YAML file.
type Environment struct {
	Name       string          `yaml:"environment"`
	APIBaseURL string          `yaml:"apiBaseUrl"`
	Auth       AuthConfig      `yaml:"auth"`
	LLM        LLMConfig       `yaml:"llm"`
	Settings   RuntimeSettings `yaml:"settings"`
	Notes      string          `yaml:"notes,omitempty"`
}

// APIConfig is a flat output structure for bridging to adapter.EnvironmentConfig.
type APIConfig struct {
	BaseURL string
	Headers map[string]string
	Values  map[string]string
}

// BuildAPIConfig authenticates and returns a flat APIConfig ready for use.
func (env *Environment) BuildAPIConfig(ctx context.Context) (*APIConfig, error) {
	token, err := Authenticate(ctx, env.Auth)
	if err != nil {
		return nil, fmt.Errorf("authenticating: %w", err)
	}

	headers := make(map[string]string)

	if token != nil {
		switch env.Auth.Type {
		case "apikey":
			headers[env.Auth.HeaderName] = token.AccessToken
		default:
			headers["Authorization"] = "Bearer " + token.AccessToken
		}
	}

	return &APIConfig{
		BaseURL: env.APIBaseURL,
		Headers: headers,
		Values:  make(map[string]string),
	}, nil
}
