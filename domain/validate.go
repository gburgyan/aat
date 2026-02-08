package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidationError collects all structural validation errors for a KnowledgeBase.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("domain validation failed:\n  - %s", strings.Join(e.Errors, "\n  - "))
}

// Validate performs structural validation on a parsed KnowledgeBase.
// It collects all errors rather than failing fast, returning a
// *ValidationError if any problems are found.
func Validate(kb *KnowledgeBase) error {
	var errs []string

	// Must define at least one concept, type, or pool
	if len(kb.Concepts) == 0 && len(kb.Types) == 0 && len(kb.ValuePools) == 0 {
		errs = append(errs, "knowledge base must define at least one concept, type, or value pool")
	}

	// Validate concepts
	for name, c := range kb.Concepts {
		if c == nil {
			errs = append(errs, fmt.Sprintf("concept %q: is nil", name))
			continue
		}
		if c.Description == "" {
			errs = append(errs, fmt.Sprintf("concept %q: description is required", name))
		}
		if len(c.AppliesTo) == 0 {
			errs = append(errs, fmt.Sprintf("concept %q: at least one applies_to entry is required", name))
		}
	}

	// Validate types
	for name, t := range kb.Types {
		if t == nil {
			errs = append(errs, fmt.Sprintf("type %q: is nil", name))
			continue
		}
		if t.Description == "" {
			errs = append(errs, fmt.Sprintf("type %q: description is required", name))
		}
		if t.Format == "" {
			errs = append(errs, fmt.Sprintf("type %q: format is required", name))
		}
		if t.Validation != "" {
			if _, err := regexp.Compile(t.Validation); err != nil {
				errs = append(errs, fmt.Sprintf("type %q: invalid validation regex: %v", name, err))
			}
		}
		if t.Pool != "" {
			if kb.ValuePools[t.Pool] == nil {
				errs = append(errs, fmt.Sprintf("type %q: pool references unknown value pool %q", name, t.Pool))
			}
		}
	}

	// Validate value pools
	for name, p := range kb.ValuePools {
		if p == nil {
			errs = append(errs, fmt.Sprintf("pool %q: is nil", name))
			continue
		}
		if p.Description == "" {
			errs = append(errs, fmt.Sprintf("pool %q: description is required", name))
		}
		if p.Type == "" {
			errs = append(errs, fmt.Sprintf("pool %q: type is required", name))
		}
		if len(p.Values) == 0 && len(p.Groups) == 0 {
			errs = append(errs, fmt.Sprintf("pool %q: must have values or groups (at least one non-empty)", name))
		}
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}
