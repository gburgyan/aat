package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadNamedEnvironment_LegacyFormat(t *testing.T) {
	yaml := `
environment: test-env
apiBaseUrl: https://api.example.com
auth:
  type: none
llm:
  endpoint: https://api.openai.com/v1
  apiKey:
    source: literal
    value: test-key
  model: gpt-4
`
	path := writeTempYAML(t, yaml)

	// Legacy format with empty envName should work
	env, err := LoadNamedEnvironment(path, "")
	require.NoError(t, err)
	assert.Equal(t, "test-env", env.Name)
	assert.Equal(t, "https://api.example.com", env.APIBaseURL)

	// Legacy format with envName should error
	_, err = LoadNamedEnvironment(path, "something")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "single-environment format")
}

func TestLoadNamedEnvironment_MultiEnvBasic(t *testing.T) {
	yaml := `
shared:
  headers:
    Accept: application/json
  values:
    key1: val1

environments:
  dev:
    apiBaseUrl: https://dev.example.com
    auth:
      type: none
  prod:
    apiBaseUrl: https://prod.example.com
    auth:
      type: none
`
	path := writeTempYAML(t, yaml)

	// Load dev environment
	env, err := LoadNamedEnvironment(path, "dev")
	require.NoError(t, err)
	assert.Equal(t, "dev", env.Name)
	assert.Equal(t, "https://dev.example.com", env.APIBaseURL)
	assert.Equal(t, "application/json", env.Headers["Accept"])
	assert.Equal(t, "val1", env.Values["key1"])

	// Load prod environment
	env, err = LoadNamedEnvironment(path, "prod")
	require.NoError(t, err)
	assert.Equal(t, "prod", env.Name)
	assert.Equal(t, "https://prod.example.com", env.APIBaseURL)
	assert.Equal(t, "application/json", env.Headers["Accept"])
}

func TestLoadNamedEnvironment_MultiEnvNoName(t *testing.T) {
	yaml := `
environments:
  dev:
    apiBaseUrl: https://dev.example.com
    auth:
      type: none
  prod:
    apiBaseUrl: https://prod.example.com
    auth:
      type: none
`
	path := writeTempYAML(t, yaml)

	_, err := LoadNamedEnvironment(path, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple environments")
	assert.Contains(t, err.Error(), "dev")
	assert.Contains(t, err.Error(), "prod")
}

func TestLoadNamedEnvironment_NotFound(t *testing.T) {
	yaml := `
environments:
  dev:
    apiBaseUrl: https://dev.example.com
    auth:
      type: none
`
	path := writeTempYAML(t, yaml)

	_, err := LoadNamedEnvironment(path, "staging")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Contains(t, err.Error(), "dev")
}

func TestLoadNamedEnvironment_AbstractNotSelectable(t *testing.T) {
	yaml := `
environments:
  _base:
    apiBaseUrl: https://base.example.com
    auth:
      type: none
  dev:
    extends: _base
`
	path := writeTempYAML(t, yaml)

	_, err := LoadNamedEnvironment(path, "_base")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "abstract")
}

func TestLoadNamedEnvironment_Extends(t *testing.T) {
	yaml := `
environments:
  _base:
    apiBaseUrl: https://base.example.com
    auth:
      type: none
    headers:
      Accept: application/json
      X-Custom: base-val
    values:
      region: us-east

  dev:
    extends: _base
    headers:
      X-Custom: dev-val
    values:
      debug: "true"
`
	path := writeTempYAML(t, yaml)

	env, err := LoadNamedEnvironment(path, "dev")
	require.NoError(t, err)
	assert.Equal(t, "dev", env.Name)
	assert.Equal(t, "https://base.example.com", env.APIBaseURL)
	assert.Equal(t, "none", env.Auth.Type)
	assert.Equal(t, "application/json", env.Headers["Accept"]) // inherited
	assert.Equal(t, "dev-val", env.Headers["X-Custom"])        // overridden
	assert.Equal(t, "us-east", env.Values["region"])           // inherited
	assert.Equal(t, "true", env.Values["debug"])               // added
}

