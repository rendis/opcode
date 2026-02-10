package schema

import "fmt"

// ValidationSeverity indicates whether an issue is an error or warning.
type ValidationSeverity string

const (
	SeverityError   ValidationSeverity = "error"
	SeverityWarning ValidationSeverity = "warning"
)

// ValidationIssue is a single validation problem with location context.
type ValidationIssue struct {
	Path     string             `json:"path"`
	Code     string             `json:"code"`
	Message  string             `json:"message"`
	Severity ValidationSeverity `json:"severity"`
}

// ValidationResult aggregates all issues from the validation pipeline.
type ValidationResult struct {
	Errors   []ValidationIssue `json:"errors,omitempty"`
	Warnings []ValidationIssue `json:"warnings,omitempty"`
}

// Valid returns true if there are no errors (warnings are acceptable).
func (r *ValidationResult) Valid() bool {
	return len(r.Errors) == 0
}

// AddError appends an error-severity issue.
func (r *ValidationResult) AddError(path, code, message string) {
	r.Errors = append(r.Errors, ValidationIssue{
		Path: path, Code: code, Message: message, Severity: SeverityError,
	})
}

// AddWarning appends a warning-severity issue.
func (r *ValidationResult) AddWarning(path, code, message string) {
	r.Warnings = append(r.Warnings, ValidationIssue{
		Path: path, Code: code, Message: message, Severity: SeverityWarning,
	})
}

// Merge combines another ValidationResult into this one.
func (r *ValidationResult) Merge(other *ValidationResult) {
	if other == nil {
		return
	}
	r.Errors = append(r.Errors, other.Errors...)
	r.Warnings = append(r.Warnings, other.Warnings...)
}

// ToError converts the result to an OpcodeError if invalid, nil if valid.
func (r *ValidationResult) ToError() error {
	if r.Valid() {
		return nil
	}

	msg := r.Errors[0].Message
	if len(r.Errors) > 1 {
		msg = fmt.Sprintf("validation failed with %d errors", len(r.Errors))
	}

	return NewError(ErrCodeValidation, msg).
		WithDetails(map[string]any{
			"error_count":   len(r.Errors),
			"warning_count": len(r.Warnings),
			"errors":        r.Errors,
			"warnings":      r.Warnings,
		})
}
