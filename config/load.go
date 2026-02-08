package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// LoadEnvironment reads a YAML environment file, applies defaults, and validates it.
func LoadEnvironment(path string) (*Environment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading environment file: %w", err)
	}

	var env Environment
	if err := yaml.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parsing environment YAML: %w", err)
	}

	applyDefaults(&env)

	if err := ValidateEnvironment(&env); err != nil {
		return nil, err
	}

	return &env, nil
}

// LoadEnvironmentFromDir loads a named environment from a directory (e.g., "<name>.yaml").
func LoadEnvironmentFromDir(dir, name string) (*Environment, error) {
	path := filepath.Join(dir, name+".yaml")
	return LoadEnvironment(path)
}

func applyDefaults(env *Environment) {
	if env.LLM.Mode == "" {
		env.LLM.Mode = ModeLean
	}
	if env.Settings.MaxRunDuration.Duration == 0 {
		env.Settings.MaxRunDuration.Duration = 120 * time.Second
	}
	if env.Settings.DefaultRetries == 0 {
		env.Settings.DefaultRetries = 2
	}
	if env.Settings.MaxRelaxationDepth == 0 {
		env.Settings.MaxRelaxationDepth = 3
	}
	if env.Settings.ArchiveFormat == "" {
		env.Settings.ArchiveFormat = ArchiveJSON
	}
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

	errs = append(errs, validateAuth(&env.Auth)...)
	errs = append(errs, validateLLM(&env.LLM)...)
	errs = append(errs, validateSettings(&env.Settings)...)

	if len(errs) > 0 {
		return fmt.Errorf("environment validation failed:\n- %s", strings.Join(errs, "\n- "))
	}
	return nil
}

func validateAuth(auth *AuthConfig) []string {
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

func validateLLM(llm *LLMConfig) []string {
	var errs []string

	if llm.Mode != "" {
		switch llm.Mode {
		case ModeStrict, ModeLean, ModeAdaptive:
			// valid
		default:
			errs = append(errs, fmt.Sprintf("unknown llm.mode %q (expected strict, lean, or adaptive)", llm.Mode))
		}
	}

	return errs
}

func validateSettings(s *RuntimeSettings) []string {
	var errs []string

	if s.ArchiveFormat != "" {
		switch s.ArchiveFormat {
		case ArchiveJSON, ArchiveJSONGZ:
			// valid
		default:
			errs = append(errs, fmt.Sprintf("unknown archiveFormat %q (expected json or json.gz)", s.ArchiveFormat))
		}
	}

	return errs
}
