package config

import (
	"context"
	"fmt"
	"os"
)

// SecretRef holds a reference to a secret value, resolved either from an
// environment variable or a literal value.
type SecretRef struct {
	Source string `yaml:"source" json:"source"`                   // "env" or "literal"
	Var    string `yaml:"var,omitempty" json:"var,omitempty"`     // environment variable name (when source=env)
	Value  string `yaml:"value,omitempty" json:"value,omitempty"` // literal value (when source=literal)
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
	Type        string               `yaml:"type" json:"type"`                                   // oauth2, apikey, bearer, none
	TokenURL    string               `yaml:"tokenUrl,omitempty" json:"tokenUrl,omitempty"`       // token endpoint for oauth2
	HeaderName  string               `yaml:"headerName,omitempty" json:"headerName,omitempty"`   // custom header name for apikey
	GrantType   string               `yaml:"grantType,omitempty" json:"grantType,omitempty"`     // oauth2 grant_type (default: "password")
	ExtraParams map[string]string    `yaml:"extraParams,omitempty" json:"extraParams,omitempty"` // extra form params for oauth2 token request
	Credentials map[string]SecretRef `yaml:"credentials,omitempty" json:"credentials,omitempty"` // named credential fields
}

// LLMConfig holds LLM provider configuration.
type LLMConfig struct {
	Endpoint string    `yaml:"endpoint"`
	APIKey   SecretRef `yaml:"apiKey"`
	Model    string    `yaml:"model"`
	Provider string    `yaml:"provider,omitempty"` // "openai" or "anthropic"; auto-detected from endpoint if empty
}

// RuntimeSettings holds execution-time configuration.
type RuntimeSettings struct {
	OASValidation string `yaml:"oasValidation,omitempty"` // "auto" (default), "strict", "off"
	// MinRequestInterval is the least time between the starts of two requests,
	// a duration such as "250ms", shared by every run of one invocation. Empty
	// means requests are not paced. See RequestInterval.
	MinRequestInterval string `yaml:"minRequestInterval,omitempty"`
}

// PathRewrite controls URL path rewriting for overrides.
type PathRewrite struct {
	Strip  string `yaml:"strip,omitempty"`  // prefix to remove from the template path
	Prefix string `yaml:"prefix,omitempty"` // prefix to add after stripping
}

// HostOverride maps a node/adapter name pattern to alternate API configuration.
type HostOverride struct {
	Match         string                 `yaml:"match"`                   // node name or glob pattern
	BaseURL       string                 `yaml:"baseUrl,omitempty"`       // override base URL
	Auth          *AuthConfig            `yaml:"auth,omitempty"`          // nil = inherit top-level auth
	Headers       map[string]string      `yaml:"headers,omitempty"`       // merged with env-level headers
	PathRewrite   *PathRewrite           `yaml:"pathRewrite,omitempty"`   // URL path rewriting
	Values        map[string]any         `yaml:"values,omitempty"`        // per-input value overrides merged after plan/graph resolution
	ExpectFailure *OverrideExpectFailure `yaml:"expectFailure,omitempty"` // force negative-assertion behavior on matched steps
}

// OverrideExpectFailure declares expected failure statuses for matched nodes
// without editing the plan. Translated to plan.ExpectFailure at engine time.
type OverrideExpectFailure struct {
	Status      []int  `yaml:"status"`
	Description string `yaml:"description,omitempty"`
}

// ResolvedOverride is a HostOverride after authentication and header merging.
type ResolvedOverride struct {
	Pattern string
	// Routes is true when the override sets baseUrl, auth, headers, or
	// pathRewrite. Value-only overrides (values/expectFailure) must not
	// register a route, or they would shadow a broader routing match.
	Routes        bool
	APIConfig     APIConfig
	PathRewrite   *PathRewrite
	Values        map[string]any
	ExpectFailure *OverrideExpectFailure
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
	Values     map[string]string `yaml:"values,omitempty"`    // project-level values available via {{env.KEY}}
}

// BuildOverrideConfigs authenticates and resolves each HostOverride into a
// ResolvedOverride with merged headers. Overrides that omit Auth inherit the
// top-level auth; overrides that omit BaseURL inherit the top-level apiBaseUrl.
// base is the default route the overrides start from (nil for no headers).
func (env *Environment) BuildOverrideConfigs(ctx context.Context, base *APIConfig) ([]ResolvedOverride, error) {
	return env.BuildOverrideConfigsWithAuth(ctx, base, env.Auth)
}

