package adapter

import "strings"

// EnvironmentConfig holds the environment-specific settings that adapters
// need when building requests. A template reads values only from its step's
// inputs; an environment value reaches it through an input default such as
// "{{env.KEY}}".
type EnvironmentConfig struct {
	BaseURL string            // scheme+host (e.g., "https://api.example.com")
	Headers map[string]string // headers every request starts with; template headers replace them
	// Protected headers are applied after template headers, so a template
	// cannot replace them: the credential, an override's own headers, and
	// overlay headers.
	Protected map[string]string
}

// setHeader sets a header, first removing any header whose name differs only in
// case: HTTP header names are case-insensitive.
func setHeader(headers map[string]string, name, value string) {
	for existing := range headers {
		if existing != name && strings.EqualFold(existing, name) {
			delete(headers, existing)
		}
	}
	headers[name] = value
}
