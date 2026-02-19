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
	Provider string        `yaml:"provider,omitempty"` // "openai" or "anthropic"; auto-detected from endpoint if empty
}

// RuntimeSettings holds execution-time configuration with sensible defaults.
type RuntimeSettings struct {
	MaxRunDuration     Duration      `yaml:"maxRunDuration"`
	DefaultRetries     int           `yaml:"defaultRetries"`
	MaxRelaxationDepth int           `yaml:"maxRelaxationDepth"`
	ArchiveFormat      ArchiveFormat `yaml:"archiveFormat"`
}

// PathRewrite controls URL path rewriting for overrides.
type PathRewrite struct {
	Strip  string `yaml:"strip,omitempty"`  // prefix to remove from the template path
	Prefix string `yaml:"prefix,omitempty"` // prefix to add after stripping
}

// HostOverride maps a node/adapter name pattern to alternate API configuration.
type HostOverride struct {
	Match       string            `yaml:"match"`                 // node name or glob pattern
	BaseURL     string            `yaml:"baseUrl,omitempty"`     // override base URL
	Auth        *AuthConfig       `yaml:"auth,omitempty"`        // nil = inherit top-level auth
	Headers     map[string]string `yaml:"headers,omitempty"`     // merged with env-level headers
	PathRewrite *PathRewrite      `yaml:"pathRewrite,omitempty"` // URL path rewriting
}

// ResolvedOverride is a HostOverride after authentication and header merging.
type ResolvedOverride struct {
	Pattern     string
	APIConfig   APIConfig
	PathRewrite *PathRewrite
}

// Environment is the top-level configuration loaded from a YAML file.
type Environment struct {
	Name       string            `yaml:"environment"`
	APIBaseURL string            `yaml:"apiBaseUrl"`
	Auth       AuthConfig        `yaml:"auth"`
	Headers    map[string]string `yaml:"headers,omitempty"` // static headers added to every request
	LLM        LLMConfig         `yaml:"llm"`
	Settings   RuntimeSettings   `yaml:"settings"`
	Notes      string            `yaml:"notes,omitempty"`
	Overrides  []HostOverride    `yaml:"overrides,omitempty"` // per-node routing overrides
}

// BuildOverrideConfigs authenticates and resolves each HostOverride into a
// ResolvedOverride with merged headers. Overrides that omit Auth inherit the
// top-level auth; overrides that omit BaseURL inherit the top-level apiBaseUrl.
func (env *Environment) BuildOverrideConfigs(ctx context.Context, baseHeaders map[string]string) ([]ResolvedOverride, error) {
	return env.BuildOverrideConfigsWithAuth(ctx, baseHeaders, env.Auth)
}

// BuildOverrideConfigsWithAuth is like BuildOverrideConfigs but overrides that
// omit their own auth inherit defaultAuth instead of the environment auth.
// This is used when a plan provides its own auth that should cascade to overrides.
func (env *Environment) BuildOverrideConfigsWithAuth(ctx context.Context, baseHeaders map[string]string, defaultAuth AuthConfig) ([]ResolvedOverride, error) {
	if len(env.Overrides) == 0 {
		return nil, nil
	}

	resolved := make([]ResolvedOverride, 0, len(env.Overrides))
	for i, ov := range env.Overrides {
		// Determine auth: inherit defaultAuth when override omits auth
		auth := defaultAuth
		if ov.Auth != nil {
			auth = *ov.Auth
		}

		// Authenticate
		token, err := Authenticate(ctx, auth)
		if err != nil {
			return nil, fmt.Errorf("authenticating override %d (%s): %w", i, ov.Match, err)
		}

		// Merge headers: start with base, overlay override-specific, then auth
		headers := make(map[string]string)
		for k, v := range baseHeaders {
			headers[k] = v
		}
		for k, v := range ov.Headers {
			headers[k] = v
		}
		if token != nil {
			switch auth.Type {
			case "apikey":
				headers[auth.HeaderName] = token.AccessToken
			default:
				headers["Authorization"] = "Bearer " + token.AccessToken
			}
		} else {
			// auth type "none" — remove any inherited auth header
			if ov.Auth != nil && (ov.Auth.Type == "none" || ov.Auth.Type == "") {
				delete(headers, "Authorization")
			}
		}

		baseURL := ov.BaseURL
		if baseURL == "" {
			baseURL = env.APIBaseURL
		}

		resolved = append(resolved, ResolvedOverride{
			Pattern: ov.Match,
			APIConfig: APIConfig{
				BaseURL: baseURL,
				Headers: headers,
				Values:  make(map[string]string),
			},
			PathRewrite: ov.PathRewrite,
		})
	}

	return resolved, nil
}

// CollectSecrets resolves all SecretRef values from the environment and returns
// them as a set. This is used for value-matching redaction in archives.
// Resolution errors are silently ignored (missing env var = no secret to redact).
func (env *Environment) CollectSecrets() map[string]bool {
	secrets := make(map[string]bool)

	// Auth credentials
	for _, ref := range env.Auth.Credentials {
		if val, err := ref.Resolve(); err == nil && val != "" {
			secrets[val] = true
		}
	}

	// LLM API key
	if env.LLM.APIKey.IsSet() {
		if val, err := env.LLM.APIKey.Resolve(); err == nil && val != "" {
			secrets[val] = true
		}
	}

	return secrets
}

// CollectAuthSecrets resolves all SecretRef values from an AuthConfig and returns
// them as a set. This is used to merge plan-level auth secrets for redaction.
// Resolution errors are silently ignored.
func CollectAuthSecrets(auth *AuthConfig) map[string]bool {
	secrets := make(map[string]bool)
	if auth == nil {
		return secrets
	}
	for _, ref := range auth.Credentials {
		if val, err := ref.Resolve(); err == nil && val != "" {
			secrets[val] = true
		}
	}
	return secrets
}

// APIConfig is a flat output structure for bridging to adapter.EnvironmentConfig.
type APIConfig struct {
	BaseURL string
	Headers map[string]string
	Values  map[string]string
}

// BuildAPIConfig authenticates and returns a flat APIConfig ready for use.
// Custom headers from the environment are included first; auth headers override.
func (env *Environment) BuildAPIConfig(ctx context.Context) (*APIConfig, error) {
	return env.BuildAPIConfigFromAuth(ctx, env.Auth, nil)
}

// BuildAPIConfigFromAuth authenticates using the provided auth config and returns
// a flat APIConfig. Header merge order: env headers → extraHeaders → auth headers.
// extraHeaders may be nil.
func (env *Environment) BuildAPIConfigFromAuth(ctx context.Context, auth AuthConfig, extraHeaders map[string]string) (*APIConfig, error) {
	token, err := Authenticate(ctx, auth)
	if err != nil {
		return nil, fmt.Errorf("authenticating: %w", err)
	}

	headers := make(map[string]string)

	// 1. Environment static headers (base)
	for k, v := range env.Headers {
		headers[k] = v
	}

	// 2. Extra headers (e.g., plan-level headers) override env headers
	for k, v := range extraHeaders {
		headers[k] = v
	}

	// 3. Auth headers override everything
	if token != nil {
		switch auth.Type {
		case "apikey":
			headers[auth.HeaderName] = token.AccessToken
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
