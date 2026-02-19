package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/llm"
	"github.com/gburgyan/aat/plan"
)

// llmSelectValue asks the LLM to pick a value for an input when deterministic
// resolution has failed. It returns the LLM's suggested value, an LLMCallRecord
// capturing the full request/response details, and an error.
// Returns an error if the mode is strict, no LLM client is available, or the
// LLM response is empty.
func llmSelectValue(ctx context.Context, rctx *ResolveContext, input graph.Input, sv plan.StepValue, resolvedInputs map[string]any) (any, *LLMCallRecord, error) {
	mode := rctx.Mode
	if mode == "" {
		mode = config.ModeStrict
	}

	// Strict mode never calls the LLM
	if mode == config.ModeStrict {
		return nil, nil, fmt.Errorf("strict mode: LLM value selection not allowed for input %q", input.Name)
	}

	// Lean and adaptive call the LLM when pool is exhausted
	if rctx.LLM == nil {
		return nil, nil, fmt.Errorf("no LLM client configured for value selection of input %q", input.Name)
	}

	system, user := buildValueSelectionPrompt(input, sv, rctx.KB, resolvedInputs)

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: system},
		{Role: llm.RoleUser, Content: user},
	}

	record := &LLMCallRecord{
		Messages: []LLMMessage{
			{Role: string(llm.RoleSystem), Content: system},
			{Role: string(llm.RoleUser), Content: user},
		},
	}

	select {
	case <-ctx.Done():
		return nil, nil, fmt.Errorf("LLM value selection cancelled: %w", ctx.Err())
	default:
	}

	start := time.Now()
	resp, err := rctx.LLM.Complete(ctx, &llm.Request{
		Messages:    messages,
		MaxTokens:   256,
		Temperature: 0.3,
	})
	record.DurationMs = time.Since(start).Milliseconds()

	if err != nil {
		record.Error = err.Error()
		return nil, record, fmt.Errorf("LLM value selection for %q: %w", input.Name, err)
	}

	record.Model = resp.Model
	record.Response = resp.Content
	record.InputTokens = resp.InputTokens
	record.OutputTokens = resp.OutputTokens
	record.FinishReason = resp.FinishReason

	value := strings.TrimSpace(resp.Content)
	if value == "" {
		record.Error = "empty response"
		return nil, record, fmt.Errorf("LLM returned empty value for input %q", input.Name)
	}
	return value, record, nil
}

// buildValueSelectionPrompt constructs the system and user prompts for LLM
// value selection.
func buildValueSelectionPrompt(input graph.Input, sv plan.StepValue, kb *domain.KnowledgeBase, resolvedInputs map[string]any) (system, user string) {
	var sb strings.Builder
	sb.WriteString("You are an API testing assistant. Your task is to select a valid value for an API input field.\n")
	sb.WriteString("Return ONLY the value — no explanation, no quotes (unless the value itself requires them), no extra text.\n")
	system = sb.String()

	var ub strings.Builder
	fmt.Fprintf(&ub, "Select a value for the input field:\n\n")
	fmt.Fprintf(&ub, "Name: %s\n", input.Name)
	fmt.Fprintf(&ub, "Type: %s\n", input.Type)
	if input.Description != "" {
		fmt.Fprintf(&ub, "Description: %s\n", input.Description)
	}

	if sv.Constraint != "" {
		fmt.Fprintf(&ub, "\nConstraint: %s\n", sv.Constraint)
	}

	// Show values that were tried and rejected
	var tried []string
	if sv.Default != nil {
		tried = append(tried, fmt.Sprintf("%v", sv.Default))
	}
	for _, v := range sv.Pool {
		tried = append(tried, fmt.Sprintf("%v", v))
	}
	if len(tried) > 0 {
		fmt.Fprintf(&ub, "\nPreviously tried values (all rejected): %s\n", strings.Join(tried, ", "))
	}

	// Show already-resolved inputs for context
	if len(resolvedInputs) > 0 {
		ub.WriteString("\nOther resolved inputs:\n")
		for k, v := range resolvedInputs {
			fmt.Fprintf(&ub, "  %s = %v\n", k, v)
		}
	}

	// Include domain knowledge if available
	if kb != nil {
		// Type info
		if td := kb.TypeForField(input.Type); td != nil {
			fmt.Fprintf(&ub, "\nDomain type %q: %s (format: %s)\n", input.Type, td.Description, td.Format)
			if td.Pool != "" {
				if values := kb.AllValues(td.Pool); len(values) > 0 {
					max := 20
					if len(values) < max {
						max = len(values)
					}
					fmt.Fprintf(&ub, "Value pool %q: %s\n", td.Pool, strings.Join(values[:max], ", "))
				}
			}
		}

		// Applicable concepts
		concepts := kb.ConceptsForField(input.Name)
		if len(concepts) > 0 {
			ub.WriteString("\nApplicable domain concepts:\n")
			for _, c := range concepts {
				fmt.Fprintf(&ub, "  %s: %s\n", c.Name, c.Description)
			}
		}
	}

	user = ub.String()
	return
}
