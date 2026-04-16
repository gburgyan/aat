package main

import (
	"fmt"
	"strings"
)

// ANSI color escape codes.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

// colorize wraps text in an ANSI escape code when enabled.
func colorize(text, code string, enabled bool) string {
	if !enabled {
		return text
	}
	return code + text + colorReset
}

// colorStatus returns a status code string colored by HTTP range.
// 2xx → green, 4xx → yellow, 5xx → red.
func colorStatus(statusCode int, enabled bool) string {
	text := fmt.Sprintf("%d", statusCode)
	if !enabled {
		return text
	}
	switch {
	case statusCode >= 200 && statusCode < 300:
		return colorGreen + text + colorReset
	case statusCode >= 400 && statusCode < 500:
		return colorYellow + text + colorReset
	case statusCode >= 500:
		return colorRed + text + colorReset
	default:
		return text
	}
}

// colorOutcome returns an outcome word colored appropriately.
// PASSED → green+bold, FAILED/ERROR → red+bold, SKIPPED → cyan.
func colorOutcome(outcome string, enabled bool) string {
	if !enabled {
		return outcome
	}
	switch outcome {
	case "PASSED":
		return colorBold + colorGreen + outcome + colorReset
	case "FAILED", "ERROR":
		return colorBold + colorRed + outcome + colorReset
	case "ABORTED":
		return colorBold + colorYellow + outcome + colorReset
	case "SKIPPED":
		return colorCyan + outcome + colorReset
	default:
		return outcome
	}
}

// nodeColWidth computes the node name column width for a given terminal width.
// overhead is the number of characters reserved for non-node content on the line.
// Returns a width between 15 (floor) and 40 (cap).
func nodeColWidth(termWidth, overhead int) int {
	if termWidth == 0 {
		termWidth = 80
	}
	w := termWidth - overhead
	if w < 15 {
		return 15
	}
	if w > 40 {
		return 40
	}
	return w
}

// truncateNode truncates a name to maxLen, adding a ~ suffix if truncated.
func truncateNode(name string, maxLen int) string {
	if len(name) <= maxLen {
		return name
	}
	if maxLen <= 1 {
		return "~"
	}
	return name[:maxLen-1] + "~"
}

// formatNodeCol formats a node name padded to the given width, optionally colored cyan.
// Handles ANSI-aware padding so visual alignment is preserved.
func formatNodeCol(name string, width int, color bool) string {
	truncated := truncateNode(name, width)
	if !color {
		return fmt.Sprintf("%-*s", width, truncated)
	}
	pad := width - len(truncated)
	if pad < 0 {
		pad = 0
	}
	return colorCyan + truncated + colorReset + strings.Repeat(" ", pad)
}
