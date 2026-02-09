package engine

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/rendis/opcode/pkg/schema"
)

// IsRetryableError classifies whether an error should be retried.
// Retryable by default: network errors, timeouts, context.DeadlineExceeded.
// Non-retryable: validation errors, permission denied, typed OpcodeErrors with non-retryable codes.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Context deadline exceeded is retryable (step timeout, not workflow-level).
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Context cancelled is NOT retryable — means workflow is shutting down.
	if errors.Is(err, context.Canceled) {
		return false
	}

	// OpcodeError checks its own code.
	var opErr *schema.OpcodeError
	if errors.As(err, &opErr) {
		return opErr.IsRetryable()
	}

	// Network errors are retryable.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// String heuristics for common retryable patterns.
	msg := strings.ToLower(err.Error())
	retryablePatterns := []string{
		"connection refused",
		"connection reset",
		"broken pipe",
		"eof",
		"temporary failure",
		"i/o timeout",
		"service unavailable",
		"bad gateway",
		"gateway timeout",
		"internal server error",
		"too many requests",
	}
	for _, p := range retryablePatterns {
		if strings.Contains(msg, p) {
			return true
		}
	}

	// Default: retryable (conservative — let the retry policy limit attempts).
	return true
}

// ComputeBackoff calculates the delay before the next retry attempt.
// Supports none, constant, linear, and exponential backoff with optional max_delay cap.
func ComputeBackoff(policy *schema.RetryPolicy, attempt int) time.Duration {
	if policy == nil || policy.Delay == "" {
		return 0
	}

	base, err := time.ParseDuration(policy.Delay)
	if err != nil {
		return 0
	}

	var delay time.Duration
	switch policy.Backoff {
	case "exponential":
		// 2^attempt * base
		multiplier := time.Duration(1)
		for i := 0; i < attempt; i++ {
			multiplier *= 2
		}
		delay = base * multiplier
	case "linear":
		delay = base * time.Duration(attempt+1)
	case "constant":
		delay = base
	default: // "none" or empty
		delay = base
	}

	// Apply max_delay cap.
	if policy.MaxDelay != "" {
		maxDelay, parseErr := time.ParseDuration(policy.MaxDelay)
		if parseErr == nil && delay > maxDelay {
			delay = maxDelay
		}
	}

	return delay
}

// WaitForBackoff sleeps for the computed backoff duration or returns early if the context is cancelled.
// Returns an error if the context was cancelled during the wait.
func WaitForBackoff(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	select {
	case <-time.After(delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
