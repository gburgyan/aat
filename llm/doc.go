// Package llm provides a provider-agnostic LLM client interface and
// implementations for OpenAI-compatible and Anthropic APIs.
//
// The Client interface exposes a single Complete method. Provider-specific
// details (auth headers, request/response formats) are encapsulated in the
// OpenAIClient and AnthropicClient implementations. Use NewClient to
// create a Client from a config.LLMConfig, with automatic provider
// detection from the endpoint URL.
package llm
