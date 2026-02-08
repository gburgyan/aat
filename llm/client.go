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

// Request describes an LLM completion request.
type Request struct {
	Messages    []Message
	Model       string
	MaxTokens   int
	Temperature float64
	Stop        []string
}

// Response holds the result of an LLM completion.
type Response struct {
	Content      string
	Model        string
	InputTokens  int
	OutputTokens int
	FinishReason string
}

// Client is the provider-agnostic interface for LLM completions.
type Client interface {
	Complete(ctx context.Context, req *Request) (*Response, error)
}