// BuildOverrideConfigsWithAuth is like BuildOverrideConfigs but overrides that
// omit their own auth inherit defaultAuth instead of the environment auth.
// This is used when a plan provides its own auth that should cascade to overrides.
func (env *Environment) BuildOverrideConfigsWithAuth(ctx context.Context, base *APIConfig, defaultAuth AuthConfig) ([]ResolvedOverride, error) {
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

		resolved = append(resolved, env.resolveOverride(ov, overrideRoute(base, ov, defaultAuth, auth, token)))
	}

	return resolved, nil
}

// routes reports whether an override changes where or how a request is sent.
// Entries that carry only values or expectFailure leave routing to broader
// matches (or the default executor).
func (ov HostOverride) routes() bool {
	return ov.BaseURL != "" || ov.Auth != nil || len(ov.Headers) > 0 || ov.PathRewrite != nil
}

// resolveOverride assembles a ResolvedOverride from an override and the headers
// of its route. An empty BaseURL inherits the environment's apiBaseUrl.
func (env *Environment) resolveOverride(ov HostOverride, route APIConfig) ResolvedOverride {
	route.BaseURL = ov.BaseURL
	if route.BaseURL == "" {
		route.BaseURL = env.APIBaseURL
	}
	return ResolvedOverride{
		Pattern:       ov.Match,
		Routes:        ov.routes(),
		APIConfig:     route,
		PathRewrite:   ov.PathRewrite,
		Values:        ov.Values,
		ExpectFailure: ov.ExpectFailure,
	}
}

// overrideRoute merges the headers of an override's route, in order: the base
// route's headers, the override's own headers, the credential of the effective
// auth, and the base route's overlay headers, which apply on every route. When
// the override declares its own auth, the credential inherited from the base
// route is removed first so it never reaches the override's host. The
// override's headers, its credential, and the overlay headers are protected
// from template headers.
func overrideRoute(base *APIConfig, ov HostOverride, inherited, auth AuthConfig, token *OAuthToken) APIConfig {
	route := APIConfig{Headers: map[string]string{}}
	if base != nil {
		for k, v := range base.Headers {
			route.Headers[k] = v
		}
	}
	if ov.Auth != nil {
		deleteHeader(route.Headers, "Authorization")
		if inherited.Type == "apikey" && inherited.HeaderName != "" {
			deleteHeader(route.Headers, inherited.HeaderName)
		}
	}
	route.addProtected(ov.Headers)
	if name, value, ok := credentialHeader(auth, token); ok {
		route.addProtected(map[string]string{name: value})
	}
	if base != nil {
		route.AddOverlayHeaders(base.Overlay)
	}
	return route
}

// BuildOverrideConfigsWithProvider is like BuildOverrideConfigsWithAuth but uses
// an AuthProvider for overrides that inherit the default auth, avoiding redundant
// token requests. Overrides with their own explicit Auth still call Authenticate
// directly.
func (env *Environment) BuildOverrideConfigsWithProvider(ctx context.Context, base *APIConfig, provider *AuthProvider) ([]ResolvedOverride, error) {
	if len(env.Overrides) == 0 {
		return nil, nil
	}

	inherited := provider.Config()
	resolved := make([]ResolvedOverride, 0, len(env.Overrides))
	for i, ov := range env.Overrides {
		var token *OAuthToken
		auth := inherited
		var err error

		if ov.Auth != nil {
			// Explicit auth on override — authenticate directly.
			auth = *ov.Auth
			token, err = Authenticate(ctx, auth)
		} else {
			// Inherit default auth via the cached provider.
			token, err = provider.Authenticate(ctx)
		}
		if err != nil {
			return nil, fmt.Errorf("authenticating override %d (%s): %w", i, ov.Match, err)
		}

		resolved = append(resolved, env.resolveOverride(ov, overrideRoute(base, ov, inherited, auth, token)))
	}

	return resolved, nil
}

