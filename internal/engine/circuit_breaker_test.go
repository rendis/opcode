package engine

import (
	"testing"
	"time"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker_StartsClosedAllowsRequests(t *testing.T) {
	cbr := NewCircuitBreakerRegistry(DefaultCircuitBreakerConfig())
	err := cbr.AllowRequest("test_action")
	assert.NoError(t, err)
	assert.Equal(t, CircuitClosed, cbr.GetState("test_action"))
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 3,
		Cooldown:         10 * time.Second,
		HalfOpenMax:      1,
	}
	cbr := NewCircuitBreakerRegistry(cfg)

	// Record 2 failures — still closed.
	cbr.RecordFailure("action_x")
	cbr.RecordFailure("action_x")
	assert.Equal(t, CircuitClosed, cbr.GetState("action_x"))

	// 3rd failure — opens the circuit.
	state := cbr.RecordFailure("action_x")
	assert.Equal(t, CircuitOpen, state)
	assert.Equal(t, CircuitOpen, cbr.GetState("action_x"))

	// Requests should now be rejected.
	err := cbr.AllowRequest("action_x")
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, schema.ErrCodeCircuitOpen, opErr.Code)
}

func TestCircuitBreaker_SuccessResetsFailures(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 3,
		Cooldown:         10 * time.Second,
		HalfOpenMax:      1,
	}
	cbr := NewCircuitBreakerRegistry(cfg)

	cbr.RecordFailure("action_y")
	cbr.RecordFailure("action_y")
	// 2 failures, then success resets.
	cbr.RecordSuccess("action_y")
	assert.Equal(t, CircuitClosed, cbr.GetState("action_y"))

	// Need 3 more failures to open.
	cbr.RecordFailure("action_y")
	cbr.RecordFailure("action_y")
	assert.Equal(t, CircuitClosed, cbr.GetState("action_y"))

	cbr.RecordFailure("action_y")
	assert.Equal(t, CircuitOpen, cbr.GetState("action_y"))
}

func TestCircuitBreaker_HalfOpenAfterCooldown(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		Cooldown:         50 * time.Millisecond,
		HalfOpenMax:      1,
	}
	cbr := NewCircuitBreakerRegistry(cfg)

	cbr.RecordFailure("action_z")
	cbr.RecordFailure("action_z")
	assert.Equal(t, CircuitOpen, cbr.GetState("action_z"))

	// Wait for cooldown.
	time.Sleep(60 * time.Millisecond)

	// Should transition to half-open.
	assert.Equal(t, CircuitHalfOpen, cbr.GetState("action_z"))

	// Allow one test request.
	err := cbr.AllowRequest("action_z")
	assert.NoError(t, err)
}

func TestCircuitBreaker_HalfOpenToClosedOnSuccess(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		Cooldown:         50 * time.Millisecond,
		HalfOpenMax:      1,
	}
	cbr := NewCircuitBreakerRegistry(cfg)

	// Open the circuit.
	cbr.RecordFailure("action_hoc")
	cbr.RecordFailure("action_hoc")
	assert.Equal(t, CircuitOpen, cbr.GetState("action_hoc"))

	// Wait for cooldown → half-open.
	time.Sleep(60 * time.Millisecond)
	assert.Equal(t, CircuitHalfOpen, cbr.GetState("action_hoc"))

	// Allow request and record success.
	err := cbr.AllowRequest("action_hoc")
	assert.NoError(t, err)
	cbr.RecordSuccess("action_hoc")

	// Should close.
	assert.Equal(t, CircuitClosed, cbr.GetState("action_hoc"))
}

func TestCircuitBreaker_HalfOpenToOpenOnFailure(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		Cooldown:         50 * time.Millisecond,
		HalfOpenMax:      1,
	}
	cbr := NewCircuitBreakerRegistry(cfg)

	// Open the circuit.
	cbr.RecordFailure("action_hof")
	cbr.RecordFailure("action_hof")

	// Wait for cooldown → half-open.
	time.Sleep(60 * time.Millisecond)
	err := cbr.AllowRequest("action_hof")
	assert.NoError(t, err)

	// Failure in half-open reopens.
	state := cbr.RecordFailure("action_hof")
	assert.Equal(t, CircuitOpen, state)
}

func TestCircuitBreaker_HalfOpenMaxRequests(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		Cooldown:         50 * time.Millisecond,
		HalfOpenMax:      1,
	}
	cbr := NewCircuitBreakerRegistry(cfg)

	cbr.RecordFailure("action_max")
	cbr.RecordFailure("action_max")

	time.Sleep(60 * time.Millisecond)

	// First request in half-open is allowed.
	err := cbr.AllowRequest("action_max")
	assert.NoError(t, err)

	// Second request in half-open is rejected (max reached).
	err = cbr.AllowRequest("action_max")
	assert.Error(t, err)
}

func TestCircuitBreaker_PerActionIsolation(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		Cooldown:         10 * time.Second,
		HalfOpenMax:      1,
	}
	cbr := NewCircuitBreakerRegistry(cfg)

	// Open circuit for action A.
	cbr.RecordFailure("action_a")
	cbr.RecordFailure("action_a")
	assert.Equal(t, CircuitOpen, cbr.GetState("action_a"))

	// Action B should still be closed.
	assert.Equal(t, CircuitClosed, cbr.GetState("action_b"))
	err := cbr.AllowRequest("action_b")
	assert.NoError(t, err)
}

func TestCircuitBreaker_GetStats(t *testing.T) {
	cbr := NewCircuitBreakerRegistry(DefaultCircuitBreakerConfig())
	cbr.RecordFailure("stats_action")
	cbr.RecordFailure("stats_action")

	stats := cbr.GetStats("stats_action")
	assert.Equal(t, "stats_action", stats["action"])
	assert.Equal(t, "closed", stats["state"])
	assert.Equal(t, 2, stats["consecutive_failures"])
}

func TestCircuitState_String(t *testing.T) {
	assert.Equal(t, "closed", CircuitClosed.String())
	assert.Equal(t, "open", CircuitOpen.String())
	assert.Equal(t, "half_open", CircuitHalfOpen.String())
	assert.Equal(t, "unknown", CircuitState(99).String())
}
