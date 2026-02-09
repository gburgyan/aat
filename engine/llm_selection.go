package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/llm"
	"github.com/gburgyan/aat/plan"
	"github.com/tidwall/gjson"
)

// llmSelectElement asks the LLM to pick the best element from an array.
// It returns a selectionResult with the chosen element, an LLMCallRecord
// capturing the full request/response, and an error.
//
// Mode enforcement: strict → error, lean/adaptive → allowed.
// If sel.Filter is set, the array is pre-filtered before passing to the LLM.
func llmSelectElement(ctx context.Context, rctx *ResolveContext, arr []any,
	sel *plan.SelectionConfig, inputName string, g *graph.Graph,
	sourceNode, sourceField string) (*selectionResult, *LLMCallRecord, error) {

	if rctx == nil {
		return nil, nil, fmt.Errorf("llm selection for %q: no resolve context", inputName)
	}

	mode := rctx.Mode
	if mode == "" {
		mode = config.ModeStrict
	}

	if mode == config.ModeStrict {
		return nil, nil, fmt.Errorf("strict mode: LLM element selection not allowed for input %q", inputName)
	}

	if rctx.LLM == nil {
		return nil, nil, fmt.Errorf("no LLM client configured for element selection of input %q", inputName)
	}

	// Apply filter first if specified
	working := arr
	if sel != nil && sel.Filter != "" {
		filtered, err := applyFilter(arr, sel.Filter)
		if err != nil {
			return nil, nil, fmt.Errorf("pre-filtering for llm selection: %w", err)
		}
		working = filtered
	}

	if len(working) == 0 {
		return nil, nil, fmt.Errorf("llm selection for %q: array is empty after filtering", inputName)
	}

	// Build prompt
	system, user := buildElementSelectionPrompt(working, sel, inputName, g, sourceNode, sourceField, rctx)

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

	start := time.Now()
	resp, err := rctx.LLM.Complete(ctx, &llm.Request{
		Messages:    messages,
		MaxTokens:   64,
		Temperature: 0.0,
	})
	record.DurationMs = time.Since(start).Milliseconds()

	if err != nil {
		record.Error = err.Error()
		return nil, record, fmt.Errorf("LLM element selection for %q: %w", inputName, err)
	}

	record.Model = resp.Model
	record.Response = resp.Content
	record.InputTokens = resp.InputTokens
	record.OutputTokens = resp.OutputTokens
	record.FinishReason = resp.FinishReason

	// Parse the index from the response
	idx, err := parseElementIndex(resp.Content, len(working))
	if err != nil {
		record.Error = err.Error()
		return nil, record, fmt.Errorf("LLM element selection for %q: %w", inputName, err)
	}

	return &selectionResult{
		element:      working[idx],
		index:        idx,
		filteredSize: len(working),
	}, record, nil
}

// parseElementIndex extracts a 0-based index from the LLM response.
// It tries plain integer first, then JSON {"index": N}.
func parseElementIndex(response string, arrayLen int) (int, error) {
	trimmed := strings.TrimSpace(response)

	// Try plain integer
	if idx, err := strconv.Atoi(trimmed); err == nil {
		if idx < 0 || idx >= arrayLen {
			return 0, fmt.Errorf("index %d out of bounds (array length %d)", idx, arrayLen)
		}
		return idx, nil
	}

	// Try JSON {"index": N}
	var parsed struct {
		Index int `json:"index"`
	}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		if parsed.Index < 0 || parsed.Index >= arrayLen {
			return 0, fmt.Errorf("index %d out of bounds (array length %d)", parsed.Index, arrayLen)
		}
		return parsed.Index, nil
	}

	return 0, fmt.Errorf("could not parse index from LLM response: %q", trimmed)
}

// buildElementSelectionPrompt constructs the system and user prompts for LLM
// element selection.
func buildElementSelectionPrompt(arr []any, sel *plan.SelectionConfig, inputName string,
	g *graph.Graph, sourceNode, sourceField string, rctx *ResolveContext) (system, user string) {

	var sb strings.Builder
	sb.WriteString("You are an API testing assistant. Your task is to select the best element from an array.\n")
	sb.WriteString("Return ONLY the 0-based index of the chosen element — no explanation, no extra text.\n")
	system = sb.String()

	var ub strings.Builder
	fmt.Fprintf(&ub, "Select an element from the array for input %q.\n\n", inputName)

	if sel != nil && sel.Prompt != "" {
		fmt.Fprintf(&ub, "Selection criteria: %s\n\n", sel.Prompt)
	}

	// Summarize elements
	maxShow := 10
	if len(arr) <= maxShow {
		maxShow = len(arr)
	}

	// Check if we have elementFields for a tabular summary
	var elementFields []graph.Field
	if g != nil {
		if node := g.Nodes[sourceNode]; node != nil {
			for _, out := range node.Outputs {
				if out.Name == sourceField && len(out.ElementFields) > 0 {
					elementFields = out.ElementFields
					break
				}
			}
		}
	}

	if len(elementFields) > 0 {
		// Tabular summary using elementField names
		ub.WriteString("Elements (key fields):\n")
		for i := 0; i < maxShow; i++ {
			fmt.Fprintf(&ub, "[%d]", i)
			data, err := json.Marshal(arr[i])
			if err == nil {
				for _, ef := range elementFields {
					val := gjsonGet(data, ef.EffectivePath())
					if val != "" {
						fmt.Fprintf(&ub, " %s=%s", ef.Name, val)
					}
				}
			}
			ub.WriteString("\n")
		}
	} else {
		// Raw JSON summary
		ub.WriteString("Elements:\n")
		for i := 0; i < maxShow; i++ {
			data, err := json.Marshal(arr[i])
			if err == nil {
				fmt.Fprintf(&ub, "[%d] %s\n", i, truncateJSON(string(data), 200))
			}
		}
	}

	if len(arr) > maxShow {
		fmt.Fprintf(&ub, "... and %d more elements (total: %d)\n", len(arr)-maxShow, len(arr))
	}

	// Include domain knowledge if available
	if rctx != nil && rctx.KB != nil {
		domainInfo := rctx.KB.FormatForPrompt()
		if domainInfo != "" {
			fmt.Fprintf(&ub, "\nDomain context:\n%s\n", domainInfo)
		}
	}

	user = ub.String()
	return
}

// gjsonGet extracts a value from JSON data using a gjson path and returns
// it as a string. Returns empty string if not found.
func gjsonGet(data []byte, path string) string {
	result := gjson.GetBytes(data, path)
	if !result.Exists() {
		return ""
	}
	return result.String()
}

// truncateJSON truncates a JSON string to maxLen characters, adding "..." if truncated.
func truncateJSON(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
