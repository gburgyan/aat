package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Loading tests ---

func TestLoadEnvironment_Full(t *testing.T) {
	env, err := LoadEnvironment("testdata/environments/test.yaml")
	require.NoError(t, err)

	assert.Equal(t, "test", env.Name)
	assert.Equal(t, "https://api.example.com", env.APIBaseURL)
	assert.Equal(t, "oauth2", env.Auth.Type)
	assert.Equal(t, "https://auth.example.com/oauth/token", env.Auth.TokenURL)
	assert.Equal(t, "literal", env.Auth.Credentials["username"].Source)
	assert.Equal(t, "testuser", env.Auth.Credentials["username"].Value)
	assert.Equal(t, "https://llm.example.com/v1", env.LLM.Endpoint)
	assert.Equal(t, "gpt-4", env.LLM.Model)
	assert.Equal(t, 5*time.Minute, env.Settings.MaxRunDuration.Duration)
	assert.Equal(t, 3, env.Settings.DefaultRetries)
	assert.Equal(t, ArchiveJSONGZ, env.Settings.ArchiveFormat)
	assert.Equal(t, "Full test environment with all fields populated.", env.Notes)
}

func TestLoadEnvironment_Minimal(t *testing.T) {
	env, err := LoadEnvironment("testdata/environments/minimal.yaml")
	require.NoError(t, err)

	assert.Equal(t, "minimal", env.Name)
	assert.Equal(t, "https://api.example.com", env.APIBaseURL)
	assert.Equal(t, "none", env.Auth.Type)

	// Verify defaults applied
	assert.Equal(t, 120*time.Second, env.Settings.MaxRunDuration.Duration)
	assert.Equal(t, 2, env.Settings.DefaultRetries)
	assert.Equal(t, ArchiveJSON, env.Settings.ArchiveFormat)
}

func TestLoadEnvironment_MissingFile(t *testing.T) {
	_, err := LoadEnvironment("testdata/environments/nonexistent.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading environment file")
}

