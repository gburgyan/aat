package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OpenAIClient implements Client for OpenAI-compatible APIs.
// This covers OpenAI, Azure OpenAI, vLLM, Ollama, and any endpoint
// that speaks the /chat/completions protocol.
type OpenAIClient struct {
	Endpoint   string // base URL, e.g. "https://api.openai.com/v1"
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

// openAI request/response wire types

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	Stop        []string        `json:"stop,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
	Model   string         `json:"model"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type openAIError struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends a completion request to an OpenAI-compatible endpoint.
func (c *OpenAIClient) Complete(ctx context.Context, req *Request) (*Response, error) {
	model := req.Model
	if model == "" {
		model = c.Model
	}
	if model == "" {
		return nil, fmt.Errorf("llm: model is required")
	}

	msgs := make([]openAIMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = openAIMessage{Role: string(m.Role), Content: m.Content}
	}

	oReq := openAIRequest{
		Model:    model,
		Messages: msgs,
		Stop:     req.Stop,
	}
	if req.MaxTokens > 0 {
		oReq.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		t := req.Temperature
		oReq.Temperature = &t
	}

	body, err := json.Marshal(oReq)
	if err != nil {
		return nil, fmt.Errorf("llm: marshaling request: %w", err)
	}

	url := c.Endpoint + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: sending request: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm: reading response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		var oErr openAIError
		if json.Unmarshal(respBody, &oErr) == nil && oErr.Error.Message != "" {
			return nil, fmt.Errorf("llm: API error (status %d): %s", httpResp.StatusCode, oErr.Error.Message)
		}
		return nil, fmt.Errorf("llm: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var oResp openAIResponse
	if err := json.Unmarshal(respBody, &oResp); err != nil {
		return nil, fmt.Errorf("llm: parsing response: %w", err)
	}

	if len(oResp.Choices) == 0 {
		return nil, fmt.Errorf("llm: no choices in response")
	}

	return &Response{
		Content:      oResp.Choices[0].Message.Content,
		Model:        oResp.Model,
		InputTokens:  oResp.Usage.PromptTokens,
		OutputTokens: oResp.Usage.CompletionTokens,
		FinishReason: oResp.Choices[0].FinishReason,
	}, nil
}
