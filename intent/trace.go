package intent

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gburgyan/aat/llm"
	"github.com/gburgyan/aat/plan"
)

// PlanTrace captures the full pipeline state of a prompt-to-plan transformation
// for debugging and observability. It records both LLM calls (prompts/responses),
// skeleton construction, and merge results.
type PlanTrace struct {
	TraceID            string             `json:"traceId"`
	Timestamp          time.Time          `json:"timestamp"`
	Prompt             string             `json:"prompt"`
	SelectionCall      LLMCallTrace       `json:"selectionCall"`
	SelectionRetryCall *LLMCallTrace      `json:"selectionRetryCall,omitempty"`
	WorkflowSelection  *WorkflowSelection `json:"workflowSelection,omitempty"`
	Skeleton           *SkeletonTrace     `json:"skeleton,omitempty"`
	PlanCall           LLMCallTrace       `json:"planCall"`
	TargetedResponse   *TargetedResponse  `json:"targetedResponse,omitempty"`
	MergedPlan         *plan.Plan         `json:"mergedPlan,omitempty"`
	FinalPlan          *plan.Plan         `json:"finalPlan,omitempty"`
	ValidationErr      string             `json:"validationErr,omitempty"`
	RetryCall          *LLMCallTrace      `json:"retryCall,omitempty"`
	RetryValidationErr string             `json:"retryValidationErr,omitempty"`
	TotalDurationMs    int64              `json:"totalDurationMs"`
	Error              string             `json:"error,omitempty"`

	// Wrong-plan escape fields.
	WrongPlanSignal *WrongPlanSignal `json:"wrongPlanSignal,omitempty"`
	WrongPlanCall   *LLMCallTrace    `json:"wrongPlanCall,omitempty"`
	ReselectionCall *LLMCallTrace    `json:"reselectionCall,omitempty"`

	// Workflow template fields.
	WorkflowName string `json:"workflowName,omitempty"`
	TemplatePath string `json:"templatePath,omitempty"`

	// Recipe YAML (compact representation of LLM decisions).
	RecipeYAML string `json:"recipeYaml,omitempty"`
}

// LLMCallTrace captures a single LLM request/response pair.
type LLMCallTrace struct {
	Messages        []MessageTrace `json:"messages,omitempty"`
	Temperature     float64        `json:"temperature"`
	MaxTokens       int            `json:"maxTokens,omitempty"`
	ThinkingBudget  int            `json:"thinkingBudget,omitempty"`
	ReasoningEffort string         `json:"reasoningEffort,omitempty"`
	ThinkingContent string         `json:"thinkingContent,omitempty"`
	RawResponse     string         `json:"rawResponse,omitempty"`
	Model           string         `json:"model,omitempty"`
	InputTokens     int            `json:"inputTokens,omitempty"`
	OutputTokens    int            `json:"outputTokens,omitempty"`
	FinishReason    string         `json:"finishReason,omitempty"`
	DurationMs      int64          `json:"durationMs"`
	Error           string         `json:"error,omitempty"`
}

// MessageTrace captures a single message in an LLM conversation.
type MessageTrace struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// SkeletonTrace captures the skeleton plan construction and the YAML sent to the LLM.
type SkeletonTrace struct {
	Plan        *plan.Plan `json:"plan"`
	YAML        string     `json:"yaml"`
	UnfedInputs []string   `json:"unfedInputs,omitempty"`
	DurationMs  int64      `json:"durationMs"`
}

// WritePlanTrace serializes a PlanTrace as indented JSON and writes it to the given path.
// Parent directories are created if they don't exist.
func WritePlanTrace(trace *PlanTrace, path string) error {
	data, err := json.MarshalIndent(trace, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling plan trace: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating trace directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing trace file: %w", err)
	}

	return nil
}

// generateTraceID returns a unique trace identifier in the format
// "trace-YYYYMMDD-HHMMSS-XXXXXXXX" where XXXXXXXX is an 8-character hex suffix.
func generateTraceID() string {
	now := time.Now()
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	return fmt.Sprintf("trace-%s-%08x", now.Format("20060102-150405"), suffix)
}

// toLLMCallTrace builds an LLMCallTrace from the request, response, and timing.
func toLLMCallTrace(req *llm.Request, resp *llm.Response, dur time.Duration, callErr error) LLMCallTrace {
	ct := LLMCallTrace{
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		DurationMs:  dur.Milliseconds(),
	}
	for _, m := range req.Messages {
		ct.Messages = append(ct.Messages, MessageTrace{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}
	if req.Thinking != nil {
		ct.ThinkingBudget = req.Thinking.BudgetTokens
		ct.ReasoningEffort = req.Thinking.ReasoningEffort
	}
	if callErr != nil {
		ct.Error = callErr.Error()
	}
	if resp != nil {
		ct.RawResponse = resp.Content
		ct.ThinkingContent = resp.Thinking
		ct.Model = resp.Model
		ct.InputTokens = resp.InputTokens
		ct.OutputTokens = resp.OutputTokens
		ct.FinishReason = resp.FinishReason
	}
	return ct
}