func TestLoadEnvironment_MalformedYAML(t *testing.T) {
	tmpFile := t.TempDir() + "/bad.yaml"
	require.NoError(t, os.WriteFile(tmpFile, []byte(":\n  :\n    [invalid"), 0644))

	_, err := LoadEnvironment(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing environment YAML")
}

func TestLoadEnvironment_MissingName(t *testing.T) {
	_, err := LoadEnvironment("testdata/environments/invalid/missing_name.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "environment name is required")
}

func TestLoadEnvironment_MissingAPIBaseURL(t *testing.T) {
	tmpFile := t.TempDir() + "/no_url.yaml"
	require.NoError(t, os.WriteFile(tmpFile, []byte("environment: test\nauth:\n  type: none\n"), 0644))

	_, err := LoadEnvironment(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiBaseUrl is required")
}

func TestLoadEnvironmentFromDir(t *testing.T) {
	env, err := LoadEnvironmentFromDir("testdata/environments", "test")
	require.NoError(t, err)
	assert.Equal(t, "test", env.Name)
}

func TestLoadEnvironmentFromDir_NotFound(t *testing.T) {
	_, err := LoadEnvironmentFromDir("testdata/environments", "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading environment file")
}

// --- Validation tests ---

func TestValidateEnvironment_InvalidAuthType(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth:       AuthConfig{Type: "magic"},
		LLM:        LLMConfig{},
		Settings:   RuntimeSettings{ArchiveFormat: ArchiveJSON},
	}
	err := ValidateEnvironment(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth type")
}

func TestValidateEnvironment_OAuth2MissingTokenURL(t *testing.T) {
	_, err := LoadEnvironment("testdata/environments/invalid/oauth2_no_token_url.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth.tokenUrl is required")
}

func TestValidateEnvironment_OAuth2MissingCredentials(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type:     "oauth2",
			TokenURL: "https://auth.example.com/token",
			// no credentials
		},
		LLM:      LLMConfig{},
		Settings: RuntimeSettings{ArchiveFormat: ArchiveJSON},
	}
	err := ValidateEnvironment(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth.credentials.username")
	assert.Contains(t, err.Error(), "auth.credentials.password")
	assert.Contains(t, err.Error(), "auth.credentials.clientId")
	assert.Contains(t, err.Error(), "auth.credentials.clientSecret")
}

func TestValidateEnvironment_APIKeyMissingKey(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth:       AuthConfig{Type: "apikey", HeaderName: "X-Api-Key"},
		LLM:        LLMConfig{},
		Settings:   RuntimeSettings{ArchiveFormat: ArchiveJSON},
	}
	err := ValidateEnvironment(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth.credentials.key")
}

func TestValidateEnvironment_APIKeyMissingHeaderName(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type:        "apikey",
			Credentials: map[string]SecretRef{"key": {Source: "literal", Value: "abc"}},
		},
		LLM:      LLMConfig{},
		Settings: RuntimeSettings{ArchiveFormat: ArchiveJSON},
	}
	err := ValidateEnvironment(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth.headerName is required")
}

func TestValidateEnvironment_InvalidArchiveFormat(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth:       AuthConfig{Type: "none"},
		LLM:        LLMConfig{},
		Settings:   RuntimeSettings{ArchiveFormat: "xml"},
	}
	err := ValidateEnvironment(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown archiveFormat")
}

// --- SecretRef tests ---

func TestSecretRef_ResolveEnv(t *testing.T) {
	t.Setenv("AAT_TEST_SECRET", "my-secret-value")
	ref := SecretRef{Source: "env", Var: "AAT_TEST_SECRET"}
	val, err := ref.Resolve()
	require.NoError(t, err)
	assert.Equal(t, "my-secret-value", val)
}

func TestSecretRef_ResolveEnvNotSet(t *testing.T) {
	ref := SecretRef{Source: "env", Var: "AAT_DEFINITELY_NOT_SET_12345"}
	_, err := ref.Resolve()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not set")
}

func TestSecretRef_ResolveLiteral(t *testing.T) {
	ref := SecretRef{Source: "literal", Value: "plain-value"}
	val, err := ref.Resolve()
	require.NoError(t, err)
	assert.Equal(t, "plain-value", val)
}

func TestSecretRef_ResolveUnknownSource(t *testing.T) {
	ref := SecretRef{Source: "vault"}
	_, err := ref.Resolve()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown secret source")
}

func TestSecretRef_IsSet(t *testing.T) {
	assert.True(t, SecretRef{Source: "env", Var: "X"}.IsSet())
	assert.True(t, SecretRef{Source: "literal", Value: "x"}.IsSet())
	assert.False(t, SecretRef{}.IsSet())
}

// --- Duration tests ---

func TestDuration_Parse(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"120s", 120 * time.Second},
		{"5m", 5 * time.Minute},
		{"2h30m", 2*time.Hour + 30*time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			yaml := "environment: test\napiBaseUrl: https://x\nauth:\n  type: none\nsettings:\n  maxRunDuration: " + tt.input + "\n"
			tmpFile := t.TempDir() + "/dur.yaml"
			require.NoError(t, os.WriteFile(tmpFile, []byte(yaml), 0644))

			env, err := LoadEnvironment(tmpFile)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, env.Settings.MaxRunDuration.Duration)
		})
	}
}

func TestDuration_RejectBareInteger(t *testing.T) {
	yaml := "environment: test\napiBaseUrl: https://x\nauth:\n  type: none\nsettings:\n  maxRunDuration: 120\n"
	tmpFile := t.TempDir() + "/dur.yaml"
	require.NoError(t, os.WriteFile(tmpFile, []byte(yaml), 0644))

	_, err := LoadEnvironment(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid duration")
}

// --- Auth tests ---

func TestAuthenticate_OAuth2Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

		require.NoError(t, r.ParseForm())
		assert.Equal(t, "password", r.FormValue("grant_type"))
		assert.Equal(t, "testuser", r.FormValue("username"))
		assert.Equal(t, "testpass", r.FormValue("password"))
		assert.Equal(t, "test-client-id", r.FormValue("client_id"))
		assert.Equal(t, "test-client-secret", r.FormValue("client_secret"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(OAuthToken{
			AccessToken: "test-token-123",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		})
	}))
	defer server.Close()

	auth := AuthConfig{
		Type:     "oauth2",
		TokenURL: server.URL,
		Credentials: map[string]SecretRef{
			"username":     {Source: "literal", Value: "testuser"},
			"password":     {Source: "literal", Value: "testpass"},
			"clientId":     {Source: "literal", Value: "test-client-id"},
			"clientSecret": {Source: "literal", Value: "test-client-secret"},
		},
	}

	token, err := Authenticate(context.Background(), auth)
	require.NoError(t, err)
	assert.Equal(t, "test-token-123", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
	assert.Equal(t, 3600, token.ExpiresIn)
}

