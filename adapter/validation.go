package adapter

import "strings"

// ValidationResult captures the outcome of input or response validation.
// A nil *ValidationResult means "no validation performed" — distinct from
// a passing result where Valid is true.
type ValidationResult struct {
	Valid  bool
	Errors []ValidationError
}

// ValidationError describes a single validation failure, optionally scoped to a field.
type ValidationError struct {
	Field   string // e.g., "origin"; empty for general errors
	Message string
}

// Error returns a multi-line summary of all validation errors.
func (r *ValidationResult) Error() string {
	if r.Valid || len(r.Errors) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("validation failed:")
	for _, e := range r.Errors {
		b.WriteByte('\n')
		if e.Field != "" {
			b.WriteString("  " + e.Field + ": " + e.Message)
		} else {
			b.WriteString("  " + e.Message)
		}
	}
	return b.String()
}

// NewValidationPass returns a ValidationResult indicating all checks passed.
func NewValidationPass() *ValidationResult {
	return &ValidationResult{Valid: true}
}

// NewValidationFailure returns a ValidationResult with the given errors.
func NewValidationFailure(errs ...ValidationError) *ValidationResult {
	return &ValidationResult{
		Valid:  false,
		Errors: errs,
	}
}
