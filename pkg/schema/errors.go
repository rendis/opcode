package schema

import "fmt"

// Error codes for structured error reporting.
const (
	ErrCodeValidation      = "VALIDATION_ERROR"
	ErrCodeExecution       = "EXECUTION_ERROR"
	ErrCodeTimeout         = "TIMEOUT_ERROR"
	ErrCodeNotFound        = "NOT_FOUND"
	ErrCodeConflict        = "CONFLICT"
	ErrCodeInvalidTransition = "INVALID_TRANSITION"
	ErrCodeCycleDetected   = "CYCLE_DETECTED"
	ErrCodeStepFailed      = "STEP_FAILED"
	ErrCodeCancelled       = "CANCELLED"
	ErrCodeSignalFailed    = "SIGNAL_FAILED"
	ErrCodeRetryExhausted  = "RETRY_EXHAUSTED"
	ErrCodeStore           = "STORE_ERROR"
)

// OpcodeError is the structured error type for all OPCODE operations.
type OpcodeError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
	StepID  string         `json:"step_id,omitempty"`
	Cause   error          `json:"-"`
}

func (e *OpcodeError) Error() string {
	if e.StepID != "" {
		return fmt.Sprintf("[%s] step %s: %s", e.Code, e.StepID, e.Message)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *OpcodeError) Unwrap() error {
	return e.Cause
}

// NewError creates a new OpcodeError.
func NewError(code, message string) *OpcodeError {
	return &OpcodeError{Code: code, Message: message}
}

// NewErrorf creates a new OpcodeError with a formatted message.
func NewErrorf(code, format string, args ...any) *OpcodeError {
	return &OpcodeError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// WithStep attaches a step ID to the error.
func (e *OpcodeError) WithStep(stepID string) *OpcodeError {
	e.StepID = stepID
	return e
}

// WithCause attaches an underlying cause.
func (e *OpcodeError) WithCause(err error) *OpcodeError {
	e.Cause = err
	return e
}

// WithDetails attaches key-value details.
func (e *OpcodeError) WithDetails(details map[string]any) *OpcodeError {
	e.Details = details
	return e
}