func TestAuthenticate_OAuth2HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "invalid_client"}`))
	}))
	defer server.Close()

	auth := AuthConfig{
		Type:     "oauth2",
		TokenURL: server.URL,
		Credentials: map[string]SecretRef{
			"username":     {Source: "literal", Value: "bad"},
			"password":     {Source: "literal", Value: "creds"},
			"clientId":     {Source: "literal", Value: "x"},
			"clientSecret": {Source: "literal", Value: "x"},
		},
	}

	_, err := Authenticate(context.Background(), auth)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 401")
}

func TestAuthenticate_OAuth2MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	auth := AuthConfig{
		Type:     "oauth2",
		TokenURL: server.URL,
		Credentials: map[string]SecretRef{
			"username":     {Source: "literal", Value: "u"},
			"password":     {Source: "literal", Value: "p"},
			"clientId":     {Source: "literal", Value: "c"},
			"clientSecret": {Source: "literal", Value: "s"},
		},
	}

	_, err := Authenticate(context.Background(), auth)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing auth response")
}

func TestAuthenticate_OAuth2MissingAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token_type": "Bearer",
			"expires_in": 3600,
		})
	}))
	defer server.Close()

	auth := AuthConfig{
		Type:     "oauth2",
		TokenURL: server.URL,
		Credentials: map[string]SecretRef{
			"username":     {Source: "literal", Value: "u"},
			"password":     {Source: "literal", Value: "p"},
			"clientId":     {Source: "literal", Value: "c"},
			"clientSecret": {Source: "literal", Value: "s"},
		},
	}

	_, err := Authenticate(context.Background(), auth)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing access_token")
}

func TestAuthenticate_BearerPassthrough(t *testing.T) {
	auth := AuthConfig{
		Type: "bearer",
		Credentials: map[string]SecretRef{
			"token": {Source: "literal", Value: "my-bearer-token"},
		},
	}

	token, err := Authenticate(context.Background(), auth)
	require.NoError(t, err)
	assert.Equal(t, "my-bearer-token", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
}

func TestAuthenticate_APIKeyPassthrough(t *testing.T) {
	auth := AuthConfig{
		Type:       "apikey",
		HeaderName: "X-Api-Key",
		Credentials: map[string]SecretRef{
			"key": {Source: "literal", Value: "my-api-key"},
		},
	}

	token, err := Authenticate(context.Background(), auth)
	require.NoError(t, err)
	assert.Equal(t, "my-api-key", token.AccessToken)
	assert.Equal(t, "apikey", token.TokenType)
}

func TestAuthenticate_NoneReturnsNil(t *testing.T) {
	auth := AuthConfig{Type: "none"}
	token, err := Authenticate(context.Background(), auth)
	require.NoError(t, err)
	assert.Nil(t, token)
}

// --- BuildAPIConfig test ---

func TestBuildAPIConfig_OAuth2(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(OAuthToken{
			AccessToken: "built-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		})
	}))
	defer server.Close()

	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type:     "oauth2",
			TokenURL: server.URL,
			Credentials: map[string]SecretRef{
				"username":     {Source: "literal", Value: "u"},
				"password":     {Source: "literal", Value: "p"},
				"clientId":     {Source: "literal", Value: "c"},
				"clientSecret": {Source: "literal", Value: "s"},
			},
		},
	}

	cfg, err := env.BuildAPIConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com", cfg.BaseURL)
	assert.Equal(t, "Bearer built-token", cfg.Headers["Authorization"])
	assert.NotNil(t, cfg.Values)
}

func TestBuildAPIConfig_APIKey(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type:       "apikey",
			HeaderName: "X-Api-Key",
			Credentials: map[string]SecretRef{
				"key": {Source: "literal", Value: "secret-key"},
			},
		},
	}

	cfg, err := env.BuildAPIConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "secret-key", cfg.Headers["X-Api-Key"])
	_, hasAuth := cfg.Headers["Authorization"]
	assert.False(t, hasAuth)
}

func TestBuildAPIConfig_CustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(OAuthToken{
			AccessToken: "test-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		})
	}))
	defer server.Close()

	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type:     "oauth2",
			TokenURL: server.URL,
			Credentials: map[string]SecretRef{
				"username":     {Source: "literal", Value: "u"},
				"password":     {Source: "literal", Value: "p"},
				"clientId":     {Source: "literal", Value: "c"},
				"clientSecret": {Source: "literal", Value: "s"},
			},
		},
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
			"X-Access-Group":  "group-123",
		},
	}

	cfg, err := env.BuildAPIConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "custom-value", cfg.Headers["X-Custom-Header"])
	assert.Equal(t, "group-123", cfg.Headers["X-Access-Group"])
	assert.Equal(t, "Bearer test-token", cfg.Headers["Authorization"])
}

func TestBuildAPIConfig_CustomHeadersNoAuth(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth:       AuthConfig{Type: "none"},
		Headers: map[string]string{
			"X-Custom": "value",
		},
	}

	cfg, err := env.BuildAPIConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "value", cfg.Headers["X-Custom"])
	assert.Len(t, cfg.Headers, 1)
}

func TestBuildAPIConfig_None(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth:       AuthConfig{Type: "none"},
	}

	cfg, err := env.BuildAPIConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com", cfg.BaseURL)
	assert.Empty(t, cfg.Headers)
}

func TestCollectSecrets_WithEnvVars(t *testing.T) {
	t.Setenv("TEST_SECRET_1", "secret-value-1")
	t.Setenv("TEST_SECRET_2", "secret-value-2")

	env := &Environment{
		Auth: AuthConfig{
			Credentials: map[string]SecretRef{
				"clientId":     {Source: "env", Var: "TEST_SECRET_1"},
				"clientSecret": {Source: "env", Var: "TEST_SECRET_2"},
			},
		},
		LLM: LLMConfig{
			APIKey: SecretRef{Source: "literal", Value: "literal-key"},
		},
	}

	secrets := env.CollectSecrets()
	assert.True(t, secrets["secret-value-1"])
	assert.True(t, secrets["secret-value-2"])
	assert.True(t, secrets["literal-key"])
	assert.Len(t, secrets, 3)
}

func TestCollectSecrets_EmptyEnvironment(t *testing.T) {
	env := &Environment{}
	secrets := env.CollectSecrets()
	assert.Empty(t, secrets)
}

func TestCollectSecrets_UnresolvableRefsIgnored(t *testing.T) {
	env := &Environment{
		Auth: AuthConfig{
			Credentials: map[string]SecretRef{
				"missing": {Source: "env", Var: "SURELY_NOT_SET_12345"},
			},
		},
	}

	secrets := env.CollectSecrets()
	assert.Empty(t, secrets)
}

// --- Override tests ---

func TestOverrides_ParseFromYAML(t *testing.T) {
	yaml := `
