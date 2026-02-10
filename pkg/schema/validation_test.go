package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidationResult_EmptyIsValid(t *testing.T) {
	r := &ValidationResult{}
	assert.True(t, r.Valid())
}

func TestValidationResult_AddError(t *testing.T) {
	r := &ValidationResult{}
	r.AddError("steps[0].action", ErrCodeValidation, "action not found")

	assert.False(t, r.Valid())
	require.Len(t, r.Errors, 1)
	assert.Equal(t, "steps[0].action", r.Errors[0].Path)
	assert.Equal(t, ErrCodeValidation, r.Errors[0].Code)
	assert.Equal(t, "action not found", r.Errors[0].Message)
	assert.Equal(t, SeverityError, r.Errors[0].Severity)
}

func TestValidationResult_AddWarning(t *testing.T) {
	r := &ValidationResult{}
	r.AddWarning("steps[1].retry.max", ErrCodeValidation, "high retry count")

	assert.True(t, r.Valid(), "warnings alone should not make result invalid")
	require.Len(t, r.Warnings, 1)
	assert.Equal(t, SeverityWarning, r.Warnings[0].Severity)
}

func TestValidationResult_Merge(t *testing.T) {
	r1 := &ValidationResult{}
	r1.AddError("/", ErrCodeValidation, "err1")
	r1.AddWarning("/", ErrCodeValidation, "warn1")

	r2 := &ValidationResult{}
	r2.AddError("steps[0]", ErrCodeCycleDetected, "err2")
	r2.AddWarning("steps[1]", ErrCodeValidation, "warn2")

	r1.Merge(r2)

	assert.Len(t, r1.Errors, 2)
	assert.Len(t, r1.Warnings, 2)
}

func TestValidationResult_MergeNil(t *testing.T) {
	r := &ValidationResult{}
	r.AddError("/", ErrCodeValidation, "err")
	r.Merge(nil)
	assert.Len(t, r.Errors, 1)
}

func TestValidationResult_ToError_Valid(t *testing.T) {
	r := &ValidationResult{}
	r.AddWarning("/", ErrCodeValidation, "just a warning")
	assert.Nil(t, r.ToError())
}

func TestValidationResult_ToError_SingleError(t *testing.T) {
	r := &ValidationResult{}
	r.AddError("steps[0].action", ErrCodeValidation, "action not found")

	err := r.ToError()
	require.NotNil(t, err)

	opErr, ok := err.(*OpcodeError)
	require.True(t, ok)
	assert.Equal(t, ErrCodeValidation, opErr.Code)
	assert.Equal(t, "action not found", opErr.Message)
	assert.Equal(t, 1, opErr.Details["error_count"])
}

func TestValidationResult_ToError_MultipleErrors(t *testing.T) {
	r := &ValidationResult{}
	r.AddError("/", ErrCodeValidation, "err1")
	r.AddError("/", ErrCodeValidation, "err2")
	r.AddWarning("/", ErrCodeValidation, "warn1")

	err := r.ToError()
	require.NotNil(t, err)

	opErr, ok := err.(*OpcodeError)
	require.True(t, ok)
	assert.Contains(t, opErr.Message, "2 errors")
	assert.Equal(t, 2, opErr.Details["error_count"])
	assert.Equal(t, 1, opErr.Details["warning_count"])
}
