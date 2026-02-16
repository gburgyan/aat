package intent

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/llm"
	"github.com/gburgyan/aat/plan"
)

// PlanTrace captures the full pipeline state of a prompt-to-plan transformation
// for debugging and observability. It records both LLM calls (prompts/responses),
// backward chaining decisions, skeleton construction, and merge results.
type PlanTrace struct {
	TraceID         string         `json:"traceId"`
	Timestamp       time.Time      `json:"timestamp"`
	Prompt          string         `json:"prompt"`
	GoalCall        LLMCallTrace   `json:"goalCall"`
	GoalAnalysis    *GoalAnalysis  `json:"goalAnalysis,omitempty"`
	GoalFallback    bool           `json:"goalFallback"`
	ChainResult     *ChainTrace    `json:"chainResult,omitempty"`
	Skeleton        *SkeletonTrace `json:"skeleton,omitempty"`
	PlanCall        LLMCallTrace   `json:"planCall"`
	LLMPlanYAML     string         `json:"llmPlanYaml,omitempty"`
	MergedPlan      *plan.Plan     `json:"mergedPlan,omitempty"`
	FinalPlan          *plan.Plan     `json:"finalPlan,omitempty"`
	ValidationErr      string         `json:"validationErr,omitempty"`
	RetryCall          *LLMCallTrace  `json:"retryCall,omitempty"`
	RetryValidationErr string         `json:"retryValidationErr,omitempty"`
	TotalDurationMs    int64          `json:"totalDurationMs"`
	Error              string         `json:"error,omitempty"`
}

// LLMCallTrace captures a single LLM request/response pair.
type LLMCallTrace struct {
	Messages     []MessageTrace `json:"messages,omitempty"`
	Temperature  float64        `json:"temperature"`
	RawResponse  string         `json:"rawResponse,omitempty"`
	Model        string         `json:"model,omitempty"`
	InputTokens  int            `json:"inputTokens,omitempty"`
	OutputTokens int            `json:"outputTokens,omitempty"`
	FinishReason string         `json:"finishReason,omitempty"`
	DurationMs   int64          `json:"durationMs"`
	Error        string         `json:"error,omitempty"`
}

// MessageTrace captures a single message in an LLM conversation.
type MessageTrace struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChainTrace captures the backward chaining result and timing.
type ChainTrace struct {
	Nodes      []string              `json:"nodes"`
	EntryNodes []string              `json:"entryNodes"`
	Edges      []EdgeTrace           `json:"edges"`
	Decisions  []graph.ChainDecision `json:"decisions,omitempty"`
	DurationMs int64                 `json:"durationMs"`
}

// EdgeTrace captures a single edge in the chain result.
type EdgeTrace struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Select    bool   `json:"select,omitempty"`
	Preferred bool   `json:"preferred,omitempty"`
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

// toEdgeTraces converts graph edges to EdgeTrace values.
func toEdgeTraces(edges []graph.Edge) []EdgeTrace {
	traces := make([]EdgeTrace, len(edges))
	for i, e := range edges {
		traces[i] = EdgeTrace{
			From:      e.From,
			To:        e.To,
			Select:    e.Select,
			Preferred: e.Preferred,
		}
	}
	return traces
}

// toChainTrace converts a ChainResult and duration to a ChainTrace.
func toChainTrace(cr *graph.ChainResult, dur time.Duration) *ChainTrace {
	return &ChainTrace{
		Nodes:      cr.Nodes,
		EntryNodes: cr.EntryNodes,
		Edges:      toEdgeTraces(cr.Edges),
		Decisions:  cr.Decisions,
		DurationMs: dur.Milliseconds(),
	}
}

// toLLMCallTrace builds an LLMCallTrace from the messages, response, and timing.
func toLLMCallTrace(system, user string, temp float64, resp *llm.Response, dur time.Duration, callErr error) LLMCallTrace {
	ct := LLMCallTrace{
		Messages: []MessageTrace{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: temp,
		DurationMs:  dur.Milliseconds(),
	}
	if callErr != nil {
		ct.Error = callErr.Error()
	}
	if resp != nil {
		ct.RawResponse = resp.Content
		ct.Model = resp.Model
		ct.InputTokens = resp.InputTokens
		ct.OutputTokens = resp.OutputTokens
		ct.FinishReason = resp.FinishReason
	}
	return ct
}