environment: test
apiBaseUrl: https://api.example.com
auth:
  type: none
overrides:
  - match: searchFlights
    baseUrl: http://localhost:8080
    auth:
      type: none
    pathRewrite:
      strip: /11
      prefix: /api/v2
  - match: "price*"
    baseUrl: https://api.stg.example.com
`
	tmpFile := t.TempDir() + "/overrides.yaml"
	require.NoError(t, os.WriteFile(tmpFile, []byte(yaml), 0644))

	env, err := LoadEnvironment(tmpFile)
	require.NoError(t, err)
	require.Len(t, env.Overrides, 2)

	assert.Equal(t, "searchFlights", env.Overrides[0].Match)
	assert.Equal(t, "http://localhost:8080", env.Overrides[0].BaseURL)
	require.NotNil(t, env.Overrides[0].Auth)
	assert.Equal(t, "none", env.Overrides[0].Auth.Type)
	require.NotNil(t, env.Overrides[0].PathRewrite)
	assert.Equal(t, "/11", env.Overrides[0].PathRewrite.Strip)
	assert.Equal(t, "/api/v2", env.Overrides[0].PathRewrite.Prefix)

	assert.Equal(t, "price*", env.Overrides[1].Match)
	assert.Equal(t, "https://api.stg.example.com", env.Overrides[1].BaseURL)
	assert.Nil(t, env.Overrides[1].Auth)
}

func TestOverrides_Validation_EmptyMatch(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth:       AuthConfig{Type: "none"},
		Overrides: []HostOverride{
			{Match: ""}, // invalid
		},
	}
	err := ValidateEnvironment(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "match is required")
}

func TestOverrides_Validation_InvalidAuth(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth:       AuthConfig{Type: "none"},
		Overrides: []HostOverride{
			{Match: "node1", Auth: &AuthConfig{Type: "magic"}},
		},
	}
	err := ValidateEnvironment(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth type")
}

func TestBuildOverrideConfigs_InheritAuth(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type: "bearer",
			Credentials: map[string]SecretRef{
				"token": {Source: "literal", Value: "my-token"},
			},
		},
		Overrides: []HostOverride{
			{Match: "node1", BaseURL: "http://localhost:8080"},
			// Auth omitted → inherits top-level bearer auth
		},
	}

	baseHeaders := map[string]string{"Accept": "application/json"}
	resolved, err := env.BuildOverrideConfigs(context.Background(), baseHeaders)
	require.NoError(t, err)
	require.Len(t, resolved, 1)

	assert.Equal(t, "node1", resolved[0].Pattern)
	assert.Equal(t, "http://localhost:8080", resolved[0].APIConfig.BaseURL)
	assert.Equal(t, "Bearer my-token", resolved[0].APIConfig.Headers["Authorization"])
	assert.Equal(t, "application/json", resolved[0].APIConfig.Headers["Accept"])
}

func TestBuildOverrideConfigs_ExplicitNone(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type: "bearer",
			Credentials: map[string]SecretRef{
				"token": {Source: "literal", Value: "my-token"},
			},
		},
		Overrides: []HostOverride{
			{Match: "node1", BaseURL: "http://localhost:8080", Auth: &AuthConfig{Type: "none"}},
		},
	}

	baseHeaders := map[string]string{
		"Accept":        "application/json",
		"Authorization": "Bearer my-token",
	}
	resolved, err := env.BuildOverrideConfigs(context.Background(), baseHeaders)
	require.NoError(t, err)
	require.Len(t, resolved, 1)

	// Auth should be stripped
	_, hasAuth := resolved[0].APIConfig.Headers["Authorization"]
	assert.False(t, hasAuth)
	assert.Equal(t, "application/json", resolved[0].APIConfig.Headers["Accept"])
}

func TestBuildOverrideConfigs_HeaderMerge(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth:       AuthConfig{Type: "none"},
		Overrides: []HostOverride{
			{
				Match:   "node1",
				BaseURL: "http://localhost:8080",
				Headers: map[string]string{
					"X-Custom": "override-value",
					"X-New":    "new-value",
				},
			},
		},
	}

	baseHeaders := map[string]string{
		"Accept":   "application/json",
		"X-Custom": "base-value",
	}
	resolved, err := env.BuildOverrideConfigs(context.Background(), baseHeaders)
	require.NoError(t, err)
	require.Len(t, resolved, 1)

	// Override headers win on conflict
	assert.Equal(t, "override-value", resolved[0].APIConfig.Headers["X-Custom"])
	assert.Equal(t, "new-value", resolved[0].APIConfig.Headers["X-New"])
	assert.Equal(t, "application/json", resolved[0].APIConfig.Headers["Accept"])
}

func TestBuildOverrideConfigs_InheritBaseURL(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth:       AuthConfig{Type: "none"},
		Overrides: []HostOverride{
			{Match: "node1"}, // no baseUrl → inherits top-level
		},
	}

	resolved, err := env.BuildOverrideConfigs(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, "https://api.example.com", resolved[0].APIConfig.BaseURL)
}

func TestBuildOverrideConfigs_PathRewrite(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth:       AuthConfig{Type: "none"},
		Overrides: []HostOverride{
			{
				Match:   "node1",
				BaseURL: "http://localhost:8080",
				PathRewrite: &PathRewrite{
					Strip:  "/11",
					Prefix: "/api/v2",
				},
			},
		},
	}

	resolved, err := env.BuildOverrideConfigs(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	require.NotNil(t, resolved[0].PathRewrite)
	assert.Equal(t, "/11", resolved[0].PathRewrite.Strip)
	assert.Equal(t, "/api/v2", resolved[0].PathRewrite.Prefix)
}

func TestBuildOverrideConfigs_Empty(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth:       AuthConfig{Type: "none"},
	}

	resolved, err := env.BuildOverrideConfigs(context.Background(), nil)
	require.NoError(t, err)
	assert.Nil(t, resolved)
}

func TestLoadOverlayFile(t *testing.T) {
	yaml := `