func TestLoadNamedEnvironment_ExtendsChain(t *testing.T) {
	yaml := `
environments:
  _root:
    auth:
      type: none
    headers:
      Accept: application/json

  _middle:
    extends: _root
    headers:
      X-Layer: middle
    values:
      from: middle

  leaf:
    extends: _middle
    apiBaseUrl: https://leaf.example.com
    values:
      from: leaf
`
	path := writeTempYAML(t, yaml)

	env, err := LoadNamedEnvironment(path, "leaf")
	require.NoError(t, err)
	assert.Equal(t, "https://leaf.example.com", env.APIBaseURL)
	assert.Equal(t, "none", env.Auth.Type)
	assert.Equal(t, "application/json", env.Headers["Accept"]) // from _root
	assert.Equal(t, "middle", env.Headers["X-Layer"])          // from _middle
	assert.Equal(t, "leaf", env.Values["from"])                // overridden by leaf
}

func TestLoadNamedEnvironment_CircularExtends(t *testing.T) {
	yaml := `
environments:
  a:
    extends: b
    auth:
      type: none
  b:
    extends: a
    auth:
      type: none
`
	path := writeTempYAML(t, yaml)

	_, err := LoadNamedEnvironment(path, "a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular")
}

func TestLoadNamedEnvironment_ExtendsNotFound(t *testing.T) {
	yaml := `
environments:
  dev:
    extends: _nonexistent
    auth:
      type: none
`
	path := writeTempYAML(t, yaml)

	_, err := LoadNamedEnvironment(path, "dev")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "_nonexistent")
	assert.Contains(t, err.Error(), "not found")
}

func TestLoadNamedEnvironment_VarSubstitution(t *testing.T) {
	yaml := `
environments:
  _direct:
    auth:
      type: none
    overrides:
      - match: "search*"
        baseUrl: https://search-${env_tag}.example.com
      - match: "price*"
        baseUrl: https://price-${env_tag}.example.com
        pathRewrite:
          strip: /v1
          prefix: /svc-${env_tag}/v1

  dev:
    extends: _direct
    vars:
      env_tag: dev
    apiBaseUrl: https://gateway-${env_tag}.example.com
`
	path := writeTempYAML(t, yaml)

	env, err := LoadNamedEnvironment(path, "dev")
	require.NoError(t, err)
	assert.Equal(t, "https://gateway-dev.example.com", env.APIBaseURL)
	require.Len(t, env.Overrides, 2)
	assert.Equal(t, "https://search-dev.example.com", env.Overrides[0].BaseURL)
	assert.Equal(t, "https://price-dev.example.com", env.Overrides[1].BaseURL)
	assert.Equal(t, "/svc-dev/v1", env.Overrides[1].PathRewrite.Prefix)
}

func TestLoadNamedEnvironment_UnresolvedVar(t *testing.T) {
	yaml := `
environments:
  dev:
    apiBaseUrl: https://${missing_var}.example.com
    auth:
      type: none
`
	path := writeTempYAML(t, yaml)

	_, err := LoadNamedEnvironment(path, "dev")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unresolved")
	assert.Contains(t, err.Error(), "${missing_var}")
}

func TestLoadNamedEnvironment_VarsNoUnresolvedWhenAllDefined(t *testing.T) {
	yaml := `
environments:
  dev:
    apiBaseUrl: https://${host}.example.com
    auth:
      type: none
    vars:
      host: dev-api
`
	path := writeTempYAML(t, yaml)

	env, err := LoadNamedEnvironment(path, "dev")
	require.NoError(t, err)
	assert.Equal(t, "https://dev-api.example.com", env.APIBaseURL)
}

func TestLoadNamedEnvironment_AuthReplace(t *testing.T) {
	yaml := `
environments:
  _base:
    apiBaseUrl: https://api.example.com
    auth:
      type: oauth2
      tokenUrl: https://auth.example.com/token
      credentials:
        clientId:
          source: literal
          value: base-id

  child:
    extends: _base
    auth:
      type: none
`
	path := writeTempYAML(t, yaml)

	env, err := LoadNamedEnvironment(path, "child")
	require.NoError(t, err)
	assert.Equal(t, "none", env.Auth.Type)
	assert.Empty(t, env.Auth.TokenURL) // fully replaced, not merged
}

func TestLoadNamedEnvironment_OverridesPrepend(t *testing.T) {
	yaml := `
environments:
  _base:
    apiBaseUrl: https://api.example.com
    auth:
      type: none
    overrides:
      - match: "base-pattern"
        baseUrl: https://base.example.com

  child:
    extends: _base
    overrides:
      - match: "child-pattern"
        baseUrl: https://child.example.com
`
	path := writeTempYAML(t, yaml)

	env, err := LoadNamedEnvironment(path, "child")
	require.NoError(t, err)
	require.Len(t, env.Overrides, 2)
	// Child overrides come first (higher priority)
	assert.Equal(t, "child-pattern", env.Overrides[0].Match)
	assert.Equal(t, "base-pattern", env.Overrides[1].Match)
}

