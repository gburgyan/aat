package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gburgyan/aat/internal/yamlx"
)

// readFile reads an environment file and returns its contents.
func readFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading environment file: %w", err)
	}
	return data, nil
}

// LoadEnvironment reads a YAML environment file, applies defaults, and validates it.
// For multi-environment files, use LoadNamedEnvironment instead.
func LoadEnvironment(path string) (*Environment, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, err
	}

	// Detect multi-env format and reject with helpful message
	if isMultiEnv(data) {
		names, _ := listEnvNamesFromData(data)
		return nil, fmt.Errorf("env file defines multiple environments (%s); specify one with --env or set defaultEnvironment in aat-project.yaml",
			strings.Join(names, ", "))
	}

	return loadLegacyEnv(path, data)
}

// LoadEnvironmentFromDir loads a named environment from a directory (e.g., "<name>.yaml").
func LoadEnvironmentFromDir(dir, name string) (*Environment, error) {
	path := filepath.Join(dir, name+".yaml")
	return LoadEnvironment(path)
}

// OverlayFile is a sparse YAML structure containing overrides and optional
// transaction-level auth and headers. When Auth is set, it replaces the
// environment auth for the entire run (all nodes), not just matched overrides.
// When Headers is set, those headers are merged into every request.
// When Environment is set, it selects the named environment for the run unless
// the CLI --env flag or AAT_ENV_NAME env var explicitly chose one.
type OverlayFile struct {
	Environment string            `yaml:"environment,omitempty"`
	Auth        *AuthConfig       `yaml:"auth,omitempty"`
	Headers     map[string]string `yaml:"headers,omitempty"`
	Overrides   []HostOverride    `yaml:"overrides"`
}

// CollectSecrets resolves the credentials of the overlay's auth and of its
// overrides' auth, for redaction in archives.
func (o *OverlayFile) CollectSecrets() map[string]bool {
	secrets := CollectAuthSecrets(o.Auth)
	for _, ov := range o.Overrides {
		for k := range CollectAuthSecrets(ov.Auth) {
			secrets[k] = true
		}
	}
	return secrets
}

// LoadOverlayFile reads a YAML overlay file and returns the parsed overlay.
// The caller should check overlay.Auth for transaction-level auth and
// overlay.Overrides for per-node overrides.
func LoadOverlayFile(path string) (*OverlayFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading overlay file: %w", err)
	}

	var overlay OverlayFile
	if err := yamlx.Decode(data, &overlay); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if errs := validateOverrides(overlay.Overrides); len(errs) > 0 {
		return nil, fmt.Errorf("overlay overrides validation failed:\n- %s", strings.Join(errs, "\n- "))
	}

	if overlay.Auth != nil {
		if errs := ValidateAuth(overlay.Auth); len(errs) > 0 {
			return nil, fmt.Errorf("overlay auth validation failed:\n- %s", strings.Join(errs, "\n- "))
		}
	}

	return &overlay, nil
}

// MergeOverrides appends overlay overrides to base overrides.
// Later entries win on conflict for the same match pattern.
func MergeOverrides(base, overlay []HostOverride) []HostOverride {
	if len(overlay) == 0 {
		return base
	}
	result := make([]HostOverride, 0, len(base)+len(overlay))
	result = append(result, base...)
	result = append(result, overlay...)
	return result
}

// ValidateEnvironment checks that an Environment has all required fields and valid values.
func ValidateEnvironment(env *Environment) error {
	var errs []string

	if env.Name == "" {
		errs = append(errs, "environment name is required")
	}
	if env.APIBaseURL == "" {
		errs = append(errs, "apiBaseUrl is required")
	}

	errs = append(errs, ValidateAuth(&env.Auth)...)
	errs = append(errs, validateSettings(&env.Settings)...)
	errs = append(errs, validateOverrides(env.Overrides)...)

	if len(errs) > 0 {
		return fmt.Errorf("environment validation failed:\n- %s", strings.Join(errs, "\n- "))
	}
	return nil
}

// ValidateAuth checks that an AuthConfig has valid type and required fields.
func ValidateAuth(auth *AuthConfig) []string {
	var errs []string

	switch auth.Type {
	case "oauth2":
		if auth.TokenURL == "" {
			errs = append(errs, "auth.tokenUrl is required for oauth2")
		}
		for _, key := range []string{"username", "password", "clientId", "clientSecret"} {
			if _, ok := auth.Credentials[key]; !ok {
				errs = append(errs, fmt.Sprintf("auth.credentials.%s is required for oauth2", key))
			}
		}
	case "apikey":
		if _, ok := auth.Credentials["key"]; !ok {
			errs = append(errs, "auth.credentials.key is required for apikey")
		}
		if auth.HeaderName == "" {
			errs = append(errs, "auth.headerName is required for apikey")
		}
	case "bearer":
		if _, ok := auth.Credentials["token"]; !ok {
			errs = append(errs, "auth.credentials.token is required for bearer")
		}
	case "none", "":
		// no requirements
	default:
		errs = append(errs, fmt.Sprintf("unknown auth type %q (expected oauth2, apikey, bearer, or none)", auth.Type))
	}

	return errs
}

func validateOverrides(overrides []HostOverride) []string {
	var errs []string
	for i, ov := range overrides {
		if ov.Match == "" {
			errs = append(errs, fmt.Sprintf("overrides[%d]: match is required", i))
		}
		if ov.Auth != nil {
			for _, e := range ValidateAuth(ov.Auth) {
				errs = append(errs, fmt.Sprintf("overrides[%d]: %s", i, e))
			}
		}
		if ov.ExpectFailure != nil {
			if len(ov.ExpectFailure.Status) == 0 {
				errs = append(errs, fmt.Sprintf("overrides[%d]: expectFailure must have at least one status", i))
			}
			for _, code := range ov.ExpectFailure.Status {
				if code < 400 {
					errs = append(errs, fmt.Sprintf("overrides[%d]: expectFailure status %d must be >= 400", i, code))
				}
			}
		}
	}
	return errs
}

func validateSettings(s *RuntimeSettings) []string {
	var errs []string
	if err := CheckOASValidationMode(s.OASValidation); err != nil {
		errs = append(errs, fmt.Sprintf("settings.oasValidation: %s", err))
	}
	if _, err := s.RequestInterval(); err != nil {
		errs = append(errs, fmt.Sprintf("settings.minRequestInterval: %s", err))
	}
	return errs
}

// CheckOASValidationMode returns an error unless mode is an accepted
// settings.oasValidation or --oas-validate value: auto, strict, or off. Empty
// means unset and is accepted.
func CheckOASValidationMode(mode string) error {
	switch mode {
	case "", "auto", "strict", "off":
		return nil
	case "warn":
		return fmt.Errorf(`OpenAPI validation mode "warn" was removed because it behaved exactly like "auto"; use auto`)
	}
	return fmt.Errorf("unknown OpenAPI validation mode %q (expected auto, strict, or off)", mode)
}