overrides:
  - match: searchFlights
    baseUrl: http://localhost:8080
    auth:
      type: none
    pathRewrite:
      strip: /11
      prefix: /api/v2
`
	tmpFile := t.TempDir() + "/overlay.yaml"
	require.NoError(t, os.WriteFile(tmpFile, []byte(yaml), 0644))

	overrides, err := LoadOverlayFile(tmpFile)
	require.NoError(t, err)
	require.Len(t, overrides, 1)
	assert.Equal(t, "searchFlights", overrides[0].Match)
	assert.Equal(t, "http://localhost:8080", overrides[0].BaseURL)
}

func TestLoadOverlayFile_EmptyMatch(t *testing.T) {
	yaml := `
overrides:
  - baseUrl: http://localhost:8080
`
	tmpFile := t.TempDir() + "/overlay.yaml"
	require.NoError(t, os.WriteFile(tmpFile, []byte(yaml), 0644))

	_, err := LoadOverlayFile(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "match is required")
}

func TestMergeOverrides(t *testing.T) {
	base := []HostOverride{
		{Match: "node1", BaseURL: "http://base:8080"},
	}
	overlay := []HostOverride{
		{Match: "node2", BaseURL: "http://overlay:9090"},
	}

	merged := MergeOverrides(base, overlay)
	require.Len(t, merged, 2)
	assert.Equal(t, "node1", merged[0].Match)
	assert.Equal(t, "node2", merged[1].Match)
}

func TestMergeOverrides_EmptyOverlay(t *testing.T) {
	base := []HostOverride{
		{Match: "node1", BaseURL: "http://base:8080"},
	}

	merged := MergeOverrides(base, nil)
	assert.Equal(t, base, merged)
}

func TestOverrides_BackwardsCompatible(t *testing.T) {
	// Ensure env files without overrides still parse correctly
	env, err := LoadEnvironment("testdata/environments/minimal.yaml")
	require.NoError(t, err)
	assert.Empty(t, env.Overrides)
}

// --- ValidateAuth (exported) tests ---

func TestValidateAuth_Exported(t *testing.T) {
	// Verify the exported function works the same as the old unexported one
	auth := &AuthConfig{Type: "oauth2", TokenURL: "https://auth.example.com/token",
		Credentials: map[string]SecretRef{
			"username":     {Source: "literal", Value: "u"},
			"password":     {Source: "literal", Value: "p"},
			"clientId":     {Source: "literal", Value: "c"},
			"clientSecret": {Source: "literal", Value: "s"},
		},
	}
	errs := ValidateAuth(auth)
	assert.Empty(t, errs)
}

func TestValidateAuth_Exported_Invalid(t *testing.T) {
	auth := &AuthConfig{Type: "oauth2"} // missing tokenUrl and credentials
	errs := ValidateAuth(auth)
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if e == "auth.tokenUrl is required for oauth2" {
			found = true
		}
	}
	assert.True(t, found, "expected tokenUrl error")
}

// --- BuildAPIConfigFromAuth tests ---

func TestBuildAPIConfigFromAuth_DifferentAuth(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth:       AuthConfig{Type: "none"},
		Headers:    map[string]string{"X-Env": "env-value"},
	}

	planAuth := AuthConfig{
		Type: "bearer",
		Credentials: map[string]SecretRef{
			"token": {Source: "literal", Value: "plan-token"},
		},
	}
	extraHeaders := map[string]string{"X-Plan": "plan-value"}

	cfg, err := env.BuildAPIConfigFromAuth(context.Background(), planAuth, extraHeaders)
	require.NoError(t, err)
	assert.Equal(t, "Bearer plan-token", cfg.Headers["Authorization"])
	assert.Equal(t, "env-value", cfg.Headers["X-Env"])
	assert.Equal(t, "plan-value", cfg.Headers["X-Plan"])
}

func TestBuildAPIConfigFromAuth_NilExtraHeaders(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth:       AuthConfig{Type: "none"},
		Headers:    map[string]string{"X-Env": "env-value"},
	}

	cfg, err := env.BuildAPIConfigFromAuth(context.Background(), AuthConfig{Type: "none"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "env-value", cfg.Headers["X-Env"])
	assert.Len(t, cfg.Headers, 1)
}

func TestBuildAPIConfigFromAuth_PlanHeadersOverrideEnv(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth:       AuthConfig{Type: "none"},
		Headers:    map[string]string{"X-Shared": "env-value", "X-Env-Only": "stays"},
	}

	extraHeaders := map[string]string{"X-Shared": "plan-value", "X-Plan-Only": "new"}

	cfg, err := env.BuildAPIConfigFromAuth(context.Background(), AuthConfig{Type: "none"}, extraHeaders)
	require.NoError(t, err)
	assert.Equal(t, "plan-value", cfg.Headers["X-Shared"])
	assert.Equal(t, "stays", cfg.Headers["X-Env-Only"])
	assert.Equal(t, "new", cfg.Headers["X-Plan-Only"])
}

func TestBuildAPIConfigFromAuth_DelegateFromBuildAPIConfig(t *testing.T) {
	// Verify BuildAPIConfig still works (delegates to BuildAPIConfigFromAuth)
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type: "bearer",
			Credentials: map[string]SecretRef{
				"token": {Source: "literal", Value: "env-token"},
			},
		},
		Headers: map[string]string{"X-Env": "val"},
	}

	cfg, err := env.BuildAPIConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Bearer env-token", cfg.Headers["Authorization"])
	assert.Equal(t, "val", cfg.Headers["X-Env"])
}

// --- BuildOverrideConfigsWithAuth tests ---

func TestBuildOverrideConfigsWithAuth_InheritProvidedAuth(t *testing.T) {
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type: "bearer",
			Credentials: map[string]SecretRef{
				"token": {Source: "literal", Value: "env-token"},
			},
		},
		Overrides: []HostOverride{
			{Match: "node1", BaseURL: "http://localhost:8080"},
		},
	}

	// Override with a different default auth
	planAuth := AuthConfig{
		Type: "bearer",
		Credentials: map[string]SecretRef{
			"token": {Source: "literal", Value: "plan-token"},
		},
	}

	resolved, err := env.BuildOverrideConfigsWithAuth(context.Background(), nil, planAuth)
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	// Override inherits plan auth, not env auth
	assert.Equal(t, "Bearer plan-token", resolved[0].APIConfig.Headers["Authorization"])
}

func TestBuildOverrideConfigsWithAuth_DelegateFromBuildOverrideConfigs(t *testing.T) {
	// Verify BuildOverrideConfigs delegates correctly
	env := &Environment{
		Name:       "test",
		APIBaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type: "bearer",
			Credentials: map[string]SecretRef{
				"token": {Source: "literal", Value: "env-token"},
			},
		},
		Overrides: []HostOverride{
			{Match: "node1"},
		},
	}

	resolved, err := env.BuildOverrideConfigs(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, "Bearer env-token", resolved[0].APIConfig.Headers["Authorization"])
}

// --- CollectAuthSecrets tests ---

func TestCollectAuthSecrets(t *testing.T) {
	auth := &AuthConfig{
		Credentials: map[string]SecretRef{
			"username": {Source: "literal", Value: "user-val"},
			"password": {Source: "literal", Value: "pass-val"},
		},
	}

	secrets := CollectAuthSecrets(auth)
	assert.True(t, secrets["user-val"])
	assert.True(t, secrets["pass-val"])
	assert.Len(t, secrets, 2)
}

func TestCollectAuthSecrets_Nil(t *testing.T) {
	secrets := CollectAuthSecrets(nil)
	assert.Empty(t, secrets)
}

func TestCollectAuthSecrets_UnresolvableIgnored(t *testing.T) {
	auth := &AuthConfig{
		Credentials: map[string]SecretRef{
			"key": {Source: "env", Var: "DEFINITELY_NOT_SET_98765"},
		},
	}

	secrets := CollectAuthSecrets(auth)
	assert.Empty(t, secrets)
}

// --- BuildAPIConfigFromToken tests ---

func TestBuildAPIConfigFromToken_Bearer(t *testing.T) {
	env := &Environment{
		APIBaseURL: "https://api.example.com",
		Headers:    map[string]string{"X-Env": "env-val"},
	}
	token := &OAuthToken{AccessToken: "my-token", TokenType: "Bearer"}
	auth := AuthConfig{Type: "bearer"}

	cfg := env.BuildAPIConfigFromToken(token, auth, map[string]string{"X-Plan": "plan-val"})
	assert.Equal(t, "https://api.example.com", cfg.BaseURL)
	assert.Equal(t, "Bearer my-token", cfg.Headers["Authorization"])
	assert.Equal(t, "env-val", cfg.Headers["X-Env"])
	assert.Equal(t, "plan-val", cfg.Headers["X-Plan"])
	assert.NotNil(t, cfg.Values)
}

func TestBuildAPIConfigFromToken_APIKey(t *testing.T) {
	env := &Environment{
		APIBaseURL: "https://api.example.com",
	}
	token := &OAuthToken{AccessToken: "key-123", TokenType: "apikey"}
	auth := AuthConfig{Type: "apikey", HeaderName: "X-Api-Key"}

	cfg := env.BuildAPIConfigFromToken(token, auth, nil)
	assert.Equal(t, "key-123", cfg.Headers["X-Api-Key"])
	_, hasAuth := cfg.Headers["Authorization"]
	assert.False(t, hasAuth)
}

func TestBuildAPIConfigFromToken_NilToken(t *testing.T) {
	env := &Environment{
		APIBaseURL: "https://api.example.com",
		Headers:    map[string]string{"X-Env": "val"},
	}

	cfg := env.BuildAPIConfigFromToken(nil, AuthConfig{Type: "none"}, nil)
	assert.Equal(t, "val", cfg.Headers["X-Env"])
	assert.Len(t, cfg.Headers, 1)
}

// --- BuildOverrideConfigsWithProvider tests ---

func TestBuildOverrideConfigsWithProvider_InheritCached(t *testing.T) {
	env := &Environment{
		APIBaseURL: "https://api.example.com",
		Overrides: []HostOverride{
			{Match: "node1", BaseURL: "http://localhost:8080"},
			{Match: "node2"}, // inherits baseURL too
		},
	}

	provider := NewAuthProvider(AuthConfig{
		Type: "bearer",
		Credentials: map[string]SecretRef{
			"token": {Source: "literal", Value: "cached-tok"},
		},
	})

	resolved, err := env.BuildOverrideConfigsWithProvider(context.Background(), map[string]string{"Accept": "application/json"}, provider)
	require.NoError(t, err)
	require.Len(t, resolved, 2)
	assert.Equal(t, "Bearer cached-tok", resolved[0].APIConfig.Headers["Authorization"])
	assert.Equal(t, "Bearer cached-tok", resolved[1].APIConfig.Headers["Authorization"])
	assert.Equal(t, "application/json", resolved[0].APIConfig.Headers["Accept"])
	assert.Equal(t, "https://api.example.com", resolved[1].APIConfig.BaseURL)
}

func TestBuildOverrideConfigsWithProvider_ExplicitAuthBypassesProvider(t *testing.T) {
	env := &Environment{
		APIBaseURL: "https://api.example.com",
		Overrides: []HostOverride{
			{
				Match:   "node1",
				BaseURL: "http://localhost:8080",
				Auth: &AuthConfig{
					Type: "bearer",
					Credentials: map[string]SecretRef{
						"token": {Source: "literal", Value: "override-tok"},
					},
				},
			},
		},
	}

	// Provider has a different token — should NOT be used for this override
	provider := NewAuthProvider(AuthConfig{
		Type: "bearer",
		Credentials: map[string]SecretRef{
			"token": {Source: "literal", Value: "provider-tok"},
		},
	})

	resolved, err := env.BuildOverrideConfigsWithProvider(context.Background(), nil, provider)
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, "Bearer override-tok", resolved[0].APIConfig.Headers["Authorization"])
}

func TestBuildOverrideConfigsWithProvider_Empty(t *testing.T) {
	env := &Environment{APIBaseURL: "https://api.example.com"}
	provider := NewAuthProvider(AuthConfig{Type: "none"})

	resolved, err := env.BuildOverrideConfigsWithProvider(context.Background(), nil, provider)
	require.NoError(t, err)
	assert.Nil(t, resolved)
}
