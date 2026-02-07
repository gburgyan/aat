package adapter

// EnvironmentConfig holds the environment-specific settings that adapters
// need when building requests. This is intentionally minimal — the full
// config layer is Task 10.
type EnvironmentConfig struct {
	BaseURL string            // scheme+host (e.g., "https://api.example.com")
	Headers map[string]string // default headers (auth tokens, API keys)
	Values  map[string]string // arbitrary k/v (e.g., "auth.token")
}

// GetValue returns the value for the given key from the environment's Values map.
// The second return value indicates whether the key was found.
func (c *EnvironmentConfig) GetValue(key string) (string, bool) {
	if c.Values == nil {
		return "", false
	}
	v, ok := c.Values[key]
	return v, ok
}
