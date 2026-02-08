package intent

import (
	"fmt"
	"strings"
)

// ExtractYAML extracts YAML content from an LLM response string.
// It tries, in order:
//  1. ```yaml fenced code block
//  2. ``` generic fenced code block
//  3. Raw YAML (if the response looks like valid YAML)
//
// Returns an error if no extractable YAML is found.
func ExtractYAML(response string) ([]byte, error) {
	response = strings.TrimSpace(response)
	if response == "" {
		return nil, fmt.Errorf("empty response")
	}

	// Try ```yaml block
	if content, ok := extractFencedBlock(response, "```yaml"); ok {
		return []byte(content), nil
	}

	// Try ``` generic block
	if content, ok := extractFencedBlock(response, "```"); ok {
		return []byte(content), nil
	}

	// Try raw YAML — must start with a YAML-like line
	if looksLikeYAML(response) {
		return []byte(response), nil
	}

	return nil, fmt.Errorf("no YAML found in response")
}

// extractFencedBlock finds the first occurrence of a fenced code block
// starting with the given prefix and ending with ```.
func extractFencedBlock(s, prefix string) (string, bool) {
	start := strings.Index(s, prefix)
	if start < 0 {
		return "", false
	}

	// Skip past the prefix line
	contentStart := start + len(prefix)
	newline := strings.Index(s[contentStart:], "\n")
	if newline < 0 {
		return "", false
	}
	contentStart += newline + 1

	// Find closing ```
	rest := s[contentStart:]
	end := strings.Index(rest, "```")
	if end < 0 {
		return "", false
	}

	content := strings.TrimSpace(rest[:end])
	if content == "" {
		return "", false
	}
	return content, true
}

// looksLikeYAML checks if a string starts with content that looks like YAML.
func looksLikeYAML(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// Common YAML starters
	if strings.HasPrefix(s, "---") {
		return true
	}
	// Key-value at start of line
	firstLine := s
	if idx := strings.Index(s, "\n"); idx >= 0 {
		firstLine = s[:idx]
	}
	firstLine = strings.TrimSpace(firstLine)
	// "key:" pattern or "- " list pattern
	return strings.Contains(firstLine, ":") || strings.HasPrefix(firstLine, "- ")
}
