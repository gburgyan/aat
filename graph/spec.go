package graph

import (
	"fmt"
	"strings"
)

// SpecSeverity indicates the severity of a spec validation issue.
type SpecSeverity string

const (
	SpecError   SpecSeverity = "error"
	SpecWarning SpecSeverity = "warning"
)

// SpecValidationIssue describes a single issue found during spec validation.
type SpecValidationIssue struct {
	Severity SpecSeverity
	Node     string
	Message  string
}

// SpecValidationResult collects all issues found during spec validation.
type SpecValidationResult struct {
	Issues []SpecValidationIssue
}

// HasErrors returns true if there are any error-severity issues.
func (r *SpecValidationResult) HasErrors() bool {
	for _, issue := range r.Issues {
		if issue.Severity == SpecError {
			return true
		}
	}
	return false
}

// HasIssues returns true if there are any issues at all.
func (r *SpecValidationResult) HasIssues() bool {
	return len(r.Issues) > 0
}

// Error returns error-severity issues as an error string.
func (r *SpecValidationResult) Error() string {
	var msgs []string
	for _, issue := range r.Issues {
		if issue.Severity == SpecError {
			msgs = append(msgs, fmt.Sprintf("node %q: %s", issue.Node, issue.Message))
		}
	}
	if len(msgs) == 0 {
		return ""
	}
	return fmt.Sprintf("spec validation errors:\n  - %s", strings.Join(msgs, "\n  - "))
}

// Format returns a human-readable string of all issues, grouped by severity.
func (r *SpecValidationResult) Format() string {
	var errors, warnings []string
	for _, issue := range r.Issues {
		line := fmt.Sprintf("node %q: %s", issue.Node, issue.Message)
		if issue.Severity == SpecError {
			errors = append(errors, line)
		} else {
			warnings = append(warnings, line)
		}
	}

	var parts []string
	if len(errors) > 0 {
		parts = append(parts, fmt.Sprintf("Errors:\n  - %s", strings.Join(errors, "\n  - ")))
	}
	if len(warnings) > 0 {
		parts = append(parts, fmt.Sprintf("Warnings:\n  - %s", strings.Join(warnings, "\n  - ")))
	}
	return strings.Join(parts, "\n")
}

// SpecValidator validates graph nodes against a loaded specification.
type SpecValidator interface {
	// CollectSpecPaths returns spec file paths referenced by the graph for this spec type.
	CollectSpecPaths(g *Graph) []string
	// LoadSpec loads a spec. refPath is the YAML reference key, fsPath is the resolved filesystem path.
	LoadSpec(refPath, fsPath string) error
	// Validate cross-references graph nodes against loaded specs.
	Validate(g *Graph) *SpecValidationResult
}
