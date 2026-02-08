package llm

import (
	"fmt"
	"strings"

	"github.com/gburgyan/aat/config"
)

// ProviderType identifies an LLM API format.
type ProviderType string

const (
	ProviderOpenAI    ProviderType = "openai"
	ProviderAnthropic ProviderType = "anthropic"
)

// NewClient creates a Client from an LLMConfig. The provider is determined
// by the explicit Provider field, or auto-detected from the endpoint URL.
func NewClient(cfg config.LLMConfig) (Client, error) {
	apiKey, err := cfg.APIKey.Resolve()
	if err != nil {
		return nil, fmt.Errorf("llm: resolving API key: %w", err)
	}

	provider := detectProvider(cfg)

	switch provider {
	case ProviderAnthropic:
		endpoint := cfg.Endpoint
		if endpoint == "" {
			endpoint = "https://api.anthropic.com"
		}
		return &AnthropicClient{
			Endpoint: endpoint,
			APIKey:   apiKey,
			Model:    cfg.Model,
		}, nil

	case ProviderOpenAI:
		endpoint := cfg.Endpoint
		if endpoint == "" {
			endpoint = "https://api.openai.com/v1"
		}
		return &OpenAIClient{
			Endpoint: endpoint,
			APIKey:   apiKey,
			Model:    cfg.Model,
		}, nil

	default:
		return nil, fmt.Errorf("llm: unknown provider %q", provider)
	}
}

// detectProvider determines the provider from explicit config or endpoint URL.
func detectProvider(cfg config.LLMConfig) ProviderType {
	if cfg.Provider != "" {
		return ProviderType(cfg.Provider)
	}
	if strings.Contains(cfg.Endpoint, "anthropic.com") {
		return ProviderAnthropic
	}
	return ProviderOpenAI
}
