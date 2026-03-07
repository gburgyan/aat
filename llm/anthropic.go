package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// AnthropicClient implements Client for the Anthropic Messages API.
type AnthropicClient struct {
	Endpoint   string // base URL, e.g. "https://api.anthropic.com"
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

// Anthropic wire types

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Stop        []string           `json:"stop_sequences,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
	Thinking    *anthropicThinking `json:"thinking,omitempty"`
}

type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content    []anthropicContent `json:"content"`
	Model      string             `json:"model"`
	StopReason string             `json:"stop_reason"`
	Usage      anthropicUsage     `json:"usage"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicError struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends a completion request to the Anthropic Messages API.
func (c *AnthropicClient) Complete(ctx context.Context, req *Request) (*Response, error) {
	model := req.Model
	if model == "" {
		model = c.Model
	}
	if model == "" {
		return nil, fmt.Errorf("llm: model is required")
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096 // Anthropic requires max_tokens
	}

	// Separate system message from user/assistant messages.
	var system string
	var msgs []anthropicMessage
	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			system = m.Content
			continue
		}
		msgs = append(msgs, anthropicMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	aReq := anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  msgs,
		Stop:      req.Stop,
	}

	// Wire thinking or temperature (mutually exclusive per Anthropic API).
	if req.Thinking != nil && req.Thinking.BudgetTokens > 0 {
		aReq.Thinking = &anthropicThinking{
			Type:         "enabled",
			BudgetTokens: req.Thinking.BudgetTokens,
		}
		// Temperature must not be sent when thinking is enabled.
	} else if req.Temperature > 0 {
		t := req.Temperature
		aReq.Temperature = &t
	}

	body, err := json.Marshal(aReq)
	if err != nil {
		return nil, fmt.Errorf("llm: marshaling request: %w", err)
	}

	url := c.Endpoint + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.APIKey)
	httpReq.Header.Set("anthropic-version", "2025-04-15")

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
		var aErr anthropicError
		if json.Unmarshal(respBody, &aErr) == nil && aErr.Error.Message != "" {
			return nil, fmt.Errorf("llm: API error (status %d): %s", httpResp.StatusCode, aErr.Error.Message)
		}
		return nil, fmt.Errorf("llm: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var aResp anthropicResponse
	if err := json.Unmarshal(respBody, &aResp); err != nil {
		return nil, fmt.Errorf("llm: parsing response: %w", err)
	}

	var content string
	var thinking string
	for _, c := range aResp.Content {
		switch c.Type {
		case "text":
			content = c.Text
		case "thinking":
			if thinking != "" {
				thinking += "\n"
			}
			thinking += c.Text
		}
	}

	return &Response{
		Content:      content,
		Thinking:     thinking,
		Model:        aResp.Model,
		InputTokens:  aResp.Usage.InputTokens,
		OutputTokens: aResp.Usage.OutputTokens,
		FinishReason: aResp.StopReason,
	}, nil
}
