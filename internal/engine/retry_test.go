package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestIsRetryableError_Nil(t *testing.T) {
	assert.False(t, IsRetryableError(nil))
}

func TestIsRetryableError_ContextCanceled(t *testing.T) {
	assert.False(t, IsRetryableError(context.Canceled))
}

func TestIsRetryableError_ContextDeadlineExceeded(t *testing.T) {
	assert.True(t, IsRetryableError(context.DeadlineExceeded))
}

func TestIsRetryableError_OpcodeError_Retryable(t *testing.T) {
	// Execution errors are retryable.
	err := schema.NewError(schema.ErrCodeExecution, "action failed")
	assert.True(t, IsRetryableError(err))

	// Timeout errors are retryable.
	err = schema.NewError(schema.ErrCodeTimeout, "step timed out")
	assert.True(t, IsRetryableError(err))

	// Store errors are retryable.
	err = schema.NewError(schema.ErrCodeStore, "database connection lost")
	assert.True(t, IsRetryableError(err))

	// Step failed errors are retryable.
	err = schema.NewError(schema.ErrCodeStepFailed, "action threw")
	assert.True(t, IsRetryableError(err))
}

func TestIsRetryableError_OpcodeError_NonRetryable(t *testing.T) {
	nonRetryableCodes := []string{
		schema.ErrCodeValidation,
		schema.ErrCodeNotFound,
		schema.ErrCodeConflict,
		schema.ErrCodeInvalidTransition,
		schema.ErrCodeCycleDetected,
		schema.ErrCodeNonRetryable,
		schema.ErrCodePermissionDenied,
		schema.ErrCodeCircuitOpen,
	}

	for _, code := range nonRetryableCodes {
		err := schema.NewError(code, "test")
		assert.False(t, IsRetryableError(err), "expected %s to be non-retryable", code)
	}
}

func TestIsRetryableError_PlainError_DefaultRetryable(t *testing.T) {
	// Generic errors default to retryable.
	err := errors.New("something went wrong")
	assert.True(t, IsRetryableError(err))
}

func TestIsRetryableError_NetworkPatterns(t *testing.T) {
	patterns := []string{
		"connection refused",
		"connection reset by peer",
		"broken pipe",
		"unexpected EOF",
		"i/o timeout",
		"service unavailable",
		"bad gateway",
		"gateway timeout",
		"internal server error",
	}

	for _, p := range patterns {
		err := errors.New(p)
		assert.True(t, IsRetryableError(err), "expected %q to be retryable", p)
	}
}

func TestComputeBackoff_NilPolicy(t *testing.T) {
	assert.Equal(t, time.Duration(0), ComputeBackoff(nil, 0))
}

func TestComputeBackoff_EmptyDelay(t *testing.T) {
	policy := &schema.RetryPolicy{Max: 3, Backoff: "exponential"}
	assert.Equal(t, time.Duration(0), ComputeBackoff(policy, 0))
}

func TestComputeBackoff_InvalidDelay(t *testing.T) {
	policy := &schema.RetryPolicy{Max: 3, Backoff: "exponential", Delay: "invalid"}
	assert.Equal(t, time.Duration(0), ComputeBackoff(policy, 0))
}

func TestComputeBackoff_Constant(t *testing.T) {
	policy := &schema.RetryPolicy{Max: 3, Backoff: "constant", Delay: "100ms"}

	d0 := ComputeBackoff(policy, 0)
	d1 := ComputeBackoff(policy, 1)
	d2 := ComputeBackoff(policy, 2)

	assert.Equal(t, 100*time.Millisecond, d0)
	assert.Equal(t, 100*time.Millisecond, d1)
	assert.Equal(t, 100*time.Millisecond, d2)
}

func TestComputeBackoff_Exponential(t *testing.T) {
	policy := &schema.RetryPolicy{Max: 5, Backoff: "exponential", Delay: "10ms"}

	assert.Equal(t, 10*time.Millisecond, ComputeBackoff(policy, 0))
	assert.Equal(t, 20*time.Millisecond, ComputeBackoff(policy, 1))
	assert.Equal(t, 40*time.Millisecond, ComputeBackoff(policy, 2))
	assert.Equal(t, 80*time.Millisecond, ComputeBackoff(policy, 3))
}

func TestComputeBackoff_Linear(t *testing.T) {
	policy := &schema.RetryPolicy{Max: 5, Backoff: "linear", Delay: "10ms"}

	assert.Equal(t, 10*time.Millisecond, ComputeBackoff(policy, 0))
	assert.Equal(t, 20*time.Millisecond, ComputeBackoff(policy, 1))
	assert.Equal(t, 30*time.Millisecond, ComputeBackoff(policy, 2))
	assert.Equal(t, 40*time.Millisecond, ComputeBackoff(policy, 3))
}

func TestComputeBackoff_MaxDelay(t *testing.T) {
	policy := &schema.RetryPolicy{
		Max:      10,
		Backoff:  "exponential",
		Delay:    "10ms",
		MaxDelay: "50ms",
	}

	// Without cap: 10, 20, 40, 80, 160...
	// With max_delay=50ms: 10, 20, 40, 50, 50...
	assert.Equal(t, 10*time.Millisecond, ComputeBackoff(policy, 0))
	assert.Equal(t, 20*time.Millisecond, ComputeBackoff(policy, 1))
	assert.Equal(t, 40*time.Millisecond, ComputeBackoff(policy, 2))
	assert.Equal(t, 50*time.Millisecond, ComputeBackoff(policy, 3)) // capped
	assert.Equal(t, 50*time.Millisecond, ComputeBackoff(policy, 4)) // capped
}

func TestComputeBackoff_InvalidMaxDelay(t *testing.T) {
	policy := &schema.RetryPolicy{
		Max:      3,
		Backoff:  "exponential",
		Delay:    "10ms",
		MaxDelay: "invalid",
	}

	// Invalid max_delay is ignored — no cap.
	assert.Equal(t, 40*time.Millisecond, ComputeBackoff(policy, 2))
}

func TestComputeBackoff_None(t *testing.T) {
	policy := &schema.RetryPolicy{Max: 3, Backoff: "none", Delay: "100ms"}

	// "none" = constant base delay.
	assert.Equal(t, 100*time.Millisecond, ComputeBackoff(policy, 0))
	assert.Equal(t, 100*time.Millisecond, ComputeBackoff(policy, 1))
	assert.Equal(t, 100*time.Millisecond, ComputeBackoff(policy, 5))
}

func TestWaitForBackoff_ZeroDelay(t *testing.T) {
	err := WaitForBackoff(context.Background(), 0)
	assert.NoError(t, err)
}

func TestWaitForBackoff_NegativeDelay(t *testing.T) {
	err := WaitForBackoff(context.Background(), -1)
	assert.NoError(t, err)
}

func TestWaitForBackoff_Waits(t *testing.T) {
	start := time.Now()
	err := WaitForBackoff(context.Background(), 50*time.Millisecond)
	elapsed := time.Since(start)

	assert.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, 40*time.Millisecond) // allow some tolerance
}

func TestWaitForBackoff_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := WaitForBackoff(ctx, 5*time.Second)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Less(t, elapsed, time.Second) // should exit quickly, not wait 5s
}
