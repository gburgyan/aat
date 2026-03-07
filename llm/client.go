package llm

import "context"

// Role identifies the sender of a message in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a single message in a conversation.
type Message struct {
	Role    Role
	Content string
}

// ThinkingConfig controls extended thinking / reasoning.
type ThinkingConfig struct {
	BudgetTokens    int    // Anthropic: thinking budget_tokens
	ReasoningEffort string // OpenAI: "low", "medium", "high"
}

// Request describes an LLM completion request.
type Request struct {
	Messages    []Message
	Model       string
	MaxTokens   int
	Temperature float64
	Stop        []string
	Thinking    *ThinkingConfig // nil = no thinking
}

// Response holds the result of an LLM completion.
type Response struct {
	Content      string
	Thinking     string // thinking/reasoning content if returned
	Model        string
	InputTokens  int
	OutputTokens int
	FinishReason string
}

// Client is the provider-agnostic interface for LLM completions.
type Client interface {
	Complete(ctx context.Context, req *Request) (*Response, error)
}
