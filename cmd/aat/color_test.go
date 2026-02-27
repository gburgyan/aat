package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestColorize_Enabled(t *testing.T) {
	result := colorize("hello", colorRed, true)
	assert.Equal(t, "\033[31mhello\033[0m", result)
}

func TestColorize_Disabled(t *testing.T) {
	result := colorize("hello", colorRed, false)
	assert.Equal(t, "hello", result)
}

func TestColorStatus(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		enabled bool
		want    string
	}{
		{"2xx enabled", 200, true, "\033[32m200\033[0m"},
		{"4xx enabled", 404, true, "\033[33m404\033[0m"},
		{"5xx enabled", 502, true, "\033[31m502\033[0m"},
		{"1xx enabled", 100, true, "100"},
		{"disabled", 200, false, "200"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, colorStatus(tc.status, tc.enabled))
		})
	}
}

func TestColorOutcome(t *testing.T) {
	tests := []struct {
		name    string
		outcome string
		enabled bool
		has     string
		noAnsi  bool
	}{
		{"PASSED enabled", "PASSED", true, "\033[32m", false},
		{"FAILED enabled", "FAILED", true, "\033[31m", false},
		{"ERROR enabled", "ERROR", true, "\033[31m", false},
		{"SKIPPED enabled", "SKIPPED", true, "\033[36m", false},
		{"PASSED disabled", "PASSED", false, "", true},
		{"unknown enabled", "OTHER", true, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := colorOutcome(tc.outcome, tc.enabled)
			if tc.noAnsi {
				assert.Equal(t, tc.outcome, result)
			} else {
				assert.Contains(t, result, tc.has)
				assert.Contains(t, result, tc.outcome)
				assert.Contains(t, result, colorReset)
			}
		})
	}
}

func TestNodeColWidth(t *testing.T) {
	tests := []struct {
		name      string
		termWidth int
		overhead  int
		want      int
	}{
		{"standard 80 col step", 80, 60, 20},
		{"standard 80 col cleanup", 80, 58, 22},
		{"wide terminal", 120, 60, 40},
		{"narrow terminal floors", 50, 60, 15},
		{"very wide caps at 40", 200, 60, 40},
		{"zero width defaults to 80", 0, 60, 20},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, nodeColWidth(tc.termWidth, tc.overhead))
		})
	}
}

func TestTruncateNode(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"no truncation needed", "short", 10, "short"},
		{"exact fit", "exact", 5, "exact"},
		{"truncates with tilde", "longNodeName", 8, "longNod~"},
		{"minimal truncation", "ab", 1, "~"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, truncateNode(tc.input, tc.maxLen))
		})
	}
}

func TestFormatNodeCol_NoColor(t *testing.T) {
	result := formatNodeCol("searchFlights", 20, false)
	assert.Equal(t, "searchFlights       ", result)
	assert.Len(t, result, 20)
}

func TestFormatNodeCol_WithColor(t *testing.T) {
	result := formatNodeCol("searchFlights", 20, true)
	assert.Contains(t, result, colorCyan)
	assert.Contains(t, result, "searchFlights")
	assert.Contains(t, result, colorReset)
	// Visual width should be 20 (13 chars + 7 spaces), ANSI codes are zero-width
}

func TestFormatNodeCol_Truncation(t *testing.T) {
	result := formatNodeCol("veryLongNodeNameThatExceedsWidth", 15, false)
	assert.Len(t, result, 15)
	assert.Contains(t, result, "~")
}
