package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gburgyan/aat/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- OpenAI tests ---

func TestOpenAI_SuccessfulCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		var req openAIRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "gpt-4", req.Model)
		assert.Len(t, req.Messages, 2)
		assert.Equal(t, "system", req.Messages[0].Role)
		assert.Equal(t, "user", req.Messages[1].Role)

		_ = json.NewEncoder(w).Encode(openAIResponse{
			Choices: []openAIChoice{{
				Message:      openAIMessage{Role: "assistant", Content: "Hello!"},
				FinishReason: "stop",
			}},
			Model: "gpt-4-0613",
			Usage: openAIUsage{PromptTokens: 10, CompletionTokens: 5},
		})
	}))
	defer server.Close()

	client := &OpenAIClient{
		Endpoint: server.URL,
		APIKey:   "test-key",
		Model:    "gpt-4",
	}

	resp, err := client.Complete(context.Background(), &Request{
		Messages: []Message{
			{Role: RoleSystem, Content: "You are helpful."},
			{Role: RoleUser, Content: "Hi"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "Hello!", resp.Content)
	assert.Equal(t, "gpt-4-0613", resp.Model)
	assert.Equal(t, 10, resp.InputTokens)
	assert.Equal(t, 5, resp.OutputTokens)
	assert.Equal(t, "stop", resp.FinishReason)
}

func TestOpenAI_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(openAIError{
			Error: struct {
				Message string `json:"message"`
			}{Message: "rate limit exceeded"},
		})
	}))
	defer server.Close()

	client := &OpenAIClient{Endpoint: server.URL, APIKey: "k", Model: "gpt-4"}
	_, err := client.Complete(context.Background(), &Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
	assert.Contains(t, err.Error(), "429")
}

func TestOpenAI_NoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(openAIResponse{
			Choices: []openAIChoice{},
			Model:   "gpt-4",
		})
	}))
	defer server.Close()

	client := &OpenAIClient{Endpoint: server.URL, APIKey: "k", Model: "gpt-4"}
	_, err := client.Complete(context.Background(), &Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no choices")
}

func TestOpenAI_MissingModel(t *testing.T) {
	client := &OpenAIClient{Endpoint: "http://localhost", APIKey: "k"}
	_, err := client.Complete(context.Background(), &Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "model is required")
}

func TestOpenAI_RequestModelOverridesDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openAIRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "gpt-3.5-turbo", req.Model)

		_ = json.NewEncoder(w).Encode(openAIResponse{
			Choices: []openAIChoice{{
				Message:      openAIMessage{Role: "assistant", Content: "ok"},
				FinishReason: "stop",
			}},
			Model: "gpt-3.5-turbo",
		})
	}))
	defer server.Close()

	client := &OpenAIClient{Endpoint: server.URL, APIKey: "k", Model: "gpt-4"}
	resp, err := client.Complete(context.Background(), &Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
		Model:    "gpt-3.5-turbo",
	})

	require.NoError(t, err)
	assert.Equal(t, "gpt-3.5-turbo", resp.Model)
}

func TestOpenAI_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	client := &OpenAIClient{Endpoint: "http://127.0.0.1:1", APIKey: "k", Model: "gpt-4"}
	_, err := client.Complete(ctx, &Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})

	require.Error(t, err)
}

func TestOpenAI_NoAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(openAIResponse{
			Choices: []openAIChoice{{
				Message:      openAIMessage{Role: "assistant", Content: "ok"},
				FinishReason: "stop",
			}},
			Model: "local-model",
		})
	}))
	defer server.Close()

	// No API key — for local models like Ollama
	client := &OpenAIClient{Endpoint: server.URL, Model: "local-model"}
	resp, err := client.Complete(context.Background(), &Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})

	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Content)
}

// --- Anthropic tests ---

func TestAnthropic_SuccessfulCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/messages", r.URL.Path)
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))

		var req anthropicRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "claude-3-opus-20240229", req.Model)
		assert.Equal(t, "You are helpful.", req.System)
		assert.Len(t, req.Messages, 1)
		assert.Equal(t, "user", req.Messages[0].Role)
		assert.Equal(t, 4096, req.MaxTokens)

		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content:    []anthropicContent{{Type: "text", Text: "Hello!"}},
			Model:      "claude-3-opus-20240229",
			StopReason: "end_turn",
			Usage:      anthropicUsage{InputTokens: 15, OutputTokens: 3},
		})
	}))
	defer server.Close()

	client := &AnthropicClient{
		Endpoint: server.URL,
		APIKey:   "test-key",
		Model:    "claude-3-opus-20240229",
	}

	resp, err := client.Complete(context.Background(), &Request{
		Messages: []Message{
			{Role: RoleSystem, Content: "You are helpful."},
			{Role: RoleUser, Content: "Hi"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "Hello!", resp.Content)
	assert.Equal(t, "claude-3-opus-20240229", resp.Model)
	assert.Equal(t, 15, resp.InputTokens)
	assert.Equal(t, 3, resp.OutputTokens)
	assert.Equal(t, "end_turn", resp.FinishReason)
}

func TestAnthropic_SystemMessageHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

		// System should be top-level, not in messages
		assert.Equal(t, "Be brief.", req.System)
		assert.Len(t, req.Messages, 1)
		assert.Equal(t, "user", req.Messages[0].Role)

		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content:    []anthropicContent{{Type: "text", Text: "ok"}},
			Model:      "claude-3-haiku-20240307",
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	client := &AnthropicClient{Endpoint: server.URL, APIKey: "k", Model: "claude-3-haiku-20240307"}
	_, err := client.Complete(context.Background(), &Request{
		Messages: []Message{
			{Role: RoleSystem, Content: "Be brief."},
			{Role: RoleUser, Content: "Hi"},
		},
	})

	require.NoError(t, err)
}

func TestAnthropic_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(anthropicError{
			Error: struct {
				Message string `json:"message"`
			}{Message: "invalid api key"},
		})
	}))
	defer server.Close()

	client := &AnthropicClient{Endpoint: server.URL, APIKey: "bad", Model: "claude-3-opus-20240229"}
	_, err := client.Complete(context.Background(), &Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid api key")
	assert.Contains(t, err.Error(), "401")
}