func TestLoadNamedEnvironment_SharedMerge(t *testing.T) {
	yaml := `
shared:
  headers:
    Accept: application/json
    X-Shared: shared-val
  llm:
    endpoint: https://api.openai.com/v1
    apiKey:
      source: literal
      value: shared-key
    model: gpt-4
  settings:
    maxRunDuration: 5m
    defaultRetries: 3

environments:
  dev:
    apiBaseUrl: https://dev.example.com
    auth:
      type: none
    headers:
      X-Shared: overridden
      X-Dev: dev-val
    settings:
      defaultRetries: 1
`
	path := writeTempYAML(t, yaml)

	env, err := LoadNamedEnvironment(path, "dev")
	require.NoError(t, err)
	// Headers: shared + env merged, env wins on conflict
	assert.Equal(t, "application/json", env.Headers["Accept"])
	assert.Equal(t, "overridden", env.Headers["X-Shared"])
	assert.Equal(t, "dev-val", env.Headers["X-Dev"])
	// LLM: inherited from shared
	assert.Equal(t, "https://api.openai.com/v1", env.LLM.Endpoint)
	assert.Equal(t, "gpt-4", env.LLM.Model)
	// Settings: field-level merge
	assert.Equal(t, 1, env.Settings.DefaultRetries) // overridden
}

func TestLoadNamedEnvironment_SettingsFieldMerge(t *testing.T) {
	yaml := `
shared:
  settings:
    maxRunDuration: 5m
    defaultRetries: 3
    archiveFormat: json.gz

environments:
  dev:
    apiBaseUrl: https://dev.example.com
    auth:
      type: none
    settings:
      defaultRetries: 1
`
	path := writeTempYAML(t, yaml)

	env, err := LoadNamedEnvironment(path, "dev")
	require.NoError(t, err)
	assert.Equal(t, 1, env.Settings.DefaultRetries)            // overridden
	assert.Equal(t, ArchiveJSONGZ, env.Settings.ArchiveFormat) // inherited
}

func TestLoadEnvironment_RejectsMultiEnv(t *testing.T) {
	yaml := `
environments:
  dev:
    apiBaseUrl: https://dev.example.com
    auth:
      type: none
`
	path := writeTempYAML(t, yaml)

	_, err := LoadEnvironment(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple environments")
}

func TestLoadEnvironment_LegacyStillWorks(t *testing.T) {
	yaml := `
environment: test-env
apiBaseUrl: https://api.example.com
auth:
  type: none
llm:
  endpoint: https://api.openai.com/v1
  apiKey:
    source: literal
    value: test-key
  model: gpt-4
`
	path := writeTempYAML(t, yaml)

	env, err := LoadEnvironment(path)
	require.NoError(t, err)
	assert.Equal(t, "test-env", env.Name)
}

func TestListEnvironments_MultiEnv(t *testing.T) {
	yaml := `
environments:
  _base:
    auth:
      type: none
  dev:
    extends: _base
    apiBaseUrl: https://dev.example.com
  staging:
    extends: _base
    apiBaseUrl: https://staging.example.com
  prod:
    extends: _base
    apiBaseUrl: https://prod.example.com
`
	path := writeTempYAML(t, yaml)

	names, err := ListEnvironments(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"dev", "prod", "staging"}, names) // sorted, no _base
}

func TestListEnvironments_Legacy(t *testing.T) {
	yaml := `
environment: my-env
apiBaseUrl: https://api.example.com
auth:
  type: none
`
	path := writeTempYAML(t, yaml)

	names, err := ListEnvironments(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"my-env"}, names)
}

func TestMergePartials_EmptyOverlay(t *testing.T) {
	base := EnvironmentPartial{
		APIBaseURL: "https://base.com",
		Auth:       &AuthConfig{Type: "none"},
		Headers:    map[string]string{"Accept": "json"},
	}
	overlay := EnvironmentPartial{}

	result := mergePartials(base, overlay)
	assert.Equal(t, "https://base.com", result.APIBaseURL)
	assert.Equal(t, "none", result.Auth.Type)
	assert.Equal(t, "json", result.Headers["Accept"])
}

func TestMergePartials_EmptyBase(t *testing.T) {
	base := EnvironmentPartial{}
	overlay := EnvironmentPartial{
		APIBaseURL: "https://overlay.com",
		Auth:       &AuthConfig{Type: "bearer"},
	}

	result := mergePartials(base, overlay)
	assert.Equal(t, "https://overlay.com", result.APIBaseURL)
	assert.Equal(t, "bearer", result.Auth.Type)
}

