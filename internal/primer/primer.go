// Package primer holds the AAT primer for AI coding assistants. The docs site
// publishes the same Markdown as its llms page and as llms-full.txt, and
// aat docs primer prints it, so the copy in a binary matches that binary.
package primer

import _ "embed" // for the go:embed directive

//go:embed llms.md
var markdown string

// Markdown returns the primer.
func Markdown() string {
	return markdown
}