func TestAnthropic_MissingModel(t *testing.T) {
	client := &AnthropicClient{Endpoint: "http://localhost", APIKey: "k"}
	_, err := client.Complete(context.Background(), &Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "model is required")
}

func TestAnthropic_CustomMaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, 1024, req.MaxTokens)

		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content:    []anthropicContent{{Type: "text", Text: "ok"}},
			Model:      "claude-3-haiku-20240307",
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	client := &AnthropicClient{Endpoint: server.URL, APIKey: "k", Model: "claude-3-haiku-20240307"}
	_, err := client.Complete(context.Background(), &Request{
		Messages:  []Message{{Role: RoleUser, Content: "Hi"}},
		MaxTokens: 1024,
	})

	require.NoError(t, err)
}

// --- Provider auto-detection tests ---

func TestDetectProvider_ExplicitAnthropic(t *testing.T) {
	p := detectProvider(config.LLMConfig{Provider: "anthropic", Endpoint: "https://api.openai.com/v1"})
	assert.Equal(t, ProviderAnthropic, p)
}

func TestDetectProvider_ExplicitOpenAI(t *testing.T) {
	p := detectProvider(config.LLMConfig{Provider: "openai", Endpoint: "https://api.anthropic.com"})
	assert.Equal(t, ProviderOpenAI, p)
}

func TestDetectProvider_AutoAnthropicFromEndpoint(t *testing.T) {
	p := detectProvider(config.LLMConfig{Endpoint: "https://api.anthropic.com"})
	assert.Equal(t, ProviderAnthropic, p)
}

func TestDetectProvider_AutoOpenAIFromEndpoint(t *testing.T) {
	p := detectProvider(config.LLMConfig{Endpoint: "https://api.openai.com/v1"})
	assert.Equal(t, ProviderOpenAI, p)
}

func TestDetectProvider_DefaultsToOpenAI(t *testing.T) {
	p := detectProvider(config.LLMConfig{Endpoint: "https://my-vllm-server.local/v1"})
	assert.Equal(t, ProviderOpenAI, p)
}

func TestDetectProvider_EmptyEndpointDefaultsOpenAI(t *testing.T) {
	p := detectProvider(config.LLMConfig{})
	assert.Equal(t, ProviderOpenAI, p)
}

// --- NewClient factory tests ---

func TestNewClient_OpenAI(t *testing.T) {
	client, err := NewClient(config.LLMConfig{
		Endpoint: "https://api.openai.com/v1",
		APIKey:   config.SecretRef{Source: "literal", Value: "test-key"},
		Model:    "gpt-4",
	})

	require.NoError(t, err)
	_, ok := client.(*OpenAIClient)
	assert.True(t, ok)
}

func TestNewClient_Anthropic(t *testing.T) {
	client, err := NewClient(config.LLMConfig{
		Endpoint: "https://api.anthropic.com",
		APIKey:   config.SecretRef{Source: "literal", Value: "test-key"},
		Model:    "claude-3-opus-20240229",
	})

	require.NoError(t, err)
	_, ok := client.(*AnthropicClient)
	assert.True(t, ok)
}

func TestNewClient_MissingAPIKey(t *testing.T) {
	_, err := NewClient(config.LLMConfig{
		Endpoint: "https://api.openai.com/v1",
		APIKey:   config.SecretRef{Source: "env", Var: "AAT_DEFINITELY_NOT_SET_99999"},
		Model:    "gpt-4",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving API key")
}

func TestNewClient_ExplicitProvider(t *testing.T) {
	client, err := NewClient(config.LLMConfig{
		Endpoint: "https://my-proxy.local/v1",
		APIKey:   config.SecretRef{Source: "literal", Value: "k"},
		Model:    "claude-3-haiku-20240307",
		Provider: "anthropic",
	})

	require.NoError(t, err)
	_, ok := client.(*AnthropicClient)
	assert.True(t, ok)
}

func TestNewClient_DefaultEndpoints(t *testing.T) {
	// OpenAI with empty endpoint gets default
	client, err := NewClient(config.LLMConfig{
		APIKey: config.SecretRef{Source: "literal", Value: "k"},
		Model:  "gpt-4",
	})
	require.NoError(t, err)
	oai, ok := client.(*OpenAIClient)
	require.True(t, ok)
	assert.Equal(t, "https://api.openai.com/v1", oai.Endpoint)

	// Anthropic with empty endpoint gets default
	client, err = NewClient(config.LLMConfig{
		APIKey:   config.SecretRef{Source: "literal", Value: "k"},
		Model:    "claude-3-opus-20240229",
		Provider: "anthropic",
	})
	require.NoError(t, err)
	ant, ok := client.(*AnthropicClient)
	require.True(t, ok)
	assert.Equal(t, "https://api.anthropic.com", ant.Endpoint)
}