func TestSubstituteVars_InHeaders(t *testing.T) {
	p := &EnvironmentPartial{
		Headers: map[string]string{
			"X-Env": "${env_name}",
		},
		Vars: map[string]string{
			"env_name": "production",
		},
	}

	err := substituteVars(p)
	require.NoError(t, err)
	assert.Equal(t, "production", p.Headers["X-Env"])
}

func TestSubstituteVars_MultipleInOneString(t *testing.T) {
	p := &EnvironmentPartial{
		APIBaseURL: "https://${host}.${domain}",
		Auth:       &AuthConfig{Type: "none"},
		Vars: map[string]string{
			"host":   "api",
			"domain": "example.com",
		},
	}

	err := substituteVars(p)
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com", p.APIBaseURL)
}

func TestSubstituteVars_NoVarsNoPlaceholders(t *testing.T) {
	p := &EnvironmentPartial{
		APIBaseURL: "https://api.example.com",
	}

	err := substituteVars(p)
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com", p.APIBaseURL)
}

func TestLoadNamedEnvironment_NoApiBaseUrlAllowed(t *testing.T) {
	yaml := `
environments:
  direct:
    auth:
      type: none
    overrides:
      - match: "search*"
        baseUrl: https://search.example.com
`
	path := writeTempYAML(t, yaml)

	// Multi-env format allows empty apiBaseUrl
	env, err := LoadNamedEnvironment(path, "direct")
	require.NoError(t, err)
	assert.Empty(t, env.APIBaseURL)
	assert.Len(t, env.Overrides, 1)
}

func TestLoadNamedEnvironment_VarsFromSharedAndEnv(t *testing.T) {
	yaml := `
shared:
  vars:
    domain: example.com

environments:
  dev:
    apiBaseUrl: https://${host}.${domain}
    auth:
      type: none
    vars:
      host: dev-api
`
	path := writeTempYAML(t, yaml)

	env, err := LoadNamedEnvironment(path, "dev")
	require.NoError(t, err)
	assert.Equal(t, "https://dev-api.example.com", env.APIBaseURL)
}

func TestLoadNamedEnvironment_DefaultsApplied(t *testing.T) {
	yaml := `
environments:
  dev:
    apiBaseUrl: https://dev.example.com
    auth:
      type: none
`
	path := writeTempYAML(t, yaml)

	env, err := LoadNamedEnvironment(path, "dev")
	require.NoError(t, err)
	// Defaults should be applied
	assert.NotZero(t, env.Settings.MaxRunDuration.Duration)
	assert.NotZero(t, env.Settings.DefaultRetries)
	assert.Equal(t, ArchiveJSON, env.Settings.ArchiveFormat)
}

func TestLoadNamedEnvironment_IncludeSecrets(t *testing.T) {
	dir := t.TempDir()

	// Main file (committable) — structure without auth
	mainYAML := `
include:
  - env.secrets.yaml

shared:
  headers:
    Accept: application/json

environments:
  dev:
    apiBaseUrl: https://dev.example.com
  prod:
    apiBaseUrl: https://prod.example.com
`
	// Secrets file (gitignored) — auth credentials
	secretsYAML := `
environments:
  dev:
    auth:
      type: bearer
      credentials:
        token:
          source: literal
          value: dev-secret-token
  prod:
    auth:
      type: bearer
      credentials:
        token:
          source: literal
          value: prod-secret-token
`
	mainPath := filepath.Join(dir, "env.yaml")
	secretsPath := filepath.Join(dir, "env.secrets.yaml")
	require.NoError(t, os.WriteFile(mainPath, []byte(mainYAML), 0644))
	require.NoError(t, os.WriteFile(secretsPath, []byte(secretsYAML), 0644))

	// Dev should have structure from main + auth from secrets
	env, err := LoadNamedEnvironment(mainPath, "dev")
	require.NoError(t, err)
	assert.Equal(t, "https://dev.example.com", env.APIBaseURL)
	assert.Equal(t, "bearer", env.Auth.Type)
	assert.Equal(t, "application/json", env.Headers["Accept"])

	// Prod too
	env, err = LoadNamedEnvironment(mainPath, "prod")
	require.NoError(t, err)
	assert.Equal(t, "https://prod.example.com", env.APIBaseURL)
	assert.Equal(t, "bearer", env.Auth.Type)
}

