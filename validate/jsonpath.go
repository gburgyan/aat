package validate

import (
	"regexp"
	"strings"
)

var bracketRe = regexp.MustCompile(`\[(\d+)\]`)

// NormalizeJSONPath converts a JSONPath expression to GJSON syntax.
// Strips leading "$." and converts bracket notation [N] to dot notation .N.
func NormalizeJSONPath(path string) string {
	if path == "$" {
		return "@this"
	}
	path = strings.TrimPrefix(path, "$.")

	// Convert [N] bracket notation to .N dot notation
	path = bracketRe.ReplaceAllString(path, ".$1")

	return path
}