// CollectSecrets resolves all SecretRef values from the environment and returns
// them as a set. This is used for value-matching redaction in archives.
// Resolution errors are silently ignored (missing env var = no secret to redact).
func (env *Environment) CollectSecrets() map[string]bool {
	secrets := make(map[string]bool)

	// Auth credentials, including those of host overrides
	for k := range CollectAuthSecrets(&env.Auth) {
		secrets[k] = true
	}
	for _, ov := range env.Overrides {
		for k := range CollectAuthSecrets(ov.Auth) {
			secrets[k] = true
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

// CollectAuthSecrets resolves the secret credentials of an AuthConfig and
// returns them as a set, for redaction in archives. The oauth2 username and
// clientId identify an account rather than prove it, so they are left out:
// redacting them would mangle ordinary data, such as the shop sandbox's "demo"
// user in demo@example.com. Every other credential counts. Resolution errors
// are silently ignored.
func CollectAuthSecrets(auth *AuthConfig) map[string]bool {
	secrets := make(map[string]bool)
	if auth == nil {
		return secrets
	}
	for name, ref := range auth.Credentials {
		if identifierCredentials[name] {
			continue
		}
		if val, err := ref.Resolve(); err == nil && val != "" {
			secrets[val] = true
		}
	}
	return secrets
}

// identifierCredentials names the credentials that identify rather than
// authenticate; see CollectAuthSecrets.
var identifierCredentials = map[string]bool{"username": true, "clientId": true}

// RunSecrets collects every secret a run can send, for redaction in its
// archive: the environment's (its auth, its host overrides' auth, and the LLM
// API key), the plan's auth, and those of the overlays the run used. Nil
// overlays are skipped.
func RunSecrets(env *Environment, planAuth *AuthConfig, overlays ...*OverlayFile) map[string]bool {
	secrets := env.CollectSecrets()
	for k := range CollectAuthSecrets(planAuth) {
		secrets[k] = true
	}
	for _, overlay := range overlays {
		if overlay == nil {
			continue
		}
		for k := range overlay.CollectSecrets() {
			secrets[k] = true
		}
	}
	return secrets
}

// EnvironmentPartial is a sparse environment definition used in multi-environment
// files. Pointer fields distinguish "not set" from "set to zero value" during merge.
type EnvironmentPartial struct {
	APIBaseURL string            `yaml:"apiBaseUrl,omitempty"`
	Auth       *AuthConfig       `yaml:"auth,omitempty"`
	Headers    map[string]string `yaml:"headers,omitempty"`
	LLM        *LLMConfig        `yaml:"llm,omitempty"`
	Settings   *RuntimeSettings  `yaml:"settings,omitempty"`
	Notes      string            `yaml:"notes,omitempty"`
	Overrides  []HostOverride    `yaml:"overrides,omitempty"`
	Values     map[string]string `yaml:"values,omitempty"`
	Vars       map[string]string `yaml:"vars,omitempty"`
	Extends    string            `yaml:"extends,omitempty"`
}

// MultiEnvironmentFile is the top-level structure for multi-environment YAML files.
type MultiEnvironmentFile struct {
	Include      []string                      `yaml:"include,omitempty"` // paths to merge (relative to this file)
	Shared       EnvironmentPartial            `yaml:"shared"`
	Environments map[string]EnvironmentPartial `yaml:"environments"`
}

// APIConfig is the base URL and headers of one route (the default route, or an
// override's), for bridging to adapter.EnvironmentConfig.
type APIConfig struct {
	BaseURL string
	// Headers is every header the route sends before a request template adds
	// its own.
	Headers map[string]string
	// Protected is the part of Headers a request template may not replace: the
	// credential, an override's own headers, and overlay headers.
	Protected map[string]string
	// Overlay is the part of Headers that .aat-overrides.yaml and --overlay set.
	// The routes of overrides apply it last, as the default route does.
	Overlay map[string]string
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
	return env.BuildAPIConfigFromToken(token, auth, extraHeaders), nil
}

// BuildAPIConfigFromToken builds a flat APIConfig from a pre-obtained token.
// This is a pure function — no network calls, no context needed.
// Header merge order: env headers → extraHeaders → auth headers.
// extraHeaders may be nil. token may be nil (e.g., auth type "none").
func (env *Environment) BuildAPIConfigFromToken(token *OAuthToken, auth AuthConfig, extraHeaders map[string]string) *APIConfig {
	cfg := &APIConfig{BaseURL: env.APIBaseURL, Headers: make(map[string]string)}

	// 1. Environment static headers (base)
	for k, v := range env.Headers {
		cfg.Headers = withHeader(cfg.Headers, k, v)
	}

	// 2. Extra headers (e.g., plan-level headers) override env headers
	for k, v := range extraHeaders {
		cfg.Headers = withHeader(cfg.Headers, k, v)
	}

	// 3. The credential overrides both, and request templates cannot replace it
	if name, value, ok := credentialHeader(auth, token); ok {
		cfg.addProtected(map[string]string{name: value})
	}

	return cfg
}