func TestLoadNamedEnvironment_IncludeSharedSecrets(t *testing.T) {
	dir := t.TempDir()

	mainYAML := `
include:
  - secrets.yaml

shared:
  headers:
    Accept: application/json

environments:
  dev:
    apiBaseUrl: https://dev.example.com
    auth:
      type: none
`
	secretsYAML := `
shared:
  llm:
    endpoint: https://api.openai.com/v1
    apiKey:
      source: literal
      value: sk-secret
    model: gpt-4
`
	mainPath := filepath.Join(dir, "env.yaml")
	secretsPath := filepath.Join(dir, "secrets.yaml")
	require.NoError(t, os.WriteFile(mainPath, []byte(mainYAML), 0644))
	require.NoError(t, os.WriteFile(secretsPath, []byte(secretsYAML), 0644))

	env, err := LoadNamedEnvironment(mainPath, "dev")
	require.NoError(t, err)
	assert.Equal(t, "https://api.openai.com/v1", env.LLM.Endpoint)
	assert.Equal(t, "gpt-4", env.LLM.Model)
	assert.Equal(t, "application/json", env.Headers["Accept"])
}

func TestLoadNamedEnvironment_IncludeAddsEnvironment(t *testing.T) {
	dir := t.TempDir()

	mainYAML := `
include:
  - extra.yaml

environments:
  dev:
    apiBaseUrl: https://dev.example.com
    auth:
      type: none
`
	extraYAML := `
environments:
  staging:
    apiBaseUrl: https://staging.example.com
    auth:
      type: none
`
	mainPath := filepath.Join(dir, "env.yaml")
	extraPath := filepath.Join(dir, "extra.yaml")
	require.NoError(t, os.WriteFile(mainPath, []byte(mainYAML), 0644))
	require.NoError(t, os.WriteFile(extraPath, []byte(extraYAML), 0644))

	// Both environments should be available
	names, err := ListEnvironments(mainPath)
	require.NoError(t, err)
	assert.Equal(t, []string{"dev", "staging"}, names)
}

func TestLoadNamedEnvironment_IncludeNotFound(t *testing.T) {
	dir := t.TempDir()

	mainYAML := `
include:
  - nonexistent.yaml

environments:
  dev:
    apiBaseUrl: https://dev.example.com
    auth:
      type: none
`
	mainPath := filepath.Join(dir, "env.yaml")
	require.NoError(t, os.WriteFile(mainPath, []byte(mainYAML), 0644))

	_, err := LoadNamedEnvironment(mainPath, "dev")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent.yaml")
}

func TestLoadNamedEnvironment_IncludeNoRecursion(t *testing.T) {
	dir := t.TempDir()

	mainYAML := `
include:
  - second.yaml

environments:
  dev:
    apiBaseUrl: https://dev.example.com
    auth:
      type: none
`
	secondYAML := `
include:
  - third.yaml

environments:
  staging:
    apiBaseUrl: https://staging.example.com
    auth:
      type: none
`
	mainPath := filepath.Join(dir, "env.yaml")
	secondPath := filepath.Join(dir, "second.yaml")
	require.NoError(t, os.WriteFile(mainPath, []byte(mainYAML), 0644))
	require.NoError(t, os.WriteFile(secondPath, []byte(secondYAML), 0644))

	_, err := LoadNamedEnvironment(mainPath, "dev")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not contain its own include")
}

func TestLoadNamedEnvironment_MultipleIncludes(t *testing.T) {
	dir := t.TempDir()

	mainYAML := `
include:
  - auth.yaml
  - llm.yaml

shared:
  headers:
    Accept: application/json

environments:
  dev:
    apiBaseUrl: https://dev.example.com
`
	authYAML := `
environments:
  dev:
    auth:
      type: none
`
	llmYAML := `
shared:
  llm:
    endpoint: https://api.openai.com/v1
    apiKey:
      source: literal
      value: test-key
    model: gpt-4
`
	mainPath := filepath.Join(dir, "env.yaml")
	require.NoError(t, os.WriteFile(mainPath, []byte(mainYAML), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.yaml"), []byte(authYAML), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "llm.yaml"), []byte(llmYAML), 0644))

	env, err := LoadNamedEnvironment(mainPath, "dev")
	require.NoError(t, err)
	assert.Equal(t, "https://dev.example.com", env.APIBaseURL)
	assert.Equal(t, "none", env.Auth.Type)
	assert.Equal(t, "gpt-4", env.LLM.Model)
	assert.Equal(t, "application/json", env.Headers["Accept"])
}

// writeTempYAML creates a temporary YAML file and returns its path.
func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "env.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}
