package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gburgyan/aat/intent"
	"github.com/gburgyan/aat/plan"
	"gopkg.in/yaml.v3"
)

// TraceService provides read access to plan traces for the web API.
type TraceService struct {
	tracesDir string
}

// NewTraceService creates a TraceService that reads from the given directory.
func NewTraceService(tracesDir string) *TraceService {
	return &TraceService{tracesDir: tracesDir}
}

// ListTraces scans the traces directory for trace directories, reads each trace,
// and returns summaries sorted newest-first. limit=0 means no limit.
// Unreadable traces are skipped silently.
func (s *TraceService) ListTraces(limit int) ([]TraceListEntry, error) {
	if s.tracesDir == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(s.tracesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading traces directory: %w", err)
	}

	// Filter directories with "trace-" prefix.
	var traceDirs []os.DirEntry
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "trace-") {
			traceDirs = append(traceDirs, e)
		}
	}

	// Sort descending by name (timestamp-based = newest first).
	sort.Slice(traceDirs, func(i, j int) bool {
		return traceDirs[i].Name() > traceDirs[j].Name()
	})

	// Apply limit.
	if limit > 0 && len(traceDirs) > limit {
		traceDirs = traceDirs[:limit]
	}

	var results []TraceListEntry
	for _, dir := range traceDirs {
		t, err := s.loadTrace(dir.Name())
		if err != nil {
			continue
		}
		results = append(results, traceToListEntry(t))
	}

	return results, nil
}

// GetTrace loads a full trace detail by trace ID.
func (s *TraceService) GetTrace(id string) (*TraceDetail, error) {
	t, err := s.loadTrace(id)
	if err != nil {
		return nil, err
	}
	return traceToDetail(t), nil
}

// loadTrace reads a plan trace by trace ID. Returns ErrTraceNotFound if the
// trace directory or file doesn't exist.
func (s *TraceService) loadTrace(id string) (*intent.PlanTrace, error) {
	tracePath := filepath.Join(s.tracesDir, id, "plan-trace.json")
	data, err := os.ReadFile(tracePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("trace %q: %w", id, ErrTraceNotFound)
		}
		return nil, fmt.Errorf("reading trace %q: %w", id, err)
	}

	var t intent.PlanTrace
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parsing trace %q: %w", id, err)
	}
	return &t, nil
}

// --- conversion: PlanTrace → view model ---

func traceToListEntry(t *intent.PlanTrace) TraceListEntry {
	return TraceListEntry{
		TraceID:         t.TraceID,
		Timestamp:       t.Timestamp,
		Prompt:          t.Prompt,
		WorkflowName:    t.WorkflowName,
		TotalDurationMs: t.TotalDurationMs,
		HasError:        t.Error != "",
		LLMCallCount:    countLLMCalls(t),
	}
}

func traceToDetail(t *intent.PlanTrace) *TraceDetail {
	d := &TraceDetail{
		TraceID:              t.TraceID,
		Timestamp:            t.Timestamp,
		Prompt:               t.Prompt,
		SelectionCall:        traceLLMCallToDetail(&t.SelectionCall),
		SelectionRetryCall:   traceLLMCallPtrToDetail(t.SelectionRetryCall),
		WorkflowSelection:    t.WorkflowSelection,
		PlanCall:             traceLLMCallToDetail(&t.PlanCall),
		TargetedResponse:     t.TargetedResponse,
		MergedPlanYAML:       planToYAML(t.MergedPlan),
		FinalPlanYAML:        planToYAML(t.FinalPlan),
		ValidationErr:        t.ValidationErr,
		RetryCall:            traceLLMCallPtrToDetail(t.RetryCall),
		RetryValidationErr:   t.RetryValidationErr,
		TotalDurationMs:      t.TotalDurationMs,
		Error:                t.Error,
		WrongPlanSignal:      t.WrongPlanSignal,
		WrongPlanCall:        traceLLMCallPtrToDetail(t.WrongPlanCall),
		ReselectionCall:      traceLLMCallPtrToDetail(t.ReselectionCall),
		WorkflowName:         t.WorkflowName,
		TemplatePath:         t.TemplatePath,
		Repetitions:          t.Repetitions,
		TemplateExpandedYAML: planToYAML(t.TemplateExpanded),
		RecipeYAML:           t.RecipeYAML,
	}

	if t.Skeleton != nil {
		d.Skeleton = &SkeletonDetail{
			PlanYAML:    t.Skeleton.YAML,
			UnfedInputs: t.Skeleton.UnfedInputs,
			DurationMs:  t.Skeleton.DurationMs,
		}
	}

	return d
}

// traceLLMCallToDetail converts a non-nil LLMCallTrace to an LLMCallDetail.
func traceLLMCallToDetail(c *intent.LLMCallTrace) *LLMCallDetail {
	if c == nil {
		return nil
	}
	var msgs []LLMMessageDetail
	if len(c.Messages) > 0 {
		msgs = make([]LLMMessageDetail, len(c.Messages))
		for i, m := range c.Messages {
			msgs[i] = LLMMessageDetail{
				Role:    m.Role,
				Content: m.Content,
			}
		}
	}
	return &LLMCallDetail{
		Messages:     msgs,
		Model:        c.Model,
		Response:     c.RawResponse,
		InputTokens:  c.InputTokens,
		OutputTokens: c.OutputTokens,
		DurationMs:   c.DurationMs,
		FinishReason: c.FinishReason,
		Error:        c.Error,
	}
}

// traceLLMCallPtrToDetail converts an optional *LLMCallTrace to *LLMCallDetail.
func traceLLMCallPtrToDetail(c *intent.LLMCallTrace) *LLMCallDetail {
	if c == nil {
		return nil
	}
	return traceLLMCallToDetail(c)
}

// planToYAML marshals a plan to YAML string, returning empty string on nil or error.
func planToYAML(p *plan.Plan) string {
	if p == nil {
		return ""
	}
	data, err := yaml.Marshal(p)
	if err != nil {
		return ""
	}
	return string(data)
}

// countLLMCalls counts the number of non-empty LLM calls in a trace.
func countLLMCalls(t *intent.PlanTrace) int {
	count := 0
	if hasMessages(t.SelectionCall) {
		count++
	}
	if t.SelectionRetryCall != nil && hasMessages(*t.SelectionRetryCall) {
		count++
	}
	if hasMessages(t.PlanCall) {
		count++
	}
	if t.RetryCall != nil && hasMessages(*t.RetryCall) {
		count++
	}
	if t.WrongPlanCall != nil && hasMessages(*t.WrongPlanCall) {
		count++
	}
	if t.ReselectionCall != nil && hasMessages(*t.ReselectionCall) {
		count++
	}
	return count
}

// hasMessages checks whether an LLMCallTrace has any messages (i.e., was actually used).
func hasMessages(c intent.LLMCallTrace) bool {
	return len(c.Messages) > 0
}
